package handlers

import (
	"context"
	"errors"
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
	case "backup":
		return tmplPartialAppShowBackup
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
	envFileMu      sync.Map
	composeMu      sync.Map
	gitlabTokenMu  sync.Map
}

func (p *Panel) InitDeployRuns() {
	p.deployMu.Lock()
	defer p.deployMu.Unlock()
	if p.deployRuns == nil {
		p.deployRuns = make(map[string]*deployRun)
	}
}

// withUser adds the current authenticated user to a fiber.Map for template rendering.
func withUser(c *fiber.Ctx, m fiber.Map) fiber.Map {
	if u, ok := c.Locals(contextUserKey).(db.User); ok {
		m["CurrentUser"] = u
	}
	return m
}

const (
	appSlugMinLen = 2
	appSlugMaxLen = 48
)

// validateAppSlug returns the canonical app id (same as stored name): lowercase [a-z0-9-]+,
// no spaces or other symbols, no leading/trailing hyphen, no "--".
func validateAppSlug(raw string) (string, error) {
	s := strings.TrimSpace(strings.ToLower(raw))
	if s == "" {
		return "", errors.New("name is required")
	}
	if len(s) < appSlugMinLen {
		return "", errors.New("name must be at least 2 characters")
	}
	if len(s) > appSlugMaxLen {
		return "", errors.New("name must be at most 48 characters")
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '-':
		default:
			return "", errors.New("only lowercase letters, numbers, and hyphens are allowed (no spaces)")
		}
	}
	if s[0] == '-' || s[len(s)-1] == '-' {
		return "", errors.New("name cannot start or end with a hyphen")
	}
	if strings.Contains(s, "--") {
		return "", errors.New("consecutive hyphens are not allowed")
	}
	return s, nil
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

// composeNameSuffixFromAppID returns a short stable token from the app id (first 8 alphanumeric chars).
// Used only for legacy compose project names (slug_suffix) from older panel releases.
func composeNameSuffixFromAppID(id string) string {
	id = strings.TrimSpace(strings.ToLower(id))
	var b strings.Builder
	for _, r := range id {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	s := b.String()
	if len(s) >= 8 {
		return s[:8]
	}
	if s != "" {
		return s
	}
	return "app"
}

func composeProjectSuffixedLegacy(app db.App, id string) string {
	slug := sanitizeProjectName(app.Name)
	if slug == "" {
		return ""
	}
	return slug + "_" + composeNameSuffixFromAppID(id)
}

// composeProjectName is the canonical Docker Compose project name: sanitizeProjectName(app.Name) only.
// Apps without a slug cannot be created (CreateApp); empty slug here means legacy or invalid DB rows.
func (p *Panel) composeProjectName(app db.App, id string) string {
	_ = id // kept for call-site consistency with other helpers
	return sanitizeProjectName(app.Name)
}

// composeWorkspaceRootFromHint returns the git checkout root when repoURLHint is non-empty, else the file workspace root.
func (p *Panel) composeWorkspaceRootFromHint(appID, repoURLHint string) string {
	if strings.TrimSpace(repoURLHint) != "" {
		return filepath.Join(p.Store.ReservedPath(appID), "repo")
	}
	return p.Store.Path(appID)
}

// composeWorkspaceRoot is the host workspace directory used for docker compose and compose file paths
// (non-git store path, or git clone root when a repo URL is configured).
func (p *Panel) composeWorkspaceRoot(ctx context.Context, appID string) string {
	var hint string
	if cfg, err := p.DB.GetAppGitConfig(ctx, appID); err == nil {
		hint = cfg.RepoURL
	}
	return p.composeWorkspaceRootFromHint(appID, hint)
}

func (p *Panel) composeWorkspaceRootFromRepoURL(appID, repoURL string) string {
	return p.composeWorkspaceRootFromHint(appID, repoURL)
}

func (p *Panel) appSourcePath(ctx context.Context, appID string) string {
	return p.composeWorkspaceRoot(ctx, appID)
}

// isGitApp returns true when the app uses git integration (source_type git or a stored git config row).
func (p *Panel) isGitApp(ctx context.Context, appID string) bool {
	if _, err := p.DB.GetAppGitConfig(ctx, appID); err == nil {
		return true
	}
	return p.DB.GetAppSourceType(ctx, appID) == "git"
}

// appGitMetadata loads git config once and derives isGit without redundant queries when combined with AppShow.
func (p *Panel) appGitMetadata(ctx context.Context, appID string) (isGit bool, cfg db.AppGitConfig, hasCfg bool) {
	cfg, err := p.DB.GetAppGitConfig(ctx, appID)
	if err == nil {
		return true, cfg, true
	}
	if p.DB.GetAppSourceType(ctx, appID) == "git" {
		return true, db.AppGitConfig{}, false
	}
	return false, db.AppGitConfig{}, false
}

func (p *Panel) appGitConfig(ctx context.Context, appID string) (db.AppGitConfig, bool) {
	cfg, err := p.DB.GetAppGitConfig(ctx, appID)
	if err != nil {
		return db.AppGitConfig{}, false
	}
	return cfg, true
}

func (p *Panel) legacyProjectNames(app db.App, id string) []string {
	slug := sanitizeProjectName(app.Name)
	seen := map[string]struct{}{}
	var out []string
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		if _, ok := seen[s]; ok {
			return
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	// Older panel used slug_id8; probe that first so activeComposeProjectName finds running stacks.
	if suf := composeProjectSuffixedLegacy(app, id); suf != "" {
		add(suf)
	}
	if slug != "" {
		add(slug)
	}
	return out
}

// composeProjectAndPS resolves the active compose project name and returns Compose PS rows in at most
// len(legacyProjectNames) subprocess calls (reuses rows for the winning project — avoids an extra PS on list pages).
func (p *Panel) composeProjectAndPS(ctx context.Context, app db.App, id string) (project string, rows []dockerx.ComposePsRow, res dockerx.Result) {
	canonical := p.composeProjectName(app, id)
	if canonical == "" {
		return "", nil, dockerx.Result{OK: false, Output: "app has no compose project slug; set an app name with letters or numbers"}
	}
	names := p.legacyProjectNames(app, id)
	if len(names) == 0 {
		return "", nil, dockerx.Result{OK: false, Output: "no compose project candidates"}
	}
	root := p.composeWorkspaceRoot(ctx, id)
	paths := p.effectiveComposePaths(ctx, app, id)
	envFiles := p.composeEnvFiles(ctx, id)
	var lastRows []dockerx.ComposePsRow
	var lastRes dockerx.Result
	for i, proj := range names {
		lastRows, lastRes = dockerx.ComposePS(ctx, root, paths, proj, envFiles)
		if lastRes.OK && len(lastRows) > 0 {
			return proj, lastRows, lastRes
		}
		if i == len(names)-1 {
			return canonical, lastRows, lastRes
		}
	}
	return canonical, lastRows, lastRes
}

// composeProjectAndPSHint is like composeProjectAndPS but uses batched DB hints (no per-app git/env queries).
func (p *Panel) composeProjectAndPSHint(ctx context.Context, app db.App, id string, hint db.AppComposeHint) (project string, rows []dockerx.ComposePsRow, res dockerx.Result) {
	canonical := p.composeProjectName(app, id)
	if canonical == "" {
		return "", nil, dockerx.Result{OK: false, Output: "app has no compose project slug; set an app name with letters or numbers"}
	}
	names := p.legacyProjectNames(app, id)
	if len(names) == 0 {
		return "", nil, dockerx.Result{OK: false, Output: "no compose project candidates"}
	}
	root := p.composeWorkspaceRootFromRepoURL(id, hint.RepoURL)
	paths := p.effectiveComposePathsFromRoot(app, id, root)
	envFiles := p.composeEnvFilesFromContent(ctx, id, hint.PanelEnv)
	var lastRows []dockerx.ComposePsRow
	var lastRes dockerx.Result
	for i, proj := range names {
		lastRows, lastRes = dockerx.ComposePS(ctx, root, paths, proj, envFiles)
		if lastRes.OK && len(lastRows) > 0 {
			return proj, lastRows, lastRes
		}
		if i == len(names)-1 {
			return canonical, lastRows, lastRes
		}
	}
	return canonical, lastRows, lastRes
}

func (p *Panel) activeComposeProjectName(ctx context.Context, app db.App, id string) string {
	project, _, _ := p.composeProjectAndPS(ctx, app, id)
	return project
}

// stopOtherComposeStacks runs compose down (no volume removal) for every project name candidate
// except activeProject so legacy or COMPOSE_PROJECT_NAME-prefixed stacks cannot keep running
// alongside the stack the panel is about to manage.
func (p *Panel) stopOtherComposeStacks(ctx context.Context, app db.App, id, activeProject string) {
	activeProject = strings.TrimSpace(activeProject)
	if activeProject == "" {
		return
	}
	dir := p.appSourcePath(ctx, id)
	paths := p.effectiveComposePaths(ctx, app, id)
	envFiles := p.composeEnvFiles(ctx, id)
	for _, proj := range p.composeProjectCandidates(ctx, app, id) {
		proj = strings.TrimSpace(proj)
		if proj == "" || proj == activeProject {
			continue
		}
		_ = dockerx.ComposeDown(ctx, dir, paths, proj, nil, envFiles)
	}
}

func (p *Panel) composeFilePath(ctx context.Context, app db.App, id string) string {
	root := p.composeWorkspaceRoot(ctx, id)
	rel := workspace.NormalizeComposeRel(app.ComposeFile)
	parts := strings.Split(rel, "/")
	return filepath.Join(append([]string{root}, parts...)...)
}

func (p *Panel) composeOverridePath(ctx context.Context, id string) string {
	return filepath.Join(p.composeWorkspaceRoot(ctx, id), caddy.GeneratedCompose)
}

func (p *Panel) effectiveComposePathsFromRoot(app db.App, id, root string) []string {
	rel := workspace.NormalizeComposeRel(app.ComposeFile)
	parts := strings.Split(rel, "/")
	basePath := filepath.Join(append([]string{root}, parts...)...)
	overridePath := filepath.Join(root, caddy.GeneratedCompose)
	if st, err := os.Stat(overridePath); err == nil && !st.IsDir() {
		return []string{overridePath}
	}
	return []string{basePath}
}

func (p *Panel) effectiveComposePaths(ctx context.Context, app db.App, id string) []string {
	return p.effectiveComposePathsFromRoot(app, id, p.composeWorkspaceRoot(ctx, id))
}

// atomicWriteFile writes data to path using a temp file in the same directory and rename (atomic on POSIX).
func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".nextdeploy-atomic-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	done := false
	defer func() {
		if !done {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	done = true
	if err := os.Chmod(path, perm); err != nil {
		// Windows and some FS ignore full unix perms; content is already in place.
		_ = err
	}
	return nil
}

// syncWorkspaceEnvFromPanel replaces workspaceRoot/.env entirely with panelEnv (DB). It does not read
// or merge with any existing .env on disk; the database is the only source of truth for this file.
func (p *Panel) syncWorkspaceEnvFromPanel(appID, workspaceRoot, panelEnv string) error {
	v, _ := p.envFileMu.LoadOrStore(appID, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()
	dot := p.Store.DotEnvPath(workspaceRoot)
	return atomicWriteFile(dot, []byte(panelEnv), 0600)
}

// composeEnvFilesFromContent writes the given DB env to workspace .env (full replace), then returns that path for --env-file.
func (p *Panel) composeEnvFilesFromContent(ctx context.Context, appID, panelEnv string) []string {
	root := p.composeWorkspaceRoot(ctx, appID)
	_ = p.syncWorkspaceEnvFromPanel(appID, root, panelEnv)
	dot := p.Store.DotEnvPath(root)
	abs, err := filepath.Abs(dot)
	if err != nil {
		abs = dot
	}
	return []string{abs}
}

// composeEnvFiles returns --env-file paths for docker compose.
func (p *Panel) composeEnvFiles(ctx context.Context, appID string) []string {
	content, _ := p.DB.GetPanelEnv(ctx, appID)
	return p.composeEnvFilesFromContent(ctx, appID, content)
}

// panelEnvForUI returns DB-backed env for the Environment tab.
func (p *Panel) panelEnvForUI(ctx context.Context, appID string) string {
	cur, err := p.DB.GetPanelEnv(ctx, appID)
	if err != nil {
		return ""
	}
	return cur
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

func dedupeStringsPreserveOrder(in []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

func parseComposeProjectNameFromEnvFile(data []byte) string {
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}
		if !strings.HasPrefix(line, "COMPOSE_PROJECT_NAME") {
			continue
		}
		idx := strings.IndexByte(line, '=')
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		if key != "COMPOSE_PROJECT_NAME" {
			continue
		}
		val := strings.TrimSpace(line[idx+1:])
		val = strings.Trim(val, `"'`)
		if val != "" {
			return val
		}
	}
	return ""
}

// composeProjectNamesFromEnvFiles returns COMPOSE_PROJECT_NAME from panel env (DB), if set.
func (p *Panel) composeProjectNamesFromEnvFiles(ctx context.Context, appID string) []string {
	cur, err := p.DB.GetPanelEnv(ctx, appID)
	if err != nil {
		return nil
	}
	if v := parseComposeProjectNameFromEnvFile([]byte(cur)); v != "" {
		return []string{v}
	}
	return nil
}

// composeProjectCandidates merges compose legacy names with COMPOSE_PROJECT_NAME from panel env if set (deduped).
func (p *Panel) composeProjectCandidates(ctx context.Context, app db.App, appID string) []string {
	var merged []string
	canonical := p.composeProjectName(app, appID)
	merged = append(merged, canonical)
	merged = append(merged, p.legacyProjectNames(app, appID)...)
	merged = append(merged, p.composeProjectNamesFromEnvFiles(ctx, appID)...)
	return dedupeStringsPreserveOrder(merged)
}

func panelEnvDefinesComposeProjectName(s string) bool {
	for _, raw := range strings.Split(s, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}
		idx := strings.IndexByte(line, '=')
		if idx <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		if strings.EqualFold(key, "COMPOSE_PROJECT_NAME") {
			return true
		}
	}
	return false
}

// seedComposeProjectNameInPanelEnv writes COMPOSE_PROJECT_NAME to panel-managed env when unset so
// docker compose and volume/container names align with composeProjectName (sanitized app name slug).
func (p *Panel) seedComposeProjectNameInPanelEnv(ctx context.Context, appID string, app db.App) error {
	cur, err := p.DB.GetPanelEnv(ctx, appID)
	if err != nil {
		return err
	}
	if panelEnvDefinesComposeProjectName(cur) {
		return nil
	}
	proj := p.composeProjectName(app, appID)
	if proj == "" {
		return nil
	}
	line := "COMPOSE_PROJECT_NAME=" + proj
	newVal := strings.TrimSpace(cur)
	if newVal != "" {
		newVal += "\n"
	}
	newVal += line
	if err := p.DB.UpdatePanelEnv(ctx, appID, newVal); err != nil {
		return err
	}
	root := p.composeWorkspaceRoot(ctx, appID)
	return p.syncWorkspaceEnvFromPanel(appID, root, newVal)
}

func composeWorkspaceDirContainedInApp(appRoot, workDir string) bool {
	if appRoot == "" || workDir == "" {
		return false
	}
	appRoot = filepath.Clean(appRoot)
	workDir = filepath.Clean(workDir)
	if appRoot == workDir {
		return true
	}
	rel, err := filepath.Rel(appRoot, workDir)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}
