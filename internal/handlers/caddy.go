package handlers

import (
	"panel/internal/runutil"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"panel/internal/caddy"
	"panel/internal/db"
	"panel/internal/dockerapi"
	"panel/internal/phphosting"
	"panel/internal/dockerx"
	"panel/internal/sandbox"

	"github.com/gofiber/fiber/v2"
)

func sanitizeDomainRecord(d *db.AppDomain) {
	if d == nil {
		return
	}
	d.Domain = caddy.CleanQuotedValue(d.Domain)
	d.Service = caddy.CleanQuotedValue(d.Service)
}

func (p *Panel) userCanDomainFileServer(c *fiber.Ctx) bool {
	u, ok := currentUser(c)
	if !ok {
		return false
	}
	if u.Role == db.RoleAdmin {
		return true
	}
	fresh, err := p.DB.GetUserByID(c.UserContext(), u.ID)
	if err != nil {
		return false
	}
	return fresh.AllowDomainFileServer
}

func stripDomainFileServing(d *db.AppDomain) {
	d.ServeStatic = false
	d.StaticPath = ""
	d.StaticURLPrefix = ""
	d.ServeMedia = false
	d.MediaPath = ""
	d.MediaURLPrefix = ""
}

func (p *Panel) appDomainForRequest(c *fiber.Ctx) (db.AppDomain, error) {
	id := c.Params("id")
	did, err := strconv.ParseInt(c.Params("did"), 10, 64)
	if err != nil {
		return db.AppDomain{}, fiber.ErrNotFound
	}
	d, err := p.DB.GetAppDomain(c.UserContext(), did)
	if err != nil {
		return db.AppDomain{}, err
	}
	if strings.TrimSpace(d.AppID) != strings.TrimSpace(id) {
		return db.AppDomain{}, fiber.ErrNotFound
	}
	return d, nil
}

func (p *Panel) SyncAppCaddyOverride(c *fiber.Ctx, appID string) error {
	return p.SyncAppCaddyOverrideCtx(c.UserContext(), appID)
}

func (p *Panel) syncAppCaddyOverride(c *fiber.Ctx, appID string) error {
	return p.SyncAppCaddyOverride(c, appID)
}

const defaultDockerfileCompose = "services:\n  app:\n    build: .\n    restart: unless-stopped\n"

func (p *Panel) EffectiveBaseCompose(ctx context.Context, app db.App, appID string) (content []byte, onDisk bool, ok bool) {
	basePath := p.composeFilePath(ctx, app, appID)
	if data, err := os.ReadFile(basePath); err == nil {
		return data, true, true
	}
	if hasDockerfile, hasCompose := p.Store.HasDockerArtifacts(appID); hasDockerfile && !hasCompose {
		return []byte(defaultDockerfileCompose), false, true
	}
	return nil, false, false
}

// SyncAppCaddyOverrideCtx writes the merged Caddy compose override file.
// It passes the panel env so GenerateMergedCompose can inject env_file into every service.
func (p *Panel) SyncAppCaddyOverrideCtx(ctx context.Context, appID string) error {
	app, err := p.DB.GetApp(ctx, appID)
	if err != nil {
		return err
	}
	domains, err := p.DB.ListAppDomains(ctx, appID)
	if err != nil {
		return err
	}
	overridePath := p.composeOverridePath(ctx, appID)
	base, _, ok := p.EffectiveBaseCompose(ctx, app, appID)
	if !ok {
		if err := os.Remove(overridePath); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}

	owner, err := p.DB.GetUserByID(ctx, app.OwnerID)
	if err != nil {
		owner = db.User{
			ID:          app.OwnerID,
			Role:        db.RoleUser,
			MaxCPUs:     2.0,
			MaxMemoryMB: 2048,
		}
	}
	if owner.Role != db.RoleAdmin {
		secured, err := sandbox.ValidateComposeSecurity(base)
		if err != nil {
			return fmt.Errorf("security sandbox error: %w", err)
		}
		base = secured
		limited, err := sandbox.ApplyResourceLimits(base, owner.MaxCPUs, owner.MaxMemoryMB)
		if err != nil {
			return fmt.Errorf("resource limits error: %w", err)
		}
		base = limited
	}
	cgroupParent := p.AppCgroupParent(ctx, owner, appID)

	if err := p.WriteDockerRegistryConfig(ctx, appID, app.OwnerID); err != nil {
		log.Printf("failed to write docker registry config for app %s: %v", appID, err)
	}

	projCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	project := p.ActiveComposeProjectName(projCtx, app, appID)
	cancel()
	panelEnv, _ := p.DB.GetPanelEnv(ctx, appID)
	content, err := caddy.GenerateMergedCompose(base, project, domains, panelEnv, cgroupParent)
	if err != nil {
		return fmt.Errorf("generate merged compose: %w", err)
	}
	return atomicWriteFile(overridePath, content, 0640)
}

// ComposeSlotOverridePath returns the filepath for a specific slot override.
func (p *Panel) ComposeSlotOverridePath(ctx context.Context, id, slot string) string {
	if slot == "" {
		return p.ComposeOverridePath(ctx, id)
	}
	filename := fmt.Sprintf(".nextdeploy.generated.compose.%s.yml", slot)
	return filepath.Join(p.composeWorkspaceRoot(ctx, id), filename)
}

// SyncAppSlotComposeCtx generates the compose override for a specific Blue/Green slot,
// registers host port allocations in the database, and strips host port publication from the container.
func (p *Panel) SyncAppSlotComposeCtx(ctx context.Context, appID, slot string, previewOnly ...bool) ([]caddy.HostPortBinding, error) {
	isPreviewOnly := len(previewOnly) > 0 && previewOnly[0]
	app, err := p.DB.GetApp(ctx, appID)
	if err != nil {
		return nil, err
	}
	domains, err := p.DB.ListAppDomains(ctx, appID)
	if err != nil {
		return nil, err
	}
	overridePath := p.ComposeSlotOverridePath(ctx, appID, slot)
	base, _, ok := p.EffectiveBaseCompose(ctx, app, appID)
	if !ok {
		if err := os.Remove(overridePath); err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		return nil, nil
	}

	owner, err := p.DB.GetUserByID(ctx, app.OwnerID)
	if err != nil {
		owner = db.User{
			ID:          app.OwnerID,
			Role:        db.RoleUser,
			MaxCPUs:     2.0,
			MaxMemoryMB: 2048,
		}
	}
	if owner.Role != db.RoleAdmin {
		secured, err := sandbox.ValidateComposeSecurity(base)
		if err != nil {
			return nil, fmt.Errorf("security sandbox error: %w", err)
		}
		base = secured
		limited, err := sandbox.ApplyResourceLimits(base, owner.MaxCPUs, owner.MaxMemoryMB)
		if err != nil {
			return nil, fmt.Errorf("resource limits error: %w", err)
		}
		base = limited
	}
	cgroupParent := p.AppCgroupParent(ctx, owner, appID)

	if err := p.WriteDockerRegistryConfig(ctx, appID, app.OwnerID); err != nil {
		log.Printf("failed to write docker registry config for app %s: %v", appID, err)
	}

	project := p.SlotProjectName(app, appID, slot)
	panelEnv, _ := p.DB.GetPanelEnv(ctx, appID)
	var preview *db.AppDomain
	if isPreviewOnly {
		preview = p.previewSite(ctx, app, slot)
	}
	content, bindings, err := caddy.GenerateSlotMergedCompose(base, project, slot, domains, panelEnv, cgroupParent, preview, isPreviewOnly)
	if err != nil {
		return nil, fmt.Errorf("generate slot merged compose: %w", err)
	}

	for _, b := range bindings {
		if err := p.DB.AllocatePort(ctx, b.HostPort, b.BindIP, appID, b.Service, b.Protocol); err != nil {
			return nil, fmt.Errorf("port allocation conflict: %w", err)
		}
	}

	if err := atomicWriteFile(overridePath, content, 0640); err != nil {
		return nil, err
	}
	_ = atomicWriteFile(p.composeOverridePath(ctx, appID), content, 0640)
	return bindings, nil
}

// SyncAndApplyBackground writes the Caddy override for the active slot then runs `docker compose up -d`
// in a background goroutine so callers return immediately.
func (p *Panel) SyncAndApplyBackground(ctx context.Context, appID string) error {
	app, err := p.DB.GetApp(ctx, appID)
	if err != nil {
		return err
	}
	if p.PHP != nil && p.DB.IsPHPSite(ctx, appID) {
		// PHP sites have no compose project: publish their routes instead.
		sctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		return p.PHP.SyncRoutes(sctx, appID)
	}
	activeSlot := strings.TrimSpace(app.ActiveSlot)
	if activeSlot == "" {
		activeSlot = "blue"
	}
	if _, err := p.SyncAppSlotComposeCtx(ctx, appID, activeSlot, false); err != nil {
		return err
	}
	if strings.TrimSpace(app.PinnedSlot) != "" && strings.TrimSpace(app.StandbySlot) != "" {
		_, _ = p.SyncAppSlotComposeCtx(ctx, appID, strings.TrimSpace(app.StandbySlot), true)
	}
	go func() {
		bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		project := p.SlotProjectName(app, appID, activeSlot)
		paths := p.EffectiveComposePathsForSlot(bgCtx, app, appID, activeSlot)
		dir := p.AppSourcePath(bgCtx, appID)
		envFiles := p.ComposeEnvFiles(bgCtx, appID)
		if res := dockerx.ComposeUpNoBuild(bgCtx, dir, paths, project, nil, envFiles); !res.OK {
			log.Printf("compose apply app=%s project=%s: %s", appID, project, strings.TrimSpace(res.Output))
		}
		if strings.TrimSpace(app.PinnedSlot) != "" && strings.TrimSpace(app.StandbySlot) != "" {
			standbySlot := strings.TrimSpace(app.StandbySlot)
			standbyProj := p.SlotProjectName(app, appID, standbySlot)
			standbyPaths := p.EffectiveComposePathsForSlot(bgCtx, app, appID, standbySlot)
			_ = dockerx.ComposeUpNoBuild(bgCtx, dir, standbyPaths, standbyProj, nil, envFiles)
		}
	}()
	return nil
}

// POST /caddy/config — save global caddy settings
func (p *Panel) CaddySaveConfig(c *fiber.Ctx) error {
	u, ok := currentUser(c)
	if !ok || u.Role != db.RoleAdmin {
		return c.Status(fiber.StatusForbidden).SendString("forbidden")
	}
	ctx := c.UserContext()
	fields := map[string]string{
		"admin_api":   strings.TrimSpace(c.FormValue("admin_api")),
		"email":       strings.TrimSpace(c.FormValue("email")),
		"caddy_image": strings.TrimSpace(c.FormValue("caddy_image")),
	}
	for k, v := range fields {
		if err := p.DB.SetCaddyConfig(ctx, k, v); err != nil {
			return c.Status(500).SendString(err.Error())
		}
	}
	if err := p.syncRootStackCompose(ctx); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	if WantsJSON(c) {
		return c.JSON(fiber.Map{"ok": true})
	}
	return c.Redirect("/nextdeploy")
}

func (p *Panel) CaddyContainerAction(c *fiber.Ctx) error {
	u, ok := currentUser(c)
	if !ok || u.Role != db.RoleAdmin {
		return c.Status(fiber.StatusForbidden).SendString("forbidden")
	}
	action := strings.TrimSpace(c.FormValue("action"))
	ctx := c.UserContext()
	var err error
	switch action {
	case "reconcile_network":
		err = dockerapi.EnsureCaddySharedNetwork(ctx)
	case "start":
		err = dockerapi.StartContainerByName(ctx, caddy.CaddyContainerName)
		if err != nil && p.nextDeployComposePath() != "" {
			p.applyRootStackComposeBackground(p.nextDeployComposePath(), "caddy")
			err = nil
		} else if err == nil {
			_ = dockerapi.EnsureCaddySharedNetwork(ctx)
		}
	case "force_start":
		killCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		killScript := `
systemctl stop apache2 nginx caddy lighttpd httpd traefik 2>/dev/null || true
systemctl disable apache2 nginx caddy lighttpd httpd traefik 2>/dev/null || true
fuser -k -9 80/tcp 2>/dev/null || true
fuser -k -9 443/tcp 2>/dev/null || true
fuser -k -9 443/udp 2>/dev/null || true
if command -v ss >/dev/null 2>&1; then
    for pid in $(ss -lptn 'sport = :80 or sport = :443' 2>/dev/null | grep -o 'pid=[0-9]*' | cut -d= -f2); do
        [ -n "$pid" ] && kill -9 "$pid" 2>/dev/null || true
    done
fi
if command -v lsof >/dev/null 2>&1; then
    for pid in $(lsof -ti :80,:443 2>/dev/null); do
        [ -n "$pid" ] && kill -9 "$pid" 2>/dev/null || true
    done
fi
sleep 0.3
`
		_ = exec.CommandContext(killCtx, "docker", "run", "--rm", "--privileged", "--pid=host", "--entrypoint", "nsenter", runutil.HelperImage(), "-t", "1", "-m", "-u", "-n", "-i", "sh", "-c", killScript).Run()
		cancel()

		// Attempt to start or restart the existing container first
		err = dockerapi.StartContainerByName(ctx, caddy.CaddyContainerName)
		if err != nil {
			err = dockerapi.RestartContainerByName(ctx, caddy.CaddyContainerName)
		}
		if err != nil && p.nextDeployComposePath() != "" {
			p.applyRootStackComposeBackground(p.nextDeployComposePath(), "caddy")
			err = nil
		} else if err == nil {
			_ = dockerapi.EnsureCaddySharedNetwork(ctx)
		}
	case "restart":
		err = dockerapi.RestartContainerByName(ctx, caddy.CaddyContainerName)
		if err == nil {
			_ = dockerapi.EnsureCaddySharedNetwork(ctx)
		}
	case "stop":
		err = dockerapi.StopContainerByName(ctx, caddy.CaddyContainerName)
	default:
		return c.Status(400).SendString("invalid action")
	}
	if err != nil {
		return c.Status(500).SendString(err.Error())
	}
	if WantsJSON(c) {
		return c.JSON(fiber.Map{"ok": true})
	}
	return c.Redirect("/nextdeploy")
}

// POST /apps/:id/domains — create domain
func (p *Panel) AppDomainCreate(c *fiber.Ctx) error {
	id := c.Params("id")
	port, _ := strconv.Atoi(c.FormValue("port", "3000"))
	if port <= 0 {
		port = 3000
	}
	d := db.AppDomain{
		AppID:           id,
		Domain:          caddy.CleanQuotedValue(c.FormValue("domain")),
		Service:         caddy.CleanQuotedValue(c.FormValue("service")),
		Port:            port,
		IsPrimary:       c.FormValue("is_primary") == "on" || c.FormValue("is_primary") == "true" || c.FormValue("is_primary") == "1",
		EnableHTTPS:     c.FormValue("enable_https") == "on" || c.FormValue("enable_https") == "true",
		EnableWWW:       c.FormValue("enable_www") == "on" || c.FormValue("enable_www") == "true",
		ServeStatic:     c.FormValue("serve_static") == "on",
		StaticPath:      strings.TrimSpace(c.FormValue("static_path")),
		StaticURLPrefix: strings.TrimSpace(c.FormValue("static_url_prefix")),
		ServeMedia:      c.FormValue("serve_media") == "on",
		MediaPath:       strings.TrimSpace(c.FormValue("media_path")),
		MediaURLPrefix:  strings.TrimSpace(c.FormValue("media_url_prefix")),
		RouteRulesJSON:  "[]",
	}
	if d.Domain == "" {
		return c.Status(400).SendString("domain required")
	}
	if d.Service == "" {
		return c.Status(400).SendString("service required")
	}
	if p.PHP != nil && p.DB.IsPHPSite(c.UserContext(), id) && !phphosting.ValidHost(d.Domain) {
		return c.Status(400).SendString("invalid domain name")
	}
	if !p.userCanDomainFileServer(c) {
		stripDomainFileServing(&d)
	}
	if err := db.ValidateAppDomainFileServing(&d); err != nil {
		return c.Status(400).SendString(err.Error())
	}
	newID, err := p.DB.CreateAppDomain(c.UserContext(), d)
	if err != nil {
		return c.Status(500).SendString(err.Error())
	}
	if err := p.SyncAndApplyBackground(c.UserContext(), id); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	if WantsJSON(c) {
		doms, _ := p.DB.ListAppDomains(c.UserContext(), id)
		return c.JSON(fiber.Map{"ok": true, "id": newID, "domains": doms})
	}
	SetFlash(c, "domainSaved")
	return c.Redirect("/apps/" + id + "?tab=domains")
}

// POST /apps/:id/domains/:did/delete — delete domain
func (p *Panel) AppDomainDelete(c *fiber.Ctx) error {
	id := c.Params("id")
	d, err := p.appDomainForRequest(c)
	if err != nil {
		return c.Status(404).SendString("not found")
	}
	if err := p.DB.DeleteAppDomain(c.UserContext(), d.ID); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	if err := p.SyncAndApplyBackground(c.UserContext(), id); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	if WantsJSON(c) {
		doms, _ := p.DB.ListAppDomains(c.UserContext(), id)
		return c.JSON(fiber.Map{"ok": true, "domains": doms})
	}
	SetFlash(c, "domainSaved")
	return c.Redirect("/apps/" + id + "?tab=domains")
}

// POST /apps/:id/domains/:did/edit — update domain
func (p *Panel) AppDomainEdit(c *fiber.Ctx) error {
	id := c.Params("id")
	current, err := p.appDomainForRequest(c)
	if err != nil {
		return c.Status(404).SendString("not found")
	}
	port, _ := strconv.Atoi(c.FormValue("port", "3000"))
	if port <= 0 {
		port = 3000
	}
	isPrimary := current.IsPrimary
	if v := c.FormValue("is_primary"); v != "" {
		isPrimary = v == "on" || v == "true" || v == "1"
	}
	d := db.AppDomain{
		ID:              current.ID,
		AppID:           id,
		Domain:          caddy.CleanQuotedValue(c.FormValue("domain")),
		Service:         caddy.CleanQuotedValue(c.FormValue("service")),
		Port:            port,
		IsPrimary:       isPrimary,
		EnableHTTPS:     c.FormValue("enable_https") == "on" || c.FormValue("enable_https") == "true",
		EnableWWW:       c.FormValue("enable_www") == "on" || c.FormValue("enable_www") == "true",
		ServeStatic:     c.FormValue("serve_static") == "on",
		StaticPath:      strings.TrimSpace(c.FormValue("static_path")),
		StaticURLPrefix: strings.TrimSpace(c.FormValue("static_url_prefix")),
		ServeMedia:      c.FormValue("serve_media") == "on",
		MediaPath:       strings.TrimSpace(c.FormValue("media_path")),
		MediaURLPrefix:  strings.TrimSpace(c.FormValue("media_url_prefix")),
		RouteRulesJSON:  "[]",
	}
	if d.Domain == "" {
		return c.Status(400).SendString("domain required")
	}
	if d.Service == "" {
		return c.Status(400).SendString("service required")
	}
	if p.PHP != nil && p.DB.IsPHPSite(c.UserContext(), id) && !phphosting.ValidHost(d.Domain) {
		return c.Status(400).SendString("invalid domain name")
	}
	if !p.userCanDomainFileServer(c) {
		stripDomainFileServing(&d)
	}
	if err := db.ValidateAppDomainFileServing(&d); err != nil {
		return c.Status(400).SendString(err.Error())
	}
	if err := p.DB.UpdateAppDomain(c.UserContext(), d); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	if err := p.SyncAndApplyBackground(c.UserContext(), id); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	if WantsJSON(c) {
		doms, _ := p.DB.ListAppDomains(c.UserContext(), id)
		return c.JSON(fiber.Map{"ok": true, "domains": doms})
	}
	SetFlash(c, "domainSaved")
	return c.Redirect("/apps/" + id + "?tab=domains")
}

// POST /apps/:id/domains/:did/primary — pin domain as primary production URL
func (p *Panel) AppDomainSetPrimary(c *fiber.Ctx) error {
	id := c.Params("id")
	d, err := p.appDomainForRequest(c)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"ok": false, "error": "domain not found"})
	}
	if err := p.DB.SetPrimaryAppDomain(c.UserContext(), id, d.ID); err != nil {
		return c.Status(500).JSON(fiber.Map{"ok": false, "error": err.Error()})
	}
	doms, _ := p.DB.ListAppDomains(c.UserContext(), id)
	return c.JSON(fiber.Map{
		"ok":             true,
		"primary_domain": d.Domain,
		"domains":        doms,
	})
}

// GET /apps/:id/domains/:did/labels — return generated labels as JSON (for preview modal)
func (p *Panel) AppDomainLabels(c *fiber.Ctx) error {
	d, err := p.appDomainForRequest(c)
	if err != nil {
		return c.Status(404).SendString("not found")
	}
	sanitizeDomainRecord(&d)
	labels := caddy.GenerateLabels(d)
	yamlStr := caddy.LabelsToYAML(labels)
	return c.JSON(fiber.Map{
		"labels": labels,
		"yaml":   yamlStr,
	})
}

// GET /apps/:id/domains/:did/dns-check — resolve domain DNS and detect Cloudflare
func (p *Panel) AppDomainDNSCheck(c *fiber.Ctx) error {
	d, err := p.appDomainForRequest(c)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "domain not found"})
	}
	sanitizeDomainRecord(&d)
	domain := strings.TrimSpace(d.Domain)
	if domain == "" {
		return c.JSON(fiber.Map{"ok": false, "error": "empty domain"})
	}

	addrs, lookupErr := net.LookupHost(domain)
	if lookupErr != nil {
		return c.JSON(fiber.Map{
			"ok":         false,
			"domain":     domain,
			"error":      lookupErr.Error(),
			"cloudflare": false,
			"ips":        []string{},
		})
	}

	// Cloudflare IP ranges (IPv4 + IPv6) — from https://www.cloudflare.com/ips/
	cfRanges := []string{
		"173.245.48.0/20", "103.21.244.0/22", "103.22.200.0/22", "103.31.4.0/22",
		"141.101.64.0/18", "108.162.192.0/18", "190.93.240.0/20", "188.114.96.0/20",
		"197.234.240.0/22", "198.41.128.0/17", "162.158.0.0/15", "104.16.0.0/13",
		"104.24.0.0/14", "172.64.0.0/13", "131.0.72.0/22",
		"2400:cb00::/32", "2606:4700::/32", "2803:f800::/32", "2405:b500::/32",
		"2405:8100::/32", "2a06:98c0::/29", "2c0f:f248::/32",
	}

	isBehindCF := false
	for _, ip := range addrs {
		parsed := net.ParseIP(ip)
		if parsed == nil {
			continue
		}
		for _, cidr := range cfRanges {
			_, network, err := net.ParseCIDR(cidr)
			if err != nil {
				continue
			}
			if network.Contains(parsed) {
				isBehindCF = true
				break
			}
		}
		if isBehindCF {
			break
		}
	}

	return c.JSON(fiber.Map{
		"ok":          true,
		"domain":      domain,
		"ips":         addrs,
		"cloudflare":  isBehindCF,
		"internalTLS": caddy.ShouldUseInternalTLS(domain),
	})
}

// GET /caddy/logs — return caddy container logs as JSON lines
func (p *Panel) CaddyLogs(c *fiber.Ctx) error {
	u, ok := currentUser(c)
	if !ok || u.Role != db.RoleAdmin {
		return c.Status(fiber.StatusForbidden).SendString("forbidden")
	}
	tail, _ := strconv.Atoi(c.Query("tail", "300"))
	if tail <= 0 || tail > 2000 {
		tail = 300
	}
	res := dockerx.DockerLogs(c.UserContext(), caddy.CaddyContainerName, tail)
	lines := splitLogLines(res.Output)
	return c.JSON(fiber.Map{"ok": res.OK, "lines": lines})
}

// GET /caddy/overview — return live Caddy admin config & root stack compose YAML
func (p *Panel) CaddyOverview(c *fiber.Ctx) error {
	u, ok := currentUser(c)
	if !ok || u.Role != db.RoleAdmin {
		return c.Status(fiber.StatusForbidden).SendString("forbidden")
	}
	ctx := c.UserContext()

	caddyCfg, _ := p.DB.GetAllCaddyConfig(ctx)
	adminAPI := strings.TrimSpace(caddyCfg["admin_api"])
	if adminAPI == "" {
		adminAPI = "http://caddy:2019"
	}

	liveConfig, err := caddy.AdminConfigGet(ctx, adminAPI)
	if err != nil {
		liveConfig = fmt.Sprintf("{\n  \"error\": %q\n}", err.Error())
	}

	rootCompose := ""
	composePath := p.nextDeployComposePath()
	if composePath != "" {
		if data, rerr := os.ReadFile(composePath); rerr == nil {
			rootCompose = string(data)
		}
	}

	panelDomain := p.DB.GetSetting(ctx, settingPanelDomain)
	enableHTTPS := settingBool(p.DB.GetSetting(ctx, settingPanelEnableHTTPS), true)
	enableWWW := settingBool(p.DB.GetSetting(ctx, settingPanelEnableWWW), false)
	mountPrefix := normalizeCaddySharedMountPrefix(p.DB.GetSetting(ctx, settingCaddySharedMountPrefix))
	sharedVolumes := parseCaddySharedVolumeNamesJSON(p.DB.GetSetting(ctx, settingCaddySharedVolumeNames))

	return c.JSON(fiber.Map{
		"ok":                   true,
		"live_config":          liveConfig,
		"root_compose":         rootCompose,
		"panel_domain":         panelDomain,
		"panel_enable_https":   enableHTTPS,
		"panel_enable_www":     enableWWW,
		"shared_mount_prefix":  mountPrefix,
		"shared_volumes":       sharedVolumes,
	})
}

func splitLogLines(s string) []string {
	if strings.TrimSpace(s) == "" {
		return []string{}
	}
	raw := strings.Split(s, "\n")
	out := make([]string, 0, len(raw))
	for _, l := range raw {
		l = strings.TrimRight(l, "\r")
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}

// ── helpers ───────────────────────────────────────────────────────────────────

// LoadComposeServices parses the compose file to get service names.
func (p *Panel) LoadComposeServices(ctx context.Context, appID string) []string {
	return p.loadComposeServices(ctx, appID)
}

// loadComposeServices parses the compose file to get service names.
func (p *Panel) loadComposeServices(ctx context.Context, appID string) []string {
	app, err := p.DB.GetApp(ctx, appID)
	if err != nil {
		return nil
	}
	data, _, ok := p.EffectiveBaseCompose(ctx, app, appID)
	if !ok {
		return nil
	}
	return caddy.ParseComposeServiceNames(data)
}

func (p *Panel) WriteDockerRegistryConfig(ctx context.Context, appID string, ownerID int64) error {
	regs, err := p.DB.ListPrivateRegistries(ctx, &ownerID)
	if err != nil {
		return err
	}

	if len(regs) == 0 {
		dockerDir := filepath.Join(p.Store.ReservedPath(appID), ".docker")
		_ = os.Remove(filepath.Join(dockerDir, "config.json"))
		return nil
	}

	type DockerAuth struct {
		Auth string `json:"auth"`
	}
	type DockerConfig struct {
		Auths map[string]DockerAuth `json:"auths"`
	}

	cfg := DockerConfig{
		Auths: make(map[string]DockerAuth),
	}

	for _, r := range regs {
		pwd, err := sandbox.Decrypt(r.PasswordEncrypted)
		if err != nil {
			continue
		}
		authStr := base64.StdEncoding.EncodeToString([]byte(r.Username + ":" + pwd))
		serverAddr := strings.TrimSpace(r.ServerAddress)
		if serverAddr == "" {
			serverAddr = "https://index.docker.io/v1/"
		}
		cfg.Auths[serverAddr] = DockerAuth{Auth: authStr}
	}

	dockerDir := filepath.Join(p.Store.ReservedPath(appID), ".docker")
	if err := os.MkdirAll(dockerDir, 0750); err != nil {
		return err
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	return atomicWriteFile(filepath.Join(dockerDir, "config.json"), data, 0600)
}
