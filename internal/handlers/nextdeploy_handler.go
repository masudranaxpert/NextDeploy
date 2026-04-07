package handlers

import (
	"fmt"
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
	composePath := "database (root_stack_compose_base + merge)"
	composePreview := ""
	composeReadErr := ""
	composePreviewNote := ""
	mergedSaved := strings.TrimSpace(p.DB.GetSetting(ctx, settingRootStackComposeMerged))
	if mergedSaved != "" {
		composePreview = mergedSaved
	} else {
		base, err := p.loadRootStackComposeBase(ctx)
		if err != nil {
			composeReadErr = err.Error()
		} else {
			merged, mergeErr := caddy.GenerateRootStackCompose(base, panelSite.Domain, panelSite.EnableWWW, p.DB.GetCaddyConfig(ctx, "email"), p.DB.GetCaddyConfig(ctx, "caddy_image"))
			if mergeErr == nil {
				composePreview = string(merged)
			} else {
				composePreview = string(base)
				composePreviewNote = fmt.Sprintf("Effective stack preview could not be generated: %v", mergeErr)
			}
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
		"Nav":                      "nextdeploy",
		"Title":                    "NextDeploy",
		"AdminAPI":                 adminAPI,
		"CaddyUp":                  up,
		"StatusMsg":                statusMsg,
		"LiveConfig":               liveConfig,
		"CaddyRunning":             caddyRunning,
		"Email":                    strings.TrimSpace(caddyCfg["email"]),
		"CaddyImage":               strings.TrimSpace(caddyCfg["caddy_image"]),
		"CaddyNetwork":             caddy.CaddyNetwork,
		"PanelDomain":              panelSite.Domain,
		"PanelEnableWWW":           panelSite.EnableWWW,
		"PanelLabelsYAML":          strings.TrimSpace(labelYAML),
		"RootComposePath":          composePath,
		"RootCompose":              composePreview,
		"RootComposeReadErr":       composeReadErr,
		"RootComposePreviewNote":   composePreviewNote,
	}), "layouts/shell")
}
