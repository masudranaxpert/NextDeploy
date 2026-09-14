package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"nd/internal/client"
	"nd/internal/config"
)

// Version can be overwritten at build time or by main.
var Version = "1.0.6"

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
	cfg.EnsureDeviceID()
	c := client.New(cfg, cfg.DeviceID)
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

	// Register device heartbeat immediately on login
	hostname, _ := os.Hostname()
	_ = c.HeartbeatSync(hostname, runtime.GOOS, runtime.GOARCH, Version)

	fmt.Printf("✓ Logged in to %s\n", serverURL)
	return nil
}

// RunLogout clears the saved config and unregisters the device session.
func RunLogout(_ []string) error {
	cfg, err := config.Load()
	if err == nil && cfg.ServerURL != "" && cfg.DeviceID != "" {
		c := client.New(cfg, cfg.DeviceID)
		c.Disconnect()
	}
	if err := config.Save(config.Config{}); err != nil {
		return err
	}
	fmt.Println("✓ Logged out successfully.")
	return nil
}

// RunApps prints all accessible apps, or dispatches subcommands.
func RunApps(cl *client.Client, args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "create", "new":
			return RunCreate(cl, args[1:])
		case "delete", "destroy", "rm":
			return RunDelete(cl, args[1:])
		case "info", "status":
			return RunStatus(cl, args[1:])
		case "ps":
			return RunPS(cl, args[1:])
		}
	}

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
		fmt.Println("No apps found. Create one with: nd create <name>")
	}
	return nil
}

// RunCreate provisions a new application on NextDeploy.
func RunCreate(cl *client.Client, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: nd create <app_name> [--no-link]")
	}
	appName := args[0]
	autoLink := true
	for _, a := range args[1:] {
		if a == "--no-link" {
			autoLink = false
		}
	}

	payload := map[string]interface{}{
		"name": appName,
	}

	// If a local docker-compose.yml exists, include it as initial configuration
	if composeBytes, err := os.ReadFile("docker-compose.yml"); err == nil && len(composeBytes) > 0 {
		payload["compose_content"] = string(composeBytes)
	}

	body, status, err := cl.Do("POST", "/mcp", map[string]interface{}{
		"method": "tools/call",
		"params": map[string]interface{}{
			"name":      "app_create",
			"arguments": payload,
		},
	})
	if err != nil {
		return err
	}
	if status >= 400 {
		return fmt.Errorf("failed to create app: %s", client.JSONError(body))
	}

	var resp struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &resp); err != nil || len(resp.Result.Content) == 0 {
		return fmt.Errorf("unexpected server response: %s", string(body))
	}
	if resp.Result.IsError {
		return fmt.Errorf("failed: %s", resp.Result.Content[0].Text)
	}

	var resData struct {
		AppID  string `json:"app_id"`
		Name   string `json:"name"`
		Status string `json:"status"`
	}
	_ = json.Unmarshal([]byte(resp.Result.Content[0].Text), &resData)

	appID := resData.AppID
	if appID == "" {
		appID = appName
	}

	fmt.Printf("✓ Created application %s (ID: %s)\n", resData.Name, appID)
	if autoLink {
		if err := config.SaveProject(".", appID); err == nil {
			fmt.Printf("✓ Linked current directory to %s (.nd/project.json)\n", appID)
		}
	}
	fmt.Printf("\nNext steps:\n  nd push         # Upload code and build/deploy\n  nd logs         # View live container logs\n  nd status       # Check app health and URLs\n")
	return nil
}

// RunDelete deletes an application after confirmation.
func RunDelete(cl *client.Client, args []string) error {
	appID, rest, err := resolveAppID(args)
	if err != nil {
		return err
	}
	force := false
	for _, a := range rest {
		if a == "--force" || a == "-f" || a == "-y" {
			force = true
		}
	}
	if !force {
		fmt.Printf("WARNING: Deleting %q will remove its containers, volumes, and workspace files.\n", appID)
		fmt.Printf("Type the app ID %q to confirm deletion: ", appID)
		var confirm string
		_, _ = fmt.Scanln(&confirm)
		if strings.TrimSpace(confirm) != appID {
			return fmt.Errorf("deletion aborted: confirmation mismatch")
		}
	}

	// Try REST DELETE /api/v1/apps/{id} first, fallback to MCP app_delete
	body, status, err := cl.Do("DELETE", fmt.Sprintf("/api/v1/apps/%s", appID), nil)
	if err != nil || status == 404 || status == 405 {
		body, status, err = cl.Do("POST", "/mcp", map[string]interface{}{
			"method": "tools/call",
			"params": map[string]interface{}{
				"name":      "app_delete",
				"arguments": map[string]interface{}{"app_id": appID},
			},
		})
	}
	if err != nil {
		return err
	}
	if status >= 400 {
		return fmt.Errorf("failed to delete app: %s", client.JSONError(body))
	}

	var mcpResp struct {
		Result struct {
			IsError bool `json:"isError"`
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &mcpResp); err == nil && mcpResp.Result.IsError && len(mcpResp.Result.Content) > 0 {
		return fmt.Errorf("failed to delete app: %s", mcpResp.Result.Content[0].Text)
	}

	_ = config.ClearProject(".")
	fmt.Printf("✓ Application %s deleted successfully.\n", appID)
	return nil
}

// resolveAppID extracts appID from args or falls back to locally linked project (.nd/project.json).
func resolveAppID(args []string) (string, []string, error) {
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") && !strings.Contains(args[0], "=") {
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
	fmt.Printf("Device ID:   %s\n", cfg.DeviceID)
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

// RunStatus prints detailed info for an app in clean Heroku/Railway style.
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

	type domainInfo struct {
		Domain      string `json:"Domain"`
		Port        int    `json:"Port"`
		EnableHTTPS bool   `json:"EnableHTTPS"`
	}
	type psInfo struct {
		Name    string `json:"Name"`
		Service string `json:"Service"`
		State   string `json:"State"`
		Status  string `json:"Status"`
		Image   string `json:"Image"`
	}
	type httpCheck struct {
		Domain     string `json:"domain"`
		URL        string `json:"url"`
		StatusCode int    `json:"status_code"`
		LatencyMS  int    `json:"latency_ms"`
		OK         bool   `json:"ok"`
	}

	var data struct {
		App struct {
			ID        string `json:"ID"`
			Name      string `json:"Name"`
			Status    string `json:"Status"`
			CreatedAt string `json:"CreatedAt"`
		} `json:"app"`
		Domains  []domainInfo `json:"domains"`
		Services []string     `json:"services"`
		PS       []psInfo     `json:"ps"`
		Health   struct {
			Healthy    bool        `json:"healthy"`
			HTTPChecks []httpCheck `json:"http_checks"`
		} `json:"health"`
	}
	if err := json.Unmarshal([]byte(resp.Result.Content[0].Text), &data); err != nil {
		fmt.Println(resp.Result.Content[0].Text)
		return nil
	}

	created := data.App.CreatedAt
	if t, err := time.Parse(time.RFC3339, created); err == nil {
		created = t.Format("Jan 02, 2006 15:04")
	}

	fmt.Printf("=== %s (%s)\n", data.App.Name, data.App.ID)
	fmt.Printf("Status:    %s\n", data.App.Status)
	fmt.Printf("Created:   %s\n", created)

	// Domains
	if len(data.Domains) > 0 {
		fmt.Printf("Domains:\n")
		for _, d := range data.Domains {
			scheme := "http"
			if d.EnableHTTPS {
				scheme = "https"
			}
			fmt.Printf("  • %s://%s (port %d)\n", scheme, d.Domain, d.Port)
		}
	} else {
		fmt.Printf("Domains:   (none configured)\n")
	}

	// Containers
	fmt.Printf("\nContainers (%d):\n", len(data.PS))
	if len(data.PS) > 0 {
		fmt.Printf("  %-12s %-25s %-12s %s\n", "SERVICE", "CONTAINER", "STATE", "STATUS")
		fmt.Printf("  %s\n", strings.Repeat("─", 65))
		for _, p := range data.PS {
			st := p.State
			if st == "running" {
				st = "● running"
			}
			fmt.Printf("  %-12s %-25s %-12s %s\n", p.Service, p.Name, st, p.Status)
		}
	} else {
		fmt.Printf("  (no running containers — run 'nd deploy' or 'nd push')\n")
	}

	// Health
	if len(data.Health.HTTPChecks) > 0 {
		fmt.Printf("\nHealth Checks:\n")
		for _, hc := range data.Health.HTTPChecks {
			icon := "✓"
			if !hc.OK {
				icon = "✗"
			}
			fmt.Printf("  %s %s -> %d (%dms)\n", icon, hc.URL, hc.StatusCode, hc.LatencyMS)
		}
	}

	return nil
}

// RunPS lists containers, their state, status, and images for an app (or all apps).
func RunPS(cl *client.Client, args []string) error {
	appID, _, err := resolveAppID(args)
	if err != nil {
		return runAllPS(cl)
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

	var resp struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &resp); err != nil || len(resp.Result.Content) == 0 {
		return fmt.Errorf("unexpected response from server")
	}

	var data struct {
		App struct {
			ID     string `json:"ID"`
			Name   string `json:"Name"`
			Status string `json:"Status"`
		} `json:"app"`
		PS []struct {
			Name    string `json:"Name"`
			Service string `json:"Service"`
			State   string `json:"State"`
			Status  string `json:"Status"`
			Image   string `json:"Image"`
		} `json:"ps"`
		Health struct {
			Healthy bool `json:"healthy"`
		} `json:"health"`
	}
	if err := json.Unmarshal([]byte(resp.Result.Content[0].Text), &data); err != nil {
		fmt.Println(resp.Result.Content[0].Text)
		return nil
	}

	healthStr := "healthy"
	if !data.Health.Healthy && len(data.PS) > 0 {
		healthStr = "unhealthy"
	}
	fmt.Printf("=== %s (%s) — %s (%d containers)\n", data.App.Name, data.App.ID, healthStr, len(data.PS))
	if len(data.PS) == 0 {
		fmt.Println("No containers running. Deploy with: nd push or nd deploy")
		return nil
	}

	fmt.Printf("%-15s %-25s %-25s %-12s %s\n", "SERVICE", "CONTAINER", "IMAGE", "STATE", "STATUS")
	fmt.Println(strings.Repeat("─", 90))
	for _, p := range data.PS {
		img := p.Image
		if img == "" {
			img = "-"
		}
		state := p.State
		if state == "running" {
			state = "● running"
		}
		fmt.Printf("%-15s %-25s %-25s %-12s %s\n", p.Service, p.Name, img, state, p.Status)
	}
	return nil
}

func runAllPS(cl *client.Client) error {
	body, status, err := cl.Do("GET", "/api/v1/apps", nil)
	if err != nil || status >= 400 {
		return RunApps(cl, nil)
	}
	var apps []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(body, &apps); err != nil || len(apps) == 0 {
		fmt.Println("No apps found.")
		return nil
	}

	for i, a := range apps {
		if i > 0 {
			fmt.Println()
		}
		_ = RunPS(cl, []string{a.ID})
	}
	return nil
}

// RunContainers lists all Docker containers on the host VPS.
func RunContainers(cl *client.Client, args []string) error {
	cmdArgs := []string{"docker", "ps"}
	for _, a := range args {
		if a == "-a" || a == "--all" {
			cmdArgs = append(cmdArgs, "-a")
		}
	}
	err := executeHostCommand(cl, strings.Join(cmdArgs, " "))
	if err != nil {
		// Fallback: list containers across all accessible apps
		return runAllPS(cl)
	}
	return nil
}

// RunImages lists Docker images on the host VPS, or falls back to app image breakdown.
func RunImages(cl *client.Client, _ []string) error {
	err := executeHostCommand(cl, "docker images")
	if err != nil {
		// Fallback: extract images used by each app
		return runAppImages(cl)
	}
	return nil
}

func runAppImages(cl *client.Client) error {
	body, status, err := cl.Do("GET", "/api/v1/apps", nil)
	if err != nil || status >= 400 {
		return fmt.Errorf("failed to list apps: %v", err)
	}
	var apps []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(body, &apps); err != nil || len(apps) == 0 {
		fmt.Println("No applications found.")
		return nil
	}

	type imgUsage struct {
		Image   string
		App     string
		Service string
	}
	var usages []imgUsage
	distinctImages := make(map[string]bool)

	for _, a := range apps {
		cBody, cStatus, cErr := cl.Do("POST", "/mcp", map[string]interface{}{
			"method": "tools/call",
			"params": map[string]interface{}{
				"name":      "compose_get",
				"arguments": map[string]interface{}{"app_id": a.ID},
			},
		})
		if cErr != nil || cStatus >= 400 {
			continue
		}
		var cResp struct {
			Result struct {
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"result"`
		}
		if err := json.Unmarshal(cBody, &cResp); err != nil || len(cResp.Result.Content) == 0 {
			continue
		}

		lines := strings.Split(cResp.Result.Content[0].Text, "\n")
		var currentService string
		inServices := false
		serviceImages := make(map[string]string)

		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(line, "services:") {
				inServices = true
				continue
			}
			if inServices {
				if !strings.HasPrefix(line, " ") && trimmed != "" && !strings.HasPrefix(trimmed, "#") {
					inServices = false
					continue
				}
				if (strings.HasPrefix(line, "    ") && !strings.HasPrefix(line, "      ") && strings.HasSuffix(trimmed, ":")) ||
					(strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "    ") && strings.HasSuffix(trimmed, ":")) {
					currentService = strings.TrimSuffix(trimmed, ":")
					continue
				}
				if currentService != "" {
					if strings.HasPrefix(trimmed, "image:") {
						img := strings.TrimSpace(strings.TrimPrefix(trimmed, "image:"))
						img = strings.Trim(img, `"'`)
						serviceImages[currentService] = img
					} else if strings.HasPrefix(trimmed, "build:") {
						if _, exists := serviceImages[currentService]; !exists {
							serviceImages[currentService] = fmt.Sprintf("%s_%s:latest (local build)", a.ID, currentService)
						}
					}
				}
			}
		}

		for svc, img := range serviceImages {
			usages = append(usages, imgUsage{
				Image:   img,
				App:     fmt.Sprintf("%s (%s)", a.Name, a.ID),
				Service: svc,
			})
			distinctImages[img] = true
		}
	}

	fmt.Printf("=== Application Images (%d images across %d apps)\n", len(distinctImages), len(apps))
	if len(usages) == 0 {
		fmt.Println("No configured images found in application compose files.")
		return nil
	}

	fmt.Printf("%-35s %-32s %s\n", "IMAGE", "APPLICATION", "SERVICE")
	fmt.Println(strings.Repeat("─", 82))
	for _, u := range usages {
		fmt.Printf("%-35s %-32s %s\n", u.Image, u.App, u.Service)
	}
	return nil
}

func executeHostCommand(cl *client.Client, cmdStr string) error {
	body, status, err := cl.Do("POST", "/mcp", map[string]interface{}{
		"method": "tools/call",
		"params": map[string]interface{}{
			"name":      "server_exec",
			"arguments": map[string]interface{}{"command": cmdStr},
		},
	})
	if err != nil {
		return err
	}
	if status >= 400 {
		return fmt.Errorf("server error: %s", client.JSONError(body))
	}
	return parseAndPrintExecResult(body)
}

// RunOpen opens the application's live domain or panel URL in the browser.
func RunOpen(cl *client.Client, args []string) error {
	appID, _, err := resolveAppID(args)
	if err != nil {
		return err
	}

	body, status, err := cl.Do("POST", "/mcp", map[string]interface{}{
		"method": "tools/call",
		"params": map[string]interface{}{
			"name":      "app_get",
			"arguments": map[string]interface{}{"app_id": appID},
		},
	})
	if err != nil || status >= 400 {
		return fmt.Errorf("failed to get app info: %s", client.JSONError(body))
	}

	var resp struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	_ = json.Unmarshal(body, &resp)
	targetURL := ""
	if len(resp.Result.Content) > 0 {
		var data struct {
			Domains []struct {
				Domain      string `json:"Domain"`
				EnableHTTPS bool   `json:"EnableHTTPS"`
			} `json:"domains"`
		}
		_ = json.Unmarshal([]byte(resp.Result.Content[0].Text), &data)
		if len(data.Domains) > 0 {
			d := data.Domains[0]
			scheme := "http"
			if d.EnableHTTPS {
				scheme = "https"
			}
			targetURL = fmt.Sprintf("%s://%s", scheme, d.Domain)
		}
	}

	if targetURL == "" {
		cfg, _ := config.Load()
		targetURL = fmt.Sprintf("%s/apps/%s", strings.TrimRight(cfg.ServerURL, "/"), appID)
	}

	fmt.Printf("Opening %s ...\n", targetURL)
	return openBrowser(targetURL)
}

func openBrowser(url string) error {
	var c *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		c = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		c = exec.Command("open", url)
	default:
		c = exec.Command("xdg-open", url)
	}
	return c.Start()
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

		var resp struct {
			Result struct {
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"result"`
		}
		if err := json.Unmarshal(body, &resp); err == nil && len(resp.Result.Content) > 0 {
			var envData struct {
				Keys  []string `json:"keys"`
				Count int      `json:"count"`
			}
			if err := json.Unmarshal([]byte(resp.Result.Content[0].Text), &envData); err == nil {
				fmt.Printf("=== %s Environment Variables (%d)\n", appID, envData.Count)
				if len(envData.Keys) == 0 {
					fmt.Println("  (no environment variables set)")
				} else {
					for _, k := range envData.Keys {
						fmt.Printf("  • %s\n", k)
					}
				}
				return nil
			}
		}
		fmt.Println(string(body))

	case "set":
		if len(envArgs) < 1 {
			return fmt.Errorf("usage: nd env set [app_id] KEY=VALUE [KEY2=VALUE2 ...]")
		}
		vars := make(map[string]interface{}, len(envArgs))
		for _, kv := range envArgs {
			idx := strings.Index(kv, "=")
			if idx <= 0 {
				return fmt.Errorf("invalid env format %q, expected KEY=VALUE", kv)
			}
			k := strings.TrimSpace(kv[:idx])
			v := kv[idx+1:]
			vars[k] = v
		}
		body, status, err := cl.Do("POST", "/mcp", map[string]interface{}{
			"method": "tools/call",
			"params": map[string]interface{}{
				"name": "env_set",
				"arguments": map[string]interface{}{
					"app_id":    appID,
					"variables": vars,
				},
			},
		})
		if err != nil {
			return err
		}
		if status >= 400 {
			return fmt.Errorf("server error: %s", client.JSONError(body))
		}
		var setResp struct {
			Result struct {
				IsError bool `json:"isError"`
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"result"`
		}
		if err := json.Unmarshal(body, &setResp); err == nil {
			if setResp.Result.IsError && len(setResp.Result.Content) > 0 {
				return fmt.Errorf("%s", setResp.Result.Content[0].Text)
			}
		}
		fmt.Println("✓ Environment updated.")

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
	fmt.Fprintf(os.Stderr, `nd — NextDeploy CLI (PaaS Management)

Authentication & Session:
  nd login <server_url> [token]      Authenticate with an API token
  nd logout                          Remove saved credentials and disconnect session
  nd whoami                          Show authenticated server, user, and session status

App Management:
  nd apps                            List all applications
  nd create <name>                   Create and link a new application
  nd delete [app_id]                 Delete an application (requires confirmation)
  nd link <app_id>                   Link current directory to an app (.nd/project.json)
  nd unlink                          Remove link from current directory
  nd info [app_id]                   Show app details, domains, health, and status (alias: nd status)
  nd open [app_id]                   Open app domain or panel URL in browser

Process & Container Inspection:
  nd ps [app_id]                     Show containers, services, state, status, and images
  nd containers [-a]                 List all Docker containers on the host VPS
  nd images                          List Docker images on the host VPS

Deployments & Lifecycle:
  nd push [app_id] [dir]             Sync local files and trigger deployment
  nd deploy [app_id]                 Redeploy without file sync
  nd stop [app_id]                   Stop application containers
  nd restart [app_id]                Restart application containers
  nd logs [app_id] [-n lines]        Show recent container logs (default: 100 lines)

Execution & Command Run:
  nd exec [flags] [app_id] <cmd...>  Run command inside app container (alias: nd run)
  nd server-exec <cmd...>            Run command directly on host VPS (requires allow_server_exec)

Environment Variables:
  nd env list [app_id]               List configured environment variable keys
  nd env set [app_id] KEY=VALUE ...  Set or update environment variables

Other:
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

