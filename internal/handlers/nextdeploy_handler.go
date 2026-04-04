package handlers

import (
	"os"
	"strings"

	"panel/internal/caddy"
	"panel/internal/dockerapi"

	"github.com/gofiber/fiber/v2"
)

func (p *Panel) NextDeployPage(c *fiber.Ctx) error {
	ctx := c.UserContext()
	cfg, _ := p.DB.GetAllSettings(ctx)
	panelSite := nextDeployPanelDomain(cfg)
	labels := caddy.GenerateLabels(panelSite)
	labelYAML := caddy.LabelsToYAML(labels)
	composePath := p.nextDeployComposePath()
	composePreview := ""
	if base, err := os.ReadFile(composePath); err == nil {
		merged, err := caddy.GenerateRootStackCompose(base, panelSite.Domain, panelSite.EnableWWW, p.DB.GetCaddyConfig(ctx, "email"), p.DB.GetCaddyConfig(ctx, "caddy_image"))
		if err == nil {
			composePreview = string(merged)
		}
	}
	caddyCfg, _ := p.DB.GetAllCaddyConfig(ctx)
	adminAPI := strings.TrimSpace(caddyCfg["admin_api"])
	if adminAPI == "" {
		adminAPI = "http://caddy:2019"
	}
	up, statusMsg := caddy.AdminStatus(ctx, adminAPI)
	liveConfig := ""
	if up {
		liveConfig, _ = caddy.AdminConfigGet(ctx, adminAPI)
	}
	containers, _ := dockerapi.ListContainers(ctx)
	caddyRunning := false
	for _, ct := range containers {
		if strings.EqualFold(ct.Name, caddy.CaddyContainerName) && ct.State == "running" {
			caddyRunning = true
			break
		}
	}
	return c.Render("pages/nextdeploy", withUser(c, fiber.Map{
		"Nav":             "nextdeploy",
		"Title":           "NextDeploy",
		"AdminAPI":        adminAPI,
		"CaddyUp":         up,
		"StatusMsg":       statusMsg,
		"LiveConfig":      liveConfig,
		"CaddyRunning":    caddyRunning,
		"Email":           strings.TrimSpace(caddyCfg["email"]),
		"CaddyImage":      strings.TrimSpace(caddyCfg["caddy_image"]),
		"CaddyNetwork":    caddy.CaddyNetwork,
		"PanelDomain":     panelSite.Domain,
		"PanelEnableWWW":  panelSite.EnableWWW,
		"PanelLabelsYAML": strings.TrimSpace(labelYAML),
		"RootComposePath": composePath,
		"RootCompose":     composePreview,
	}), "layouts/shell")
}
