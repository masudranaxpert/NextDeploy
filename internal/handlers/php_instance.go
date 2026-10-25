package handlers

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"panel/internal/db"
	"panel/internal/phphosting"
	"panel/internal/sysinfo"

	"github.com/gofiber/fiber/v2"
)

// ---- shared helpers for the PHP hosting API ----

func phpJSONErr(c *fiber.Ctx, status int, msg string) error {
	return c.Status(status).JSON(fiber.Map{"ok": false, "error": msg})
}

func (p *Panel) phpEnabled(ctx context.Context) bool {
	return p.PHP != nil && p.DB.GetSetting(ctx, "php_enabled") == "1"
}

// phpTask tracks a slow admin action (starting the database) so the UI can poll it.
type phpTask struct {
	Running bool   `json:"running"`
	Error   string `json:"error,omitempty"`
}

type phpTasks struct {
	mu sync.Mutex
	m  map[string]*phpTask
}

func (t *phpTasks) start(key string, fn func() error) bool {
	t.mu.Lock()
	if t.m == nil {
		t.m = map[string]*phpTask{}
	}
	if cur, ok := t.m[key]; ok && cur.Running {
		t.mu.Unlock()
		return false
	}
	task := &phpTask{Running: true}
	t.m[key] = task
	t.mu.Unlock()
	go func() {
		err := fn()
		t.mu.Lock()
		task.Running = false
		if err != nil {
			task.Error = err.Error()
		}
		t.mu.Unlock()
	}()
	return true
}

func (t *phpTasks) get(key string) phpTask {
	t.mu.Lock()
	defer t.mu.Unlock()
	if cur, ok := t.m[key]; ok {
		return *cur
	}
	return phpTask{}
}

var phpAdminTasks phpTasks

// ---- GET /api/php/status (any signed-in user) ----

func (p *Panel) PHPStatus(c *fiber.Ctx) error {
	ctx := c.UserContext()
	u, _ := currentUser(c)
	enabled := p.phpEnabled(ctx)
	out := fiber.Map{"ok": true, "enabled": enabled, "default_version": phphosting.DefaultPHPVersion}
	if p.PHP == nil {
		return c.JSON(out)
	}
	installed := p.PHP.InstalledVersions(ctx)
	ms := p.PHP.MySQLStatus(ctx)
	lim := p.DB.GetPHPUserLimits(ctx, u.ID)
	usedDB, _ := p.DB.CountPHPDatabasesForOwner(ctx, u.ID)
	usedCron, _ := p.DB.CountPHPCronJobsForOwner(ctx, u.ID)
	out["installed_versions"] = installed
	out["mysql"] = fiber.Map{"enabled": ms.Enabled, "running": ms.Running, "ready": ms.Ready}
	pm := p.PHP.PMAStatus(ctx)
	out["pma"] = fiber.Map{"enabled": pm.Enabled, "running": pm.Running}
	out["limits"] = fiber.Map{
		"max_databases": lim.MaxDatabases, "used_databases": usedDB,
		"max_cron_jobs": lim.MaxCronJobs, "used_cron_jobs": usedCron,
		"unlimited": u.Role == db.RoleAdmin,
	}
	out["min_cron_interval_minutes"] = p.PHP.MinCronIntervalMinutes(ctx)
	ceil := phpCeilings(ctx, u)
	out["choices"] = fiber.Map{
		"memory_limit":       ceil.MemoryChoices(),
		"sizes":              ceil.SizeChoices(),
		"max_children":       ceil.MaxChildrenFor(),
		"times":              phphosting.TimeChoices,
		"max_input_vars":     phphosting.InputVarChoices,
		"supported_versions": phphosting.SupportedVersions,
	}
	return c.JSON(out)
}

// ---- admin: instance ----

type phpVersionRow struct {
	Version     string                 `json:"version"`
	Installed   bool                   `json:"installed"`
	Sites       int                    `json:"sites"`
	Build       *phphosting.BuildState `json:"build,omitempty"`
	Status      string                 `json:"status"`
	StatusLabel string                 `json:"status_label"`
	HasCVE      bool                   `json:"has_cve"`
	CVSS        string                 `json:"cvss,omitempty"`
	CVEs        []string               `json:"cves,omitempty"`
	CVEWarning  string                 `json:"cve_warning,omitempty"`
	EOLDate     string                 `json:"eol_date,omitempty"`
	PatchedIn   string                 `json:"patched_in,omitempty"`
	IsDefault   bool                   `json:"is_default,omitempty"`
}

// GET /api/php/instance
func (p *Panel) PHPInstance(c *fiber.Ctx) error {
	ctx := c.UserContext()
	installed := map[string]bool{}
	for _, v := range p.PHP.InstalledVersions(ctx) {
		installed[v] = true
	}
	counts, _ := p.DB.CountPHPSitesByVersion(ctx)
	versions := make([]phpVersionRow, 0, len(phphosting.SupportedVersions))
	for _, v := range phphosting.SupportedVersions {
		meta := phphosting.MetaForVersion(v)
		row := phpVersionRow{
			Version:     v,
			Installed:   installed[v],
			Sites:       counts[v],
			Status:      meta.Status,
			StatusLabel: meta.StatusLabel,
			HasCVE:      meta.HasCVE,
			CVSS:        meta.CVSS,
			CVEs:        meta.CVEs,
			CVEWarning:  meta.CVEWarning,
			EOLDate:     meta.EOLDate,
			PatchedIn:   meta.PatchedIn,
			IsDefault:   meta.IsDefault,
		}
		if b, ok := p.PHP.BuildStatus(v); ok {
			bb := b
			row.Build = &bb
		}
		versions = append(versions, row)
	}
	users, _ := p.DB.ListUsers(ctx)
	type userLimit struct {
		UserID       int64  `json:"user_id"`
		Username     string `json:"username"`
		Role         string `json:"role"`
		MaxDatabases int    `json:"max_databases"`
		MaxCronJobs  int    `json:"max_cron_jobs"`
	}
	limits := []userLimit{}
	for _, u := range users {
		l := p.DB.GetPHPUserLimits(ctx, u.ID)
		limits = append(limits, userLimit{u.ID, u.Username, u.Role, l.MaxDatabases, l.MaxCronJobs})
	}
	return c.JSON(fiber.Map{
		"ok":                        true,
		"enabled":                   p.phpEnabled(ctx),
		"mysql":                     p.PHP.MySQLStatus(ctx),
		"mysql_task":                phpAdminTasks.get("mysql"),
		"pma":                       p.PHP.PMAStatus(ctx),
		"pma_task":                  phpAdminTasks.get("pma"),
		"versions":                  versions,
		"containers":                p.PHP.ListContainers(ctx),
		"min_cron_interval_minutes": p.PHP.MinCronIntervalMinutes(ctx),
		"user_limits":               limits,
	})
}

// POST /api/php/instance/module {"enabled":true}
func (p *Panel) PHPSetModule(c *fiber.Ctx) error {
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := c.BodyParser(&body); err != nil {
		return phpJSONErr(c, 400, "invalid body")
	}
	v := "0"
	if body.Enabled {
		v = "1"
	}
	if err := p.DB.SetSetting(c.UserContext(), "php_enabled", v); err != nil {
		return phpJSONErr(c, 500, err.Error())
	}
	p.RecordAuditLog(c, "php_module", "php", "module", "PHP hosting enabled="+v)
	return c.JSON(fiber.Map{"ok": true, "enabled": body.Enabled})
}

// POST /api/php/instance/mysql {"action":"enable|disable|restart","memory_mb":512}
func (p *Panel) PHPMySQLAction(c *fiber.Ctx) error {
	var body struct {
		Action       string `json:"action"`
		MemoryMB     int    `json:"memory_mb"`
		QuerySeconds *int   `json:"query_seconds"`
	}
	if err := c.BodyParser(&body); err != nil {
		return phpJSONErr(c, 400, "invalid body")
	}
	ctx := c.UserContext()
	switch body.Action {
	case "limits":
		if body.QuerySeconds == nil {
			return phpJSONErr(c, 400, "query_seconds is required")
		}
		if err := p.PHP.SetQuerySeconds(ctx, *body.QuerySeconds); err != nil {
			return phpJSONErr(c, 400, err.Error())
		}
		p.RecordAuditLog(c, "php_mysql", "php", "mysql", fmt.Sprintf("query limit %ds", *body.QuerySeconds))
		return c.JSON(fiber.Map{"ok": true})
	case "enable", "restart":
		if hostMB := int(sysinfo.Collect(ctx).MemTotalGB * 1024); hostMB > 0 && body.MemoryMB > hostMB {
			return phpJSONErr(c, 400, fmt.Sprintf("the database cannot have %d MB: this server has %d MB of RAM", body.MemoryMB, hostMB))
		}
		mem := body.MemoryMB
		started := phpAdminTasks.start("mysql", func() error {
			bg, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
			defer cancel()
			if body.Action == "restart" {
				p.PHP.StopContainerOnly(bg)
			}
			return p.PHP.EnableMySQL(bg, mem)
		})
		if !started {
			return phpJSONErr(c, 409, "the database is already being started")
		}
		p.RecordAuditLog(c, "php_mysql", "php", "mysql", body.Action)
		return c.Status(202).JSON(fiber.Map{"ok": true, "started": true})
	case "disable":
		sites, _ := p.DB.ListPHPSites(ctx)
		for _, s := range sites {
			if dbs, _ := p.DB.ListPHPDatabases(ctx, s.AppID); len(dbs) > 0 {
				return phpJSONErr(c, 409, "databases still exist; delete them (or the sites that own them) before stopping the database")
			}
		}
		if err := p.PHP.StopMySQL(ctx); err != nil {
			return phpJSONErr(c, 500, err.Error())
		}
		p.RecordAuditLog(c, "php_mysql", "php", "mysql", "disable")
		return c.JSON(fiber.Map{"ok": true})
	}
	return phpJSONErr(c, 400, "unknown action")
}

// POST /api/php/instance/versions {"version":"8.3","action":"install|remove"}
func (p *Panel) PHPVersionAction(c *fiber.Ctx) error {
	var body struct {
		Version string `json:"version"`
		Action  string `json:"action"`
	}
	if err := c.BodyParser(&body); err != nil {
		return phpJSONErr(c, 400, "invalid body")
	}
	ctx := c.UserContext()
	if !phphosting.IsSupportedVersion(body.Version) {
		return phpJSONErr(c, 400, "unsupported PHP version")
	}
	switch body.Action {
	case "install":
		if err := p.PHP.StartBuild(body.Version); err != nil {
			return phpJSONErr(c, 409, err.Error())
		}
		p.RecordAuditLog(c, "php_version", "php", body.Version, "install")
		return c.Status(202).JSON(fiber.Map{"ok": true, "started": true})
	case "remove":
		counts, _ := p.DB.CountPHPSitesByVersion(ctx)
		if n := counts[body.Version]; n > 0 {
			return phpJSONErr(c, 409, "PHP "+body.Version+" is used by "+strconv.Itoa(n)+" site(s); move them to another version first")
		}
		if err := p.PHP.RemoveImage(ctx, body.Version); err != nil {
			return phpJSONErr(c, 500, err.Error())
		}
		p.RecordAuditLog(c, "php_version", "php", body.Version, "remove")
		return c.JSON(fiber.Map{"ok": true})
	}
	return phpJSONErr(c, 400, "unknown action")
}

// GET /api/php/instance/versions/:version/build
func (p *Panel) PHPBuildStatus(c *fiber.Ctx) error {
	st, ok := p.PHP.BuildStatus(c.Params("version"))
	if !ok {
		return c.JSON(fiber.Map{"ok": true, "build": nil})
	}
	return c.JSON(fiber.Map{"ok": true, "build": st})
}

// POST /api/php/instance/containers/restart {"name":"nd-php83-u1"}
func (p *Panel) PHPRestartContainer(c *fiber.Ctx) error {
	var body struct {
		Name string `json:"name"`
	}
	if err := c.BodyParser(&body); err != nil || strings.TrimSpace(body.Name) == "" {
		return phpJSONErr(c, 400, "name required")
	}
	if err := p.PHP.RestartContainer(c.UserContext(), body.Name); err != nil {
		return phpJSONErr(c, 500, err.Error())
	}
	return c.JSON(fiber.Map{"ok": true})
}

// POST /api/php/instance/settings {"min_cron_interval_minutes":5}
func (p *Panel) PHPInstanceSettings(c *fiber.Ctx) error {
	var body struct {
		MinCronIntervalMinutes int `json:"min_cron_interval_minutes"`
	}
	if err := c.BodyParser(&body); err != nil {
		return phpJSONErr(c, 400, "invalid body")
	}
	if err := p.PHP.SetMinCronIntervalMinutes(c.UserContext(), body.MinCronIntervalMinutes); err != nil {
		return phpJSONErr(c, 400, err.Error())
	}
	return c.JSON(fiber.Map{"ok": true})
}

// POST /api/php/instance/user-limits {"user_id":2,"max_databases":5,"max_cron_jobs":10}
func (p *Panel) PHPUserLimits(c *fiber.Ctx) error {
	var body db.PHPUserLimits
	if err := c.BodyParser(&body); err != nil || body.UserID <= 0 {
		return phpJSONErr(c, 400, "invalid body")
	}
	if body.MaxDatabases < 0 || body.MaxCronJobs < 0 || body.MaxDatabases > 1000 || body.MaxCronJobs > 1000 {
		return phpJSONErr(c, 400, "limits must be between 0 and 1000 (0 = unlimited)")
	}
	if _, err := p.DB.GetUserByID(c.UserContext(), body.UserID); err != nil {
		return phpJSONErr(c, 404, "user not found")
	}
	if err := p.DB.SetPHPUserLimits(c.UserContext(), body); err != nil {
		return phpJSONErr(c, 500, err.Error())
	}
	return c.JSON(fiber.Map{"ok": true})
}

// phpAppInfo is the PHP part of an app summary in the SPA bootstrap payload.
type phpAppInfo struct {
	SiteType   string `json:"site_type"`
	PHPVersion string `json:"php_version"`
	RootDir    string `json:"root_dir"`
	UID        int    `json:"uid"`
	State      string `json:"state"` // running | stopped | none (static)
}

// phpBootstrap is the module summary the SPA needs on every page load.
func (p *Panel) phpBootstrap(ctx context.Context) fiber.Map {
	if p.PHP == nil {
		return fiber.Map{"enabled": false, "installed_versions": []string{}}
	}
	if !p.phpEnabled(ctx) {
		return fiber.Map{"enabled": false, "installed_versions": []string{}, "default_version": phphosting.DefaultPHPVersion}
	}
	return fiber.Map{
		"enabled":            true,
		"installed_versions": p.PHP.InstalledVersions(ctx),
		"default_version":    phphosting.DefaultPHPVersion,
	}
}
