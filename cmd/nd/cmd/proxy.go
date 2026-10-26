package cmd

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

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
	case "reload":
		return runProxyReload(cl, rest)
	case "logs":
		return runProxyLogs(cl, rest)
	default:
		return fmt.Errorf("unknown proxy command %q. Run 'nd proxy --help' for usage", sub)
	}
}

func printProxyHelp() {
	fmt.Println("Usage: nd proxy <command> [args...]")
	fmt.Println("\nManage Caddy reverse proxy routes and global configuration:")
	fmt.Println("  list, ls                                     List custom reverse proxy routes")
	fmt.Println("  add <domain> <upstream> [flags]              Add custom reverse proxy or redirect")
	fmt.Println("  rm <route_id>                                Delete a proxy route")
	fmt.Println("  reload                                       Reload Caddy configuration and routes")
	fmt.Println("  logs [-n lines]                              View live Caddy proxy server logs")
}

func runProxyList(cl *client.Client, args []string) error {
	data, status, err := cl.Do("GET", "/api/v1/proxy/routes", nil)
	if err != nil {
		return err
	}
	if status >= 400 {
		return fmt.Errorf("error: %s", client.JSONError(data))
	}

	var resp struct {
		Routes []struct {
			ID       int64  `json:"id"`
			Domain   string `json:"domain"`
			Target   string `json:"target"`
			Upstream string `json:"upstream"`
			Kind     string `json:"kind"`
			Type     string `json:"route_type"`
			Enabled  bool   `json:"enabled"`
		} `json:"routes"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return fmt.Errorf("parse error: %w", err)
	}

	if len(resp.Routes) == 0 {
		fmt.Println("No custom proxy routes configured. Add one with: nd proxy add <domain> <upstream>")
		return nil
	}

	fmt.Printf("%-6s %-28s %-32s %-12s %-8s\n", "ID", "DOMAIN", "TARGET / UPSTREAM", "TYPE", "STATUS")
	fmt.Println(strings.Repeat("─", 90))
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
		statusStr := "active"
		if !r.Enabled {
			statusStr = "disabled"
		}
		fmt.Printf("%-6d %-28s %-32s %-12s %-8s\n", r.ID, r.Domain, target, typeStr, statusStr)
	}
	return nil
}

func runProxyAdd(cl *client.Client, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: nd proxy add <domain> <upstream_url> [--type reverse_proxy|redirect] [--strip-path]")
	}

	domain := args[0]
	upstream := args[1]
	routeType := "proxy"
	stripPath := false

	for i := 2; i < len(args); i++ {
		a := args[i]
		if (a == "-t" || a == "--type") && i+1 < len(args) {
			routeType = args[i+1]
			i++
		} else if strings.HasPrefix(a, "--type=") {
			routeType = strings.TrimPrefix(a, "--type=")
		} else if a == "--strip-path" {
			stripPath = true
		}
	}

	if routeType == "reverse_proxy" {
		routeType = "proxy"
	}

	payload := map[string]interface{}{
		"domain":       domain,
		"target":       upstream,
		"upstream":     upstream,
		"kind":         routeType,
		"route_type":   routeType,
		"strip_path":   stripPath,
		"enabled":      true,
		"enable_https": true,
	}

	data, status, err := cl.Do("POST", "/api/v1/proxy/routes", payload)
	if err != nil {
		return err
	}
	if status >= 400 {
		return fmt.Errorf("failed to add route: %s", client.JSONError(data))
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

func runProxyLogs(cl *client.Client, args []string) error {
	lines := 100
	for i := 0; i < len(args); i++ {
		if (args[i] == "-n" || args[i] == "--lines" || args[i] == "--tail") && i+1 < len(args) {
			if n, err := strconv.Atoi(args[i+1]); err == nil && n > 0 {
				lines = n
			}
			i++
		}
	}

	data, status, err := cl.Do("GET", "/api/v1/proxy/logs?lines="+strconv.Itoa(lines), nil)
	if err != nil {
		return err
	}
	if status >= 400 {
		return fmt.Errorf("error: %s", client.JSONError(data))
	}
	var resp struct {
		Logs string `json:"logs"`
	}
	if err := json.Unmarshal(data, &resp); err == nil && resp.Logs != "" {
		fmt.Println(resp.Logs)
	} else {
		fmt.Println(string(data))
	}
	return nil
}
