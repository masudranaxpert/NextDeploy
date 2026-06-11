package handlers

// Per-user cgroup resource groups: all containers of a non-admin user run under
// one cgroup (via compose cgroup_parent) so their apps share a single
// kernel-enforced memory/CPU budget. Limits are applied from inside the panel
// container through a short-lived privileged helper container.

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	"panel/internal/db"
)

func UserSliceName(userID int64) string {
	return fmt.Sprintf("nextdeploy-user-%d.slice", userID)
}

func userCgroupfsParent(userID int64) string {
	return fmt.Sprintf("/nextdeploy/user-%d", userID)
}

func cgroupHelperImage() string {
	if img := strings.TrimSpace(os.Getenv("PANEL_CGROUP_HELPER_IMAGE")); img != "" {
		return img
	}
	return "docker:cli"
}

// cgroupMode returns "systemd" or "cgroupfs" when per-user group limits are
// usable on this host, or "" when unsupported. Detection results are cached;
// transient `docker info` failures are retried on the next call.
func (p *Panel) cgroupMode() string {
	p.cgroupMu.Lock()
	defer p.cgroupMu.Unlock()
	if p.cgroupChecked {
		return p.cgroupModeVal
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "docker", "info", "--format", "{{.CgroupDriver}} {{.CgroupVersion}}").Output()
	if err != nil {
		log.Printf("user cgroup: failed to detect docker cgroup driver (will retry): %v", err)
		return ""
	}
	fields := strings.Fields(strings.TrimSpace(string(out)))
	driver, version := "", ""
	if len(fields) > 0 {
		driver = fields[0]
	}
	if len(fields) > 1 {
		version = fields[1]
	}
	switch {
	case driver == "systemd":
		p.cgroupModeVal = "systemd"
	case driver == "cgroupfs" && version == "2":
		p.cgroupModeVal = "cgroupfs"
	}
	p.cgroupChecked = true
	if p.cgroupModeVal == "" {
		log.Printf("user cgroup: driver=%q cgroup-version=%q unsupported; per-user group limits disabled", driver, version)
	} else {
		log.Printf("user cgroup: per-user group limits enabled (mode=%s)", p.cgroupModeVal)
	}
	return p.cgroupModeVal
}

func (p *Panel) UserSliceSupported(ctx context.Context) bool {
	return p.cgroupMode() != ""
}

// EnsureUserSliceLimits applies the user's memory/CPU limits to their cgroup.
// Successful applies are cached, so repeated calls only hit the host when the
// limits changed or after a panel restart.
func (p *Panel) EnsureUserSliceLimits(ctx context.Context, userID int64, maxMemoryMB int, maxCPUs float64) error {
	if userID <= 0 || maxMemoryMB <= 0 || maxCPUs <= 0 {
		return nil
	}
	mode := p.cgroupMode()
	if mode == "" {
		return nil
	}
	want := fmt.Sprintf("%d:%.2f", maxMemoryMB, maxCPUs)
	if got, ok := p.userSliceApplied.Load(userID); ok && got == want {
		return nil
	}
	var err error
	if mode == "systemd" {
		err = p.applySystemdSliceLimits(ctx, userID, maxMemoryMB, maxCPUs)
	} else {
		err = p.applyCgroupfsLimits(ctx, userID, maxMemoryMB, maxCPUs)
	}
	if err != nil {
		return err
	}
	p.userSliceApplied.Store(userID, want)
	return nil
}

func (p *Panel) runCgroupHelper(ctx context.Context, userID int64, extraArgs []string, cmd []string) error {
	cctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	name := fmt.Sprintf("nextdeploy-cgroup-%d-%d", userID, time.Now().UnixNano())
	args := []string{"run", "--rm", "--name", name, "--privileged", "--network=none"}
	args = append(args, extraArgs...)
	args = append(args, cgroupHelperImage())
	args = append(args, cmd...)
	out, err := exec.CommandContext(cctx, "docker", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("cgroup helper failed: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// systemd mode: `systemctl set-property` writes a persistent drop-in, so it
// works for not-yet-active slices and survives host reboots.
func (p *Panel) applySystemdSliceLimits(ctx context.Context, userID int64, maxMemoryMB int, maxCPUs float64) error {
	slice := UserSliceName(userID)
	err := p.runCgroupHelper(ctx, userID, []string{"--pid=host"}, []string{
		"nsenter", "-t", "1", "-m", "-u", "-i", "--",
		"systemctl", "set-property", slice,
		fmt.Sprintf("MemoryMax=%dM", maxMemoryMB),
		"MemorySwapMax=0",
		fmt.Sprintf("CPUQuota=%d%%", int(maxCPUs*100)),
	})
	if err != nil {
		return fmt.Errorf("apply limits for %s: %w", slice, err)
	}
	log.Printf("user cgroup: %s => MemoryMax=%dM CPUQuota=%d%%", slice, maxMemoryMB, int(maxCPUs*100))
	return nil
}

// cgroupfs mode (cgroup v2): write memory.max / cpu.max directly under
// /sys/fs/cgroup/nextdeploy/user-<id>. Not persistent across engine restarts;
// ReapplyUserCgroupLimitsOnStart restores it.
func (p *Panel) applyCgroupfsLimits(ctx context.Context, userID int64, maxMemoryMB int, maxCPUs float64) error {
	cg := fmt.Sprintf("/sys/fs/cgroup/nextdeploy/user-%d", userID)
	memBytes := int64(maxMemoryMB) * 1024 * 1024
	period := int64(100000)
	quota := int64(maxCPUs * float64(period))
	script := strings.Join([]string{
		"set -e",
		"echo +cpu > /sys/fs/cgroup/cgroup.subtree_control 2>/dev/null || true",
		"echo +memory > /sys/fs/cgroup/cgroup.subtree_control 2>/dev/null || true",
		"mkdir -p " + cg,
		"echo +cpu > /sys/fs/cgroup/nextdeploy/cgroup.subtree_control 2>/dev/null || true",
		"echo +memory > /sys/fs/cgroup/nextdeploy/cgroup.subtree_control 2>/dev/null || true",
		fmt.Sprintf("echo %d > %s/memory.max", memBytes, cg),
		fmt.Sprintf("echo 0 > %s/memory.swap.max 2>/dev/null || true", cg),
		fmt.Sprintf("echo '%d %d' > %s/cpu.max", quota, period, cg),
	}, "\n")
	err := p.runCgroupHelper(ctx, userID,
		[]string{"-v", "/sys/fs/cgroup:/sys/fs/cgroup"},
		[]string{"sh", "-c", script})
	if err != nil {
		return fmt.Errorf("apply limits for %s: %w", userCgroupfsParent(userID), err)
	}
	log.Printf("user cgroup: %s => memory.max=%dM cpu.max=%.2f", userCgroupfsParent(userID), maxMemoryMB, maxCPUs)
	return nil
}

// UserCgroupParent returns the cgroup_parent to inject for an app owner, or ""
// when group limits do not apply (admins, unsupported host). Apply failures are
// logged but never block a deploy.
func (p *Panel) UserCgroupParent(ctx context.Context, owner db.User) string {
	if owner.ID <= 0 || owner.Role == db.RoleAdmin {
		return ""
	}
	mode := p.cgroupMode()
	if mode == "" {
		return ""
	}
	if err := p.EnsureUserSliceLimits(ctx, owner.ID, owner.MaxMemoryMB, owner.MaxCPUs); err != nil {
		log.Printf("user cgroup: %v", err)
	}
	if mode == "cgroupfs" {
		return userCgroupfsParent(owner.ID)
	}
	return UserSliceName(owner.ID)
}

// ReapplyUserCgroupLimitsOnStart restores every user's cgroup limits after a
// panel/engine restart. Needed for cgroupfs mode where limits live in a tmpfs
// and containers with restart policies may come back up before any deploy.
func (p *Panel) ReapplyUserCgroupLimitsOnStart() {
	if p.cgroupMode() == "" {
		return
	}
	ctx := context.Background()
	users, err := p.DB.ListUsers(ctx)
	if err != nil {
		log.Printf("user cgroup: startup reapply skipped: %v", err)
		return
	}
	for _, u := range users {
		if u.Role == db.RoleAdmin {
			continue
		}
		if err := p.EnsureUserSliceLimits(ctx, u.ID, u.MaxMemoryMB, u.MaxCPUs); err != nil {
			log.Printf("user cgroup: startup reapply user %d: %v", u.ID, err)
		}
	}
}
