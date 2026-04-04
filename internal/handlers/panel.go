package handlers

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"panel/internal/caddy"
	"panel/internal/db"
	"panel/internal/dockerapi"
	"panel/internal/dockerx"
	"panel/internal/logview"
	"panel/internal/sysinfo"
	"panel/internal/volumex"
	"panel/internal/workspace"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

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
		rows, res := dockerx.ComposePS(ctx, p.appSourcePath(ctx, id), p.effectiveComposePaths(ctx, app, id), project)
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

func (p *Panel) Overview(c *fiber.Ctx) error {
	si := sysinfo.Collect(c.UserContext())
	return c.Render("pages/overview", withUser(c, fiber.Map{
		"Nav":   "overview",
		"Title": "Overview",
		"Sys":   si,
	}), "layouts/shell")
}

func (p *Panel) MonitorPage(c *fiber.Ctx) error {
	sys := sysinfo.Collect(c.UserContext())
	rows, errMsg := dockerapi.ListContainerUsage(c.UserContext())
	return c.Render("pages/monitor", withUser(c, fiber.Map{
		"Nav":         "monitor",
		"Title":       "Monitor",
		"Sys":         sys,
		"UsageRows":   rows,
		"DockerError": errMsg,
	}), "layouts/shell")
}

func (p *Panel) MonitorPartial(c *fiber.Ctx) error {
	sys := sysinfo.Collect(c.UserContext())
	rows, errMsg := dockerapi.ListContainerUsage(c.UserContext())
	return c.Render("partials/monitor_stats", fiber.Map{
		"Sys":         sys,
		"UsageRows":   rows,
		"DockerError": errMsg,
	})
}

func (p *Panel) Containers(c *fiber.Ctx) error {
	rows, errMsg := dockerapi.ListContainers(c.UserContext())
	return c.Render("pages/containers", withUser(c, fiber.Map{
		"Nav":         "containers",
		"Title":       "Containers",
		"Containers":  rows,
		"DockerError": errMsg,
	}), "layouts/shell")
}

func (p *Panel) ImagesPage(c *fiber.Ctx) error {
	rows, errMsg := dockerapi.ListImages(c.UserContext())
	return c.Render("pages/images", withUser(c, fiber.Map{
		"Nav":         "images",
		"Title":       "Images",
		"Images":      rows,
		"DockerError": errMsg,
	}), "layouts/shell")
}

func (p *Panel) VolumesPage(c *fiber.Ctx) error {
	names, errMsg := volumex.List(c.UserContext())
	return c.Render("pages/volumes", withUser(c, fiber.Map{
		"Nav":         "volumes",
		"Title":       "Volumes",
		"Volumes":     names,
		"VolumeError": errMsg,
	}), "layouts/shell")
}

type volRow struct {
	Name    string
	IsDir   bool
	RelPath string
}

func (p *Panel) VolumeBrowse(c *fiber.Ctx) error {
	name := strings.TrimSpace(c.Query("name"))
	if !volumex.ValidVolumeName(name) {
		return c.Status(400).SendString("invalid volume")
	}
	rel := c.Query("path", "")
	entries, msg := volumex.ListDir(c.UserContext(), name, rel)
	parent := volumex.ParentRel(rel)
	rows := make([]volRow, 0, len(entries))
	for _, e := range entries {
		rp := e.Name
		if rel != "" {
			rp = rel + "/" + e.Name
		}
		rows = append(rows, volRow{Name: e.Name, IsDir: e.IsDir, RelPath: rp})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].IsDir != rows[j].IsDir {
			return rows[i].IsDir
		}
		return strings.ToLower(rows[i].Name) < strings.ToLower(rows[j].Name)
	})
	return c.Render("pages/volume_browse", withUser(c, fiber.Map{
		"Nav":         "volumes",
		"Title":       name,
		"VolumeName":  name,
		"Path":        rel,
		"ParentPath":  parent,
		"VolRows":     rows,
		"BrowseError": msg,
		"Flash":       c.Query("flash", ""),
	}), "layouts/shell")
}

func (p *Panel) VolumeDownload(c *fiber.Ctx) error {
	name := strings.TrimSpace(c.Query("name"))
	if !volumex.ValidVolumeName(name) {
		return c.Status(400).SendString("invalid volume")
	}
	ctx, cancel := context.WithTimeout(c.UserContext(), 60*time.Minute)
	defer cancel()
	r, err := volumex.OpenTarStream(ctx, name)
	if err != nil {
		return c.Status(500).SendString(err.Error())
	}
	c.Set("Content-Type", "application/gzip")
	safe := strings.ReplaceAll(name, `"`, "")
	c.Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s-backup.tar.gz"`, safe))
	return c.SendStream(r)
}

func (p *Panel) VolumeRestore(c *fiber.Ctx) error {
	name := strings.TrimSpace(c.FormValue("name"))
	if !volumex.ValidVolumeName(name) {
		return c.Status(400).SendString("invalid volume")
	}
	fh, err := c.FormFile("backup")
	if err != nil {
		return c.Status(400).SendString("upload a .tar.gz backup")
	}
	src, err := fh.Open()
	if err != nil {
		return c.Status(500).SendString(err.Error())
	}
	defer src.Close()
	msg := volumex.RestoreTarGz(c.UserContext(), name, src)
	q := url.Values{}
	q.Set("name", name)
	if msg != "" {
		q.Set("flash", msg)
	}
	return c.Redirect("/volumes/browse?" + q.Encode())
}

func (p *Panel) AppsPage(c *fiber.Ctx) error {
	list, err := p.DB.ListApps(c.UserContext())
	if err != nil {
		return c.Status(500).SendString(err.Error())
	}
	type appListItem struct {
		db.App
		State           string
		RunningCount    int
		ExitedCount     int
		ContainerCount  int
	}
	items := make([]appListItem, 0, len(list))
	for _, app := range list {
		project := p.activeComposeProjectName(c.UserContext(), app, app.ID)
		rows, _ := dockerx.ComposePS(c.UserContext(), p.appSourcePath(c.UserContext(), app.ID), p.effectiveComposePaths(c.UserContext(), app, app.ID), project)
		item := appListItem{App: app, State: "not deployed"}
		for _, row := range rows {
			item.ContainerCount++
			state := strings.ToLower(strings.TrimSpace(row.State))
			status := strings.ToLower(strings.TrimSpace(row.Status))
			switch state {
			case "running":
				item.RunningCount++
			case "exited":
				// exited(0) = completed successfully (migrate, init containers) — treat as ok
				if strings.Contains(status, "exited (0)") {
					item.RunningCount++
				} else {
					item.ExitedCount++
				}
			case "dead":
				item.ExitedCount++
			}
		}
		if item.ContainerCount > 0 {
			switch {
			case item.RunningCount == item.ContainerCount:
				item.State = "running"
			case item.RunningCount > 0:
				item.State = "degraded"
			case item.ExitedCount > 0:
				item.State = "failed"
			default:
				item.State = "stopped"
			}
		}
		items = append(items, item)
	}
	return c.Render("pages/apps", withUser(c, fiber.Map{
		"Nav":   "apps",
		"Title": "Apps",
		"Apps":  items,
	}), "layouts/shell")
}

func (p *Panel) CreateApp(c *fiber.Ctx) error {
	name := strings.TrimSpace(c.FormValue("name"))
	if name == "" {
		return c.Status(400).SendString("name required")
	}
	if sanitizeProjectName(name) == "" {
		return c.Status(400).SendString("use lowercase letters, numbers, spaces, hyphens, or underscores")
	}
	id := uuid.NewString()
	if err := os.MkdirAll(p.Store.Path(id), 0750); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	if err := p.Store.WriteMeta(id, name); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	if err := p.DB.CreateApp(c.UserContext(), id, name); err != nil {
		_ = os.RemoveAll(p.Store.Path(id))
		if strings.Contains(strings.ToLower(err.Error()), "already exists") {
			return c.Status(400).SendString(err.Error())
		}
		return c.Status(500).SendString(err.Error())
	}
	sourceType := strings.TrimSpace(c.FormValue("source_type"))
	if sourceType == "github" || sourceType == "git" {
		_ = p.DB.SetAppSourceType(c.UserContext(), id, "git")
		repoURL := strings.TrimSpace(c.FormValue("repo_url"))
		if repoURL != "" {
			cfg := db.AppGitConfig{
				AppID:         id,
				Provider:      "github",
				RepoURL:       normalizeRepoURL(repoURL),
				RepoFullName:  repoFullNameFromURL(repoURL),
				Branch:        normalizeBranch(c.FormValue("branch")),
				AuthMode:      strings.TrimSpace(c.FormValue("auth_mode")),
				Token:         strings.TrimSpace(c.FormValue("token")),
				WebhookSecret: randomSecret(),
				AutoDeploy:    true,
			}
			if cfg.AuthMode == "" {
				cfg.AuthMode = "public"
			}
			if err := p.DB.UpsertAppGitConfig(c.UserContext(), cfg); err != nil {
				return c.Status(500).SendString(err.Error())
			}
			if err := os.MkdirAll(filepath.Join(p.Store.ReservedPath(id), "repo"), 0750); err != nil {
				return c.Status(500).SendString(err.Error())
			}
		}
	}
	return c.Redirect(fmt.Sprintf("/apps/%s", id))
}

func (p *Panel) SaveAppCompose(c *fiber.Ctx) error {
	id := c.Params("id")
	if _, err := p.DB.GetApp(c.UserContext(), id); err != nil {
		return c.Status(404).SendString("app not found")
	}
	raw := workspace.NormalizeComposeRel(c.FormValue("compose_file"))
	if err := p.DB.UpdateComposeFile(c.UserContext(), id, raw); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	if err := p.syncAppCaddyOverride(c, id); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	return c.Redirect(fmt.Sprintf("/apps/%s?tab=overview", id))
}

func (p *Panel) SaveAppEnv(c *fiber.Ctx) error {
	id := c.Params("id")
	if _, err := p.DB.GetApp(c.UserContext(), id); err != nil {
		return c.Status(404).SendString("app not found")
	}
	content := c.FormValue("env")
	if err := p.Store.WriteDotEnv(p.appSourcePath(c.UserContext(), id), content); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	return c.Redirect(fmt.Sprintf("/apps/%s?tab=environment", id))
}

func (p *Panel) AppShow(c *fiber.Ctx) error {
	id := c.Params("id")
	app, err := p.DB.GetApp(c.UserContext(), id)
	if err != nil {
		return c.Status(404).SendString("app not found")
	}
	tab := c.Query("tab", "overview")
	isGitApp := p.isGitApp(c.UserContext(), id)
	switch tab {
	case "overview", "files", "logs", "containers", "environment", "deployment", "volumes", "terminal", "domains", "git":
	default:
		tab = "overview"
	}
	if isGitApp && tab == "files" {
		tab = "git"
	}
	rel := c.Query("path", "")
	var children []workspace.FileEntry
	if !isGitApp {
		children, err = p.Store.ListChildren(id, rel)
		if err != nil {
			children = nil
		}
	}
	parent := p.Store.ParentRel(rel)
	sourcePath := p.appSourcePath(c.UserContext(), id)
	hasDF, _ := p.Store.HasDockerArtifacts(sourcePath)

	composePath := p.composeFilePath(app, id)
	composeDisplay := workspace.NormalizeComposeRel(app.ComposeFile)
	var composeName string
	hasComp := false
	if st, err := os.Stat(composePath); err == nil && !st.IsDir() {
		hasComp = true
		composeName = composeDisplay
	}

	storagePath := filepath.ToSlash(sourcePath)

	envContent, _ := p.Store.ReadDotEnv(sourcePath)

	var composeRows []dockerx.ComposePsRow
	var composePsMsg string
	if hasComp {
		ctx, cancel := context.WithTimeout(c.UserContext(), 60*time.Second)
		project := p.activeComposeProjectName(ctx, app, id)
		rows, pr := dockerx.ComposePS(ctx, sourcePath, p.effectiveComposePaths(c.UserContext(), app, id), project)
		cancel()
		if pr.OK {
			composeRows = rows
		} else {
			composePsMsg = pr.Output
		}
	}

	deployLogs, _ := p.DB.ListDeployLogs(c.UserContext(), id, 5)
	appVols, appVolErr := volumex.ListForApp(c.UserContext(), id)
	deployBusy := c.Query("busy") == "1"
	liveOut, liveAct, liveRun := p.deploySnapshot(id)

	// Domains tab data
	appDomains, _ := p.DB.ListAppDomains(c.UserContext(), id)
	for i := range appDomains {
		sanitizeDomainRecord(&appDomains[i])
	}
	domainServices := p.loadComposeServices(c, id)
	gitCfg, hasGitCfg := p.appGitConfig(c.UserContext(), id)
	panelDomain := p.DB.GetSetting(c.UserContext(), settingPanelDomain)
	appWebhookURL := ""
	if hasGitCfg {
		appWebhookURL = p.appWebhookURL(c, id)
	}

	return c.Render("pages/app_show", withUser(c, fiber.Map{
		"Nav":                "apps",
		"Title":              app.Name,
		"App":                app,
		"Tab":                tab,
		"Path":               rel,
		"ParentPath":         parent,
		"Children":           children,
		"HasDockerfile":      hasDF,
		"IsGitApp":           isGitApp,
		"HasGitConfig":       hasGitCfg,
		"GitConfig":          gitCfg,
		"PanelDomain":        panelDomain,
		"AppWebhookURL":      appWebhookURL,
		"HasCompose":         hasComp,
		"ComposeFile":        composeName,
		"ComposeFileSetting": composeDisplay,
		"ID":                 id,
		"StoragePath":        storagePath,
		"UploadZipTarget":    fmt.Sprintf("/apps/%s/upload-zip", id),
		"UploadFileTarget":   fmt.Sprintf("/apps/%s/upload", id),
		"ComposeRows":        composeRows,
		"RunningCount":       countComposeOkRunning(composeRows),
		"ExitedCount":        countComposeState(composeRows, "exited"),
		"DeadCount":          countComposeState(composeRows, "dead"),
		"ComposePsMsg":       composePsMsg,
		"BrowseFlash":        "",
		"DeleteTarget":       fmt.Sprintf("/apps/%s/files/delete", id),
		"EnvContent":         envContent,
		"DeployLogs":         deployLogs,
		"AppVolumes":         appVols,
		"AppVolumeError":     appVolErr,
		"DeployLiveOutput":   liveOut,
		"DeployLiveAction":   liveAct,
		"DeployJobRunning":   liveRun,
		"DeployQueueBusy":    deployBusy,
		"AppDomains":         appDomains,
		"DomainServices":     domainServices,
		"DomainSaved":        c.Query("domainSaved") == "1",
		"GitSaved":           c.Query("saved") == "1",
		"GitSynced":          c.Query("synced") == "1",
		"GitError":           c.Query("error"),
		"SourceSwitched":     c.Query("sourceSwitched") == "1",
	}), "layouts/shell")
}

func (p *Panel) BrowsePartial(c *fiber.Ctx) error {
	id := c.Params("id")
	if _, err := p.DB.GetApp(c.UserContext(), id); err != nil {
		return c.Status(404).SendString("not found")
	}
	if p.isGitApp(c.UserContext(), id) {
		return c.Status(400).SendString("files browser is disabled for git-backed apps")
	}
	return p.renderBrowse(c, id, c.Query("path", ""), "")
}

func (p *Panel) renderBrowse(c *fiber.Ctx, id, rel, flash string) error {
	children, err := p.Store.ListChildren(id, rel)
	if err != nil {
		return c.Status(500).SendString(err.Error())
	}
	parent := p.Store.ParentRel(rel)
	return c.Render("partials/browser", fiber.Map{
		"ID":           id,
		"Path":         rel,
		"ParentPath":   parent,
		"Children":     children,
		"BrowseFlash":  flash,
		"DeleteTarget": fmt.Sprintf("/apps/%s/files/delete", id),
	})
}

func (p *Panel) BrowseDelete(c *fiber.Ctx) error {
	id := c.Params("id")
	if _, err := p.DB.GetApp(c.UserContext(), id); err != nil {
		return c.Status(404).SendString("not found")
	}
	if p.isGitApp(c.UserContext(), id) {
		return c.Status(400).SendString("files delete is disabled for git-backed apps")
	}
	returnPath := c.FormValue("path")
	var paths []string
	c.Request().PostArgs().VisitAll(func(key, val []byte) {
		if string(key) == "paths" {
			paths = append(paths, string(val))
		}
	})
	if len(paths) == 0 {
		return p.renderBrowse(c, id, returnPath, "Select at least one file or folder.")
	}
	var errs []string
	for _, pth := range paths {
		if err := p.Store.RemoveRel(id, pth); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", pth, err))
		}
	}
	flash := ""
	if len(errs) > 0 {
		flash = "Some deletions failed: " + strings.Join(errs, " · ")
	}
	return p.renderBrowse(c, id, returnPath, flash)
}

const (
	maxWorkspaceFileInline   = 8 << 20  // 8 MiB
	maxWorkspaceFileDownload = 512 << 20 // 512 MiB
)

func workspaceFileContentType(abs string, head []byte) string {
	t := http.DetectContentType(head)
	if t != "application/octet-stream" && t != "" {
		return t
	}
	switch strings.ToLower(filepath.Ext(abs)) {
	case ".html", ".htm":
		return "text/html; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".js", ".mjs":
		return "text/javascript; charset=utf-8"
	case ".json":
		return "application/json; charset=utf-8"
	case ".yml", ".yaml":
		return "text/yaml; charset=utf-8"
	case ".txt", ".conf", ".env", ".md", ".log", ".ini", ".sh":
		return "text/plain; charset=utf-8"
	default:
		return "application/octet-stream"
	}
}

func safeContentDispositionFilename(rel string) string {
	base := filepath.Base(strings.ReplaceAll(rel, "\\", "/"))
	base = strings.ReplaceAll(base, `"`, "")
	if base == "." || base == "/" || base == "" {
		return "file"
	}
	return base
}

// WorkspaceFile serves a single file from the app workspace (?path=relative). Use download=1 for attachment.
func (p *Panel) WorkspaceFile(c *fiber.Ctx) error {
	id := c.Params("id")
	if _, err := p.DB.GetApp(c.UserContext(), id); err != nil {
		return c.Status(404).SendString("app not found")
	}
	if p.isGitApp(c.UserContext(), id) {
		return c.Status(400).SendString("file view is disabled for git-backed apps")
	}
	rel := c.Query("path", "")
	full, err := p.Store.SafeFilePath(id, rel)
	if err != nil {
		return c.Status(400).SendString("invalid path")
	}
	st, err := os.Stat(full)
	if err != nil {
		if os.IsNotExist(err) {
			return c.Status(404).SendString("not found")
		}
		return c.Status(500).SendString(err.Error())
	}
	if st.IsDir() {
		return c.Status(400).SendString("not a file")
	}
	download := c.Query("download") == "1"
	maxSz := int64(maxWorkspaceFileInline)
	if download {
		maxSz = maxWorkspaceFileDownload
	}
	if st.Size() > maxSz {
		if !download {
			return c.Status(413).SendString("file too large for inline view; add ?download=1")
		}
		return c.Status(413).SendString("file too large")
	}
	f, err := os.Open(full)
	if err != nil {
		return c.Status(500).SendString(err.Error())
	}
	defer f.Close()
	b, err := io.ReadAll(f)
	if err != nil {
		return c.Status(500).SendString(err.Error())
	}
	head := b
	if len(head) > 512 {
		head = head[:512]
	}
	ct := workspaceFileContentType(full, head)
	fn := safeContentDispositionFilename(rel)
	if download {
		c.Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, fn))
	} else {
		c.Set("Content-Disposition", fmt.Sprintf(`inline; filename="%s"`, fn))
	}
	c.Type(ct)
	return c.Send(b)
}

func (p *Panel) WorkspaceFileModal(c *fiber.Ctx) error {
	id := c.Params("id")
	if _, err := p.DB.GetApp(c.UserContext(), id); err != nil {
		return c.Status(404).SendString("app not found")
	}
	if p.isGitApp(c.UserContext(), id) {
		return c.Status(400).Render("partials/file_preview_modal", fiber.Map{
			"PreviewError": "Files preview is disabled for git-backed apps. Use the Git tab and redeploy from repository source.",
		})
	}
	rel := c.Query("path", "")
	full, err := p.Store.SafeFilePath(id, rel)
	if err != nil {
		return c.Status(400).Render("partials/file_preview_modal", fiber.Map{
			"PreviewError": "invalid path",
		})
	}
	st, err := os.Stat(full)
	if err != nil {
		if os.IsNotExist(err) {
			return c.Status(404).Render("partials/file_preview_modal", fiber.Map{
				"PreviewError": "file not found",
			})
		}
		return c.Status(500).Render("partials/file_preview_modal", fiber.Map{
			"PreviewError": err.Error(),
		})
	}
	if st.IsDir() {
		return c.Status(400).Render("partials/file_preview_modal", fiber.Map{
			"PreviewError": "cannot preview a directory",
		})
	}
	if st.Size() > maxWorkspaceFileInline {
		return c.Status(413).Render("partials/file_preview_modal", fiber.Map{
			"PreviewName":  safeContentDispositionFilename(rel),
			"PreviewPath":  rel,
			"PreviewTooBig": true,
		})
	}
	b, err := os.ReadFile(full)
	if err != nil {
		return c.Status(500).Render("partials/file_preview_modal", fiber.Map{
			"PreviewError": err.Error(),
		})
	}
	head := b
	if len(head) > 512 {
		head = head[:512]
	}
	ct := workspaceFileContentType(full, head)
	textTypes := []string{"text/", "application/json", "application/javascript", "application/xml", "text/yaml"}
	isText := false
	for _, tt := range textTypes {
		if strings.HasPrefix(ct, tt) {
			isText = true
			break
		}
	}
	if !isText {
		ct = "application/octet-stream"
	}
	return c.Render("partials/file_preview_modal", fiber.Map{
		"PreviewName":    safeContentDispositionFilename(rel),
		"PreviewPath":    rel,
		"PreviewContent": string(b),
		"PreviewCT":      ct,
		"PreviewBinary":  !isText,
	})
}

func (p *Panel) ComposeFileView(c *fiber.Ctx) error {
	id := c.Params("id")
	app, err := p.DB.GetApp(c.UserContext(), id)
	if err != nil {
		return c.Status(404).SendString("app not found")
	}
	if err := p.syncAppCaddyOverride(c, id); err != nil {
		c.Type("text/plain; charset=utf-8")
		return c.Status(500).SendString(err.Error())
	}
	cp := p.composeFilePath(app, id)
	overridePath := p.composeOverridePath(id)
	b, err := os.ReadFile(overridePath)
	if err != nil {
		b, err = os.ReadFile(cp)
		if err != nil {
			c.Type("text/plain; charset=utf-8")
			return c.Status(404).SendString("Compose file not found. Set the path on Overview or upload the file under Files.")
		}
	}
	const maxPreview = 1024 * 1024
	suffix := ""
	if len(b) > maxPreview {
		b = b[:maxPreview]
		suffix = "\n\n... (truncated at 1 MB for this preview)\n"
	}
	c.Type("text/plain; charset=utf-8")
	return c.SendString(string(b) + suffix)
}

func (p *Panel) ComposeFileModal(c *fiber.Ctx) error {
	id := c.Params("id")
	app, err := p.DB.GetApp(c.UserContext(), id)
	if err != nil {
		return c.Status(404).SendString("app not found")
	}
	if err := p.syncAppCaddyOverride(c, id); err != nil {
		return c.Status(500).Render("partials/compose_preview_modal", fiber.Map{
			"ComposePreview": err.Error(),
			"ComposeError":   true,
		})
	}
	cp := p.composeFilePath(app, id)
	overridePath := p.composeOverridePath(id)
	b, err := os.ReadFile(overridePath)
	if err != nil {
		b, err = os.ReadFile(cp)
		if err != nil {
			return c.Status(404).Render("partials/compose_preview_modal", fiber.Map{
				"ComposePreview": "Compose file not found. Set the path on Overview or upload the file under Files.",
				"ComposeError":   true,
			})
		}
	}
	const maxPreview = 1024 * 1024
	suffix := ""
	if len(b) > maxPreview {
		b = b[:maxPreview]
		suffix = "\n\n... (truncated at 1 MB for this preview)\n"
	}
	return c.Render("partials/compose_preview_modal", fiber.Map{
		"ComposePreview": string(b) + suffix,
		"ComposeError":   false,
	})
}

func (p *Panel) AppComposePartial(c *fiber.Ctx) error {
	return p.renderComposeTable(c, c.Params("id"))
}

func (p *Panel) renderComposeTable(c *fiber.Ctx, id string) error {
	app, err := p.DB.GetApp(c.UserContext(), id)
	if err != nil {
		return c.Status(404).SendString("not found")
	}
	dir := p.appSourcePath(c.UserContext(), id)
	cp := p.composeFilePath(app, id)
	if _, err := os.Stat(cp); err != nil {
		return c.Render("partials/compose_table", fiber.Map{
			"ID":           id,
			"ComposeRows":  []dockerx.ComposePsRow(nil),
			"ComposePsMsg": "Compose file not found. Set the filename on Overview or upload it in Files.",
		})
	}
	if err := p.syncAppCaddyOverride(c, id); err != nil {
		return c.Render("partials/compose_table", fiber.Map{
			"ID":           id,
			"ComposeRows":  []dockerx.ComposePsRow(nil),
			"ComposePsMsg": err.Error(),
		})
	}
	ctx, cancel := context.WithTimeout(c.UserContext(), 60*time.Second)
	defer cancel()
	project := p.activeComposeProjectName(ctx, app, id)
	rows, res := dockerx.ComposePS(ctx, dir, p.effectiveComposePaths(c.UserContext(), app, id), project)
	errMsg := ""
	if !res.OK {
		errMsg = res.Output
		rows = nil
	}
	return c.Render("partials/compose_table", fiber.Map{
		"ID":           id,
		"ComposeRows":  rows,
		"ComposePsMsg": errMsg,
	})
}

func (p *Panel) ContainerRestartOp(c *fiber.Ctx) error {
	id := c.Params("id")
	if _, err := p.DB.GetApp(c.UserContext(), id); err != nil {
		return c.Status(404).SendString("not found")
	}
	name := strings.TrimSpace(c.FormValue("container"))
	ctx, cancel := context.WithTimeout(c.UserContext(), 3*time.Minute)
	defer cancel()
	_ = dockerx.ContainerRestart(ctx, name)
	return p.renderComposeTable(c, id)
}

func (p *Panel) ContainerRemoveOp(c *fiber.Ctx) error {
	id := c.Params("id")
	if _, err := p.DB.GetApp(c.UserContext(), id); err != nil {
		return c.Status(404).SendString("not found")
	}
	name := strings.TrimSpace(c.FormValue("container"))
	ctx, cancel := context.WithTimeout(c.UserContext(), 3*time.Minute)
	defer cancel()
	_ = dockerx.ContainerRemove(ctx, name)
	return p.renderComposeTable(c, id)
}

func (p *Panel) ContainerRemoveSelectedOp(c *fiber.Ctx) error {
	id := c.Params("id")
	if _, err := p.DB.GetApp(c.UserContext(), id); err != nil {
		return c.Status(404).SendString("not found")
	}
	var names []string
	c.Request().PostArgs().VisitAll(func(key, val []byte) {
		if string(key) == "containers" {
			names = append(names, strings.TrimSpace(string(val)))
		}
	})
	ctx, cancel := context.WithTimeout(c.UserContext(), 5*time.Minute)
	defer cancel()
	for _, name := range names {
		if !p.containerBelongsToApp(id, name) {
			continue
		}
		_ = dockerx.ContainerRemove(ctx, name)
	}
	return p.renderComposeTable(c, id)
}

func logTailLines(q string) int {
	switch strings.TrimSpace(q) {
	case "100":
		return 100
	case "500":
		return 500
	case "1000":
		return 1000
	case "300":
		return 300
	default:
		return 300
	}
}

func (p *Panel) containerBelongsToApp(appID, containerName string) bool {
	containerName = strings.TrimSpace(containerName)
	if containerName == "" {
		return false
	}
	app, err := p.DB.GetApp(context.Background(), appID)
	if err != nil {
		return strings.Contains(containerName, appID)
	}
	for _, project := range p.legacyProjectNames(app, appID) {
		if strings.Contains(containerName, project) {
			return true
		}
	}
	return false
}

func (p *Panel) AppLogPartial(c *fiber.Ctx) error {
	id := c.Params("id")
	if _, err := p.DB.GetApp(c.UserContext(), id); err != nil {
		return c.Status(404).SendString("not found")
	}
	name := strings.TrimPrefix(strings.TrimSpace(c.Query("container")), "/")
	tail := logTailLines(c.Query("tail"))
	if name == "" {
		return c.Render("partials/log_view", fiber.Map{
			"LogHTML": logview.FormatDockerLog("Select a container from the list."),
			"LogMeta": "",
		})
	}
	if !p.containerBelongsToApp(id, name) {
		return c.Render("partials/log_view", fiber.Map{
			"LogHTML": logview.FormatDockerLog("That container does not belong to this app."),
			"LogMeta": "",
		})
	}
	ctx, cancel := context.WithTimeout(c.UserContext(), 45*time.Second)
	defer cancel()
	res := dockerx.DockerLogs(ctx, name, tail)
	raw := res.Output
	if !res.OK && strings.TrimSpace(raw) == "" {
		raw = "docker logs failed."
	}
	status := "ok"
	if !res.OK {
		status = "error"
	}
	meta := fmt.Sprintf("%s · %s · last %d lines", name, status, tail)
	html := logview.FormatDockerLog(raw)
	return c.Render("partials/log_view", fiber.Map{
		"LogHTML": html,
		"LogMeta": meta,
	})
}

func (p *Panel) AppExec(c *fiber.Ctx) error {
	id := c.Params("id")
	if _, err := p.DB.GetApp(c.UserContext(), id); err != nil {
		return c.Status(404).SendString("not found")
	}
	name := strings.TrimPrefix(strings.TrimSpace(c.FormValue("container")), "/")
	cmd := c.FormValue("command")
	if !p.containerBelongsToApp(id, name) {
		return c.Status(400).SendString("invalid container for this app")
	}
	ctx, cancel := context.WithTimeout(c.UserContext(), 3*time.Minute)
	defer cancel()
	res := dockerx.DockerExec(ctx, name, cmd)
	out := res.Output
	if strings.TrimSpace(out) == "" {
		out = "(no output — either the command produced nothing or the container has no such path)"
	} else if !res.OK {
		// non-zero exit — append note
		out = out + "\n[non-zero exit: " + res.Output + "]"
	}
	return c.Render("partials/terminal_out", fiber.Map{
		"ExecHTML": logview.FormatTerminalOutput(out),
		"ExecOK":   res.OK,
	})
}

func (p *Panel) ClearDeployLogs(c *fiber.Ctx) error {
	id := c.Params("id")
	if _, err := p.DB.GetApp(c.UserContext(), id); err != nil {
		return c.Status(404).SendString("app not found")
	}
	if err := p.DB.ClearDeployLogs(c.UserContext(), id); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	return c.Redirect(fmt.Sprintf("/apps/%s?tab=deployment", id))
}

func (p *Panel) DeleteApp(c *fiber.Ctx) error {
	id := c.Params("id")
	app, err := p.DB.GetApp(c.UserContext(), id)
	if err != nil {
		return c.Status(404).SendString("app not found")
	}
	dir := p.appSourcePath(c.UserContext(), id)
	cp := p.composeFilePath(app, id)
	ctx, cancel := context.WithTimeout(c.UserContext(), 15*time.Minute)
	defer cancel()
	var cleanupErrs []string
	if _, err := os.Stat(cp); err == nil {
		for _, project := range p.legacyProjectNames(app, id) {
			if res := dockerx.ComposeDownDeleteProject(ctx, dir, p.effectiveComposePaths(c.UserContext(), app, id), project, nil); !res.OK && strings.TrimSpace(res.Output) != "" && !strings.Contains(strings.ToLower(res.Output), "no resource found") {
				cleanupErrs = append(cleanupErrs, res.Output)
			}
		}
	}
	for _, project := range p.legacyProjectNames(app, id) {
		if errs := dockerapi.RemoveAppContainers(ctx, project); len(errs) > 0 {
			cleanupErrs = append(cleanupErrs, errs...)
		}
		if errs := dockerapi.RemoveAppImages(ctx, project); len(errs) > 0 {
			cleanupErrs = append(cleanupErrs, errs...)
		}
		if errs := dockerapi.RemoveAppNetworks(ctx, project); len(errs) > 0 {
			cleanupErrs = append(cleanupErrs, errs...)
		}
		if msg := volumex.RemoveMatching(ctx, project); msg != "" {
			cleanupErrs = append(cleanupErrs, msg)
		}
	}
	if len(cleanupErrs) > 0 {
		return c.Status(500).SendString(strings.Join(cleanupErrs, "\n"))
	}
	if err := os.RemoveAll(dir); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	if err := p.DB.DeleteApp(c.UserContext(), id); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	return c.Redirect("/apps")
}

func (p *Panel) UploadZip(c *fiber.Ctx) error {
	id := c.Params("id")
	if _, err := p.DB.GetApp(c.UserContext(), id); err != nil {
		return c.Status(404).SendString("app not found")
	}
	if p.isGitApp(c.UserContext(), id) {
		return c.Status(400).SendString("ZIP upload is disabled for git-backed apps")
	}
	app, _ := p.DB.GetApp(c.UserContext(), id)
	fh, err := c.FormFile("archive")
	if err != nil {
		return c.Status(400).SendString("missing archive field (zip)")
	}
	src, err := fh.Open()
	if err != nil {
		return c.Status(500).SendString(err.Error())
	}
	defer src.Close()
	tmp, err := os.CreateTemp("", "panel-upload-*.zip")
	if err != nil {
		return c.Status(500).SendString(err.Error())
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := io.Copy(tmp, src); err != nil {
		_ = tmp.Close()
		return c.Status(500).SendString(err.Error())
	}
	if err := tmp.Close(); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	st, err := os.Stat(tmpPath)
	if err != nil {
		return c.Status(500).SendString(err.Error())
	}
	f, err := os.Open(tmpPath)
	if err != nil {
		return c.Status(500).SendString(err.Error())
	}
	defer f.Close()
	if err := p.Store.ClearAllUserFiles(id); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	if err := p.Store.ExtractZip(id, f, st.Size()); err != nil {
		return c.Status(400).SendString(err.Error())
	}
	if err := p.Store.WriteMeta(id, app.Name); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	if err := p.syncAppCaddyOverride(c, id); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	return c.Redirect(fmt.Sprintf("/apps/%s?tab=files", id))
}

func (p *Panel) UploadFile(c *fiber.Ctx) error {
	id := c.Params("id")
	if _, err := p.DB.GetApp(c.UserContext(), id); err != nil {
		return c.Status(404).SendString("app not found")
	}
	if p.isGitApp(c.UserContext(), id) {
		return c.Status(400).SendString("file upload is disabled for git-backed apps")
	}
	file, err := c.FormFile("file")
	if err != nil {
		return c.Status(400).SendString("missing file")
	}
	src, err := file.Open()
	if err != nil {
		return c.Status(500).SendString(err.Error())
	}
	defer src.Close()
	if _, err := p.Store.SaveUploadedFile(id, file.Filename, src); err != nil {
		return c.Status(400).SendString("invalid path")
	}
	return c.Redirect(fmt.Sprintf("/apps/%s?tab=files", id))
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

func (p *Panel) ComposeUp(c *fiber.Ctx) error {
	return p.enqueueCompose(c, "Deploy", dockerx.ComposeUp)
}

func (p *Panel) ComposeDown(c *fiber.Ctx) error {
	return p.enqueueCompose(c, "Stop", dockerx.ComposeDown)
}

func (p *Panel) ComposeRestart(c *fiber.Ctx) error {
	return p.enqueueCompose(c, "Stack restart", dockerx.ComposeRestart)
}

func (p *Panel) ComposeRedeploy(c *fiber.Ctx) error {
	return p.enqueueCompose(c, "Redeploy (pull + up)", dockerx.ComposePullUp)
}

func (p *Panel) GlobalImageRemove(c *fiber.Ctx) error {
	imageID := strings.TrimSpace(c.FormValue("image_id"))
	if imageID == "" {
		return c.Status(400).SendString("image_id required")
	}
	if err := dockerapi.RemoveImageByID(c.UserContext(), imageID); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	return c.Redirect("/images")
}

func (p *Panel) GlobalContainerRemove(c *fiber.Ctx) error {
	name := strings.TrimSpace(c.FormValue("name"))
	if name == "" {
		return c.Status(400).SendString("name required")
	}
	if err := dockerapi.RemoveContainerByName(c.UserContext(), name); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	return c.Redirect("/containers")
}

func (p *Panel) GlobalVolumeRemove(c *fiber.Ctx) error {
	name := strings.TrimSpace(c.FormValue("name"))
	if name == "" {
		return c.Status(400).SendString("name required")
	}
	if err := dockerapi.RemoveVolumeByName(c.UserContext(), name); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	return c.Redirect("/volumes")
}

// GlobalImagePrune removes all unused (dangling) images.
func (p *Panel) GlobalImagePrune(c *fiber.Ctx) error {
	if err := dockerapi.PruneImages(c.UserContext()); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	return c.Redirect("/images")
}

// GlobalContainerPrune removes all stopped containers.
func (p *Panel) GlobalContainerPrune(c *fiber.Ctx) error {
	if err := dockerapi.PruneContainers(c.UserContext()); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	return c.Redirect("/containers")
}

func (p *Panel) enqueueCompose(c *fiber.Ctx, action string, fn func(context.Context, string, []string, string, io.Writer) dockerx.Result) error {
	id := c.Params("id")
	app, err := p.DB.GetApp(c.UserContext(), id)
	if err != nil {
		return c.Status(404).SendString("app not found")
	}
	if p.isGitApp(c.UserContext(), id) {
		ctx, cancel := context.WithTimeout(c.UserContext(), 15*time.Minute)
		if out, err := p.syncGitAppSource(ctx, id); err != nil {
			cancel()
			msg := "[error]\nGit sync failed.\n\n" + err.Error()
			if strings.TrimSpace(out) != "" {
				msg += "\n\n" + out
			}
			_ = p.DB.InsertDeployLog(c.UserContext(), id, action, false, msg)
			return c.Redirect(fmt.Sprintf("/apps/%s?tab=deployment", id))
		}
		cancel()
	}
	cp := p.composeFilePath(app, id)
	if _, err := os.Stat(cp); err != nil {
		msg := "[error]\nCompose file not found. Set path on Overview or upload the file / sync the repository first."
		_ = p.DB.InsertDeployLog(c.UserContext(), id, action, false, msg)
		return c.Redirect(fmt.Sprintf("/apps/%s?tab=deployment", id))
	}
	if err := p.syncAppCaddyOverride(c, id); err != nil {
		msg := "[error]\n" + err.Error()
		_ = p.DB.InsertDeployLog(c.UserContext(), id, action, false, msg)
		return c.Redirect(fmt.Sprintf("/apps/%s?tab=deployment", id))
	}
	project := p.composeProjectName(app, id)
	if err := p.startComposeJob(id, project, p.effectiveComposePaths(c.UserContext(), app, id), action, fn); err != nil {
		return c.Redirect(fmt.Sprintf("/apps/%s?tab=deployment&busy=1", id))
	}
	return c.Redirect(fmt.Sprintf("/apps/%s?tab=deployment", id))
}

func (p *Panel) DeployProgressPartial(c *fiber.Ctx) error {
	id := c.Params("id")
	if _, err := p.DB.GetApp(c.UserContext(), id); err != nil {
		return c.Status(404).SendString("not found")
	}
	r := p.getDeployRun(id)
	r.mu.Lock()
	out := r.Output.String()
	running := r.Running
	act := r.Action
	r.mu.Unlock()
	return c.Render("partials/deploy_progress", fiber.Map{
		"ID":          id,
		"LiveOutput":  out,
		"LiveRunning": running,
		"LiveAction":  act,
		"OOBOnly":     c.Query("oob") == "1",
	})
}

func formatOut(r dockerx.Result) string {
	status := "ok"
	if !r.OK {
		status = "error"
	}
	return fmt.Sprintf("[%s]\n%s", status, r.Output)
}
