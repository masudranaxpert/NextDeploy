# Proxy Engines (Caddy, Angie, Ferron) & Edge Shield (`nd`)

Reference for custom reverse proxy routes (`nd proxy`), switching the panel proxy engine (`nd proxy engine`), global proxy options (timeouts & Real-IP), Edge Shield nodes (`nd shield`), and the standalone multi-panel Shield VPS daemon (`nd edge` + `nd edge domain`).

---

## Reverse Proxy Routes (`nd proxy`)

Route any domain to an internal/external upstream HTTP(S) service or configure HTTP redirects.

```bash
# List and inspect routes
nd proxy list [--json]
nd proxy status [--json]             # Show active proxy engine, panel TLS settings, and live status

# Add a reverse proxy or redirect route
nd proxy add <domain> <upstream_url> [flags]
  --type proxy|redirect              # Route kind (default: proxy)
  --shield <node_id> | --direct      # Route through Edge Shield node or direct origin
  --www                              # Also answer requests for www.<domain>
  --no-https                         # Disable automatic HTTPS certificate provisioning
  --disabled                         # Create route in disabled state
  --skip-tls-verify                  # Trust self-signed HTTPS upstream certificates
  --trusted-proxy                    # Trust Cloudflare/upstream forwarded client IP headers
  --keep-path                        # Redirects: append visited path and query string
  --redirect-code 301|302|307|308    # Redirect HTTP status code
  --note "<text>" --json

# Enable, disable, toggle, delete, and reload
nd proxy enable <route_id> [--json]
nd proxy disable <route_id> [--json]
nd proxy toggle <route_id> [on|off] [--json]
nd proxy rm <route_id>
nd proxy reload

# Inspect or tail reverse proxy logs
nd proxy logs [-n 100] [-f|--follow] [--json]
```

---

## Reverse Proxy Engine Switching (`nd proxy engine`)

Inspect or switch the panel's active reverse proxy engine between **Caddy** (default), **Angie** (low RAM / high idle HTTP/2 concurrency), and **Ferron** (experimental). Admin only.

```bash
nd proxy engine status [--json]                         # Active engine, version, running state, last switch
nd proxy engine preflight <caddy|angie|ferron> [--json] # Check blockers, warnings, and rendered config validation
nd proxy engine set <caddy|angie|ferron> --yes [--experimental] [--json]
```

- Switching validates configs and rolls back automatically on failure.
- Selecting `ferron` requires `--experimental`.

---

## Global Proxy Timeouts & Real-IP (`nd proxy options`)

Manage server-wide timeouts, request body pause limits, header size caps, and trusted Real-IP proxy ranges.

```bash
nd proxy options show [--json]

# Apply a timeout preset and Cloudflare Real-IP trust
nd proxy options set --profile balanced --real-ip cloudflare

# Or configure custom trusted proxy CIDRs and explicit timeouts
nd proxy options set \
  --real-ip custom \
  --trusted-proxies "10.0.0.0/8,192.0.2.10/32" \
  --read-header 15s \
  --idle 2m \
  --grace-period 30s \
  --max-header-size 16KB
```

Valid profiles: `default`, `balanced`, `high`, `strict`, `custom`.
Valid `--real-ip` modes: `none`, `cloudflare`, `custom`.
Pass `--no-apply` to save without triggering an immediate proxy reload.

---

## Edge Shield Nodes (`nd shield`)

Manage Edge Shield nodes registered to the NextDeploy panel so domains terminate TLS and filter traffic on remote edge VPSes before tunneling back to origin.

```bash
nd shield list [--json]
nd shield add <name> <ip>
nd shield update <node_id> [--name <name>] [--ip <ip>]
nd shield rotate-token <node_id>
nd shield test <node_id>             # Verify edge health and panel reachability
nd shield setup-script [node_id|name]
nd shield reassign <source_node_id> [target_node_id]   # Omit target to move back to direct origin
nd shield rm <node_id>
```

---

## Multi-Panel Edge Daemon (`nd edge`)

Run on the **Edge Shield VPS** itself to install, switch proxies, manage standalone domains, and maintain the `nd-edge` daemon serving one or more NextDeploy panels.

```bash
# Install & switch reverse proxy engine on the Shield VPS
nd edge install --panel-url https://panel.example.com --token-stdin [--proxy caddy|angie|ferron] [--experimental] [--acme-email E] [--dns-gate on|off] [--adopt-existing] [--force]
nd edge switch-proxy <caddy|angie|ferron> [--experimental] [--yes]

# Multi-panel registration & sync
nd edge add-panel --panel-url https://panel2.example.com --token-stdin
nd edge remove-panel <panel_id|name>
nd edge list [--json]                # List connected panels & last sync revision
nd edge status [--json] [host]       # Per-host TLS & route reconciliation state
nd edge sync [--panel ID] [--force]  # Trigger immediate proxy config sync
nd edge doctor                       # Services, config, certificates, disk, and firewall diagnostics
nd edge set-token <panel_id|name> --token-stdin
nd edge set-url <panel_id|name> --panel-url <url>
nd edge upgrade [--url <url>] [--sha256 <hex>]
nd edge uninstall [--all]
```

### Standalone Manual & User-Config Domains (`nd edge domain`)

Manage non-panel reverse-proxy domains directly on the Shield VPS (survives panel syncs and `nd edge switch-proxy`), inspect or delete hand-written `user/*.conf` / unmanaged Caddyfile sites, and prune stale domains safely to 30-day trash:

```bash
# Add, update, enable/disable, or remove manual domains
nd edge domain add <domain> <upstream>[,<upstream>...] [--www] [--alias h] [--redirect-www to-apex|to-www] \
  [--tls auto|none|manual] [--no-verify] [--sni name] [--host-header original|upstream|VAL] \
  [--max-body 200m] [--timeout 60s] [--real-ip cloudflare|none] [--replace] [--dry-run] [--json]
nd edge domain set <domain> [flags...]
nd edge domain enable <domain>
nd edge domain disable <domain>
nd edge domain rm <domain>... [--yes] [--json]          # Moves manual domains to 30d trash; also removes user/*.conf / Caddyfile sites

# List, inspect, health-check, and prune stale domains
nd edge domain list [--all] [--json]                    # Manual + user-config sites; --all adds read-only panel domains
nd edge domain show <domain> [--config] [--json]        # Inspect config or rendered/raw proxy snippet
nd edge domain check [<domain>...] [--all] [--json]     # Read-only DNS, origin connectivity & TLS check
nd edge domain prune [--dry-run] [--apply] [--reason dns|origin|all] [--grace 7d] [--all] [--yes] [--json]
nd edge domain restore --list | <domain>                # Restore trashed domain (kept 30d)
nd edge domain export [--file backup.json]
nd edge domain import --file backup.json [--replace] [--dry-run] [--yes]
```
