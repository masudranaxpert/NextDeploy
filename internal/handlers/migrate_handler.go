package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"panel/internal/db"
	"panel/internal/handlers/utils"
	"panel/internal/migrate"
	"panel/internal/volumex"

	"github.com/gofiber/fiber/v2"
)

func (p *Panel) MigratePage(c *fiber.Ctx) error {
	ctx := c.UserContext()
	apps, _ := p.DB.ListApps(ctx)
	recent, _ := p.DB.ListRecentMigrateExports(ctx, 8)
	est, _ := p.migrateEstimateApps(ctx, apps)
	exportRunning, _ := p.DB.HasRunningMigrateExport(ctx)
	runningExportID := int64(0)
	if exportRunning {
		if row, err := p.DB.GetRunningMigrateExport(ctx); err == nil {
			runningExportID = row.ID
		}
	}
	focusID := migrateFocusExportID(c, recent, runningExportID)
	focus := fiber.Map{}
	if focusID > 0 {
		if row, err := p.DB.GetMigrateExport(ctx, focusID); err == nil {
			focus = migrateExportView(p, c, row)
		}
	}
	return c.Render("pages/migrate", WithUser(c, fiber.Map{
		"Nav":              "migrate",
		"Title":            "Panel Migration",
		"Apps":             apps,
		"RecentExports":    recent,
		"EstimateTotal":    migrate.FormatBytes(est.TotalBytes()),
		"EstimateApps":     est.AppCount,
		"EstimateVols":     est.VolumeCount,
		"ExportRunning":    exportRunning,
		"RunningExportID":  runningExportID,
		"FocusExportID":    focusID,
		"FocusExport":      focus,
		"Flash":            utils.ReadFlash(c),
		"FlashError":       utils.ReadFlashError(c),
	}), "layouts/shell")
}

func migrateFocusExportID(c *fiber.Ctx, recent []db.MigrateExport, runningID int64) int64 {
	if q := strings.TrimSpace(c.Query("export")); q != "" {
		if id, err := strconv.ParseInt(q, 10, 64); err == nil && id > 0 {
			return id
		}
	}
	if runningID > 0 {
		return runningID
	}
	for _, row := range recent {
		if row.Status == db.MigrateExportFailed {
			return row.ID
		}
	}
	for _, row := range recent {
		if row.Status == db.MigrateExportReady || row.Status == db.MigrateExportDownloaded {
			return row.ID
		}
	}
	return 0
}

func migrateExportView(p *Panel, c *fiber.Ctx, row db.MigrateExport) fiber.Map {
	view := fiber.Map{
		"ID":              row.ID,
		"Status":          row.Status,
		"ProgressLog":     row.ProgressLog,
		"Error":           row.Error,
		"SizeHuman":       migrate.FormatBytes(row.SizeBytes),
		"EstimatedHuman":  migrate.FormatBytes(row.EstimatedBytes),
		"CreatedAt":       row.CreatedAt.UTC().Format(time.RFC3339),
		"ShowProgress":    true,
	}
	if row.Status == db.MigrateExportReady || row.Status == db.MigrateExportDownloaded {
		if tok, ok := p.migrateTokens.Load(row.ID); ok {
			if plain, ok2 := tok.(string); ok2 && plain != "" {
				url := migrate.DownloadURL(p.migratePublicBase(c), plain)
				view["DownloadURL"] = url
				view["MigrateCmd"] = migrateShellCommand(url)
			}
		}
	}
	return view
}

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
		utils.SetFlashError(c, "Select at least one app to export.")
		return c.Redirect("/migrate")
	}
	apps, err := p.migrateAppsByIDs(ctx, selected)
	if err != nil {
		utils.SetFlashError(c, err.Error())
		return c.Redirect("/migrate")
	}
	est, _ := p.migrateEstimateApps(ctx, apps)
	running, err := p.DB.HasRunningMigrateExport(ctx)
	if err != nil {
		utils.SetFlashError(c, "Could not check export status.")
		return c.Redirect("/migrate")
	}
	if running {
		utils.SetFlashError(c, "An export is already running. Wait for it to finish or delete it first.")
		return c.Redirect("/migrate")
	}
	exportID, err := p.DB.CreateMigrateExport(ctx, selected, est.TotalBytes())
	if err != nil {
		utils.SetFlashError(c, "Could not start export.")
		return c.Redirect("/migrate")
	}
	go p.runMigrateExport(exportID, selected)
	utils.SetFlash(c, "Export started. Track progress below.")
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
		VolumeNames: p.migrateVolumeNames,
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
	if c.Get("HX-Request") == "true" || c.Query("ajax") == "1" {
		return c.JSON(fiber.Map{"ok": true})
	}
	utils.SetFlash(c, "Export deleted.")
	return c.Redirect("/migrate")
}

func (p *Panel) migrateVolumeNames(ctx context.Context, app db.App) ([]string, error) {
	allVolNames, listErr := volumex.List(ctx)
	if listErr != "" {
		return nil, fmt.Errorf("%s", listErr)
	}
	proj := append([]string{app.ID, strings.ReplaceAll(app.ID, "-", "_"), app.Name}, p.ComposeProjectCandidates(ctx, app, app.ID)...)
	vols, errMsg := volumex.ListForAppFromNames(ctx, app.ID, allVolNames, proj)
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
		DB:            p.DB,
		Store:         p.Store,
		AdminOwnerID:  adminID,
		WorkspaceRoot: func(appID string) string {
			return p.ComposeWorkspaceRoot(context.Background(), appID)
		},
		ComposeFilePath: func(app db.App) string {
			return p.ComposeFilePath(context.Background(), app, app.ID)
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

func (p *Panel) migrateSweepLoop() {
	ticker := time.NewTicker(30 * time.Minute)
	defer ticker.Stop()
	for {
		migrate.SweepExpiredExports(p.DB)
		migrate.CleanOrphans(p.DB)
		<-ticker.C
	}
}
