package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"

	"nd/internal/client"
	"nd/internal/config"
)

// RunLogin handles: nd login <server_url>
// Prompts for API token (or accepts as argument), validates, saves to ~/.nd/config.json
func RunLogin(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: nd login <server_url> [token]")
	}
	serverURL := strings.TrimRight(args[0], "/")

	var token string
	if len(args) >= 2 {
		token = strings.TrimSpace(args[1])
		if token == "--token" && len(args) >= 3 {
			token = strings.TrimSpace(args[2])
		}
	}

	if token == "" {
		fmt.Print("API Token (generate in panel under Developer & AI -> CLI Sessions): ")
		if _, err := fmt.Scanln(&token); err != nil {
			return fmt.Errorf("failed to read token: %w", err)
		}
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
		return fmt.Errorf("could not reach server: %w", err)
	}
	if status == 401 {
		return fmt.Errorf("invalid token: authentication failed")
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
// Usage: nd logs [app_id] [-n lines]
func RunLogs(cl *client.Client, args []string) error {
	lines := 100
	var cleanArgs []string

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if (arg == "-n" || arg == "--tail" || arg == "--lines") && i+1 < len(args) {
			if n, err := strconv.Atoi(args[i+1]); err == nil && n > 0 {
				lines = n
			}
			i++
		} else if n, err := strconv.Atoi(arg); err == nil && n > 0 && len(cleanArgs) > 0 {
			lines = n
		} else {
			cleanArgs = append(cleanArgs, arg)
		}
	}

	appID, _, err := resolveAppID(cleanArgs)
	if err != nil {
		return err
	}

	body, status, err := cl.Do("POST", "/mcp", map[string]interface{}{
		"method": "tools/call",
		"params": map[string]interface{}{
			"name":      "container_logs",
			"arguments": map[string]interface{}{"app_id": appID, "lines": lines},
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

// RunExec executes a command inside an application container (Heroku-style: nd run / nd exec)
// Usage:
//   nd exec [flags] [app_id] <command...>
//   nd run [flags] [app_id] <command...>
//
// Flags:
//   -a, --app <app_id>       Target application ID (optional if linked)
//   -s, --service <service>  Target compose service name (optional)
//   -w, --workdir <dir>      Working directory inside container
//   --server                 Run directly on the host VPS (requires allow_server_exec)
func RunExec(cl *client.Client, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: nd exec [flags] [app_id] <command...>\nexample: nd exec myapp ls -la\n         nd exec -a myapp python manage.py migrate")
	}

	var (
		appID     string
		service   string
		workDir   string
		isServer  bool
		cmdTokens []string
	)

	// Check if local project is linked
	linkedAppID := ""
	if pc, err := config.LoadProject("."); err == nil && pc.AppID != "" {
		linkedAppID = pc.AppID
	}

	i := 0
	for i < len(args) {
		arg := args[i]
		if arg == "--server" {
			isServer = true
			i++
		} else if (arg == "-a" || arg == "--app") && i+1 < len(args) {
			appID = args[i+1]
			i += 2
		} else if (arg == "-s" || arg == "--service") && i+1 < len(args) {
			service = args[i+1]
			i += 2
		} else if (arg == "-w" || arg == "--workdir") && i+1 < len(args) {
			workDir = args[i+1]
			i += 2
		} else if arg == "--" {
			i++
			cmdTokens = append(cmdTokens, args[i:]...)
			break
		} else {
			cmdTokens = append(cmdTokens, args[i:]...)
			break
		}
	}

	if isServer {
		if len(cmdTokens) == 0 {
			return fmt.Errorf("command required for server execution")
		}
		return RunServerExec(cl, cmdTokens)
	}

	if len(cmdTokens) == 0 {
		return fmt.Errorf("command required: nd exec [app_id] <command...>")
	}

	// If appID not provided via -a, resolve from cmdTokens or linked app
	if appID == "" {
		if linkedAppID != "" {
			if cmdTokens[0] == linkedAppID {
				appID = linkedAppID
				cmdTokens = cmdTokens[1:]
			} else if len(cmdTokens) == 1 {
				appID = linkedAppID
			} else {
				first := strings.ToLower(cmdTokens[0])
				if isCommonCommand(first) {
					appID = linkedAppID
				} else {
					appID = cmdTokens[0]
					cmdTokens = cmdTokens[1:]
				}
			}
		} else {
			if len(cmdTokens) < 2 {
				return fmt.Errorf("app_id and command required: nd exec <app_id> <command...>")
			}
			appID = cmdTokens[0]
			cmdTokens = cmdTokens[1:]
		}
	}

	if len(cmdTokens) == 0 {
		return fmt.Errorf("command required to execute inside container")
	}

	cmdStr := strings.Join(cmdTokens, " ")

	mcpArgs := map[string]interface{}{
		"app_id":  appID,
		"command": cmdStr,
	}
	if service != "" {
		mcpArgs["service"] = service
	}
	if workDir != "" {
		mcpArgs["work_dir"] = workDir
	}

	body, status, err := cl.Do("POST", "/mcp", map[string]interface{}{
		"method": "tools/call",
		"params": map[string]interface{}{
			"name":      "container_exec",
			"arguments": mcpArgs,
		},
	})
	if err != nil {
		return fmt.Errorf("failed to execute: %w", err)
	}
	if status >= 400 {
		return fmt.Errorf("server error (%d): %s", status, client.JSONError(body))
	}

	return parseAndPrintExecResult(body)
}

// RunServerExec executes a command directly on the panel host server (requires allow_server_exec).
// Usage: nd server-exec <command...>
func RunServerExec(cl *client.Client, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: nd server-exec <command...>")
	}
	cmdStr := strings.Join(args, " ")
	body, status, err := cl.Do("POST", "/mcp", map[string]interface{}{
		"method": "tools/call",
		"params": map[string]interface{}{
			"name": "server_exec",
			"arguments": map[string]interface{}{
				"command": cmdStr,
			},
		},
	})
	if err != nil {
		return fmt.Errorf("failed to call server_exec: %w", err)
	}
	if status >= 400 {
		return fmt.Errorf("server error (%d): %s", status, client.JSONError(body))
	}
	return parseAndPrintExecResult(body)
}

func isCommonCommand(cmd string) bool {
	switch cmd {
	case "ls", "ps", "cat", "sh", "bash", "zsh", "python", "python3", "node", "npm", "npx",
		"yarn", "pnpm", "pip", "pip3", "go", "php", "artisan", "composer", "rails", "rake",
		"bundle", "django-admin", "echo", "env", "grep", "find", "mkdir", "rm", "touch",
		"cp", "mv", "tail", "head", "curl", "wget", "git", "docker", "tar", "gzip", "whoami",
		"pwd", "export", "sleep", "which", "chmod", "chown", "test":
		return true
	}
	return false
}

func parseAndPrintExecResult(body []byte) error {
	var resp struct {
		Result struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
		} `json:"result"`
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.Unmarshal(body, &resp); err != nil {
		fmt.Println(string(body))
		return nil
	}

	if resp.Error != nil {
		return fmt.Errorf("MCP error %d: %s", resp.Error.Code, resp.Error.Message)
	}

	if resp.Result.IsError || len(resp.Result.Content) == 0 {
		if len(resp.Result.Content) > 0 {
			return fmt.Errorf("%s", resp.Result.Content[0].Text)
		}
		return fmt.Errorf("command execution failed")
	}

	rawText := resp.Result.Content[0].Text

	var res struct {
		AppID     string `json:"app_id"`
		Container string `json:"container"`
		Service   string `json:"service"`
		Command   string `json:"command"`
		OK        bool   `json:"ok"`
		ExitCode  int    `json:"exit_code"`
		Output    string `json:"output"`
	}

	if err := json.Unmarshal([]byte(rawText), &res); err != nil {
		fmt.Print(rawText)
		if !strings.HasSuffix(rawText, "\n") {
			fmt.Println()
		}
		return nil
	}

	if res.Output != "" {
		fmt.Print(res.Output)
		if !strings.HasSuffix(res.Output, "\n") {
			fmt.Println()
		}
	}

	if !res.OK || res.ExitCode != 0 {
		if res.ExitCode != 0 {
			return fmt.Errorf("exit status %d", res.ExitCode)
		}
		return fmt.Errorf("command failed")
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
  nd login <server_url> [token]      Authenticate with an API token
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
  nd logs [app_id] [-n lines]        Show recent container logs (default: 100 lines)

  nd exec [flags] [app_id] <cmd...>  Run command inside app container (alias: nd run)
  nd server-exec <cmd...>            Run command directly on host VPS (requires allow_server_exec)

  nd env list [app_id]               Show environment variables
  nd env set [app_id] KEY=VALUE ...  Set environment variables

  nd version                         Print version
  nd help                            Print this help

Exec Flags:
  -a, --app <app_id>                 Target application ID
  -s, --service <service>            Target compose service
  -w, --workdir <dir>                Working directory inside container
  --server                           Execute on host VPS instead of container

Environment variables:
  ND_SERVER_URL                      Panel URL fallback (e.g. in CI/CD)
  ND_TOKEN                           API token fallback
`)
}
