package handlers

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"panel/internal/db"
	"panel/internal/dockerapi"
	"panel/internal/phphosting"
	"panel/internal/runutil"
	"panel/internal/sysinfo"
	"panel/internal/volumex"

	"github.com/gofiber/fiber/v2"
)

// ─── PHP Management Endpoints (/api/v1/php/...) ─────────────────────────────

// APIPHPSitesList returns all PHP sites accessible to the caller.
// GET /api/v1/php/sites
func (p *Panel) APIPHPSitesList(c *fiber.Ctx) error {
	ctx := c.UserContext()
	u, ok := currentUser(c)
	if !ok {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}

	var apps []db.App
	var err error
	if u.Role == db.RoleAdmin {
		apps, err = p.DB.ListApps(ctx)
	} else {
		apps, err = p.DB.ListAppsForUser(ctx, u.ID)
	}
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	type phpSiteItem struct {
		AppID      string   `json:"app_id"`
		Name       string   `json:"name"`
		SiteType   string   `json:"site_type"`
		PHPVersion string   `json:"php_version"`
		RootDir    string   `json:"root_dir"`
		Domains    []string `json:"domains"`
		CreatedAt  string   `json:"created_at"`
		UID        int      `json:"uid"`
		Status     string   `json:"status"`
	}

	var out []phpSiteItem
	for _, a := range apps {
		site, serr := p.DB.GetPHPSite(ctx, a.ID)
		if serr != nil {
			continue
		}
		domains, _ := p.DB.ListAppDomains(ctx, a.ID)
		dNames := make([]string, 0, len(domains))
		for _, d := range domains {
			dNames = append(dNames, d.Domain)
		}
		out = append(out, phpSiteItem{
			AppID:      site.AppID,
			Name:       a.Name,
			SiteType:   site.SiteType,
			PHPVersion: site.PHPVersion,
			RootDir:    site.RootDir,
			Domains:    dNames,
			CreatedAt:  a.CreatedAt.Format(time.RFC3339),
			UID:        site.UID,
			Status:     a.Status,
		})
	}
	if out == nil {
		out = []phpSiteItem{}
	}
	return c.JSON(out)
}

// APIPHPSiteGet returns detailed configuration for a single PHP site.
// GET /api/v1/php/sites/:id
func (p *Panel) APIPHPSiteGet(c *fiber.Ctx) error {
	ctx := c.UserContext()
	_, site, err := p.phpSiteFor(c)
	if site.AppID == "" {
		return err
	}
	app, err := p.DB.GetApp(ctx, site.AppID)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "app not found"})
	}

	domains, _ := p.DB.ListAppDomains(ctx, app.ID)
	dNames := make([]string, 0, len(domains))
	for _, d := range domains {
		dNames = append(dNames, d.Domain)
	}

	dbs, _ := p.DB.ListPHPDatabases(ctx, site.AppID)
	dbUsers, _ := p.DB.ListPHPDBUsers(ctx, site.AppID)
	crons, _ := p.DB.ListPHPCronJobs(ctx, site.AppID)

	var settings phphosting.SiteSettings
	if site.SettingsJSON != "" {
		_ = json.Unmarshal([]byte(site.SettingsJSON), &settings)
	}

	return c.JSON(fiber.Map{
		"ok":          true,
		"app_id":      site.AppID,
		"name":        app.Name,
		"site_type":   site.SiteType,
		"php_version": site.PHPVersion,
		"root_dir":    site.RootDir,
		"domains":     dNames,
		"uid":         site.UID,
		"status":      app.Status,
		"settings":    settings,
		"databases":   dbs,
		"db_users":    dbUsers,
		"crons":       crons,
	})
}

// APIPHPPMAUrl generates a one-click SSO URL into phpMyAdmin.
// GET /api/v1/php/sites/:id/pma-url
func (p *Panel) APIPHPPMAUrl(c *fiber.Ctx) error {
	ctx := c.UserContext()
	u, ok := currentUser(c)
	if !ok {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}
	appID := c.Params("id")
	if allowed, _ := p.CanAccessApp(ctx, u.ID, u.Role, appID, db.CollabRoleDeveloper); !allowed {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "forbidden"})
	}
	dbs, _ := p.DB.ListPHPDatabases(ctx, appID)
	names := make([]string, 0, len(dbs))
	for _, d := range dbs {
		names = append(names, d.DBName)
	}
	sess, err := p.PHP.PMAOpen(ctx, u.ID, appID, names, false)
	if err != nil {
		return c.Status(fiber.StatusConflict).JSON(fiber.Map{"error": err.Error()})
	}
	proto := "http"
	if c.Protocol() == "https" || c.Get("X-Forwarded-Proto") == "https" {
		proto = "https"
	}
	pmaURL := fmt.Sprintf("%s://%s/phpmyadmin/?token=%s", proto, c.Hostname(), sess.Token)
	return c.JSON(fiber.Map{
		"ok":    true,
		"token": sess.Token,
		"url":   pmaURL,
	})
}

// APIPHPDatabasesList lists databases for a PHP site.
// GET /api/v1/php/sites/:id/databases
func (p *Panel) APIPHPDatabasesList(c *fiber.Ctx) error {
	ctx := c.UserContext()
	_, site, err := p.phpSiteFor(c)
	if site.AppID == "" {
		return err
	}
	dbs, err := p.DB.ListPHPDatabases(ctx, site.AppID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(dbs)
}

// APIPHPDBUsersList lists database users for a PHP site.
// GET /api/v1/php/sites/:id/db-users
func (p *Panel) APIPHPDBUsersList(c *fiber.Ctx) error {
	ctx := c.UserContext()
	_, site, err := p.phpSiteFor(c)
	if site.AppID == "" {
		return err
	}
	users, err := p.DB.ListPHPDBUsers(ctx, site.AppID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(users)
}

// APIPHPCronList lists cron jobs for a PHP site.
// GET /api/v1/php/sites/:id/cron
func (p *Panel) APIPHPCronList(c *fiber.Ctx) error {
	ctx := c.UserContext()
	_, site, err := p.phpSiteFor(c)
	if site.AppID == "" {
		return err
	}
	crons, err := p.DB.ListPHPCronJobs(ctx, site.AppID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(crons)
}

// ─── Volumes Management Endpoints (/api/v1/volumes/...) ──────────────────────

// APIVolumesList lists all Docker volumes accessible to the user with app attributions.
// GET /api/v1/volumes
func (p *Panel) APIVolumesList(c *fiber.Ctx) error {
	ctx := c.UserContext()
	u, ok := currentUser(c)
	if !ok {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}

	allVolNames, _ := volumex.List(ctx)
	visible := p.visibleVolumes(ctx, u, allVolNames)

	type volItem struct {
		Name  string `json:"name"`
		InUse bool   `json:"in_use"`
	}

	out := make([]volItem, 0, len(visible))
	for _, v := range visible {
		out = append(out, volItem{
			Name:  v,
			InUse: true,
		})
	}
	return c.JSON(out)
}

// APIVolumeRemove removes a Docker volume.
// DELETE /api/v1/volumes/:name
// POST /api/v1/volumes/remove
func (p *Panel) APIVolumeRemove(c *fiber.Ctx) error {
	name := strings.TrimSpace(c.Params("name"))
	if name == "" {
		name = strings.TrimSpace(c.FormValue("name"))
	}
	if !volumex.ValidVolumeName(name) {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid volume name"})
	}

	allowed, err := p.isVolumeWriteAllowed(c, name)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	if !allowed {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "forbidden"})
	}

	ctx := c.UserContext()
	res := runutil.Run(ctx, "", nil, "docker", "volume", "rm", name)
	if !res.OK {
		return c.Status(fiber.StatusConflict).JSON(fiber.Map{"error": res.Output})
	}
	p.RecordAuditLog(c, "volume_remove", "volume", name, "Removed volume via CLI: "+name)
	return c.JSON(fiber.Map{"ok": true, "message": "Volume removed successfully"})
}

// APIVolumesPrune removes all unused Docker volumes.
// POST /api/v1/volumes/prune
func (p *Panel) APIVolumesPrune(c *fiber.Ctx) error {
	u, ok := currentUser(c)
	if !ok || u.Role != db.RoleAdmin {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "admin privileges required"})
	}
	ctx := c.UserContext()
	res := runutil.Run(ctx, "", nil, "docker", "volume", "prune", "-f")
	p.RecordAuditLog(c, "volumes_prune", "volume", "all", "Pruned unused volumes via CLI")
	return c.JSON(fiber.Map{"ok": res.OK, "output": res.Output})
}

// ─── Backup Management Endpoints (/api/v1/backups/...) ──────────────────────

// APIBackupHistory lists backup history for a specific app or all accessible apps.
// GET /api/v1/backups/history?app_id=
func (p *Panel) APIBackupHistory(c *fiber.Ctx) error {
	ctx := c.UserContext()
	u, ok := currentUser(c)
	if !ok {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}

	appID := strings.TrimSpace(c.Query("app_id"))
	if appID != "" {
		allowed, err := p.CanAccessApp(ctx, u.ID, u.Role, appID, db.CollabRoleViewer)
		if err != nil || !allowed {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "forbidden"})
		}
		rows, err := p.DB.ListBackupHistory(ctx, appID, 50)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(rows)
	}

	var apps []db.App
	var err error
	if u.Role == db.RoleAdmin {
		apps, err = p.DB.ListApps(ctx)
	} else {
		apps, err = p.DB.ListAppsForUser(ctx, u.ID)
	}
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	var allRows []db.BackupHistory
	for _, a := range apps {
		rows, _ := p.DB.ListBackupHistory(ctx, a.ID, 10)
		allRows = append(allRows, rows...)
	}
	if allRows == nil {
		allRows = []db.BackupHistory{}
	}
	return c.JSON(allRows)
}

// APIBackupManual triggers a manual backup for an application.
// POST /api/v1/backups/manual
func (p *Panel) APIBackupManual(c *fiber.Ctx) error {
	ctx := c.UserContext()
	u, ok := currentUser(c)
	if !ok {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}

	var req struct {
		AppID  string `json:"app_id"`
		DestID int64  `json:"dest_id"`
	}
	if err := c.BodyParser(&req); err != nil || req.AppID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "app_id is required"})
	}

	allowed, err := p.CanAccessApp(ctx, u.ID, u.Role, req.AppID, db.CollabRoleDeveloper)
	if err != nil || !allowed {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "forbidden"})
	}

	dest, err := p.DB.GetBackupDestination(ctx, req.DestID)
	if err != nil {
		dests, _ := p.DB.ListBackupDestinations(ctx, nil)
		if len(dests) == 0 {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "no backup destinations configured"})
		}
		dest = dests[0]
	}

	c.Params("id", req.AppID)
	c.Params("destid", strconv.FormatInt(dest.ID, 10))
	return p.AppBackupManual(c)
}

// APIBackupRestore triggers a backup restore.
// POST /api/v1/backups/restore/:id
func (p *Panel) APIBackupRestore(c *fiber.Ctx) error {
	ctx := c.UserContext()
	u, ok := currentUser(c)
	if !ok {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}

	historyID, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid history id"})
	}

	hist, err := p.DB.GetBackupHistory(ctx, historyID)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "backup history record not found"})
	}

	allowed, err := p.CanAccessApp(ctx, u.ID, u.Role, hist.AppID, db.CollabRoleDeveloper)
	if err != nil || !allowed {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "forbidden"})
	}

	c.Params("id", hist.AppID)
	c.Params("historyid", strconv.FormatInt(historyID, 10))
	return p.AppBackupRestore(c)
}

// APIBackupSchedulesList lists backup schedules.
// GET /api/v1/backups/schedules?app_id=
func (p *Panel) APIBackupSchedulesList(c *fiber.Ctx) error {
	ctx := c.UserContext()
	u, ok := currentUser(c)
	if !ok {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}

	appID := strings.TrimSpace(c.Query("app_id"))
	if appID != "" {
		allowed, err := p.CanAccessApp(ctx, u.ID, u.Role, appID, db.CollabRoleViewer)
		if err != nil || !allowed {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "forbidden"})
		}
		schedules, err := p.DB.ListBackupSchedules(ctx, appID)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(schedules)
	}

	var apps []db.App
	var err error
	if u.Role == db.RoleAdmin {
		apps, err = p.DB.ListApps(ctx)
	} else {
		apps, err = p.DB.ListAppsForUser(ctx, u.ID)
	}
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	var allSchedules []db.BackupSchedule
	for _, a := range apps {
		schedules, _ := p.DB.ListBackupSchedules(ctx, a.ID)
		allSchedules = append(allSchedules, schedules...)
	}
	if allSchedules == nil {
		allSchedules = []db.BackupSchedule{}
	}
	return c.JSON(allSchedules)
}

// APIBackupScheduleCreate creates a backup schedule.
// POST /api/v1/backups/schedules
func (p *Panel) APIBackupScheduleCreate(c *fiber.Ctx) error {
	var body struct {
		AppID       string `json:"app_id"`
		DestID      int64  `json:"dest_id"`
		CronExpr    string `json:"cron"`
		KeepCount   int    `json:"retention"`
		IncludeCode bool   `json:"include_code"`
	}
	if err := c.BodyParser(&body); err != nil || body.AppID == "" || body.DestID == 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "app_id and dest_id are required"})
	}
	c.Params("id", body.AppID)
	c.Params("destid", strconv.FormatInt(body.DestID, 10))
	return p.AppBackupScheduleCreate(c)
}

// APIBackupHistoryLog returns the log for a backup history record.
// GET /api/v1/backups/history/:id/log
func (p *Panel) APIBackupHistoryLog(c *fiber.Ctx) error {
	ctx := c.UserContext()
	u, ok := currentUser(c)
	if !ok {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}

	historyID, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid history id"})
	}

	hist, err := p.DB.GetBackupHistory(ctx, historyID)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "backup history record not found"})
	}

	allowed, err := p.CanAccessApp(ctx, u.ID, u.Role, hist.AppID, db.CollabRoleViewer)
	if err != nil || !allowed {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "forbidden"})
	}

	logContent := hist.Log
	if logContent == "" {
		if hist.Status == "failed" && hist.ErrorMessage != "" {
			logContent = "Error: " + hist.ErrorMessage
		} else {
			logContent = "No log available"
		}
	}

	return c.JSON(fiber.Map{
		"ok":     true,
		"log":    logContent,
		"status": hist.Status,
	})
}

// APIBackupScheduleDelete removes a backup schedule.
// DELETE /api/v1/backups/schedules/:id
func (p *Panel) APIBackupScheduleDelete(c *fiber.Ctx) error {
	ctx := c.UserContext()
	u, ok := currentUser(c)
	if !ok {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}

	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid schedule id"})
	}

	sched, err := p.DB.GetBackupSchedule(ctx, id)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "schedule not found"})
	}

	allowed, err := p.CanAccessApp(ctx, u.ID, u.Role, sched.AppID, db.CollabRoleDeveloper)
	if err != nil || !allowed {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "forbidden"})
	}

	if err := p.DB.DeleteBackupSchedule(ctx, sched.AppID, id); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	p.RecordAuditLog(c, "backup_schedule_delete", "app", sched.AppID, fmt.Sprintf("Deleted backup schedule #%d", id))
	return c.JSON(fiber.Map{"ok": true, "message": "schedule deleted"})
}

// ─── Proxy Reload & System (/api/v1/proxy/..., /api/v1/system/...) ───────────

// APIProxyReload reconciles and reloads Caddy proxy routes.
// POST /api/v1/proxy/reload
func (p *Panel) APIProxyReload(c *fiber.Ctx) error {
	u, ok := currentUser(c)
	if !ok || u.Role != db.RoleAdmin {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "admin privileges required"})
	}
	ctx := c.UserContext()
	p.ReconcileProxyRoutes(ctx)
	p.RecordAuditLog(c, "proxy_reload", "proxy", "caddy", "Reloaded Caddy proxy routes via CLI")
	return c.JSON(fiber.Map{"ok": true, "message": "Proxy routes reloaded successfully"})
}

// APISystemTop returns current host telemetry (CPU, RAM, Disk, Containers).
// GET /api/v1/system/top
// GET /api/v1/system/stats
func (p *Panel) APISystemTop(c *fiber.Ctx) error {
	ctx := c.UserContext()
	u, ok := currentUser(c)
	if !ok {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}

	snap := sysinfo.Collect(ctx)
	rows, _ := dockerapi.ListContainers(ctx)
	visible := p.visibleContainers(ctx, u, rows)

	type cStat struct {
		Name    string `json:"name"`
		Service string `json:"service"`
		State   string `json:"state"`
		Status  string `json:"status"`
		Image   string `json:"image"`
	}
	cList := make([]cStat, 0, len(visible))
	for _, r := range visible {
		cList = append(cList, cStat{
			Name:    r.Name,
			Service: r.ComposeService,
			State:   r.State,
			Status:  r.Status,
			Image:   r.Image,
		})
	}

	return c.JSON(fiber.Map{
		"ok":             true,
		"cpu_pct":        snap.CPUPercent,
		"mem_used_gb":    snap.MemUsedGB,
		"mem_total_gb":   snap.MemTotalGB,
		"mem_pct":        snap.MemUsedPct,
		"disk_used_gb":   snap.DiskUsedGB,
		"disk_total_gb":  snap.DiskTotalGB,
		"disk_pct":       snap.DiskUsedPct,
		"containers_num": len(cList),
		"containers":     cList,
	})
}

// APISystemCleanup triggers pruning of build cache, stopped containers, dangling images, and temporary files.
// POST /api/v1/system/cleanup
func (p *Panel) APISystemCleanup(c *fiber.Ctx) error {
	u, ok := currentUser(c)
	if !ok || u.Role != db.RoleAdmin {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "admin privileges required"})
	}
	ctx := c.UserContext()
	cRes := runutil.Run(ctx, "", nil, "docker", "container", "prune", "-f")
	iRes := runutil.Run(ctx, "", nil, "docker", "image", "prune", "-f")
	bRes := runutil.Run(ctx, "", nil, "docker", "builder", "prune", "-f")

	tmpPath := os.TempDir()
	_ = filepath.Walk(tmpPath, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && time.Since(info.ModTime()) > 24*time.Hour {
			_ = os.Remove(path)
		}
		return nil
	})

	p.RecordAuditLog(c, "system_cleanup", "system", "host", "Triggered system cleanup via CLI")
	return c.JSON(fiber.Map{
		"ok":               true,
		"containers_prune": cRes.Output,
		"images_prune":     iRes.Output,
		"builder_prune":    bRes.Output,
		"message":          "System cleanup completed successfully",
	})
}
