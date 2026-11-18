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
			"Hand-written sites are not converted: they live in your Caddyfile or in /etc/angie/nd-edge/user/ and have to be ported.",
			"Expect a short interruption while ports 80/443 move from one proxy to the other.",
		},
		Examples: []string{"nd edge switch-proxy angie --yes"},
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
	"install", "switch-proxy", "add-panel", "remove-panel", "list", "status", "sync",
	"run", "doctor", "set-token", "set-url", "upgrade", "uninstall",
}

// printEdgeCommandHelp prints `nd edge <name> --help`.
func printEdgeCommandHelp(w io.Writer, name string) {
	doc, ok := edgeCommandDocs[name]
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
