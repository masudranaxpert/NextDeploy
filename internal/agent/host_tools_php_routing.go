package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"panel/internal/agent/omprpc"
	"panel/internal/caddy"
	"panel/internal/db"
	"panel/internal/htaccess"
	"panel/internal/phphosting"
)

func (m *Manager) recordAgentAudit(ctx context.Context, action, appID, details string) {
	if m.DB == nil {
		return
	}
	_ = m.DB.CreateAuditLog(ctx, db.AuditLog{
		Username:   "nextdeploy-ai",
		Action:     action,
		TargetType: "app",
		TargetID:   appID,
		IPAddress:  "internal-ai",
		Details:    details,
		CreatedAt:  time.Now().UTC(),
	})
}

func (m *Manager) siteDocRoot(site db.PHPSite) string {
	docRoot := filepath.Join(m.Root, site.AppID, "htdocs")
	if rd := strings.TrimSpace(site.RootDir); rd != "" {
		docRoot = filepath.Join(docRoot, filepath.FromSlash(rd))
	}
	return docRoot
}

// 12. manage_php_routing
func (m *Manager) toolManagePHPRouting() omprpc.HostTool {
	return omprpc.HostTool{
		Definition: omprpc.HostToolDefinition{
			Name:        "manage_php_routing",
			Description: "Inspect, convert Apache .htaccess files, or configure per-site Caddy URL routing rules (rewrite, redir, block) and fallback mode (default, strict_404, custom) for a PHP or static site.",
			Parameters: map[string]json.RawMessage{
				"type": json.RawMessage(`"object"`),
				"properties": json.RawMessage(`{
					"app_id": {"type": "string", "description": "PHP or static site ID"},
					"action": {"type": "string", "enum": ["get", "set", "scan_htaccess", "convert_htaccess"], "description": "Operation to perform"},
					"routing": {"type": "object", "description": "For action=set: {fallback_mode, fallback_entry, rules: [{id, kind, match_type, pattern, target, status, enabled, note}]}"},
					"content": {"type": "string", "description": "For action=convert_htaccess: raw .htaccess content"},
					"file_path": {"type": "string", "description": "Optional relative path for subfolder .htaccess conversion"},
					"apply": {"type": "boolean", "description": "For action=scan_htaccess: true to save converted rules onto the site"},
					"mode": {"type": "string", "enum": ["replace", "append"], "description": "For action=scan_htaccess with apply=true"}
				}`),
				"required": json.RawMessage(`["app_id", "action"]`),
			},
		},
		Handler: func(ctx context.Context, call *omprpc.HostToolCall, args json.RawMessage) (omprpc.HostToolResultPayload, error) {
			var p struct {
				AppID    string             `json:"app_id"`
				Action   string             `json:"action"`
				Routing  *caddy.SiteRouting `json:"routing"`
				Content  string             `json:"content"`
				FilePath string             `json:"file_path"`
				Apply    bool               `json:"apply"`
				Mode     string             `json:"mode"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return omprpc.TextResult("Invalid arguments"), err
			}

			action := strings.ToLower(strings.TrimSpace(p.Action))
			if action == "convert_htaccess" {
				if len(p.Content) > 512<<10 {
					return omprpc.TextResult(".htaccess content exceeds 512 KiB limit"), nil
				}
				conv, err := htaccess.New()
				if err != nil {
					return omprpc.TextResult(fmt.Sprintf("Converter error: %v", err)), nil
				}
				rel := strings.TrimSpace(p.FilePath)
				if rel == "" {
					rel = ".htaccess"
				}
				res := conv.Parse(p.Content, rel)
				return jsonResult(res), nil
			}

			site, err := m.lookupSite(ctx, p.AppID)
			if err != nil {
				return omprpc.TextResult(fmt.Sprintf("Site not found: %s", p.AppID)), nil
			}
			isStatic := site.SiteType == phphosting.SiteTypeStatic

			switch action {
			case "get":
				s := phphosting.SettingsFor(site)
				return jsonResult(map[string]any{
					"app_id":  site.AppID,
					"stopped": s.Stopped,
					"routing": s.Routing,
				}), nil

			case "scan_htaccess":
				conv, err := htaccess.New()
				if err != nil {
					return omprpc.TextResult(fmt.Sprintf("Converter error: %v", err)), nil
				}
				res, err := conv.ScanDir(ctx, m.siteDocRoot(site))
				if err != nil {
					return omprpc.TextResult(fmt.Sprintf("Failed to scan htdocs: %v", err)), nil
				}
				if !p.Apply {
					return jsonResult(res), nil
				}
				if !m.requestApproval(ctx, "manage_php_routing", "Apply .htaccess routing rules",
					fmt.Sprintf("Apply %d converted .htaccess rule(s) to %s (mode=%s)?", len(res.Rules), site.AppID, p.Mode)) {
					return omprpc.TextResult("Action declined by the user."), nil
				}
				s := phphosting.SettingsFor(site)
				if strings.EqualFold(strings.TrimSpace(p.Mode), "append") {
					s.Routing.Rules = caddy.MergeUniqueRoutingRules(s.Routing.Rules, res.Rules)
				} else {
					s.Routing.Rules = res.Rules
				}
				if res.FallbackMode != "" && s.Routing.FallbackMode == "" {
					s.Routing.FallbackMode = res.FallbackMode
					s.Routing.FallbackEntry = res.FallbackEntry
				}
				if err := caddy.ValidateRouting(&s.Routing, isStatic); err != nil {
					return omprpc.TextResult(fmt.Sprintf("Invalid converted routing: %v", err)), nil
				}
				site.SettingsJSON = s.JSON()
				if err := m.DB.UpdatePHPSite(ctx, site); err != nil {
					return omprpc.TextResult(fmt.Sprintf("Failed to save routing: %v", err)), nil
				}
				if m.PHP != nil {
					if err := m.PHP.SyncSite(ctx, site.AppID); err != nil {
						return omprpc.TextResult(fmt.Sprintf("Saved to DB, but Caddy sync failed: %v", err)), nil
					}
				}
				m.recordAgentAudit(ctx, "php_htaccess_scan", site.AppID, fmt.Sprintf("AI scanned %d .htaccess file(s) and applied %d rule(s)", res.FilesScanned, len(res.Rules)))
				return jsonResult(map[string]any{"ok": true, "result": res, "routing": s.Routing}), nil

			case "set":
				if p.Routing == nil {
					return omprpc.TextResult("routing object is required for action=set"), nil
				}
				if err := caddy.ValidateRouting(p.Routing, isStatic); err != nil {
					return omprpc.TextResult(fmt.Sprintf("Validation failed: %v", err)), nil
				}
				if !m.requestApproval(ctx, "manage_php_routing", "Update PHP URL routing",
					fmt.Sprintf("Apply %d routing rule(s) (fallback=%q) to %s?", len(p.Routing.Rules), p.Routing.FallbackMode, site.AppID)) {
					return omprpc.TextResult("Action declined by the user."), nil
				}
				s := phphosting.SettingsFor(site)
				s.Routing = *p.Routing
				site.SettingsJSON = s.JSON()
				if err := m.DB.UpdatePHPSite(ctx, site); err != nil {
					return omprpc.TextResult(fmt.Sprintf("Failed to save routing: %v", err)), nil
				}
				if m.PHP != nil {
					if err := m.PHP.SyncSite(ctx, site.AppID); err != nil {
						return omprpc.TextResult(fmt.Sprintf("Saved to DB, but Caddy sync failed: %v", err)), nil
					}
				}
				m.recordAgentAudit(ctx, "php_routing_rules", site.AppID, fmt.Sprintf("AI updated %d routing rule(s) (fallback=%s)", len(s.Routing.Rules), s.Routing.FallbackMode))
				return jsonResult(map[string]any{"ok": true, "routing": s.Routing}), nil

			default:
				return omprpc.TextResult(fmt.Sprintf("Unsupported action %q (use get, set, scan_htaccess, convert_htaccess)", p.Action)), nil
			}
		},
	}
}

// 13. php_site_power
func (m *Manager) toolSetPHPSitePower() omprpc.HostTool {
	return omprpc.HostTool{
		Definition: omprpc.HostToolDefinition{
			Name:        "php_site_power",
			Description: "Starts, stops, or restarts a PHP or static site. Stopping removes its Caddy route (offline) and shuts down its PHP-FPM pool.",
			Parameters: map[string]json.RawMessage{
				"type": json.RawMessage(`"object"`),
				"properties": json.RawMessage(`{
					"app_id": {"type": "string", "description": "PHP or static site ID"},
					"action": {"type": "string", "enum": ["start", "stop", "restart"], "description": "Power action"}
				}`),
				"required": json.RawMessage(`["app_id", "action"]`),
			},
		},
		Handler: func(ctx context.Context, call *omprpc.HostToolCall, args json.RawMessage) (omprpc.HostToolResultPayload, error) {
			var p struct {
				AppID  string `json:"app_id"`
				Action string `json:"action"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return omprpc.TextResult("Invalid arguments"), err
			}
			site, err := m.lookupSite(ctx, p.AppID)
			if err != nil {
				return omprpc.TextResult(fmt.Sprintf("Site not found: %s", p.AppID)), nil
			}
			if m.PHP == nil {
				return omprpc.TextResult("PHP manager not initialized"), nil
			}
			action := strings.ToLower(strings.TrimSpace(p.Action))
			switch action {
			case "start":
				if !m.requestApproval(ctx, "php_site_power", "Start PHP/static site", fmt.Sprintf("Start site %s and restore live Caddy routing?", site.AppID)) {
					return omprpc.TextResult("Action declined by the user."), nil
				}
				if err := m.PHP.StartSite(ctx, site.AppID); err != nil {
					return omprpc.TextResult(fmt.Sprintf("Failed to start site: %v", err)), nil
				}
				m.recordAgentAudit(ctx, "php_site_start", site.AppID, "AI started PHP/static site")
				return omprpc.TextResult(fmt.Sprintf("Site '%s' started and live.", site.AppID)), nil

			case "stop":
				if !m.requestApproval(ctx, "php_site_power", "Stop PHP/static site", fmt.Sprintf("Stop site %s (takes site offline and removes Caddy route)?", site.AppID)) {
					return omprpc.TextResult("Action declined by the user."), nil
				}
				if err := m.PHP.StopSite(ctx, site.AppID); err != nil {
					return omprpc.TextResult(fmt.Sprintf("Failed to stop site: %v", err)), nil
				}
				m.recordAgentAudit(ctx, "php_site_stop", site.AppID, "AI stopped PHP/static site")
				return omprpc.TextResult(fmt.Sprintf("Site '%s' stopped.", site.AppID)), nil

			case "restart":
				if !m.requestApproval(ctx, "php_site_power", "Restart PHP/static site", fmt.Sprintf("Sync and reload site %s?", site.AppID)) {
					return omprpc.TextResult("Action declined by the user."), nil
				}
				if err := m.PHP.SyncSite(ctx, site.AppID); err != nil {
					return omprpc.TextResult(fmt.Sprintf("Failed to restart site: %v", err)), nil
				}
				m.PHP.ReloadCaddy(ctx)
				m.recordAgentAudit(ctx, "php_site_restart", site.AppID, "AI restarted PHP/static site")
				return omprpc.TextResult(fmt.Sprintf("Site '%s' reloaded successfully.", site.AppID)), nil

			default:
				return omprpc.TextResult(fmt.Sprintf("Unsupported action %q (use start, stop, restart)", p.Action)), nil
			}
		},
	}
}
