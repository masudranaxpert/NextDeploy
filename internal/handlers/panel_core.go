package handlers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"panel/internal/caddy"
	"panel/internal/db"
	"panel/internal/dockerx"
	"panel/internal/gitx"
	"panel/internal/perflog"
	"panel/internal/handlers/audit"
	"panel/internal/handlers/utils"
	"panel/internal/volumex"
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
		return utils.TmplPartialGitTab
	case "files":
		return utils.TmplPartialAppShowFiles
	case "environment":
		return utils.TmplPartialAppShowEnvironment
	case "deployment":
		return utils.TmplPartialAppShowDeployment
	case "logs":
		return utils.TmplPartialAppShowLogs
	case "terminal":
		return utils.TmplPartialAppShowTerminal
	case "containers":
		return utils.TmplPartialAppShowContainers
	case "volumes":
		return utils.TmplPartialAppShowVolumes
	case "domains":
		return utils.TmplPartialAppShowDomains
	case "backup":
		return utils.TmplPartialAppShowBackup
	case "collaborators":
		return utils.TmplPartialAppShowCollaborators
	default:
		return utils.TmplPartialAppShowOverview
	}
}

type GitSyncer interface {
	SyncGitAppSource(ctx context.Context, appID string) (string, error)
}

type Panel struct {
	DB               *db.Store
	Store            *workspace.Store
	WorkspacesRoot   string
	deployMu         sync.Mutex
	deployRuns       map[string]*DeployRun
	VolRestoreMu     sync.Mutex
	volRestoreJobs   map[string]*VolumeRestoreJob
	VolRestoreActive sync.Map
	envFileMu        sync.Map
	ComposeMu        sync.Map
	GitlabTokenMu    sync.Map

	backupRestoreMu    sync.Mutex
	BackupRestoreState map[string]BackupRestoreState // app id -> current/last restore state

	GitSyncer GitSyncer

	cgroupMu         sync.Mutex
	cgroupChecked    bool
	cgroupModeVal    string   // "systemd", "cgroupfs", or "" (unsupported)
	userSliceApplied sync.Map // user id (int64) -> "memMB:cpus" last applied

	appStorage sync.Map // app id (string) -> appStorageEntry

	setupDone atomic.Bool

	migrateTokens sync.Map // export id (int64) -> plain download token (string)
}

func (p *Panel) MarkSetupComplete() {
	p.setupDone.Store(true)
}

func (p *Panel) ClearSetupComplete() {
	p.setupDone.Store(false)
}

func (p *Panel) setupRedirectNeeded(ctx context.Context) (bool, error) {
	if p.setupDone.Load() {
		return false, nil
	}
	count, err := p.DB.UserCount(ctx)
	if err != nil {
		return false, err
	}
	if count > 0 {
		p.setupDone.Store(true)
		return false, nil
	}
	return true, nil
}

type BackupRestoreState struct {
	ActiveHistoryID int64
	LastHistoryID   int64
	Status          string
	Error           string
}

func (p *Panel) InitDeployRuns() {
	p.deployMu.Lock()
	defer p.deployMu.Unlock()
	if p.deployRuns == nil {
		p.deployRuns = make(map[string]*DeployRun)
	}
	p.VolRestoreMu.Lock()
	if p.volRestoreJobs == nil {
		p.volRestoreJobs = make(map[string]*VolumeRestoreJob)
	}
	p.VolRestoreMu.Unlock()
}

func (p *Panel) StartBackupRestore(appID string, historyID int64) bool {
	p.backupRestoreMu.Lock()
	defer p.backupRestoreMu.Unlock()
	if p.BackupRestoreState == nil {
		p.BackupRestoreState = make(map[string]BackupRestoreState)
	}
	cur := p.BackupRestoreState[appID]
	if cur.ActiveHistoryID > 0 {
		return false
	}
	p.BackupRestoreState[appID] = BackupRestoreState{
		ActiveHistoryID: historyID,
		LastHistoryID:   historyID,
		Status:          "running",
	}
	return true
}

func (p *Panel) startBackupRestore(appID string, historyID int64) bool {
	return p.StartBackupRestore(appID, historyID)
}

func (p *Panel) FinishBackupRestore(appID string, historyID int64, errMsg string) {
	p.backupRestoreMu.Lock()
	defer p.backupRestoreMu.Unlock()
	if p.BackupRestoreState == nil {
		p.BackupRestoreState = make(map[string]BackupRestoreState)
	}
	st := p.BackupRestoreState[appID]
	st.ActiveHistoryID = 0
	st.LastHistoryID = historyID
	if strings.TrimSpace(errMsg) != "" {
		st.Status = "failed"
		st.Error = errMsg
	} else {
		st.Status = "completed"
		st.Error = ""
	}
	p.BackupRestoreState[appID] = st
}

func (p *Panel) finishBackupRestore(appID string, historyID int64, errMsg string) {
	p.FinishBackupRestore(appID, historyID, errMsg)
}

func (p *Panel) BackupRestoreSnapshot(appID string) BackupRestoreState {
	p.backupRestoreMu.Lock()
	defer p.backupRestoreMu.Unlock()
	return p.BackupRestoreState[appID]
}

func (p *Panel) backupRestoreSnapshot(appID string) BackupRestoreState {
	return p.BackupRestoreSnapshot(appID)
}

// WithUser adds the current authenticated user to a fiber.Map for template rendering.
func WithUser(c *fiber.Ctx, m fiber.Map) fiber.Map {
	if u, ok := c.Locals(contextUserKey).(db.User); ok {
		m["CurrentUser"] = u
	}
	return m
}

func withUser(c *fiber.Ctx, m fiber.Map) fiber.Map {
	return WithUser(c, m)
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

// composeProjectName is the canonical Docker Compose project name. It is derived from the app ID
// (slug + random suffix, e.g. "blog-c66b") so two users with same-named apps can never collide on
// one Docker project. Plain app-name slugs remain as legacy candidates for stacks deployed earlier.
func (p *Panel) composeProjectName(app db.App, id string) string {
	if proj := sanitizeProjectName(id); proj != "" {
		return proj
	}
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
func (p *Panel) ComposeWorkspaceRoot(ctx context.Context, appID string) string {
	var hint string
	if cfg, err := p.DB.GetAppGitConfig(ctx, appID); err == nil {
		hint = cfg.RepoURL
	}
	return p.composeWorkspaceRootFromHint(appID, hint)
}

func (p *Panel) composeWorkspaceRoot(ctx context.Context, appID string) string {
	return p.ComposeWorkspaceRoot(ctx, appID)
}

func (p *Panel) composeWorkspaceRootFromRepoURL(appID, repoURL string) string {
	return p.composeWorkspaceRootFromHint(appID, repoURL)
}

func (p *Panel) AppSourcePath(ctx context.Context, appID string) string {
	return p.ComposeWorkspaceRoot(ctx, appID)
}

func (p *Panel) appSourcePath(ctx context.Context, appID string) string {
	return p.AppSourcePath(ctx, appID)
}

// IsGitApp returns true when the app uses git integration (source_type git or a stored git config row).
func (p *Panel) IsGitApp(ctx context.Context, appID string) bool {
	if _, err := p.DB.GetAppGitConfig(ctx, appID); err == nil {
		return true
	}
	return p.DB.GetAppSourceType(ctx, appID) == "git"
}

func (p *Panel) isGitApp(ctx context.Context, appID string) bool {
	return p.IsGitApp(ctx, appID)
}

// AppGitMetadata loads git config once and derives isGit without redundant queries when combined with AppShow.
func (p *Panel) AppGitMetadata(ctx context.Context, appID string) (isGit bool, cfg db.AppGitConfig, hasCfg bool) {
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
	// Canonical (app-ID based, unique) first: fast path for stacks deployed after the rename.
	add(p.composeProjectName(app, id))
	// Older panels used slug_id8 and the plain name slug; probe those so existing stacks are found.
	if suf := composeProjectSuffixedLegacy(app, id); suf != "" {
		add(suf)
	}
	if slug != "" {
		add(slug)
	}
	return out
}

// ComposeRowsBelongToApp is the exported wrapper for composeRowsBelongToApp.
func (p *Panel) ComposeRowsBelongToApp(id string, rows []dockerx.ComposePsRow) bool {
	return p.composeRowsBelongToApp(id, rows)
}

// ProjectNameSharedWithOtherApp reports whether any other app resolves to the same compose
// project name. Used to decide if cleanup of a legacy (slug-based) project name is safe.
func (p *Panel) ProjectNameSharedWithOtherApp(ctx context.Context, appID, project string) bool {
	project = strings.TrimSpace(project)
	if project == "" {
		return false
	}
	apps, err := p.DB.ListApps(ctx)
	if err != nil {
		return false
	}
	for _, a := range apps {
		if a.ID == appID {
			continue
		}
		if sanitizeProjectName(a.Name) == project || sanitizeProjectName(a.ID) == project {
			return true
		}
	}
	return false
}

// composeRowsBelongToApp verifies that a probed compose project actually belongs to this app by
// checking the compose working_dir label against the app workspace. Prevents an app whose legacy
// slug collides with another user's project (e.g. both named "blog") from claiming that stack.
// Rows without a WorkingDir (CLI fallback) are accepted for backwards compatibility.
func (p *Panel) composeRowsBelongToApp(id string, rows []dockerx.ComposePsRow) bool {
	appRoot := filepath.Clean(p.Store.Path(id))
	sawWorkDir := false
	for _, row := range rows {
		wd := strings.TrimSpace(row.WorkingDir)
		if wd == "" {
			continue
		}
		sawWorkDir = true
		if composeWorkspaceDirContainedInApp(appRoot, wd) {
			return true
		}
	}
	return !sawWorkDir
}

// composeProjectAndPS resolves the active compose project name and returns Compose PS rows in at most
// len(legacyProjectNames) subprocess calls (reuses rows for the winning project — avoids an extra PS on list pages).
func (p *Panel) ComposeProjectAndPS(ctx context.Context, app db.App, id string) (project string, rows []dockerx.ComposePsRow, res dockerx.Result) {
	tr := perflog.Start("ComposeProjectAndPS")
	defer tr.Finish()
	tr.Field("app", id)

	canonical := p.composeProjectName(app, id)
	if canonical == "" {
		return "", nil, dockerx.Result{OK: false, Output: "app has no compose project slug; set an app name with letters or numbers"}
	}
	names := p.legacyProjectNames(app, id)
	tr.Field("candidates", fmt.Sprintf("%d", len(names)))
	if len(names) == 0 {
		return "", nil, dockerx.Result{OK: false, Output: "no compose project candidates"}
	}
	root := p.composeWorkspaceRoot(ctx, id)
	paths := p.effectiveComposePaths(ctx, app, id)
	envFiles := p.composeEnvFiles(ctx, id)
	var lastRows []dockerx.ComposePsRow
	var lastRes dockerx.Result
	probeStart := time.Now()
	for i, proj := range names {
		lastRows, lastRes = dockerx.ComposePS(ctx, root, paths, proj, envFiles)
		if lastRes.OK && len(lastRows) > 0 && p.composeRowsBelongToApp(id, lastRows) {
			tr.Field("winner", proj)
			tr.Field("probes", fmt.Sprintf("%d", i+1))
			tr.StepDur("probes", probeStart)
			return proj, lastRows, lastRes
		}
		if i == len(names)-1 {
			tr.Field("winner", canonical)
			tr.Field("probes", fmt.Sprintf("%d", len(names)))
			tr.StepDur("probes", probeStart)
			return canonical, nil, lastRes
		}
	}
	tr.Field("winner", canonical)
	tr.Field("probes", fmt.Sprintf("%d", len(names)))
	tr.StepDur("probes", probeStart)
	return canonical, nil, lastRes
}

func (p *Panel) composeProjectAndPS(ctx context.Context, app db.App, id string) (project string, rows []dockerx.ComposePsRow, res dockerx.Result) {
	return p.ComposeProjectAndPS(ctx, app, id)
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
		if lastRes.OK && len(lastRows) > 0 && p.composeRowsBelongToApp(id, lastRows) {
			return proj, lastRows, lastRes
		}
		if i == len(names)-1 {
			return canonical, nil, lastRes
		}
	}
	return canonical, nil, lastRes
}

func (p *Panel) ActiveComposeProjectName(ctx context.Context, app db.App, id string) string {
	project, _, _ := p.composeProjectAndPS(ctx, app, id)
	return project
}

func (p *Panel) activeComposeProjectName(ctx context.Context, app db.App, id string) string {
	return p.ActiveComposeProjectName(ctx, app, id)
}

// stopOtherComposeStacks runs compose down (no volume removal) for every project name candidate
// except activeProject so legacy or COMPOSE_PROJECT_NAME-prefixed stacks cannot keep running
// alongside the stack the panel is about to manage.
func (p *Panel) StopOtherComposeStacks(ctx context.Context, app db.App, id, activeProject string) {
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
		// Legacy slug candidates can collide with another user's project (same app name).
		// Only down a candidate when its containers were deployed from this app's workspace.
		rows, res := dockerx.ComposePS(ctx, dir, paths, proj, envFiles)
		if !res.OK || len(rows) == 0 || !p.composeRowsBelongToApp(id, rows) {
			continue
		}
		_ = dockerx.ComposeDown(ctx, dir, paths, proj, nil, envFiles)
	}
}

func (p *Panel) stopOtherComposeStacks(ctx context.Context, app db.App, id, activeProject string) {
	p.StopOtherComposeStacks(ctx, app, id, activeProject)
}

func (p *Panel) ComposeFilePath(ctx context.Context, app db.App, id string) string {
	root := p.composeWorkspaceRoot(ctx, id)
	rel := workspace.NormalizeComposeRel(app.ComposeFile)
	parts := strings.Split(rel, "/")
	return filepath.Join(append([]string{root}, parts...)...)
}

func (p *Panel) composeFilePath(ctx context.Context, app db.App, id string) string {
	return p.ComposeFilePath(ctx, app, id)
}

func (p *Panel) ComposeOverridePath(ctx context.Context, id string) string {
	return filepath.Join(p.composeWorkspaceRoot(ctx, id), caddy.GeneratedCompose)
}

func (p *Panel) composeOverridePath(ctx context.Context, id string) string {
	return p.ComposeOverridePath(ctx, id)
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

func (p *Panel) EffectiveComposePaths(ctx context.Context, app db.App, id string) []string {
	return p.effectiveComposePathsFromRoot(app, id, p.composeWorkspaceRoot(ctx, id))
}

func (p *Panel) effectiveComposePaths(ctx context.Context, app db.App, id string) []string {
	return p.EffectiveComposePaths(ctx, app, id)
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
func (p *Panel) SyncWorkspaceEnvFromPanel(appID, workspaceRoot, panelEnv string) error {
	v, _ := p.envFileMu.LoadOrStore(appID, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()
	dot := p.Store.DotEnvPath(workspaceRoot)
	return atomicWriteFile(dot, []byte(panelEnv), 0600)
}

func (p *Panel) syncWorkspaceEnvFromPanel(appID, workspaceRoot, panelEnv string) error {
	return p.SyncWorkspaceEnvFromPanel(appID, workspaceRoot, panelEnv)
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

// ComposeEnvFiles returns --env-file paths for docker compose.
func (p *Panel) ComposeEnvFiles(ctx context.Context, appID string) []string {
	content, _ := p.DB.GetPanelEnv(ctx, appID)
	return p.composeEnvFilesFromContent(ctx, appID, content)
}

func (p *Panel) composeEnvFiles(ctx context.Context, appID string) []string {
	return p.ComposeEnvFiles(ctx, appID)
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
func (p *Panel) ComposeProjectCandidates(ctx context.Context, app db.App, appID string) []string {
	var merged []string
	canonical := p.composeProjectName(app, appID)
	merged = append(merged, canonical)
	merged = append(merged, p.legacyProjectNames(app, appID)...)
	merged = append(merged, p.composeProjectNamesFromEnvFiles(ctx, appID)...)
	return dedupeStringsPreserveOrder(merged)
}

func (p *Panel) composeProjectCandidates(ctx context.Context, app db.App, appID string) []string {
	return p.ComposeProjectCandidates(ctx, app, appID)
}

func (p *Panel) AllPanelComposeProjects(ctx context.Context) []string {
	apps, err := p.DB.ListApps(ctx)
	if err != nil {
		return nil
	}
	return p.composeProjectsForApps(ctx, apps)
}

func (p *Panel) composeProjectsForApps(ctx context.Context, apps []db.App) []string {
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
	for _, app := range apps {
		add(app.ID)
		add(strings.ReplaceAll(app.ID, "-", "_"))
		add(app.Name)
		for _, c := range p.ComposeProjectCandidates(ctx, app, app.ID) {
			add(c)
		}
	}
	return out
}

func (p *Panel) BackupVolumeComposeProjects(ctx context.Context, app db.App, appID string) []string {
	volProjects := p.composeProjectCandidates(ctx, app, appID)
	if active, _, pr := p.composeProjectAndPS(ctx, app, appID); pr.OK && strings.TrimSpace(active) != "" {
		volProjects = dedupeStringsPreserveOrder(append([]string{strings.TrimSpace(active)}, volProjects...))
	}
	return volProjects
}

func (p *Panel) backupVolumeComposeProjects(ctx context.Context, app db.App, appID string) []string {
	return p.BackupVolumeComposeProjects(ctx, app, appID)
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

func (p *Panel) RecordAuditLog(c *fiber.Ctx, action, targetType, targetID, details string) {
	audit.Record(p.DB, c, action, targetType, targetID, details)
}

func (p *Panel) ResolveRequestedBackupVolume(ctx context.Context, app db.App, requested string) (string, string) {
	requested = strings.TrimSpace(requested)
	if requested != "" {
		ok, msg := volumex.VolumeExists(ctx, requested)
		if !ok {
			if strings.TrimSpace(msg) == "" {
				msg = "Docker volume not found"
			}
			return "", msg
		}
		return requested, ""
	}
	volProjects := p.BackupVolumeComposeProjects(ctx, app, app.ID)
	return volumex.ResolveBackupDataVolumeName(ctx, app.ID, app.Name, volProjects)
}

func (p *Panel) resolveRequestedBackupVolume(ctx context.Context, app db.App, requested string) (string, string) {
	return p.ResolveRequestedBackupVolume(ctx, app, requested)
}

func (p *Panel) GitDeployedSummary(ctx context.Context, appID string, cfg db.AppGitConfig) (shortSHA, subject, pageURL string) {
	sha := strings.TrimSpace(cfg.LastDeployRef)
	if sha == "" {
		return "", "", ""
	}
	if len(sha) >= 7 {
		shortSHA = sha[:7]
	} else {
		shortSHA = sha
	}
	pageURL = utils.CommitPageURL(cfg, sha)
	repoDir := filepath.Join(p.Store.ReservedPath(appID), "repo")
	if gitx.RepoExists(repoDir) {
		if s := gitx.CurrentCommitSubject(ctx, repoDir); s != "" {
			subject = s
		}
	}
	return shortSHA, subject, pageURL
}

func (p *Panel) gitDeployedSummary(ctx context.Context, appID string, cfg db.AppGitConfig) (shortSHA, subject, pageURL string) {
	return p.GitDeployedSummary(ctx, appID, cfg)
}

func (p *Panel) PanelBaseURL(c *fiber.Ctx) string {
	panelDomain := strings.TrimSpace(p.DB.GetSetting(c.UserContext(), "panel_domain"))
	if panelDomain != "" {
		return "https://" + panelDomain
	}
	if c.Protocol() == "https" {
		return "https://" + c.Hostname()
	}
	return "http://" + c.Hostname()
}

func (p *Panel) AppWebhookURL(c *fiber.Ctx, appID string) string {
	return gitx.WebhookURL(p.PanelBaseURL(c), appID)
}

func (p *Panel) appWebhookURL(c *fiber.Ctx, appID string) string {
	return p.AppWebhookURL(c, appID)
}
