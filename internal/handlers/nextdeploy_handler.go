package handlers

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

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
	composeReadErr := ""
	composePreviewNote := ""
	composeApplyPath := composePath
	composeApplyNote := ""
	if err := rootStackComposeFileOrError(composePath); err != nil {
		composeReadErr = err.Error()
	}
	var base []byte
	var readErr error
	if composeReadErr == "" {
		base, readErr = os.ReadFile(composePath)
	}
	if readErr != nil {
		composeReadErr = readErr.Error()
	} else if composeReadErr == "" {
		merged, mergeErr := caddy.GenerateRootStackCompose(base, panelSite.Domain, panelSite.EnableWWW, p.DB.GetCaddyConfig(ctx, "email"), p.DB.GetCaddyConfig(ctx, "caddy_image"))
		if mergeErr == nil {
			composePreview = string(merged)
		} else {
			composePreview = string(base)
			composePreviewNote = fmt.Sprintf("Effective stack preview could not be generated: %v", mergeErr)
		}
	}
	if useDockerComposeHelper() {
		applyCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if hostComposePath, _, err := rootStackComposeHelperTarget(applyCtx); err == nil && strings.TrimSpace(hostComposePath) != "" {
			composeApplyPath = filepath.Clean(hostComposePath)
			if filepath.Clean(composeApplyPath) != filepath.Clean(composePath) {
				composeApplyNote = "The path above is inside the panel container. Save/apply uses the bind-mounted host compose file shown below."
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
		"PanelSaved":             c.Query("panelSaved") == "1",
		"RootComposePath":        composePath,
		"RootComposeApplyPath":   composeApplyPath,
		"RootComposeApplyNote":   composeApplyNote,
		"RootCompose":            composePreview,
		"RootComposeReadErr":     composeReadErr,
		"RootComposePreviewNote": composePreviewNote,
	}), "layouts/shell")
}
