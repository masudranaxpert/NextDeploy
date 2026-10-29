package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"panel/internal/db"
	"panel/internal/dockerx"
	"panel/internal/migrate"
	"panel/internal/volumex"

	"github.com/gofiber/fiber/v2"
)

func migrateShellCommand(downloadURL string) string {
	return "curl -fsSL https://raw.githubusercontent.com/masudranaxpert/NextDeploy/main/migrate.sh | sudo bash -s -- \\\n  --url \"" + downloadURL + "\""
}

func (p *Panel) MigrateEstimate(c *fiber.Ctx) error {
	ctx := c.UserContext()
	appIDs := c.Query("apps")
	selected := parseAppIDList(appIDs)
	apps, err := p.migrateAppsByIDs(ctx, selected)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}
	est, err := p.migrateEstimateApps(ctx, apps)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{
		"workspace_bytes": est.WorkspaceBytes,
		"volume_bytes":    est.VolumeBytes,
		"total_bytes":     est.TotalBytes(),
		"total_human":     migrate.FormatBytes(est.TotalBytes()),
		"app_count":       est.AppCount,
		"volume_count":    est.VolumeCount,
	})
}

func (p *Panel) MigrateExportStart(c *fiber.Ctx) error {
	ctx := c.UserContext()
	var selected []string
	c.Request().PostArgs().VisitAll(func(key, val []byte) {
		if string(key) == "app_ids" {
			id := strings.TrimSpace(string(val))
			if id != "" {
				selected = append(selected, id)
			}
		}
	})
	if len(selected) == 0 {
		raw := c.FormValue("app_ids")
		if raw == "" {
			raw = c.Query("app_ids")
		}
		selected = parseAppIDList(raw)
	}
	selected = parseAppIDList(strings.Join(selected, ","))
	if len(selected) == 0 {
		if WantsJSON(c) {
			return c.Status(400).JSON(fiber.Map{"ok": false, "error": "Select at least one app to export."})
		}
		SetFlashError(c, "Select at least one app to export.")
		return c.Redirect("/migrate")
	}
	apps, err := p.migrateAppsByIDs(ctx, selected)
	if err != nil {
		if WantsJSON(c) {
			return c.Status(400).JSON(fiber.Map{"ok": false, "error": err.Error()})
		}
		SetFlashError(c, err.Error())
		return c.Redirect("/migrate")
	}
	est, _ := p.migrateEstimateApps(ctx, apps)
	running, err := p.DB.HasRunningMigrateExport(ctx)
	if err != nil {
		if WantsJSON(c) {
			return c.Status(500).JSON(fiber.Map{"ok": false, "error": "Could not check export status."})
		}
		SetFlashError(c, "Could not check export status.")
		return c.Redirect("/migrate")
	}
	if running {
		if WantsJSON(c) {
			return c.Status(409).JSON(fiber.Map{"ok": false, "error": "An export is already running. Wait for it to finish or delete it first."})
		}
		SetFlashError(c, "An export is already running. Wait for it to finish or delete it first.")
		return c.Redirect("/migrate")
	}
	exportID, err := p.DB.CreateMigrateExport(ctx, selected, est.TotalBytes())
	if err != nil {
		if WantsJSON(c) {
			return c.Status(500).JSON(fiber.Map{"ok": false, "error": "Could not start export."})
		}
		SetFlashError(c, "Could not start export.")
		return c.Redirect("/migrate")
	}
	go p.runMigrateExport(exportID, selected)
	if WantsJSON(c) {
		return c.JSON(fiber.Map{"ok": true, "export_id": exportID, "estimated_bytes": est.TotalBytes(), "estimated_human": migrate.FormatBytes(est.TotalBytes())})
	}
	SetFlash(c, "Export started. Track progress below.")
	return c.Redirect("/migrate?export=" + strconv.FormatInt(exportID, 10))
}

func (p *Panel) MigrateExportStatus(c *fiber.Ctx) error {
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil || id <= 0 {
		return c.Status(400).JSON(fiber.Map{"error": "invalid id"})
	}
	row, err := p.DB.GetMigrateExport(c.UserContext(), id)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "not found"})
	}
	resp := fiber.Map{
		"id":              row.ID,
		"status":          row.Status,
		"progress_log":    row.ProgressLog,
		"error":           row.Error,
		"size_bytes":      row.SizeBytes,
		"size_human":      migrate.FormatBytes(row.SizeBytes),
		"estimated_bytes": row.EstimatedBytes,
		"estimated_human": migrate.FormatBytes(row.EstimatedBytes),
		"created_at":      row.CreatedAt.UTC().Format(time.RFC3339),
	}
	if row.Status == db.MigrateExportReady || row.Status == db.MigrateExportDownloaded {
		if tok, ok := p.migrateTokens.Load(id); ok {
			if plain, ok2 := tok.(string); ok2 && plain != "" {
				url := migrate.DownloadURL(p.migratePublicBase(c), plain)
				resp["download_url"] = url
				resp["migrate_cmd"] = migrateShellCommand(url)
			}
		}
	}
	return c.JSON(resp)
}

func (p *Panel) MigrateDownload(c *fiber.Ctx) error {
	token := strings.TrimSpace(c.Params("token"))
	if token == "" {
		return c.Status(404).SendString("not found")
	}
	row, err := p.DB.GetMigrateExportByTokenHash(context.Background(), migrate.HashToken(token))
	if err != nil {
		return c.Status(404).SendString("not found")
	}
	if row.Status != db.MigrateExportReady && row.Status != db.MigrateExportDownloaded {
		return c.Status(410).SendString("link expired or not ready")
	}
	if time.Now().UTC().After(row.ExpiresAt) {
		return c.Status(410).SendString("link expired")
	}
	if row.BundlePath == "" {
		return c.Status(404).SendString("bundle missing")
	}
	if _, err := os.Stat(row.BundlePath); err != nil {
		return c.Status(404).SendString("bundle missing")
	}
	if row.Status == db.MigrateExportReady {
		_ = p.DB.MarkMigrateExportDownloaded(context.Background(), row.ID)
	}
	p.migrateTokens.Delete(row.ID)
	return c.Download(row.BundlePath, filepath.Base(row.BundlePath))
}

func (p *Panel) runMigrateExport(exportID int64, appIDs []string) {
	_ = p.DB.AppendMigrateExportLog(context.Background(), exportID, "export job started")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Hour)
	defer cancel()
	plain, err := migrate.RunExport(ctx, exportID, appIDs, migrate.ExportDeps{
		DB: p.DB,
		WorkspaceRoot: func(appID string) string {
			return p.ComposeWorkspaceRoot(context.Background(), appID)
		},
		VolumeNames:    p.migrateVolumeNames,
		SourcePanelURL: p.migratePublicBase(nil),
		AppendLog: func(id int64, msg string) {
			_ = p.DB.AppendMigrateExportLog(context.Background(), id, msg)
		},
		QuiescePanel: p.migrateQuiescePanel,
	})
	if err != nil {
		_ = p.DB.FailMigrateExport(context.Background(), exportID, err.Error())
		_ = p.DB.AppendMigrateExportLog(context.Background(), exportID, "failed: "+err.Error())
		migrate.CleanOrphans(p.DB)
		return
	}
	p.migrateSupersedeExports(exportID)
	p.migrateTokens.Store(exportID, plain)
}

func (p *Panel) migrateSupersedeExports(keepID int64) {
	ctx := context.Background()
	rows, err := p.DB.ListSupersededMigrateExports(ctx, keepID)
	if err != nil || len(rows) == 0 {
		return
	}
	for _, row := range rows {
		migrate.RemoveExportArtifacts(row)
		p.migrateTokens.Delete(row.ID)
		_ = p.DB.ExpireMigrateExport(ctx, row.ID)
	}
	_ = p.DB.AppendMigrateExportLog(ctx, keepID, fmt.Sprintf("replaced %d previous bundle(s)", len(rows)))
}

func (p *Panel) MigrateExportDelete(c *fiber.Ctx) error {
	ctx := c.UserContext()
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil || id <= 0 {
		return c.Status(400).JSON(fiber.Map{"error": "invalid id"})
	}
	row, err := p.DB.GetMigrateExport(ctx, id)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "not found"})
	}
	if row.Status == db.MigrateExportRunning {
		return c.Status(409).JSON(fiber.Map{"error": "export is still running"})
	}
	migrate.RemoveExportArtifacts(row)
	p.migrateTokens.Delete(id)
	if _, err := p.DB.DeleteMigrateExport(ctx, id); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "delete failed"})
	}
	migrate.CleanOrphans(p.DB)
	if WantsJSON(c) || c.Get("HX-Request") == "true" || c.Query("ajax") == "1" {
		return c.JSON(fiber.Map{"ok": true})
	}
	SetFlash(c, "Export deleted.")
	return c.Redirect("/migrate")
}

func (p *Panel) migrateVolumeNames(ctx context.Context, app db.App) ([]string, error) {
	allVolNames, listErr := volumex.List(ctx)
	if listErr != "" {
		return nil, fmt.Errorf("%s", listErr)
	}
	proj := append([]string{app.ID, strings.ReplaceAll(app.ID, "-", "_"), app.Name}, p.ComposeProjectCandidates(ctx, app, app.ID)...)
	vols, errMsg := volumex.ListForAppFromNames(ctx, p.AppVolumeQuery(ctx, app, p.AllPanelComposeProjects(ctx), proj...), allVolNames)
	if errMsg != "" {
		return nil, fmt.Errorf("%s", errMsg)
	}
	return vols, nil
}

func (p *Panel) migrateEstimateApps(ctx context.Context, apps []db.App) (migrate.SizeEstimate, error) {
	return migrate.EstimateApps(ctx, p.DB, apps, migrate.EstimateInput{
		WorkspaceBytes: p.AppStorageBytes,
		VolumeNames:    p.migrateVolumeNames,
	})
}

func (p *Panel) migrateAppsByIDs(ctx context.Context, ids []string) ([]db.App, error) {
	if len(ids) == 0 {
		return nil, fmt.Errorf("no apps selected")
	}
	var out []db.App
	for _, id := range ids {
		app, err := p.DB.GetApp(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("app %s not found", id)
		}
		out = append(out, app)
	}
	return out, nil
}

func parseAppIDList(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var ids []string
	if strings.HasPrefix(raw, "[") {
		_ = json.Unmarshal([]byte(raw), &ids)
	} else {
		for _, part := range strings.Split(raw, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				ids = append(ids, part)
			}
		}
	}
	seen := map[string]struct{}{}
	var out []string
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func (p *Panel) migratePublicBase(c *fiber.Ctx) string {
	ctx := context.Background()
	if c != nil {
		ctx = c.UserContext()
	}
	domain := strings.TrimSpace(p.DB.GetSetting(ctx, "panel_domain"))
	https := strings.EqualFold(p.DB.GetSetting(ctx, "panel_enable_https"), "true") || strings.EqualFold(p.DB.GetSetting(ctx, "panel_enable_https"), "1")
	if domain != "" {
		return migrate.PublicBaseURL(domain, https, "", 0)
	}
	host := "127.0.0.1"
	port := 8080
	if c != nil {
		raw := strings.TrimSpace(c.Hostname())
		if h, pStr, err := net.SplitHostPort(raw); err == nil {
			host = h
			if n, err := strconv.Atoi(pStr); err == nil && n > 0 {
				port = n
			}
		} else {
			host = raw
			if strings.EqualFold(c.Protocol(), "https") {
				port = 443
			} else {
				port = 80
			}
		}
	}
	return migrate.PublicBaseURL("", https, host, port)
}

func (p *Panel) MigrateImportDeps(adminID int64) migrate.ImportDeps {
	return migrate.ImportDeps{
		DB:           p.DB,
		Store:        p.Store,
		AdminOwnerID: adminID,
		WorkspaceRoot: func(appID string) string {
			return p.ComposeWorkspaceRoot(context.Background(), appID)
		},
		ComposePaths: func(app db.App) []string {
			return p.EffectiveComposePaths(context.Background(), app, app.ID)
		},
		ProjectName: func(app db.App) string {
			return p.ActiveComposeProjectName(context.Background(), app, app.ID)
		},
		EnvFiles: func(appID string) []string {
			return p.ComposeEnvFiles(context.Background(), appID)
		},
		AfterAppImported: func(ctx context.Context, app db.App) error {
			_ = p.WriteDockerRegistryConfig(ctx, app.ID, app.OwnerID)
			return p.SyncAppCaddyOverrideCtx(ctx, app.ID)
		},
		DeployAfterImport: true,
	}
}

type migrateImportTracker struct {
	mu        sync.Mutex
	Running   bool      `json:"running"`
	Status    string    `json:"status"` // "idle", "starting", "running", "completed", "failed"
	Error     string    `json:"error,omitempty"`
	StartedAt time.Time `json:"started_at,omitempty"`
	Logs      []string  `json:"logs"`
}

var globalMigrateImport = &migrateImportTracker{Logs: []string{}}

func (t *migrateImportTracker) addLog(msg string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.Logs = append(t.Logs, fmt.Sprintf("[%s] %s", time.Now().UTC().Format("15:04:05"), msg))
	if len(t.Logs) > 200 {
		t.Logs = t.Logs[len(t.Logs)-200:]
	}
}

// POST /migrate/import-upload
func (p *Panel) MigrateImportUpload(c *fiber.Ctx) error {
	globalMigrateImport.mu.Lock()
	if globalMigrateImport.Running {
		globalMigrateImport.mu.Unlock()
		return c.Status(409).JSON(fiber.Map{"ok": false, "error": "An import is already running."})
	}
	globalMigrateImport.Running = true
	globalMigrateImport.Status = "starting"
	globalMigrateImport.Error = ""
	globalMigrateImport.StartedAt = time.Now().UTC()
	globalMigrateImport.Logs = []string{fmt.Sprintf("[%s] Starting migration import job", time.Now().UTC().Format("15:04:05"))}
	globalMigrateImport.mu.Unlock()

	resetPanel := c.FormValue("reset_panel") == "true" || c.FormValue("reset_panel") == "1"

	fileHeader, err := c.FormFile("bundle")
	stagingDir := migrate.StagingRoot()
	_ = os.MkdirAll(stagingDir, 0700)
	stagingBundlePath := filepath.Join(stagingDir, fmt.Sprintf("web-import-%d%s", time.Now().UnixNano(), migrate.BundleExtension))

	if err == nil && fileHeader != nil {
		if ferr := c.SaveFile(fileHeader, stagingBundlePath); ferr != nil {
			globalMigrateImport.mu.Lock()
			globalMigrateImport.Running = false
			globalMigrateImport.Status = "failed"
			globalMigrateImport.Error = "Failed to save uploaded bundle: " + ferr.Error()
			globalMigrateImport.mu.Unlock()
			return c.Status(500).JSON(fiber.Map{"ok": false, "error": ferr.Error()})
		}
	} else {
		bundleURL := strings.TrimSpace(c.FormValue("bundle_url"))
		if bundleURL == "" {
			var body struct {
				BundleURL  string `json:"bundle_url"`
				ResetPanel bool   `json:"reset_panel"`
			}
			if bErr := c.BodyParser(&body); bErr == nil && body.BundleURL != "" {
				bundleURL = strings.TrimSpace(body.BundleURL)
				resetPanel = body.ResetPanel
			}
		}

		if bundleURL == "" {
			globalMigrateImport.mu.Lock()
			globalMigrateImport.Running = false
			globalMigrateImport.Status = "failed"
			globalMigrateImport.Error = "Provide an uploaded .nd-migrate file or a remote bundle URL"
			globalMigrateImport.mu.Unlock()
			return c.Status(400).JSON(fiber.Map{"ok": false, "error": "Provide an uploaded .nd-migrate file or a remote bundle URL"})
		}

		go func(url string, dest string, reset bool) {
			globalMigrateImport.addLog("Downloading bundle from " + url)
			resp, derr := http.Get(url)
			if derr != nil {
				globalMigrateImport.mu.Lock()
				globalMigrateImport.Running = false
				globalMigrateImport.Status = "failed"
				globalMigrateImport.Error = "Failed to download remote bundle: " + derr.Error()
				globalMigrateImport.mu.Unlock()
				return
			}
			defer resp.Body.Close()
			if resp.StatusCode != 200 {
				globalMigrateImport.mu.Lock()
				globalMigrateImport.Running = false
				globalMigrateImport.Status = "failed"
				globalMigrateImport.Error = fmt.Sprintf("Failed to download remote bundle: HTTP %d", resp.StatusCode)
				globalMigrateImport.mu.Unlock()
				return
			}
			outF, cerr := os.Create(dest)
			if cerr != nil {
				globalMigrateImport.mu.Lock()
				globalMigrateImport.Running = false
				globalMigrateImport.Status = "failed"
				globalMigrateImport.Error = "Failed to create local staging file: " + cerr.Error()
				globalMigrateImport.mu.Unlock()
				return
			}
			defer outF.Close()
			if _, cperr := io.Copy(outF, resp.Body); cperr != nil {
				globalMigrateImport.mu.Lock()
				globalMigrateImport.Running = false
				globalMigrateImport.Status = "failed"
				globalMigrateImport.Error = "Failed to write bundle file: " + cperr.Error()
				globalMigrateImport.mu.Unlock()
				return
			}
			p.runWebMigrateImport(dest, reset)
		}(bundleURL, stagingBundlePath, resetPanel)

		return c.JSON(fiber.Map{"ok": true, "message": "Downloading and importing remote bundle..."})
	}

	go p.runWebMigrateImport(stagingBundlePath, resetPanel)
	return c.JSON(fiber.Map{"ok": true, "message": "Import job started successfully"})
}

// GET /migrate/import/status
func (p *Panel) MigrateImportStatus(c *fiber.Ctx) error {
	globalMigrateImport.mu.Lock()
	defer globalMigrateImport.mu.Unlock()
	return c.JSON(fiber.Map{
		"running":    globalMigrateImport.Running,
		"status":     globalMigrateImport.Status,
		"error":      globalMigrateImport.Error,
		"started_at": globalMigrateImport.StartedAt,
		"logs":       globalMigrateImport.Logs,
	})
}

func (p *Panel) runWebMigrateImport(bundlePath string, resetPanel bool) {
	defer os.Remove(bundlePath)
	globalMigrateImport.addLog("Beginning bundle verification & extraction...")

	var adminID int64 = 1
	if users, err := p.DB.ListUsers(context.Background()); err == nil {
		for _, u := range users {
			if u.Role == db.RoleAdmin {
				adminID = u.ID
				break
			}
		}
	}

	deps := p.MigrateImportDeps(adminID)
	deps.ResetBeforeImport = resetPanel
	deps.OnProgress = func(msg string) {
		globalMigrateImport.addLog(msg)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()

	if err := migrate.RunImport(ctx, bundlePath, true, deps); err != nil {
		globalMigrateImport.mu.Lock()
		globalMigrateImport.Running = false
		globalMigrateImport.Status = "failed"
		globalMigrateImport.Error = err.Error()
		globalMigrateImport.mu.Unlock()
		globalMigrateImport.addLog("Import failed: " + err.Error())
		return
	}

	globalMigrateImport.mu.Lock()
	globalMigrateImport.Running = false
	globalMigrateImport.Status = "completed"
	globalMigrateImport.mu.Unlock()
	globalMigrateImport.addLog("Migration bundle imported and all apps restored successfully!")
}

func (p *Panel) migrateSweepLoop() {
	ticker := time.NewTicker(30 * time.Minute)
	defer ticker.Stop()
	for {
		migrate.SweepExpiredExports(p.DB)
		migrate.CleanOrphans(p.DB)
		<-ticker.C
	}
}

type migrateRunningStack struct {
	appID    string
	appName  string
	project  string
	dir      string
	paths    []string
	envFiles []string
}

func composeDownBenign(output string) bool {
	low := strings.ToLower(strings.TrimSpace(output))
	if low == "" {
		return true
	}
	return strings.Contains(low, "no resource found") ||
		strings.Contains(low, "warning") && strings.Contains(low, "no resource")
}

func (p *Panel) migrateQuiescePanel(ctx context.Context, logf func(string)) (func(context.Context), error) {
	apps, err := p.DB.ListApps(ctx)
	if err != nil {
		return nil, fmt.Errorf("list apps: %w", err)
	}
	if len(apps) == 0 {
		return nil, nil
	}

	logf(fmt.Sprintf("stopping %d app(s) across the panel for a consistent snapshot", len(apps)))

	var toRestart []migrateRunningStack
	rollback := func() {
		if len(toRestart) == 0 {
			return
		}
		rbCtx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()
		logf("rolling back: restarting apps stopped before failed quiesce")
		for _, stack := range toRestart {
			v, _ := p.ComposeMu.LoadOrStore(stack.appID, &sync.Mutex{})
			mu := v.(*sync.Mutex)
			mu.Lock()
			_ = dockerx.ComposeUp(rbCtx, stack.dir, stack.paths, stack.project, nil, stack.envFiles)
			mu.Unlock()
		}
	}

	for _, app := range apps {
		if err := ctx.Err(); err != nil {
			rollback()
			return nil, err
		}

		v, _ := p.ComposeMu.LoadOrStore(app.ID, &sync.Mutex{})
		mu := v.(*sync.Mutex)
		mu.Lock()

		dir := p.ComposeWorkspaceRoot(ctx, app.ID)
		paths := p.EffectiveComposePaths(ctx, app, app.ID)
		if len(paths) == 0 {
			mu.Unlock()
			continue
		}
		envFiles := p.ComposeEnvFiles(ctx, app.ID)
		project := p.composeProjectName(app, app.ID)
		if project == "" {
			mu.Unlock()
			continue
		}

		rows, res := dockerx.ComposePS(ctx, dir, paths, project, envFiles)
		wasRunning := res.OK && countComposeOkRunning(rows) > 0

		logf("stopping " + app.Name)
		p.StopOtherComposeStacks(ctx, app, app.ID, project)
		downRes := dockerx.ComposeDown(ctx, dir, paths, project, nil, envFiles)
		if !downRes.OK && !composeDownBenign(downRes.Output) {
			mu.Unlock()
			rollback()
			return nil, fmt.Errorf("stop %s: %s", app.Name, strings.TrimSpace(downRes.Output))
		}

		if wasRunning {
			toRestart = append(toRestart, migrateRunningStack{
				appID: app.ID, appName: app.Name, project: project,
				dir: dir, paths: paths, envFiles: envFiles,
			})
		}
		mu.Unlock()
	}

	if len(toRestart) == 0 {
		logf("panel quiesced (no running stacks to restore later)")
	} else {
		logf(fmt.Sprintf("panel quiesced (%d stack(s) will be restarted after export)", len(toRestart)))
	}

	return func(restoreCtx context.Context) {
		if len(toRestart) == 0 {
			return
		}
		restoreCtx, cancel := context.WithTimeout(restoreCtx, 30*time.Minute)
		defer cancel()
		logf(fmt.Sprintf("restarting %d app stack(s)", len(toRestart)))
		for _, stack := range toRestart {
			select {
			case <-restoreCtx.Done():
				logf("restore cancelled: " + restoreCtx.Err().Error())
				return
			default:
			}
			v, _ := p.ComposeMu.LoadOrStore(stack.appID, &sync.Mutex{})
			mu := v.(*sync.Mutex)
			mu.Lock()
			logf("starting " + stack.appName)
			upRes := dockerx.ComposeUp(restoreCtx, stack.dir, stack.paths, stack.project, nil, stack.envFiles)
			if !upRes.OK {
				logf("restart " + stack.appName + " failed: " + strings.TrimSpace(upRes.Output))
			}
			mu.Unlock()
		}
		logf("panel restore finished")
	}, nil
}
