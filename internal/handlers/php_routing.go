package handlers

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"panel/internal/caddy"
	"panel/internal/db"
	"panel/internal/htaccess"
	"panel/internal/phphosting"

	"github.com/gofiber/fiber/v2"
)

// POST /apps/:id/php/stop  and  POST /api/v1/php/sites/:id/stop
func (p *Panel) PHPSiteStop(c *fiber.Ctx) error {
	app, _, err := p.phpSiteWithRole(c, db.CollabRoleDeveloper)
	if app.ID == "" {
		return err
	}
	sctx, cancel := context.WithTimeout(c.UserContext(), 2*time.Minute)
	defer cancel()
	if err := p.PHP.StopSite(sctx, app.ID); err != nil {
		return phpJSONErr(c, 500, err.Error())
	}
	p.InvalidateAfterAppDeployChange(app.ID)
	p.RecordAuditLog(c, "php_site_stop", "app", app.ID, "Stopped PHP/static site")
	return c.JSON(fiber.Map{"ok": true, "status": "stopped"})
}

// POST /apps/:id/php/start  and  POST /api/v1/php/sites/:id/start
func (p *Panel) PHPSiteStart(c *fiber.Ctx) error {
	app, _, err := p.phpSiteWithRole(c, db.CollabRoleDeveloper)
	if app.ID == "" {
		return err
	}
	sctx, cancel := context.WithTimeout(c.UserContext(), 2*time.Minute)
	defer cancel()
	if err := p.PHP.StartSite(sctx, app.ID); err != nil {
		return phpJSONErr(c, 500, err.Error())
	}
	p.InvalidateAfterAppDeployChange(app.ID)
	p.RecordAuditLog(c, "php_site_start", "app", app.ID, "Started PHP/static site")
	return c.JSON(fiber.Map{"ok": true, "status": "running"})
}

// GET /apps/:id/php/routing  and  GET /api/v1/php/sites/:id/routing
func (p *Panel) PHPRoutingGet(c *fiber.Ctx) error {
	_, site, err := p.phpSiteWithRole(c, db.CollabRoleViewer)
	if site.AppID == "" {
		return err
	}
	s := phphosting.SettingsFor(site)
	return c.JSON(fiber.Map{
		"ok":      true,
		"routing": s.Routing,
		"stopped": s.Stopped,
	})
}

// POST / PUT /apps/:id/php/routing  and  /api/v1/php/sites/:id/routing
func (p *Panel) PHPRoutingSave(c *fiber.Ctx) error {
	ctx := c.UserContext()
	app, site, err := p.phpSiteWithRole(c, db.CollabRoleDeveloper)
	if site.AppID == "" {
		return err
	}
	var body caddy.SiteRouting
	if err := c.BodyParser(&body); err != nil {
		return phpJSONErr(c, 400, "invalid routing payload")
	}
	owner, err := p.phpOwner(ctx, site)
	if err != nil {
		return phpJSONErr(c, 500, err.Error())
	}
	s := phphosting.SettingsFor(site)
	s.Routing = body
	if err := s.ValidateForSite(phpCeilings(ctx, owner), site.SiteType == phphosting.SiteTypeStatic); err != nil {
		return phpJSONErr(c, 400, err.Error())
	}
	site.SettingsJSON = s.JSON()
	if err := p.DB.UpdatePHPSite(ctx, site); err != nil {
		return phpJSONErr(c, 500, err.Error())
	}
	sctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if err := p.PHP.SyncSite(sctx, app.ID); err != nil {
		return phpJSONErr(c, 500, err.Error())
	}
	p.RecordAuditLog(c, "php_routing_rules", "app", app.ID, fmt.Sprintf("Saved %d routing rules (fallback=%s)", len(s.Routing.Rules), s.Routing.FallbackMode))
	return c.JSON(fiber.Map{"ok": true, "routing": s.Routing})
}

// POST /apps/:id/php/routing/convert-htaccess
func (p *Panel) PHPRoutingConvertHtaccess(c *fiber.Ctx) error {
	_, site, err := p.phpSiteWithRole(c, db.CollabRoleDeveloper)
	if site.AppID == "" {
		return err
	}
	var body struct {
		Content  string `json:"content"`
		FilePath string `json:"file_path"`
	}
	if err := c.BodyParser(&body); err != nil {
		return phpJSONErr(c, 400, "invalid body")
	}
	if len(body.Content) > 512<<10 {
		return phpJSONErr(c, 400, "htaccess content exceeds 512 KiB limit")
	}
	conv, err := htaccess.New()
	if err != nil {
		return phpJSONErr(c, 500, err.Error())
	}
	rel := strings.TrimSpace(body.FilePath)
	if rel == "" {
		rel = ".htaccess"
	}
	res := conv.Parse(body.Content, rel)
	return c.JSON(fiber.Map{
		"ok":     true,
		"result": res,
	})
}

// POST /apps/:id/php/routing/scan-htaccess
func (p *Panel) PHPRoutingScanHtaccess(c *fiber.Ctx) error {
	ctx := c.UserContext()
	app, site, err := p.phpSiteWithRole(c, db.CollabRoleDeveloper)
	if site.AppID == "" {
		return err
	}
	var body struct {
		Apply bool   `json:"apply"`
		Mode  string `json:"mode"` // "replace" | "append"
	}
	_ = c.BodyParser(&body)

	docRoot := filepath.Join(p.WorkspacesRoot, app.ID, "htdocs")
	if rd := strings.TrimSpace(site.RootDir); rd != "" {
		docRoot = filepath.Join(docRoot, filepath.FromSlash(rd))
	}
	conv, err := htaccess.New()
	if err != nil {
		return phpJSONErr(c, 500, err.Error())
	}
	res, scanErr := conv.ScanDir(ctx, docRoot)
	if scanErr != nil {
		return phpJSONErr(c, 400, "failed to scan htdocs: "+scanErr.Error())
	}

	if !body.Apply {
		return c.JSON(fiber.Map{
			"ok":     true,
			"result": res,
		})
	}

	owner, err := p.phpOwner(ctx, site)
	if err != nil {
		return phpJSONErr(c, 500, err.Error())
	}
	s := phphosting.SettingsFor(site)
	if strings.EqualFold(strings.TrimSpace(body.Mode), "append") {
		s.Routing.Rules = caddy.MergeUniqueRoutingRules(s.Routing.Rules, res.Rules)
	} else {
		s.Routing.Rules = res.Rules
	}
	if res.FallbackMode != "" && s.Routing.FallbackMode == "" {
		s.Routing.FallbackMode = res.FallbackMode
		s.Routing.FallbackEntry = res.FallbackEntry
	}
	if err := s.ValidateForSite(phpCeilings(ctx, owner), site.SiteType == phphosting.SiteTypeStatic); err != nil {
		return phpJSONErr(c, 400, err.Error())
	}
	site.SettingsJSON = s.JSON()
	if err := p.DB.UpdatePHPSite(ctx, site); err != nil {
		return phpJSONErr(c, 500, err.Error())
	}
	sctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if err := p.PHP.SyncSite(sctx, app.ID); err != nil {
		return phpJSONErr(c, 500, err.Error())
	}
	p.RecordAuditLog(c, "php_htaccess_scan", "app", app.ID, fmt.Sprintf("Scanned %d .htaccess file(s) in htdocs and applied %d rules", res.FilesScanned, len(res.Rules)))
	return c.JSON(fiber.Map{
		"ok":      true,
		"result":  res,
		"routing": s.Routing,
	})
}
