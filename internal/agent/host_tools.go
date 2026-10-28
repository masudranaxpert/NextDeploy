package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"panel/internal/agent/omprpc"
	"panel/internal/db"
	"panel/internal/sysinfo"
)

var domainPattern = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$`)

const (
	maxToolOutput = 64 << 10 // bytes returned to the model per tool call
	bashTimeout   = 2 * time.Minute
)

// tailFile returns at most maxBytes from the end of path without reading the whole file.
func tailFile(path string, maxBytes int64) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return "", err
	}
	off := int64(0)
	if fi.Size() > maxBytes {
		off = fi.Size() - maxBytes
	}
	if _, err := f.Seek(off, io.SeekStart); err != nil {
		return "", err
	}
	b, err := io.ReadAll(f)
	return string(b), err
}

func truncateOutput(s string) string {
	if len(s) > maxToolOutput {
		return "...[truncated]...\n" + s[len(s)-maxToolOutput:]
	}
	return s
}

func (m *Manager) buildHostTools() []omprpc.HostTool {
	tools := append(m.coreHostTools(), m.extraHostTools()...)
	for i := range tools {
		// Essential: the model sees each tool directly instead of having to
		// discover it through omp's xd:// URL indirection (fewer calls and tokens).
		tools[i].Definition.LoadMode = omprpc.Ptr(omprpc.ToolLoadModeEssential)
	}
	return tools
}

func (m *Manager) coreHostTools() []omprpc.HostTool {
	return []omprpc.HostTool{
		m.toolListSites(),
		m.toolGetSiteDetails(),
		m.toolAddDomain(),
		m.toolRemoveDomain(),
		m.toolDiagnosePerformance(),
		m.toolUpdatePHPConfig(),
		m.toolRestartSite(),
		m.toolGetSiteLogs(),
		m.toolCheckEdgeShield(),
		m.toolGetServerResources(),
		m.toolExecuteBash(),
	}
}

// lookupSite validates app_id (no path separators) and loads the PHP site.
func (m *Manager) lookupSite(ctx context.Context, appID string) (db.PHPSite, error) {
	if !ValidAppID(appID) {
		return db.PHPSite{}, fmt.Errorf("invalid app_id")
	}
	return m.DB.GetPHPSite(ctx, appID)
}

// 1. list_sites
func (m *Manager) toolListSites() omprpc.HostTool {
	return omprpc.HostTool{
		Definition: omprpc.HostToolDefinition{
			Name:        "list_sites",
			Description: "Lists all deployed PHP/WordPress and Docker applications in NextDeploy.",
			Parameters: map[string]json.RawMessage{
				"type":       json.RawMessage(`"object"`),
				"properties": json.RawMessage(`{}`),
			},
		},
		Handler: func(ctx context.Context, call *omprpc.HostToolCall, args json.RawMessage) (omprpc.HostToolResultPayload, error) {
			sites, err := m.DB.ListPHPSites(ctx)
			if err != nil {
				return omprpc.TextResult(fmt.Sprintf("Failed to list PHP sites: %v", err)), nil
			}

			type siteItem struct {
				AppID      string   `json:"app_id"`
				SiteType   string   `json:"type"`
				PHPVersion string   `json:"php_version"`
				Domains    []string `json:"domains"`
			}

			var items []siteItem
			for _, s := range sites {
				domains, _ := m.DB.ListAppDomains(ctx, s.AppID)
				var dNames []string
				for _, d := range domains {
					dNames = append(dNames, d.Domain)
				}
				items = append(items, siteItem{
					AppID:      s.AppID,
					SiteType:   s.SiteType,
					PHPVersion: s.PHPVersion,
					Domains:    dNames,
				})
			}

			data, _ := json.MarshalIndent(items, "", "  ")
			return omprpc.TextResult(string(data)), nil
		},
	}
}

// 2. get_site_details
func (m *Manager) toolGetSiteDetails() omprpc.HostTool {
	return omprpc.HostTool{
		Definition: omprpc.HostToolDefinition{
			Name:        "get_site_details",
			Description: "Retrieves complete configuration, domains, and settings for a given site by its app_id or name.",
			Parameters: map[string]json.RawMessage{
				"type": json.RawMessage(`"object"`),
				"properties": json.RawMessage(`{
					"app_id": {"type": "string", "description": "The site ID or name"}
				}`),
				"required": json.RawMessage(`["app_id"]`),
			},
		},
		Handler: func(ctx context.Context, call *omprpc.HostToolCall, args json.RawMessage) (omprpc.HostToolResultPayload, error) {
			var p struct {
				AppID string `json:"app_id"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return omprpc.TextResult("Invalid arguments"), err
			}

			site, err := m.lookupSite(ctx, p.AppID)
			if err != nil {
				return omprpc.TextResult(fmt.Sprintf("Site not found: %s", p.AppID)), nil
			}

			domains, _ := m.DB.ListAppDomains(ctx, site.AppID)
			res := map[string]any{
				"app_id":        site.AppID,
				"owner_id":      site.OwnerID,
				"uid":           site.UID,
				"site_type":     site.SiteType,
				"php_version":   site.PHPVersion,
				"root_dir":      site.RootDir,
				"settings":      site.SettingsJSON,
				"domains":       domains,
				"workspace_dir": filepath.Join(m.Root, site.AppID),
			}
			data, _ := json.MarshalIndent(res, "", "  ")
			return omprpc.TextResult(string(data)), nil
		},
	}
}

// 3. add_domain
func (m *Manager) toolAddDomain() omprpc.HostTool {
	return omprpc.HostTool{
		Definition: omprpc.HostToolDefinition{
			Name:        "add_domain",
			Description: "Adds a custom domain to a site in NextDeploy and syncs Caddy On-Demand TLS.",
			Parameters: map[string]json.RawMessage{
				"type": json.RawMessage(`"object"`),
				"properties": json.RawMessage(`{
					"app_id": {"type": "string", "description": "Site ID"},
					"domain": {"type": "string", "description": "Domain name (e.g. app.example.com)"}
				}`),
				"required": json.RawMessage(`["app_id", "domain"]`),
			},
		},
		Handler: func(ctx context.Context, call *omprpc.HostToolCall, args json.RawMessage) (omprpc.HostToolResultPayload, error) {
			var p struct {
				AppID  string `json:"app_id"`
				Domain string `json:"domain"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return omprpc.TextResult("Invalid arguments"), err
			}

			domain := strings.TrimSpace(strings.ToLower(p.Domain))
			if !domainPattern.MatchString(domain) {
				return omprpc.TextResult(fmt.Sprintf("Invalid domain name: %q", p.Domain)), nil
			}
			if _, err := m.lookupSite(ctx, p.AppID); err != nil {
				return omprpc.TextResult(fmt.Sprintf("Site not found: %s", p.AppID)), nil
			}
			if !m.requestApproval(ctx, "add_domain", "Add domain", fmt.Sprintf("Attach %s to site %s and reload Caddy?", domain, p.AppID)) {
				return omprpc.TextResult("Action declined by the user."), nil
			}
			id, err := m.DB.CreateAppDomain(ctx, db.AppDomain{
				AppID:  p.AppID,
				Domain: domain,
			})
			if err != nil {
				return omprpc.TextResult(fmt.Sprintf("Failed to add domain: %v", err)), nil
			}

			// Trigger Caddy route synchronization
			if m.PHP != nil {
				_ = m.PHP.SyncSite(ctx, p.AppID)
				m.PHP.ReloadCaddy(ctx)
			}

			return omprpc.TextResult(fmt.Sprintf("Domain '%s' added successfully with ID %d. Caddy TLS synchronized.", domain, id)), nil
		},
	}
}

// 4. remove_domain
func (m *Manager) toolRemoveDomain() omprpc.HostTool {
	return omprpc.HostTool{
		Definition: omprpc.HostToolDefinition{
			Name:        "remove_domain",
			Description: "Removes a domain from a site in NextDeploy.",
			Parameters: map[string]json.RawMessage{
				"type": json.RawMessage(`"object"`),
				"properties": json.RawMessage(`{
					"app_id": {"type": "string", "description": "Site ID"},
					"domain": {"type": "string", "description": "Domain to remove"}
				}`),
				"required": json.RawMessage(`["app_id", "domain"]`),
			},
		},
		Handler: func(ctx context.Context, call *omprpc.HostToolCall, args json.RawMessage) (omprpc.HostToolResultPayload, error) {
			var p struct {
				AppID  string `json:"app_id"`
				Domain string `json:"domain"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return omprpc.TextResult("Invalid arguments"), err
			}

			if _, err := m.lookupSite(ctx, p.AppID); err != nil {
				return omprpc.TextResult(fmt.Sprintf("Site not found: %s", p.AppID)), nil
			}
			domains, err := m.DB.ListAppDomains(ctx, p.AppID)
			if err != nil {
				return omprpc.TextResult(fmt.Sprintf("Failed to list domains: %v", err)), nil
			}
			if !m.requestApproval(ctx, "remove_domain", "Remove domain", fmt.Sprintf("Remove %s from site %s?", p.Domain, p.AppID)) {
				return omprpc.TextResult("Action declined by the user."), nil
			}

			found := false
			for _, d := range domains {
				if strings.EqualFold(d.Domain, p.Domain) {
					_ = m.DB.DeleteAppDomain(ctx, d.ID)
					found = true
					break
				}
			}

			if !found {
				return omprpc.TextResult(fmt.Sprintf("Domain '%s' not found on site '%s'", p.Domain, p.AppID)), nil
			}

			if m.PHP != nil {
				_ = m.PHP.SyncSite(ctx, p.AppID)
				m.PHP.ReloadCaddy(ctx)
			}

			return omprpc.TextResult(fmt.Sprintf("Domain '%s' successfully removed.", p.Domain)), nil
		},
	}
}

// 5. diagnose_site_performance
func (m *Manager) toolDiagnosePerformance() omprpc.HostTool {
	return omprpc.HostTool{
		Definition: omprpc.HostToolDefinition{
			Name:        "diagnose_site_performance",
			Description: "Analyzes PHP-FPM slow logs, error logs, memory limits, and container runtime stats to diagnose performance bottlenecks.",
			Parameters: map[string]json.RawMessage{
				"type": json.RawMessage(`"object"`),
				"properties": json.RawMessage(`{
					"app_id": {"type": "string", "description": "Site ID"}
				}`),
				"required": json.RawMessage(`["app_id"]`),
			},
		},
		Handler: func(ctx context.Context, call *omprpc.HostToolCall, args json.RawMessage) (omprpc.HostToolResultPayload, error) {
			var p struct {
				AppID string `json:"app_id"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return omprpc.TextResult("Invalid arguments"), err
			}

			site, err := m.lookupSite(ctx, p.AppID)
			if err != nil {
				return omprpc.TextResult(fmt.Sprintf("Site not found: %s", p.AppID)), nil
			}

			// Read logs and settings
			var slowLogs, errLogs string
			logDir := filepath.Join(m.Root, site.AppID, "logs")
			slowLogs, _ = tailFile(filepath.Join(logDir, "php-fpm-slow.log"), 2000)
			errLogs, _ = tailFile(filepath.Join(logDir, "php-error.log"), 2000)

			diag := map[string]any{
				"site":         site.AppID,
				"version":      site.PHPVersion,
				"settings":     site.SettingsJSON,
				"slow_queries": slowLogs,
				"error_log":    errLogs,
			}
			data, _ := json.MarshalIndent(diag, "", "  ")
			return omprpc.TextResult(string(data)), nil
		},
	}
}

// 6. update_php_config
func (m *Manager) toolUpdatePHPConfig() omprpc.HostTool {
	return omprpc.HostTool{
		Definition: omprpc.HostToolDefinition{
			Name:        "update_php_config",
			Description: "Updates PHP runtime pool settings such as memory_limit, upload_max_filesize, max_execution_time.",
			Parameters: map[string]json.RawMessage{
				"type": json.RawMessage(`"object"`),
				"properties": json.RawMessage(`{
					"app_id": {"type": "string", "description": "Site ID"},
					"memory_limit": {"type": "string", "description": "e.g. 256M, 512M"},
					"max_execution_time": {"type": "string", "description": "seconds, e.g. 120"},
					"upload_max_filesize": {"type": "string", "description": "e.g. 64M, 128M"}
				}`),
				"required": json.RawMessage(`["app_id"]`),
			},
		},
		Handler: func(ctx context.Context, call *omprpc.HostToolCall, args json.RawMessage) (omprpc.HostToolResultPayload, error) {
			var p struct {
				AppID             string `json:"app_id"`
				MemoryLimit       string `json:"memory_limit"`
				MaxExecutionTime  string `json:"max_execution_time"`
				UploadMaxFilesize string `json:"upload_max_filesize"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return omprpc.TextResult("Invalid arguments"), err
			}

			site, err := m.lookupSite(ctx, p.AppID)
			if err != nil {
				return omprpc.TextResult(fmt.Sprintf("Site not found: %s", p.AppID)), nil
			}

			sizeRe := regexp.MustCompile(`^\d{1,5}[KMG]?$`)
			numRe := regexp.MustCompile(`^\d{1,5}$`)
			if (p.MemoryLimit != "" && p.MemoryLimit != "-1" && !sizeRe.MatchString(p.MemoryLimit)) ||
				(p.UploadMaxFilesize != "" && !sizeRe.MatchString(p.UploadMaxFilesize)) ||
				(p.MaxExecutionTime != "" && !numRe.MatchString(p.MaxExecutionTime)) {
				return omprpc.TextResult("Invalid value: use sizes like 256M and whole seconds like 120."), nil
			}
			if !m.requestApproval(ctx, "update_php_config", "Update PHP settings", fmt.Sprintf("Change PHP limits for %s (memory_limit=%q, max_execution_time=%q, upload_max_filesize=%q)?", site.AppID, p.MemoryLimit, p.MaxExecutionTime, p.UploadMaxFilesize)) {
				return omprpc.TextResult("Action declined by the user."), nil
			}

			var currentSettings map[string]any
			if site.SettingsJSON != "" {
				_ = json.Unmarshal([]byte(site.SettingsJSON), &currentSettings)
			}
			if currentSettings == nil {
				currentSettings = make(map[string]any)
			}

			if p.MemoryLimit != "" {
				currentSettings["memory_limit"] = p.MemoryLimit
			}
			if p.MaxExecutionTime != "" {
				currentSettings["max_execution_time"] = p.MaxExecutionTime
			}
			if p.UploadMaxFilesize != "" {
				currentSettings["upload_max_filesize"] = p.UploadMaxFilesize
			}

			newJSON, _ := json.Marshal(currentSettings)
			site.SettingsJSON = string(newJSON)
			err = m.DB.UpdatePHPSite(ctx, site)
			if err != nil {
				return omprpc.TextResult(fmt.Sprintf("Failed to update site settings: %v", err)), nil
			}

			if m.PHP != nil {
				_ = m.PHP.SyncSite(ctx, site.AppID)
			}

			return omprpc.TextResult(fmt.Sprintf("PHP settings updated for %s: %s", site.AppID, string(newJSON))), nil
		},
	}
}

// 7. restart_site
func (m *Manager) toolRestartSite() omprpc.HostTool {
	return omprpc.HostTool{
		Definition: omprpc.HostToolDefinition{
			Name:        "restart_site",
			Description: "Reloads PHP-FPM or restarts the container for a site.",
			Parameters: map[string]json.RawMessage{
				"type": json.RawMessage(`"object"`),
				"properties": json.RawMessage(`{
					"app_id": {"type": "string", "description": "Site ID"}
				}`),
				"required": json.RawMessage(`["app_id"]`),
			},
		},
		Handler: func(ctx context.Context, call *omprpc.HostToolCall, args json.RawMessage) (omprpc.HostToolResultPayload, error) {
			var p struct {
				AppID string `json:"app_id"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return omprpc.TextResult("Invalid arguments"), err
			}

			site, err := m.lookupSite(ctx, p.AppID)
			if err != nil {
				return omprpc.TextResult(fmt.Sprintf("Site not found: %s", p.AppID)), nil
			}

			if !m.requestApproval(ctx, "restart_site", "Reload site", fmt.Sprintf("Re-sync and reload site %s?", site.AppID)) {
				return omprpc.TextResult("Action declined by the user."), nil
			}
			if m.PHP != nil {
				_ = m.PHP.SyncSite(ctx, site.AppID)
				m.PHP.ReloadCaddy(ctx)
			}

			return omprpc.TextResult(fmt.Sprintf("Site '%s' reloaded successfully.", site.AppID)), nil
		},
	}
}

// 8. get_site_logs
func (m *Manager) toolGetSiteLogs() omprpc.HostTool {
	return omprpc.HostTool{
		Definition: omprpc.HostToolDefinition{
			Name:        "get_site_logs",
			Description: "Retrieves recent isolated runtime and error logs for a site.",
			Parameters: map[string]json.RawMessage{
				"type": json.RawMessage(`"object"`),
				"properties": json.RawMessage(`{
					"app_id": {"type": "string", "description": "Site ID"},
					"lines": {"type": "number", "description": "Number of lines (default 100)"}
				}`),
				"required": json.RawMessage(`["app_id"]`),
			},
		},
		Handler: func(ctx context.Context, call *omprpc.HostToolCall, args json.RawMessage) (omprpc.HostToolResultPayload, error) {
			var p struct {
				AppID string `json:"app_id"`
				Lines int    `json:"lines"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return omprpc.TextResult("Invalid arguments"), err
			}
			if p.Lines <= 0 {
				p.Lines = 100
			}

			if _, err := m.lookupSite(ctx, p.AppID); err != nil {
				return omprpc.TextResult(fmt.Sprintf("Site not found: %s", p.AppID)), nil
			}
			if p.Lines > 2000 {
				p.Lines = 2000
			}
			logDir := filepath.Join(m.Root, p.AppID, "logs")
			var out strings.Builder
			for _, file := range []string{"php-error.log", "php-fpm-slow.log", "caddy-access.log"} {
				path := filepath.Join(logDir, file)
				if tail, err := tailFile(path, maxToolOutput); err == nil && len(tail) > 0 {
					out.WriteString(fmt.Sprintf("=== %s ===\n", file))
					lines := strings.Split(tail, "\n")
					start := 0
					if len(lines) > p.Lines {
						start = len(lines) - p.Lines
					}
					out.WriteString(strings.Join(lines[start:], "\n"))
					out.WriteString("\n\n")
				}
			}

			if out.Len() == 0 {
				return omprpc.TextResult(fmt.Sprintf("No logs found for site '%s'", p.AppID)), nil
			}
			return omprpc.TextResult(truncateOutput(out.String())), nil
		},
	}
}

// 9. check_edge_shield
func (m *Manager) toolCheckEdgeShield() omprpc.HostTool {
	return omprpc.HostTool{
		Definition: omprpc.HostToolDefinition{
			Name:        "check_edge_shield",
			Description: "Inspects status of all registered Swiss Edge Shield nodes, verifying ping, BBR, and Caddy catch-all proxy.",
			Parameters: map[string]json.RawMessage{
				"type":       json.RawMessage(`"object"`),
				"properties": json.RawMessage(`{}`),
			},
		},
		Handler: func(ctx context.Context, call *omprpc.HostToolCall, args json.RawMessage) (omprpc.HostToolResultPayload, error) {
			nodes, err := m.DB.ListShieldNodes(ctx)
			if err != nil {
				return omprpc.TextResult(fmt.Sprintf("Failed to list shield nodes: %v", err)), nil
			}

			data, _ := json.MarshalIndent(nodes, "", "  ")
			return omprpc.TextResult(string(data)), nil
		},
	}
}

// 10. get_server_resources
func (m *Manager) toolGetServerResources() omprpc.HostTool {
	return omprpc.HostTool{
		Definition: omprpc.HostToolDefinition{
			Name:        "get_server_resources",
			Description: "Returns current host system metrics: CPU count, memory usage, 5GB Swap usage, swappiness, and disk space.",
			Parameters: map[string]json.RawMessage{
				"type":       json.RawMessage(`"object"`),
				"properties": json.RawMessage(`{}`),
			},
		},
		Handler: func(ctx context.Context, call *omprpc.HostToolCall, args json.RawMessage) (omprpc.HostToolResultPayload, error) {
			snap := sysinfo.Collect(ctx)
			res := map[string]any{
				"cpu_count":     snap.NumCPU,
				"cpu_percent":   snap.CPUPercent,
				"mem_total_gb":  snap.MemTotalGB,
				"mem_used_gb":   snap.MemUsedGB,
				"mem_used_pct":  snap.MemUsedPct,
				"disk_total_gb": snap.DiskTotalGB,
				"disk_used_gb":  snap.DiskUsedGB,
				"disk_used_pct": snap.DiskUsedPct,
				"public_ip":     snap.PublicIP,
			}
			data, _ := json.MarshalIndent(res, "", "  ")
			return omprpc.TextResult(string(data)), nil
		},
	}
}

// 11. execute_bash
func (m *Manager) toolExecuteBash() omprpc.HostTool {
	return omprpc.HostTool{
		Definition: omprpc.HostToolDefinition{
			Name:        "execute_bash",
			Description: "Executes a shell command in a site's workspace directory (e.g. wp-cli, composer, npm).",
			Parameters: map[string]json.RawMessage{
				"type": json.RawMessage(`"object"`),
				"properties": json.RawMessage(`{
					"app_id": {"type": "string", "description": "Site ID"},
					"command": {"type": "string", "description": "Shell command to run"}
				}`),
				"required": json.RawMessage(`["app_id", "command"]`),
			},
		},
		Handler: func(ctx context.Context, call *omprpc.HostToolCall, args json.RawMessage) (omprpc.HostToolResultPayload, error) {
			var p struct {
				AppID   string `json:"app_id"`
				Command string `json:"command"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return omprpc.TextResult("Invalid arguments"), err
			}

			site, err := m.lookupSite(ctx, p.AppID)
			if err != nil {
				return omprpc.TextResult(fmt.Sprintf("Site not found: %s", p.AppID)), nil
			}

			if strings.TrimSpace(p.Command) == "" {
				return omprpc.TextResult("Empty command"), nil
			}
			if !m.requestApproval(ctx, "execute_bash", "Run shell command", fmt.Sprintf("Run in %s:\n%s", site.AppID, p.Command)) {
				return omprpc.TextResult("Action declined by the user."), nil
			}
			if m.PHP != nil {
				runCtx, cancel := context.WithTimeout(ctx, bashTimeout)
				defer cancel()
				res := m.PHP.RunInSite(runCtx, site, p.Command)
				return omprpc.TextResult(fmt.Sprintf("Exit code: %d\nOutput:\n%s", res.ExitCode, truncateOutput(res.Output))), nil
			}

			return omprpc.TextResult("PHP manager not initialized"), nil
		},
	}
}
