package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"nd/internal/client"
	"nd/internal/ui"
)

type whoamiPayload struct {
	UserID             int64  `json:"user_id"`
	Username           string `json:"username"`
	Role               string `json:"role"`
	TokenName          string `json:"token_name"`
	TokenPrefix        string `json:"token_prefix"`
	AllowEnvReveal     bool   `json:"allow_env_reveal"`
	AllowServerExec    bool   `json:"allow_server_exec"`
	AllowContainerExec bool   `json:"allow_container_exec"`
	AllowAppDelete     bool   `json:"allow_app_delete"`
	ServerIP           string `json:"server_ip"`
	Quota              *struct {
		Unlimited    bool    `json:"unlimited"`
		MaxApps      int     `json:"max_apps"`
		AppsUsed     int     `json:"apps_used"`
		MaxMemoryMB  int     `json:"max_memory_mb"`
		MaxCPUs      float64 `json:"max_cpus"`
		MaxStorageMB int     `json:"max_storage_mb"`
	} `json:"quota,omitempty"`
}

func fetchWhoami(cl *client.Client) (*whoamiPayload, []byte, int, error) {
	body, status, err := cl.Do("GET", "/api/v1/cli/whoami", nil)
	if err != nil {
		return nil, body, status, err
	}
	if status >= 400 {
		return nil, body, status, fmt.Errorf("server error: %s", client.JSONError(body))
	}
	var p whoamiPayload
	if uerr := json.Unmarshal(body, &p); uerr != nil {
		return nil, body, status, uerr
	}
	return &p, body, status, nil
}

// detectRole returns "admin" or "user" using saved account config first, falling back to /api/v1/cli/whoami.
func detectRole(cl *client.Client) string {
	if cl == nil {
		return ""
	}
	if role := cl.Config().ActiveRole(); role != "" {
		return role
	}
	if p, _, status, err := fetchWhoami(cl); err == nil && status < 400 && p != nil {
		return strings.ToLower(strings.TrimSpace(p.Role))
	}
	return ""
}

// requireAdminRole fails early with a clear role message when signed in as a non-admin user.
func requireAdminRole(cl *client.Client, cmdName string) error {
	role := detectRole(cl)
	if role != "" && role != "admin" {
		return fmt.Errorf("permission denied: %s requires administrator privileges (signed in with role %q; run 'nd help user' to see user commands)", cmdName, role)
	}
	return nil
}

// RunQuota displays the authenticated user's resource limits and usage.
// Usage: nd quota [--json]
func RunQuota(cl *client.Client, args []string) error {
	jsonOut, _ := extractJSONFlag(args)
	p, _, _, err := fetchWhoami(cl)
	if err != nil {
		return err
	}
	if jsonOut {
		out := map[string]interface{}{
			"username": p.Username,
			"role":     p.Role,
			"quota":    p.Quota,
		}
		pretty, _ := json.MarshalIndent(out, "", "  ")
		fmt.Println(string(pretty))
		return nil
	}

	fmt.Printf("=== Account Quota (%s)\n", ui.Bold(p.Username))
	ui.KeyValue("Role", formatRoleBadge(p.Role))
	if p.Quota == nil || p.Quota.Unlimited || strings.EqualFold(p.Role, "admin") {
		ui.KeyValue("Apps Limit", ui.Green("Unlimited (admin)"))
		ui.KeyValue("Compute & Storage", ui.Green("Unlimited (admin)"))
		return nil
	}

	appsLimit := "unlimited"
	if p.Quota.MaxApps > 0 {
		appsLimit = fmt.Sprintf("%d / %d", p.Quota.AppsUsed, p.Quota.MaxApps)
	} else {
		appsLimit = fmt.Sprintf("%d used", p.Quota.AppsUsed)
	}
	cpuStr := "host default"
	if p.Quota.MaxCPUs > 0 {
		cpuStr = fmt.Sprintf("%.2f vCPU", p.Quota.MaxCPUs)
	}
	memStr := "host default"
	if p.Quota.MaxMemoryMB > 0 {
		memStr = fmt.Sprintf("%d MB", p.Quota.MaxMemoryMB)
	}
	diskStr := "host default"
	if p.Quota.MaxStorageMB > 0 {
		diskStr = fmt.Sprintf("%d MB", p.Quota.MaxStorageMB)
	}
	ui.KeyValue("Applications", appsLimit)
	ui.KeyValue("Max CPU", cpuStr)
	ui.KeyValue("Max Memory", memStr)
	ui.KeyValue("Max Storage", diskStr)
	return nil
}

// RunUsers lists panel users and quotas (admin only).
// Usage: nd users [list] [--json]
func RunUsers(cl *client.Client, args []string) error {
	if hasHelpFlag(args) {
		fmt.Println("Usage: nd users [list] [--json]  [admin]")
		fmt.Println("\nList all panel users, roles, account status, and resource quotas.")
		return nil
	}
	if err := requireAdminRole(cl, "nd users"); err != nil {
		return err
	}
	jsonOut, rest := extractJSONFlag(args)
	if len(rest) > 0 && rest[0] != "list" && rest[0] != "ls" {
		return fmt.Errorf("unknown users subcommand %q (usage: nd users [list] [--json])", rest[0])
	}

	body, status, err := cl.Do("GET", "/api/v1/admin/users", nil)
	if err != nil {
		return err
	}
	if status >= 400 {
		return fmt.Errorf("server error: %s", client.JSONError(body))
	}
	if jsonOut {
		fmt.Println(strings.TrimSpace(string(body)))
		return nil
	}

	var users []struct {
		ID           int64   `json:"id"`
		Username     string  `json:"username"`
		Role         string  `json:"role"`
		Status       string  `json:"status"`
		MaxApps      int     `json:"max_apps"`
		MaxMemoryMB  int     `json:"max_memory_mb"`
		MaxCPUs      float64 `json:"max_cpus"`
		MaxStorageMB int     `json:"max_storage_mb"`
	}
	if err := json.Unmarshal(body, &users); err != nil {
		return fmt.Errorf("parse error: %w", err)
	}
	if len(users) == 0 {
		fmt.Println("No users found.")
		return nil
	}

	headers := []string{"ID", "USERNAME", "ROLE", "STATUS", "APPS MAX", "QUOTA (CPU / RAM / DISK)"}
	var rows [][]string
	for _, u := range users {
		appsCol := "unlimited"
		if u.Role != "admin" {
			if u.MaxApps > 0 {
				appsCol = fmt.Sprintf("%d", u.MaxApps)
			} else {
				appsCol = "default"
			}
		}
		quotaCol := "unlimited"
		if u.Role != "admin" {
			quotaCol = fmt.Sprintf("%.1fc / %dMB / %dMB", u.MaxCPUs, u.MaxMemoryMB, u.MaxStorageMB)
		}
		rows = append(rows, []string{
			fmt.Sprintf("%d", u.ID),
			ui.Bold(u.Username),
			formatRoleBadge(u.Role),
			ui.StatePill(u.Status),
			appsCol,
			quotaCol,
		})
	}
	ui.PrintTable(os.Stdout, headers, rows)
	return nil
}

func formatRoleBadge(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "admin":
		return ui.HiYellow("admin")
	case "user":
		return ui.Cyan("user")
	default:
		if role == "" {
			return ui.Dim("unknown")
		}
		return role
	}
}
