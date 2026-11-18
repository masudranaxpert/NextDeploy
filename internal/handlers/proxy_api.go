package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"panel/internal/caddy"
	"panel/internal/db"
	"panel/internal/dockerapi"
	"panel/internal/proxy"
	pcaddy "panel/internal/proxy/caddy"

	"github.com/gofiber/fiber/v2"
	"gopkg.in/yaml.v3"
)

const settingProxyLastSwitch = "proxy_last_switch"

// proxySwitchMu serializes engine switches (one at a time per panel process).
var proxySwitchMu sync.Mutex

type proxyLastSwitch struct {
	From       string `json:"from"`
	To         string `json:"to"`
	Status     string `json:"status"` // running | ok | failed | rolled_back
	OK         bool   `json:"ok"`
	RolledBack bool   `json:"rolled_back,omitempty"`
	Error      string `json:"error,omitempty"`
	Message    string `json:"message,omitempty"`
	UpdatedAt  string `json:"updated_at"`
}

func (p *Panel) readLastSwitch(ctx context.Context) *proxyLastSwitch {
	raw := strings.TrimSpace(p.DB.GetSetting(ctx, settingProxyLastSwitch))
	if raw == "" {
		return nil
	}
	var ls proxyLastSwitch
	if json.Unmarshal([]byte(raw), &ls) != nil {
		return nil
	}
	return &ls
}

func (p *Panel) writeLastSwitch(ctx context.Context, ls proxyLastSwitch) {
	ls.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if b, err := json.Marshal(ls); err == nil {
		_ = p.DB.SetSetting(ctx, settingProxyLastSwitch, string(b))
	}
}

func requireAdmin(c *fiber.Ctx) bool {
	u, ok := currentUser(c)
	return ok && u.Role == db.RoleAdmin
}

func proxyEngineChoices() []fiber.Map {
	return []fiber.Map{
		{"id": "caddy", "label": "Caddy", "description": "Default. Label-driven, simplest setup."},
		{"id": "angie", "label": "Angie", "description": "nginx-family. Lowest RAM per connection; best for small VPS and high traffic."},
		{"id": "ferron", "label": "Ferron", "description": "Fastest throughput, but no HTTP/2 idle or write timeouts yet.", "experimental": true},
	}
}

func (p *Panel) latestApplyMap(ctx context.Context) fiber.Map {
	rec, err := p.DB.LatestProxyApply(ctx)
	if err != nil || rec == nil {
		return nil
	}
	return fiber.Map{
		"generation": rec.Generation,
		"driver":     rec.Driver,
		"status":     rec.Status,
		"error":      rec.Error,
		"updated_at": rec.AppliedAt.UTC().Format(time.RFC3339),
	}
}

// GET /proxy/engine — active engine, live status, capabilities, last apply/switch.
func (p *Panel) ProxyEngineStatus(c *fiber.Ctx) error {
	if !requireAdmin(c) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"ok": false, "error": "forbidden"})
	}
	ctx := c.UserContext()
	engine := p.ProxyEngine()
	drv, err := p.ProxyDriver()
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"ok": false, "error": err.Error()})
	}
	sctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	st, serr := drv.Status(sctx)
	resp := fiber.Map{
		"ok":                true,
		"engine":            engine,
		"version":           st.Version,
		"running":           st.Running,
		"up":                st.Running,
		"experimental":      drv.Capabilities().Experimental,
		"capabilities":      drv.Capabilities(),
		"container_name":    drv.ContainerName(),
		"last_apply":        p.latestApplyMap(ctx),
		"last_switch":       p.readLastSwitch(ctx),
		"available_engines": proxyEngineChoices(),
	}
	if serr != nil {
		resp["status"] = serr.Error()
	} else if st.Running {
		resp["status"] = "running"
	} else {
		resp["status"] = "stopped"
	}
	return c.JSON(resp)
}

type proxySwitchRequest struct {
	Target          string `json:"target"`
	ExperimentalAck bool   `json:"experimental_ack"`
	Yes             bool   `json:"yes"`
}

// proxyPreflight evaluates a switch to target: capability gaps plus a dry-run render+validate.
func (p *Panel) proxyPreflight(ctx context.Context, target string) (fiber.Map, proxy.PreflightDiff, error) {
	target = normalizeProxyEngine(target)
	drv, err := p.proxyDriverFor(target)
	if err != nil {
		return nil, proxy.PreflightDiff{}, err
	}
	st, err := p.buildProxyState(ctx)
	if err != nil {
		return nil, proxy.PreflightDiff{}, fmt.Errorf("build proxy state: %w", err)
	}
	diff := proxy.EvaluateCapabilities(st, drv.Capabilities())
	checks := []fiber.Map{}
	if target != "caddy" {
		if bad, remapped := p.hostPathMismatches(ctx, []string{proxyEngineDataDir(target), p.WorkspacesRoot}); len(bad) > 0 {
			diff.CanSwitch = false
			checks = append(checks, fiber.Map{"name": "host-paths", "ok": false, "detail": "The proxy container cannot reach these panel paths because they are not on a host mount: " +
				strings.Join(bad, "; ") + ". Mount the panel data directory from the host (e.g. /data:/data, as install.sh does) before switching."})
		} else if len(remapped) > 0 {
			checks = append(checks, fiber.Map{"name": "host-paths", "ok": true, "detail": "Host paths mapped automatically: " + strings.Join(remapped, "; ")})
		} else {
			checks = append(checks, fiber.Map{"name": "host-paths", "ok": true})
		}
		files, rerr := drv.Render(st)
		if rerr != nil {
			diff.CanSwitch = false
			checks = append(checks, fiber.Map{"name": "render", "ok": false, "detail": rerr.Error()})
		} else {
			checks = append(checks, fiber.Map{"name": "render", "ok": true, "detail": fmt.Sprintf("%d files", len(files))})
			if vErr := p.dryRunValidate(ctx, target, drv, st, files); vErr != nil {
				diff.CanSwitch = false
				checks = append(checks, fiber.Map{"name": "validate", "ok": false, "detail": vErr.Error()})
			} else {
				checks = append(checks, fiber.Map{"name": "validate", "ok": true})
			}
		}
	}
	resp := fiber.Map{
		"ok":                        true,
		"target":                    target,
		"can_switch":                diff.CanSwitch,
		"requires_experimental_ack": diff.RequiresExperimentalAck,
		"hard_blocks":               diff.HardBlocks,
		"soft_warnings":             diff.SoftWarnings,
		"checks":                    checks,
	}
	return resp, diff, nil
}

// dryRunValidate stages files into a throwaway generation dir (host-visible for `docker run -v`),
// validates with the engine binary, and removes the stage again.
func (p *Panel) dryRunValidate(ctx context.Context, engine string, drv proxy.Driver, st *proxy.State, files proxy.Files) error {
	stage, err := proxyStageStore(engine)
	if err != nil {
		return err
	}
	if stage == nil {
		return nil
	}
	staged, _, err := stage.Stage("preflight-"+st.ComputeGeneration(), files)
	if err != nil {
		return err
	}
	defer stage.RemoveStaged(staged)
	vctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	return drv.Validate(vctx, staged)
}

// POST /proxy/preflight {target}
func (p *Panel) ProxyPreflight(c *fiber.Ctx) error {
	if !requireAdmin(c) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"ok": false, "error": "forbidden"})
	}
	var req proxySwitchRequest
	_ = c.BodyParser(&req)
	if strings.TrimSpace(req.Target) == "" {
		return c.Status(400).JSON(fiber.Map{"ok": false, "error": "target is required"})
	}
	resp, _, err := p.proxyPreflight(c.UserContext(), req.Target)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"ok": false, "target": req.Target, "error": err.Error()})
	}
	return c.JSON(resp)
}

// POST /proxy/switch {target, experimental_ack}
func (p *Panel) ProxySwitch(c *fiber.Ctx) error {
	if !requireAdmin(c) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"ok": false, "error": "forbidden"})
	}
	var req proxySwitchRequest
	_ = c.BodyParser(&req)
	target := normalizeProxyEngine(req.Target)
	from := p.ProxyEngine()
	if strings.TrimSpace(req.Target) == "" {
		return c.Status(400).JSON(fiber.Map{"ok": false, "error": "target is required"})
	}
	if target == from {
		return c.Status(400).JSON(fiber.Map{"ok": false, "error": "already using " + target})
	}
	if !proxySwitchMu.TryLock() {
		return c.Status(409).JSON(fiber.Map{"ok": false, "error": "a proxy engine switch is already running"})
	}
	ctx := c.UserContext()
	_, diff, err := p.proxyPreflight(ctx, target)
	if err != nil {
		proxySwitchMu.Unlock()
		return c.Status(500).JSON(fiber.Map{"ok": false, "error": err.Error()})
	}
	if !diff.CanSwitch {
		proxySwitchMu.Unlock()
		return c.Status(409).JSON(fiber.Map{"ok": false, "error": "preflight failed; run preflight for details", "from": from, "to": target})
	}
	if diff.RequiresExperimentalAck && !req.ExperimentalAck {
		proxySwitchMu.Unlock()
		return c.Status(400).JSON(fiber.Map{"ok": false, "error": "experimental engine requires experimental_ack", "from": from, "to": target})
	}
	if p.nextDeployComposePath() == "" {
		proxySwitchMu.Unlock()
		return c.Status(500).JSON(fiber.Map{"ok": false, "error": "root stack compose file not found"})
	}
	ls := proxyLastSwitch{From: from, To: target, Status: "running", Message: "switch started"}
	p.writeLastSwitch(ctx, ls)
	p.RecordAuditLog(c, "proxy_switch", "system", target, "Switching proxy engine from "+from+" to "+target)
	go func() {
		defer proxySwitchMu.Unlock()
		bg, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
		defer cancel()
		p.runProxySwitch(bg, from, target)
	}()
	return c.JSON(fiber.Map{"ok": true, "from": from, "to": target, "status": "running", "last_switch": ls})
}

// runProxySwitch performs the cutover and rolls back to `from` on any failure.
func (p *Panel) runProxySwitch(ctx context.Context, from, to string) {
	rootPath := p.nextDeployComposePath()
	prevRoot, err := os.ReadFile(rootPath)
	if err != nil {
		p.writeLastSwitch(ctx, proxyLastSwitch{From: from, To: to, Status: "failed", Error: "read root compose: " + err.Error()})
		return
	}
	backup := rootPath + ".proxy-switch-" + time.Now().UTC().Format("20060102T150405") + ".bak"
	_ = os.WriteFile(backup, prevRoot, 0o640)
	var oldImage string
	if oldDrv, derr := p.proxyDriverFor(from); derr == nil {
		oldImage = proxyContainerImage(ctx, oldDrv.ContainerName())
	}

	if err := p.cutoverProxy(ctx, from, to); err != nil {
		log.Printf("proxy switch %s -> %s failed: %v; rolling back", from, to, err)
		rbErr := p.rollbackProxy(ctx, from, to, rootPath, prevRoot)
		ls := proxyLastSwitch{From: from, To: to, Status: "rolled_back", RolledBack: rbErr == nil, Error: err.Error()}
		if rbErr != nil {
			ls.Status = "failed"
			ls.Error = err.Error() + "; rollback failed: " + rbErr.Error()
		}
		p.writeLastSwitch(ctx, ls)
		return
	}
	msg := "switched; previous root compose saved as " + filepath.Base(backup)
	if cleaned := p.cleanupAfterProxySwitch(ctx, from, to, oldImage); len(cleaned) > 0 {
		msg += "; cleaned up " + from + ": " + strings.Join(cleaned, ", ")
	}
	p.writeLastSwitch(ctx, proxyLastSwitch{From: from, To: to, Status: "ok", OK: true, Message: msg})
}

func (p *Panel) cutoverProxy(ctx context.Context, from, to string) error {
	// 1. File-rendered targets: write and activate the config before the container starts
	//    (the nd-proxy command waits for sites/<engine>.conf).
	if to != "caddy" {
		drv, err := p.proxyDriverFor(to)
		if err != nil {
			return err
		}
		p.setProxyEngine(to) // IR/labels must be built for the target engine
		st, err := p.buildProxyState(ctx)
		if err != nil {
			return fmt.Errorf("build state: %w", err)
		}
		files, err := drv.Render(st)
		if err != nil {
			return fmt.Errorf("render: %w", err)
		}
		stage, err := proxyStageStore(to)
		if err != nil {
			return err
		}
		staged, targetBase, err := stage.Stage(st.ComputeGeneration(), files)
		if err != nil {
			return fmt.Errorf("stage: %w", err)
		}
		if err := drv.Validate(ctx, staged); err != nil {
			stage.RemoveStaged(staged)
			return fmt.Errorf("validate: %w", err)
		}
		if _, err := stage.Activate(targetBase); err != nil {
			return fmt.Errorf("activate: %w", err)
		}
	}

	// 2. Persist the engine and rewrite the root stack.
	p.setProxyEngine(to)
	if err := p.DB.SetSetting(ctx, db.SettingProxyEngine, to); err != nil {
		return err
	}
	if err := p.rewriteRootComposeForEngine(ctx, to); err != nil {
		return fmt.Errorf("root compose: %w", err)
	}

	// 3. Stop the old proxy (frees :80/:443), start the new one.
	oldDrv, _ := p.proxyDriverFor(from)
	if oldDrv != nil {
		_ = dockerapi.StopContainerByName(ctx, oldDrv.ContainerName())
		_ = dockerapi.RemoveContainerByName(ctx, oldDrv.ContainerName())
	}
	p.rootApplyMu.Lock()
	p.runRootStackComposeLocked(ctx, p.nextDeployComposePath(), p.proxyServiceKey())
	p.rootApplyMu.Unlock()
	newDrv, err := p.ProxyDriver()
	if err != nil {
		return err
	}
	if err := waitProxyRunning(ctx, newDrv.ContainerName(), 90*time.Second); err != nil {
		return err
	}

	// 4. Re-label running apps (Angie upstream labels / Caddy site labels) and PHP/route holders.
	p.relabelRunningApps(ctx)
	if p.PHP != nil {
		p.PHP.Reconcile(ctx)
	}
	if to == "caddy" {
		p.ReconcileProxyRoutes(ctx)
		return nil
	}
	// 5. Reload with the final state and verify.
	if _, err := p.ProxySyncNow(ctx, proxy.Reason{Kind: "engine_switch"}, true); err != nil {
		return fmt.Errorf("apply on %s: %w", to, err)
	}
	return nil
}

func (p *Panel) rollbackProxy(ctx context.Context, from, to, rootPath string, prevRoot []byte) error {
	var errs []error
	newDrv, _ := p.proxyDriverFor(to)
	if newDrv != nil {
		_ = dockerapi.StopContainerByName(ctx, newDrv.ContainerName())
		_ = dockerapi.RemoveContainerByName(ctx, newDrv.ContainerName())
	}
	if err := atomicWriteFile(rootPath, prevRoot, 0o640); err != nil {
		errs = append(errs, err)
	}
	p.setProxyEngine(from)
	if err := p.DB.SetSetting(ctx, db.SettingProxyEngine, from); err != nil {
		errs = append(errs, err)
	}
	p.rootApplyMu.Lock()
	p.runRootStackComposeLocked(ctx, rootPath, p.proxyServiceKey())
	p.rootApplyMu.Unlock()
	p.relabelRunningApps(ctx)
	if p.PHP != nil {
		p.PHP.Reconcile(ctx)
	}
	if from == "caddy" {
		p.ReconcileProxyRoutes(ctx)
	} else if _, err := p.ProxySyncNow(ctx, proxy.Reason{Kind: "engine_switch_rollback"}, true); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// rewriteRootComposeForEngine makes the root stack run `engine` (recreating the Caddy service
// definition when switching back to Caddy) and then applies the usual managed fields.
func (p *Panel) rewriteRootComposeForEngine(ctx context.Context, engine string) error {
	path := p.nextDeployComposePath()
	p.rootComposeMu.Lock()
	base, err := os.ReadFile(path)
	if err == nil && engine == "caddy" && !rootComposeHasService(base, caddy.CaddyContainerName) {
		// Switching back to Caddy: recreate its service definition, then let the regular
		// root sync add labels, options, gateway ports and shared mounts.
		var svc, vols map[string]any
		svc, vols, err = pcaddy.New().StackService(proxy.StackOptions{Engine: proxy.EngineCaddy, WorkspacesDir: p.WorkspacesRoot})
		if err == nil {
			translateServiceVolumes(svc, p.hostPathOf)
			var merged []byte
			if merged, err = caddy.GenerateRootStackComposeForEngine(base, caddy.CaddyContainerName, svc, vols); err == nil {
				err = atomicWriteFile(path, merged, 0o640)
			}
		}
	}
	p.rootComposeMu.Unlock()
	if err != nil {
		return err
	}
	return p.syncRootStackCompose(ctx)
}

func rootComposeHasService(base []byte, key string) bool {
	var doc struct {
		Services map[string]any `yaml:"services"`
	}
	if yaml.Unmarshal(base, &doc) != nil {
		return false
	}
	_, ok := doc.Services[key]
	return ok
}

func waitProxyRunning(ctx context.Context, name string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		info, err := dockerapi.InspectContainerReadiness(ctx, name)
		if err == nil && info.Running {
			return nil
		}
		if time.Now().After(deadline) {
			if err != nil {
				return fmt.Errorf("proxy container %s not running: %w", name, err)
			}
			return fmt.Errorf("proxy container %s not running (status %s, exit %d)", name, info.Status, info.ExitCode)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

// relabelRunningApps regenerates compose overrides for every app and recreates the running
// ones so their containers carry the active engine's labels (stopped apps stay stopped).
func (p *Panel) relabelRunningApps(ctx context.Context) {
	apps, err := p.DB.ListApps(ctx)
	if err != nil {
		return
	}
	live, _ := p.inspectLivePortState(ctx)
	sort.Slice(apps, func(i, j int) bool { return apps[i].ID < apps[j].ID })
	for _, a := range apps {
		if p.PHP != nil && p.DB.IsPHPSite(ctx, a.ID) {
			continue
		}
		running := false
		if live != nil {
			for _, cand := range p.composeProjectCandidates(ctx, a, a.ID) {
				if live.RunningProjects[cand] {
					running = true
					break
				}
			}
		}
		if running {
			if err := p.SyncAndApplyBackground(ctx, a.ID); err != nil {
				log.Printf("proxy switch: relabel app %s: %v", a.ID, err)
			}
			continue
		}
		slot := strings.TrimSpace(a.ActiveSlot)
		if slot == "" {
			slot = "blue"
		}
		_, _ = p.SyncAppSlotComposeCtx(ctx, a.ID, slot, false)
	}
}

// GET /proxy/overview — Caddy keeps its admin-API overview; other engines report driver status
// and the active rendered config.
func (p *Panel) ProxyOverview(c *fiber.Ctx) error {
	if p.isCaddyEngine() {
		return p.CaddyOverview(c)
	}
	if !requireAdmin(c) {
		return c.Status(fiber.StatusForbidden).SendString("forbidden")
	}
	ctx := c.UserContext()
	drv, err := p.ProxyDriver()
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"ok": false, "error": err.Error()})
	}
	sctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	st, _ := drv.Status(sctx)
	rootCompose := ""
	if data, rerr := os.ReadFile(p.nextDeployComposePath()); rerr == nil {
		rootCompose = string(data)
	}
	return c.JSON(fiber.Map{
		"ok":                  true,
		"engine":              p.ProxyEngine(),
		"container_name":      drv.ContainerName(),
		"running":             st.Running,
		"up":                  st.Running,
		"version":             st.Version,
		"experimental":        drv.Capabilities().Experimental,
		"capabilities":        drv.Capabilities(),
		"last_apply":          p.latestApplyMap(ctx),
		"last_switch":         p.readLastSwitch(ctx),
		"live_config":         p.activeProxyConfigText(p.ProxyEngine()),
		"root_compose":        rootCompose,
		"panel_domain":        p.DB.GetSetting(ctx, settingPanelDomain),
		"panel_enable_https":  settingBool(p.DB.GetSetting(ctx, settingPanelEnableHTTPS), true),
		"panel_enable_www":    settingBool(p.DB.GetSetting(ctx, settingPanelEnableWWW), false),
		"shared_mount_prefix": normalizeCaddySharedMountPrefix(p.DB.GetSetting(ctx, settingCaddySharedMountPrefix)),
		"shared_volumes":      parseCaddySharedVolumeNamesJSON(p.DB.GetSetting(ctx, settingCaddySharedVolumeNames)),
	})
}

// activeProxyConfigText concatenates the files of the live generation for display.
func (p *Panel) activeProxyConfigText(engine string) string {
	stage, err := proxyStageStore(engine)
	if err != nil || stage == nil {
		return ""
	}
	target, err := proxy.ReadDirLink(stage.LinkPath())
	if err != nil || target == "" {
		return ""
	}
	files, err := stage.ReadTargetFiles(target)
	if err != nil {
		return ""
	}
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, n := range names {
		fmt.Fprintf(&b, "# ===== %s =====\n%s\n", n, files[n])
	}
	return b.String()
}

// POST /proxy/options — save, then re-render for file-rendered engines.
func (p *Panel) ProxyOptionsSave(c *fiber.Ctx) error {
	err := p.CaddyOptionsSave(c)
	if err == nil && !p.isCaddyEngine() && c.Response().StatusCode() < 300 {
		_ = p.ProxySync(c.UserContext(), proxy.Reason{Kind: "global_options"})
	}
	return err
}

// POST /proxy/config — Caddy saves admin/email/image as before; other engines only keep the
// ACME email (the image is pinned by the driver) and re-render.
func (p *Panel) ProxySaveConfig(c *fiber.Ctx) error {
	if p.isCaddyEngine() {
		return p.CaddySaveConfig(c)
	}
	if !requireAdmin(c) {
		return c.Status(fiber.StatusForbidden).SendString("forbidden")
	}
	ctx := c.UserContext()
	email := strings.TrimSpace(c.FormValue("email"))
	if err := p.DB.SetCaddyConfig(ctx, "email", email); err != nil {
		return c.Status(500).JSON(fiber.Map{"ok": false, "error": err.Error()})
	}
	_ = p.ProxySync(ctx, proxy.Reason{Kind: "acme_email"})
	return c.JSON(fiber.Map{"ok": true, "engine": p.ProxyEngine()})
}

// GET /apps/:id/domains/:did/config — Caddy labels or the engine's rendered site config.
func (p *Panel) AppDomainProxyConfig(c *fiber.Ctx) error {
	if p.isCaddyEngine() {
		return p.AppDomainLabels(c)
	}
	d, err := p.appDomainForRequest(c)
	if err != nil {
		return c.Status(404).SendString("not found")
	}
	text := p.activeProxyConfigText(p.ProxyEngine())
	host := strings.ToLower(strings.TrimSpace(d.Domain))
	// Show only the blocks that mention this host (best effort).
	var parts []string
	for _, chunk := range strings.Split(text, "# ===== ") {
		if host != "" && strings.Contains(strings.ToLower(chunk), host) {
			parts = append(parts, "# ===== "+chunk)
		}
	}
	cfg := strings.Join(parts, "\n")
	return c.JSON(fiber.Map{"ok": true, "engine": p.ProxyEngine(), "domain": d.Domain, "port": d.Port, "config": cfg, "rendered_config": cfg})
}

type selfMount struct {
	Source      string `json:"Source"`
	Destination string `json:"Destination"`
}

// inspectSelfMounts returns the bind mounts of the panel's own container. It returns an error
// when the panel does not run in a container (local development), which skips the check.
var inspectSelfMounts = func(ctx context.Context) ([]selfMount, error) {
	var lastErr error
	for _, name := range selfContainerCandidates() {
		out, err := exec.CommandContext(ctx, "docker", "inspect", "--format", "{{json .Mounts}}", name).Output()
		if err != nil {
			lastErr = err
			continue
		}
		var mounts []selfMount
		if err := json.Unmarshal(out, &mounts); err != nil {
			return nil, err
		}
		return mounts, nil
	}
	return nil, lastErr
}

// selfContainerCandidates lists names that may identify the panel's own container. The
// hostname (the container ID by default) is always tried: some runtimes do not create
// /.dockerenv. The "panel" name is only a fallback when /.dockerenv proves we are containerised,
// so a native panel never borrows the mounts of an unrelated container.
func selfContainerCandidates() []string {
	var out []string
	if hn, err := os.Hostname(); err == nil && strings.TrimSpace(hn) != "" {
		out = append(out, strings.TrimSpace(hn))
	}
	if _, err := os.Stat("/.dockerenv"); err == nil {
		out = append(out, "panel")
	}
	return out
}

// translateSelfMount maps a panel path to the Docker host path using the panel container's
// mounts (longest destination wins). ok is false when the path is not on any mount.
func translateSelfMount(mounts []selfMount, pth string) (string, bool) {
	pth = filepath.ToSlash(filepath.Clean(strings.TrimSpace(pth)))
	best, bestLen := -1, -1
	for i, m := range mounts {
		dst := strings.TrimRight(filepath.ToSlash(m.Destination), "/")
		if dst == "" || strings.TrimSpace(m.Source) == "" {
			continue
		}
		if (pth == dst || strings.HasPrefix(pth, dst+"/")) && len(dst) > bestLen {
			best, bestLen = i, len(dst)
		}
	}
	if best < 0 {
		return pth, false
	}
	m := mounts[best]
	dst := strings.TrimRight(filepath.ToSlash(m.Destination), "/")
	return strings.TrimRight(filepath.ToSlash(m.Source), "/") + strings.TrimPrefix(pth, dst), true
}

// selfMounts returns the panel container's mounts, cached after the first successful inspect.
// It returns nil when the panel is not containerised (paths are host paths already).
func (p *Panel) selfMounts(ctx context.Context) []selfMount {
	p.hostMountsMu.Lock()
	defer p.hostMountsMu.Unlock()
	if p.hostMountsLoaded {
		return p.hostMounts
	}
	ictx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	mounts, err := inspectSelfMounts(ictx)
	if err != nil && ictx.Err() != nil {
		return nil // docker was slow: retry on the next call
	}
	// A failed inspect means no container of ours exists (native panel): paths are host paths.
	p.hostMounts, p.hostMountsLoaded = mounts, true
	return mounts
}

// hostPathOf maps a panel path to the path the Docker daemon sees, so `docker run -v` and
// the proxy service volumes work with layouts such as ./data:/data.
func (p *Panel) hostPathOf(pth string) string {
	if strings.TrimSpace(pth) == "" || !strings.HasPrefix(filepath.ToSlash(pth), "/") {
		return pth
	}
	mounts := p.selfMounts(context.Background())
	if len(mounts) == 0 {
		return pth
	}
	if host, ok := translateSelfMount(mounts, pth); ok {
		return host
	}
	return pth
}

// hostPathMismatches lists panel paths the Angie/Ferron containers cannot reach: paths that
// are not on any mount of the panel container (they exist only inside its own filesystem).
// remapped lists paths that live under a different host path; they are translated
// automatically and only reported for information.
func (p *Panel) hostPathMismatches(ctx context.Context, paths []string) (bad, remapped []string) {
	mounts := p.selfMounts(ctx)
	if len(mounts) == 0 {
		return nil, nil
	}
	for _, pth := range paths {
		pth = filepath.ToSlash(filepath.Clean(strings.TrimSpace(pth)))
		if pth == "" || pth == "." {
			continue
		}
		host, ok := translateSelfMount(mounts, pth)
		if !ok {
			bad = append(bad, pth+" (not on a host mount)")
			continue
		}
		if host != pth {
			remapped = append(remapped, pth+" → "+host)
		}
	}
	return bad, remapped
}
