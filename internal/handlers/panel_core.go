package handlers

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"panel/internal/caddy"
	"panel/internal/db"
	"panel/internal/dockerx"
	"panel/internal/workspace"

	"github.com/gofiber/fiber/v2"
)

// viewsRenderer matches Fiber's template engine (e.g. github.com/gofiber/template/html/v2).
type viewsRenderer interface {
	Render(out io.Writer, name string, binding interface{}, layout ...string) error
}

func appShowTabPartialName(tab string) string {
	switch tab {
	case "git":
		return tmplPartialGitTab
	case "files":
		return tmplPartialAppShowFiles
	case "environment":
		return tmplPartialAppShowEnvironment
	case "deployment":
		return tmplPartialAppShowDeployment
	case "logs":
		return tmplPartialAppShowLogs
	case "terminal":
		return tmplPartialAppShowTerminal
	case "containers":
		return tmplPartialAppShowContainers
	case "volumes":
		return tmplPartialAppShowVolumes
	case "domains":
		return tmplPartialAppShowDomains
	default:
		return tmplPartialAppShowOverview
	}
}

type Panel struct {
	DB             *db.Store
	Store          *workspace.Store
	WorkspacesRoot string
	deployMu       sync.Mutex
	deployRuns     map[string]*deployRun
}

// withUser adds the current authenticated user to a fiber.Map for template rendering.
func withUser(c *fiber.Ctx, m fiber.Map) fiber.Map {
	if u, ok := c.Locals(contextUserKey).(db.User); ok {
		m["CurrentUser"] = u
	}
	if v := c.Locals("PHPPanelNavAppID"); v != nil {
		m["PHPPanelNavAppID"] = v
	}
	if v := c.Locals("PHPPanelNavName"); v != nil {
		m["PHPPanelNavName"] = v
	}
	if v := c.Locals("ScopedPHPPanelOnly"); v != nil {
		m["ScopedPHPPanelOnly"] = v
	}
	if v := c.Locals("php_panel_owner"); v != nil {
		m["PHPPanelOwnerContext"] = v
	}
	return m
}

func sanitizeProjectName(name string) string {
	name = strings.TrimSpace(strings.ToLower(name))
	if name == "" {
		return ""
	}
	var b strings.Builder
	lastDash := false
	for _, r := range name {
		isAlphaNum := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if isAlphaNum {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if r == '-' || r == '_' || r == ' ' {
			if b.Len() == 0 || lastDash {
				continue
			}
			b.WriteByte('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-_")
	if out == "" {
		return ""
	}
	return out
}

func (p *Panel) composeProjectName(app db.App, id string) string {
	if slug := sanitizeProjectName(app.Name); slug != "" {
		return slug
	}
	return id
}

func (p *Panel) appSourcePath(ctx context.Context, appID string) string {
	if _, err := p.DB.GetAppGitConfig(ctx, appID); err == nil {
		return filepath.Join(p.Store.ReservedPath(appID), "repo")
	}
	return p.Store.Path(appID)
}

func (p *Panel) isGitApp(ctx context.Context, appID string) bool {
	if p.DB.GetAppSourceType(ctx, appID) == "git" {
		return true
	}
	_, err := p.DB.GetAppGitConfig(ctx, appID)
	return err == nil
}

func (p *Panel) appGitConfig(ctx context.Context, appID string) (db.AppGitConfig, bool) {
	cfg, err := p.DB.GetAppGitConfig(ctx, appID)
	if err != nil {
		return db.AppGitConfig{}, false
	}
	return cfg, true
}

func (p *Panel) legacyProjectNames(app db.App, id string) []string {
	current := p.composeProjectName(app, id)
	if current == id {
		return []string{current}
	}
	return []string{current, id}
}

func (p *Panel) activeComposeProjectName(ctx context.Context, app db.App, id string) string {
	names := p.legacyProjectNames(app, id)
	for _, project := range names {
		rows, res := dockerx.ComposePS(ctx, p.appSourcePath(ctx, id), p.effectiveComposePaths(ctx, app, id), project, p.composeEnvFiles(ctx, id))
		if res.OK && len(rows) > 0 {
			return project
		}
	}
	return names[0]
}

func (p *Panel) composeFilePath(app db.App, id string) string {
	rel := workspace.NormalizeComposeRel(app.ComposeFile)
	parts := strings.Split(rel, "/")
	base := p.Store.Path(id)
	if cfg, err := p.DB.GetAppGitConfig(context.Background(), id); err == nil && strings.TrimSpace(cfg.RepoURL) != "" {
		base = filepath.Join(p.Store.ReservedPath(id), "repo")
	}
	return filepath.Join(append([]string{base}, parts...)...)
}

func (p *Panel) composeOverridePath(id string) string {
	base := p.Store.Path(id)
	if cfg, err := p.DB.GetAppGitConfig(context.Background(), id); err == nil && strings.TrimSpace(cfg.RepoURL) != "" {
		base = filepath.Join(p.Store.ReservedPath(id), "repo")
	}
	return filepath.Join(base, caddy.GeneratedCompose)
}

func (p *Panel) effectiveComposePaths(ctx context.Context, app db.App, id string) []string {
	basePath := p.composeFilePath(app, id)
	overridePath := p.composeOverridePath(id)
	if st, err := os.Stat(overridePath); err == nil && !st.IsDir() {
		return []string{overridePath}
	}
	return []string{basePath}
}

// syncPanelEnvFileToDisk writes panel DB env to .nextdeploy/panel.compose.env for docker compose --env-file.
func (p *Panel) syncPanelEnvFileToDisk(appID string) error {
	content, err := p.DB.GetPanelEnv(context.Background(), appID)
	if err != nil {
		return err
	}
	if app, err := p.DB.GetApp(context.Background(), appID); err == nil && app.TemplateID == "php_panel" {
		workspaceRoot := fmt.Sprintf("/data/workspaces/%s", strings.TrimSpace(appID))
		content = ensureComposeEnvLine(content, "APP_WORKSPACE_ROOT", workspaceRoot)
	}
	dir := p.Store.ReservedPath(appID)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return err
	}
	pth := p.Store.PanelComposeEnvPath(appID)
	if b, err := os.ReadFile(pth); err == nil && string(b) == content {
		return nil
	}
	return os.WriteFile(pth, []byte(content), 0600)
}

func ensureComposeEnvLine(content, key, value string) string {
	prefix := key + "="
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	for _, line := range strings.Split(normalized, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), prefix) {
			return normalized
		}
	}
	if normalized != "" && !strings.HasSuffix(normalized, "\n") {
		normalized += "\n"
	}
	return normalized + prefix + value + "\n"
}

// composeEnvFiles returns --env-file paths for docker compose.
func (p *Panel) composeEnvFiles(ctx context.Context, appID string) []string {
	_ = p.syncPanelEnvFileToDisk(appID)
	var out []string
	projDot := p.Store.DotEnvPath(p.appSourcePath(ctx, appID))
	if st, err := os.Stat(projDot); err == nil && !st.IsDir() {
		if abs, err := filepath.Abs(projDot); err == nil {
			out = append(out, abs)
		} else {
			out = append(out, projDot)
		}
	}
	pth := p.Store.PanelComposeEnvPath(appID)
	absPanel, err := filepath.Abs(pth)
	if err != nil {
		absPanel = pth
	}
	out = append(out, absPanel)
	return out
}

// panelEnvForUI returns env for the Environment tab (DB-backed, with one-time import from workspace .env if empty).
func (p *Panel) panelEnvForUI(ctx context.Context, appID, sourcePath string) string {
	cur, err := p.DB.GetPanelEnv(ctx, appID)
	if err != nil {
		return ""
	}
	if strings.TrimSpace(cur) != "" {
		return cur
	}
	legacy, _ := p.Store.ReadDotEnv(sourcePath)
	if strings.TrimSpace(legacy) == "" {
		return ""
	}
	if err := p.DB.UpdatePanelEnv(ctx, appID, legacy); err != nil {
		return legacy
	}
	_ = p.syncPanelEnvFileToDisk(appID)
	return legacy
}

func countComposeState(rows []dockerx.ComposePsRow, want string) int {
	want = strings.ToLower(strings.TrimSpace(want))
	n := 0
	for _, row := range rows {
		if strings.ToLower(strings.TrimSpace(row.State)) == want {
			n++
		}
	}
	return n
}

// countComposeOkRunning counts containers that are "running" OR "exited(0)" (completed successfully).
func countComposeOkRunning(rows []dockerx.ComposePsRow) int {
	n := 0
	for _, row := range rows {
		state := strings.ToLower(strings.TrimSpace(row.State))
		status := strings.ToLower(strings.TrimSpace(row.Status))
		if state == "running" {
			n++
		} else if state == "exited" && strings.Contains(status, "exited (0)") {
			n++
		}
	}
	return n
}

func panelEnvValue(content, key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		if strings.TrimSpace(parts[0]) == key {
			return strings.TrimSpace(parts[1])
		}
	}
	return ""
}

func (p *Panel) currentPHPPanelApp(ctx context.Context) (db.App, bool) {
	app, err := p.DB.GetTemplateAppByTemplateID(ctx, "php_panel")
	if err != nil {
		return db.App{}, false
	}
	return app, true
}

func (p *Panel) userPHPPanelState(ctx context.Context, user db.User) (enabled bool, app db.App, hasApp bool) {
	if user.Role == db.RoleAdmin {
		app, hasApp = p.currentPHPPanelApp(ctx)
		return hasApp, app, hasApp
	}
	if !p.DB.PHPPanelEnabledForUser(ctx, user.ID) {
		return false, db.App{}, false
	}
	app, hasApp = p.currentPHPPanelApp(ctx)
	return hasApp, app, hasApp
}

func (p *Panel) allowPHPPanelScopedAppRoute(ctx context.Context, path string) bool {
	app, hasApp := p.currentPHPPanelApp(ctx)
	if !hasApp {
		return false
	}
	allowedPrefixes := []string{
		"/php-panel/" + app.ID,
		"/php-panel/" + app.ID + "/",
		"/apps/" + app.ID + "/files",
		"/apps/" + app.ID + "/file",
		"/apps/" + app.ID + "/file-preview",
		"/apps/" + app.ID + "/upload",
		"/apps/" + app.ID + "/domains/",
	}
	allowedExact := map[string]bool{
		"/php-panel/" + app.ID:      true,
		"/php-panel-blocked":        true,
		"/php-panel/exit-impersonation": true,
		"/logout":                   true,
		"/overview":                 true,
	}
	if allowedExact[path] {
		return true
	}
	for _, prefix := range allowedPrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

func (p *Panel) ensurePHPPanelOwnedBase(ctx context.Context, appID string, ownerID int64, baseRel string) error {
	baseRel = strings.Trim(strings.TrimSpace(filepath.ToSlash(baseRel)), "/")
	if baseRel == "" {
		return fmt.Errorf("base path required")
	}
	sites, err := p.DB.ListPHPPanelSitesByOwner(ctx, appID, ownerID)
	if err != nil {
		return err
	}
	for _, site := range sites {
		want := phpPanelSiteBasePath(site.Slug)
		if baseRel == want {
			return nil
		}
	}
	return fmt.Errorf("base path not allowed")
}
