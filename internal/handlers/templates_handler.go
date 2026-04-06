package handlers

import (
	"context"
	"encoding/json"
	"database/sql"
	"fmt"
	"html/template"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"sort"
	"strings"
	"time"

	"panel/internal/caddy"
	"panel/internal/db"
	"panel/internal/dockerx"
	"panel/internal/phppanel"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

type phpPanelServiceStatus struct {
	Label      string
	Service    string
	Running    bool
	CanStart   bool
	CanStop    bool
	CanRestart bool
}

type phpPanelVersionOption struct {
	Version string
	Label   string
}

type phpPanelDatabaseInfo struct {
	Name string
}

type phpPanelDatabaseUserInfo struct {
	User       string
	Host       string
	Key        string
	Access     string
	Grants     []phpPanelDatabaseGrantInfo
	Credential bool
}

type phpPanelSiteBrowserContext struct {
	Site     db.PHPPanelSite
	BasePath string
}

type phpPanelDatabaseGrantInfo struct {
	Database   string
	Privileges string
}

type phpPanelOwnerSummary struct {
	User          db.User
	Account       db.PHPPanelAccount
	SiteCount     int
	DatabaseCount int
	DomainCount   int
	TotalSites    int
	TotalDatabases int
}

func newPHPPanelServiceStatus(label, service string, running bool) phpPanelServiceStatus {
	return phpPanelServiceStatus{
		Label:      label,
		Service:    service,
		Running:    running,
		CanStart:   !running,
		CanStop:    running,
		CanRestart: running,
	}
}

func phpPanelVersionOptions(versions []string) []phpPanelVersionOption {
	out := make([]phpPanelVersionOption, 0, len(versions))
	for _, version := range versions {
		version = phppanel.NormalizePHPVersion(version)
		out = append(out, phpPanelVersionOption{
			Version: version,
			Label:   "PHP " + version,
		})
	}
	return out
}

func sanitizeMySQLIdentifier(raw string) string {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range raw {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '_':
			b.WriteRune(r)
		}
	}
	return b.String()
}

func mysqlQuotedIdentifier(name string) string {
	return "`" + strings.ReplaceAll(strings.TrimSpace(name), "`", "``") + "`"
}

func mysqlStringLiteral(raw string) string {
	raw = strings.ReplaceAll(raw, "\\", "\\\\")
	raw = strings.ReplaceAll(raw, "'", "''")
	return "'" + raw + "'"
}

func parseMySQLAccountKey(raw string) (string, string, bool) {
	raw = strings.TrimSpace(raw)
	parts := strings.SplitN(raw, "@", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	user := sanitizeMySQLIdentifier(parts[0])
	host := strings.TrimSpace(parts[1])
	if user == "" || host == "" {
		return "", "", false
	}
	return user, host, true
}

func sanitizeMySQLHost(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if raw == "%" || raw == "localhost" || raw == "php_mysql" {
		return raw
	}
	for _, r := range raw {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '.', r == '-', r == ':', r == '%':
		default:
			return ""
		}
	}
	return raw
}

func phpPanelPrivilegeOptions() []string {
	return []string{"SELECT", "INSERT", "UPDATE", "DELETE", "CREATE", "ALTER", "INDEX", "DROP"}
}

func selectedPHPPanelPrivileges(c *fiber.Ctx) []string {
	var privileges []string
	c.Request().PostArgs().VisitAll(func(key, val []byte) {
		if string(key) == "privileges" {
			privileges = append(privileges, strings.TrimSpace(string(val)))
		}
	})
	return compactStrings(privileges)
}

func composeURL(domain string, https bool) string {
	domain = strings.TrimSpace(domain)
	if domain == "" {
		return ""
	}
	if https {
		return "https://" + domain
	}
	return "http://" + domain
}

func phpPanelSiteBasePath(siteSlug string) string {
	return strings.Trim(strings.TrimSpace(filepath.ToSlash(path.Join("sites", siteSlug, "public_html"))), "/")
}

func (p *Panel) phpPanelMySQLExec(ctx context.Context, app db.App, sqlInput string) dockerx.Result {
	project := p.activeComposeProjectName(ctx, app, app.ID)
	dir := p.appSourcePath(ctx, app.ID)
	return dockerx.ComposeExecServiceInput(
		ctx,
		dir,
		p.effectiveComposePaths(ctx, app, app.ID),
		project,
		p.composeEnvFiles(ctx, app.ID),
		"php_mysql",
		`MYSQL_PWD="$MYSQL_ROOT_PASSWORD" mysql -N -B -uroot`,
		sqlInput,
	)
}

func mysqlOutputLines(output string) []string {
	rawLines := strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n")
	lines := make([]string, 0, len(rawLines))
	for _, line := range rawLines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lines = append(lines, line)
	}
	return lines
}

func phpPanelOwnerUser(c *fiber.Ctx) db.User {
	if u, ok := c.Locals("php_panel_owner").(db.User); ok && u.ID > 0 {
		return u
	}
	u, _ := currentUser(c)
	return u
}

func phpPanelPrivilegesLabel(raw string) string {
	var items []string
	_ = json.Unmarshal([]byte(strings.TrimSpace(raw)), &items)
	for i := range items {
		items[i] = strings.TrimSpace(items[i])
	}
	items = compactStrings(items)
	if len(items) == 0 {
		return "No database access yet"
	}
	sort.Strings(items)
	return strings.Join(items, ", ")
}

func compactStrings(in []string) []string {
	out := make([]string, 0, len(in))
	seen := map[string]struct{}{}
	for _, item := range in {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

func (p *Panel) listOwnedPHPPanelDomains(ctx context.Context, appID string, ownerID int64) ([]db.AppDomain, map[int64]db.TemplateAppDomain, error) {
	domainOwners, err := p.DB.ListPHPPanelDomainOwners(ctx, appID, ownerID)
	if err != nil {
		return nil, nil, err
	}
	allowed := map[int64]struct{}{}
	for _, item := range domainOwners {
		allowed[item.AppDomainID] = struct{}{}
	}
	allDomains, err := p.DB.ListAppDomains(ctx, appID)
	if err != nil {
		return nil, nil, err
	}
	templateDomains, err := p.DB.ListTemplateAppDomains(ctx, appID)
	if err != nil {
		return nil, nil, err
	}
	templateMap := map[int64]db.TemplateAppDomain{}
	for _, item := range templateDomains {
		if _, ok := allowed[item.AppDomainID]; ok {
			templateMap[item.AppDomainID] = item
		}
	}
	out := make([]db.AppDomain, 0, len(allDomains))
	for i := range allDomains {
		if _, ok := allowed[allDomains[i].ID]; !ok {
			continue
		}
		sanitizeDomainRecord(&allDomains[i])
		out = append(out, allDomains[i])
	}
	return out, templateMap, nil
}

func (p *Panel) ownedPHPPanelSummary(ctx context.Context, appID string, owner db.User) phpPanelOwnerSummary {
	account, err := p.DB.GetPHPPanelAccount(ctx, owner.ID)
	if err != nil {
		account = db.PHPPanelAccount{
			UserID:        owner.ID,
			Enabled:       owner.Role == db.RoleAdmin,
			SiteLimit:     3,
			DatabaseLimit: 3,
		}
	}
	siteCount, _ := p.DB.CountOwnedPHPPanelSites(ctx, appID, owner.ID)
	databaseCount, _ := p.DB.CountOwnedPHPPanelDatabases(ctx, appID, owner.ID)
	domainOwners, _ := p.DB.ListPHPPanelDomainOwners(ctx, appID, owner.ID)
	totalSites := 0
	if allSites, err := p.DB.ListPHPPanelSites(ctx, appID); err == nil {
		totalSites = len(allSites)
	}
	return phpPanelOwnerSummary{
		User:           owner,
		Account:        account,
		SiteCount:      siteCount,
		DatabaseCount:  databaseCount,
		DomainCount:    len(domainOwners),
		TotalSites:     totalSites,
	}
}

func (p *Panel) phpPanelTotalDatabaseCount(ctx context.Context, app db.App) int {
	res := p.phpPanelMySQLExec(ctx, app, `SELECT COUNT(*) FROM information_schema.SCHEMATA
WHERE SCHEMA_NAME NOT IN ('information_schema','mysql','performance_schema','sys');`)
	if res.OK {
		lines := mysqlOutputLines(res.Output)
		if len(lines) > 0 {
			if n, err := strconv.Atoi(strings.TrimSpace(lines[0])); err == nil {
				return n
			}
		}
	}
	n, _ := p.DB.CountPHPPanelDatabases(ctx, app.ID)
	return n
}

func (p *Panel) phpPanelLoadDatabaseState(ctx context.Context, app db.App, ownerID int64) ([]phpPanelDatabaseInfo, []phpPanelDatabaseUserInfo, string) {
	pingRes := p.phpPanelMySQLExec(ctx, app, "SELECT 1;\n")
	if !pingRes.OK {
		return nil, nil, strings.TrimSpace(pingRes.Output)
	}
	ownedDatabases, err := p.DB.ListPHPPanelDatabasesByOwner(ctx, app.ID, ownerID)
	if err != nil {
		return nil, nil, err.Error()
	}
	ownedUsers, err := p.DB.ListPHPPanelDBUsersByOwner(ctx, app.ID, ownerID)
	if err != nil {
		return nil, nil, err.Error()
	}
	databases := make([]phpPanelDatabaseInfo, 0, len(ownedDatabases))
	for _, item := range ownedDatabases {
		databases = append(databases, phpPanelDatabaseInfo{Name: item.DatabaseName})
	}
	users := make([]phpPanelDatabaseUserInfo, 0, len(ownedUsers))
	for _, item := range ownedUsers {
		grants, _ := p.DB.ListPHPPanelDBGrantsForUser(ctx, item.ID)
		var access []string
		grantCards := make([]phpPanelDatabaseGrantInfo, 0, len(grants))
		for _, grant := range grants {
			privileges := phpPanelPrivilegesLabel(grant.PrivilegesJSON)
			label := grant.DatabaseName
			if privileges != "No database access yet" {
				label = grant.DatabaseName + " (" + privileges + ")"
			}
			access = append(access, label)
			grantCards = append(grantCards, phpPanelDatabaseGrantInfo{
				Database:   grant.DatabaseName,
				Privileges: privileges,
			})
		}
		sort.Strings(access)
		users = append(users, phpPanelDatabaseUserInfo{
			User:       item.Username,
			Host:       item.Host,
			Key:        item.Username + "@" + item.Host,
			Grants:     grantCards,
			Credential: strings.TrimSpace(item.PasswordEncrypted) != "",
			Access: func() string {
				if len(access) == 0 {
					return "No database access yet"
				}
				return strings.Join(access, ", ")
			}(),
		})
	}
	return databases, users, ""
}

func (p *Panel) TemplatesPage(c *fiber.Ctx) error {
	app, err := p.DB.GetTemplateAppByTemplateID(c.UserContext(), phppanel.TemplateID)
	hasApp := err == nil
	return c.Render("pages/templates", withUser(c, fiber.Map{
		"Nav":       "templates",
		"Title":     "Templates",
		"HasPHPApp": hasApp,
		"PHPApp":    app,
	}), "layouts/shell")
}

func (p *Panel) TemplatesLaunchPHPPanel(c *fiber.Ctx) error {
	ctx := c.UserContext()
	user, _ := currentUser(c)
	if app, err := p.DB.GetTemplateAppByTemplateID(ctx, phppanel.TemplateID); err == nil {
		return c.Redirect("/php-panel/" + app.ID)
	}

	name := strings.TrimSpace(c.FormValue("name"))
	if name == "" {
		name = "PHP Panel"
	}
	id := uuid.NewString()
	if err := os.MkdirAll(p.Store.Path(id), 0750); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	if err := p.Store.WriteMeta(id, name); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	if err := p.DB.CreateApp(ctx, id, name); err != nil {
		_ = os.RemoveAll(p.Store.Path(id))
		return c.Status(500).SendString(err.Error())
	}
	if err := p.DB.SetAppTemplate(ctx, id, phppanel.TemplateID); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	if err := p.DB.SetAppSourceType(ctx, id, "template"); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	if err := p.DB.UpdateComposeFile(ctx, id, phppanel.DefaultComposeFile); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	if err := p.DB.UpdatePanelEnv(ctx, id, "MYSQL_ROOT_PASSWORD=changeme_please\nPMA_PORT=8081\n"); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	if err := phppanel.SeedWorkspace(p.Store.Path(id), name); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	_ = p.DB.EnsurePHPPanelAccount(ctx, user.ID, true, 3, 3)
	if _, err := p.DB.CreateOwnedPHPPanelSite(ctx, id, user.ID, "Default Site", "index", "8.3"); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	return c.Redirect("/php-panel/" + id)
}

func (p *Panel) PHPPanelPage(c *fiber.Ctx) error {
	id := c.Params("id")
	app, err := p.DB.GetApp(c.UserContext(), id)
	if err != nil {
		return c.Status(404).SendString("app not found")
	}
	if !phppanel.AppIsPHPPanel(app) {
		return c.Redirect("/apps/" + id)
	}
	_ = phppanel.EnsurePHPMyAdminConfig(p.Store.Path(id))
	user, _ := currentUser(c)
	owner := phpPanelOwnerUser(c)
	tab := strings.TrimSpace(c.Query("tab"))
	if tab == "" {
		tab = "overview"
	}
	allowedTabs := map[string]bool{
		"overview":  true,
		"sites":     true,
		"databases": true,
		"domains":   true,
		"files":     true,
	}
	if user.Role == db.RoleAdmin && c.Locals("ScopedPHPPanelOnly") != true {
		allowedTabs["settings"] = true
	}
	if !allowedTabs[tab] {
		tab = "overview"
	}
	sites, _ := p.DB.ListPHPPanelSitesByOwner(c.UserContext(), id, owner.ID)
	rows, _ := p.phpPanelComposeRows(c.UserContext(), app)
	running := map[string]bool{}
	for _, row := range rows {
		if strings.EqualFold(strings.TrimSpace(row.State), "running") {
			running[row.Service] = true
		}
	}
	serviceCards := []phpPanelServiceStatus{
		newPHPPanelServiceStatus("PHP 7.4", "php_fpm_74", running["php_fpm_74"]),
		newPHPPanelServiceStatus("PHP 8.1", "php_fpm_81", running["php_fpm_81"]),
		newPHPPanelServiceStatus("PHP 8.2", "php_fpm_82", running["php_fpm_82"]),
		newPHPPanelServiceStatus("PHP 8.3", "php_fpm_83", running["php_fpm_83"]),
		newPHPPanelServiceStatus("MySQL", "php_mysql", running["php_mysql"]),
		newPHPPanelServiceStatus("phpMyAdmin", "php_pma", running["php_pma"]),
	}
	runningPHPVersions := make([]string, 0, len(phppanel.SupportedPHPVersions))
	for _, version := range phppanel.SupportedPHPVersions {
		if running[phppanel.ServiceForVersion(version)] {
			runningPHPVersions = append(runningPHPVersions, version)
		}
	}
	domains, templateDomainMap, _ := p.listOwnedPHPPanelDomains(c.UserContext(), id, owner.ID)
	phpMyAdminURL := ""
	for i := range domains {
		if phpMyAdminURL == "" && strings.EqualFold(domains[i].Service, "php_pma") {
			phpMyAdminURL = "/php-panel/" + id + "/phpmyadmin/open"
		}
	}
	ownerSummary := p.ownedPHPPanelSummary(c.UserContext(), id, owner)
	if user.Role == db.RoleAdmin {
		ownerSummary.TotalDatabases = p.phpPanelTotalDatabaseCount(c.UserContext(), app)
	}
	m := fiber.Map{
		"Nav":               "templates",
		"Title":             app.Name,
		"App":               app,
		"AppID":             id,
		"Tab":               tab,
		"RouteBase":         "/php-panel/" + id,
		"Sites":             sites,
		"Domains":           domains,
		"TemplateDomains":   templateDomainMap,
		"SupportedVersions": phppanel.SupportedPHPVersions,
		"RunningPHPVersions": runningPHPVersions,
		"RunningPHPVersionOptions": phpPanelVersionOptions(runningPHPVersions),
		"ServiceRunning":    running,
		"PHPServices":       serviceCards,
		"DBHost":            "php_mysql",
		"DBPort":            3306,
		"DBPrivilegeOptions": phpPanelPrivilegeOptions(),
		"PHPMyAdminURL":     phpMyAdminURL,
		"OwnerSummary":      ownerSummary,
		"OwnerUser":         owner,
		"UploadMaxMB":       p.uploadMaxMB(c.UserContext()),
		"Flash":             c.Query("flash"),
		"Error":             c.Query("error"),
	}
	if tab == "databases" {
		if running["php_mysql"] {
			databases, users, mysqlError := p.phpPanelLoadDatabaseState(c.UserContext(), app, owner.ID)
			m["DatabasesList"] = databases
			m["DatabaseUsers"] = users
			m["MySQLError"] = mysqlError
		} else {
			m["MySQLError"] = "MySQL is stopped. Start `php_mysql` from Settings to manage databases."
		}
	}
	if tab == "files" {
		selectedSlug := strings.TrimSpace(c.Query("site"))
		if selectedSlug == "" && len(sites) > 0 {
			selectedSlug = sites[0].Slug
		}
		if selectedSlug != "" {
			site, siteErr := p.DB.GetPHPPanelSiteBySlugAndOwner(c.UserContext(), id, selectedSlug, owner.ID)
			if siteErr == nil {
				m["BrowserSite"] = phpPanelSiteBrowserContext{
					Site:     site,
					BasePath: phpPanelSiteBasePath(site.Slug),
				}
			}
		}
	}
	var view string
	switch tab {
	case "files":
		view = "pages/php_panel"
	default:
		view = "pages/php_panel"
	}
	return c.Render(view, withUser(c, m), "layouts/shell")
}

func (p *Panel) PHPPanelCreateSite(c *fiber.Ctx) error {
	id := c.Params("id")
	owner := phpPanelOwnerUser(c)
	name := strings.TrimSpace(c.FormValue("name"))
	slug := sanitizeProjectName(name)
	if slug == "" {
		return c.Redirect("/php-panel/" + id + "?tab=sites&error=Invalid+site+name")
	}
	version := phppanel.NormalizePHPVersion(c.FormValue("php_version"))
	app, err := p.DB.GetApp(c.UserContext(), id)
	if err != nil {
		return c.Redirect("/php-panel/" + id + "?tab=sites&error=App+not+found")
	}
	rows, _ := p.phpPanelComposeRows(c.UserContext(), app)
	running := map[string]bool{}
	for _, row := range rows {
		if strings.EqualFold(strings.TrimSpace(row.State), "running") {
			running[row.Service] = true
		}
	}
	if !running[phppanel.ServiceForVersion(version)] {
		return c.Redirect("/php-panel/" + id + "?tab=sites&error=Start+that+PHP+version+from+Settings+first")
	}
	if existing, err := p.DB.GetPHPPanelSiteBySlug(c.UserContext(), id, slug); err == nil {
		if existing.UserID == owner.ID {
			return c.Redirect("/php-panel/" + id + "?tab=sites&error=Site+already+exists")
		}
		return c.Redirect("/php-panel/" + id + "?tab=sites&error=That+site+slug+is+already+owned+by+another+user")
	}
	account, _ := p.DB.GetPHPPanelAccount(c.UserContext(), owner.ID)
	siteCount, _ := p.DB.CountOwnedPHPPanelSites(c.UserContext(), id, owner.ID)
	if account.SiteLimit > 0 && siteCount >= account.SiteLimit {
		return c.Redirect("/php-panel/" + id + "?tab=sites&error=Site+limit+reached")
	}
	root := filepath.Join(p.Store.Path(id), "sites", slug, "public_html")
	if err := os.MkdirAll(root, 0750); err != nil {
		return c.Redirect("/php-panel/" + id + "?tab=sites&error=Could+not+create+site+folder")
	}
	p.ensurePHPPanelPublicReadable(id, filepath.ToSlash(filepath.Join("sites", slug, "public_html")), true)
	index := fmt.Sprintf("<?php\necho '<h1>%s</h1>';\necho '<p>PHP ' . PHP_VERSION . '</p>';\n", name)
	if err := os.WriteFile(filepath.Join(root, "index.php"), []byte(index), 0640); err != nil {
		return c.Redirect("/php-panel/" + id + "?tab=sites&error=Could+not+write+index")
	}
	p.ensurePHPPanelPublicReadable(id, filepath.ToSlash(filepath.Join("sites", slug, "public_html", "index.php")), false)
	if _, err := p.DB.CreateOwnedPHPPanelSite(c.UserContext(), id, owner.ID, name, slug, version); err != nil {
		return c.Redirect("/php-panel/" + id + "?tab=sites&error=Could+not+save+site")
	}
	return c.Redirect("/php-panel/" + id + "?tab=sites&flash=Site+created")
}

func (p *Panel) PHPPanelUpdateSiteVersion(c *fiber.Ctx) error {
	id := c.Params("id")
	owner := phpPanelOwnerUser(c)
	slug := strings.TrimSpace(c.FormValue("slug"))
	version := phppanel.NormalizePHPVersion(c.FormValue("php_version"))
	if slug == "" {
		return c.Redirect("/php-panel/" + id + "?tab=sites&error=Site+required")
	}
	app, err := p.DB.GetApp(c.UserContext(), id)
	if err != nil {
		return c.Redirect("/php-panel/" + id + "?tab=sites&error=App+not+found")
	}
	rows, _ := p.phpPanelComposeRows(c.UserContext(), app)
	running := map[string]bool{}
	for _, row := range rows {
		if strings.EqualFold(strings.TrimSpace(row.State), "running") {
			running[row.Service] = true
		}
	}
	if !running[phppanel.ServiceForVersion(version)] {
		return c.Redirect("/php-panel/" + id + "?tab=sites&error=Start+that+PHP+version+from+Settings+first")
	}
	if err := p.DB.UpdateOwnedPHPPanelSiteVersion(c.UserContext(), id, slug, owner.ID, version); err != nil {
		return c.Redirect("/php-panel/" + id + "?tab=sites&error=Could+not+update+PHP+version")
	}
	if err := p.syncPHPPanelDomains(c, id); err != nil {
		return c.Redirect("/php-panel/" + id + "?tab=sites&error=Could+not+refresh+proxy")
	}
	return c.Redirect("/php-panel/" + id + "?tab=sites&flash=PHP+version+updated")
}

func (p *Panel) PHPPanelDeleteSite(c *fiber.Ctx) error {
	id := c.Params("id")
	owner := phpPanelOwnerUser(c)
	slug := strings.TrimSpace(c.FormValue("slug"))
	if slug == "" {
		return c.Redirect("/php-panel/" + id + "?tab=sites&error=Site+required")
	}
	if !p.DB.HasOwnedPHPPanelSite(c.UserContext(), id, slug, owner.ID) {
		return c.Redirect("/php-panel/" + id + "?tab=sites&error=Site+not+found")
	}
	_ = os.RemoveAll(filepath.Join(p.Store.Path(id), "sites", slug))
	_ = p.DB.DeleteOwnedPHPPanelSite(c.UserContext(), id, slug, owner.ID)
	templateDomains, _ := p.DB.ListTemplateAppDomains(c.UserContext(), id)
	for _, item := range templateDomains {
		if strings.EqualFold(item.SiteSlug, slug) {
			ownerRec, err := p.DB.GetPHPPanelDomainOwnerByDomainID(c.UserContext(), item.AppDomainID)
			if err == nil && ownerRec.UserID != owner.ID {
				continue
			}
			_ = p.DB.DeletePHPPanelDomainOwnerByDomainID(c.UserContext(), item.AppDomainID)
			_ = p.DB.DeleteTemplateAppDomainByDomainID(c.UserContext(), item.AppDomainID)
			_ = p.DB.DeleteAppDomain(c.UserContext(), item.AppDomainID)
		}
	}
	_ = p.syncPHPPanelDomains(c, id)
	return c.Redirect("/php-panel/" + id + "?tab=sites&flash=Site+deleted")
}

func (p *Panel) PHPPanelDomainCreate(c *fiber.Ctx) error {
	id := c.Params("id")
	owner := phpPanelOwnerUser(c)
	slug := strings.TrimSpace(c.FormValue("site_slug"))
	domain := strings.TrimSpace(caddy.CleanQuotedValue(c.FormValue("domain")))
	if domain == "" || slug == "" {
		return c.Redirect("/php-panel/" + id + "?tab=domains&error=Domain+and+site+are+required")
	}
	site, err := p.DB.GetPHPPanelSiteBySlugAndOwner(c.UserContext(), id, slug, owner.ID)
	if err != nil {
		return c.Redirect("/php-panel/" + id + "?tab=domains&error=Invalid+site")
	}
	d := db.AppDomain{
		AppID:       id,
		Domain:      domain,
		Service:     phppanel.ServiceForVersion(site.PHPVersion),
		Port:        9000,
		EnableHTTPS: c.FormValue("enable_https") == "on",
		EnableWWW:   c.FormValue("enable_www") == "on",
	}
	domainID, err := p.DB.CreateAppDomain(c.UserContext(), d)
	if err != nil {
		return c.Redirect("/php-panel/" + id + "?tab=domains&error=Could+not+save+domain")
	}
	if err := p.DB.UpsertTemplateAppDomain(c.UserContext(), db.TemplateAppDomain{
		AppDomainID: domainID,
		AppID:       id,
		TemplateID:  phppanel.TemplateID,
		SiteSlug:    site.Slug,
		RootPath:    phppanel.SitePublicRoot(id, site.Slug),
		PHPVersion:  site.PHPVersion,
	}); err != nil {
		return c.Redirect("/php-panel/" + id + "?tab=domains&error=Could+not+save+template+domain")
	}
	if err := p.DB.UpsertPHPPanelDomainOwner(c.UserContext(), domainID, id, owner.ID); err != nil {
		return c.Redirect("/php-panel/" + id + "?tab=domains&error=Could+not+save+domain+owner")
	}
	if err := p.syncPHPPanelDomains(c, id); err != nil {
		return c.Redirect("/php-panel/" + id + "?tab=domains&error=Could+not+refresh+proxy")
	}
	return c.Redirect("/php-panel/" + id + "?tab=domains&flash=Domain+saved")
}

func (p *Panel) PHPPanelPMADomainCreate(c *fiber.Ctx) error {
	id := c.Params("id")
	owner := phpPanelOwnerUser(c)
	domain := strings.TrimSpace(caddy.CleanQuotedValue(c.FormValue("domain")))
	if domain == "" {
		return c.Redirect("/php-panel/" + id + "?tab=domains&error=Domain+is+required")
	}
	d := db.AppDomain{
		AppID:       id,
		Domain:      domain,
		Service:     "php_pma",
		Port:        80,
		EnableHTTPS: c.FormValue("enable_https") == "on",
		EnableWWW:   c.FormValue("enable_www") == "on",
	}
	domainID, err := p.DB.CreateAppDomain(c.UserContext(), d)
	if err != nil {
		return c.Redirect("/php-panel/" + id + "?tab=domains&error=Could+not+save+phpMyAdmin+domain")
	}
	if err := p.DB.UpsertPHPPanelDomainOwner(c.UserContext(), domainID, id, owner.ID); err != nil {
		return c.Redirect("/php-panel/" + id + "?tab=domains&error=Could+not+save+domain+owner")
	}
	if err := p.syncPHPPanelDomains(c, id); err != nil {
		return c.Redirect("/php-panel/" + id + "?tab=domains&error=Could+not+refresh+proxy")
	}
	return c.Redirect("/php-panel/" + id + "?tab=domains&flash=phpMyAdmin+domain+saved")
}

func (p *Panel) PHPPanelDomainDelete(c *fiber.Ctx) error {
	id := c.Params("id")
	owner := phpPanelOwnerUser(c)
	did := int64(c.QueryInt("domain_id"))
	if did <= 0 {
		return c.Redirect("/php-panel/" + id + "?tab=domains&error=Invalid+domain")
	}
	ownerRec, err := p.DB.GetPHPPanelDomainOwnerByDomainID(c.UserContext(), did)
	if err != nil || ownerRec.UserID != owner.ID {
		return c.Redirect("/php-panel/" + id + "?tab=domains&error=Domain+not+found")
	}
	if err := p.DB.DeleteAppDomain(c.UserContext(), did); err != nil {
		return c.Redirect("/php-panel/" + id + "?tab=domains&error=Could+not+delete+domain")
	}
	_ = p.DB.DeletePHPPanelDomainOwnerByDomainID(c.UserContext(), did)
	_ = p.DB.DeleteTemplateAppDomainByDomainID(c.UserContext(), did)
	if err := p.syncPHPPanelDomains(c, id); err != nil {
		return c.Redirect("/php-panel/" + id + "?tab=domains&error=Could+not+refresh+proxy")
	}
	return c.Redirect("/php-panel/" + id + "?tab=domains&flash=Domain+removed")
}

func (p *Panel) PHPPanelDatabaseCreate(c *fiber.Ctx) error {
	id := c.Params("id")
	owner := phpPanelOwnerUser(c)
	app, err := p.DB.GetApp(c.UserContext(), id)
	if err != nil {
		return c.Status(404).SendString("app not found")
	}
	name := sanitizeMySQLIdentifier(c.FormValue("database"))
	if name == "" {
		return c.Redirect("/php-panel/" + id + "?tab=databases&error=Valid+database+name+required")
	}
	if existing, err := p.DB.GetPHPPanelDatabaseByName(c.UserContext(), id, name); err == nil {
		if existing.UserID == owner.ID {
			return c.Redirect("/php-panel/" + id + "?tab=databases&error=Database+already+exists")
		}
		return c.Redirect("/php-panel/" + id + "?tab=databases&error=That+database+is+owned+by+another+user")
	}
	account, _ := p.DB.GetPHPPanelAccount(c.UserContext(), owner.ID)
	dbCount, _ := p.DB.CountOwnedPHPPanelDatabases(c.UserContext(), id, owner.ID)
	if account.DatabaseLimit > 0 && dbCount >= account.DatabaseLimit {
		return c.Redirect("/php-panel/" + id + "?tab=databases&error=Database+limit+reached")
	}
	sqlInput := fmt.Sprintf("CREATE DATABASE IF NOT EXISTS %s CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;\n", mysqlQuotedIdentifier(name))
	res := p.phpPanelMySQLExec(c.UserContext(), app, sqlInput)
	if !res.OK {
		return c.Redirect("/php-panel/" + id + "?tab=databases&error=Could+not+create+database")
	}
	if _, err := p.DB.CreatePHPPanelDatabase(c.UserContext(), id, owner.ID, name); err != nil && !strings.Contains(strings.ToLower(err.Error()), "unique") {
		return c.Redirect("/php-panel/" + id + "?tab=databases&error=Could+not+save+database+metadata")
	}
	return c.Redirect("/php-panel/" + id + "?tab=databases&flash=Database+created")
}

func (p *Panel) PHPPanelDatabaseDelete(c *fiber.Ctx) error {
	id := c.Params("id")
	owner := phpPanelOwnerUser(c)
	app, err := p.DB.GetApp(c.UserContext(), id)
	if err != nil {
		return c.Status(404).SendString("app not found")
	}
	name := sanitizeMySQLIdentifier(c.FormValue("database"))
	if name == "" {
		return c.Redirect("/php-panel/" + id + "?tab=databases&error=Valid+database+name+required")
	}
	dbMeta, err := p.DB.GetPHPPanelDatabaseByName(c.UserContext(), id, name)
	if err != nil || dbMeta.UserID != owner.ID {
		return c.Redirect("/php-panel/" + id + "?tab=databases&error=Database+not+found")
	}
	sqlInput := fmt.Sprintf("DROP DATABASE IF EXISTS %s;\n", mysqlQuotedIdentifier(name))
	res := p.phpPanelMySQLExec(c.UserContext(), app, sqlInput)
	if !res.OK {
		return c.Redirect("/php-panel/" + id + "?tab=databases&error=Could+not+delete+database")
	}
	_ = p.DB.DeleteOwnedPHPPanelDatabase(c.UserContext(), id, owner.ID, name)
	return c.Redirect("/php-panel/" + id + "?tab=databases&flash=Database+deleted")
}

func (p *Panel) PHPPanelDatabaseUserCreate(c *fiber.Ctx) error {
	id := c.Params("id")
	owner := phpPanelOwnerUser(c)
	app, err := p.DB.GetApp(c.UserContext(), id)
	if err != nil {
		return c.Status(404).SendString("app not found")
	}
	username := sanitizeMySQLIdentifier(c.FormValue("username"))
	password := strings.TrimSpace(c.FormValue("password"))
	host := sanitizeMySQLHost(c.FormValue("host"))
	databaseName := sanitizeMySQLIdentifier(c.FormValue("database"))
	privileges := selectedPHPPanelPrivileges(c)
	if username == "" || password == "" || host == "" {
		return c.Redirect("/php-panel/" + id + "?tab=databases&error=Username,+host,+and+password+required")
	}
	if databaseName != "" {
		dbMeta, err := p.DB.GetPHPPanelDatabaseByName(c.UserContext(), id, databaseName)
		if err != nil || dbMeta.UserID != owner.ID {
			return c.Redirect("/php-panel/" + id + "?tab=databases&error=Database+not+found")
		}
	}
	if existingUser, err := p.DB.GetPHPPanelDBUserByNameHost(c.UserContext(), id, username, host); err == nil {
		if existingUser.UserID == owner.ID {
			return c.Redirect("/php-panel/" + id + "?tab=databases&error=Database+user+already+exists")
		}
		return c.Redirect("/php-panel/" + id + "?tab=databases&error=That+database+user+is+owned+by+another+user")
	}
	if len(privileges) == 0 {
		privileges = []string{"ALL PRIVILEGES"}
	}
	privilegeSQL := strings.Join(privileges, ", ")
	var sqlLines []string
	sqlLines = append(sqlLines, fmt.Sprintf("CREATE USER IF NOT EXISTS %s@%s IDENTIFIED BY %s;", mysqlStringLiteral(username), mysqlStringLiteral(host), mysqlStringLiteral(password)))
	sqlLines = append(sqlLines, fmt.Sprintf("ALTER USER %s@%s IDENTIFIED BY %s;", mysqlStringLiteral(username), mysqlStringLiteral(host), mysqlStringLiteral(password)))
	if databaseName != "" {
		sqlLines = append(sqlLines, fmt.Sprintf("GRANT %s ON %s.* TO %s@%s;", privilegeSQL, mysqlQuotedIdentifier(databaseName), mysqlStringLiteral(username), mysqlStringLiteral(host)))
	}
	sqlLines = append(sqlLines, "FLUSH PRIVILEGES;")
	res := p.phpPanelMySQLExec(c.UserContext(), app, strings.Join(sqlLines, "\n")+"\n")
	if !res.OK {
		return c.Redirect("/php-panel/" + id + "?tab=databases&error=Could+not+save+database+user")
	}
	encryptedPassword, err := p.encryptPHPPanelSecret(c.UserContext(), password)
	if err != nil {
		return c.Redirect("/php-panel/" + id + "?tab=databases&error=Could+not+store+database+password")
	}
	dbUserID, err := p.DB.UpsertPHPPanelDBUser(c.UserContext(), db.PHPPanelDBUser{
		AppID:             id,
		UserID:            owner.ID,
		Username:          username,
		Host:              host,
		PasswordEncrypted: encryptedPassword,
	})
	if err != nil {
		return c.Redirect("/php-panel/" + id + "?tab=databases&error=Could+not+save+database+user+metadata")
	}
	if databaseName != "" {
		rawPrivileges, _ := json.Marshal(privileges)
		if err := p.DB.UpsertPHPPanelDBGrant(c.UserContext(), dbUserID, databaseName, string(rawPrivileges)); err != nil {
			return c.Redirect("/php-panel/" + id + "?tab=databases&error=Could+not+save+grant+metadata")
		}
	}
	return c.Redirect("/php-panel/" + id + "?tab=databases&flash=Database+user+saved")
}

func (p *Panel) PHPPanelDatabaseUserDelete(c *fiber.Ctx) error {
	id := c.Params("id")
	owner := phpPanelOwnerUser(c)
	app, err := p.DB.GetApp(c.UserContext(), id)
	if err != nil {
		return c.Status(404).SendString("app not found")
	}
	user, host, ok := parseMySQLAccountKey(c.FormValue("user_key"))
	if !ok {
		return c.Redirect("/php-panel/" + id + "?tab=databases&error=Invalid+database+user")
	}
	metaUser, err := p.DB.GetPHPPanelDBUserByNameHost(c.UserContext(), id, user, host)
	if err != nil || metaUser.UserID != owner.ID {
		return c.Redirect("/php-panel/" + id + "?tab=databases&error=Database+user+not+found")
	}
	sqlInput := fmt.Sprintf("DROP USER IF EXISTS %s@%s;\nFLUSH PRIVILEGES;\n", mysqlStringLiteral(user), mysqlStringLiteral(host))
	res := p.phpPanelMySQLExec(c.UserContext(), app, sqlInput)
	if !res.OK {
		return c.Redirect("/php-panel/" + id + "?tab=databases&error=Could+not+delete+database+user")
	}
	_ = p.DB.DeleteOwnedPHPPanelDBUser(c.UserContext(), id, owner.ID, user, host)
	return c.Redirect("/php-panel/" + id + "?tab=databases&flash=Database+user+deleted")
}

func (p *Panel) PHPPanelDatabaseGrant(c *fiber.Ctx) error {
	id := c.Params("id")
	owner := phpPanelOwnerUser(c)
	app, err := p.DB.GetApp(c.UserContext(), id)
	if err != nil {
		return c.Status(404).SendString("app not found")
	}
	user, host, ok := parseMySQLAccountKey(c.FormValue("user_key"))
	if !ok {
		return c.Redirect("/php-panel/" + id + "?tab=databases&error=Invalid+database+user")
	}
	metaUser, err := p.DB.GetPHPPanelDBUserByNameHost(c.UserContext(), id, user, host)
	if err != nil || metaUser.UserID != owner.ID {
		return c.Redirect("/php-panel/" + id + "?tab=databases&error=Database+user+not+found")
	}
	databaseName := sanitizeMySQLIdentifier(c.FormValue("database"))
	if databaseName == "" {
		return c.Redirect("/php-panel/" + id + "?tab=databases&error=Valid+database+name+required")
	}
	privileges := selectedPHPPanelPrivileges(c)
	if len(privileges) == 0 {
		privileges = []string{"ALL PRIVILEGES"}
	}
	dbMeta, err := p.DB.GetPHPPanelDatabaseByName(c.UserContext(), id, databaseName)
	if err != nil || dbMeta.UserID != owner.ID {
		return c.Redirect("/php-panel/" + id + "?tab=databases&error=Database+not+found")
	}
	sqlInput := fmt.Sprintf("GRANT %s ON %s.* TO %s@%s;\nFLUSH PRIVILEGES;\n", strings.Join(privileges, ", "), mysqlQuotedIdentifier(databaseName), mysqlStringLiteral(user), mysqlStringLiteral(host))
	res := p.phpPanelMySQLExec(c.UserContext(), app, sqlInput)
	if !res.OK {
		return c.Redirect("/php-panel/" + id + "?tab=databases&error=Could+not+grant+database+access")
	}
	rawPrivileges, _ := json.Marshal(privileges)
	if err := p.DB.UpsertPHPPanelDBGrant(c.UserContext(), metaUser.ID, databaseName, string(rawPrivileges)); err != nil {
		return c.Redirect("/php-panel/" + id + "?tab=databases&error=Could+not+save+grant+metadata")
	}
	return c.Redirect("/php-panel/" + id + "?tab=databases&flash=Database+access+granted")
}

func (p *Panel) PHPPanelServiceAction(c *fiber.Ctx) error {
	user, _ := currentUser(c)
	if user.Role != db.RoleAdmin || c.Locals("ScopedPHPPanelOnly") == true {
		return c.Redirect("/php-panel/" + c.Params("id") + "?tab=overview&error=Settings+access+is+admin+only")
	}
	id := c.Params("id")
	app, err := p.DB.GetApp(c.UserContext(), id)
	if err != nil {
		return c.Status(404).SendString("app not found")
	}
	service := strings.TrimSpace(c.FormValue("service"))
	action := strings.TrimSpace(c.FormValue("action"))
	if service == "" || action == "" {
		return c.Redirect("/php-panel/" + id + "?tab=settings&error=Missing+service+action")
	}
	project := p.activeComposeProjectName(c.UserContext(), app, id)
	dir := p.appSourcePath(c.UserContext(), id)
	ctx, cancel := context.WithTimeout(c.UserContext(), 5*time.Minute)
	defer cancel()
	var res dockerx.Result
	switch action {
	case "start":
		services := []string{service}
		if service == "php_pma" {
			services = []string{"php_mysql", "php_pma"}
		}
		res = dockerx.ComposeUpServices(ctx, dir, p.effectiveComposePaths(ctx, app, id), project, nil, p.composeEnvFiles(ctx, id), services...)
	case "stop":
		services := []string{service}
		if service == "php_mysql" {
			services = []string{"php_pma", "php_mysql"}
		}
		res = dockerx.ComposeStopServices(ctx, dir, p.effectiveComposePaths(ctx, app, id), project, nil, p.composeEnvFiles(ctx, id), services...)
	case "restart":
		res = dockerx.ComposeRestartServices(ctx, dir, p.effectiveComposePaths(ctx, app, id), project, nil, p.composeEnvFiles(ctx, id), service)
	default:
		return c.Redirect("/php-panel/" + id + "?tab=settings&error=Invalid+action")
	}
	if !res.OK {
		return c.Redirect("/php-panel/" + id + "?tab=settings&error=Service+action+failed")
	}
	return c.Redirect("/php-panel/" + id + "?tab=settings&flash=Service+updated")
}

func (p *Panel) phpPanelComposeRows(ctx context.Context, app db.App) ([]dockerx.ComposePsRow, error) {
	project := p.activeComposeProjectName(ctx, app, app.ID)
	rows, res := dockerx.ComposePS(ctx, p.appSourcePath(ctx, app.ID), p.effectiveComposePaths(ctx, app, app.ID), project, p.composeEnvFiles(ctx, app.ID))
	if !res.OK {
		return nil, fmt.Errorf(res.Output)
	}
	return rows, nil
}

func (p *Panel) syncPHPPanelDomains(c *fiber.Ctx, appID string) error {
	domains, err := p.DB.ListAppDomains(c.UserContext(), appID)
	if err != nil {
		return err
	}
	templateDomains, err := p.DB.ListTemplateAppDomains(c.UserContext(), appID)
	if err != nil {
		return err
	}
	templateMap := map[int64]db.TemplateAppDomain{}
	for _, item := range templateDomains {
		templateMap[item.AppDomainID] = item
	}
	for i := range domains {
		templateDomain, ok := templateMap[domains[i].ID]
		if !ok || strings.TrimSpace(templateDomain.SiteSlug) == "" {
			continue
		}
		site, err := p.DB.GetPHPPanelSiteBySlug(c.UserContext(), appID, templateDomain.SiteSlug)
		if err != nil {
			if err == sql.ErrNoRows {
				continue
			}
			return err
		}
		domains[i].Service = phppanel.ServiceForVersion(site.PHPVersion)
		domains[i].Port = 9000
		if err := p.DB.UpdateAppDomain(c.UserContext(), domains[i]); err != nil {
			return err
		}
		templateDomain.RootPath = phppanel.SitePublicRoot(appID, site.Slug)
		templateDomain.PHPVersion = site.PHPVersion
		if err := p.DB.UpsertTemplateAppDomain(c.UserContext(), templateDomain); err != nil {
			return err
		}
	}
	return p.syncAndApplyCaddyOverride(c, appID)
}

func (p *Panel) PHPPanelOpenPHPMyAdmin(c *fiber.Ctx) error {
	id := c.Params("id")
	owner := phpPanelOwnerUser(c)
	app, err := p.DB.GetApp(c.UserContext(), id)
	if err != nil {
		return c.Status(404).SendString("app not found")
	}
	if !phppanel.AppIsPHPPanel(app) {
		return c.Redirect("/apps/" + id)
	}
	domains, _, err := p.listOwnedPHPPanelDomains(c.UserContext(), id, owner.ID)
	if err != nil {
		return c.Redirect("/php-panel/" + id + "?error=Could+not+load+phpMyAdmin+domain")
	}
	targetURL := ""
	for i := range domains {
		if strings.EqualFold(domains[i].Service, "php_pma") {
			targetURL = composeURL(domains[i].Domain, domains[i].EnableHTTPS)
			break
		}
	}
	if targetURL == "" {
		return c.Redirect("/php-panel/" + id + "?error=Add+a+phpMyAdmin+domain+first")
	}
	dbUsers, err := p.DB.ListPHPPanelDBUsersByOwner(c.UserContext(), id, owner.ID)
	if err != nil {
		return c.Redirect("/php-panel/" + id + "?error=Could+not+load+database+user")
	}
	selectedUser := db.PHPPanelDBUser{}
	for _, item := range dbUsers {
		if strings.TrimSpace(item.PasswordEncrypted) != "" {
			selectedUser = item
			break
		}
	}
	if selectedUser.ID == 0 {
		return c.Redirect("/php-panel/" + id + "?error=Create+a+database+user+with+a+saved+password+first")
	}
	password, err := p.decryptPHPPanelSecret(c.UserContext(), selectedUser.PasswordEncrypted)
	if err != nil || strings.TrimSpace(password) == "" {
		return c.Redirect("/php-panel/" + id + "?error=Could+not+load+database+credential")
	}
	page := fmt.Sprintf(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="referrer" content="no-referrer">
  <title>Opening phpMyAdmin</title>
</head>
<body style="font-family: Arial, sans-serif; padding: 24px;">
  <p>Opening phpMyAdmin...</p>
  <form id="auto-pma-login" method="post" action="%s">
    <input type="hidden" name="pma_username" value="%s">
    <input type="hidden" name="pma_password" value="%s">
    <input type="hidden" name="server" value="1">
    <noscript><button type="submit">Continue to phpMyAdmin</button></noscript>
  </form>
  <script>document.getElementById('auto-pma-login').submit();</script>
</body>
</html>`,
		template.HTMLEscapeString(targetURL),
		template.HTMLEscapeString(selectedUser.Username),
		template.HTMLEscapeString(password),
	)
	c.Type("html")
	return c.SendString(page)
}

func (p *Panel) TemplateNavContextMiddleware(c *fiber.Ctx) error {
	if app, err := p.DB.GetTemplateAppByTemplateID(c.UserContext(), phppanel.TemplateID); err == nil {
		c.Locals("PHPPanelNavAppID", app.ID)
		c.Locals("PHPPanelNavName", app.Name)
	}
	return c.Next()
}
