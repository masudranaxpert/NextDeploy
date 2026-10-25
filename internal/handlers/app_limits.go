package handlers

// Per-project resource limits. An owner can cap one project's memory and CPU. The cap is a
// cgroup of its own that sits inside the owner's account cgroup, so the kernel enforces both:
// the project cannot pass its own limit, and all projects together cannot pass the account's.

import (
	"context"
	"fmt"
	"log"
	"strings"

	"panel/internal/db"
	"panel/internal/sysinfo"

	"github.com/gofiber/fiber/v2"
)

const (
	minAppMemoryMB = 64
	minAppCPUs     = 0.1
)

func appSliceKey(appID string) string { return strings.ReplaceAll(appID, "-", "_") }

// AppSliceName is the systemd slice of one project. systemd nests it inside the owner's slice
// because the name starts with it.
func AppSliceName(ownerID int64, appID string) string {
	return fmt.Sprintf("nextdeploy-user-%d-app-%s.slice", ownerID, appSliceKey(appID))
}

func appCgroupfsParent(ownerID int64, appID string) string {
	return fmt.Sprintf("/nextdeploy/user-%d/app-%s", ownerID, appSliceKey(appID))
}

// EnsureAppSliceLimits applies a project's memory/CPU ceiling to its own cgroup.
func (p *Panel) EnsureAppSliceLimits(ctx context.Context, ownerID int64, appID string, memMB int, cpus float64) error {
	mode := p.cgroupMode()
	if mode == "" {
		return nil
	}
	want := fmt.Sprintf("%d:%.2f", memMB, cpus)
	key := fmt.Sprintf("%d/%s", ownerID, appID)
	if got, ok := p.appSliceApplied.Load(key); ok && got == want {
		return nil
	}
	var err error
	if mode == "systemd" {
		mem, cpu := "MemoryMax=infinity", "CPUQuota="
		if memMB > 0 {
			mem = fmt.Sprintf("MemoryMax=%dM", memMB)
		}
		if cpus > 0 {
			cpu = fmt.Sprintf("CPUQuota=%d%%", int(cpus*100))
		}
		err = p.runCgroupHelper(ctx, ownerID, []string{"--pid=host"}, []string{
			"nsenter", "-t", "1", "-m", "-u", "-i", "--",
			"systemctl", "set-property", AppSliceName(ownerID, appID), mem, "MemorySwapMax=0", cpu,
		})
	} else {
		cg := "/sys/fs/cgroup" + appCgroupfsParent(ownerID, appID)
		userCg := fmt.Sprintf("/sys/fs/cgroup/nextdeploy/user-%d", ownerID)
		memVal, cpuVal := "max", "max 100000"
		if memMB > 0 {
			memVal = fmt.Sprintf("%d", int64(memMB)*1024*1024)
		}
		if cpus > 0 {
			cpuVal = fmt.Sprintf("%d 100000", int64(cpus*100000))
		}
		script := strings.Join([]string{
			"set -e",
			"echo +cpu > /sys/fs/cgroup/cgroup.subtree_control 2>/dev/null || true",
			"echo +memory > /sys/fs/cgroup/cgroup.subtree_control 2>/dev/null || true",
			"mkdir -p " + userCg,
			"echo +cpu > /sys/fs/cgroup/nextdeploy/cgroup.subtree_control 2>/dev/null || true",
			"echo +memory > /sys/fs/cgroup/nextdeploy/cgroup.subtree_control 2>/dev/null || true",
			"echo +cpu > " + userCg + "/cgroup.subtree_control 2>/dev/null || true",
			"echo +memory > " + userCg + "/cgroup.subtree_control 2>/dev/null || true",
			"mkdir -p " + cg,
			fmt.Sprintf("echo %s > %s/memory.max", memVal, cg),
			fmt.Sprintf("echo 0 > %s/memory.swap.max 2>/dev/null || true", cg),
			fmt.Sprintf("echo '%s' > %s/cpu.max", cpuVal, cg),
		}, "\n")
		err = p.runCgroupHelper(ctx, ownerID, []string{"-v", "/sys/fs/cgroup:/sys/fs/cgroup"}, []string{"sh", "-c", script})
	}
	if err != nil {
		return fmt.Errorf("apply project limits: %w", err)
	}
	p.appSliceApplied.Store(key, want)
	log.Printf("project cgroup: %s => memory=%dMB cpus=%.2f", appID, memMB, cpus)
	return nil
}

// RemoveAppSlice forgets a project's cgroup once its limits are cleared or the project is deleted.
func (p *Panel) RemoveAppSlice(ctx context.Context, ownerID int64, appID string) {
	mode := p.cgroupMode()
	if mode == "" {
		return
	}
	p.appSliceApplied.Delete(fmt.Sprintf("%d/%s", ownerID, appID))
	var err error
	if mode == "systemd" {
		err = p.runCgroupHelper(ctx, ownerID, []string{"--pid=host"}, []string{
			"nsenter", "-t", "1", "-m", "-u", "-i", "--", "systemctl", "revert", AppSliceName(ownerID, appID)})
	} else {
		err = p.runCgroupHelper(ctx, ownerID, []string{"-v", "/sys/fs/cgroup:/sys/fs/cgroup"},
			[]string{"sh", "-c", "rmdir /sys/fs/cgroup" + appCgroupfsParent(ownerID, appID) + " 2>/dev/null || true"})
	}
	if err != nil {
		log.Printf("project cgroup: remove %s: %v", appID, err)
	}
}

// AppCgroupParent is the cgroup a project's containers run in: its own limited one when the owner
// set limits, otherwise the owner's account cgroup (none for administrators).
func (p *Panel) AppCgroupParent(ctx context.Context, owner db.User, appID string) string {
	userParent := p.UserCgroupParent(ctx, owner)
	l, err := p.DB.GetAppLimits(ctx, appID)
	if err != nil || (l.MemoryMB <= 0 && l.CPUs <= 0) || p.cgroupMode() == "" {
		return userParent
	}
	if err := p.EnsureAppSliceLimits(ctx, owner.ID, appID, l.MemoryMB, l.CPUs); err != nil {
		log.Printf("project cgroup: %v", err)
		return userParent
	}
	if p.cgroupMode() == "cgroupfs" {
		return appCgroupfsParent(owner.ID, appID)
	}
	return AppSliceName(owner.ID, appID)
}

// ReapplyAppCgroupLimitsOnStart restores every project's cgroup after a panel or engine restart.
func (p *Panel) ReapplyAppCgroupLimitsOnStart() {
	if p.cgroupMode() == "" {
		return
	}
	ctx := context.Background()
	limits, err := p.DB.ListAppLimits(ctx)
	if err != nil {
		return
	}
	for _, l := range limits {
		app, err := p.DB.GetApp(ctx, l.AppID)
		if err != nil {
			continue
		}
		if err := p.EnsureAppSliceLimits(ctx, app.OwnerID, l.AppID, l.MemoryMB, l.CPUs); err != nil {
			log.Printf("project cgroup: startup reapply %s: %v", l.AppID, err)
		}
	}
}

// limitCaps is the most a project owned by owner may be given: their account's limits, and never
// more than the server really has.
func limitCaps(ctx context.Context, owner db.User) (memMB int, cpus float64, source string) {
	snap := sysinfo.Collect(ctx)
	memMB, cpus, source = int(snap.MemTotalGB*1024), float64(snap.NumCPU), "server"
	if owner.Role != db.RoleAdmin {
		if owner.MaxMemoryMB > 0 && (memMB == 0 || owner.MaxMemoryMB < memMB) {
			memMB, source = owner.MaxMemoryMB, "account"
		}
		if owner.MaxCPUs > 0 && (cpus == 0 || owner.MaxCPUs < cpus) {
			cpus, source = owner.MaxCPUs, "account"
		}
	}
	return memMB, cpus, source
}

// validateAppLimits checks a requested ceiling against the owner's caps.
func validateAppLimits(ctx context.Context, owner db.User, memMB int, cpus float64) error {
	if memMB < 0 || cpus < 0 {
		return fmt.Errorf("limits cannot be negative")
	}
	if memMB > 0 && memMB < minAppMemoryMB {
		return fmt.Errorf("memory must be at least %d MB (or 0 for no limit)", minAppMemoryMB)
	}
	if cpus > 0 && cpus < minAppCPUs {
		return fmt.Errorf("CPU must be at least %.1f cores (or 0 for no limit)", minAppCPUs)
	}
	capMem, capCPU, source := limitCaps(ctx, owner)
	where := "this server has"
	if source == "account" {
		where = "your account allows"
	}
	if capMem > 0 && memMB > capMem {
		return fmt.Errorf("memory %d MB is more than %s (%d MB)", memMB, where, capMem)
	}
	if capCPU > 0 && cpus > capCPU {
		return fmt.Errorf("CPU %.2f is more than %s (%.2f cores)", cpus, where, capCPU)
	}
	return nil
}

// AppLimitsGet GET /apps/:id/limits: this project's limits, the most it may be given, and what it uses now.
func (p *Panel) AppLimitsGet(c *fiber.Ctx) error {
	ctx := c.UserContext()
	app, owner, err := p.limitsApp(c)
	if err != nil {
		return err
	}
	l, _ := p.DB.GetAppLimits(ctx, app.ID)
	capMem, capCPU, source := limitCaps(ctx, owner)
	projects := map[string]bool{}
	for _, proj := range p.composeProjectsForApps(ctx, []db.App{app}) {
		projects[proj] = true
	}
	var memUsed uint64
	var cpuUsed float64
	for _, r := range p.usageRows(ctx) {
		if r.ComposeProject != "" && projects[r.ComposeProject] {
			memUsed += r.MemUsage
			cpuUsed += r.CPUPercent
		}
	}
	return c.JSON(fiber.Map{
		"ok":       true,
		"limits":   fiber.Map{"memory_mb": l.MemoryMB, "cpus": l.CPUs},
		"cap":      fiber.Map{"memory_mb": capMem, "cpus": capCPU, "source": source},
		"usage":    fiber.Map{"memory_mb": int64(memUsed / (1024 * 1024)), "cpu_cores": cpuUsed / 100},
		"enforced": p.cgroupMode() != "",
	})
}

// AppLimitsSet POST /apps/:id/limits {memory_mb, cpus}: 0 clears a limit.
func (p *Panel) AppLimitsSet(c *fiber.Ctx) error {
	ctx := c.UserContext()
	app, owner, err := p.limitsApp(c)
	if err != nil {
		return err
	}
	var body struct {
		MemoryMB int     `json:"memory_mb"`
		CPUs     float64 `json:"cpus"`
	}
	if err := c.BodyParser(&body); err != nil {
		return c.Status(400).JSON(fiber.Map{"ok": false, "error": "invalid body"})
	}
	if err := validateAppLimits(ctx, owner, body.MemoryMB, body.CPUs); err != nil {
		return c.Status(400).JSON(fiber.Map{"ok": false, "error": err.Error()})
	}
	// apply to the host first: a limit that could not be applied must not be saved as if it were
	if body.MemoryMB > 0 || body.CPUs > 0 {
		if err := p.EnsureAppSliceLimits(ctx, owner.ID, app.ID, body.MemoryMB, body.CPUs); err != nil {
			return c.Status(500).JSON(fiber.Map{"ok": false, "error": err.Error()})
		}
	}
	if err := p.DB.SetAppLimits(ctx, db.AppLimits{AppID: app.ID, MemoryMB: body.MemoryMB, CPUs: body.CPUs}); err != nil {
		return c.Status(500).JSON(fiber.Map{"ok": false, "error": err.Error()})
	}
	if body.MemoryMB <= 0 && body.CPUs <= 0 {
		p.RemoveAppSlice(ctx, owner.ID, app.ID)
	}
	// the new cgroup goes into the project's compose file; containers move into it when they are recreated
	if err := p.SyncAppCaddyOverrideCtx(ctx, app.ID); err != nil {
		log.Printf("app limits: sync override %s: %v", app.ID, err)
	}
	p.RecordAuditLog(c, "set_app_limits", "app", app.ID, fmt.Sprintf("Project limits: memory %d MB, CPU %.2f (0 = none)", body.MemoryMB, body.CPUs))
	return c.JSON(fiber.Map{"ok": true, "needs_redeploy": true})
}

// limitsApp loads the project and its owner; only the owner or an administrator may change limits.
func (p *Panel) limitsApp(c *fiber.Ctx) (db.App, db.User, error) {
	ctx := c.UserContext()
	app, err := p.DB.GetApp(ctx, c.Params("id"))
	if err != nil {
		return app, db.User{}, c.Status(404).JSON(fiber.Map{"ok": false, "error": "project not found"})
	}
	u, ok := currentUser(c)
	if !ok {
		return app, db.User{}, c.Status(401).JSON(fiber.Map{"ok": false, "error": "unauthorized"})
	}
	if c.Method() != fiber.MethodGet && u.Role != db.RoleAdmin && app.OwnerID != u.ID {
		return app, db.User{}, c.Status(403).JSON(fiber.Map{"ok": false, "error": "only the project owner or an administrator can change limits"})
	}
	if p.PHP != nil && p.DB.IsPHPSite(ctx, app.ID) {
		return app, db.User{}, c.Status(400).JSON(fiber.Map{"ok": false, "error": "PHP sites use the PHP settings (memory limit and workers) instead"})
	}
	owner, err := p.DB.GetUserByID(ctx, app.OwnerID)
	if err != nil {
		return app, db.User{}, c.Status(500).JSON(fiber.Map{"ok": false, "error": "owner not found"})
	}
	return app, owner, nil
}
