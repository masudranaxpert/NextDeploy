package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"panel/internal/db"
	"panel/internal/sysinfo"

	"github.com/gofiber/fiber/v2"
)

// EdgeVerifyDomain checks if a domain is legally configured on this NextDeploy instance.
// Called by Shield Node Caddy on-demand TLS before issuing Let's Encrypt certificates.
// GET /api/v1/edge/verify-domain?domain=example.com&token=shld_...
func (p *Panel) EdgeVerifyDomain(c *fiber.Ctx) error {
	domain := strings.TrimSpace(strings.ToLower(c.Query("domain")))
	if domain == "" {
		return c.Status(fiber.StatusBadRequest).SendString("Missing domain parameter")
	}

	token := strings.TrimSpace(c.Query("token"))
	ctx := c.UserContext()

	// 1. Verify token against shield_nodes if any nodes exist
	if token != "" {
		node, err := p.DB.GetShieldNodeByToken(ctx, token)
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

	// 2. Check app_domains (Docker Apps & PHP Hosting)
	var count int
	err := p.DB.DB().QueryRowContext(ctx,
		`SELECT COUNT(1) FROM app_domains 
		 WHERE LOWER(domain) = ? OR (enable_www = 1 AND LOWER(domain) = ?)`,
		domain, cleanDomain).Scan(&count)
	if err == nil && count > 0 {
		return c.SendString("OK")
	}

	// 3. Check proxy_routes
	err = p.DB.DB().QueryRowContext(ctx,
		`SELECT COUNT(1) FROM proxy_routes 
		 WHERE enabled = 1 AND (LOWER(domain) = ? OR (enable_www = 1 AND LOWER(domain) = ?))`,
		domain, cleanDomain).Scan(&count)
	if err == nil && count > 0 {
		return c.SendString("OK")
	}

	// 4. Check Panel Domain
	panelDomain := strings.ToLower(strings.TrimSpace(p.DB.GetSetting(ctx, "panel_domain")))
	if panelDomain != "" && (domain == panelDomain || domain == "www."+panelDomain) {
		return c.SendString("OK")
	}

	return c.Status(fiber.StatusNotFound).SendString("Domain not configured on this origin")
}

// EdgeShieldHeartbeat updates the last heartbeat time of a shield node.
// POST /api/v1/edge/shield-heartbeat or GET /api/v1/edge/shield-heartbeat?token=...
func (p *Panel) EdgeShieldHeartbeat(c *fiber.Ctx) error {
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

// GetEdgeShieldSetupScript returns a complete, automated bash installer for a Shield VPS node.
// GET /api/v1/edge/setup.sh?id=1&token=shld_...
func (p *Panel) GetEdgeShieldSetupScript(c *fiber.Ctx) error {
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
        apt-get install -qy debian-keyring debian-archive-keyring apt-transport-https curl
        curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' | gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg --yes
        curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' | tee /etc/apt/sources.list.d/caddy-stable.list
        apt-get update -qy
        apt-get install -qy caddy
    elif [ -f /etc/redhat-release ]; then
        yum install -y yum-plugin-copr curl
        yum copr enable -y @caddy/caddy
        yum install -y caddy
    else
        echo "Please install Caddy manually from https://caddyserver.com"
        exit 1
    fi
fi

echo "==> [2/4] Generating optimized Caddy reverse-proxy configuration..."
mkdir -p /etc/caddy
if [ -f /etc/caddy/Caddyfile ]; then
    BACKUP_FILE="/etc/caddy/Caddyfile.backup.$(date +%%Y%%m%%d_%%H%%M%%S)"
    echo "==> [Notice] Existing Caddyfile detected! Safely backed up to: $BACKUP_FILE"
    cp /etc/caddy/Caddyfile "$BACKUP_FILE"
fi

cat << 'EOF' > /etc/caddy/Caddyfile
{
    # Global On-Demand TLS with NextDeploy verification
    on_demand_tls {
        ask %s
    }
}

# Catch-all HTTPS for all verified domains
https:// {
    tls {
        on_demand
    }

    reverse_proxy http://%s:80 {
        header_up Host {host}
        header_up X-Real-IP {remote_host}
        header_up X-Forwarded-For {remote_host}
        header_up X-Forwarded-Proto {scheme}
        
        # Real-time WebSocket & streaming support
        flush_interval -1

        # Resilient timeouts for large file uploads & migrations
        transport http {
            response_header_timeout 600s
            dial_timeout 15s
        }
    }
}

# Catch-all HTTP (handles ACME challenges & basic forwarding)
http:// {
    reverse_proxy http://%s:80 {
        header_up Host {host}
        header_up X-Real-IP {remote_host}
        header_up X-Forwarded-For {remote_host}
        header_up X-Forwarded-Proto {scheme}
    }
}
EOF

echo "==> [3/4] Validating and starting Caddy service..."
if command -v systemctl &> /dev/null; then
    systemctl enable caddy
    systemctl restart caddy
else
    caddy reload --config /etc/caddy/Caddyfile || caddy start --config /etc/caddy/Caddyfile
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
`, node.Name, node.IP, originIP, askURL, originIP, originIP, heartbeatURL, node.Name, node.IP, originIP, node.IP)

	c.Set("Content-Type", "text/x-shellscript; charset=utf-8")
	return c.SendString(script)
}

// GetEdgeShieldUninstallScript returns a complete, automated bash uninstaller for a Shield VPS node.
// GET /api/v1/edge/uninstall.sh?token=shld_...
func (p *Panel) GetEdgeShieldUninstallScript(c *fiber.Ctx) error {
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
	originIP := sysinfo.DetectHostIP()
	return c.JSON(fiber.Map{
		"ok":        true,
		"nodes":     nodes,
		"origin_ip": originIP,
	})
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

	// Automatically sync Caddy TrustedProxies to include this shield IP
	p.syncShieldProxiesToCaddy(ctx)

	panelHost := c.Hostname()
	scheme := "http"
	if c.Secure() || c.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	setupCmd := fmt.Sprintf("curl -sSL \"%s://%s/api/v1/edge/setup.sh?token=%s\" | bash", scheme, panelHost, node.Token)
	uninstallCmd := fmt.Sprintf("curl -sSL \"%s://%s/api/v1/edge/uninstall.sh?token=%s\" | bash", scheme, panelHost, node.Token)

	return c.JSON(fiber.Map{
		"ok":                true,
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
	node := db.ShieldNode{
		ID:   int64(id),
		Name: strings.TrimSpace(body.Name),
		IP:   strings.TrimSpace(body.IP),
	}

	if err := p.DB.UpdateShieldNode(ctx, &node); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"ok": false, "error": err.Error()})
	}

	p.syncShieldProxiesToCaddy(ctx)
	return c.JSON(fiber.Map{"ok": true})
}

// DeleteShieldNode removes a Shield node and unlinks domains.
// DELETE /api/v1/shield-nodes/:id
func (p *Panel) DeleteShieldNode(c *fiber.Ctx) error {
	id, err := c.ParamsInt("id")
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"ok": false, "error": "Invalid ID"})
	}

	ctx := c.UserContext()
	if err := p.DB.DeleteShieldNode(ctx, int64(id)); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"ok": false, "error": err.Error()})
	}

	p.syncShieldProxiesToCaddy(ctx)
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
	_, err = p.DB.DB().ExecContext(ctx, `UPDATE app_domains SET shield_node_id = ? WHERE shield_node_id = ?`, body.TargetShieldID, id)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"ok": false, "error": err.Error()})
	}
	_, _ = p.DB.DB().ExecContext(ctx, `UPDATE proxy_routes SET shield_node_id = ? WHERE shield_node_id = ?`, body.TargetShieldID, id)
	_, _ = p.DB.DB().ExecContext(ctx, `UPDATE users SET default_shield_id = ? WHERE default_shield_id = ?`, body.TargetShieldID, id)

	return c.JSON(fiber.Map{"ok": true})
}

// syncShieldProxiesToCaddy ensures all active Shield Node IPs are in Caddy TrustedProxies.
func (p *Panel) syncShieldProxiesToCaddy(ctx context.Context) {
	nodes, err := p.DB.ListShieldNodes(ctx)
	if err != nil || len(nodes) == 0 {
		return
	}

	opts := p.caddyGlobalOptions(ctx)
	trustedSet := make(map[string]bool)
	for _, ip := range opts.TrustedProxies {
		trustedSet[strings.TrimSpace(ip)] = true
	}

	changed := false
	for _, n := range nodes {
		ip := strings.TrimSpace(n.IP)
		if ip != "" && !trustedSet[ip] {
			opts.TrustedProxies = append(opts.TrustedProxies, ip)
			trustedSet[ip] = true
			changed = true
		}
	}

	if changed {
		raw, _ := json.Marshal(opts)
		_ = p.DB.SetCaddyConfig(ctx, caddyOptionsKey, string(raw))
		_ = p.syncRootStackCompose(ctx)
	}
}
