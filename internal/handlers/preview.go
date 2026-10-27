package handlers

import (
	"context"
	"encoding/json"
	"strings"

	"panel/internal/caddy"
	"panel/internal/db"

	"github.com/gofiber/fiber/v2"
)

// PreviewConfig is the per-app preview URL setting. An empty Provider means preview is disabled.
type PreviewConfig struct {
	Provider string `json:"provider"` // "sslip.io", "nip.io" or "custom"
	Base     string `json:"base"`     // custom base domain, used when Provider is "custom"
	Service  string `json:"service"`
	Port     int    `json:"port"`
}

func previewSettingKey(appID string) string { return "preview:" + appID }

func (p *Panel) previewConfig(ctx context.Context, appID string) PreviewConfig {
	var cfg PreviewConfig
	_ = json.Unmarshal([]byte(p.DB.GetSetting(ctx, previewSettingKey(appID))), &cfg)
	return cfg
}

// previewHostBase returns the slot-independent preview host; the slot prefix is applied later.
func previewHostBase(app db.App, cfg PreviewConfig, publicIP string) string {
	switch cfg.Provider {
	case "sslip.io", "nip.io":
		return caddy.FormatWildcardDomain(app.Name, publicIP, cfg.Provider)
	case "custom":
		return strings.ToLower(strings.TrimSpace(cfg.Base))
	}
	return ""
}

// previewSite builds the explicit preview route for a slot, or nil when preview is disabled.
func (p *Panel) previewSite(ctx context.Context, app db.App, slot string) *db.AppDomain {
	cfg := p.previewConfig(ctx, app.ID)
	targetIP, targetShieldID := p.ResolveAppPublicIP(ctx, app)
	base := previewHostBase(app, cfg, targetIP)
	if base == "" || cfg.Service == "" {
		return nil
	}
	port := cfg.Port
	if port <= 0 {
		port = 3000
	}
	return &db.AppDomain{
		Domain:       caddy.SlotPreviewDomain(base, slot),
		Service:      cfg.Service,
		Port:         port,
		EnableHTTPS:  true,
		ShieldNodeID: targetShieldID,
	}
}

// POST /apps/:id/preview — save preview URL settings
func (p *Panel) AppPreviewSave(c *fiber.Ctx) error {
	id := c.Params("id")
	var cfg PreviewConfig
	if err := c.BodyParser(&cfg); err != nil {
		return c.Status(400).JSON(fiber.Map{"ok": false, "error": "invalid body"})
	}
	cfg.Provider = strings.TrimSpace(cfg.Provider)
	cfg.Base = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(cfg.Base, "https://"), "http://")))
	cfg.Service = caddy.CleanQuotedValue(cfg.Service)
	switch cfg.Provider {
	case "", "sslip.io", "nip.io":
	case "custom":
		if cfg.Base == "" {
			return c.Status(400).JSON(fiber.Map{"ok": false, "error": "custom preview domain required"})
		}
	default:
		return c.Status(400).JSON(fiber.Map{"ok": false, "error": "invalid provider"})
	}
	if cfg.Provider != "" && cfg.Service == "" {
		return c.Status(400).JSON(fiber.Map{"ok": false, "error": "service required"})
	}
	raw, _ := json.Marshal(cfg)
	if err := p.DB.SetSetting(c.UserContext(), previewSettingKey(id), string(raw)); err != nil {
		return c.Status(500).JSON(fiber.Map{"ok": false, "error": err.Error()})
	}
	if err := p.SyncAndApplyBackground(c.UserContext(), id); err != nil {
		return c.Status(500).JSON(fiber.Map{"ok": false, "error": err.Error()})
	}
	app, _ := p.DB.GetApp(c.UserContext(), id)
	targetIP, _ := p.ResolveAppPublicIP(c.UserContext(), app)
	return c.JSON(fiber.Map{
		"ok":        true,
		"preview":   cfg,
		"host_base": previewHostBase(app, cfg, targetIP),
	})
}
