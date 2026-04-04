package handlers

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"panel/internal/caddy"
	"panel/internal/db"
	"panel/internal/dockerx"

	"github.com/gofiber/fiber/v2"
)

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
	return c.Render("pages/settings", withUser(c, fiber.Map{
		"Nav":              "settings",
		"Title":            "Settings",
		"Flash":            c.Query("flash"),
		"CleanupEnabled":   settingBool(cfg[settingCleanupEnabled], true),
		"CleanupInterval":  normalizeCleanupInterval(cfg[settingCleanupInterval]),
		"CleanupIntervals": cleanupIntervalOptions(),
		"CleanupLastRun":   cfg[settingCleanupLastRun],
		"CleanupLastLog":   cfg[settingCleanupLastLog],
	}), "layouts/shell")
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
	status := "[ok]\n" + res.Output
	if !res.OK {
		status = "[error]\n" + res.Output
	}
	_ = p.DB.SetSetting(ctx, settingCleanupLastLog, status)
}

func (p *Panel) StartBackgroundJobs() {
	go p.cleanupLoop()
	go p.sessionPruneLoop()
}

func (p *Panel) sessionPruneLoop() {
	ticker := time.NewTicker(6 * time.Hour)
	defer ticker.Stop()
	for {
		_ = p.DB.PruneExpiredSessions(context.Background())
		<-ticker.C
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
	status := "[ok]\n" + res.Output
	if !res.OK {
		status = "[error]\n" + res.Output
	}
	_ = p.DB.SetSetting(ctx, settingCleanupLastLog, status)
}

func nextDeployPanelDomain(cfg map[string]string) db.AppDomain {
	return db.AppDomain{
		Domain:      strings.TrimSpace(cfg[settingPanelDomain]),
		Port:        8080,
		EnableHTTPS: true,
		EnableWWW:   settingBool(cfg[settingPanelEnableWWW], false),
	}
}
