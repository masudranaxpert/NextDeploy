package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strings"

	"nd/internal/client"
	"nd/internal/config"
)

// RunLogin handles: nd login <server_url>
// Prompts for API token, validates, saves to ~/.nd/config.json
func RunLogin(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: nd login <server_url>")
	}
	serverURL := strings.TrimRight(args[0], "/")

	fmt.Print("API Token (from Settings → Tokens): ")
	var token string
	if _, err := fmt.Scanln(&token); err != nil {
		return fmt.Errorf("failed to read token: %w", err)
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return fmt.Errorf("token cannot be empty")
	}

	// Validate token by listing apps
	cfg := config.Config{ServerURL: serverURL, Token: token}
	c := client.New(cfg, "")
	body, status, err := c.Do("GET", "/api/v1/apps", nil)
	if err != nil {
		// Try MCP app_list as fallback — older servers may not have REST /api/v1/apps
		body, status, err = c.Do("POST", "/mcp", map[string]interface{}{
			"method": "tools/call",
			"params": map[string]interface{}{"name": "app_list", "arguments": map[string]interface{}{}},
		})
	}
	if err != nil {
		return fmt.Errorf("could not reach server: %w", err)
	}
	if status == 401 {
		return fmt.Errorf("invalid token")
	}
	if status >= 400 {
		return fmt.Errorf("server error %d: %s", status, client.JSONError(body))
	}

	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("could not save config: %w", err)
	}
	fmt.Printf("✓ Logged in to %s\n", serverURL)
	return nil
}

// RunLogout clears the saved config.
func RunLogout(_ []string) error {
	cfg, err := config.Load()
	if err == nil && cfg.ServerURL != "" {
		// best-effort disconnect
		c := client.New(cfg, "")
		c.Disconnect()
	}
	if err := config.Save(config.Config{}); err != nil {
		return err
	}
	fmt.Println("Logged out.")
	return nil
}

// RunApps prints all accessible apps.
func RunApps(cl *client.Client, _ []string) error {
	body, status, err := cl.Do("POST", "/mcp", map[string]interface{}{
		"method": "tools/call",
		"params": map[string]interface{}{"name": "app_list", "arguments": map[string]interface{}{}},
	})
	if err != nil {
		return err
	}
	if status >= 400 {
		return fmt.Errorf("server error: %s", client.JSONError(body))
	}

	// Parse MCP tool response
	var resp struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &resp); err != nil || len(resp.Result.Content) == 0 {
		fmt.Println(string(body))
		return nil
	}

	var apps []struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(resp.Result.Content[0].Text), &apps); err != nil {
		fmt.Println(resp.Result.Content[0].Text)
		return nil
	}

	fmt.Printf("%-30s %-20s %s\n", "ID", "NAME", "STATUS")
	fmt.Println(strings.Repeat("─", 60))
	for _, a := range apps {
		status := a.Status
		if status == "active" {
			status = "● " + status
		}
		fmt.Printf("%-30s %-20s %s\n", a.ID, a.Name, status)
	}
	if len(apps) == 0 {
		fmt.Println("No apps found.")
	}
	return nil
}

// resolveAppID extracts appID from args or falls back to locally linked project (.nd/project.json).
func resolveAppID(args []string) (string, []string, error) {
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		return args[0], args[1:], nil
	}
	pc, err := config.LoadProject(".")
	if err == nil && pc.AppID != "" {
		return pc.AppID, args, nil
	}
	return "", args, fmt.Errorf("app_id required (specify as argument or link with: nd link <app_id>)")
}

// RunLink links the current directory to an app ID (.nd/project.json).
func RunLink(cl *client.Client, args []string) error {
	if len(args) < 1 {
		fmt.Println("Usage: nd link <app_id>\nAvailable applications:")
		return RunApps(cl, nil)
	}
	appID := args[0]
	if err := config.SaveProject(".", appID); err != nil {
		return fmt.Errorf("failed to link project: %w", err)
	}
	fmt.Printf("✓ Linked current directory to %s (.nd/project.json)\n", appID)
	return nil
}

// RunUnlink removes the project link in the current directory.
func RunUnlink(_ *client.Client, _ []string) error {
	if err := config.ClearProject("."); err != nil {
		return fmt.Errorf("failed to unlink: %w", err)
	}
	fmt.Println("✓ Unlinked project from current directory.")
	return nil
}

// RunWhoami prints currently configured server, device info, and token validity.
func RunWhoami(cl *client.Client, cfg config.Config) error {
	hostname, _ := os.Hostname()
	maskedToken := cfg.Token
	if len(maskedToken) > 8 {
		maskedToken = maskedToken[:4] + "..." + maskedToken[len(maskedToken)-4:]
	}
	fmt.Printf("Server URL:  %s\n", cfg.ServerURL)
	fmt.Printf("Device:      %s (%s/%s)\n", hostname, runtime.GOOS, runtime.GOARCH)
	fmt.Printf("Token:       %s\n", maskedToken)

	// Check if local project is linked
	if pc, err := config.LoadProject("."); err == nil && pc.AppID != "" {
		fmt.Printf("Linked App:  %s\n", pc.AppID)
	}

	body, status, err := cl.Do("POST", "/mcp", map[string]interface{}{
		"method": "tools/call",
		"params": map[string]interface{}{"name": "app_list", "arguments": map[string]interface{}{}},
	})
	if err != nil || status >= 400 {
		fmt.Printf("Status:      Error connecting (%s)\n", client.JSONError(body))
		return err
	}
	fmt.Println("Status:      Authenticated ✓")
	return nil
}

// RunStatus prints detailed info for an app.
func RunStatus(cl *client.Client, args []string) error {
	appID, _, err := resolveAppID(args)
	if err != nil {
		return err
	}

	body, status, err := cl.Do("POST", "/mcp", map[string]interface{}{
		"method": "tools/call",
		"params": map[string]interface{}{
			"name": "app_get",
			"arguments": map[string]interface{}{
				"app_id":         appID,
				"include_health": true,
			},
		},
	})
	if err != nil {
		return err
	}
	if status >= 400 {
		return fmt.Errorf("server error: %s", client.JSONError(body))
	}
	fmt.Println(string(body))
	return nil
}

// RunDeploy triggers a redeploy for an app.
func RunDeploy(cl *client.Client, args []string) error {
	appID, _, err := resolveAppID(args)
	if err != nil {
		return err
	}
	fmt.Printf("Deploying %s...\n", appID)

	body, status, err := cl.Do("POST", "/mcp", map[string]interface{}{
		"method": "tools/call",
		"params": map[string]interface{}{
			"name":      "deploy",
			"arguments": map[string]interface{}{"app_id": appID, "wait_seconds": 120},
		},
	})
	if err != nil {
		return err
	}
	if status >= 400 {
		return fmt.Errorf("deploy error: %s", client.JSONError(body))
	}
	fmt.Println("✓ Deploy triggered.")
	fmt.Println(string(body))
	return nil
}

// RunLogs streams recent logs for an app.
func RunLogs(cl *client.Client, args []string) error {
	appID, _, err := resolveAppID(args)
	if err != nil {
		return err
	}

	body, status, err := cl.Do("POST", "/mcp", map[string]interface{}{
		"method": "tools/call",
		"params": map[string]interface{}{
			"name":      "container_logs",
			"arguments": map[string]interface{}{"app_id": appID, "lines": 100},
		},
	})
	if err != nil {
		return err
	}
	if status >= 400 {
		return fmt.Errorf("server error: %s", client.JSONError(body))
	}

	var resp struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &resp); err == nil && len(resp.Result.Content) > 0 {
		fmt.Println(resp.Result.Content[0].Text)
	} else {
		fmt.Println(string(body))
	}
	return nil
}

// RunEnv handles: nd env list [app_id] | nd env set [app_id] KEY=VALUE ...
func RunEnv(cl *client.Client, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: nd env list [app_id] | nd env set [app_id] KEY=VALUE ...")
	}
	sub := args[0]
	rest := args[1:]

	appID, envArgs, err := resolveAppID(rest)
	if err != nil {
		return err
	}

	switch sub {
	case "list":
		body, status, err := cl.Do("POST", "/mcp", map[string]interface{}{
			"method": "tools/call",
			"params": map[string]interface{}{
				"name":      "env_list",
				"arguments": map[string]interface{}{"app_id": appID},
			},
		})
		if err != nil {
			return err
		}
		if status >= 400 {
			return fmt.Errorf("server error: %s", client.JSONError(body))
		}
		fmt.Println(string(body))

	case "set":
		if len(envArgs) < 1 {
			return fmt.Errorf("usage: nd env set [app_id] KEY=VALUE [KEY2=VALUE2 ...]")
		}
		// Merge all KEY=VALUE pairs into one env block
		lines := make([]string, 0, len(envArgs))
		for _, kv := range envArgs {
			if !strings.Contains(kv, "=") {
				return fmt.Errorf("invalid env format %q, expected KEY=VALUE", kv)
			}
			lines = append(lines, kv)
		}
		body, status, err := cl.Do("POST", "/mcp", map[string]interface{}{
			"method": "tools/call",
			"params": map[string]interface{}{
				"name": "env_set",
				"arguments": map[string]interface{}{
					"app_id": appID,
					"env":    strings.Join(lines, "\n"),
					"merge":  true,
				},
			},
		})
		if err != nil {
			return err
		}
		if status >= 400 {
			return fmt.Errorf("server error: %s", client.JSONError(body))
		}
		fmt.Println("✓ Environment updated.")
		_ = body

	default:
		return fmt.Errorf("unknown env subcommand: %s (use list or set)", sub)
	}
	return nil
}

// RunStop stops an app.
func RunStop(cl *client.Client, args []string) error {
	appID, _, err := resolveAppID(args)
	if err != nil {
		return err
	}
	_, _, err = cl.Do("POST", "/mcp", map[string]interface{}{
		"method": "tools/call",
		"params": map[string]interface{}{
			"name":      "stop",
			"arguments": map[string]interface{}{"app_id": appID},
		},
	})
	if err != nil {
		return err
	}
	fmt.Printf("✓ %s stopped.\n", appID)
	return nil
}

// RunRestart restarts an app.
func RunRestart(cl *client.Client, args []string) error {
	appID, _, err := resolveAppID(args)
	if err != nil {
		return err
	}
	_, _, err = cl.Do("POST", "/mcp", map[string]interface{}{
		"method": "tools/call",
		"params": map[string]interface{}{
			"name":      "restart",
			"arguments": map[string]interface{}{"app_id": appID},
		},
	})
	if err != nil {
		return err
	}
	fmt.Printf("✓ %s restarted.\n", appID)
	return nil
}

// PrintHelp prints the CLI usage.
func PrintHelp() {
	fmt.Fprintf(os.Stderr, `nd — NextDeploy CLI

Usage:
  nd login <server_url>              Authenticate with an API token
  nd logout                          Remove saved credentials
  nd whoami                          Show authenticated server and session status

  nd apps                            List all applications
  nd link <app_id>                   Link current directory to an app (.nd/project.json)
  nd unlink                          Remove link from current directory
  nd status [app_id]                 Show app status and containers
  nd push [app_id] [dir]             Sync local files and deploy
  nd deploy [app_id]                 Redeploy without file sync
  nd stop [app_id]                   Stop an app
  nd restart [app_id]                Restart an app
  nd logs [app_id]                   Show recent container logs

  nd env list [app_id]               Show environment variables
  nd env set [app_id] KEY=VALUE ...  Set environment variables

  nd version                         Print version
  nd help                            Print this help

Environment variables:
  ND_SERVER_URL                      Panel URL fallback (e.g. in CI/CD)
  ND_TOKEN                           API token fallback
`)
}
