package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"nd/internal/client"
	"nd/internal/config"
	"nd/internal/ui"

	"github.com/gorilla/websocket"
)

// Version can be overwritten at build time or by main.
var Version = "2.9.5"

// resolveHostIP returns the IPv4 address of the server host.
func resolveHostIP(serverURL string) string {
	if serverURL == "" {
		return ""
	}
	u, err := url.Parse(serverURL)
	if err != nil {
		return ""
	}
	host := u.Hostname()
	if host == "" {
		return ""
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.String()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	var r net.Resolver
	ips, err := r.LookupIP(ctx, "ip4", host)
	if err != nil || len(ips) == 0 {
		return ""
	}
	return ips[0].String()
}

// extractHostPorts extracts published host port numbers from a Docker port mapping string.
// Example: "0.0.0.0:3000->3000/tcp, :::3000->3000/tcp" -> [3000]
func extractHostPorts(portsStr string) []int {
	var ports []int
	seen := make(map[int]bool)
	parts := strings.Split(portsStr, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		idx := strings.Index(part, "->")
		if idx == -1 {
			continue
		}
		hostPart := part[:idx]
		colonIdx := strings.LastIndex(hostPart, ":")
		portStr := hostPart
		if colonIdx != -1 {
			portStr = hostPart[colonIdx+1:]
		}
		portStr = strings.TrimSpace(portStr)
		if p, err := strconv.Atoi(portStr); err == nil && p > 0 {
			if !seen[p] {
				seen[p] = true
				ports = append(ports, p)
			}
		}
	}
	return ports
}

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

// RunLogin handles: nd login <server_url> [token] [--name <alias>]
// Prompts for API token (or accepts as argument/flag), validates, saves to ~/.nd/config.json
func RunLogin(args []string) error {
	var (
		serverURL   string
		token       string
		accountName string
		cleanArgs   []string
	)

	// Extract flags like --name / -n and --token / -t
	for i := 0; i < len(args); i++ {
		a := args[i]
		if (a == "--name" || a == "-n") && i+1 < len(args) {
			accountName = strings.TrimSpace(args[i+1])
			i++
		} else if strings.HasPrefix(a, "--name=") {
			accountName = strings.TrimSpace(strings.TrimPrefix(a, "--name="))
		} else if strings.HasPrefix(a, "-n=") {
			accountName = strings.TrimSpace(strings.TrimPrefix(a, "-n="))
		} else if (a == "--token" || a == "-t") && i+1 < len(args) {
			token = strings.TrimSpace(args[i+1])
			i++
		} else if strings.HasPrefix(a, "--token=") {
			token = strings.TrimSpace(strings.TrimPrefix(a, "--token="))
		} else {
			cleanArgs = append(cleanArgs, a)
		}
	}

	if len(cleanArgs) >= 1 {
		serverURL = strings.TrimRight(cleanArgs[0], "/")
	}
	if serverURL == "" {
		return fmt.Errorf("usage: nd login <server_url> [token] [--name <alias>]")
	}

	if token == "" && len(cleanArgs) >= 2 {
		token = strings.TrimSpace(cleanArgs[1])
	}

	if token == "" {
		fmt.Print(ui.Bold("API Token") + " (generate in panel under Automation -> CLI Sessions): ")
		if _, err := fmt.Scanln(&token); err != nil {
			return fmt.Errorf("failed to read token: %w", err)
		}
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return fmt.Errorf("token cannot be empty")
	}

	// Validate token by querying server whoami or apps
	cfg, _ := config.Load()
	tempCfg := config.Config{ServerURL: serverURL, Token: token}
	tempCfg.EnsureDeviceID()
	c := client.New(tempCfg, tempCfg.DeviceID, Version)

	var whoamiData struct {
		Username string `json:"username"`
		Role     string `json:"role"`
	}
	wBody, wStatus, wErr := c.Do("GET", "/api/v1/cli/whoami", nil)
	if wErr != nil {
		return fmt.Errorf("could not reach server: %w", wErr)
	}
	if wStatus == 401 {
		return fmt.Errorf("invalid token: authentication failed")
	}
	if wStatus < 400 {
		_ = json.Unmarshal(wBody, &whoamiData)
	} else {
		// Fallback for older servers
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
	}

	// Auto-generate account name if not explicitly provided
	if accountName == "" {
		parsedHost := serverURL
		if u, err := url.Parse(serverURL); err == nil && u.Host != "" {
			parsedHost = u.Host
		} else {
			parsedHost = strings.TrimPrefix(parsedHost, "https://")
			parsedHost = strings.TrimPrefix(parsedHost, "http://")
		}
		if whoamiData.Username != "" {
			accountName = fmt.Sprintf("%s@%s", whoamiData.Username, parsedHost)
		} else {
			accountName = parsedHost
		}
	}

	_, alreadyExists := cfg.Accounts[accountName]

	cfg.SetAccount(accountName, config.AccountConfig{
		ServerURL: serverURL,
		Token:     token,
		DeviceID:  cfg.EnsureDeviceID(),
		Username:  whoamiData.Username,
		Role:      whoamiData.Role,
	}, true)

	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("could not save config: %w", err)
	}

	// Register device heartbeat immediately on login
	hostname, _ := os.Hostname()
	_ = c.HeartbeatSync(hostname, runtime.GOOS, runtime.GOARCH, Version)

	roleLabel := ""
	if whoamiData.Role != "" {
		roleLabel = fmt.Sprintf(" [%s]", whoamiData.Role)
	}
	if alreadyExists {
		ui.Success("Updated credentials for account %s%s (%s)", ui.Bold(accountName), roleLabel, ui.Cyan(serverURL))
	} else {
		ui.Success("Logged in as %s%s (%s)", ui.Bold(accountName), roleLabel, ui.Cyan(serverURL))
	}
	return nil
}

// RunLogout clears saved credentials. Supports:
// nd logout                 (logs out active account)
// nd logout <account_name>  (logs out specific account)
// nd logout --all           (clears all accounts)
func RunLogout(args []string) error {
	allFlag := false
	var targetAccount string
	for _, a := range args {
		if a == "--all" || a == "-a" {
			allFlag = true
		} else if !strings.HasPrefix(a, "-") && targetAccount == "" {
			targetAccount = strings.TrimSpace(a)
		}
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	if allFlag {
		if cfg.ServerURL != "" && cfg.DeviceID != "" {
			c := client.New(cfg, cfg.DeviceID, Version)
			c.Disconnect()
		}
		if err := config.Save(config.Config{}); err != nil {
			return err
		}
		ui.Success("Logged out of all accounts.")
		return nil
	}

	if targetAccount == "" {
		targetAccount = cfg.CurrentAccount
	}
	if targetAccount == "" {
		ui.Warn("Not logged in.")
		return nil
	}

	// If logging out the active account, attempt graceful session disconnect
	if targetAccount == cfg.CurrentAccount && cfg.ServerURL != "" && cfg.DeviceID != "" {
		c := client.New(cfg, cfg.DeviceID, Version)
		c.Disconnect()
	}

	newActive, err := cfg.DeleteAccount(targetAccount)
	if err != nil {
		return err
	}
	if err := config.Save(cfg); err != nil {
		return err
	}

	if newActive != "" {
		ui.Success("Logged out of account %s. Switched active account to %s.", ui.Bold(targetAccount), ui.Cyan(newActive))
	} else {
		ui.Success("Logged out of account %s. No remaining accounts.", ui.Bold(targetAccount))
	}
	return nil
}

// RunAccount manages multiple NextDeploy accounts (list, switch, rename, delete, current).
func RunAccount(args []string) error {
	jsonOut, args := extractJSONFlag(args)
	subcmd := "list"
	if len(args) > 0 {
		subcmd = args[0]
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	switch subcmd {
	case "list":
		if jsonOut {
			type accountInfo struct {
				Name      string `json:"name"`
				Active    bool   `json:"active"`
				ServerURL string `json:"server_url"`
				Username  string `json:"username,omitempty"`
				Role      string `json:"role,omitempty"`
				DeviceID  string `json:"device_id,omitempty"`
			}
			var list []accountInfo
			var names []string
			for n := range cfg.Accounts {
				names = append(names, n)
			}
			sort.Strings(names)
			for _, n := range names {
				acc := cfg.Accounts[n]
				list = append(list, accountInfo{
					Name:      n,
					Active:    n == cfg.CurrentAccount,
					ServerURL: acc.ServerURL,
					Username:  acc.Username,
					Role:      acc.Role,
					DeviceID:  acc.DeviceID,
				})
			}
			if list == nil {
				list = []accountInfo{}
			}
			pretty, _ := json.MarshalIndent(list, "", "  ")
			fmt.Println(string(pretty))
			return nil
		}

		if len(cfg.Accounts) == 0 {
			fmt.Println(ui.Dim("No saved accounts found. Log in with: nd login <server_url>"))
			return nil
		}

		headers := []string{"Active", "Name", "User", "Role", "Server URL", "Device ID"}
		var rows [][]string
		var names []string
		for n := range cfg.Accounts {
			names = append(names, n)
		}
		sort.Strings(names)

		for _, n := range names {
			acc := cfg.Accounts[n]
			activeMarker := " "
			nameDisplay := n
			if n == cfg.CurrentAccount {
				activeMarker = ui.Green("✓")
				nameDisplay = ui.Bold(n)
			}
			userDisplay := acc.Username
			if userDisplay == "" {
				userDisplay = ui.Dim("-")
			}
			deviceDisplay := acc.DeviceID
			if len(deviceDisplay) > 16 {
				deviceDisplay = deviceDisplay[:14] + ".."
			}
			rows = append(rows, []string{
				activeMarker,
				nameDisplay,
				userDisplay,
				formatRoleBadge(acc.Role),
				ui.Cyan(acc.ServerURL),
				ui.Dim(deviceDisplay),
			})
		}
		ui.PrintTable(os.Stdout, headers, rows)
		return nil

	case "switch", "use":
		if len(args) < 2 {
			return fmt.Errorf("usage: nd account switch <name>")
		}
		target := strings.TrimSpace(args[1])
		if err := cfg.SwitchAccount(target); err != nil {
			return err
		}
		if err := config.Save(cfg); err != nil {
			return fmt.Errorf("failed to save config: %w", err)
		}
		ui.Success("Switched to account %s (%s)", ui.Bold(target), ui.Cyan(cfg.ServerURL))
		return nil

	case "rename":
		if len(args) < 3 {
			return fmt.Errorf("usage: nd account rename <old_name> <new_name>")
		}
		oldName := strings.TrimSpace(args[1])
		newName := strings.TrimSpace(args[2])
		if err := cfg.RenameAccount(oldName, newName); err != nil {
			return err
		}
		if err := config.Save(cfg); err != nil {
			return fmt.Errorf("failed to save config: %w", err)
		}
		ui.Success("Renamed account %s to %s", ui.Bold(oldName), ui.Bold(newName))
		return nil

	case "delete", "remove", "rm":
		if len(args) < 2 {
			return fmt.Errorf("usage: nd account delete <name> [-f]")
		}
		target := strings.TrimSpace(args[1])
		force := false
		for _, a := range args[2:] {
			if a == "-f" || a == "--force" || a == "-y" {
				force = true
			}
		}
		if !force {
			fmt.Printf("Delete account %q? (y/N): ", target)
			var confirm string
			_, _ = fmt.Scanln(&confirm)
			confirm = strings.ToLower(strings.TrimSpace(confirm))
			if confirm != "y" && confirm != "yes" {
				return fmt.Errorf("account deletion cancelled")
			}
		}
		newActive, err := cfg.DeleteAccount(target)
		if err != nil {
			return err
		}
		if err := config.Save(cfg); err != nil {
			return fmt.Errorf("failed to save config: %w", err)
		}
		if newActive != "" {
			ui.Success("Deleted account %s. Switched active account to %s.", ui.Bold(target), ui.Cyan(newActive))
		} else {
			ui.Success("Deleted account %s. No remaining accounts.", ui.Bold(target))
		}
		return nil

	case "current":
		if jsonOut {
			curName, curAcc, ok := cfg.ActiveAccount()
			if !ok {
				fmt.Println("{}")
				return nil
			}
			outMap := map[string]interface{}{
				"name":       curName,
				"server_url": curAcc.ServerURL,
				"username":   curAcc.Username,
				"role":       curAcc.Role,
				"device_id":  curAcc.DeviceID,
			}
			if vpsIP := resolveHostIP(curAcc.ServerURL); vpsIP != "" {
				outMap["vps_ip"] = vpsIP
			}
			pretty, _ := json.MarshalIndent(outMap, "", "  ")
			fmt.Println(string(pretty))
			return nil
		}
		curName, curAcc, ok := cfg.ActiveAccount()
		if !ok {
			fmt.Println(ui.Dim("No active account. Log in with: nd login <server_url>"))
			return nil
		}
		ui.KeyValue("Account", ui.Bold(curName))
		ui.KeyValue("Server URL", ui.Cyan(curAcc.ServerURL))
		if vpsIP := resolveHostIP(curAcc.ServerURL); vpsIP != "" {
			ui.KeyValue("VPS IP", ui.Bold(vpsIP))
		}
		if curAcc.Username != "" {
			ui.KeyValue("User", fmt.Sprintf("%s (%s)", curAcc.Username, formatRoleBadge(curAcc.Role)))
		}
		if curAcc.DeviceID != "" {
			ui.KeyValue("Device ID", ui.Dim(curAcc.DeviceID))
		}
		return nil

	default:
		return fmt.Errorf("unknown account subcommand %q. Available: list, switch, rename, delete, current", subcmd)
	}
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

	body, status, err := cl.Do("GET", "/api/v1/apps", nil)
	if err != nil {
		return err
	}
	if status >= 400 {
		return fmt.Errorf("server error: %s", client.JSONError(body))
	}

	var apps []struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(body, &apps); err != nil {
		fmt.Println(string(body))
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

	body, status, err := cl.Do("POST", "/api/v1/apps", payload)
	if err != nil {
		return err
	}
	if status >= 400 {
		return fmt.Errorf("failed to create app: %s", client.JSONError(body))
	}

	var resData struct {
		AppID  string `json:"app_id"`
		Name   string `json:"name"`
		Status string `json:"status"`
		Error  string `json:"error"`
	}
	if err := json.Unmarshal(body, &resData); err != nil {
		return fmt.Errorf("unexpected server response: %s", string(body))
	}
	if resData.Error != "" {
		return fmt.Errorf("failed: %s", resData.Error)
	}

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

	body, status, err := cl.Do("DELETE", fmt.Sprintf("/api/v1/apps/%s", url.PathEscape(appID)), nil)
	if err != nil {
		return err
	}
	if status >= 400 {
		return fmt.Errorf("failed to delete app: %s", client.JSONError(body))
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
	var whoamiData whoamiPayload

	wBody, wStatus, wErr := cl.Do("GET", "/api/v1/cli/whoami", nil)
	authOK := wErr == nil && wStatus < 400
	if authOK {
		_ = json.Unmarshal(wBody, &whoamiData)
	} else {
		// Fallback for older server versions: test auth via app list
		body, status, err := cl.Do("GET", "/api/v1/apps", nil)
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
		if vpsIP := resolveHostIP(cfg.ServerURL); vpsIP != "" {
			out["vps_ip"] = vpsIP
		}
		if cfg.CurrentAccount != "" {
			out["account"] = cfg.CurrentAccount
		}
		if whoamiData.Username != "" {
			out["username"] = whoamiData.Username
			out["role"] = whoamiData.Role
		}
		if whoamiData.Quota != nil {
			out["quota"] = whoamiData.Quota
		}
		if whoamiData.TokenName != "" {
			out["token_name"] = whoamiData.TokenName
			out["token_prefix"] = whoamiData.TokenPrefix
			out["permissions"] = map[string]bool{
				"env_reveal":     whoamiData.AllowEnvReveal,
				"server_exec":    whoamiData.AllowServerExec,
				"container_exec": whoamiData.AllowContainerExec,
				"app_delete":     whoamiData.AllowAppDelete,
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

	if cfg.CurrentAccount != "" {
		ui.KeyValue("Account", fmt.Sprintf("%s %s", ui.Bold(cfg.CurrentAccount), ui.Green("(active)")))
	}
	ui.KeyValue("Server URL", ui.Cyan(cfg.ServerURL))
	if vpsIP := resolveHostIP(cfg.ServerURL); vpsIP != "" {
		ui.KeyValue("VPS IP", ui.Bold(vpsIP))
	}
	if whoamiData.Username != "" {
		ui.KeyValue("User", fmt.Sprintf("%s (%s)", ui.Bold(whoamiData.Username), formatRoleBadge(whoamiData.Role)))
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
		if whoamiData.AllowServerExec && strings.EqualFold(whoamiData.Role, "admin") {
			perms = append(perms, ui.Yellow("server_exec"))
		}
		if whoamiData.AllowEnvReveal {
			perms = append(perms, ui.Yellow("env_reveal"))
		}
		if whoamiData.AllowAppDelete {
			perms = append(perms, ui.Yellow("app_delete"))
		}
		if len(perms) == 0 {
			perms = append(perms, ui.Dim("standard"))
		}
		ui.KeyValue("Permissions", strings.Join(perms, ", "))
	}
	if whoamiData.Quota != nil && !whoamiData.Quota.Unlimited && whoamiData.Quota.MaxApps > 0 {
		ui.KeyValue("App Quota", fmt.Sprintf("%d / %d apps", whoamiData.Quota.AppsUsed, whoamiData.Quota.MaxApps))
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

	body, status, err := cl.Do("GET", fmt.Sprintf("/api/v1/apps/%s?include_health=true", url.PathEscape(appID)), nil)
	if err != nil {
		return err
	}
	if status >= 400 {
		return fmt.Errorf("server error: %s", client.JSONError(body))
	}

	if jsonOut {
		var raw interface{}
		if err := json.Unmarshal(body, &raw); err == nil {
			pretty, _ := json.MarshalIndent(raw, "", "  ")
			fmt.Println(string(pretty))
		} else {
			fmt.Println(string(body))
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
		Ports   string `json:"Ports,omitempty"`
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
	if err := json.Unmarshal(body, &data); err != nil {
		fmt.Println(string(body))
		return nil
	}

	created := data.App.CreatedAt
	if t, err := time.Parse(time.RFC3339, created); err == nil {
		created = t.Format("Jan 02, 2006 15:04")
	}

	vpsIP := resolveHostIP(cl.ServerURL())

	fmt.Printf("\n=== %s (%s)\n", ui.HiCyan(data.App.Name), ui.Dim(data.App.ID))
	ui.KeyValue("Status", ui.StatePill(data.App.Status))
	ui.KeyValue("Created", created)
	if vpsIP != "" {
		ui.KeyValue("VPS IP", ui.Bold(vpsIP))
	}

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
		if vpsIP != "" {
			fmt.Printf("  %s %s %s\n", ui.Dim("└─ Point your DNS A-record to"), ui.Bold(vpsIP), ui.Dim("then run: nd domain add <app> <domain>"))
		}
	}

	// Direct Access URLs if containers have published host ports
	var directURLs []string
	if vpsIP != "" {
		for _, p := range data.PS {
			for _, port := range extractHostPorts(p.Ports) {
				directURLs = append(directURLs, fmt.Sprintf("http://%s:%d %s", vpsIP, port, ui.Dim(fmt.Sprintf("(service: %s)", p.Service))))
			}
		}
	}
	if len(directURLs) > 0 {
		fmt.Printf("\n%s\n", ui.Bold("Direct Access:"))
		for _, u := range directURLs {
			fmt.Printf("  • %s\n", ui.Cyan(u))
		}
	}

	// Containers
	fmt.Printf("\n%s (%d):\n", ui.Bold("Containers"), len(data.PS))
	if len(data.PS) > 0 {
		headers := []string{"Service", "Container", "Ports", "State", "Status"}
		var rows [][]string
		for _, p := range data.PS {
			portsDisplay := p.Ports
			if portsDisplay == "" {
				portsDisplay = ui.Dim("-")
			}
			rows = append(rows, []string{
				ui.Bold(p.Service),
				p.Name,
				portsDisplay,
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

	body, status, err := cl.Do("GET", fmt.Sprintf("/api/v1/apps/%s?include_health=true", url.PathEscape(appID)), nil)
	if err != nil {
		return err
	}
	if status >= 400 {
		return fmt.Errorf("server error: %s", client.JSONError(body))
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
	if err := json.Unmarshal(body, &data); err != nil {
		fmt.Println(string(body))
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
		cBody, cStatus, cErr := cl.Do("GET", "/api/v1/apps/"+url.PathEscape(a.ID), nil)
		if cErr != nil || cStatus >= 400 {
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
		if json.Unmarshal(cBody, &data) != nil {
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

	if detectRole(cl) != "admin" {
		return runAllPS(cl, jsonOut)
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
	if detectRole(cl) != "admin" {
		return runAppImages(cl, jsonOut)
	}
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
		cBody, cStatus, cErr := cl.Do("GET", fmt.Sprintf("/api/v1/apps/%s/compose", url.PathEscape(a.ID)), nil)
		if cErr != nil || cStatus >= 400 {
			continue
		}
		var cResp struct {
			Content string `json:"content"`
		}
		if err := json.Unmarshal(cBody, &cResp); err != nil || cResp.Content == "" {
			continue
		}

		lines := strings.Split(cResp.Content, "\n")
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
	body, status, err := cl.Do("POST", "/api/v1/system/exec", map[string]interface{}{
		"command": cmdStr,
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

	body, status, err := cl.Do("GET", "/api/v1/apps/"+url.PathEscape(appID), nil)
	if err != nil || status >= 400 {
		return fmt.Errorf("failed to get app info: %s", client.JSONError(body))
	}

	targetURL := ""
	var data struct {
		Domains []struct {
			Domain      string `json:"Domain"`
			EnableHTTPS bool   `json:"EnableHTTPS"`
		} `json:"domains"`
		PS []struct {
			Ports string `json:"Ports"`
		} `json:"ps"`
	}
	_ = json.Unmarshal(body, &data)
	if len(data.Domains) > 0 {
		d := data.Domains[0]
		scheme := "http"
		if d.EnableHTTPS {
			scheme = "https"
		}
		targetURL = fmt.Sprintf("%s://%s", scheme, d.Domain)
	}

	// If no domain mapped, attempt direct port access on the VPS host
	if targetURL == "" {
		if vpsIP := resolveHostIP(cl.ServerURL()); vpsIP != "" {
			for _, p := range data.PS {
				if ports := extractHostPorts(p.Ports); len(ports) > 0 {
					targetURL = fmt.Sprintf("http://%s:%d", vpsIP, ports[0])
					break
				}
			}
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

func parseDeployFlags(args []string) (rebuild bool, force bool, cleanArgs []string) {
	for _, a := range args {
		if a == "--rebuild" || a == "-r" {
			rebuild = true
		} else if a == "--force" || a == "-f" {
			force = true
		} else {
			cleanArgs = append(cleanArgs, a)
		}
	}
	return rebuild, force, cleanArgs
}

// RunDeploy triggers a redeploy for an app.
func RunDeploy(cl *client.Client, args []string) error {
	jsonOut, args := extractJSONFlag(args)
	rebuild, force, cleanArgs := parseDeployFlags(args)

	appID, _, err := resolveAppID(cleanArgs)
	if err != nil {
		return err
	}
	if !jsonOut {
		if rebuild && force {
			ui.Step("Rebuilding & force-deploying %s (pulling latest from Git)...", ui.Cyan(appID))
		} else if force {
			ui.Step("Force-deploying %s (pulling latest from Git)...", ui.Cyan(appID))
		} else if rebuild {
			ui.Step("Rebuilding & deploying %s...", ui.Cyan(appID))
		} else {
			ui.Step("Deploying %s...", ui.Cyan(appID))
		}
	}

	deployArgs := map[string]interface{}{"wait_seconds": 120}
	if rebuild {
		deployArgs["rebuild"] = true
	}
	if force {
		deployArgs["git_pull"] = true
	}

	body, status, err := cl.DoWithTimeout("POST", fmt.Sprintf("/api/v1/apps/%s/deploy", url.PathEscape(appID)), deployArgs, 3*time.Minute)
	if err != nil {
		return err
	}
	if status >= 400 {
		return fmt.Errorf("deploy error: %s", client.JSONError(body))
	}

	var data struct {
		JobID          string  `json:"job_id"`
		AppID          string  `json:"app_id"`
		Action         string  `json:"action"`
		OK             bool    `json:"ok"`
		Status         string  `json:"status"`
		DurationS      float64 `json:"duration_s"`
		OutputTail     string  `json:"output_tail"`
		Message        string  `json:"message"`
		GitSyncWarning string  `json:"git_sync_warning"`
		Error          string  `json:"error"`
	}
	_ = json.Unmarshal(body, &data)

	deployStatus := "success"
	if data.Error != "" || (!data.OK && data.Status != "started") {
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
		if data.GitSyncWarning != "" {
			out["git_sync_warning"] = data.GitSyncWarning
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

	if data.GitSyncWarning != "" {
		ui.Warn("%s", data.GitSyncWarning)
	}

	if deployStatus == "failed" {
		if data.OutputTail != "" {
			fmt.Println(data.OutputTail)
		}
		return fmt.Errorf("deployment failed after %.0fs (Job ID: %s)", data.DurationS, data.JobID)
	}

	if deployStatus == "timeout" {
		if data.OutputTail != "" {
			fmt.Println(data.OutputTail)
		}
		return fmt.Errorf("deployment is still running after the wait window (Job ID: %s); it was not confirmed. Follow it with: nd logs --deploy -f", data.JobID)
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
	isErrorLog := false
	service := ""
	var cleanArgs []string

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "-f" || arg == "--follow" {
			follow = true
		} else if arg == "--error" || arg == "-e" {
			isErrorLog = true
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

	// Real-time zero-latency WebSocket streaming when follow is requested, or when error logs are requested
	if follow || isErrorLog {
		q := url.Values{}
		q.Set("tail", strconv.Itoa(lines))
		if service != "" {
			q.Set("container", service)
		}
		if isErrorLog {
			q.Set("error", "true")
		}
		if follow {
			q.Set("follow", "true")
		} else {
			q.Set("follow", "false")
		}
		ws, _, wsErr := cl.DialWebSocket("/api/v1/apps/"+appID+"/ws/logs", q)
		if wsErr == nil {
			defer ws.Close()
			if !follow {
				_ = ws.SetReadDeadline(time.Now().Add(1500 * time.Millisecond))
			}
			for {
				msgType, data, rerr := ws.ReadMessage()
				if rerr != nil {
					return nil
				}
				if msgType == websocket.TextMessage || msgType == websocket.BinaryMessage {
					os.Stdout.Write(data)
				}
				if !follow {
					return nil
				}
			}
		}
		if isErrorLog {
			return fmt.Errorf("could not connect to error log stream: %w", wsErr)
		}
		// If WS dial fails for container follow, fall through to polling
	}

	fetchLogs := func(tailCount int) (string, error) {
		q := fmt.Sprintf("?tail=%d&lines=%d", tailCount, tailCount)
		if service != "" {
			q += "&service=" + url.QueryEscape(service)
		}
		body, status, err := cl.Do("GET", fmt.Sprintf("/api/v1/apps/%s/logs%s", url.PathEscape(appID), q), nil)
		if err != nil {
			return "", err
		}
		if status >= 400 {
			return "", fmt.Errorf("server error: %s", client.JSONError(body))
		}
		var resp struct {
			Logs string `json:"logs"`
		}
		if err := json.Unmarshal(body, &resp); err == nil {
			return resp.Logs, nil
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
		apiURL := fmt.Sprintf("/api/v1/apps/%s/deploy-logs?since_offset=%d&wait_seconds=%d",
			url.PathEscape(appID), offset, waitSec)
		body, status, doErr := cl.Do("GET", apiURL, nil)
		if doErr != nil {
			return "", offset, false, doErr
		}
		if status >= 400 {
			return "", offset, false, fmt.Errorf("server error: %s", client.JSONError(body))
		}

		// Check if active job streaming response
		var streamData struct {
			JobID      string `json:"job_id"`
			Action     string `json:"action"`
			Running    bool   `json:"running"`
			NextOffset int    `json:"next_offset"`
			NewOutput  string `json:"new_output"`
		}
		if json.Unmarshal(body, &streamData) == nil && streamData.JobID != "" {
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
		if json.Unmarshal(body, &snapData) == nil {
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
		stdinFlag   bool
		interactive bool
		isServer    bool
		cmdTokens   []string
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
		} else if arg == "-it" || arg == "-ti" || arg == "--interactive" {
			interactive = true
			i++
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

	if interactive {
		return runPTYSession(cl, "/api/v1/apps/"+appID+"/ws/terminal", service, strings.Join(cmdTokens, " "))
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

	execArgs := map[string]interface{}{
		"command": cmdStr,
		"args":    cmdTokens,
	}
	if service != "" {
		execArgs["service"] = service
	}
	if workDir != "" {
		execArgs["work_dir"] = workDir
	}
	if stdinData != "" {
		execArgs["stdin"] = stdinData
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
	execArgs["timeout_seconds"] = timeoutSec

	body, status, err := cl.DoWithTimeout("POST", fmt.Sprintf("/api/v1/apps/%s/exec", url.PathEscape(appID)), execArgs, time.Duration(timeoutSec+15)*time.Second)
	if err != nil {
		return fmt.Errorf("failed to execute: %w", err)
	}
	if status >= 400 {
		return fmt.Errorf("server error (%d): %s", status, client.JSONError(body))
	}

	return parseAndPrintExecResult(body)
}

// RunServerExec executes a command directly on the panel host server (admin + allow_server_exec required).
// Usage: nd server-exec <command...>
func RunServerExec(cl *client.Client, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: nd server-exec <command...>")
	}
	if err := requireAdminRole(cl, "nd server-exec"); err != nil {
		return err
	}
	cmdStr := posixJoin(args)
	body, status, err := cl.DoWithTimeout("POST", "/api/v1/system/exec", map[string]interface{}{
		"command": cmdStr,
	}, 10*time.Minute)
	if err != nil {
		return fmt.Errorf("failed to call server-exec: %w", err)
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
	var res struct {
		AppID     string `json:"app_id"`
		Container string `json:"container"`
		Service   string `json:"service"`
		Command   string `json:"command"`
		OK        bool   `json:"ok"`
		ExitCode  int    `json:"exit_code"`
		Output    string `json:"output"`
		Error     string `json:"error"`
	}

	if err := json.Unmarshal(body, &res); err != nil {
		rawText := string(body)
		fmt.Print(rawText)
		if !strings.HasSuffix(rawText, "\n") {
			fmt.Println()
		}
		return nil
	}
	if res.Error != "" {
		return fmt.Errorf("%s", res.Error)
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

// resolveEnvAppAndArgs resolves (appID, envArgs) taking linked project context and env subcommand into account.
func resolveEnvAppAndArgs(sub string, args []string) (string, []string, error) {
	var appFlag string
	var posArgs []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if (a == "-a" || a == "--app") && i+1 < len(args) {
			appFlag = args[i+1]
			i++
		} else if strings.HasPrefix(a, "--app=") {
			appFlag = strings.TrimPrefix(a, "--app=")
		} else if strings.HasPrefix(a, "-a=") {
			appFlag = strings.TrimPrefix(a, "-a=")
		} else {
			posArgs = append(posArgs, a)
		}
	}

	if appFlag != "" {
		return appFlag, posArgs, nil
	}

	pc, pcErr := config.LoadProject(".")
	hasLinked := pcErr == nil && pc.AppID != ""

	if hasLinked {
		switch sub {
		case "list", "ls":
			if len(posArgs) == 0 {
				return pc.AppID, nil, nil
			}
			return posArgs[0], posArgs[1:], nil
		case "set":
			if len(posArgs) > 0 && strings.Contains(posArgs[0], "=") {
				return pc.AppID, posArgs, nil
			}
			if len(posArgs) > 0 {
				return posArgs[0], posArgs[1:], nil
			}
			return pc.AppID, nil, nil
		case "unset", "rm", "delete":
			if len(posArgs) > 0 && posArgs[0] == pc.AppID {
				return pc.AppID, posArgs[1:], nil
			}
			return pc.AppID, posArgs, nil
		case "get", "reveal":
			if len(posArgs) > 0 && posArgs[0] == pc.AppID {
				return pc.AppID, posArgs[1:], nil
			}
			return pc.AppID, posArgs, nil
		}
	}

	if len(posArgs) == 0 {
		return "", nil, fmt.Errorf("app_id required (pass --app <id>, specify as argument, or run 'nd link <id>')")
	}
	return posArgs[0], posArgs[1:], nil
}

// RunEnv handles: nd env list [app_id] [--json] | nd env set [app_id] KEY=VALUE ...
func RunEnv(cl *client.Client, args []string) error {
	jsonOut, args := extractJSONFlag(args)
	if len(args) < 1 {
		return fmt.Errorf("usage: nd env list [app_id] [--json] | nd env get [app_id] [KEY] | nd env set [app_id] KEY=VALUE ... | nd env unset [app_id] KEY ...")
	}
	sub := args[0]
	rest := args[1:]

	appID, envArgs, err := resolveEnvAppAndArgs(sub, rest)
	if err != nil {
		return err
	}

	switch sub {
	case "get", "reveal":
		q := "?reveal=true"
		if len(envArgs) > 0 {
			q += "&keys=" + url.QueryEscape(strings.Join(envArgs, ","))
		}
		body, status, err := cl.Do("GET", fmt.Sprintf("/api/v1/apps/%s/env%s", url.PathEscape(appID), q), nil)
		if err != nil {
			return err
		}
		if status >= 400 {
			return fmt.Errorf("server error: %s", client.JSONError(body))
		}
		var envResp struct {
			Env   map[string]string `json:"env"`
			Error string            `json:"error"`
		}
		if err := json.Unmarshal(body, &envResp); err == nil && envResp.Env != nil {
			if jsonOut {
				pretty, _ := json.MarshalIndent(envResp.Env, "", "  ")
				fmt.Println(string(pretty))
				return nil
			}
			for k, v := range envResp.Env {
				fmt.Printf("%s=%s\n", k, v)
			}
			return nil
		}
		fmt.Println(string(body))
	case "list":
		body, status, err := cl.Do("GET", fmt.Sprintf("/api/v1/apps/%s/env", url.PathEscape(appID)), nil)
		if err != nil {
			return err
		}
		if status >= 400 {
			return fmt.Errorf("server error: %s", client.JSONError(body))
		}

		var envData struct {
			Keys  []string `json:"keys"`
			Count int      `json:"count"`
		}
		if err := json.Unmarshal(body, &envData); err == nil {
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
		body, status, err := cl.Do("POST", fmt.Sprintf("/api/v1/apps/%s/env", url.PathEscape(appID)), map[string]interface{}{
			"variables": vars,
		})
		if err != nil {
			return err
		}
		if status >= 400 {
			return fmt.Errorf("server error: %s", client.JSONError(body))
		}
		fmt.Println("✓ Environment updated.")

	case "unset", "rm", "delete":
		if len(envArgs) < 1 {
			return fmt.Errorf("usage: nd env unset [app_id] KEY [KEY2 ...]")
		}
		body, status, err := cl.Do("POST", fmt.Sprintf("/api/v1/apps/%s/env/unset", url.PathEscape(appID)), map[string]interface{}{
			"keys": envArgs,
		})
		if err != nil {
			return err
		}
		if status >= 400 {
			return fmt.Errorf("server error: %s", client.JSONError(body))
		}
		if len(envArgs) == 1 {
			fmt.Printf("✓ Environment variable %q removed.\n", envArgs[0])
		} else {
			fmt.Printf("✓ %d environment variable(s) removed.\n", len(envArgs))
		}

	default:
		return fmt.Errorf("unknown env subcommand: %s (use list, get, set, or unset)", sub)
	}
	return nil
}

// RunDomain handles custom domain management:
//   nd domain list [app_id] [--json]
//   nd domain add [app_id] <domain> [-s service] [-p port] [--no-https] [--www]
//   nd domain delete [app_id] <domain|id>
func RunDomain(cl *client.Client, args []string) error {
	jsonOut, cleanArgs := extractJSONFlag(args)
	if len(cleanArgs) == 0 {
		appID, _, err := resolveAppID(nil)
		if err == nil && appID != "" {
			return runDomainList(cl, appID, jsonOut)
		}
		return fmt.Errorf("usage: nd domain list [app_id] [--json] | nd domain add [app_id] <domain> [flags] | nd domain rm [app_id] <domain|id>")
	}

	sub := strings.ToLower(cleanArgs[0])
	subArgs := cleanArgs[1:]

	switch sub {
	case "list", "ls":
		appID, _, err := resolveAppID(subArgs)
		if err != nil {
			return err
		}
		return runDomainList(cl, appID, jsonOut)

	case "add", "new", "create":
		var (
			appFlag     string
			service     string
			port        = 80
			enableHTTPS = true
			enableWWW   = false
			shieldID    *int64
			direct      bool
			posArgs     []string
		)

		for i := 0; i < len(subArgs); i++ {
			a := subArgs[i]
			if (a == "-a" || a == "--app") && i+1 < len(subArgs) {
				appFlag = subArgs[i+1]
				i++
			} else if strings.HasPrefix(a, "--app=") || strings.HasPrefix(a, "-a=") {
				appFlag = a[strings.Index(a, "=")+1:]
			} else if (a == "-s" || a == "--service") && i+1 < len(subArgs) {
				service = subArgs[i+1]
				i++
			} else if strings.HasPrefix(a, "--service=") || strings.HasPrefix(a, "-s=") {
				service = a[strings.Index(a, "=")+1:]
			} else if (a == "-p" || a == "--port") && i+1 < len(subArgs) {
				i++
				if p, err := strconv.Atoi(subArgs[i]); err == nil && p > 0 && p <= 65535 {
					port = p
				}
			} else if strings.HasPrefix(a, "--port=") || strings.HasPrefix(a, "-p=") {
				if p, err := strconv.Atoi(a[strings.Index(a, "=")+1:]); err == nil && p > 0 && p <= 65535 {
					port = p
				}
			} else if a == "--no-https" || a == "--https=false" {
				enableHTTPS = false
			} else if a == "--https" || a == "--https=true" {
				enableHTTPS = true
			} else if a == "--www" || a == "--www=true" {
				enableWWW = true
			} else if a == "--no-www" || a == "--www=false" {
				enableWWW = false
			} else if a == "--shield" && i+1 < len(subArgs) {
				id, err := strconv.ParseInt(strings.TrimSpace(subArgs[i+1]), 10, 64)
				if err != nil || id <= 0 {
					return fmt.Errorf("invalid --shield node ID %q: must be a positive integer", subArgs[i+1])
				}
				shieldID = &id
				i++
			} else if strings.HasPrefix(a, "--shield=") {
				val := strings.TrimSpace(strings.TrimPrefix(a, "--shield="))
				id, err := strconv.ParseInt(val, 10, 64)
				if err != nil || id <= 0 {
					return fmt.Errorf("invalid --shield node ID %q: must be a positive integer", val)
				}
				shieldID = &id
			} else if a == "--direct" {
				direct = true
			} else if strings.HasPrefix(a, "-") {
				return fmt.Errorf("unknown flag: %s", a)
			} else {
				posArgs = append(posArgs, a)
			}
		}

		if shieldID != nil && direct {
			return fmt.Errorf("cannot specify both --shield and --direct")
		}

		var appID, domain string
		if appFlag != "" {
			appID = appFlag
			if len(posArgs) < 1 {
				return fmt.Errorf("domain required: nd domain add -a %s <domain>", appID)
			}
			domain = posArgs[0]
		} else {
			if len(posArgs) >= 2 {
				appID = posArgs[0]
				domain = posArgs[1]
			} else if len(posArgs) == 1 {
				linked, _, err := resolveAppID(nil)
				if err == nil && linked != "" {
					appID = linked
					domain = posArgs[0]
				} else {
					return fmt.Errorf("app_id required: nd domain add <app_id> <domain> (or link directory with 'nd link <app_id>')")
				}
			} else {
				return fmt.Errorf("domain required: nd domain add [app_id] <domain> [--service <svc>] [--port <port>] [--shield <node_id> | --direct]")
			}
		}

		payload := map[string]interface{}{
			"app_id":       appID,
			"domain":       domain,
			"port":         port,
			"enable_https": enableHTTPS,
			"enable_www":   enableWWW,
		}
		if service != "" {
			payload["service"] = service
		}
		if shieldID != nil {
			payload["shield_node_id"] = *shieldID
		} else if direct {
			payload["shield_node_id"] = int64(0)
			payload["direct"] = true
		}

		body, status, err := cl.Do("POST", fmt.Sprintf("/api/v1/apps/%s/domains", url.PathEscape(appID)), payload)
		if err != nil {
			return err
		}
		if status >= 400 {
			return fmt.Errorf("server error: %s", client.JSONError(body))
		}

		if jsonOut {
			fmt.Println(string(body))
			return nil
		}

		var res struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(body, &res) == nil && res.Message != "" {
			ui.Success("%s", res.Message)
		} else {
			ui.Success("Domain %s added to %s.", ui.Cyan(domain), ui.Cyan(appID))
		}
		if vpsIP := resolveHostIP(cl.ServerURL()); vpsIP != "" {
			fmt.Printf("  %s %s\n", ui.Dim("DNS check: Ensure your domain's A-record points to"), ui.Bold(vpsIP))
		}
		return nil

	case "delete", "del", "rm", "remove":
		var (
			appFlag string
			posArgs []string
		)
		for i := 0; i < len(subArgs); i++ {
			a := subArgs[i]
			if (a == "-a" || a == "--app") && i+1 < len(subArgs) {
				appFlag = subArgs[i+1]
				i++
			} else if strings.HasPrefix(a, "--app=") || strings.HasPrefix(a, "-a=") {
				appFlag = a[strings.Index(a, "=")+1:]
			} else if strings.HasPrefix(a, "-") {
				return fmt.Errorf("unknown flag: %s", a)
			} else {
				posArgs = append(posArgs, a)
			}
		}

		var appID, target string
		if appFlag != "" {
			appID = appFlag
			if len(posArgs) < 1 {
				return fmt.Errorf("domain or domain_id required: nd domain rm -a %s <domain|id>", appID)
			}
			target = posArgs[0]
		} else {
			if len(posArgs) >= 2 {
				appID = posArgs[0]
				target = posArgs[1]
			} else if len(posArgs) == 1 {
				linked, _, err := resolveAppID(nil)
				if err == nil && linked != "" {
					appID = linked
					target = posArgs[0]
				} else {
					return fmt.Errorf("app_id required: nd domain rm <app_id> <domain|id> (or link directory with 'nd link <app_id>')")
				}
			} else {
				return fmt.Errorf("domain or domain_id required: nd domain rm [app_id] <domain|id>")
			}
		}

		body, status, err := cl.Do("DELETE", fmt.Sprintf("/api/v1/apps/%s/domains?domain=%s", url.PathEscape(appID), url.QueryEscape(target)), nil)
		if err != nil {
			return err
		}
		if status >= 400 {
			return fmt.Errorf("server error: %s", client.JSONError(body))
		}

		if jsonOut {
			fmt.Println(string(body))
			return nil
		}

		var res struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(body, &res) == nil && res.Message != "" {
			ui.Success("%s", res.Message)
		} else {
			ui.Success("Domain %s removed from %s.", ui.Cyan(target), ui.Cyan(appID))
		}
		return nil

	default:
		return fmt.Errorf("unknown domain subcommand: %s (use list, add, or rm)", sub)
	}
}

func runDomainList(cl *client.Client, appID string, jsonOut bool) error {
	body, status, err := cl.Do("GET", fmt.Sprintf("/api/v1/apps/%s/domains", url.PathEscape(appID)), nil)
	if err != nil {
		return err
	}
	if status >= 400 {
		return fmt.Errorf("server error: %s", client.JSONError(body))
	}

	if jsonOut {
		var raw interface{}
		if json.Unmarshal(body, &raw) == nil {
			pretty, _ := json.MarshalIndent(raw, "", "  ")
			fmt.Println(string(pretty))
			return nil
		}
		fmt.Println(string(body))
		return nil
	}

	type domainItem struct {
		ID          int64  `json:"id"`
		AppID       string `json:"app_id"`
		Domain      string `json:"domain"`
		Service     string `json:"service"`
		Port        int    `json:"port"`
		EnableHTTPS bool   `json:"enable_https"`
		EnableWWW   bool   `json:"enable_www"`
		CreatedAt   string `json:"created_at"`
	}

	var items []domainItem
	if err := json.Unmarshal(body, &items); err != nil {
		fmt.Println(string(body))
		return nil
	}

	fmt.Printf("=== %s Domains (%d)\n", appID, len(items))
	if len(items) == 0 {
		fmt.Println("  (no custom domains configured)")
		return nil
	}

	for _, d := range items {
		httpsStatus := "https: on"
		if !d.EnableHTTPS {
			httpsStatus = "https: off"
		}
		wwwStatus := ""
		if d.EnableWWW {
			wwwStatus = ", www: on"
		}
		fmt.Printf("  • %s -> %s:%d (%s%s) [ID: %d]\n", ui.Cyan(d.Domain), d.Service, d.Port, httpsStatus, wwwStatus, d.ID)
	}
	return nil
}

// resolveAppAndService resolves (appID, service) taking linked project into account.
func resolveAppAndService(args []string, explicitService string) (string, string, error) {
	var appFlag string
	var posArgs []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if (a == "-a" || a == "--app") && i+1 < len(args) {
			appFlag = args[i+1]
			i++
		} else if strings.HasPrefix(a, "--app=") {
			appFlag = strings.TrimPrefix(a, "--app=")
		} else if strings.HasPrefix(a, "-a=") {
			appFlag = strings.TrimPrefix(a, "-a=")
		} else {
			posArgs = append(posArgs, a)
		}
	}

	service := explicitService

	// 1. If explicit --app flag was passed:
	if appFlag != "" {
		if service == "" && len(posArgs) > 0 {
			service = posArgs[0]
		}
		return appFlag, service, nil
	}

	// 2. Check for linked project in current directory
	pc, pcErr := config.LoadProject(".")
	hasLinked := pcErr == nil && pc.AppID != ""

	if hasLinked {
		if len(posArgs) == 0 {
			// 'nd stop' -> stop linked app
			return pc.AppID, service, nil
		}
		if len(posArgs) == 1 {
			// If service was already provided via -s, single positional arg is appID override
			if service != "" {
				return posArgs[0], service, nil
			}
			// In linked project context, a single positional arg is interpreted as service name
			return pc.AppID, posArgs[0], nil
		}
		// 2 or more positional args: 'nd stop other-app redis'
		return posArgs[0], posArgs[1], nil
	}

	// 3. Not in linked project: first positional argument is app ID
	if len(posArgs) == 0 {
		return "", "", fmt.Errorf("app_id required (pass --app <id>, specify as argument, or run 'nd link <id>')")
	}
	appID := posArgs[0]
	if service == "" && len(posArgs) > 1 {
		service = posArgs[1]
	}
	return appID, service, nil
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
		} else if strings.HasPrefix(args[i], "--service=") {
			service = strings.TrimPrefix(args[i], "--service=")
		} else {
			cleanArgs = append(cleanArgs, args[i])
		}
	}

	appID, service, err := resolveAppAndService(cleanArgs, service)
	if err != nil {
		return err
	}

	payload := map[string]interface{}{}
	if service != "" {
		payload["service"] = service
	}

	body, status, err := cl.Do("POST", fmt.Sprintf("/api/v1/apps/%s/stop", url.PathEscape(appID)), payload)
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

	body, status, err := cl.Do("POST", fmt.Sprintf("/api/v1/apps/%s/stop", url.PathEscape(appID)), map[string]interface{}{})
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

	appID, service, err := resolveAppAndService(cleanArgs, service)
	if err != nil {
		return err
	}

	payload := map[string]interface{}{}
	if service != "" {
		payload["service"] = service
	}
	if recreate {
		payload["recreate"] = true
	}

	body, status, err := cl.Do("POST", fmt.Sprintf("/api/v1/apps/%s/restart", url.PathEscape(appID)), payload)
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

// PrintHelp prints the complete CLI usage with User and Admin sections cleanly separated.
func PrintHelp() {
	PrintHelpSection("")
}

// PrintHelpSection prints CLI help filtered by "user", "admin", or all ("").
func PrintHelpSection(section string) {
	showUser := section == "" || section == "user" || section == "users"
	showAdmin := section == "" || section == "admin"
	if !showUser && !showAdmin {
		showUser, showAdmin = true, true
	}

	fmt.Fprintf(os.Stderr, "nd - NextDeploy CLI (PaaS Management)\n\n")
	fmt.Fprintf(os.Stderr, `Authentication & Account (All Roles):
  nd login <server_url> [token] [--name alias] Authenticate with an API token (saves role & alias)
  nd logout [name] [--all]           Log out of active or specified account (--all clears all)
  nd whoami [--json]                 Show active account, role, quota, token scopes & session
  nd quota [--json]                  Show application, CPU, memory & storage quota limits
  nd account list|switch|rename|rm|current Manage saved accounts (alias: nd account)
  nd help [user|admin]               Show full help, or filter to user / admin commands

`)

	if showUser {
		fmt.Fprintf(os.Stderr, `================ USER COMMANDS (Own & Collaborator Apps) ================

App Management:
  nd apps [--json]                   List accessible applications
  nd create <name>                   Create and link a new application
  nd delete [app_id] [-f]            Delete your application (requires allow_app_delete on token)
  nd git [cmd] [app_id]              Git integration (status, on/off, providers, repos, branches, webhooks, connect, sync, push, disconnect)
  nd link <app_id>                   Link current directory to an app (.nd/project.json)
  nd unlink                          Remove link from current directory
  nd info [app_id] [--json]          Show app details, domains, health, and status (alias: nd status)
  nd open [app_id]                   Open app domain or panel URL in browser

Deployments & Containers:
  nd push [app_id] [path] [--deploy] [--prune] [-y] Sync workspace files (--deploy builds/starts)
  nd pull [app_id] [target_dir]      Download remote workspace files (preserves local .env)
  nd diff [app_id] [path]            Compare local files against remote workspace before sync
  nd deploy [app_id] [--rebuild] [-f/--force] Deploy app (--rebuild rebuilds image, -f/--force pulls Git)
  nd stop [app_id] [service]         Stop application stack or a specific service container
  nd restart [app_id] [service] [--recreate] Restart stack or container (--recreate rebuilds container)
  nd down [app_id]                   Stop and remove application containers
  nd ps [app_id] [--json]            Show containers, services, states, and images
  nd logs [app_id] [-s svc] [-n 50] [-f] Stream container logs (--deploy for build logs)
  nd exec [flags] [app_id] <cmd...>  Run command inside container (requires allow_container_exec)
  nd shell [app_id] [-c container]   Open interactive TTY shell into app container or PHP runtime

Domains, Env, Files & Personal Proxy Routes:
  nd domain list|add|rm [app_id]     Manage app custom domains (--service, --port, --www)
  nd env list|get|set|unset [app_id] Manage environment variables (get requires allow_env_reveal)
  nd files [path] [-r] [--json]      List workspace files (aliases: nd file list, nd ls)
  nd file read|write|edit|rm <path>  Read, write, interactively edit, or delete workspace files
  nd folder rm <path> [-f]           Delete workspace folder recursively
  nd proxy list|add|enable|disable|toggle|rm Manage your personal reverse proxy & redirect routes

PHP Hosting & Databases:
  nd php list                        List accessible PHP, WordPress, and static sites
  nd php create <name> [domain] [flags] Create a PHP/WordPress/static site (--domain, --no-default-domain, --db)
  nd php info|set [app_id]           Inspect or update PHP version, memory & upload limits
  nd php start|stop|restart [app_id] Control PHP/static site status and proxy route
  nd php rules list|scan|fallback|add|import|export|rm Manage URL routing & .htaccess import
  nd php import-manifest <file.json> Batch provision sites, domains, DBs & rules
  nd php fix-perms [app_id]          Repair htdocs ownership & POSIX permissions
  nd php pma [app_id]                Generate one-click auto-login SSO URL for phpMyAdmin
  nd php db list|create|grant|user   Manage MariaDB databases, users & privileges (alias: nd db)
  nd php cron list|add|rm|run        Manage scheduled PHP cron jobs

Volumes, Backups & Activity:
  nd volume list|browse|download|restore|rm Manage Docker volumes owned by your apps
  nd backup list|create|log|restore|schedule|dest Manage backups for your apps and personal destinations
  nd shield list [--json]            View available Edge Shield nodes for domain routing
  nd audit [-n 25]                   View audit trail for your account and accessible apps

`)
	}

	if showAdmin {
		fmt.Fprintf(os.Stderr, `================ ADMIN COMMANDS (Host & Infrastructure) =================

Host VPS & Global Inspection [admin]:
  nd containers [-a] [--json]        List ALL host Docker containers (users see only own apps)
  nd images [--json]                 List ALL host Docker images (users see only own app images)
  nd top / stats                     Host CPU, RAM, disk & container telemetry
  nd clean [--all]                   Prune stopped containers, unused images & build cache
  nd volume prune                    Prune unused host Docker volumes
  nd server-exec <cmd...>            Run command on host VPS (admin + allow_server_exec)
  nd server-shell                    Open interactive admin host shell on VPS
  nd audit [-n 25]                   View full panel-wide security and operation audit trail
  nd users [list] [--json]           List all panel users, roles, status & resource quotas

Reverse Proxy Engine & Edge Shield [admin]:
  nd proxy reload|logs|status|options|engine Control global proxy engine (caddy, angie, ferron)
  nd shield add|update|rm|rotate-token|test|setup-script|reassign Manage Edge Shield nodes
  nd edge <subcommand>               Manage multi-panel Edge Shield daemon on an Edge VPS (nd edge --help)

`)
	}

	fmt.Fprintf(os.Stderr, `Other:
  nd update                          Update nd CLI to the latest GitHub release
  nd completion [bash|zsh|ps1]       Generate shell autocompletion script
  nd version                         Print version

Global Flags & Env:
  -A, --account <name>               Target specific saved account
  -a, --app <app_id>                 Target application ID (overrides .nd/project.json)
  -j, --json                         Output structured JSON
  ND_ACCOUNT / ND_SERVER_URL / ND_TOKEN Environment overrides
`)
}

