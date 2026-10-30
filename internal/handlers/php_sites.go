package handlers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"panel/internal/caddy"
	"panel/internal/db"
	"panel/internal/phphosting"
	"panel/internal/quota"
	"panel/internal/sysinfo"

	"github.com/gofiber/fiber/v2"
)

const defaultPHPIndex = `<?php
// Welcome to your NextDeploy PHP site. Replace this file with your own code.
?><!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>It works</title>
<style>body{font:16px/1.5 system-ui,sans-serif;max-width:40rem;margin:12vh auto;padding:0 1rem;color:#1b1b1f}code{background:#f0f0f3;padding:.1em .35em;border-radius:4px}</style></head>
<body><h1>It works 🎉</h1>
<p>This site runs on PHP <code><?= htmlspecialchars(PHP_VERSION) ?></code>.</p>
<p>Upload your files to <code>htdocs/</code> with the File Manager.</p></body></html>
`

const defaultStaticIndex = `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>It works</title></head>
<body style="font:16px/1.5 system-ui,sans-serif;max-width:40rem;margin:12vh auto;padding:0 1rem">
<h1>It works 🎉</h1><p>Upload your files to <code>htdocs/</code> with the File Manager.</p></body></html>
`

// phpSiteFor loads the app + php site behind a request that already passed
// AppAccessMiddleware (routes live under /apps/:id/php).
func (p *Panel) phpSiteFor(c *fiber.Ctx) (db.App, db.PHPSite, error) {
	ctx := c.UserContext()
	app, err := p.DB.GetApp(ctx, c.Params("id"))
	if err != nil {
		return db.App{}, db.PHPSite{}, phpJSONErr(c, 404, "app not found")
	}
	site, err := p.DB.GetPHPSite(ctx, app.ID)
	if err != nil {
		return db.App{}, db.PHPSite{}, phpJSONErr(c, 404, "this app is not a PHP site")
	}
	return app, site, nil
}

// phpOwner returns the app owner: quotas always apply to the owner, not the caller.
func (p *Panel) phpOwner(ctx context.Context, site db.PHPSite) (db.User, error) {
	return p.DB.GetUserByID(ctx, site.OwnerID)
}

// phpCeilings is what the owner's PHP settings may not exceed: their quota (non-admins) capped by
// what the server really has, so nobody can pick 5 GB on a 1 GB machine.
func phpCeilings(ctx context.Context, u db.User) phphosting.Ceilings {
	snap := sysinfo.Collect(ctx)
	c := phphosting.Ceilings{MemoryMB: int(snap.MemTotalGB * 1024), StorageMB: int(snap.DiskTotalGB * 1024)}
	if u.Role != db.RoleAdmin {
		if u.MaxMemoryMB > 0 && (c.MemoryMB == 0 || u.MaxMemoryMB < c.MemoryMB) {
			c.MemoryMB = u.MaxMemoryMB
		}
		if u.MaxStorageMB > 0 && (c.StorageMB == 0 || u.MaxStorageMB < c.StorageMB) {
			c.StorageMB = u.MaxStorageMB
		}
	}
	return c
}

// ---- create ----

type phpCreateBody struct {
	Name         string `json:"name"`
	Type         string `json:"type"` // php | wordpress | static
	PHPVersion   string `json:"php_version"`
	Title        string `json:"title"`
	WPAdminUser  string `json:"wp_admin_user"`
	WPAdminEmail string `json:"wp_admin_email"`
}

// POST /api/php/sites
func (p *Panel) PHPCreateSite(c *fiber.Ctx) error {
	ctx := c.UserContext()
	u, ok := currentUser(c)
	if !ok {
		return phpJSONErr(c, 401, "unauthorized")
	}
	if !p.phpEnabled(ctx) {
		return phpJSONErr(c, 400, "PHP hosting is not enabled. Ask an administrator to enable it.")
	}
	var body phpCreateBody
	if err := c.BodyParser(&body); err != nil {
		return phpJSONErr(c, 400, "invalid body")
	}
	body.Type = strings.ToLower(strings.TrimSpace(body.Type))
	if body.Type == "" {
		body.Type = phphosting.SiteTypePHP
	}
	if !phphosting.IsValidSiteType(body.Type) {
		return phpJSONErr(c, 400, "type must be php, wordpress or static")
	}
	slug, err := validateAppSlug(body.Name)
	if err != nil {
		return phpJSONErr(c, 400, err.Error())
	}
	if exists, err := p.DB.PHPSiteNameExistsForUser(ctx, slug, u.ID); err != nil {
		return phpJSONErr(c, 500, err.Error())
	} else if exists {
		return phpJSONErr(c, 400, "an app with this name already exists")
	}

	version := ""
	if body.Type != phphosting.SiteTypeStatic {
		installed := p.PHP.InstalledVersions(ctx)
		if len(installed) == 0 {
			return phpJSONErr(c, 400, "no PHP version is installed yet. Ask an administrator to install one in PHP Hosting → Instance.")
		}
		version = strings.TrimSpace(body.PHPVersion)
		if version == "" {
			version = installed[0]
			for _, v := range installed {
				if v == phphosting.DefaultPHPVersion {
					version = v
				}
			}
		}
		if !phphosting.IsSupportedVersion(version) || !containsStr(installed, version) {
			return phpJSONErr(c, 400, "PHP "+version+" is not installed")
		}
	}
	// a new site writes files at once (WordPress alone is ~70 MB), so refuse when there is no room
	var needBytes int64
	if body.Type == phphosting.SiteTypeWordPress {
		needBytes = 80 << 20
	}
	if err := p.CheckStorageRoom(ctx, u, needBytes); err != nil {
		return phpJSONErr(c, 400, err.Error())
	}
	var wp phphosting.WPOptions
	if body.Type == phphosting.SiteTypeWordPress {
		if !p.PHP.MySQLStatus(ctx).Ready {
			return phpJSONErr(c, 400, "the database server is not running. Ask an administrator to enable it in PHP Hosting → Instance.")
		}
		if err := quota.CheckDatabaseSlot(ctx, p.DB, u); err != nil {
			return phpJSONErr(c, 400, err.Error())
		}
		wp.AdminUser = strings.TrimSpace(body.WPAdminUser)
		if wp.AdminUser == "" {
			wp.AdminUser = "admin"
		}
		wp.AdminEmail = strings.TrimSpace(body.WPAdminEmail)
		if wp.AdminEmail == "" {
			wp.AdminEmail = u.Username + "@example.com"
		}
		wp.Title = strings.TrimSpace(body.Title)
		if wp.Title == "" {
			wp.Title = slug
		}
		wp.AdminPass = phphosting.RandomPassword(20)
		if err := wp.Validate(); err != nil {
			return phpJSONErr(c, 400, err.Error())
		}
	}
	if err := quota.CheckAppSlot(ctx, p.DB, u); err != nil {
		return phpJSONErr(c, 400, "maximum app limit reached")
	}

	var id string
	for {
		id = fmt.Sprintf("%s-%s", slug, randomAppSuffix())
		if _, err := p.DB.GetApp(ctx, id); errors.Is(err, sql.ErrNoRows) {
			break
		}
	}
	if err := os.MkdirAll(p.Store.Path(id), 0750); err != nil {
		return phpJSONErr(c, 500, err.Error())
	}
	cleanup := func() { _ = os.RemoveAll(p.Store.Path(id)) }
	if err := p.Store.WriteMeta(id, slug); err != nil {
		cleanup()
		return phpJSONErr(c, 500, err.Error())
	}
	site := &db.PHPSite{SiteType: body.Type, PHPVersion: version, SettingsJSON: phphosting.DefaultSettings().JSON()}
	if err := quota.CreatePHPSite(ctx, p.DB, u, id, slug, site); err != nil {
		cleanup()
		return phpJSONErr(c, 400, err.Error())
	}
	rollback := func() {
		_ = p.PHP.TeardownSite(context.Background(), id)
		_ = p.DB.DeleteApp(context.Background(), id)
		cleanup()
	}
	if err := p.PHP.PrepareSiteDirs(*site); err != nil {
		rollback()
		return phpJSONErr(c, 500, err.Error())
	}
	if body.Type != phphosting.SiteTypeWordPress {
		name, content := "index.php", defaultPHPIndex
		if body.Type == phphosting.SiteTypeStatic {
			name, content = "index.html", defaultStaticIndex
		}
		_ = os.WriteFile(filepath.Join(p.PHP.HtdocsPath(id), name), []byte(content), 0640)
	}
	p.PHP.FixOwnership(*site)

	domain := ""
	targetIP, targetShieldID := p.ResolveUserPublicIP(ctx, &u)
	if targetIP != "" {
		domain = caddy.FormatWildcardDomain(id, targetIP, "sslip.io")
		if _, err := p.DB.CreateAppDomain(ctx, db.AppDomain{
			AppID: id, Domain: domain, Service: "php", Port: 80, IsPrimary: true,
			EnableHTTPS: true, RouteRulesJSON: "[]",
			ShieldNodeID: targetShieldID,
		}); err != nil {
			domain = ""
		}
	}

	sctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	if err := p.PHP.SyncSite(sctx, id); err != nil {
		rollback()
		return phpJSONErr(c, 500, "could not start the site: "+err.Error())
	}
	p.RecordAuditLog(c, "php_site_create", "app", id, "Created "+body.Type+" site "+slug)

	resp := fiber.Map{"ok": true, "id": id, "name": slug, "site_type": body.Type, "php_version": version, "domain": domain}
	if body.Type == phphosting.SiteTypeWordPress {
		url := "http://localhost"
		if domain != "" {
			url = "https://" + domain
		}
		wp.URL = url
		p.startWordPressProvision(u, *site, wp)
		resp["provisioning"] = true
	}
	return c.JSON(resp)
}

func containsStr(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

// startWordPressProvision creates the database and installs WordPress in the background.
func (p *Panel) startWordPressProvision(owner db.User, site db.PHPSite, wp phphosting.WPOptions) {
	prov := p.PHP.ProvisionStart(site.AppID)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()
		log := func(s string) { p.PHP.ProvisionLog(prov, s) }
		err := func() error {
			log("==> Creating database")
			dbName := fmt.Sprintf("u%d_wp_%s%s", owner.ID, randomAppSuffix(), randomAppSuffix())
			dbPass := phphosting.RandomPassword(24)
			if err := p.PHP.CreateDatabase(ctx, dbName, dbName, dbPass, site.AppID); err != nil {
				return err
			}
			dbID, err := p.DB.CreatePHPDatabase(ctx, db.PHPDatabase{AppID: site.AppID, DBName: dbName})
			if err != nil {
				_ = p.PHP.DropDatabase(ctx, dbName, dbName)
				return err
			}
			uid, err := p.DB.CreatePHPDBUser(ctx, site.AppID, dbName)
			if err != nil {
				_ = p.PHP.DropDatabase(ctx, dbName, dbName)
				_ = p.DB.DeletePHPDatabase(ctx, dbID)
				return err
			}
			_ = p.DB.ReplacePHPUserGrants(ctx, uid, []db.PHPDBGrant{{DBID: dbID, Access: "all"}})
			wp.DBName, wp.DBUser, wp.DBPass = dbName, dbName, dbPass
			return p.PHP.InstallWordPress(ctx, site, wp, log)
		}()
		secrets := map[string]string{"admin_user": wp.AdminUser, "admin_password": wp.AdminPass, "admin_email": wp.AdminEmail, "url": wp.URL + "/wp-admin/"}
		p.PHP.ProvisionFinish(prov, err, secrets)
	}()
}

// ---- detail ----

// GET /apps/:id/php
func (p *Panel) PHPSiteDetail(c *fiber.Ctx) error {
	ctx := c.UserContext()
	app, site, err := p.phpSiteFor(c)
	if site.AppID == "" {
		return err
	}
	owner, _ := p.phpOwner(ctx, site)
	ceil := phpCeilings(ctx, owner)
	settings := phphosting.SettingsFor(site)

	ms := p.PHP.MySQLStatus(ctx)
	dbs, _ := p.DB.ListPHPDatabases(ctx, app.ID)
	names := make([]string, 0, len(dbs))
	for _, d := range dbs {
		names = append(names, d.DBName)
	}
	sizes := map[string]int64{}
	if ms.Ready {
		sizes = p.PHP.DatabaseSizes(ctx, names)
	}
	dbUsers, _ := p.DB.ListPHPDBUsers(ctx, app.ID)
	grants, _ := p.DB.ListPHPGrants(ctx, app.ID)
	type accessRow struct {
		UserID int64  `json:"user_id"`
		User   string `json:"user"`
		Access string `json:"access"`
	}
	type dbRow struct {
		db.PHPDatabase
		SizeBytes int64       `json:"size_bytes"`
		Users     []accessRow `json:"users"`
	}
	userName := map[int64]string{}
	for _, u := range dbUsers {
		userName[u.ID] = u.UserName
	}
	dbRows := make([]dbRow, 0, len(dbs))
	for _, d := range dbs {
		row := dbRow{PHPDatabase: d, SizeBytes: sizes[d.DBName], Users: []accessRow{}}
		for _, g := range grants {
			if g.DBID == d.ID {
				row.Users = append(row.Users, accessRow{g.UserID, userName[g.UserID], g.Access})
			}
		}
		dbRows = append(dbRows, row)
	}
	type userRow struct {
		db.PHPDBUser
		Grants []db.PHPDBGrant `json:"grants"`
	}
	userRows := make([]userRow, 0, len(dbUsers))
	for _, u := range dbUsers {
		row := userRow{PHPDBUser: u, Grants: []db.PHPDBGrant{}}
		for _, g := range grants {
			if g.UserID == u.ID {
				row.Grants = append(row.Grants, g)
			}
		}
		userRows = append(userRows, row)
	}
	cron, _ := p.DB.ListPHPCronJobs(ctx, app.ID)
	if cron == nil {
		cron = []db.PHPCronJob{} // JSON [] rather than null
	}

	runtime := fiber.Map{"container": "", "state": "none"}
	if site.SiteType != phphosting.SiteTypeStatic {
		name := phphosting.RuntimeContainer(site.OwnerID, site.PHPVersion)
		state := "missing"
		for _, ct := range p.PHP.ListContainers(ctx) {
			if ct.Name == name {
				state = ct.State
			}
		}
		runtime = fiber.Map{"container": name, "state": state}
	}
	var prov any // null unless a WordPress install ran in this panel session
	if st, ok := p.PHP.ProvisionState(app.ID, false); ok {
		prov = st
	}
	lim := p.DB.GetPHPUserLimits(ctx, owner.ID)
	usedDB, _ := p.DB.CountPHPDatabasesForOwner(ctx, owner.ID)
	usedCron, _ := p.DB.CountPHPCronJobsForOwner(ctx, owner.ID)

	// Detect installed WordPress cache plugins and generated disk cache files
	var wpCachePlugins []fiber.Map
	if site.SiteType == phphosting.SiteTypeWordPress {
		htdocsPath := filepath.Join(p.WorkspacesRoot, app.ID, "htdocs")
		if site.RootDir != "" {
			htdocsPath = filepath.Join(htdocsPath, site.RootDir)
		}
		checkPlugin := func(slug, name, cacheRelPath string) fiber.Map {
			pluginDir := filepath.Join(htdocsPath, "wp-content", "plugins", slug)
			installed := false
			if fi, err := os.Stat(pluginDir); err == nil && fi.IsDir() {
				installed = true
			}
			cacheDir := filepath.Join(htdocsPath, "wp-content", "cache", cacheRelPath)
			hasCache := false
			if fi, err := os.Stat(cacheDir); err == nil && fi.IsDir() {
				hasCache = true
			}
			return fiber.Map{
				"slug":       slug,
				"name":       name,
				"installed":  installed,
				"has_cache":  hasCache,
				"cache_path": "/wp-content/cache/" + cacheRelPath,
			}
		}
		wpCachePlugins = []fiber.Map{
			checkPlugin("wp-fastest-cache", "WP Fastest Cache", "all"),
			checkPlugin("wp-super-cache", "WP Super Cache", "supercache"),
			checkPlugin("cache-enabler", "Cache Enabler", "cache-enabler"),
			checkPlugin("wp-rocket", "WP Rocket", "wp-rocket"),
		}
	}

	return c.JSON(fiber.Map{
		"ok":                        true,
		"id":                        app.ID,
		"site":                      site,
		"settings":                  settings,
		"doc_root":                  phphosting.DocRoot(app.ID, site.RootDir),
		"runtime":                   runtime,
		"mysql":                     fiber.Map{"enabled": ms.Enabled, "running": ms.Running, "ready": ms.Ready, "host": phphosting.MySQLContainer, "port": 3306},
		"pma":                       fiber.Map{"enabled": p.PHP.PMAEnabled(ctx)},
		"databases":                 dbRows,
		"db_users":                  userRows,
		"cron_jobs":                 cron,
		"provision":                 prov,
		"installed_versions":        p.PHP.InstalledVersions(ctx),
		"min_cron_interval_minutes": p.PHP.MinCronIntervalMinutes(ctx),
		"wp_cache_plugins":          wpCachePlugins,
		"limits": fiber.Map{
			"max_memory_mb":  ceil.MemoryMB,
			"max_storage_mb": ceil.StorageMB,
			"max_children":   ceil.MaxChildrenFor(),
			"max_databases":  lim.MaxDatabases, "used_databases": usedDB,
			"max_cron_jobs": lim.MaxCronJobs, "used_cron_jobs": usedCron,
			"unlimited": owner.Role == db.RoleAdmin,
		},
	})
}

// ---- settings ----

type phpSettingsBody struct {
	PHPVersion        *string `json:"php_version"`
	RootDir           *string `json:"root_dir"`
	MemoryLimit       *string `json:"memory_limit"`
	MaxExecutionTime  *int    `json:"max_execution_time"`
	MaxInputTime      *int    `json:"max_input_time"`
	MaxInputVars      *int    `json:"max_input_vars"`
	PostMaxSize       *string `json:"post_max_size"`
	UploadMaxFilesize *string `json:"upload_max_filesize"`
	MaxChildren       *int    `json:"max_children"`
	ProcessManager      *string `json:"process_manager"`
	AllowShellFunctions *bool   `json:"allow_shell_functions"`
	DisableFunctions    *string `json:"disable_functions"`
	Additional          *string `json:"additional"`
}

// POST /apps/:id/php/settings
func (p *Panel) PHPSiteSettings(c *fiber.Ctx) error {
	ctx := c.UserContext()
	app, site, err := p.phpSiteFor(c)
	if site.AppID == "" {
		return err
	}
	var body phpSettingsBody
	if err := c.BodyParser(&body); err != nil {
		return phpJSONErr(c, 400, "invalid body")
	}
	owner, err := p.phpOwner(ctx, site)
	if err != nil {
		return phpJSONErr(c, 500, err.Error())
	}
	s := phphosting.SettingsFor(site)
	if body.MemoryLimit != nil {
		s.MemoryLimit = *body.MemoryLimit
	}
	if body.MaxExecutionTime != nil {
		s.MaxExecutionTime = *body.MaxExecutionTime
	}
	if body.MaxInputTime != nil {
		s.MaxInputTime = *body.MaxInputTime
	}
	if body.MaxInputVars != nil {
		s.MaxInputVars = *body.MaxInputVars
	}
	if body.PostMaxSize != nil {
		s.PostMaxSize = *body.PostMaxSize
	}
	if body.UploadMaxFilesize != nil {
		s.UploadMaxFilesize = *body.UploadMaxFilesize
	}
	if body.MaxChildren != nil {
		s.MaxChildren = *body.MaxChildren
	}
	if body.ProcessManager != nil {
		s.ProcessManager = *body.ProcessManager
	}
	if body.AllowShellFunctions != nil {
		s.AllowShellFunctions = *body.AllowShellFunctions
	}
	if body.DisableFunctions != nil {
		s.DisableFunctions = *body.DisableFunctions
	}
	if body.Additional != nil {
		s.Additional = *body.Additional
	}
	if err := s.Validate(phpCeilings(ctx, owner)); err != nil {
		return phpJSONErr(c, 400, err.Error())
	}
	rootChanged := false
	if body.RootDir != nil {
		rd, err := phphosting.ValidateRootDir(*body.RootDir)
		if err != nil {
			return phpJSONErr(c, 400, err.Error())
		}
		rootChanged = rd != site.RootDir
		site.RootDir = rd
	}
	versionChange := ""
	if body.PHPVersion != nil && *body.PHPVersion != "" && *body.PHPVersion != site.PHPVersion {
		if site.SiteType == phphosting.SiteTypeStatic {
			return phpJSONErr(c, 400, "static sites do not use PHP")
		}
		versionChange = *body.PHPVersion
	}
	site.SettingsJSON = s.JSON()
	if err := p.DB.UpdatePHPSite(ctx, site); err != nil {
		return phpJSONErr(c, 500, err.Error())
	}
	sctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	if versionChange != "" {
		if err := p.PHP.ChangeVersion(sctx, app.ID, versionChange); err != nil {
			return phpJSONErr(c, 400, err.Error())
		}
	} else if err := p.PHP.SyncSite(sctx, app.ID); err != nil {
		return phpJSONErr(c, 500, err.Error())
	}
	if rootChanged && versionChange != "" {
		_ = p.PHP.SyncRoutes(sctx, app.ID)
	}
	p.RecordAuditLog(c, "php_site_settings", "app", app.ID, "Updated PHP settings")
	return c.JSON(fiber.Map{"ok": true})
}

// POST /apps/:id/php/restart — restart the PHP runtime container
func (p *Panel) PHPSiteRestart(c *fiber.Ctx) error {
	_, site, err := p.phpSiteFor(c)
	if site.AppID == "" {
		return err
	}
	if site.SiteType == phphosting.SiteTypeStatic {
		return phpJSONErr(c, 400, "static sites have no PHP runtime")
	}
	if err := p.PHP.RestartContainer(c.UserContext(), phphosting.RuntimeContainer(site.OwnerID, site.PHPVersion)); err != nil {
		// not running at all: recreate it
		if e2 := p.PHP.SyncSite(c.UserContext(), site.AppID); e2 != nil {
			return phpJSONErr(c, 500, e2.Error())
		}
	}
	return c.JSON(fiber.Map{"ok": true})
}

// POST /apps/:id/php/fix-permissions
func (p *Panel) PHPSiteFixPermissions(c *fiber.Ctx) error {
	_, site, err := p.phpSiteFor(c)
	if site.AppID == "" {
		return err
	}
	p.PHP.FixOwnership(site)
	return c.JSON(fiber.Map{"ok": true})
}

// GET /apps/:id/php/provision — WordPress install progress; ?consume=1 returns the one-time credentials.
func (p *Panel) PHPSiteProvision(c *fiber.Ctx) error {
	_, site, err := p.phpSiteFor(c)
	if site.AppID == "" {
		return err
	}
	if u, ok := currentUser(c); ok {
		if allowed, _ := p.CanAccessApp(c.UserContext(), u.ID, u.Role, site.AppID, db.CollabRoleDeveloper); !allowed {
			return phpJSONErr(c, 403, "forbidden: developer access required")
		}
	}
	st, ok := p.PHP.ProvisionState(site.AppID, c.Query("consume") == "1")
	if !ok {
		return c.JSON(fiber.Map{"ok": true, "provision": nil})
	}
	return c.JSON(fiber.Map{"ok": true, "provision": st})
}

// PHPPurgeCache deletes all cached static files inside htdocs/wp-content/cache.
func (p *Panel) PHPPurgeCache(c *fiber.Ctx) error {
	app, _, err := p.phpSiteFor(c)
	if err != nil {
		return err
	}
	cacheDir := filepath.Join(p.WorkspacesRoot, app.ID, "htdocs", "wp-content", "cache")
	if fi, err := os.Stat(cacheDir); err == nil && fi.IsDir() {
		entries, err := os.ReadDir(cacheDir)
		if err == nil {
			for _, entry := range entries {
				_ = os.RemoveAll(filepath.Join(cacheDir, entry.Name()))
			}
		}
	}
	return c.JSON(fiber.Map{"ok": true, "message": "Cache purged successfully"})
}

// phpOwnershipMiddleware re-owns files after any write through the panel so the FPM pool
// (which runs as the site's own UID) can read and write them.
func (p *Panel) PHPOwnershipMiddleware(c *fiber.Ctx) error {
	err := c.Next()
	if p.PHP == nil || c.Method() == fiber.MethodGet || c.Method() == fiber.MethodHead || c.Response().StatusCode() >= 400 {
		return err
	}
	id := c.Params("id")
	if id != "" && p.DB.IsPHPSite(c.UserContext(), id) {
		p.PHP.DebounceFixOwnership(id)
	}
	return err
}

// deletePHPSiteResources removes a PHP site: route holder, databases, runtime mount,
// then the app record and workspace.
func (p *Panel) deletePHPSiteResources(ctx context.Context, id string) error {
	if err := p.PHP.TeardownSite(ctx, id); err != nil {
		return err
	}
	p.RemoveDeployRun(id)
	if err := p.DB.DeleteApp(ctx, id); err != nil {
		return err
	}
	p.InvalidateAfterAppDeployChange(id)
	p.CleanupWorkspaceDir(id)
	return nil
}
