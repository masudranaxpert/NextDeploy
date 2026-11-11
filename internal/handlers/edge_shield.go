package handlers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"panel/internal/db"
	"panel/internal/runutil"
	"panel/internal/sysinfo"

	"github.com/gofiber/fiber/v2"
)

const (
	settingPanelShieldNodeID  = "panel_shield_node_id"
	caddyShieldTrustedProxies = "shield_trusted_proxies"
)

// EdgeVerifyDomain checks if a domain is legally configured on this NextDeploy instance.
// Called by Shield Node Caddy on-demand TLS before issuing Let's Encrypt certificates.
// GET /api/v1/edge/verify-domain?domain=example.com&token=shld_...
//
// Deprecated: Legacy on-demand TLS verification endpoint. Use GET /api/v1/edge/routes instead.
func (p *Panel) EdgeVerifyDomain(c *fiber.Ctx) error {
	c.Set("Deprecation", "true")
	domain := strings.TrimSuffix(strings.TrimSpace(strings.ToLower(c.Query("domain"))), ".")
	if domain == "" {
		return c.Status(fiber.StatusBadRequest).SendString("Missing domain parameter")
	}

	token := strings.TrimSpace(c.Query("token"))
	ctx := c.UserContext()

	// 1. Verify token against shield_nodes if any nodes exist
	var node *db.ShieldNode
	if token != "" {
		var err error
		node, err = p.DB.GetShieldNodeByToken(ctx, token)
		if err == nil && node != nil {
			// Record heartbeat from this shield node
			_ = p.DB.UpdateShieldNodeHeartbeat(ctx, node.ID)
		} else {
			return c.Status(fiber.StatusUnauthorized).SendString("Invalid shield token")
		}
	} else {
		// If token wasn't passed, check if any shield nodes are registered.
		// If shield nodes exist, token is mandatory!
		nodes, _ := p.DB.ListShieldNodes(ctx)
		if len(nodes) > 0 {
			return c.Status(fiber.StatusUnauthorized).SendString("Shield token required")
		}
	}

	cleanDomain := strings.TrimPrefix(domain, "www.")

	// 2. Check app_domains (Docker Apps & PHP Hosting), scoped to node.ID when authenticated
	var count int
	if node != nil {
		err := p.DB.DB().QueryRowContext(ctx,
			`SELECT COUNT(1) FROM app_domains
			 WHERE shield_node_id = ? AND (LOWER(domain) = ? OR (enable_www = 1 AND LOWER(domain) = ?))`,
			node.ID, domain, cleanDomain).Scan(&count)
		if err == nil && count > 0 {
			return c.SendString("OK")
		}

		// 3. Check proxy_routes scoped to node.ID
		err = p.DB.DB().QueryRowContext(ctx,
			`SELECT COUNT(1) FROM proxy_routes
			 WHERE enabled = 1 AND shield_node_id = ? AND (LOWER(domain) = ? OR (enable_www = 1 AND LOWER(domain) = ?))`,
			node.ID, domain, cleanDomain).Scan(&count)
		if err == nil && count > 0 {
			return c.SendString("OK")
		}

		// 4. Check active preview hosts assigned to this shield node
		for _, pv := range p.activePreviewRoutesForShield(ctx, &node.ID) {
			if strings.EqualFold(pv.Domain, domain) {
				return c.SendString("OK")
			}
		}

		// 5. Check Panel Domain only if explicitly shielded on this node
		panelDomain := strings.ToLower(strings.TrimSpace(p.DB.GetSetting(ctx, settingPanelDomain)))
		if panelDomain != "" && p.panelShieldNodeID(ctx) == node.ID {
			if domain == panelDomain || (settingBool(p.DB.GetSetting(ctx, settingPanelEnableWWW), false) && domain == "www."+panelDomain) {
				return c.SendString("OK")
			}
		}
	} else {
		err := p.DB.DB().QueryRowContext(ctx,
			`SELECT COUNT(1) FROM app_domains
			 WHERE LOWER(domain) = ? OR (enable_www = 1 AND LOWER(domain) = ?)`,
			domain, cleanDomain).Scan(&count)
		if err == nil && count > 0 {
			return c.SendString("OK")
		}

		err = p.DB.DB().QueryRowContext(ctx,
			`SELECT COUNT(1) FROM proxy_routes
			 WHERE enabled = 1 AND (LOWER(domain) = ? OR (enable_www = 1 AND LOWER(domain) = ?))`,
			domain, cleanDomain).Scan(&count)
		if err == nil && count > 0 {
			return c.SendString("OK")
		}

		for _, pv := range p.activePreviewRoutesForShield(ctx, nil) {
			if strings.EqualFold(pv.Domain, domain) {
				return c.SendString("OK")
			}
		}

		panelDomain := strings.ToLower(strings.TrimSpace(p.DB.GetSetting(ctx, settingPanelDomain)))
		if panelDomain != "" && (domain == panelDomain || domain == "www."+panelDomain) {
			return c.SendString("OK")
		}
	}

	return c.Status(fiber.StatusNotFound).SendString("Domain not configured on this origin")
}

func (p *Panel) panelShieldNodeID(ctx context.Context) int64 {
	raw := strings.TrimSpace(p.DB.GetSetting(ctx, settingPanelShieldNodeID))
	if raw == "" {
		return 0
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return 0
	}
	return id
}

type previewRouteEntry struct {
	AppID  string
	Slot   string
	Domain string
}

func (p *Panel) activePreviewRoutesForShield(ctx context.Context, nodeID *int64) []previewRouteEntry {
	apps, err := p.DB.ListApps(ctx)
	if err != nil {
		return nil
	}
	var out []previewRouteEntry
	for _, app := range apps {
		pinned := strings.TrimSpace(app.PinnedSlot)
		standby := strings.TrimSpace(app.StandbySlot)
		if pinned == "" || standby == "" {
			continue
		}
		pv := p.previewSite(ctx, app, standby)
		if pv == nil || strings.TrimSpace(pv.Domain) == "" {
			continue
		}
		if nodeID != nil {
			if pv.ShieldNodeID == nil || *pv.ShieldNodeID != *nodeID {
				continue
			}
		}
		out = append(out, previewRouteEntry{
			AppID:  app.ID,
			Slot:   standby,
			Domain: strings.ToLower(strings.TrimSpace(pv.Domain)),
		})
	}
	return out
}

// EdgeShieldHeartbeat updates the last heartbeat time of a shield node.
// POST /api/v1/edge/shield-heartbeat or GET /api/v1/edge/shield-heartbeat?token=...
//
// Deprecated: Legacy heartbeat endpoint. Use POST /api/v1/edge/status instead.
func (p *Panel) EdgeShieldHeartbeat(c *fiber.Ctx) error {
	c.Set("Deprecation", "true")
	token := strings.TrimSpace(c.Query("token"))
	if token == "" {
		var body struct {
			Token string `json:"token"`
		}
		_ = c.BodyParser(&body)
		token = strings.TrimSpace(body.Token)
	}

	if token == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"ok": false, "error": "token required"})
	}

	ctx := c.UserContext()
	node, err := p.DB.GetShieldNodeByToken(ctx, token)
	if err != nil || node == nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"ok": false, "error": "invalid token"})
	}

	action := strings.TrimSpace(c.Query("action"))
	if action == "uninstall" || action == "unregistered" {
		_ = p.DB.UpdateShieldNodeStatus(ctx, node.ID, "unregistered")
		return c.JSON(fiber.Map{"ok": true, "status": "unregistered", "node": node.Name})
	}

	_ = p.DB.UpdateShieldNodeHeartbeat(ctx, node.ID)
	return c.JSON(fiber.Map{"ok": true, "status": "connected", "node": node.Name})
}

// ReadOriginCaddyRootCA reads the Origin Caddy local root CA PEM, its sha256 hex digest, and "ok"|"missing".
func (p *Panel) ReadOriginCaddyRootCA(ctx context.Context) (pem string, sha256Hex string, status string) {
	if p.originCAFn != nil {
		return p.originCAFn(ctx)
	}
	format := func(raw string) (string, string, string) {
		trimmed := strings.TrimSpace(raw) + "\n"
		sum := sha256.Sum256([]byte(trimmed))
		return trimmed, hex.EncodeToString(sum[:]), "ok"
	}

	// 1. Try reading directly from Caddy container via docker exec
	res := runutil.Run(ctx, "", nil, "docker", "exec", "caddy", "cat", "/data/caddy/pki/authorities/local/root.crt")
	if res.OK && strings.Contains(res.Output, "-----BEGIN CERTIFICATE-----") {
		return format(res.Output)
	}

	// 2. Try reading from host filesystem (if caddy_data volume or local path exists)
	for _, path := range []string{
		"/data/caddy/pki/authorities/local/root.crt",
		"/var/lib/docker/volumes/nextdeploy_caddy_data/_data/caddy/pki/authorities/local/root.crt",
	} {
		if data, rerr := os.ReadFile(path); rerr == nil && strings.Contains(string(data), "-----BEGIN CERTIFICATE-----") {
			return format(string(data))
		}
	}

	// 3. Fallback: query Caddy Admin API on internal docker network
	client := http.Client{Timeout: 3 * time.Second}
	for _, adminURL := range []string{"http://caddy:2019/pki/ca/local", "http://127.0.0.1:2019/pki/ca/local"} {
		resp, aerr := client.Get(adminURL)
		if aerr == nil {
			func() {
				defer resp.Body.Close()
				if resp.StatusCode == 200 {
					var caInfo struct {
						RootCert string `json:"root_certificate"`
					}
					if jerr := json.NewDecoder(resp.Body).Decode(&caInfo); jerr == nil && strings.Contains(caInfo.RootCert, "-----BEGIN CERTIFICATE-----") {
						pem, sha256Hex, status = format(caInfo.RootCert)
					}
				}
			}()
			if status == "ok" {
				return pem, sha256Hex, status
			}
		}
	}

	return "", "", "missing"
}

// GetOriginCaddyRootCA returns the Origin Caddy local root CA certificate (PEM).
// GET /api/v1/edge/origin-ca.crt?token=shld_...
//
// Deprecated: Legacy endpoint. Use GET /api/v1/edge/routes instead.
func (p *Panel) GetOriginCaddyRootCA(c *fiber.Ctx) error {
	c.Set("Deprecation", "true")
	token := strings.TrimSpace(c.Query("token"))
	if token == "" {
		return c.Status(fiber.StatusBadRequest).SendString("Missing token parameter")
	}

	ctx := c.UserContext()
	node, err := p.DB.GetShieldNodeByToken(ctx, token)
	if err != nil || node == nil {
		return c.Status(fiber.StatusUnauthorized).SendString("Invalid shield token")
	}

	pem, _, status := p.ReadOriginCaddyRootCA(ctx)
	if status == "ok" && pem != "" {
		c.Set("Content-Type", "application/x-x509-ca-cert; charset=utf-8")
		return c.SendString(pem)
	}

	return c.Status(fiber.StatusNotFound).SendString("Origin Caddy local root CA certificate not generated yet")
}

// GetEdgeShieldSetupScript returns a complete, automated bash installer for a Shield VPS node.
// GET /api/v1/edge/setup.sh?id=1&token=shld_...
//
// Deprecated: Legacy catch-all installer. Stage 2 replaces with nd edge install wrapper.
func (p *Panel) GetEdgeShieldSetupScript(c *fiber.Ctx) error {
	c.Set("Deprecation", "true")
	token := strings.TrimSpace(c.Query("token"))
	if token == "" {
		return c.Status(fiber.StatusBadRequest).SendString("Missing token parameter")
	}

	ctx := c.UserContext()
	node, err := p.DB.GetShieldNodeByToken(ctx, token)
	if err != nil || node == nil {
		return c.Status(fiber.StatusUnauthorized).SendString("Invalid shield token")
	}

	// Detect Origin Public IP
	originIP := sysinfo.DetectHostIP()
	if originIP == "" {
		originIP = c.IP()
	}

	// Panel Origin Scheme & Host for Caddy ask query
	panelHost := c.Hostname()
	scheme := "http"
	if c.Secure() || c.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}

	askURL := fmt.Sprintf("%s://%s/api/v1/edge/verify-domain?token=%s", scheme, panelHost, token)
	originCAURL := fmt.Sprintf("%s://%s/api/v1/edge/origin-ca.crt?token=%s", scheme, panelHost, token)
	heartbeatURL := fmt.Sprintf("%s://%s/api/v1/edge/shield-heartbeat?token=%s", scheme, panelHost, token)

	script := fmt.Sprintf(`#!/usr/bin/env bash
# ==============================================================================
# NextDeploy Shield Node Automated Setup Script
# Node: %s (IP: %s)
# Origin IP: %s
# ==============================================================================
set -e

echo "==> [1/4] Checking requirements & installing Caddy..."
if ! command -v caddy &> /dev/null; then
    if [ -f /etc/debian_version ]; then
        apt-get update -qy
        apt-get install -qy debian-keyring debian-archive-keyring apt-transport-https curl python3
        curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' | gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg --yes
        curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' | tee /etc/apt/sources.list.d/caddy-stable.list
        apt-get update -qy
        apt-get install -qy caddy
    elif [ -f /etc/redhat-release ]; then
        yum install -y yum-plugin-copr curl python3
        yum copr enable -y @caddy/caddy
        yum install -y caddy
    else
        echo "Please install Caddy manually from https://caddyserver.com"
        exit 1
    fi
fi

echo "==> [Kernel & Memory Tuning Check]..."
TOTAL_RAM_MB=$(free -m 2>/dev/null | awk '/Mem:/{print $2}' || echo "0")
TOTAL_RAM_MB=${TOTAL_RAM_MB:-0}
CURRENT_SWAP_MB=$(free -m 2>/dev/null | awk '/Swap:/{print $2}' || echo "0")
CURRENT_SWAP_MB=${CURRENT_SWAP_MB:-0}
CURRENT_CC=$(sysctl -n net.ipv4.tcp_congestion_control 2>/dev/null || echo "unknown")

IS_INTERACTIVE=0
if [ -c /dev/tty ] && [ -t 1 ]; then
    IS_INTERACTIVE=1
fi

# 1. Google BBR + FQ Congestion Control
if [ "$CURRENT_CC" = "bbr" ]; then
    echo "==> [Network] Google BBR is already active. Skipping."
else
    ENABLE_BBR="y"
    if [ "$IS_INTERACTIVE" -eq 1 ]; then
        read -r -p "Enable Google BBR + FQ network pacing for cross-continental speed? [Y/n]: " BBR_ANS </dev/tty || BBR_ANS="y"
        if [ "$BBR_ANS" = "n" ] || [ "$BBR_ANS" = "N" ]; then
            ENABLE_BBR="n"
        fi
    fi
    if [ "$ENABLE_BBR" = "y" ]; then
        echo "==> Enabling Google BBR & FQ pacing..."
        modprobe tcp_bbr 2>/dev/null || true
        if ! grep -q 'tcp_congestion_control=bbr' /etc/sysctl.conf 2>/dev/null; then
            echo 'net.core.default_qdisc=fq' >> /etc/sysctl.conf
            echo 'net.ipv4.tcp_congestion_control=bbr' >> /etc/sysctl.conf
            echo 'net.ipv4.tcp_slow_start_after_idle=0' >> /etc/sysctl.conf
            echo 'net.core.somaxconn=65535' >> /etc/sysctl.conf
        fi
        sysctl -p 2>/dev/null || true
        APPLIED_CC=$(sysctl -n net.ipv4.tcp_congestion_control 2>/dev/null || echo "")
        if [ "$APPLIED_CC" = "bbr" ]; then
            echo "==> [✓] Google BBR active."
        else
            echo "==> [Notice] Kernel did not switch to BBR (requires kernel 4.9+). Kept $APPLIED_CC."
        fi
    fi
fi

# 2. Swap Safety & Configuration Check
echo "==> [Memory] Total RAM: ${TOTAL_RAM_MB} MB | Existing Swap: ${CURRENT_SWAP_MB} MB"
SWAP_TARGET_GB=""

if [ "$CURRENT_SWAP_MB" -gt 0 ]; then
    if [ "$IS_INTERACTIVE" -eq 1 ]; then
        read -r -p "Existing Swap is ${CURRENT_SWAP_MB} MB. Keep existing swap? [Y/n, or enter size in GB (0 to remove)]: " SWAP_ANS </dev/tty || SWAP_ANS="y"
        if [ "$SWAP_ANS" = "0" ]; then
            SWAP_TARGET_GB="0"
        elif [ -n "$SWAP_ANS" ] && [ "$SWAP_ANS" -eq "$SWAP_ANS" ] 2>/dev/null; then
            SWAP_TARGET_GB="$SWAP_ANS"
        else
            echo "==> Keeping existing ${CURRENT_SWAP_MB} MB swap."
        fi
    else
        echo "==> Existing swap detected (${CURRENT_SWAP_MB} MB). Keeping intact."
    fi
else
    # No swap exists
    if [ "$IS_INTERACTIVE" -eq 1 ]; then
        read -r -p "No swap found. Configure Swapfile for buffer protection? [Default: 2 GB, 0 to skip, or enter GB]: " SWAP_ANS </dev/tty || SWAP_ANS="2"
        if [ -z "$SWAP_ANS" ]; then
            SWAP_TARGET_GB="2"
        elif [ "$SWAP_ANS" -eq "$SWAP_ANS" ] 2>/dev/null; then
            SWAP_TARGET_GB="$SWAP_ANS"
        fi
    else
        # In non-interactive mode, check environment or default to 2GB if RAM <= 2GB
        if [ -n "$SHIELD_SWAP_GB" ]; then
            SWAP_TARGET_GB="$SHIELD_SWAP_GB"
        elif [ "$TOTAL_RAM_MB" -gt 0 ] && [ "$TOTAL_RAM_MB" -le 2048 ]; then
            SWAP_TARGET_GB="2"
        fi
    fi
fi

if [ -n "$SWAP_TARGET_GB" ]; then
    if [ "$SWAP_TARGET_GB" -eq 0 ]; then
        echo "==> Disabling swap..."
        swapoff -a 2>/dev/null || true
        rm -f /swapfile 2>/dev/null || true
        sed -i '\|/swapfile|d' /etc/fstab 2>/dev/null || true
        echo "==> [✓] Swap removed/disabled."
    else
        echo "==> Setting up ${SWAP_TARGET_GB} GB Swapfile (/swapfile)..."
        swapoff /swapfile 2>/dev/null || true
        rm -f /swapfile 2>/dev/null || true
        (fallocate -l "${SWAP_TARGET_GB}G" /swapfile 2>/dev/null || dd if=/dev/zero of=/swapfile bs=1M count=$((SWAP_TARGET_GB * 1024)) 2>/dev/null) || true
        if [ -f /swapfile ]; then
            chmod 600 /swapfile
            mkswap /swapfile 2>/dev/null || true
            swapon /swapfile 2>/dev/null || true
            if ! grep -q '/swapfile' /etc/fstab 2>/dev/null; then
                echo '/swapfile none swap sw 0 0' >> /etc/fstab
            fi
            echo "==> [✓] ${SWAP_TARGET_GB} GB swapfile active and persistent."
        fi
    fi
fi

if [ -f /swapfile ] || [ "$CURRENT_SWAP_MB" -gt 0 ]; then
    if ! grep -q 'vm.swappiness' /etc/sysctl.conf 2>/dev/null; then
        echo 'vm.swappiness=10' >> /etc/sysctl.conf
        sysctl -w vm.swappiness=10 2>/dev/null || true
    fi
fi

echo "==> [2/4] Generating optimized Caddy reverse-proxy configuration..."
mkdir -p /etc/caddy
if [ -f /etc/caddy/Caddyfile ]; then
    BACKUP_FILE="/etc/caddy/Caddyfile.backup.$(date +%%Y%%m%%d_%%H%%M%%S)"
    echo "==> [Notice] Existing Caddyfile detected! Safely backed up to: $BACKUP_FILE"
    cp /etc/caddy/Caddyfile "$BACKUP_FILE"
fi

ORIGIN_CA_URL="%s"
ASK_URL="%s"
ORIGIN_IP="%s"

if curl -fsSL "$ORIGIN_CA_URL" -o /etc/caddy/origin-root.crt 2>/dev/null && [ -s /etc/caddy/origin-root.crt ]; then
    echo "==> [✓] Origin Root CA pinned successfully at /etc/caddy/origin-root.crt"
else
    echo "==> [Notice] Origin Root CA not available yet, using fallback verification"
    rm -f /etc/caddy/origin-root.crt 2>/dev/null || true
fi

if command -v python3 &>/dev/null; then
    python3 - "$ASK_URL" "$ORIGIN_IP" << 'PYEOF'
import os
import sys

ask_url = sys.argv[1]
origin_ip = sys.argv[2]
caddyfile_path = "/etc/caddy/Caddyfile"
origin_ca_path = "/etc/caddy/origin-root.crt"

has_ca = os.path.isfile(origin_ca_path) and os.path.getsize(origin_ca_path) > 0
tls_directive = f"tls_trust_pool file {{\n            pem_file {origin_ca_path}\n        }}" if has_ca else "tls_insecure_skip_verify"

content = ""
try:
    with open(caddyfile_path, "r", encoding="utf-8") as f:
        content = f.read().strip()
except Exception:
    content = ""

new_catchall = f"""# === NextDeploy Edge Shield Catch-all ===
https:// {{
    tls {{
        on_demand
    }}

    reverse_proxy https://{origin_ip}:443 {{
        header_up Host {{host}}
        header_up X-Real-IP {{remote_host}}
        header_up X-Forwarded-For {{remote_host}}
        header_up X-Forwarded-Proto https
        flush_interval -1
        lb_retries 2
        lb_try_duration 5s
        lb_try_interval 250ms
        transport http {{
            {tls_directive}
            tls_server_name {{host}}
            versions 1.1
            keepalive 30s
            keepalive_interval 15s
            keepalive_idle_conns 256
            keepalive_idle_conns_per_host 64
            response_header_timeout 600s
            dial_timeout 15s
        }}
    }}
}}

# Catch-all HTTP (redirects visitor to HTTPS edge)
http:// {{
    redir https://{{host}}{{uri}} permanent
}}
# === End NextDeploy Edge Shield Catch-all ==="""

if not content:
    merged = f"""{{
    # Global On-Demand TLS with NextDeploy verification
    on_demand_tls {{
        ask {ask_url}
    }}
}}

{new_catchall}
"""
else:
    # Smart merge: preserve all existing user sites and cleanly update NextDeploy catch-all
    if "on_demand_tls" not in content:
        lines = content.splitlines()
        first_code_idx = -1
        for i, line in enumerate(lines):
            stripped = line.strip()
            if stripped and not stripped.startswith("#"):
                first_code_idx = i
                break
        if first_code_idx != -1 and lines[first_code_idx].strip() == "{":
            lines.insert(first_code_idx + 1, f"    # NextDeploy verification\n    on_demand_tls {{\n        ask {ask_url}\n    }}")
            content = "\n".join(lines)
        else:
            content = f"""{{
    on_demand_tls {{
        ask {ask_url}
    }}
}}

""" + content

    start_marker = "# === NextDeploy Edge Shield Catch-all"
    end_marker = "# === End NextDeploy Edge Shield Catch-all ==="

    if start_marker in content:
        if end_marker in content:
            before = content.split(start_marker)[0].rstrip()
            after = content.split(end_marker)[1].lstrip()
            merged = before + "\n\n" + new_catchall + "\n\n" + after
        else:
            before = content.split(start_marker)[0].rstrip()
            merged = before + "\n\n" + new_catchall
    else:
        merged = content.rstrip() + "\n\n" + new_catchall

with open(caddyfile_path, "w", encoding="utf-8") as f:
    f.write(merged.strip() + "\n")
print("==> [Notice] NextDeploy Shield successfully merged into /etc/caddy/Caddyfile without modifying existing sites!")
PYEOF
else
    if [ ! -s /etc/caddy/Caddyfile ]; then
        cat << CADDY_EOF > /etc/caddy/Caddyfile
{
    on_demand_tls {
        ask $ASK_URL
    }
}
CADDY_EOF
    elif grep -q "# === NextDeploy Edge Shield Catch-all" /etc/caddy/Caddyfile; then
        if grep -q "# === End NextDeploy Edge Shield Catch-all ===" /etc/caddy/Caddyfile; then
            sed -i '/# === NextDeploy Edge Shield Catch-all/,/# === End NextDeploy Edge Shield Catch-all ===/d' /etc/caddy/Caddyfile
        else
            sed -i '/# === NextDeploy Edge Shield Catch-all/,$d' /etc/caddy/Caddyfile
        fi
    fi
    TLS_VERIFY="tls_insecure_skip_verify"
    if [ -s /etc/caddy/origin-root.crt ]; then
        TLS_VERIFY="tls_trust_pool file {
            pem_file /etc/caddy/origin-root.crt
        }"
    fi
    cat << CADDY_EOF >> /etc/caddy/Caddyfile

# === NextDeploy Edge Shield Catch-all ===
https:// {
    tls {
        on_demand
    }
    reverse_proxy https://$ORIGIN_IP:443 {
        header_up Host {host}
        header_up X-Real-IP {remote_host}
        header_up X-Forwarded-For {remote_host}
        header_up X-Forwarded-Proto https
        flush_interval -1
        lb_try_duration 5s
        lb_try_interval 250ms
        transport http {
            $TLS_VERIFY
            tls_server_name {host}
            versions 1.1
            keepalive 90s
            keepalive_interval 30s
            keepalive_idle_conns 256
            keepalive_idle_conns_per_host 64
            response_header_timeout 600s
            dial_timeout 15s
        }
    }
}
http:// {
    redir https://{host}{uri} permanent
}
# === End NextDeploy Edge Shield Catch-all ===
CADDY_EOF
fi

echo "==> [3/4] Validating and reloading Caddy service..."
caddy fmt --overwrite /etc/caddy/Caddyfile 2>/dev/null || true

if caddy validate --config /etc/caddy/Caddyfile; then
    echo "==> [✓] Caddy configuration validated successfully!"
    if command -v systemctl &> /dev/null; then
        systemctl enable caddy
        systemctl reload caddy || systemctl restart caddy
    else
        caddy reload --config /etc/caddy/Caddyfile || caddy start --config /etc/caddy/Caddyfile
    fi
else
    echo "==> [ERROR] Caddyfile validation failed! Rolling back to original configuration..."
    if [ -n "$BACKUP_FILE" ] && [ -f "$BACKUP_FILE" ]; then
        cp -f "$BACKUP_FILE" /etc/caddy/Caddyfile
        echo "==> [✓] Restored original Caddyfile. Your existing websites were kept completely safe."
    fi
    exit 1
fi

echo "==> [4/4] Sending registration heartbeat to NextDeploy origin..."
curl -sS -X POST "%s" || true

echo ""
echo "=========================================================================="
echo " [✓] NextDeploy Shield Node '%s' successfully configured!"
echo " Public IP: %s -> Forwarding to Origin: %s"
echo " All client domains pointed to %s will now automatically receive"
echo " Let's Encrypt SSL and proxy seamlessly with zero manual effort."
echo "=========================================================================="
`, node.Name, node.IP, originIP, originCAURL, askURL, originIP, heartbeatURL, node.Name, node.IP, originIP, node.IP)

	c.Set("Content-Type", "text/x-shellscript; charset=utf-8")
	return c.SendString(script)
}

// GetEdgeShieldUninstallScript returns a complete, automated bash uninstaller for a Shield VPS node.
// GET /api/v1/edge/uninstall.sh?token=shld_...
//
// Deprecated: Legacy uninstaller.
func (p *Panel) GetEdgeShieldUninstallScript(c *fiber.Ctx) error {
	c.Set("Deprecation", "true")
	token := strings.TrimSpace(c.Query("token"))
	if token == "" {
		return c.Status(fiber.StatusBadRequest).SendString("Missing token parameter")
	}

	ctx := c.UserContext()
	node, err := p.DB.GetShieldNodeByToken(ctx, token)
	if err != nil || node == nil {
		return c.Status(fiber.StatusUnauthorized).SendString("Invalid shield token")
	}

	panelHost := c.Hostname()
	scheme := "http"
	if c.Secure() || c.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}

	unregURL := fmt.Sprintf("%s://%s/api/v1/edge/shield-heartbeat?token=%s&action=uninstall", scheme, panelHost, token)

	script := fmt.Sprintf(`#!/usr/bin/env bash
# ==============================================================================
# NextDeploy Shield Node Automated Uninstall & Cleanup Script
# Node: %s (IP: %s)
# ==============================================================================
set -e

echo "==> [1/3] Restoring original Caddy configuration..."
LATEST_BACKUP=$(ls -t /etc/caddy/Caddyfile.backup.* 2>/dev/null | head -n 1 || true)

if [ -n "$LATEST_BACKUP" ] && [ -f "$LATEST_BACKUP" ]; then
    echo "==> Found backup configuration: $LATEST_BACKUP"
    cp -f "$LATEST_BACKUP" /etc/caddy/Caddyfile
    echo "==> Restored original /etc/caddy/Caddyfile from backup."
    
    echo "==> [2/3] Reloading Caddy with restored configuration..."
    if command -v systemctl &> /dev/null; then
        systemctl reload caddy || systemctl restart caddy || true
    else
        caddy reload --config /etc/caddy/Caddyfile || true
    fi
else
    echo "==> No prior Caddyfile backup found."
    if [ -f /etc/caddy/Caddyfile ]; then
        rm -f /etc/caddy/Caddyfile
        echo "==> Removed NextDeploy Caddyfile."
    fi
    echo "==> [2/3] Stopping and disabling Caddy service..."
    if command -v systemctl &> /dev/null; then
        systemctl stop caddy || true
        systemctl disable caddy || true
    fi
fi

echo "==> [3/3] Notifying NextDeploy origin..."
curl -sS -X POST "%s" || true

echo ""
echo "=========================================================================="
echo " [✓] NextDeploy Shield Node '%s' uninstalled successfully!"
echo " Caddy configuration restored and Edge Shield unlinked."
echo "=========================================================================="
`, node.Name, node.IP, unregURL, node.Name)

	c.Set("Content-Type", "text/x-shellscript; charset=utf-8")
	return c.SendString(script)
}

func (p *Panel) attachShieldNodeDomainStatuses(ctx context.Context, nodes []db.ShieldNode) {
	statuses, err := p.DB.ListAllShieldDomainStatuses(ctx)
	if err != nil || len(statuses) == 0 {
		return
	}
	byNode := make(map[int64][]db.ShieldDomainStatus)
	for _, st := range statuses {
		byNode[st.NodeID] = append(byNode[st.NodeID], st)
	}
	for i := range nodes {
		nodes[i].DomainStatuses = byNode[nodes[i].ID]
	}
}

type nodeHostStatusKey struct {
	nodeID int64
	host   string
}

func (p *Panel) loadEdgeStatusLookup(ctx context.Context) map[nodeHostStatusKey]db.ShieldDomainStatus {
	statuses, err := p.DB.ListAllShieldDomainStatuses(ctx)
	if err != nil || len(statuses) == 0 {
		return nil
	}
	byKey := make(map[nodeHostStatusKey]db.ShieldDomainStatus, len(statuses))
	for _, st := range statuses {
		host := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(st.Host), "."))
		if norm, ok := normalizeEdgeHost(host); ok {
			host = norm
		}
		byKey[nodeHostStatusKey{nodeID: st.NodeID, host: host}] = st
	}
	return byKey
}

func lookupNodeHostStatus(byKey map[nodeHostStatusKey]db.ShieldDomainStatus, nodeID int64, rawDomain string) *db.ShieldDomainStatus {
	if len(byKey) == 0 {
		return nil
	}
	host := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(rawDomain), "."))
	if norm, ok := normalizeEdgeHost(host); ok {
		host = norm
	}
	if st, ok := byKey[nodeHostStatusKey{nodeID: nodeID, host: host}]; ok {
		copySt := st
		return &copySt
	}
	return nil
}

func (p *Panel) attachDomainEdgeStatuses(ctx context.Context, domains []db.AppDomain) {
	hasShield := false
	for _, d := range domains {
		if d.ShieldNodeID != nil {
			hasShield = true
			break
		}
	}
	if !hasShield {
		return
	}
	byKey := p.loadEdgeStatusLookup(ctx)
	if len(byKey) == 0 {
		return
	}
	for i := range domains {
		if domains[i].ShieldNodeID == nil {
			continue
		}
		domains[i].EdgeStatus = lookupNodeHostStatus(byKey, *domains[i].ShieldNodeID, domains[i].Domain)
	}
}

func (p *Panel) attachProxyRouteEdgeStatuses(ctx context.Context, routes []db.ProxyRoute) {
	hasShield := false
	for _, r := range routes {
		if r.ShieldNodeID != nil {
			hasShield = true
			break
		}
	}
	if !hasShield {
		return
	}
	byKey := p.loadEdgeStatusLookup(ctx)
	if len(byKey) == 0 {
		return
	}
	for i := range routes {
		if routes[i].ShieldNodeID == nil {
			continue
		}
		routes[i].EdgeStatus = lookupNodeHostStatus(byKey, *routes[i].ShieldNodeID, routes[i].Domain)
	}
}

// ListShieldNodes returns all registered shield nodes with domain counts.
// GET /api/v1/shield-nodes
func (p *Panel) ListShieldNodes(c *fiber.Ctx) error {
	ctx := c.UserContext()
	nodes, err := p.DB.ListShieldNodes(ctx)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"ok": false, "error": err.Error()})
	}
	if nodes == nil {
		nodes = []db.ShieldNode{}
	}
	panelID, _ := p.DB.EnsurePanelID(ctx)
	u, ok := currentUser(c)
	if ok && u.Role != db.RoleAdmin {
		userShield := p.ResolveUserShield(ctx, &u)
		filtered := make([]db.ShieldNode, 0, len(nodes))
		for _, n := range nodes {
			n.Token = ""
			n.EdgeStatusJSON = ""
			n.LastError = ""
			n.DomainStatuses = nil
			if userShield == nil || n.ID == userShield.ID {
				filtered = append(filtered, n)
			}
		}
		// Mask origin IP from non-admins
		return c.JSON(fiber.Map{
			"ok":        true,
			"panel_id":  panelID,
			"nodes":     filtered,
			"origin_ip": "",
		})
	}
	p.attachShieldNodeDomainStatuses(ctx, nodes)
	originIP := p.originPublicIP(ctx)
	return c.JSON(fiber.Map{
		"ok":        true,
		"panel_id":  panelID,
		"nodes":     nodes,
		"origin_ip": originIP,
	})
}

func (p *Panel) originPublicIP(ctx context.Context) string {
	if p.originIPFn != nil {
		return p.originIPFn(ctx)
	}
	if ip := strings.TrimSpace(sysinfo.DetectHostIP()); ip != "" {
		return ip
	}
	return strings.TrimSpace(sysinfo.Collect(ctx).PublicIP)
}

// CreateShieldNode adds a new Shield VPS node.
// POST /api/v1/shield-nodes
func (p *Panel) CreateShieldNode(c *fiber.Ctx) error {
	var body struct {
		Name string `json:"name"`
		IP   string `json:"ip"`
	}
	if err := c.BodyParser(&body); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"ok": false, "error": "Invalid request body"})
	}

	name := strings.TrimSpace(body.Name)
	ip := strings.TrimSpace(body.IP)
	if name == "" || ip == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"ok": false, "error": "Name and IP are required"})
	}

	ctx := c.UserContext()
	node := db.ShieldNode{
		Name:   name,
		IP:     ip,
		Token:  db.GenerateShieldToken(),
		Status: "pending",
	}

	id, err := p.DB.CreateShieldNode(ctx, &node)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"ok": false, "error": err.Error()})
	}
	node.ID = id
	node.SyncState = "pending"

	// Automatically sync Caddy TrustedProxies to include this shield IP
	p.syncShieldProxiesToCaddy(ctx)

	panelHost := c.Hostname()
	scheme := "http"
	if c.Secure() || c.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	setupCmd := fmt.Sprintf("curl -sSL \"%s://%s/api/v1/edge/setup.sh?token=%s\" | bash", scheme, panelHost, node.Token)
	uninstallCmd := fmt.Sprintf("curl -sSL \"%s://%s/api/v1/edge/uninstall.sh?token=%s\" | bash", scheme, panelHost, node.Token)
	panelID, _ := p.DB.EnsurePanelID(ctx)

	return c.JSON(fiber.Map{
		"ok":                true,
		"panel_id":          panelID,
		"node":              node,
		"setup_command":     setupCmd,
		"uninstall_command": uninstallCmd,
	})
}

// UpdateShieldNode updates an existing Shield node.
// PUT /api/v1/shield-nodes/:id
func (p *Panel) UpdateShieldNode(c *fiber.Ctx) error {
	id, err := c.ParamsInt("id")
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"ok": false, "error": "Invalid ID"})
	}

	var body struct {
		Name string `json:"name"`
		IP   string `json:"ip"`
	}
	if err := c.BodyParser(&body); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"ok": false, "error": "Invalid request body"})
	}

	ctx := c.UserContext()
	oldNode, _ := p.DB.GetShieldNode(ctx, int64(id))

	node := db.ShieldNode{
		ID:   int64(id),
		Name: strings.TrimSpace(body.Name),
		IP:   strings.TrimSpace(body.IP),
	}

	if err := p.DB.UpdateShieldNode(ctx, &node); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"ok": false, "error": err.Error()})
	}

	if oldNode != nil && strings.TrimSpace(oldNode.IP) != "" && strings.TrimSpace(oldNode.IP) != node.IP {
		p.syncShieldProxiesToCaddy(ctx, oldNode.IP)
	} else {
		p.syncShieldProxiesToCaddy(ctx)
	}
	return c.JSON(fiber.Map{"ok": true})
}

func (p *Panel) collectShieldAffectedResources(ctx context.Context, nodeID int64) (domainIDs []int64, appIDs []string, routeIDs []int64) {
	if rows, err := p.DB.DB().QueryContext(ctx, `SELECT id, app_id FROM app_domains WHERE shield_node_id = ?`, nodeID); err == nil {
		seenApps := map[string]bool{}
		for rows.Next() {
			var did int64
			var appID string
			if rows.Scan(&did, &appID) == nil {
				domainIDs = append(domainIDs, did)
				if appID != "" && !seenApps[appID] {
					seenApps[appID] = true
					appIDs = append(appIDs, appID)
				}
			}
		}
		_ = rows.Close()
	}
	if rows, err := p.DB.DB().QueryContext(ctx, `SELECT id FROM proxy_routes WHERE shield_node_id = ?`, nodeID); err == nil {
		for rows.Next() {
			var rid int64
			if rows.Scan(&rid) == nil {
				routeIDs = append(routeIDs, rid)
			}
		}
		_ = rows.Close()
	}
	return domainIDs, appIDs, routeIDs
}

func (p *Panel) resyncAffectedShieldResources(ctx context.Context, appIDs []string, routeIDs []int64) {
	for _, appID := range appIDs {
		_ = p.SyncAndApplyBackground(ctx, appID)
	}
	if len(routeIDs) > 0 {
		p.ReconcileProxyRoutes(ctx)
	}
}

// DeleteShieldNode removes a Shield node, unlinks domains, prunes trusted proxies, and re-syncs affected origin routes.
// DELETE /api/v1/shield-nodes/:id
func (p *Panel) DeleteShieldNode(c *fiber.Ctx) error {
	id, err := c.ParamsInt("id")
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"ok": false, "error": "Invalid ID"})
	}

	ctx := c.UserContext()
	oldNode, _ := p.DB.GetShieldNode(ctx, int64(id))
	_, affectedAppIDs, affectedRouteIDs := p.collectShieldAffectedResources(ctx, int64(id))

	if err := p.DB.DeleteShieldNode(ctx, int64(id)); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"ok": false, "error": err.Error()})
	}

	if oldNode != nil && strings.TrimSpace(oldNode.IP) != "" {
		p.syncShieldProxiesToCaddy(ctx, oldNode.IP)
	} else {
		p.syncShieldProxiesToCaddy(ctx)
	}
	p.resyncAffectedShieldResources(ctx, affectedAppIDs, affectedRouteIDs)
	return c.JSON(fiber.Map{"ok": true})
}

// TestShieldNode tests TCP connectivity and HTTP status of a Shield node.
// POST /api/v1/shield-nodes/:id/test
func (p *Panel) TestShieldNode(c *fiber.Ctx) error {
	id, err := c.ParamsInt("id")
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"ok": false, "error": "Invalid ID"})
	}

	ctx := c.UserContext()
	node, err := p.DB.GetShieldNode(ctx, int64(id))
	if err != nil || node == nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"ok": false, "error": "Shield node not found"})
	}

	// 1. Test TCP port 80/443 on the Shield IP
	timeout := 4 * time.Second
	conn, tcpErr := net.DialTimeout("tcp", net.JoinHostPort(node.IP, "80"), timeout)
	if tcpErr != nil {
		return c.JSON(fiber.Map{
			"ok":        false,
			"reachable": false,
			"error":     fmt.Sprintf("TCP port 80 unreachable on %s: %v", node.IP, tcpErr),
		})
	}
	_ = conn.Close()

	// 2. HTTP ping to check if Caddy is responding
	client := &http.Client{Timeout: timeout}
	resp, httpErr := client.Get(fmt.Sprintf("http://%s/", node.IP))
	statusCode := 0
	if resp != nil {
		statusCode = resp.StatusCode
		_ = resp.Body.Close()
	}

	// Mark connected if responding
	_ = p.DB.UpdateShieldNodeHeartbeat(ctx, node.ID)

	return c.JSON(fiber.Map{
		"ok":          true,
		"reachable":   true,
		"http_status": statusCode,
		"error":       fmt.Sprintf("%v", httpErr),
	})
}

// ResolveUserShield returns the explicitly assigned default shield node for the given user.
func (p *Panel) ResolveUserShield(ctx context.Context, u *db.User) *db.ShieldNode {
	if u == nil {
		return nil
	}
	if u.DefaultShieldID != nil && *u.DefaultShieldID > 0 {
		if node, err := p.DB.GetShieldNode(ctx, *u.DefaultShieldID); err == nil && node != nil {
			return node
		}
	}
	return nil
}

// ResolveAppShield returns the shield node associated with an app based on its owner.
func (p *Panel) ResolveAppShield(ctx context.Context, app db.App) *db.ShieldNode {
	if app.OwnerID > 0 {
		if owner, err := p.DB.GetUserByID(ctx, app.OwnerID); err == nil {
			return p.ResolveUserShield(ctx, &owner)
		}
	}
	return nil
}

// ResolveUserPublicIP returns the IP and ShieldNodeID to be used for the given user.
func (p *Panel) ResolveUserPublicIP(ctx context.Context, u *db.User) (string, *int64) {
	if node := p.ResolveUserShield(ctx, u); node != nil && node.IP != "" {
		return node.IP, &node.ID
	}
	return sysinfo.Collect(ctx).PublicIP, nil
}

// ResolveAppPublicIP returns the IP and ShieldNodeID to be used for the given app.
func (p *Panel) ResolveAppPublicIP(ctx context.Context, app db.App) (string, *int64) {
	if node := p.ResolveAppShield(ctx, app); node != nil && node.IP != "" {
		return node.IP, &node.ID
	}
	return sysinfo.Collect(ctx).PublicIP, nil
}

// ReassignShieldNodeDomains migrates all domains from one Shield node to another (or to Direct Origin).
// POST /api/v1/shield-nodes/:id/reassign
func (p *Panel) ReassignShieldNodeDomains(c *fiber.Ctx) error {
	id, err := c.ParamsInt("id")
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"ok": false, "error": "Invalid ID"})
	}

	var body struct {
		TargetShieldID *int64 `json:"target_shield_id"`
	}
	_ = c.BodyParser(&body)

	ctx := c.UserContext()
	if body.TargetShieldID != nil && *body.TargetShieldID <= 0 {
		body.TargetShieldID = nil
	}
	var targetNode *db.ShieldNode
	if body.TargetShieldID != nil {
		n, err := p.DB.GetShieldNode(ctx, *body.TargetShieldID)
		if err != nil || n == nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"ok": false, "error": "Target shield node not found"})
		}
		targetNode = n
	}

	oldNode, _ := p.DB.GetShieldNode(ctx, int64(id))

	// Capture affected row IDs BEFORE updating shield_node_id so sslip/nip REPLACE only rewrites migrated rows.
	affectedDomainIDs, affectedAppIDs, affectedRouteIDs := p.collectShieldAffectedResources(ctx, int64(id))

	if body.TargetShieldID != nil {
		_, err = p.DB.DB().ExecContext(ctx, `UPDATE app_domains SET shield_node_id = ?, enable_https = 1 WHERE shield_node_id = ?`, body.TargetShieldID, id)
	} else {
		_, err = p.DB.DB().ExecContext(ctx, `UPDATE app_domains SET shield_node_id = NULL, pending_origin_apply = 0 WHERE shield_node_id = ?`, id)
	}
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"ok": false, "error": err.Error()})
	}
	if body.TargetShieldID != nil {
		_, _ = p.DB.DB().ExecContext(ctx, `UPDATE proxy_routes SET shield_node_id = ?, enable_https = 1 WHERE shield_node_id = ?`, body.TargetShieldID, id)
	} else {
		_, _ = p.DB.DB().ExecContext(ctx, `UPDATE proxy_routes SET shield_node_id = NULL, pending_origin_apply = 0 WHERE shield_node_id = ?`, id)
	}
	_, _ = p.DB.DB().ExecContext(ctx, `UPDATE users SET default_shield_id = ? WHERE default_shield_id = ?`, body.TargetShieldID, id)

	// Update any auto-domains (.sslip.io / .nip.io) ONLY for the migrated row IDs.
	if oldNode != nil && oldNode.IP != "" && len(affectedDomainIDs) > 0 {
		oldDashed := strings.ReplaceAll(strings.TrimSpace(oldNode.IP), ".", "-")
		var newDashed string
		if targetNode != nil && targetNode.IP != "" {
			newDashed = strings.ReplaceAll(strings.TrimSpace(targetNode.IP), ".", "-")
		} else {
			originIP := p.originPublicIP(ctx)
			newDashed = strings.ReplaceAll(strings.TrimSpace(originIP), ".", "-")
		}

		if oldDashed != "" && newDashed != "" && oldDashed != newDashed {
			placeholders := make([]string, len(affectedDomainIDs))
			args := make([]interface{}, 0, 2+len(affectedDomainIDs))
			args = append(args, oldDashed, newDashed)
			for i, did := range affectedDomainIDs {
				placeholders[i] = "?"
				args = append(args, did)
			}
			_, _ = p.DB.DB().ExecContext(ctx,
				fmt.Sprintf(`UPDATE app_domains SET domain = REPLACE(domain, ?, ?) WHERE id IN (%s) AND (domain LIKE '%%.sslip.io' OR domain LIKE '%%.nip.io')`, strings.Join(placeholders, ",")),
				args...)
		}
	}

	p.syncShieldProxiesToCaddy(ctx)
	_ = p.syncRootStackCompose(ctx)
	p.resyncAffectedShieldResources(ctx, affectedAppIDs, affectedRouteIDs)

	return c.JSON(fiber.Map{"ok": true})
}

// syncShieldProxiesToCaddy reconciles active Shield Node IPs in Caddy TrustedProxies,
// ensuring RealIP is set to "custom", pruning deleted/changed Shield IPs even when
// len(nodes) == 0, and triggering root stack apply when the options change.
func (p *Panel) syncShieldProxiesToCaddy(ctx context.Context, removedIPs ...string) {
	nodes, err := p.DB.ListShieldNodes(ctx)
	if err != nil {
		return
	}

	activeSet := make(map[string]bool)
	activeList := make([]string, 0, len(nodes))
	for _, n := range nodes {
		ip := strings.TrimSpace(n.IP)
		if ip != "" && !activeSet[ip] {
			activeSet[ip] = true
			activeList = append(activeList, ip)
		}
	}

	prevManagedSet := make(map[string]bool)
	if rawPrev := strings.TrimSpace(p.DB.GetCaddyConfig(ctx, caddyShieldTrustedProxies)); rawPrev != "" {
		var prevList []string
		if json.Unmarshal([]byte(rawPrev), &prevList) == nil {
			for _, ip := range prevList {
				ip = strings.TrimSpace(ip)
				if ip != "" {
					prevManagedSet[ip] = true
				}
			}
		}
	}
	for _, ip := range removedIPs {
		ip = strings.TrimSpace(ip)
		if ip != "" {
			prevManagedSet[ip] = true
		}
	}

	opts := p.caddyGlobalOptions(ctx)
	oldRealIP := opts.RealIP
	oldProxies := append([]string(nil), opts.TrustedProxies...)

	kept := make([]string, 0, len(opts.TrustedProxies)+len(activeList))
	keptSet := make(map[string]bool)
	for _, rawIP := range opts.TrustedProxies {
		ip := strings.TrimSpace(rawIP)
		if ip == "" {
			continue
		}
		// Prune previously managed shield IPs that are no longer active
		if prevManagedSet[ip] && !activeSet[ip] {
			continue
		}
		if !keptSet[ip] {
			keptSet[ip] = true
			kept = append(kept, ip)
		}
	}
	for _, ip := range activeList {
		if !keptSet[ip] {
			keptSet[ip] = true
			kept = append(kept, ip)
		}
	}

	opts.TrustedProxies = kept
	if len(opts.TrustedProxies) > 0 {
		opts.RealIP = "custom"
	} else if opts.RealIP == "custom" {
		opts.RealIP = "none"
	}

	// Persist current managed shield IP set
	if managedRaw, mErr := json.Marshal(activeList); mErr == nil {
		_ = p.DB.SetCaddyConfig(ctx, caddyShieldTrustedProxies, string(managedRaw))
	}

	changed := opts.RealIP != oldRealIP || len(opts.TrustedProxies) != len(oldProxies)
	if !changed {
		for i := range opts.TrustedProxies {
			if opts.TrustedProxies[i] != oldProxies[i] {
				changed = true
				break
			}
		}
	}

	if changed {
		raw, _ := json.Marshal(opts)
		_ = p.DB.SetCaddyConfig(ctx, caddyOptionsKey, string(raw))
		if err := p.syncRootStackCompose(ctx); err == nil {
			p.applyRootStackComposeBackground(p.nextDeployComposePath(), "caddy")
		}
	}
}

// StartShieldNodeHealthLoop runs proactive background health checks on all shield nodes.
func (p *Panel) StartShieldNodeHealthLoop() {
	p.probeAllShieldNodes()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		p.probeAllShieldNodes()
	}
}

func (p *Panel) probeAllShieldNodes() {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	nodes, err := p.DB.ListShieldNodes(ctx)
	if err != nil || len(nodes) == 0 {
		return
	}

	for _, node := range nodes {
		if node.Status == "unregistered" {
			continue
		}
		ip := strings.TrimSpace(node.IP)
		if ip == "" {
			continue
		}
		// Probe TCP port 80 with short 2.5s timeout
		conn, err := net.DialTimeout("tcp", net.JoinHostPort(ip, "80"), 2500*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			_ = p.DB.UpdateShieldNodeHeartbeat(ctx, node.ID)
		} else {
			// If last heartbeat was more than 10 minutes ago and unreachable, mark offline
			if node.LastHeartbeatAt != nil && time.Since(*node.LastHeartbeatAt) > 10*time.Minute && node.Status == "connected" {
				_ = p.DB.UpdateShieldNodeStatus(ctx, node.ID, "offline")
			}
		}
	}
}
