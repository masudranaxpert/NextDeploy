package handlers

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"panel/internal/caddy"
	"panel/internal/db"
	"panel/internal/dockerx"
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

func settingBool(v string, def bool) bool {
	v = strings.TrimSpace(strings.ToLower(v))
	if v == "" {
		return def
	}
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

// panelStackComposeContainerPath is the bind-mount path inside the panel container (docker-compose.yml).
const panelStackComposeContainerPath = "/stack/docker-compose.yml"

func (p *Panel) nextDeployComposePath() string {
	if custom := strings.TrimSpace(os.Getenv("PANEL_STACK_COMPOSE_FILE")); custom != "" {
		if _, err := os.Stat(custom); err == nil {
			return custom
		}
		// Legacy/broken installs sometimes set PANEL_STACK_COMPOSE_FILE to /docker-compose.yml or a missing path
		// while the real file is bind-mounted at /stack/docker-compose.yml.
		if custom != panelStackComposeContainerPath {
			if _, err := os.Stat(panelStackComposeContainerPath); err == nil {
				return panelStackComposeContainerPath
			}
		}
		return custom
	}
	wd, err := os.Getwd()
	if err != nil {
		return "docker-compose.yml"
	}
	local := filepath.Join(wd, "docker-compose.yml")
	if _, err := os.Stat(local); err == nil {
		return local
	}
	for d := wd; ; {
		parent := filepath.Dir(d)
		if parent == d {
			break
		}
		d = parent
		candidate := filepath.Join(d, "docker-compose.yml")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return local
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

// rootStackComposeProjectName is the docker compose -p project name for the root NextDeploy stack.
func rootStackComposeProjectName(projectDir string) string {
	if v := strings.TrimSpace(os.Getenv("PANEL_STACK_COMPOSE_PROJECT")); v != "" {
		return v
	}
	base := filepath.Base(filepath.Clean(projectDir))
	if strings.EqualFold(base, "stack") {
		// Panel mounts host compose at /stack/docker-compose.yml; host dir is usually .../nextdeploy
		return "nextdeploy"
	}
	return base
}

// shouldUseComposeHelperContainer is true when the panel process runs inside Docker with compose at /stack/...
// Applying compose from inside the same "panel" container can kill that process mid-command and leave the stack broken.
func shouldUseComposeHelperContainer(composeFile string) bool {
	cf := filepath.ToSlash(filepath.Clean(composeFile))
	return cf == "/stack/docker-compose.yml" || strings.HasSuffix(cf, "/stack/docker-compose.yml")
}

// runRootStackComposeViaHelperContainer runs compose in a one-off container (docker:cli) so the panel container
// is not the client process that triggers its own recreate.
func runRootStackComposeViaHelperContainer(ctx context.Context, hostInstallDir, projectName string) error {
	hostInstallDir = filepath.Clean(hostInstallDir)
	img := strings.TrimSpace(os.Getenv("PANEL_COMPOSE_HELPER_IMAGE"))
	if img == "" {
		img = "docker:cli"
	}
	name := fmt.Sprintf("nextdeploy-compose-apply-%d", time.Now().UnixNano())
	// No -d: wait for the helper to exit so we capture success/failure and compose logs on stderr.
	args := []string{
		"run", "--rm", "--name", name,
		"-v", "/var/run/docker.sock:/var/run/docker.sock",
		"-v", hostInstallDir + ":/work",
		"-w", "/work",
		img,
		"compose", "--project-directory", "/work", "-p", projectName,
		"-f", "docker-compose.yml",
		"up", "-d", "panel",
	}
	out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		return fmt.Errorf("%w: %s", err, text)
	}
	if text != "" {
		log.Printf("root stack compose helper: %s", text)
	}
	return nil
}

// applyRootStackPanelBackground runs `docker compose up -d panel` so Caddy picks up new labels (caddy-docker-proxy).
func (p *Panel) applyRootStackPanelBackground(composeFile string) {
	composeFile = filepath.Clean(composeFile)
	projectDir := filepath.Dir(composeFile)
	project := rootStackComposeProjectName(projectDir)
	hostDir := strings.TrimSpace(os.Getenv("PANEL_HOST_INSTALL_DIR"))
	if hostDir == "" {
		hostDir = "/opt/nextdeploy"
		if shouldUseComposeHelperContainer(composeFile) {
			log.Printf("root stack compose helper: PANEL_HOST_INSTALL_DIR unset, using default %s (set in compose for custom --dir installs)", hostDir)
		}
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		if shouldUseComposeHelperContainer(composeFile) {
			if err := runRootStackComposeViaHelperContainer(ctx, hostDir, project); err != nil {
				log.Printf("root stack compose helper: %v", err)
			}
			return
		}
		if res := dockerx.ComposeApplyServices(ctx, projectDir, []string{composeFile}, project, nil, nil, "panel"); !res.OK {
			log.Printf("root stack compose apply panel service: %s", strings.TrimSpace(res.Output))
		}
	}()
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
	if err := p.DB.SetSetting(ctx, settingCleanupEnabled, boolString(enabled)); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	if err := p.DB.SetSetting(ctx, settingCleanupInterval, interval); err != nil {
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
	p.applyRootStackPanelBackground(p.nextDeployComposePath())
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
	go p.cleanOrphanTempFiles()
}

func (p *Panel) prePullAlpineImage() {
	ctx := context.Background()
	cmd := exec.CommandContext(ctx, "docker", "pull", "alpine:3.20")
	_ = cmd.Run()
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
