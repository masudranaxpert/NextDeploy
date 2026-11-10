package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"panel/internal/caddy"
	"panel/internal/db"
	"panel/internal/htaccess"
	"panel/internal/phphosting"
)

func (h *Handler) recordMCPAudit(u db.User, action, appID, details string) {
	go func() {
		auditCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = h.p.DB.CreateAuditLog(auditCtx, db.AuditLog{
			UserID:     u.ID,
			Username:   u.Username,
			Action:     action,
			TargetType: "app",
			TargetID:   appID,
			Details:    details,
			CreatedAt:  time.Now(),
		})
	}()
}

func (h *Handler) handlePHPRouting(ctx context.Context, u db.User, args map[string]interface{}) (CallToolResult, error) {
	appID := getStringArg(args, "app_id")
	action := strings.ToLower(getStringArg(args, "action"))
	if action == "" {
		action = "get"
	}

	applyScan := getBoolArg(args, "apply")
	reqRole := db.CollabRoleViewer
	if action == "set" || (action == "scan_htaccess" && applyScan) {
		reqRole = db.CollabRoleDeveloper
	}

	if _, err := h.hasAppAccess(ctx, u, appID, reqRole); err != nil {
		return errorResult(err)
	}
	site, err := h.p.DB.GetPHPSite(ctx, appID)
	if err != nil {
		return errorResult(fmt.Errorf("app %q is not a PHP or static site", appID))
	}
	isStatic := site.SiteType == phphosting.SiteTypeStatic

	switch action {
	case "get":
		s := phphosting.SettingsFor(site)
		return jsonResult(map[string]interface{}{
			"app_id":  site.AppID,
			"stopped": s.Stopped,
			"routing": s.Routing,
		})

	case "convert_htaccess":
		content := ""
		if raw, ok := args["content"].(string); ok {
			content = raw
		}
		if len(content) > 512<<10 {
			return errorResult(errors.New("htaccess content exceeds 512 KiB limit"))
		}
		conv, err := htaccess.New()
		if err != nil {
			return errorResult(err)
		}
		rel := getStringArg(args, "file_path")
		if rel == "" {
			rel = ".htaccess"
		}
		return jsonResult(conv.Parse(content, rel))

	case "scan_htaccess":
		docRoot := filepath.Join(h.p.WorkspacesRoot, appID, "htdocs")
		if rd := strings.TrimSpace(site.RootDir); rd != "" {
			docRoot = filepath.Join(docRoot, filepath.FromSlash(rd))
		}
		conv, err := htaccess.New()
		if err != nil {
			return errorResult(err)
		}
		res, err := conv.ScanDir(ctx, docRoot)
		if err != nil {
			return errorResult(fmt.Errorf("failed to scan htdocs: %w", err))
		}
		if !applyScan {
			return jsonResult(res)
		}
		s := phphosting.SettingsFor(site)
		if strings.EqualFold(getStringArg(args, "mode"), "append") {
			s.Routing.Rules = caddy.MergeUniqueRoutingRules(s.Routing.Rules, res.Rules)
		} else {
			s.Routing.Rules = res.Rules
		}
		if res.FallbackMode != "" && s.Routing.FallbackMode == "" {
			s.Routing.FallbackMode = res.FallbackMode
			s.Routing.FallbackEntry = res.FallbackEntry
		}
		if err := caddy.ValidateRouting(&s.Routing, isStatic); err != nil {
			return errorResult(err)
		}
		site.SettingsJSON = s.JSON()
		if err := h.p.DB.UpdatePHPSite(ctx, site); err != nil {
			return errorResult(err)
		}
		if h.p.PHP != nil {
			sctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
			defer cancel()
			if err := h.p.PHP.SyncSite(sctx, appID); err != nil {
				return errorResult(err)
			}
		}
		h.recordMCPAudit(u, "php_htaccess_scan", appID, fmt.Sprintf("MCP scanned %d .htaccess file(s) and applied %d rules", res.FilesScanned, len(res.Rules)))
		return jsonResult(map[string]interface{}{"ok": true, "result": res, "routing": s.Routing})

	case "set":
		s := phphosting.SettingsFor(site)
		next := s.Routing
		if fm := getStringArg(args, "fallback_mode"); fm != "" {
			next.FallbackMode = fm
			next.FallbackEntry = getStringArg(args, "fallback_entry")
		} else if _, hasEntry := args["fallback_entry"]; hasEntry {
			next.FallbackEntry = getStringArg(args, "fallback_entry")
		}
		if rawRules, hasRules := args["rules"]; hasRules {
			b, err := json.Marshal(rawRules)
			if err != nil {
				return errorResult(fmt.Errorf("invalid rules array: %w", err))
			}
			var rules []caddy.RoutingRule
			if err := json.Unmarshal(b, &rules); err != nil {
				return errorResult(fmt.Errorf("invalid rules array: %w", err))
			}
			next.Rules = rules
		}
		if err := caddy.ValidateRouting(&next, isStatic); err != nil {
			return errorResult(err)
		}
		s.Routing = next
		site.SettingsJSON = s.JSON()
		if err := h.p.DB.UpdatePHPSite(ctx, site); err != nil {
			return errorResult(err)
		}
		if h.p.PHP != nil {
			sctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
			defer cancel()
			if err := h.p.PHP.SyncSite(sctx, appID); err != nil {
				return errorResult(err)
			}
		}
		h.recordMCPAudit(u, "php_routing_rules", appID, fmt.Sprintf("MCP saved %d routing rules (fallback=%s)", len(s.Routing.Rules), s.Routing.FallbackMode))
		return jsonResult(map[string]interface{}{"ok": true, "routing": s.Routing})

	default:
		return errorResult(fmt.Errorf("unsupported php_routing action %q (use get, set, scan_htaccess, convert_htaccess)", action))
	}
}

func (h *Handler) handlePHPPower(ctx context.Context, u db.User, args map[string]interface{}) (CallToolResult, error) {
	appID := getStringArg(args, "app_id")
	action := strings.ToLower(getStringArg(args, "action"))
	if _, err := h.hasAppAccess(ctx, u, appID, db.CollabRoleDeveloper); err != nil {
		return errorResult(err)
	}
	if _, err := h.p.DB.GetPHPSite(ctx, appID); err != nil {
		return errorResult(fmt.Errorf("app %q is not a PHP or static site", appID))
	}
	if h.p.PHP == nil {
		return errorResult(errors.New("PHP hosting manager not initialized"))
	}

	sctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	switch action {
	case "start":
		if err := h.p.PHP.StartSite(sctx, appID); err != nil {
			return errorResult(err)
		}
		h.p.InvalidateAfterAppDeployChange(appID)
		h.recordMCPAudit(u, "php_site_start", appID, "Started PHP/static site via MCP")
		return jsonResult(map[string]interface{}{"ok": true, "status": "running"})

	case "stop":
		if err := h.p.PHP.StopSite(sctx, appID); err != nil {
			return errorResult(err)
		}
		h.p.InvalidateAfterAppDeployChange(appID)
		h.recordMCPAudit(u, "php_site_stop", appID, "Stopped PHP/static site via MCP")
		return jsonResult(map[string]interface{}{"ok": true, "status": "stopped"})

	case "restart":
		if err := h.p.PHP.SyncSite(sctx, appID); err != nil {
			return errorResult(err)
		}
		h.p.PHP.ReloadCaddy(sctx)
		h.recordMCPAudit(u, "php_site_restart", appID, "Restarted PHP/static site via MCP")
		return jsonResult(map[string]interface{}{"ok": true, "status": "running"})

	default:
		return errorResult(fmt.Errorf("unsupported php_power action %q (use start, stop, restart)", action))
	}
}
