package handlers

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"panel/internal/caddy"
	"panel/internal/db"
	"panel/internal/dockerx"
	"panel/internal/phppanel"
	"panel/internal/runutil"

	"github.com/gofiber/fiber/v2"
)

// panelTempPatterns are the glob patterns NextDeploy uses for temp files in os.TempDir().
var panelTempPatterns = []string{
	"vol-backup-*.tar.gz",
	"panel-upload-*.zip",
	".nextdeploy-atomic-*",
}

// ScanPanelTempFiles returns count and total size of orphaned NextDeploy temp files.
func ScanPanelTempFiles() (count int, totalBytes int64, paths []string) {
	dir := os.TempDir()
	for _, pattern := range panelTempPatterns {
		matches, err := filepath.Glob(filepath.Join(dir, pattern))
		if err != nil {
			continue
		}
		for _, m := range matches {
			st, err := os.Stat(m)
			if err != nil {
				continue
			}
			count++
			totalBytes += st.Size()
			paths = append(paths, m)
		}
	}
	return
}

// CleanPanelTempFiles removes all orphaned NextDeploy temp files and returns
// the number removed and the freed bytes.
func CleanPanelTempFiles() (removed int, freedBytes int64) {
	_, _, paths := ScanPanelTempFiles()
	for _, p := range paths {
		st, err := os.Stat(p)
		if err != nil {
			continue
		}
		size := st.Size()
		if err := os.Remove(p); err == nil {
			removed++
			freedBytes += size
		}
	}
	return
}

// formatBytes returns a human-readable byte size string.
func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

const (
	settingCleanupEnabled  = "cleanup_enabled"
	settingCleanupInterval = "cleanup_interval"
	settingCleanupLastRun  = "cleanup_last_run"
	settingCleanupLastLog  = "cleanup_last_log"
	settingPanelDomain     = "panel_domain"
	settingPanelEnableWWW  = "panel_enable_www"
	settingUploadMaxMB     = "upload_max_mb"
	defaultUploadMaxMB     = 250
	maxUploadMaxMB         = 2048
)

type intervalOption struct {
	Value string
	Label string
}

func cleanupIntervalOptions() []intervalOption {
	return []intervalOption{
		{Value: "1h", Label: "Every 1 hour"},
		{Value: "6h", Label: "Every 6 hours"},
		{Value: "12h", Label: "Every 12 hours"},
		{Value: "24h", Label: "Every day"},
		{Value: "168h", Label: "Every week"},
	}
}

func normalizeCleanupInterval(v string) string {
	switch strings.TrimSpace(v) {
	case "1h", "6h", "12h", "24h", "168h":
		return strings.TrimSpace(v)
	default:
		return "1h"
	}
}

func parseCleanupInterval(v string) time.Duration {
	d, err := time.ParseDuration(normalizeCleanupInterval(v))
	if err != nil || d <= 0 {
		return time.Hour
	}
	return d
}

func normalizeUploadMaxMB(v string) int {
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n <= 0 {
		return defaultUploadMaxMB
	}
	if n > maxUploadMaxMB {
		return maxUploadMaxMB
	}
	return n
}

func (p *Panel) uploadMaxMB(ctx context.Context) int {
	return normalizeUploadMaxMB(p.DB.GetSetting(ctx, settingUploadMaxMB))
}

func (p *Panel) uploadMaxBytes(ctx context.Context) int64 {
	return int64(p.uploadMaxMB(ctx)) * 1024 * 1024
}

func settingBool(v string, def bool) bool {
	v = strings.TrimSpace(strings.ToLower(v))
	if v == "" {
		return def
	}
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

func (p *Panel) nextDeployComposePath() string {
	if custom := strings.TrimSpace(os.Getenv("PANEL_STACK_COMPOSE_FILE")); custom != "" {
		return custom
	}
	return filepath.Join(".", "docker-compose.yml")
}

func (p *Panel) syncRootStackCompose(ctx context.Context) error {
	path := p.nextDeployComposePath()
	base, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	panelDomain := p.DB.GetSetting(ctx, settingPanelDomain)
	enableWWW := settingBool(p.DB.GetSetting(ctx, settingPanelEnableWWW), false)
	email := p.DB.GetCaddyConfig(ctx, "email")
	caddyImage := p.DB.GetCaddyConfig(ctx, "caddy_image")
	merged, err := caddy.GenerateRootStackCompose(base, panelDomain, enableWWW, email, caddyImage)
	if err != nil {
		return err
	}
	return os.WriteFile(path, merged, 0640)
}

func (p *Panel) SyncRootStackComposeOnStart() error {
	return p.syncRootStackCompose(context.Background())
}

func (p *Panel) SettingsPage(c *fiber.Ctx) error {
	cfg, _ := p.DB.GetAllSettings(c.UserContext())
	tmpCount, tmpBytes, _ := ScanPanelTempFiles()
	return c.Render("pages/settings", withUser(c, fiber.Map{
		"Nav":              "settings",
		"Title":            "Settings",
		"Flash":            c.Query("flash"),
		"CleanupEnabled":   settingBool(cfg[settingCleanupEnabled], true),
		"CleanupInterval":  normalizeCleanupInterval(cfg[settingCleanupInterval]),
		"CleanupIntervals": cleanupIntervalOptions(),
		"CleanupLastRun":   cfg[settingCleanupLastRun],
		"CleanupLastLog":   cfg[settingCleanupLastLog],
		"UploadMaxMB":      normalizeUploadMaxMB(cfg[settingUploadMaxMB]),
		"UploadMaxMBMax":   maxUploadMaxMB,
		"TmpFileCount":     tmpCount,
		"TmpFileSize":      formatBytes(tmpBytes),
		"TmpFileSizeRaw":   tmpBytes,
	}), "layouts/shell")
}

// TempCleanupRun deletes orphaned NextDeploy temp files immediately.
func (p *Panel) TempCleanupRun(c *fiber.Ctx) error {
	removed, freed := CleanPanelTempFiles()
	msg := fmt.Sprintf("Removed %d temp file(s), freed %s.", removed, formatBytes(freed))
	_ = p.DB.SetSetting(c.UserContext(), "tmp_cleanup_last_run", time.Now().UTC().Format(time.RFC3339))
	_ = p.DB.SetSetting(c.UserContext(), "tmp_cleanup_last_log", msg)
	return c.Redirect("/settings?flash=tmp_cleaned")
}

// TempCleanupInfo returns live temp file info as JSON (for AJAX refresh).
func (p *Panel) TempCleanupInfo(c *fiber.Ctx) error {
	count, totalBytes, _ := ScanPanelTempFiles()
	return c.JSON(fiber.Map{
		"count": count,
		"size":  formatBytes(totalBytes),
		"bytes": totalBytes,
	})
}

func (p *Panel) SettingsSave(c *fiber.Ctx) error {
	ctx := c.UserContext()
	enabled := c.FormValue(settingCleanupEnabled) == "on"
	interval := normalizeCleanupInterval(c.FormValue(settingCleanupInterval))
	uploadMaxMB := normalizeUploadMaxMB(c.FormValue(settingUploadMaxMB))
	if err := p.DB.SetSetting(ctx, settingCleanupEnabled, boolString(enabled)); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	if err := p.DB.SetSetting(ctx, settingCleanupInterval, interval); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	if err := p.DB.SetSetting(ctx, settingUploadMaxMB, strconv.Itoa(uploadMaxMB)); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	if p.DB.GetSetting(ctx, settingCleanupLastRun) == "" {
		_ = p.DB.SetSetting(ctx, settingCleanupLastRun, time.Now().UTC().Format(time.RFC3339))
	}
	return c.Redirect("/settings")
}

func (p *Panel) SaveNextDeployPanelConfig(c *fiber.Ctx) error {
	ctx := c.UserContext()
	panelDomain := strings.TrimSpace(c.FormValue(settingPanelDomain))
	enableWWW := c.FormValue(settingPanelEnableWWW) == "on"
	if err := p.DB.SetSetting(ctx, settingPanelDomain, panelDomain); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	if err := p.DB.SetSetting(ctx, settingPanelEnableWWW, boolString(enableWWW)); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	if err := p.syncRootStackCompose(ctx); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	return c.Redirect("/nextdeploy")
}

func boolString(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

// ManualCleanupRun triggers an immediate Docker cleanup regardless of schedule.
func (p *Panel) ManualCleanupRun(c *fiber.Ctx) error {
	go p.runScheduledCleanupForce()
	return c.Redirect("/settings?flash=cleanup_started")
}

func (p *Panel) runScheduledCleanupForce() {
	ctx := context.Background()
	runCtx, cancel := context.WithTimeout(ctx, 20*time.Minute)
	defer cancel()
	res := dockerx.DockerPruneUnused(runCtx)
	now := time.Now().UTC()
	_ = p.DB.SetSetting(ctx, settingCleanupLastRun, now.Format(time.RFC3339))
	_ = p.DB.SetSetting(ctx, settingCleanupLastLog, runutil.StatusText(runutil.Result{OK: res.OK, Output: res.Output}))
}

func (p *Panel) StartBackgroundJobs() {
	go p.prePullAlpineImage()
	go p.cleanupLoop()
	go p.sessionPruneLoop()
	go p.migratePHPPanelComposeFiles()
	go p.cleanOrphanTempFiles()
}

func (p *Panel) prePullAlpineImage() {
	ctx := context.Background()
	cmd := exec.CommandContext(ctx, "docker", "pull", "alpine:3.20")
	_ = cmd.Run()
}

// migratePHPPanelComposeFiles updates any existing PHP Panel compose.yml files
// that still use legacy FPM images or obsolete command overrides to serversideup/php
// FPM alpine images (extensions pre-installed; listen suitable for Caddy on Docker network).
func (p *Panel) migratePHPPanelComposeFiles() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	apps, err := p.DB.ListApps(ctx)
	if err != nil {
		return
	}
	for _, app := range apps {
		if app.TemplateID != phppanel.TemplateID {
			continue
		}
		workspaceRoot := p.Store.Path(app.ID)
		if err := phppanel.MigrateCompose(workspaceRoot); err != nil {
			log.Printf("php panel compose migrate %s: %v", app.ID, err)
		}
	}
}

// cleanOrphanTempFiles removes any leftover NextDeploy temp files from a previous
// run (e.g. after a panel crash mid-download). Runs once at startup.
func (p *Panel) cleanOrphanTempFiles() {
	removed, freed := CleanPanelTempFiles()
	if removed > 0 {
		msg := fmt.Sprintf("[startup] Removed %d orphaned temp file(s), freed %s.", removed, formatBytes(freed))
		_ = p.DB.SetSetting(context.Background(), "tmp_cleanup_last_run", time.Now().UTC().Format(time.RFC3339))
		_ = p.DB.SetSetting(context.Background(), "tmp_cleanup_last_log", msg)
	}
}

func (p *Panel) sessionPruneLoop() {
	ticker := time.NewTicker(6 * time.Hour)
	defer ticker.Stop()
	for {
		<-ticker.C
		_ = p.DB.PruneExpiredSessions(context.Background())
	}
}

func (p *Panel) cleanupLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	for {
		p.runScheduledCleanup()
		<-ticker.C
	}
}

func (p *Panel) runScheduledCleanup() {
	ctx := context.Background()
	cfg, err := p.DB.GetAllSettings(ctx)
	if err != nil {
		return
	}
	if !settingBool(cfg[settingCleanupEnabled], true) {
		return
	}
	interval := parseCleanupInterval(cfg[settingCleanupInterval])
	lastRaw := strings.TrimSpace(cfg[settingCleanupLastRun])
	if lastRaw == "" {
		_ = p.DB.SetSetting(ctx, settingCleanupLastRun, time.Now().UTC().Format(time.RFC3339))
		return
	}
	lastRun, err := time.Parse(time.RFC3339, lastRaw)
	if err != nil {
		_ = p.DB.SetSetting(ctx, settingCleanupLastRun, time.Now().UTC().Format(time.RFC3339))
		return
	}
	if time.Since(lastRun) < interval {
		return
	}
	runCtx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	res := dockerx.DockerPruneUnused(runCtx)
	now := time.Now().UTC()
	_ = p.DB.SetSetting(ctx, settingCleanupLastRun, now.Format(time.RFC3339))
	_ = p.DB.SetSetting(ctx, settingCleanupLastLog, runutil.StatusText(runutil.Result{OK: res.OK, Output: res.Output}))
}

func nextDeployPanelDomain(cfg map[string]string) db.AppDomain {
	return db.AppDomain{
		Domain:      strings.TrimSpace(cfg[settingPanelDomain]),
		Port:        8080,
		EnableHTTPS: true,
		EnableWWW:   settingBool(cfg[settingPanelEnableWWW], false),
	}
}
