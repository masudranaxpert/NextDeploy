package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"time"

	"nd/internal/client"
	"nd/internal/ui"
)

// RunProxy handles all Caddy proxy and custom route management under 'nd proxy'.
func RunProxy(cl *client.Client, args []string) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		printProxyHelp()
		return nil
	}

	sub := args[0]
	rest := args[1:]

	switch sub {
	case "list", "ls":
		return runProxyList(cl, rest)
	case "add", "create":
		return runProxyAdd(cl, rest)
	case "rm", "delete":
		return runProxyRemove(cl, rest)
	case "enable":
		return runProxySetEnabled(cl, rest, true)
	case "disable":
		return runProxySetEnabled(cl, rest, false)
	case "toggle":
		return runProxyToggle(cl, rest)
	case "reload":
		return runProxyReload(cl, rest)
	case "logs":
		return runProxyLogs(cl, rest)
	case "status":
		return runProxyStatus(cl, rest)
	case "options":
		return runProxyOptions(cl, rest)
	default:
		return fmt.Errorf("unknown proxy command %q. Run 'nd proxy --help' for usage", sub)
	}
}

func printProxyHelp() {
	fmt.Println("Usage: nd proxy <command> [args...]")
	fmt.Println("\nManage Caddy reverse proxy routes and global configuration:")
	fmt.Println("  list, ls [--json]                            List custom reverse proxy routes")
	fmt.Println("  add <domain> <upstream> [flags]              Add reverse proxy or redirect route")
	fmt.Println("      --type proxy|redirect                    Route kind (default: proxy)")
	fmt.Println("      --shield <id> | --direct                 Route via Shield node or direct origin")
	fmt.Println("      --www | --no-https | --disabled          Enable www alias / skip HTTPS / start disabled")
	fmt.Println("      --skip-tls-verify | --trusted-proxy      Upstream TLS skip / trust forwarded IP headers")
	fmt.Println("      --keep-path | --redirect-code <code>     Redirect path preservation / status (301/302/307/308)")
	fmt.Println("  enable <route_id>                            Enable a proxy route and reload Caddy")
	fmt.Println("  disable <route_id>                           Disable a proxy route and reload Caddy")
	fmt.Println("  toggle <route_id> [on|off]                   Toggle or explicitly set route state")
	fmt.Println("  rm <route_id>                                Delete a proxy route")
	fmt.Println("  reload                                       Reload Caddy configuration and routes")
	fmt.Println("  logs [-n lines] [-f|--follow] [--json]       View or stream Caddy proxy logs")
	fmt.Println("  status [--json]                              Show panel proxy status and live Caddy config")
	fmt.Println("  options [show|set [flags]] [--json]          View or update global Caddy timeouts & Real-IP")
}

func runProxyList(cl *client.Client, args []string) error {
	jsonOut, _ := extractJSONFlag(args)
	data, status, err := cl.Do("GET", "/api/v1/proxy/routes", nil)
	if err != nil {
		return err
	}
	if status >= 400 {
		return fmt.Errorf("error: %s", client.JSONError(data))
	}
	if jsonOut {
		fmt.Println(strings.TrimSpace(string(data)))
		return nil
	}

	var resp struct {
		Routes []struct {
			ID           int64  `json:"id"`
			Domain       string `json:"domain"`
			Target       string `json:"target"`
			Upstream     string `json:"upstream"`
			Kind         string `json:"kind"`
			Type         string `json:"route_type"`
			Enabled      bool   `json:"enabled"`
			EnableHTTPS  bool   `json:"enable_https"`
			ShieldNodeID *int64 `json:"shield_node_id"`
		} `json:"routes"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return fmt.Errorf("parse error: %w", err)
	}

	if len(resp.Routes) == 0 {
		fmt.Println("No custom proxy routes configured. Add one with: nd proxy add <domain> <upstream>")
		return nil
	}

	fmt.Printf("%-6s %-26s %-28s %-10s %-6s %-8s %-8s\n", "ID", "DOMAIN", "TARGET / UPSTREAM", "TYPE", "HTTPS", "EDGE", "STATUS")
	fmt.Println(strings.Repeat("─", 98))
	for _, r := range resp.Routes {
		target := r.Target
		if target == "" {
			target = r.Upstream
		}
		typeStr := r.Kind
		if typeStr == "" {
			typeStr = r.Type
		}
		if typeStr == "" {
			typeStr = "proxy"
		}
		httpsStr := "off"
		if r.EnableHTTPS {
			httpsStr = "on"
		}
		edgeStr := "direct"
		if r.ShieldNodeID != nil && *r.ShieldNodeID > 0 {
			edgeStr = fmt.Sprintf("shield#%d", *r.ShieldNodeID)
		}
		statusStr := "active"
		if !r.Enabled {
			statusStr = "disabled"
		}
		fmt.Printf("%-6d %-26s %-28s %-10s %-6s %-8s %-8s\n", r.ID, r.Domain, target, typeStr, httpsStr, edgeStr, statusStr)
	}
	return nil
}

func runProxyAdd(cl *client.Client, args []string) error {
	jsonOut, args := extractJSONFlag(args)
	if len(args) < 2 {
		return fmt.Errorf("usage: nd proxy add <domain> <upstream_url> [--type proxy|redirect] [--shield <id>|--direct] [--no-https] [--disabled] [--www] [--keep-path] [--skip-tls-verify] [--trusted-proxy]")
	}

	domain := args[0]
	upstream := args[1]
	routeType := "proxy"
	keepPath := false
	enableHTTPS := true
	enabled := true
	enableWWW := false
	skipTLSVerify := false
	trustedProxy := false
	redirectCode := 0
	note := ""
	var shieldID *int64
	direct := false

	for i := 2; i < len(args); i++ {
		a := args[i]
		switch {
		case (a == "-t" || a == "--type") && i+1 < len(args):
			routeType = args[i+1]
			i++
		case strings.HasPrefix(a, "--type="):
			routeType = strings.TrimPrefix(a, "--type=")
		case a == "--keep-path" || a == "--strip-path":
			keepPath = true
		case a == "--no-https":
			enableHTTPS = false
		case a == "--disabled":
			enabled = false
		case a == "--www":
			enableWWW = true
		case a == "--skip-tls-verify":
			skipTLSVerify = true
		case a == "--trusted-proxy":
			trustedProxy = true
		case a == "--redirect-code" && i+1 < len(args):
			code, err := strconv.Atoi(args[i+1])
			if err != nil {
				return fmt.Errorf("invalid --redirect-code %q", args[i+1])
			}
			redirectCode = code
			i++
		case a == "--note" && i+1 < len(args):
			note = args[i+1]
			i++
		case a == "--shield" && i+1 < len(args):
			id, err := strconv.ParseInt(strings.TrimSpace(args[i+1]), 10, 64)
			if err != nil || id <= 0 {
				return fmt.Errorf("invalid --shield node ID %q: must be a positive integer", args[i+1])
			}
			shieldID = &id
			i++
		case strings.HasPrefix(a, "--shield="):
			val := strings.TrimSpace(strings.TrimPrefix(a, "--shield="))
			id, err := strconv.ParseInt(val, 10, 64)
			if err != nil || id <= 0 {
				return fmt.Errorf("invalid --shield node ID %q: must be a positive integer", val)
			}
			shieldID = &id
		case a == "--direct":
			direct = true
		case strings.HasPrefix(a, "-"):
			return fmt.Errorf("unknown flag: %s", a)
		}
	}

	if shieldID != nil && direct {
		return fmt.Errorf("cannot specify both --shield and --direct")
	}

	if routeType == "reverse_proxy" {
		routeType = "proxy"
	}

	payload := map[string]interface{}{
		"domain":          domain,
		"target":          upstream,
		"upstream":        upstream,
		"kind":            routeType,
		"route_type":      routeType,
		"keep_path":       keepPath,
		"enabled":         enabled,
		"enable_https":    enableHTTPS,
		"enable_www":      enableWWW,
		"skip_tls_verify": skipTLSVerify,
		"trusted_proxy":   trustedProxy,
		"note":            note,
	}
	if redirectCode > 0 {
		payload["redirect_code"] = redirectCode
	}
	if shieldID != nil {
		payload["shield_node_id"] = *shieldID
	} else if direct {
		payload["shield_node_id"] = nil
	}

	data, status, err := cl.Do("POST", "/api/v1/proxy/routes", payload)
	if err != nil {
		return err
	}
	if status >= 400 {
		return fmt.Errorf("failed to add route: %s", client.JSONError(data))
	}
	if jsonOut {
		fmt.Println(strings.TrimSpace(string(data)))
		return nil
	}
	ui.Success("Added proxy route for %s -> %s", ui.Bold(domain), ui.Cyan(upstream))
	return nil
}

func runProxyRemove(cl *client.Client, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("route ID required: nd proxy rm <route_id>")
	}
	id := args[0]
	data, status, err := cl.Do("DELETE", "/api/v1/proxy/routes/"+id, nil)
	if err != nil {
		return err
	}
	if status >= 400 {
		return fmt.Errorf("failed to remove route: %s", client.JSONError(data))
	}
	ui.Success("Deleted proxy route #%s", id)
	return nil
}

func runProxySetEnabled(cl *client.Client, args []string, enabled bool) error {
	jsonOut, args := extractJSONFlag(args)
	if len(args) == 0 {
		action := "enable"
		if !enabled {
			action = "disable"
		}
		return fmt.Errorf("route ID required: nd proxy %s <route_id>", action)
	}
	id := strings.TrimSpace(args[0])
	data, status, err := cl.Do("POST", "/api/v1/proxy/routes/"+id+"/toggle", map[string]bool{"enabled": enabled})
	if err != nil {
		return err
	}
	if status >= 400 {
		return fmt.Errorf("failed to update route #%s: %s", id, client.JSONError(data))
	}
	if jsonOut {
		fmt.Println(strings.TrimSpace(string(data)))
		return nil
	}
	state := "enabled"
	if !enabled {
		state = "disabled"
	}
	ui.Success("Proxy route #%s %s", id, state)
	return nil
}

func runProxyToggle(cl *client.Client, args []string) error {
	jsonOut, clean := extractJSONFlag(args)
	if len(clean) == 0 {
		return fmt.Errorf("usage: nd proxy toggle <route_id> [on|off]")
	}
	id := strings.TrimSpace(clean[0])
	if len(clean) >= 2 {
		switch strings.ToLower(clean[1]) {
		case "on", "enable", "enabled", "true", "1":
			if jsonOut {
				return runProxySetEnabled(cl, []string{id, "--json"}, true)
			}
			return runProxySetEnabled(cl, []string{id}, true)
		case "off", "disable", "disabled", "false", "0":
			if jsonOut {
				return runProxySetEnabled(cl, []string{id, "--json"}, false)
			}
			return runProxySetEnabled(cl, []string{id}, false)
		default:
			return fmt.Errorf("invalid toggle value %q (use on or off)", clean[1])
		}
	}

	// Read current route state to flip it cleanly.
	data, status, err := cl.Do("GET", "/api/v1/proxy/routes", nil)
	if err != nil {
		return err
	}
	if status >= 400 {
		return fmt.Errorf("error listing routes: %s", client.JSONError(data))
	}
	var resp struct {
		Routes []struct {
			ID      int64 `json:"id"`
			Enabled bool  `json:"enabled"`
		} `json:"routes"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return fmt.Errorf("parse error: %w", err)
	}
	idNum, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid route ID %q", id)
	}
	for _, r := range resp.Routes {
		if r.ID == idNum {
			next := []string{id}
			if jsonOut {
				next = append(next, "--json")
			}
			return runProxySetEnabled(cl, next, !r.Enabled)
		}
	}
	return fmt.Errorf("route #%s not found", id)
}

func runProxyReload(cl *client.Client, args []string) error {
	fmt.Println("→ Reloading Caddy proxy routes...")
	data, status, err := cl.Do("POST", "/api/v1/proxy/reload", nil)
	if err != nil {
		return err
	}
	if status >= 400 {
		return fmt.Errorf("reload failed: %s", client.JSONError(data))
	}
	ui.Success("Caddy proxy configuration reloaded successfully")
	return nil
}

func fetchProxyLogLines(cl *client.Client, tail int) ([]byte, []string, error) {
	data, status, err := cl.Do("GET", "/api/v1/proxy/logs?tail="+strconv.Itoa(tail), nil)
	if err != nil {
		return nil, nil, err
	}
	if status >= 400 {
		return nil, nil, fmt.Errorf("error: %s", client.JSONError(data))
	}
	var resp struct {
		Lines []string `json:"lines"`
		Logs  string   `json:"logs"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return data, []string{string(data)}, nil
	}
	if len(resp.Lines) > 0 {
		return data, resp.Lines, nil
	}
	if resp.Logs != "" {
		return data, strings.Split(strings.TrimRight(resp.Logs, "\n"), "\n"), nil
	}
	return data, nil, nil
}

func runProxyLogs(cl *client.Client, args []string) error {
	jsonOut, clean := extractJSONFlag(args)
	lines := 100
	follow := false
	for i := 0; i < len(clean); i++ {
		arg := clean[i]
		if arg == "-f" || arg == "--follow" {
			follow = true
		} else if (arg == "-n" || arg == "--lines" || arg == "--tail") && i+1 < len(clean) {
			if n, err := strconv.Atoi(clean[i+1]); err == nil && n > 0 {
				lines = n
			}
			i++
		} else if n, err := strconv.Atoi(arg); err == nil && n > 0 {
			lines = n
		}
	}

	raw, logLines, err := fetchProxyLogLines(cl, lines)
	if err != nil {
		return err
	}
	if jsonOut {
		fmt.Println(strings.TrimSpace(string(raw)))
		return nil
	}
	for _, line := range logLines {
		fmt.Println(line)
	}
	if !follow {
		return nil
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	defer signal.Stop(sigCh)

	prev := append([]string(nil), logLines...)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-sigCh:
			return nil
		case <-ticker.C:
			_, cur, err := fetchProxyLogLines(cl, lines)
			if err != nil {
				continue
			}
			for _, line := range newLogSuffix(prev, cur) {
				fmt.Println(line)
			}
			prev = cur
		}
	}
}

func newLogSuffix(prev, cur []string) []string {
	if len(cur) == 0 {
		return nil
	}
	if len(prev) == 0 {
		return cur
	}
	maxOverlap := len(prev)
	if len(cur) < maxOverlap {
		maxOverlap = len(cur)
	}
	for k := maxOverlap; k > 0; k-- {
		match := true
		for i := 0; i < k; i++ {
			if prev[len(prev)-k+i] != cur[i] {
				match = false
				break
			}
		}
		if match {
			return cur[k:]
		}
	}
	return cur
}

func runProxyStatus(cl *client.Client, args []string) error {
	jsonOut, _ := extractJSONFlag(args)
	data, status, err := cl.Do("GET", "/api/v1/proxy/status", nil)
	if err != nil {
		return err
	}
	if status >= 400 {
		return fmt.Errorf("error: %s", client.JSONError(data))
	}
	if jsonOut {
		fmt.Println(strings.TrimSpace(string(data)))
		return nil
	}

	var resp struct {
		PanelDomain      string `json:"panel_domain"`
		PanelEnableHTTPS bool   `json:"panel_enable_https"`
		PanelEnableWWW   bool   `json:"panel_enable_www"`
		LiveConfig       string `json:"live_config"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return fmt.Errorf("parse error: %w", err)
	}
	domain := resp.PanelDomain
	if domain == "" {
		domain = "(unset)"
	}
	fmt.Printf("Panel Domain: %s  (HTTPS: %v, WWW: %v)\n", ui.Bold(domain), resp.PanelEnableHTTPS, resp.PanelEnableWWW)
	if strings.TrimSpace(resp.LiveConfig) != "" {
		fmt.Println("\nLive Caddy Config:")
		fmt.Println(strings.TrimSpace(resp.LiveConfig))
	}
	return nil
}

func runProxyOptions(cl *client.Client, args []string) error {
	jsonOut, clean := extractJSONFlag(args)
	sub := "show"
	if len(clean) > 0 && !strings.HasPrefix(clean[0], "-") {
		sub = strings.ToLower(clean[0])
		clean = clean[1:]
	}

	if sub == "show" || sub == "get" {
		data, status, err := cl.Do("GET", "/api/v1/proxy/options", nil)
		if err != nil {
			return err
		}
		if status >= 400 {
			return fmt.Errorf("error: %s", client.JSONError(data))
		}
		if jsonOut {
			fmt.Println(strings.TrimSpace(string(data)))
			return nil
		}
		var resp struct {
			ClientIP string `json:"client_ip"`
			Options  struct {
				Profile        string   `json:"profile"`
				ReadHeader     string   `json:"read_header"`
				ReadBodyIdle   string   `json:"read_body_idle"`
				Idle           string   `json:"idle"`
				GracePeriod    string   `json:"grace_period"`
				MaxHeaderSize  string   `json:"max_header_size"`
				RealIP         string   `json:"real_ip"`
				TrustedProxies []string `json:"trusted_proxies"`
				TrustedStrict  bool     `json:"trusted_strict"`
				ShieldProxies  []string `json:"shield_proxies"`
			} `json:"options"`
		}
		if err := json.Unmarshal(data, &resp); err != nil {
			return fmt.Errorf("parse error: %w", err)
		}
		o := resp.Options
		fmt.Printf("Profile:        %s\n", o.Profile)
		fmt.Printf("Real-IP Mode:   %s\n", o.RealIP)
		if len(o.TrustedProxies) > 0 {
			fmt.Printf("Trusted CIDRs:  %s\n", strings.Join(o.TrustedProxies, ", "))
		}
		if len(o.ShieldProxies) > 0 {
			fmt.Printf("Shield Proxies: %s\n", strings.Join(o.ShieldProxies, ", "))
		}
		fmt.Printf("Timeouts:       read_header=%s  read_body_idle=%s  idle=%s  grace=%s  max_header=%s\n",
			emptyDash(o.ReadHeader), emptyDash(o.ReadBodyIdle), emptyDash(o.Idle), emptyDash(o.GracePeriod), emptyDash(o.MaxHeaderSize))
		return nil
	}

	if sub != "set" {
		return fmt.Errorf("usage: nd proxy options [show | set --profile default|balanced|high|strict --real-ip none|cloudflare|custom --trusted-proxies cidrs --apply]")
	}

	// Load existing options so partial updates don't wipe other fields.
	curData, status, err := cl.Do("GET", "/api/v1/proxy/options", nil)
	if err != nil {
		return err
	}
	if status >= 400 {
		return fmt.Errorf("error loading current options: %s", client.JSONError(curData))
	}
	var curResp struct {
		Options map[string]interface{} `json:"options"`
	}
	if err := json.Unmarshal(curData, &curResp); err != nil || curResp.Options == nil {
		curResp.Options = map[string]interface{}{}
	}
	payload := curResp.Options
	payload["apply"] = true

	for i := 0; i < len(clean); i++ {
		switch clean[i] {
		case "--profile":
			if i+1 >= len(clean) {
				return fmt.Errorf("--profile requires a value")
			}
			payload["profile"] = clean[i+1]
			i++
		case "--real-ip":
			if i+1 >= len(clean) {
				return fmt.Errorf("--real-ip requires none|cloudflare|custom")
			}
			payload["real_ip"] = clean[i+1]
			i++
		case "--trusted-proxies":
			if i+1 >= len(clean) {
				return fmt.Errorf("--trusted-proxies requires comma-separated CIDRs")
			}
			var cidrs []string
			for _, part := range strings.Split(clean[i+1], ",") {
				if p := strings.TrimSpace(part); p != "" {
					cidrs = append(cidrs, p)
				}
			}
			payload["trusted_proxies"] = cidrs
			i++
		case "--read-header":
			if i+1 >= len(clean) {
				return fmt.Errorf("--read-header requires a duration")
			}
			payload["read_header"] = clean[i+1]
			i++
		case "--idle":
			if i+1 >= len(clean) {
				return fmt.Errorf("--idle requires a duration")
			}
			payload["idle"] = clean[i+1]
			i++
		case "--grace-period":
			if i+1 >= len(clean) {
				return fmt.Errorf("--grace-period requires a duration")
			}
			payload["grace_period"] = clean[i+1]
			i++
		case "--max-header-size":
			if i+1 >= len(clean) {
				return fmt.Errorf("--max-header-size requires a size (e.g. 16KB)")
			}
			payload["max_header_size"] = clean[i+1]
			i++
		case "--no-apply":
			payload["apply"] = false
		default:
			return fmt.Errorf("unknown options flag: %s", clean[i])
		}
	}

	data, status, err := cl.Do("POST", "/api/v1/proxy/options", payload)
	if err != nil {
		return err
	}
	if status >= 400 {
		return fmt.Errorf("failed to save options: %s", client.JSONError(data))
	}
	if jsonOut {
		fmt.Println(strings.TrimSpace(string(data)))
		return nil
	}
	ui.Success("Saved global Caddy proxy options")
	return nil
}

func emptyDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}
