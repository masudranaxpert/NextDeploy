package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"nd/internal/client"
	"nd/internal/config"
	"nd/internal/ui"
)

// Version can be overwritten at build time or by main.
var Version = "1.2.1"

// posixQuote escapes a single shell argument safely.
func posixQuote(arg string) string {
	if arg == "" {
		return "''"
	}
	safe := true
	for _, r := range arg {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') ||
			r == '-' || r == '_' || r == '.' || r == '/' || r == ':' || r == '@') {
			safe = false
			break
		}
	}
	if safe {
		return arg
	}
	return "'" + strings.ReplaceAll(arg, "'", `'\''`) + "'"
}

// posixJoin joins tokens into a safe shell command string.
func posixJoin(args []string) string {
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = posixQuote(a)
	}
	return strings.Join(quoted, " ")
}

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
		fmt.Print(ui.Bold("API Token") + " (generate in panel under Developer & AI -> CLI Sessions): ")
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
	c := client.New(cfg, cfg.DeviceID, Version)
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

	ui.Success("Logged in to %s", ui.Cyan(serverURL))
	return nil
}

// RunLogout clears the saved config and unregisters the device session.
func RunLogout(_ []string) error {
	cfg, err := config.Load()
	if err == nil && cfg.ServerURL != "" && cfg.DeviceID != "" {
		c := client.New(cfg, cfg.DeviceID, Version)
		c.Disconnect()
	}
	if err := config.Save(config.Config{}); err != nil {
		return err
	}
	ui.Success("Logged out successfully.")
	return nil
}

// RunApps prints all accessible apps, or dispatches subcommands.
func RunApps(cl *client.Client, args []string) error {
	jsonOut, args := extractJSONFlag(args)
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

	if jsonOut {
		if apps == nil {
			apps = []struct {
				ID     string `json:"id"`
				Name   string `json:"name"`
				Status string `json:"status"`
			}{}
		}
		pretty, _ := json.MarshalIndent(apps, "", "  ")
		fmt.Println(string(pretty))
		return nil
	}

	headers := []string{"ID", "Name", "Status"}
	var rows [][]string
	for _, a := range apps {
		rows = append(rows, []string{
			ui.Cyan(a.ID),
			ui.Bold(a.Name),
			ui.StatePill(a.Status),
		})
	}
	ui.PrintTable(os.Stdout, headers, rows)
	if len(apps) == 0 {
		fmt.Println(ui.Dim("No apps found. Create one with: nd create <name>"))
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

	ui.Success("Created application %s (ID: %s)", ui.Bold(resData.Name), ui.Cyan(appID))
	if autoLink {
		if err := config.SaveProject(".", appID); err == nil {
			ui.Success("Linked current directory to %s (.nd/project.json)", ui.Cyan(appID))
		}
	}
	fmt.Printf("\n%s\n  %s %s\n  %s %s\n  %s %s\n",
		ui.Bold("Next steps:"),
		ui.Cyan("nd push"), ui.Dim("# Upload code and build/deploy"),
		ui.Cyan("nd logs"), ui.Dim("# View live container logs"),
		ui.Cyan("nd status"), ui.Dim("# Check app health and URLs"),
	)
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
		ui.Warn("Deleting %q will remove its containers, volumes, and workspace files.", ui.Red(appID))
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
				"arguments": map[string]interface{}{"app_id": appID, "confirm_name": appID},
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
	ui.Success("Application %s deleted successfully.", ui.Cyan(appID))
	return nil
}

// extractJSONFlag removes --json / -j from args and returns whether it was present.
func extractJSONFlag(args []string) (bool, []string) {
	var remaining []string
	jsonOut := false
	for _, a := range args {
		if a == "--json" || a == "-j" {
			jsonOut = true
		} else {
			remaining = append(remaining, a)
		}
	}
	return jsonOut, remaining
}

// resolveAppID extracts appID from args (--app / -a flag, positional argument, or .nd/project.json).
func resolveAppID(args []string) (string, []string, error) {
	var appID string
	var remaining []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if (a == "-a" || a == "--app") && i+1 < len(args) {
			appID = args[i+1]
			i++
		} else if strings.HasPrefix(a, "--app=") {
			appID = strings.TrimPrefix(a, "--app=")
		} else if strings.HasPrefix(a, "-a=") {
			appID = strings.TrimPrefix(a, "-a=")
		} else {
			remaining = append(remaining, a)
		}
	}

	if appID != "" {
		return appID, remaining, nil
	}

	if len(remaining) > 0 && !strings.HasPrefix(remaining[0], "-") && !strings.Contains(remaining[0], "=") {
		return remaining[0], remaining[1:], nil
	}

	pc, err := config.LoadProject(".")
	if err == nil && pc.AppID != "" {
		return pc.AppID, remaining, nil
	}
	return "", remaining, fmt.Errorf("app_id required (pass --app <id>, specify as argument, or run 'nd link <id>')")
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
	ui.Success("Linked current directory to %s (.nd/project.json)", ui.Cyan(appID))
	return nil
}

// RunUnlink removes the project link in the current directory.
func RunUnlink(_ *client.Client, _ []string) error {
	if err := config.ClearProject("."); err != nil {
		return fmt.Errorf("failed to unlink: %w", err)
	}
	ui.Success("Unlinked project from current directory.")
	return nil
}

// RunWhoami prints currently configured server, device info, and token validity.
func RunWhoami(cl *client.Client, cfg config.Config, args []string) error {
	jsonOut, _ := extractJSONFlag(args)
	hostname, _ := os.Hostname()
	maskedToken := cfg.Token
	if len(maskedToken) > 12 {
		maskedToken = maskedToken[:8] + "..." + maskedToken[len(maskedToken)-4:]
	}

	var linkedApp string
	if pc, err := config.LoadProject("."); err == nil && pc.AppID != "" {
		linkedApp = pc.AppID
	}

	// Try fetching server-side whoami information (username, role, token_name, permissions)
	var whoamiData struct {
		OK                 bool   `json:"ok"`
		Username           string `json:"username"`
		Role               string `json:"role"`
		TokenName          string `json:"token_name"`
		TokenPrefix        string `json:"token_prefix"`
		AllowEnvReveal     bool   `json:"allow_env_reveal"`
		AllowServerExec    bool   `json:"allow_server_exec"`
		AllowContainerExec bool   `json:"allow_container_exec"`
	}

	wBody, wStatus, wErr := cl.Do("GET", "/api/v1/cli/whoami", nil)
	authOK := wErr == nil && wStatus < 400
	if authOK {
		_ = json.Unmarshal(wBody, &whoamiData)
	} else {
		// Fallback for older server versions: test auth via app_list
		body, status, err := cl.Do("POST", "/mcp", map[string]interface{}{
			"method": "tools/call",
			"params": map[string]interface{}{"name": "app_list", "arguments": map[string]interface{}{}},
		})
		authOK = err == nil && status < 400
		if !authOK && wBody == nil {
			wBody = body
		}
	}

	if jsonOut {
		out := map[string]interface{}{
			"server_url": cfg.ServerURL,
			"device_id":  cfg.DeviceID,
			"hostname":   hostname,
			"os":         runtime.GOOS,
			"arch":       runtime.GOARCH,
			"status":     "authenticated",
		}
		if !authOK {
			out["status"] = "error"
			out["error"] = client.JSONError(wBody)
		}
		if whoamiData.Username != "" {
			out["username"] = whoamiData.Username
			out["role"] = whoamiData.Role
		}
		if whoamiData.TokenName != "" {
			out["token_name"] = whoamiData.TokenName
			out["token_prefix"] = whoamiData.TokenPrefix
			out["permissions"] = map[string]bool{
				"env_reveal":     whoamiData.AllowEnvReveal,
				"server_exec":    whoamiData.AllowServerExec,
				"container_exec": whoamiData.AllowContainerExec,
			}
		}
		if linkedApp != "" {
			out["linked_app"] = linkedApp
		}
		pretty, _ := json.MarshalIndent(out, "", "  ")
		fmt.Println(string(pretty))
		if !authOK {
			return fmt.Errorf("authentication failed: %s", client.JSONError(wBody))
		}
		return nil
	}

	ui.KeyValue("Server URL", ui.Cyan(cfg.ServerURL))
	if whoamiData.Username != "" {
		ui.KeyValue("User", fmt.Sprintf("%s (%s)", ui.Bold(whoamiData.Username), whoamiData.Role))
	}
	if whoamiData.TokenName != "" {
		ui.KeyValue("Token Name", ui.HiYellow(whoamiData.TokenName))
	}
	ui.KeyValue("Token", ui.Dim(maskedToken))

	if whoamiData.TokenName != "" {
		var perms []string
		if whoamiData.AllowContainerExec {
			perms = append(perms, ui.Green("container_exec"))
		}
		if whoamiData.AllowServerExec {
			perms = append(perms, ui.Yellow("server_exec"))
		}
		if whoamiData.AllowEnvReveal {
			perms = append(perms, ui.Yellow("secrets"))
		}
		if len(perms) == 0 {
			perms = append(perms, ui.Dim("standard"))
		}
		ui.KeyValue("Permissions", strings.Join(perms, ", "))
	}

	ui.KeyValue("Device", fmt.Sprintf("%s (%s/%s)", hostname, runtime.GOOS, runtime.GOARCH))
	ui.KeyValue("Device ID", ui.Dim(cfg.DeviceID))

	// Check if local project is linked
	if linkedApp != "" {
		ui.KeyValue("Linked App", ui.HiCyan(linkedApp))
	}

	if !authOK {
		ui.KeyValue("Status", ui.Red("Connection Error (%s)", client.JSONError(wBody)))
		return fmt.Errorf("server error: %s", client.JSONError(wBody))
	}
	ui.KeyValue("Status", ui.Green("Authenticated ✓"))
	return nil
}

// RunStatus prints detailed info for an app in clean Heroku/Railway style.
func RunStatus(cl *client.Client, args []string) error {
	jsonOut, args := extractJSONFlag(args)
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

	if jsonOut {
		var raw interface{}
		if err := json.Unmarshal([]byte(resp.Result.Content[0].Text), &raw); err == nil {
			pretty, _ := json.MarshalIndent(raw, "", "  ")
			fmt.Println(string(pretty))
		} else {
			fmt.Println(resp.Result.Content[0].Text)
		}
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

	fmt.Printf("\n=== %s (%s)\n", ui.HiCyan(data.App.Name), ui.Dim(data.App.ID))
	ui.KeyValue("Status", ui.StatePill(data.App.Status))
	ui.KeyValue("Created", created)

	// Domains
	if len(data.Domains) > 0 {
		fmt.Printf("\n%s\n", ui.Bold("Domains:"))
		for _, d := range data.Domains {
			scheme := "http"
			if d.EnableHTTPS {
				scheme = "https"
			}
			fmt.Printf("  • %s %s\n", ui.Cyan(fmt.Sprintf("%s://%s", scheme, d.Domain)), ui.Dim(fmt.Sprintf("(port %d)", d.Port)))
		}
	} else {
		ui.KeyValue("Domains", ui.Dim("(none configured)"))
	}

	// Containers
	fmt.Printf("\n%s (%d):\n", ui.Bold("Containers"), len(data.PS))
	if len(data.PS) > 0 {
		headers := []string{"Service", "Container", "State", "Status"}
		var rows [][]string
		for _, p := range data.PS {
			rows = append(rows, []string{
				ui.Bold(p.Service),
				p.Name,
				ui.StatePill(p.State),
				p.Status,
			})
		}
		ui.PrintTable(os.Stdout, headers, rows)
	} else {
		fmt.Printf("  %s\n", ui.Dim("(no running containers — run 'nd deploy' or 'nd push')"))
	}

	// Health
	if len(data.Health.HTTPChecks) > 0 {
		fmt.Printf("\n%s\n", ui.Bold("Health Checks:"))
		for _, hc := range data.Health.HTTPChecks {
			icon := ui.Green("✓")
			if !hc.OK {
				icon = ui.Red("✗")
			}
			fmt.Printf("  %s %s -> %d (%dms)\n", icon, ui.Cyan(hc.URL), hc.StatusCode, hc.LatencyMS)
		}
	}

	return nil
}

// RunPS lists containers, their state, status, and images for an app (or all apps).
func RunPS(cl *client.Client, args []string) error {
	jsonOut, args := extractJSONFlag(args)
	appID, _, err := resolveAppID(args)
	if err != nil {
		return runAllPS(cl, jsonOut)
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
			Ports   string `json:"Ports,omitempty"`
		} `json:"ps"`
		Health struct {
			Healthy bool `json:"healthy"`
		} `json:"health"`
	}
	if err := json.Unmarshal([]byte(resp.Result.Content[0].Text), &data); err != nil {
		fmt.Println(resp.Result.Content[0].Text)
		return nil
	}

	if jsonOut {
		if data.PS == nil {
			data.PS = []struct {
				Name    string `json:"Name"`
				Service string `json:"Service"`
				State   string `json:"State"`
				Status  string `json:"Status"`
				Image   string `json:"Image"`
				Ports   string `json:"Ports,omitempty"`
			}{}
		}
		pretty, _ := json.MarshalIndent(data.PS, "", "  ")
		fmt.Println(string(pretty))
		return nil
	}

	healthStr := ui.HealthBadge(data.Health.Healthy)
	if len(data.PS) == 0 {
		healthStr = ui.Dim("idle")
	}
	fmt.Printf("\n=== %s (%s) — %s (%d containers)\n",
		ui.HiCyan(data.App.Name), ui.Dim(data.App.ID), healthStr, len(data.PS))
	if len(data.PS) == 0 {
		fmt.Println("  No containers running. Deploy with: nd push or nd deploy")
		return nil
	}

	headers := []string{"Service", "Container", "Image", "Ports", "State", "Status"}
	var rows [][]string
	for _, p := range data.PS {
		img := p.Image
		if img == "" {
			img = ui.Dim("-")
		}
		ports := p.Ports
		if ports == "" {
			ports = ui.Dim("-")
		}
		rows = append(rows, []string{
			ui.Bold(p.Service),
			p.Name,
			img,
			ports,
			ui.StatePill(p.State),
			p.Status,
		})
	}
	ui.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runAllPS(cl *client.Client, jsonOut bool) error {
	body, status, err := cl.Do("GET", "/api/v1/apps", nil)
	if err != nil || status >= 400 {
		return RunApps(cl, nil)
	}
	var apps []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(body, &apps); err != nil || len(apps) == 0 {
		if jsonOut {
			fmt.Println("[]")
		} else {
			fmt.Println("No apps found.")
		}
		return nil
	}

	type containerWithApp struct {
		AppID   string `json:"app_id"`
		AppName string `json:"app_name"`
		Name    string `json:"name"`
		Service string `json:"service"`
		Image   string `json:"image"`
		Ports   string `json:"ports,omitempty"`
		State   string `json:"state"`
		Status  string `json:"status"`
	}
	var allContainers []containerWithApp

	for i, a := range apps {
		cBody, cStatus, cErr := cl.Do("POST", "/mcp", map[string]interface{}{
			"method": "tools/call",
			"params": map[string]interface{}{
				"name": "app_get",
				"arguments": map[string]interface{}{
					"app_id": a.ID,
				},
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
		if json.Unmarshal(cBody, &cResp) != nil || len(cResp.Result.Content) == 0 {
			continue
		}
		var data struct {
			PS []struct {
				Name    string `json:"Name"`
				Service string `json:"Service"`
				State   string `json:"State"`
				Status  string `json:"Status"`
				Image   string `json:"Image"`
				Ports   string `json:"Ports,omitempty"`
			} `json:"ps"`
		}
		if json.Unmarshal([]byte(cResp.Result.Content[0].Text), &data) != nil {
			continue
		}
		if jsonOut {
			for _, p := range data.PS {
				allContainers = append(allContainers, containerWithApp{
					AppID:   a.ID,
					AppName: a.Name,
					Name:    p.Name,
					Service: p.Service,
					Image:   p.Image,
					Ports:   p.Ports,
					State:   p.State,
					Status:  p.Status,
				})
			}
		} else {
			if i > 0 {
				fmt.Println()
			}
			_ = RunPS(cl, []string{a.ID})
		}
	}

	if jsonOut {
		if allContainers == nil {
			allContainers = []containerWithApp{}
		}
		pretty, _ := json.MarshalIndent(allContainers, "", "  ")
		fmt.Println(string(pretty))
	}
	return nil
}

// RunContainers lists all Docker containers on the host VPS, or for a specific app if provided.
func RunContainers(cl *client.Client, args []string) error {
	jsonOut, args := extractJSONFlag(args)

	// If appID is provided or directory is linked and explicit app requested, delegate to RunPS
	var cleanArgs []string
	hasAllFlag := false
	for _, a := range args {
		if a == "-a" || a == "--all" {
			hasAllFlag = true
		} else {
			cleanArgs = append(cleanArgs, a)
		}
	}

	if len(cleanArgs) > 0 {
		appID, _, err := resolveAppID(cleanArgs)
		if err == nil && appID != "" {
			var psArgs []string
			if jsonOut {
				psArgs = append(psArgs, "--json")
			}
			psArgs = append(psArgs, appID)
			return RunPS(cl, psArgs)
		}
	}

	cmdArgs := []string{"docker", "ps"}
	if hasAllFlag {
		cmdArgs = append(cmdArgs, "-a")
	}
	if jsonOut {
		cmdArgs = append(cmdArgs, "--format", "{{json .}}")
	}
	err := executeHostCommand(cl, strings.Join(cmdArgs, " "))
	if err != nil {
		// Fallback: list containers across all accessible apps
		return runAllPS(cl, jsonOut)
	}
	return nil
}

// RunImages lists Docker images on the host VPS, or falls back to app image breakdown.
func RunImages(cl *client.Client, args []string) error {
	jsonOut, _ := extractJSONFlag(args)
	cmdStr := "docker images"
	if jsonOut {
		cmdStr = "docker images --format {{json .}}"
	}
	err := executeHostCommand(cl, cmdStr)
	if err != nil {
		// Fallback: extract images used by each app
		return runAppImages(cl, jsonOut)
	}
	return nil
}

func runAppImages(cl *client.Client, jsonOut bool) error {
	body, status, err := cl.Do("GET", "/api/v1/apps", nil)
	if err != nil || status >= 400 {
		return fmt.Errorf("failed to list apps: %v", err)
	}
	var apps []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(body, &apps); err != nil || len(apps) == 0 {
		if jsonOut {
			fmt.Println("[]")
		} else {
			fmt.Println("No applications found.")
		}
		return nil
	}

	type imgUsage struct {
		Image   string `json:"image"`
		App     string `json:"app"`
		Service string `json:"service"`
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

	if jsonOut {
		if usages == nil {
			usages = []imgUsage{}
		}
		pretty, _ := json.MarshalIndent(usages, "", "  ")
		fmt.Println(string(pretty))
		return nil
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
	jsonOut, args := extractJSONFlag(args)
	rebuild := false
	var cleanArgs []string
	for _, a := range args {
		if a == "--rebuild" || a == "-r" {
			rebuild = true
		} else {
			cleanArgs = append(cleanArgs, a)
		}
	}

	appID, _, err := resolveAppID(cleanArgs)
	if err != nil {
		return err
	}
	if !jsonOut {
		if rebuild {
			ui.Step("Rebuilding & deploying %s...", ui.Cyan(appID))
		} else {
			ui.Step("Deploying %s...", ui.Cyan(appID))
		}
	}

	deployArgs := map[string]interface{}{"app_id": appID, "wait_seconds": 120}
	if rebuild {
		deployArgs["rebuild"] = true
	}

	body, status, err := cl.DoWithTimeout("POST", "/mcp", map[string]interface{}{
		"method": "tools/call",
		"params": map[string]interface{}{
			"name":      "deploy",
			"arguments": deployArgs,
		},
	}, 3*time.Minute)
	if err != nil {
		return err
	}
	if status >= 400 {
		return fmt.Errorf("deploy error: %s", client.JSONError(body))
	}

	var resp struct {
		Result struct {
			IsError bool `json:"isError"`
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	_ = json.Unmarshal(body, &resp)

	var data struct {
		JobID      string  `json:"job_id"`
		AppID      string  `json:"app_id"`
		Action     string  `json:"action"`
		OK         bool    `json:"ok"`
		Status     string  `json:"status"`
		DurationS  float64 `json:"duration_s"`
		OutputTail string  `json:"output_tail"`
		Message    string  `json:"message"`
	}
	if len(resp.Result.Content) > 0 {
		_ = json.Unmarshal([]byte(resp.Result.Content[0].Text), &data)
	}

	deployStatus := "success"
	if resp.Result.IsError || (!data.OK && data.Status != "started") {
		deployStatus = "failed"
		if data.Status == "timeout" {
			deployStatus = "timeout"
		}
	}

	if jsonOut {
		out := map[string]interface{}{
			"job_id":     data.JobID,
			"app_id":     appID,
			"status":     deployStatus,
			"duration_s": data.DurationS,
		}
		if data.Message != "" {
			out["message"] = data.Message
		}
		if deployStatus == "failed" && data.OutputTail != "" {
			out["error_tail"] = data.OutputTail
		}
		pretty, _ := json.MarshalIndent(out, "", "  ")
		fmt.Println(string(pretty))
		if deployStatus == "failed" {
			return fmt.Errorf("deployment failed")
		}
		return nil
	}

	if deployStatus == "failed" {
		if data.OutputTail != "" {
			fmt.Println(data.OutputTail)
		}
		return fmt.Errorf("deployment failed after %.0fs (Job ID: %s)", data.DurationS, data.JobID)
	}

	ui.Success("Deployment succeeded in %.0fs (Job ID: %s)", data.DurationS, ui.Cyan(data.JobID))
	return nil
}

// RunLogs streams recent logs for an app (container logs or deployment logs).
// Usage:
//   nd logs [app_id] [-n lines] [-f/--follow]
//   nd logs --deploy [app_id] [-f/--follow]
func RunLogs(cl *client.Client, args []string) error {
	lines := 100
	follow := false
	isDeploy := false
	service := ""
	var cleanArgs []string

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "-f" || arg == "--follow" {
			follow = true
		} else if arg == "--deploy" || arg == "-d" {
			isDeploy = true
		} else if (arg == "-s" || arg == "--service") && i+1 < len(args) {
			service = args[i+1]
			i++
		} else if strings.HasPrefix(arg, "--service=") {
			service = strings.TrimPrefix(arg, "--service=")
		} else if (arg == "-n" || arg == "--tail" || arg == "--lines") && i+1 < len(args) {
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

	if isDeploy {
		return runDeployLogs(cl, appID, follow)
	}

	fetchLogs := func(tailCount int) (string, error) {
		logArgs := map[string]interface{}{
			"app_id": appID,
			"tail":   tailCount,
			"lines":  tailCount,
		}
		if service != "" {
			logArgs["service"] = service
		}
		body, status, err := cl.Do("POST", "/mcp", map[string]interface{}{
			"method": "tools/call",
			"params": map[string]interface{}{
				"name":      "container_logs",
				"arguments": logArgs,
			},
		})
		if err != nil {
			return "", err
		}
		if status >= 400 {
			return "", fmt.Errorf("server error: %s", client.JSONError(body))
		}
		var resp struct {
			Result struct {
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"result"`
		}
		if err := json.Unmarshal(body, &resp); err == nil && len(resp.Result.Content) > 0 {
			return resp.Result.Content[0].Text, nil
		}
		return string(body), nil
	}

	initialLogs, err := fetchLogs(lines)
	if err != nil {
		return err
	}
	if initialLogs != "" {
		fmt.Print(initialLogs)
		if !strings.HasSuffix(initialLogs, "\n") {
			fmt.Println()
		}
	}

	if !follow {
		return nil
	}

	// Follow loop: polls every 1500ms and prints newly appended lines
	lastLogs := initialLogs
	for {
		time.Sleep(1500 * time.Millisecond)
		current, err := fetchLogs(100)
		if err != nil {
			continue
		}
		if current == lastLogs {
			continue
		}

		curLines := strings.Split(current, "\n")
		oldLines := strings.Split(lastLogs, "\n")

		// Find the longest overlap between tail of oldLines and head of curLines
		bestOverlap := 0
		for k := 1; k <= len(oldLines) && k <= len(curLines); k++ {
			match := true
			for j := 0; j < k; j++ {
				if oldLines[len(oldLines)-k+j] != curLines[j] {
					match = false
					break
				}
			}
			if match {
				bestOverlap = k
			}
		}

		newLines := curLines[bestOverlap:]
		if len(newLines) > 0 {
			for _, l := range newLines {
				if strings.TrimSpace(l) != "" {
					fmt.Println(l)
				}
			}
		}
		lastLogs = current
	}
}

func runDeployLogs(cl *client.Client, appID string, follow bool) error {
	fetchTail := func(offset int, waitSec int) (output string, nextOffset int, running bool, err error) {
		payload := map[string]interface{}{
			"app_id":       appID,
			"since_offset": offset,
			"wait_seconds": waitSec,
		}
		body, status, doErr := cl.Do("POST", "/mcp", map[string]interface{}{
			"method": "tools/call",
			"params": map[string]interface{}{
				"name":      "deploy_log_tail",
				"arguments": payload,
			},
		})
		if doErr != nil {
			return "", offset, false, doErr
		}
		if status >= 400 {
			return "", offset, false, fmt.Errorf("server error: %s", client.JSONError(body))
		}

		var resp struct {
			Result struct {
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"result"`
		}
		if unErr := json.Unmarshal(body, &resp); unErr != nil || len(resp.Result.Content) == 0 {
			return "", offset, false, fmt.Errorf("empty response from server")
		}

		// Check if active job streaming response
		var streamData struct {
			JobID      string `json:"job_id"`
			Action     string `json:"action"`
			Running    bool   `json:"running"`
			NextOffset int    `json:"next_offset"`
			NewOutput  string `json:"new_output"`
		}
		if json.Unmarshal([]byte(resp.Result.Content[0].Text), &streamData) == nil && streamData.JobID != "" {
			return streamData.NewOutput, streamData.NextOffset, streamData.Running, nil
		}

		// Otherwise fallback snapshot
		var snapData struct {
			LiveRunning bool   `json:"live_running"`
			LiveAction  string `json:"live_action"`
			LiveOutput  string `json:"live_output"`
			History     []struct {
				Action    string `json:"action"`
				Output    string `json:"output"`
				CreatedAt string `json:"created_at"`
				OK        bool   `json:"ok"`
			} `json:"history"`
		}
		if json.Unmarshal([]byte(resp.Result.Content[0].Text), &snapData) == nil {
			if snapData.LiveOutput != "" {
				return snapData.LiveOutput, len(snapData.LiveOutput), snapData.LiveRunning, nil
			}
			if len(snapData.History) > 0 {
				h := snapData.History[0]
				statusStr := "succeeded"
				if !h.OK {
					statusStr = "failed"
				}
				header := fmt.Sprintf("[%s - %s (%s)]\n", h.CreatedAt, h.Action, statusStr)
				return header + h.Output, len(h.Output), false, nil
			}
		}

		return "", offset, false, nil
	}

	initial, offset, running, err := fetchTail(0, 0)
	if err != nil {
		return err
	}
	if initial != "" {
		fmt.Print(initial)
		if !strings.HasSuffix(initial, "\n") {
			fmt.Println()
		}
	} else if !follow {
		fmt.Println("No deployment logs found for", appID)
		return nil
	}

	if !follow {
		return nil
	}

	// Stream logs if follow is enabled
	for {
		time.Sleep(500 * time.Millisecond)
		newOutput, next, isRunning, tailErr := fetchTail(offset, 15)
		if tailErr != nil {
			time.Sleep(1 * time.Second)
			continue
		}
		if newOutput != "" {
			fmt.Print(newOutput)
			if !strings.HasSuffix(newOutput, "\n") {
				fmt.Println()
			}
			offset = next
		}
		if running && !isRunning {
			// Deployment finished
			break
		}
		running = isRunning
		if !running && newOutput == "" {
			// No ongoing job
			break
		}
	}

	return nil
}

// RunExec executes a command inside an application container (Heroku-style: nd run / nd exec)
// Usage:
//   nd exec [flags] [app_id] <command...>
//   nd run [flags] [app_id] <command...>
//
// Flags:
//   -a, --app <app_id>               Target application ID (optional if linked)
//   -s, --service, -c, --container   Target service or container name (optional)
//   -w, --workdir <dir>              Working directory inside container
//   --server                         Run directly on the host VPS (requires allow_server_exec)
func RunExec(cl *client.Client, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: nd exec [flags] [app_id] <command...>\nexample: nd exec myapp ls -la\n         nd exec myapp -s redis redis-cli ping\n         nd exec -c myapp_db_1 psql -U postgres")
	}

	var (
		appID      string
		service    string
		workDir    string
		timeoutStr string
		stdinFlag  bool
		isServer   bool
		cmdTokens  []string
	)

	// Check if local project is linked
	linkedAppID := ""
	if pc, err := config.LoadProject("."); err == nil && pc.AppID != "" {
		linkedAppID = pc.AppID
	}

	i := 0
	for i < len(args) {
		arg := args[i]
		if arg == "-h" || arg == "--help" {
			fmt.Println("Usage: nd exec [flags] [app_id] <command...>")
			fmt.Println("       nd run [flags] [app_id] <command...>")
			fmt.Println("\nFlags:")
			fmt.Println("  -a, --app <app_id>               Target application ID (optional if linked)")
			fmt.Println("  -s, --service, -c, --container   Target service or container name (optional)")
			fmt.Println("  -w, --workdir <dir>              Working directory inside container")
			fmt.Println("  -i, --stdin                      Pass standard input to the command")
			fmt.Println("  -t, --timeout <duration>         Command timeout (e.g. 300s, 5m, default: 300s)")
			fmt.Println("  --server                         Run directly on the host VPS (requires allow_server_exec)")
			return nil
		} else if arg == "-i" || arg == "--stdin" {
			stdinFlag = true
			i++
		} else if (arg == "-t" || arg == "--timeout") && i+1 < len(args) {
			timeoutStr = args[i+1]
			i += 2
		} else if strings.HasPrefix(arg, "--timeout=") {
			timeoutStr = strings.TrimPrefix(arg, "--timeout=")
			i++
		} else if strings.HasPrefix(arg, "-t=") {
			timeoutStr = strings.TrimPrefix(arg, "-t=")
			i++
		} else if arg == "--server" {
			isServer = true
			i++
		} else if (arg == "-a" || arg == "--app") && i+1 < len(args) {
			appID = args[i+1]
			i += 2
		} else if strings.HasPrefix(arg, "--app=") {
			appID = strings.TrimPrefix(arg, "--app=")
			i++
		} else if (arg == "-s" || arg == "--service" || arg == "-c" || arg == "--container") && i+1 < len(args) {
			service = args[i+1]
			i += 2
		} else if strings.HasPrefix(arg, "--service=") {
			service = strings.TrimPrefix(arg, "--service=")
			i++
		} else if strings.HasPrefix(arg, "--container=") {
			service = strings.TrimPrefix(arg, "--container=")
			i++
		} else if (arg == "-w" || arg == "--workdir") && i+1 < len(args) {
			workDir = args[i+1]
			i += 2
		} else if strings.HasPrefix(arg, "--workdir=") {
			workDir = strings.TrimPrefix(arg, "--workdir=")
			i++
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

	var stdinData string
	stat, err := os.Stdin.Stat()
	isPipe := err == nil && (stat.Mode()&os.ModeCharDevice) == 0
	if stdinFlag || isPipe {
		data, rerr := io.ReadAll(os.Stdin)
		if rerr == nil && len(data) > 0 {
			stdinData = string(data)
		}
	}

	cmdStr := posixJoin(cmdTokens)

	mcpArgs := map[string]interface{}{
		"app_id":  appID,
		"command": cmdStr,
		"args":    cmdTokens,
	}
	if service != "" {
		mcpArgs["service"] = service
	}
	if workDir != "" {
		mcpArgs["work_dir"] = workDir
	}
	if stdinData != "" {
		mcpArgs["stdin"] = stdinData
	}

	timeoutSec := 300
	if timeoutStr != "" {
		if d, err := time.ParseDuration(timeoutStr); err == nil {
			timeoutSec = int(d.Seconds())
		} else if s, err := strconv.Atoi(timeoutStr); err == nil && s > 0 {
			timeoutSec = s
		}
	}
	if timeoutSec < 1 {
		timeoutSec = 1
	}
	mcpArgs["timeout_seconds"] = timeoutSec

	body, status, err := cl.DoWithTimeout("POST", "/mcp", map[string]interface{}{
		"method": "tools/call",
		"params": map[string]interface{}{
			"name":      "container_exec",
			"arguments": mcpArgs,
		},
	}, time.Duration(timeoutSec+15)*time.Second)
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
	cmdStr := posixJoin(args)
	body, status, err := cl.DoWithTimeout("POST", "/mcp", map[string]interface{}{
		"method": "tools/call",
		"params": map[string]interface{}{
			"name": "server_exec",
			"arguments": map[string]interface{}{
				"command": cmdStr,
			},
		},
	}, 10*time.Minute)
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

// RunEnv handles: nd env list [app_id] [--json] | nd env set [app_id] KEY=VALUE ...
func RunEnv(cl *client.Client, args []string) error {
	jsonOut, args := extractJSONFlag(args)
	if len(args) < 1 {
		return fmt.Errorf("usage: nd env list [app_id] [--json] | nd env get [app_id] [KEY] | nd env set [app_id] KEY=VALUE ...")
	}
	sub := args[0]
	rest := args[1:]

	appID, envArgs, err := resolveAppID(rest)
	if err != nil {
		return err
	}

	switch sub {
	case "get", "reveal":
		revealArgs := map[string]interface{}{"app_id": appID}
		if len(envArgs) > 0 {
			revealArgs["keys"] = envArgs
		}
		body, status, err := cl.Do("POST", "/mcp", map[string]interface{}{
			"method": "tools/call",
			"params": map[string]interface{}{
				"name":      "env_reveal",
				"arguments": revealArgs,
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
				IsError bool `json:"isError"`
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"result"`
		}
		if err := json.Unmarshal(body, &resp); err == nil && len(resp.Result.Content) > 0 {
			if resp.Result.IsError {
				return fmt.Errorf("%s", resp.Result.Content[0].Text)
			}
			var kvMap map[string]string
			if err := json.Unmarshal([]byte(resp.Result.Content[0].Text), &kvMap); err == nil {
				if jsonOut {
					pretty, _ := json.MarshalIndent(kvMap, "", "  ")
					fmt.Println(string(pretty))
					return nil
				}
				for k, v := range kvMap {
					fmt.Printf("%s=%s\n", k, v)
				}
				return nil
			}
			fmt.Println(resp.Result.Content[0].Text)
			return nil
		}
		fmt.Println(string(body))
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
				if jsonOut {
					if envData.Keys == nil {
						envData.Keys = []string{}
					}
					out := map[string]interface{}{
						"app_id": appID,
						"count":  envData.Count,
						"keys":   envData.Keys,
					}
					pretty, _ := json.MarshalIndent(out, "", "  ")
					fmt.Println(string(pretty))
					return nil
				}

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
		return fmt.Errorf("unknown env subcommand: %s (use list, get, or set)", sub)
	}
	return nil
}

// RunStop stops an app or a specific service container.
// Usage: nd stop [app_id] [service] [-s <service>]
func RunStop(cl *client.Client, args []string) error {
	service := ""
	var cleanArgs []string
	for i := 0; i < len(args); i++ {
		if (args[i] == "-s" || args[i] == "--service") && i+1 < len(args) {
			service = args[i+1]
			i++
		} else {
			cleanArgs = append(cleanArgs, args[i])
		}
	}

	appID, rest, err := resolveAppID(cleanArgs)
	if err != nil {
		return err
	}
	if service == "" && len(rest) > 0 {
		service = rest[0]
	}

	payload := map[string]interface{}{"app_id": appID}
	if service != "" {
		payload["service"] = service
	}

	body, status, err := cl.Do("POST", "/mcp", map[string]interface{}{
		"method": "tools/call",
		"params": map[string]interface{}{
			"name":      "stop",
			"arguments": payload,
		},
	})
	if err != nil {
		return err
	}
	if status >= 400 {
		return fmt.Errorf("server error: %s", client.JSONError(body))
	}

	if service != "" {
		fmt.Printf("✓ %s service %s stopped.\n", appID, ui.Cyan(service))
	} else {
		fmt.Printf("✓ %s stopped.\n", appID)
	}
	return nil
}

// RunDown stops and removes application containers.
// Usage: nd down [app_id]
func RunDown(cl *client.Client, args []string) error {
	appID, _, err := resolveAppID(args)
	if err != nil {
		return err
	}

	body, status, err := cl.Do("POST", "/mcp", map[string]interface{}{
		"method": "tools/call",
		"params": map[string]interface{}{
			"name":      "stop",
			"arguments": map[string]interface{}{"app_id": appID},
		},
	})
	if err != nil {
		return err
	}
	if status >= 400 {
		return fmt.Errorf("server error: %s", client.JSONError(body))
	}

	fmt.Printf("✓ %s containers stopped and removed.\n", appID)
	return nil
}

// RunRestart restarts an app or a specific service container.
// Usage: nd restart [app_id] [service] [-s <service>] [--recreate]
func RunRestart(cl *client.Client, args []string) error {
	service := ""
	recreate := false
	var cleanArgs []string
	for i := 0; i < len(args); i++ {
		if (args[i] == "-s" || args[i] == "--service") && i+1 < len(args) {
			service = args[i+1]
			i++
		} else if strings.HasPrefix(args[i], "--service=") {
			service = strings.TrimPrefix(args[i], "--service=")
		} else if args[i] == "--recreate" || args[i] == "-r" {
			recreate = true
		} else {
			cleanArgs = append(cleanArgs, args[i])
		}
	}

	appID, rest, err := resolveAppID(cleanArgs)
	if err != nil {
		return err
	}
	if service == "" && len(rest) > 0 {
		service = rest[0]
	}

	payload := map[string]interface{}{"app_id": appID}
	if service != "" {
		payload["service"] = service
	}
	if recreate {
		payload["recreate"] = true
	}

	body, status, err := cl.Do("POST", "/mcp", map[string]interface{}{
		"method": "tools/call",
		"params": map[string]interface{}{
			"name":      "restart",
			"arguments": payload,
		},
	})
	if err != nil {
		return err
	}
	if status >= 400 {
		return fmt.Errorf("server error: %s", client.JSONError(body))
	}

	actionStr := "restarted"
	if recreate {
		actionStr = "recreated"
	}

	if service != "" {
		fmt.Printf("✓ %s service %s %s.\n", appID, ui.Cyan(service), actionStr)
	} else {
		fmt.Printf("✓ %s %s.\n", appID, actionStr)
	}
	return nil
}

// PrintHelp prints the CLI usage.
func PrintHelp() {
	fmt.Fprintf(os.Stderr, `nd — NextDeploy CLI (PaaS Management)

Authentication & Session:
  nd login <server_url> [token]      Authenticate with an API token
  nd logout                          Remove saved credentials and disconnect session
  nd whoami [--json]                 Show authenticated server, user, and session status

App Management:
  nd apps [--json]                   List all applications
  nd create <name>                   Create and link a new application
  nd delete [app_id] [-f]            Delete an application (requires confirmation unless -f)
  nd link <app_id>                   Link current directory to an app (.nd/project.json)
  nd unlink                          Remove link from current directory
  nd info [app_id] [--json]          Show app details, domains, health, and status (alias: nd status)
  nd open [app_id]                   Open app domain or panel URL in browser

Process & Container Inspection:
  nd ps [app_id] [--json]            Show containers, services, state, status, and images
  nd containers [app_id] [-a]        List all host containers, or filter by application
  nd images [--json]                 List Docker images on the host VPS

Deployments & Lifecycle:
  nd push [app_id] [path] [--deploy] [--prune] [-y] Sync local files or single file (--deploy to auto-deploy, --prune removes server-only files)
  nd pull [app_id] [target_dir]      Download remote workspace files to local directory (preserves local .env)
  nd diff [app_id] [path]            Compare local files against remote workspace before sync
  nd deploy [app_id] [--rebuild]     Deploy application (--rebuild to pull & build image; alias: nd redeploy)
  nd stop [app_id] [service]         Stop application stack or a specific service container
  nd restart [app_id] [service] [--recreate] Restart application stack or container (--recreate forces container rebuild)
  nd down [app_id]                   Stop and remove application containers
  nd logs [app_id] [-s svc] [-n 50] [-f] Show container logs (--deploy to view deployment build logs)

Execution & Command Run:
  nd exec [flags] [app_id] <cmd...>  Run command inside container (supports -i/--stdin, -t/--timeout)
  nd server-exec <cmd...>            Run command directly on host VPS (requires allow_server_exec)

Environment Variables:
  nd env list [app_id] [--json]      List configured environment variable keys
  nd env set [app_id] KEY=VAL ...    Set or update environment variables

File & Folder Management:
  nd files [path] [-r] [--json]      List workspace files and directories (alias: nd file list, nd ls)
  nd file read <path> [--full] [-o file] View or download remote file content (alias: nd cat)
  nd file write <path> [content|file] Write/upload file to workspace (supports stdin, --from)
  nd file edit <path>                Interactively edit remote file in $EDITOR (alias: nd edit)
  nd file rm <path> [-r] [-f]        Delete remote file or directory (alias: nd rm)
  nd folder rm <path> [-f]           Delete remote directory and contents (alias: nd folder delete)

Other:
  nd update                          Update nd CLI to the latest release from GitHub
  nd completion [bash|zsh|ps1]       Generate shell autocompletion script
  nd version                         Print version
  nd help                            Print this help

Flags:
  -a, --app <app_id>                 Target application ID (overrides linked directory)
  -j, --json                         Output structured JSON (compatible with CI/CD and AI agents)
  -s, --service <service>            Target compose service for exec / logs / stop / restart
  -c, --container <container>        Target specific container name for exec
  -w, --workdir <dir>                Working directory inside container
  -t, --timeout <secs>               Execution timeout in seconds (default: 300s, max: 3600s)
  -i, --stdin                        Forward stdin to command inside container
  --recreate                         Force recreate containers during restart
  --server                           Execute on host VPS instead of container

Environment variables:
  ND_SERVER_URL                      Panel URL fallback (e.g. in CI/CD)
  ND_TOKEN                           API token fallback
`)
}

