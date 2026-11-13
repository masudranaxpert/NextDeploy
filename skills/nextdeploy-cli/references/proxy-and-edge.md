# Proxy, Caddy & Edge Shield (`nd`)

Reference for custom Caddy reverse proxy routes, global Caddy options (timeouts & Real-IP), Edge Shield proxy nodes (`nd shield`), and the standalone multi-panel Shield VPS daemon (`nd edge`).

---

## Caddy Reverse Proxy Routes (`nd proxy`)

Route any domain to an internal/external upstream HTTP(S) service or configure HTTP redirects.

```bash
# List and inspect routes
nd proxy list [--json]
nd proxy status [--json]             # Show panel domain TLS settings and live Caddy admin config

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

# Inspect or tail Caddy server logs
nd proxy logs [-n 100] [-f|--follow] [--json]
```

---

## Global Caddy Timeouts & Real-IP (`nd proxy options`)

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
Pass `--no-apply` to save without triggering an immediate Caddy reload.

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

Run on the **Edge Shield VPS** itself to install, monitor, and maintain the `nd-edge` daemon serving one or more NextDeploy panels.

```bash
nd edge install --panel-url https://panel.example.com --token <shield_token>
nd edge add-panel --panel-url https://panel2.example.com --token-stdin
nd edge remove-panel <panel_id|name>

nd edge list [--json]                # List connected panels & last sync revision
nd edge status [--json] [host]       # Per-host TLS & route reconciliation state
nd edge sync [--panel ID] [--force]  # Trigger immediate Caddyfile sync
nd edge doctor                       # Firewall, port 80/443, and TLS diagnostics
nd edge set-token <panel_id|name> --token-stdin
nd edge set-url <panel_id|name> --panel-url <url>
nd edge upgrade [--url <url>] [--sha256 <hex>]
nd edge uninstall [--all]
```
