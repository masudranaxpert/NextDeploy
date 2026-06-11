package handlers

import (
	"panel/internal/handlers/utils"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"panel/internal/caddy"
	"panel/internal/dockerapi"
	"panel/internal/volumex"

	"github.com/gofiber/fiber/v2"
)

func (p *Panel) NextDeployPage(c *fiber.Ctx) error {
	ctx := c.UserContext()
	nextDeployFlash := utils.ReadFlash(c) // read once; cookie is cleared after this call
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
		sharedMounts := p.caddySharedMountsFromSettings(ctx)
		merged, mergeErr := caddy.GenerateRootStackCompose(base, panelSite.Domain, panelSite.EnableHTTPS, panelSite.EnableWWW, p.DB.GetCaddyConfig(ctx, "email"), p.DB.GetCaddyConfig(ctx, "caddy_image"), sharedMounts)
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
	dockerVolNames, _ := volumex.List(ctx)
	sort.Strings(dockerVolNames)
	sharedSel := map[string]bool{}
	for _, n := range parseCaddySharedVolumeNamesJSON(p.DB.GetSetting(ctx, settingCaddySharedVolumeNames)) {
		sharedSel[n] = true
	}
	sharedPrefix := normalizeCaddySharedMountPrefix(p.DB.GetSetting(ctx, settingCaddySharedMountPrefix))
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
		"PanelEnableHTTPS": panelSite.EnableHTTPS,
		"PanelEnableWWW":  panelSite.EnableWWW,
		"PanelLabelsYAML": strings.TrimSpace(labelYAML),
		// utils.ReadFlash is called once; legacy ?panelSaved=1 / ?volumesSaved=1 still accepted.
		"PanelSaved":   nextDeployFlash == "panelSaved" || c.Query("panelSaved") == "1",
		"VolumesSaved": nextDeployFlash == "volumesSaved" || c.Query("volumesSaved") == "1",
		"RootApplyStatus": func() string {
			// Only surface apply status right after a save redirect (panel or shared volumes).
			panelSaved := nextDeployFlash == "panelSaved" || c.Query("panelSaved") == "1"
			volSaved := nextDeployFlash == "volumesSaved" || c.Query("volumesSaved") == "1"
			if !panelSaved && !volSaved {
				return ""
			}
			return strings.TrimSpace(cfg[settingRootApplyStatus])
		}(),
		"RootComposePath":        composePath,
		"RootComposeApplyPath":   composeApplyPath,
		"RootComposeApplyNote":   composeApplyNote,
		"RootCompose":            composePreview,
		"RootComposeReadErr":     composeReadErr,
		"RootComposePreviewNote": composePreviewNote,
		"DockerVolumeNames":      dockerVolNames,
		"CaddySharedMountPrefix": sharedPrefix,
		"CaddySharedVolumeSelected": sharedSel,
	}), "layouts/shell")
}
