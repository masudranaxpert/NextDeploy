package cmd

import (
	"fmt"
	"io"
	"strings"
)

// edgeCommandDoc is the help text of one `nd edge` subcommand.
type edgeCommandDoc struct {
	Summary  string
	Usage    string
	Flags    [][2]string // flag, explanation
	Notes    []string
	Examples []string
}

// edgeCommandDocs is the single source of the per-command help (`nd edge <cmd> --help`).
// Keep the flags in step with the parsers in edge.go / edge_switch.go.
var edgeCommandDocs = map[string]edgeCommandDoc{
	"install": {
		Summary: "Install the reverse proxy and the nd-edge sync daemon on this Shield VPS and register it with a panel.",
		Usage:   "nd edge install --panel-url URL [--token TOKEN | --token-stdin] [--proxy caddy|angie|ferron] [--experimental] [--acme-email E] [--adopt-existing] [--panel-ca-sha256 S] [--insecure-http] [--dns-gate on|off] [--force]",
		Flags: [][2]string{
			{"--panel-url URL", "Panel address, e.g. https://panel.example.com (required; https unless --insecure-http)"},
			{"--token TOKEN", "Shield bearer token from the panel (Shield page). Also read from $ND_EDGE_TOKEN"},
			{"--token-stdin", "Read the token from stdin instead of the command line (keeps it out of shell history)"},
			{"--proxy NAME", "Reverse proxy to install: caddy (default), angie or ferron"},
			{"--experimental", "Required with --proxy ferron (still a release candidate)"},
			{"--acme-email E", "Contact e-mail for Let's Encrypt certificates"},
			{"--adopt-existing", "Caddy only: keep an existing Caddyfile, back it up and add nd's managed block after it"},
			{"--panel-ca-sha256 S", "Pin the panel's TLS CA (64 hex chars) when the panel uses a private CA"},
			{"--insecure-http", "Allow a plain http:// panel URL (the token is NOT encrypted; testing only)"},
			{"--dns-gate on|off", "Only serve a domain once its DNS points at this Shield or Cloudflare, so Let's Encrypt is not asked for certificates it cannot issue (default on)"},
			{"--force", "Take over a Shield node that the panel already shows as registered by another server"},
		},
		Notes: []string{
			"Angie needs OpenSSL 3.5+ to serve Cloudflare zones that connect to the origin with post-quantum key exchange only;",
			"older systems (for example Ubuntu 20.04) get HTTP 525 there. nd warns when it detects this.",
		},
		Examples: []string{
			"echo \"$TOKEN\" | nd edge install --panel-url https://panel.example.com --token-stdin --proxy angie",
		},
	},
	"switch-proxy": {
		Summary: "Replace the running reverse proxy (caddy, angie, ferron) with another one and roll back automatically on failure.",
		Usage:   "nd edge switch-proxy <caddy|angie|ferron> [--experimental] [--yes]",
		Flags: [][2]string{
			{"--experimental", "Required when the target is ferron"},
			{"--yes", "Skip the confirmation question (it is only asked when you run the command from a terminal)"},
		},
		Notes: []string{
			"Caddy certificates are imported into the new engine, then every active HTTPS host is probed before the old proxy is disabled.",
			"Managed manual domains ('nd edge domain') are automatically re-rendered and validated for the target engine.",
			"Hand-written sites are not converted: they live in your Caddyfile or in /etc/angie/nd-edge/user/ and have to be ported.",
			"Expect a short interruption while ports 80/443 move from one proxy to the other.",
		},
		Examples: []string{"nd edge switch-proxy angie --yes"},
	},
	"domain": {
		Summary: "Manage standalone manual reverse-proxy domains on this Shield (survives panel syncs and switch-proxy) and find/prune stale garbage domains.",
		Usage:   "nd edge domain <add|set|rm|list|show|enable|disable|check|prune|restore|export|import> [args...]",
		Notes: []string{
			"Subcommands: add, set, rm (aliases remove|delete), list (alias ls), show, enable, disable, check, prune, restore, export, import.",
			"All changes are validated against the active proxy (Caddy, Angie, or Ferron) BEFORE domains.json is updated, and rolled back on reload failure.",
			"Out of scope (kept in user/*.conf or unmanaged Caddyfile): per-path routing, rate limits, bot blocking, custom log formats, /media/ prefix stripping.",
		},
		Examples: []string{
			"nd edge domain add shop.example.com 10.0.0.5:8080 --www",
			"nd edge domain add api.example.com https://10.0.0.6 --no-verify --max-body 200m",
			"nd edge domain check",
			"nd edge domain prune            # shows what would be removed (dry-run)",
			"nd edge domain prune --apply    # removes qualifying domains to trash (restorable 30d)",
		},
	},
	"add-panel": {
		Summary: "Register this Shield with one more panel (one Shield can serve several panels).",
		Usage:   "nd edge add-panel --panel-url URL [--token TOKEN | --token-stdin] [--force] [--panel-ca-sha256 S] [--insecure-http]",
		Flags: [][2]string{
			{"--panel-url URL", "Address of the additional panel (required)"},
			{"--token TOKEN", "Shield bearer token created on that panel. Also read from $ND_EDGE_TOKEN"},
			{"--token-stdin", "Read the token from stdin"},
			{"--force", "Take over a Shield node that the panel already shows as registered by another server"},
			{"--panel-ca-sha256 S", "Pin the panel's TLS CA (64 hex chars)"},
			{"--insecure-http", "Allow a plain http:// panel URL (testing only)"},
		},
	},
	"remove-panel": {
		Summary: "Unregister one panel; its routes are removed from this Shield.",
		Usage:   "nd edge remove-panel <panel_id|name>",
		Notes:   []string{"Find the id or name with 'nd edge list'."},
	},
	"list": {
		Summary: "List the registered panels with their sync state, host count and conflicts.",
		Usage:   "nd edge list [--json]",
		Flags:   [][2]string{{"--json", "Machine-readable output"}},
	},
	"status": {
		Summary: "Show every routed host with its TLS and routing state, or one host in detail.",
		Usage:   "nd edge status [--json] [host]",
		Flags:   [][2]string{{"--json", "Machine-readable output (includes proxy_engine, proxy_running and proxy_status)"}},
		Examples: []string{
			"nd edge status",
			"nd edge status example.com",
		},
	},
	"sync": {
		Summary: "Ask the running daemon to fetch the panel state and re-apply it now instead of waiting for the next poll.",
		Usage:   "nd edge sync [--panel ID] [--force]",
		Flags: [][2]string{
			{"--panel ID", "Only this panel"},
			{"--force", "Reconcile in this process (stop nd-edge first), ignore the failure back-off and re-apply even if nothing changed"},
		},
	},
	"run": {
		Summary: "Run the sync daemon in the foreground. systemd starts it as nd-edge.service; you normally never run this by hand.",
		Usage:   "nd edge run",
	},
	"doctor": {
		Summary: "Check this Shield: root, systemd, config files, proxy binary and service, proxy configuration, clock skew, free disk and firewall hints.",
		Usage:   "nd edge doctor",
		Notes:   []string{"PASS = fine, WARN = fix recommended, INFO = please verify yourself (for example the cloud firewall)."},
	},
	"set-token": {
		Summary: "Replace the bearer token of a registered panel (after rotating it on the panel).",
		Usage:   "nd edge set-token <panel_id|name> [--token TOKEN | --token-stdin]",
		Flags: [][2]string{
			{"--token TOKEN", "New token. Also read from $ND_EDGE_TOKEN"},
			{"--token-stdin", "Read the token from stdin"},
		},
	},
	"set-url": {
		Summary: "Change the API address of a registered panel (for example after moving it to another domain).",
		Usage:   "nd edge set-url <panel_id|name> --panel-url URL [--panel-ca-sha256 S] [--insecure-http]",
		Flags: [][2]string{
			{"--panel-url URL", "New panel address (required)"},
			{"--panel-ca-sha256 S", "Pin the panel's TLS CA (64 hex chars)"},
			{"--insecure-http", "Allow a plain http:// panel URL (testing only)"},
		},
	},
	"upgrade": {
		Summary: "Replace this nd binary with a newer release; the daemon is restarted and the old binary is kept if the check fails.",
		Usage:   "nd edge upgrade [--url URL] [--sha256 HEX]",
		Flags: [][2]string{
			{"--url URL", "Download address of the nd binary (default: latest GitHub release for this OS/arch)"},
			{"--sha256 HEX", "Expected SHA-256 of the download; the upgrade is refused on mismatch"},
		},
	},
	"uninstall": {
		Summary: "Remove the nd-edge daemon and everything nd generated for the active proxy.",
		Usage:   "nd edge uninstall [--all]",
		Flags: [][2]string{
			{"--all", "Also unregister from every panel that is still registered and delete the imported certificates"},
		},
		Notes: []string{
			"Without --all the command refuses while panels are still registered ('nd edge remove-panel' first).",
			"Caddy: your own Caddyfile content is kept (a backup is restored if one exists).",
			"Angie / Ferron: your own sites in the proxy's user/ directory are kept; nd's generated files are removed.",
			"The proxy package itself is not uninstalled.",
		},
	},
}

// edgeCommandOrder is the order of `nd edge --help`.
var edgeCommandOrder = []string{
	"install", "switch-proxy", "domain", "add-panel", "remove-panel", "list", "status", "sync",
	"run", "doctor", "set-token", "set-url", "upgrade", "uninstall",
}

var edgeDomainOrder = []string{
	"add", "set", "rm", "list", "show", "enable", "disable", "check", "prune", "restore", "export", "import",
}

var edgeDomainDocs = map[string]edgeCommandDoc{
	"add": {
		Summary: "Add a manual domain reverse proxy (or replace an existing user/*.conf / Caddyfile entry with --replace).",
		Usage:   "nd edge domain add <domain> <upstream>[,<upstream>...] [--www] [--alias host] [--redirect-www to-apex|to-www] [--tls auto|none|manual] [--cert F] [--key F] [--force-https] [--no-force-https] [--origin-http] [--origin-https] [--lb round_robin|least_conn|ip_hash] [--no-verify] [--sni name] [--host-header original|upstream|VAL] [--websocket] [--no-websocket] [--max-body SIZE] [--timeout DUR] [--connect-timeout DUR] [--real-ip cloudflare|none] [--header-up K=V] [--header-down K=V] [--keepalive N] [--note TEXT] [--disabled] [--replace] [--dry-run] [--json]",
		Flags: [][2]string{
			{"--www", "Also serve www.<domain>"},
			{"--alias host", "Extra hostnames on the same site (repeatable)"},
			{"--redirect-www", "308 redirect one form to the other: to-apex or to-www"},
			{"--tls", "TLS mode: auto (ACME, DNS-gated), none (HTTP only), or manual (--cert/--key)"},
			{"--cert F", "Certificate PEM path for --tls manual (copied into nd certs dir)"},
			{"--key F", "Private key PEM path for --tls manual (copied into nd certs dir)"},
			{"--force-https", "Redirect port 80 to 443 when TLS is active (default: on)"},
			{"--no-force-https", "Serve plain HTTP alongside HTTPS instead of redirecting port 80"},
			{"--origin-http", "Connect to upstreams over plain HTTP"},
			{"--origin-https", "Connect to upstreams over HTTPS"},
			{"--lb", "Load balancing across multiple upstreams: round_robin, least_conn, ip_hash"},
			{"--no-verify", "Skip TLS verification when connecting to HTTPS origin"},
			{"--sni name", "Override TLS SNI sent to HTTPS origin"},
			{"--host-header", "Host header sent to origin: original (default), upstream, or literal value"},
			{"--websocket", "Enable WebSocket upgrades (default: on)"},
			{"--no-websocket", "Disable WebSocket upgrade headers"},
			{"--max-body SIZE", "Client request body limit, e.g. 100m or 0 for unlimited"},
			{"--timeout DUR", "Origin read/send timeout (default: 300s)"},
			{"--connect-timeout DUR", "Origin connect timeout (default: 10s)"},
			{"--real-ip", "Real visitor IP handling: cloudflare or none"},
			{"--header-up K=V", "Add request header sent to origin (repeatable)"},
			{"--header-down K=V", "Add response header returned to clients (repeatable)"},
			{"--keepalive N", "Upstream idle keepalive connections (default: 32; 0 disables)"},
			{"--note TEXT", "Free-text note shown in 'nd edge domain list'"},
			{"--disabled", "Save entry without serving it yet"},
			{"--replace", "Remove matching host from user/*.conf or unmanaged Caddyfile and adopt into domains.json"},
			{"--dry-run", "Print rendered engine config without modifying files"},
			{"--json", "Output JSON"},
		},
		Notes: []string{
			"Out of scope: per-path routing, rate limits, bot blocking, custom log formats, /media/ prefix stripping belong in user/ files.",
		},
		Examples: []string{
			"nd edge domain add shop.example.com 10.0.0.5:8080 --www",
			"nd edge domain add api.example.com https://10.0.0.6 --no-verify --max-body 200m",
		},
	},
	"set": {
		Summary: "Modify options of an existing manual domain and re-validate before applying.",
		Usage:   "nd edge domain set <domain> [--upstreams U] [--www] [--no-www] [--alias host] [--clear-aliases] [--redirect-www to-apex|to-www|none] [--tls auto|none|manual] [--cert F] [--key F] [--force-https] [--no-force-https] [--origin-http] [--origin-https] [--lb round_robin|least_conn|ip_hash] [--no-verify] [--verify] [--sni name] [--host-header MODE] [--websocket] [--no-websocket] [--max-body SIZE] [--timeout DUR] [--connect-timeout DUR] [--real-ip MODE] [--header-up K=V] [--clear-headers-up] [--header-down K=V] [--clear-headers-down] [--keepalive N] [--note TEXT] [--enabled] [--disabled] [--dry-run] [--json]",
		Flags: [][2]string{
			{"--upstreams U", "Replace upstream list (comma-separated)"},
			{"--www", "Serve www.<domain>"},
			{"--no-www", "Stop serving automatic www.<domain> alias"},
			{"--alias host", "Set alias hostnames (repeatable)"},
			{"--clear-aliases", "Remove all aliases"},
			{"--redirect-www", "Set 308 redirect (to-apex, to-www, or none)"},
			{"--tls", "Change TLS mode (auto, none, manual)"},
			{"--cert F", "Certificate PEM for --tls manual"},
			{"--key F", "Private key PEM for --tls manual"},
			{"--force-https", "Enable 80 -> 443 redirect"},
			{"--no-force-https", "Disable 80 -> 443 redirect"},
			{"--origin-http", "Use HTTP scheme to origin"},
			{"--origin-https", "Use HTTPS scheme to origin"},
			{"--lb", "Load-balancer policy (round_robin, least_conn, ip_hash)"},
			{"--no-verify", "Disable origin TLS certificate verification"},
			{"--verify", "Enable origin TLS certificate verification"},
			{"--sni name", "Override origin TLS SNI"},
			{"--host-header", "Set origin Host header (original, upstream, or literal)"},
			{"--websocket", "Enable WebSocket upgrade headers"},
			{"--no-websocket", "Disable WebSocket upgrade headers"},
			{"--max-body SIZE", "Client body size limit (e.g. 100m, 0)"},
			{"--timeout DUR", "Origin read/send timeout"},
			{"--connect-timeout DUR", "Origin dial timeout"},
			{"--real-ip", "Visitor IP mode (cloudflare, none, auto)"},
			{"--header-up K=V", "Append upstream request header"},
			{"--clear-headers-up", "Remove existing --header-up rules"},
			{"--header-down K=V", "Append client response header"},
			{"--clear-headers-down", "Remove existing --header-down rules"},
			{"--keepalive N", "Upstream keepalive pool size"},
			{"--note TEXT", "Update operator note"},
			{"--enabled", "Enable domain"},
			{"--disabled", "Disable domain without deleting it"},
			{"--dry-run", "Print rendered engine config without modifying files"},
			{"--json", "Output JSON"},
		},
		Examples: []string{"nd edge domain set shop.example.com --max-body 200m --real-ip cloudflare"},
	},
	"rm": {
		Summary: "Remove manual domains (to 30-day trash) or hand-written user/*.conf / Caddyfile sites (validated + reloaded).",
		Usage:   "nd edge domain rm <domain>... [--yes] [--json]",
		Flags: [][2]string{
			{"--yes", "Skip interactive confirmation prompt"},
			{"--json", "Output JSON"},
		},
		Examples: []string{"nd edge domain rm old.example.com --yes"},
	},
	"list": {
		Summary: "List manual domains and user/*.conf / unmanaged Caddyfile sites (include panel domains with --all).",
		Usage:   "nd edge domain list [--all] [--json]",
		Flags: [][2]string{
			{"--all", "Also list panel-owned domains (read-only)"},
			{"--json", "Output JSON"},
		},
		Examples: []string{"nd edge domain list --all"},
	},
	"show": {
		Summary: "Inspect a manual or user-config domain's configuration or rendered/raw proxy snippet.",
		Usage:   "nd edge domain show <domain> [--config] [--json]",
		Flags: [][2]string{
			{"--config", "Print rendered or user-config snippet for the active proxy engine"},
			{"--json", "Output JSON"},
		},
		Examples: []string{"nd edge domain show shop.example.com --config"},
	},
	"enable": {
		Summary:  "Enable a disabled manual domain and apply proxy config.",
		Usage:    "nd edge domain enable <domain> [--json]",
		Flags:    [][2]string{{"--json", "Output JSON"}},
		Examples: []string{"nd edge domain enable shop.example.com"},
	},
	"disable": {
		Summary:  "Stop serving a manual domain while keeping its definition in domains.json.",
		Usage:    "nd edge domain disable <domain> [--json]",
		Flags:    [][2]string{{"--json", "Output JSON"}},
		Examples: []string{"nd edge domain disable shop.example.com"},
	},
	"check": {
		Summary: "Read-only health check across DNS, origin connectivity, and served TLS certificates.",
		Usage:   "nd edge domain check [<domain>...] [--all] [--json]",
		Flags: [][2]string{
			{"--all", "Also inspect panel-owned domains (read-only)"},
			{"--json", "Output JSON report"},
		},
		Examples: []string{"nd edge domain check", "nd edge domain check --all --json"},
	},
	"prune": {
		Summary: "Find and safely trash garbage manual domains whose DNS or origin has failed persistently past --grace (dry-run by default).",
		Usage:   "nd edge domain prune [--dry-run] [--apply] [--reason dns|origin|all] [--grace 7d] [--all] [--yes] [--json] [--schedule daily|off] [--auto-apply]",
		Flags: [][2]string{
			{"--dry-run", "Show candidates only without deleting anything (default)"},
			{"--apply", "Move qualifying manual domains to trash (restorable 30d)"},
			{"--reason", "Filter condition: dns (nxdomain/no_records/elsewhere, default), origin, or all"},
			{"--grace", "Required persistence duration before pruning (default: 7d, min 2 checks)"},
			{"--all", "Also report failing panel domains with reassignment hints (never modified)"},
			{"--yes", "Skip interactive confirmation prompt when --apply is set"},
			{"--json", "Output JSON report"},
			{"--schedule", "Install or remove a daily systemd timer (daily or off; dry-run unless --auto-apply)"},
			{"--auto-apply", "Pass --apply --yes inside the scheduled systemd unit"},
		},
		Notes: []string{
			"Cloudflare-proxied domains (ok_proxied) and panel domains are never pruned.",
		},
		Examples: []string{
			"nd edge domain prune",
			"nd edge domain prune --apply --grace 7d",
		},
	},
	"restore": {
		Summary: "Restore a removed or pruned manual domain from the 30-day trash.",
		Usage:   "nd edge domain restore <domain> [--list] [--json]",
		Flags: [][2]string{
			{"--list", "List entries currently in trash"},
			{"--json", "Output JSON"},
		},
		Examples: []string{"nd edge domain restore --list", "nd edge domain restore shop.example.com"},
	},
	"export": {
		Summary: "Export all manual domains as JSON.",
		Usage:   "nd edge domain export [--file F]",
		Flags: [][2]string{
			{"--file F", "Write JSON to file F instead of stdout"},
		},
		Examples: []string{"nd edge domain export --file manual-backup.json"},
	},
	"import": {
		Summary: "Import manual domains from an exported JSON file with full validation.",
		Usage:   "nd edge domain import --file F [--replace] [--dry-run] [--yes] [--json]",
		Flags: [][2]string{
			{"--file F", "Input JSON file exported by 'nd edge domain export'"},
			{"--replace", "Replace existing manual domains instead of merging"},
			{"--dry-run", "Validate and preview changes without applying"},
			{"--yes", "Skip confirmation prompt when --replace is set"},
			{"--json", "Output JSON"},
		},
		Examples: []string{"nd edge domain import --file manual-backup.json --dry-run"},
	},
}

// printEdgeCommandHelp prints `nd edge <name> --help`.
func printEdgeCommandHelp(w io.Writer, name string) {
	if name == "domain" {
		doc := edgeCommandDocs["domain"]
		fmt.Fprintf(w, "%s\n\nUsage: %s\n", doc.Summary, doc.Usage)
		fmt.Fprintln(w, "\nSubcommands:")
		for _, sub := range edgeDomainOrder {
			fmt.Fprintf(w, "  %-10s %s\n", sub, edgeDomainDocs[sub].Summary)
		}
		fmt.Fprintln(w, "\nNotes:")
		for _, n := range doc.Notes {
			fmt.Fprintf(w, "  %s\n", n)
		}
		fmt.Fprintln(w, "\nExamples:")
		for _, e := range doc.Examples {
			fmt.Fprintf(w, "  %s\n", e)
		}
		return
	}
	var doc edgeCommandDoc
	var ok bool
	if strings.HasPrefix(name, "domain ") {
		sub := strings.TrimPrefix(name, "domain ")
		doc, ok = edgeDomainDocs[sub]
	} else {
		doc, ok = edgeCommandDocs[name]
	}
	if !ok {
		fmt.Fprintf(w, "Usage: nd edge %s\n", name)
		return
	}
	fmt.Fprintf(w, "%s\n\nUsage: %s\n", doc.Summary, doc.Usage)
	if len(doc.Flags) > 0 {
		width := 0
		for _, f := range doc.Flags {
			if len(f[0]) > width {
				width = len(f[0])
			}
		}
		fmt.Fprintln(w, "\nFlags:")
		for _, f := range doc.Flags {
			fmt.Fprintf(w, "  %-*s  %s\n", width, f[0], f[1])
		}
	}
	if len(doc.Notes) > 0 {
		fmt.Fprintln(w, "\nNotes:")
		for _, n := range doc.Notes {
			fmt.Fprintf(w, "  %s\n", n)
		}
	}
	if len(doc.Examples) > 0 {
		fmt.Fprintln(w, "\nExamples:")
		for _, e := range doc.Examples {
			fmt.Fprintf(w, "  %s\n", e)
		}
	}
}

// edgeUsageLine returns the one-line usage of a subcommand (used in error messages).
func edgeUsageLine(name string) string {
	return "Usage: " + strings.TrimSpace(edgeCommandDocs[name].Usage)
}
