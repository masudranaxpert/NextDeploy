package handlers

// Per-user cgroup resource groups: all containers of a non-admin user run under
// one cgroup (via compose cgroup_parent) so their apps share a single
// kernel-enforced memory/CPU budget. Limits are applied from inside the panel
// container through a short-lived privileged helper container.

import (
	"context"
	"fmt"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v3/disk"

	"panel/internal/db"
	"panel/internal/phphosting"
	"panel/internal/sysinfo"
	"panel/internal/workspace"
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

const appStorageCacheTTL = time.Minute

type appStorageEntry struct {
	size int64
	at   time.Time
}

func dirSizeBytes(root string) int64 {
	var total int64
	_ = filepath.WalkDir(root, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, ierr := d.Info(); ierr == nil {
			total += info.Size()
		}
		return nil
	})
	return total
}

// AppStorageBytes returns the on-disk size of an app workspace (cached with a short TTL).
func (p *Panel) AppStorageBytes(appID string) int64 {
	if v, ok := p.appStorage.Load(appID); ok {
		if e, ok2 := v.(appStorageEntry); ok2 && time.Since(e.at) < appStorageCacheTTL {
			return e.size
		}
	}
	size := dirSizeBytes(p.Store.Path(appID))
	p.appStorage.Store(appID, appStorageEntry{size: size, at: time.Now()})
	return size
}

// InvalidateAppStorageCache drops the cached size after writes (upload, extract, delete).
func (p *Panel) InvalidateAppStorageCache(appID string) {
	p.appStorage.Delete(appID)
}

// CheckStorageQuota returns an error when writing incomingBytes more into the
// app would push its owner past their storage limit. Admins are unlimited.
func (p *Panel) CheckStorageQuota(ctx context.Context, appID string, incomingBytes int64) error {
	app, err := p.DB.GetApp(ctx, appID)
	if err != nil {
		return nil
	}
	owner, err := p.DB.GetUserByID(ctx, app.OwnerID)
	if err != nil {
		return nil
	}
	return p.CheckStorageRoom(ctx, owner, incomingBytes)
}

const HostEmergencyFreeDiskBytes = int64(1536 * 1024 * 1024) // 1.5 GB host safety buffer

// HostFreeDiskBytes returns the currently available disk space on the root filesystem in bytes.
func (p *Panel) HostFreeDiskBytes(ctx context.Context) (int64, error) {
	if p.diskUsageFn != nil {
		return p.diskUsageFn(ctx)
	}
	usage, err := disk.UsageWithContext(ctx, "/")
	if err != nil {
		return 0, err
	}
	return int64(usage.Free), nil
}

// HostDiskCapacityMB returns total physical disk capacity in megabytes.
func (p *Panel) HostDiskCapacityMB(ctx context.Context) int {
	usage, err := disk.UsageWithContext(ctx, "/")
	if err == nil && usage != nil && usage.Total > 0 {
		return int(usage.Total / (1024 * 1024))
	}
	snap := sysinfo.Collect(ctx)
	return int(snap.DiskTotalGB * 1024)
}

// CheckStorageRoom refuses new data when owner (files + databases + volumes) is, or would
// become, over their storage limit, or when physical host disk space drops below the 1.5 GB
// emergency safety buffer. Admins are never refused.
func (p *Panel) CheckStorageRoom(ctx context.Context, owner db.User, incomingBytes int64) error {
	if owner.Role == db.RoleAdmin {
		return nil
	}

	// Host emergency safety buffer: if physical disk free space drops below 1.5 GB,
	// halt all non-admin writes to prevent OS deadlock and MariaDB table corruption.
	if freeBytes, err := p.HostFreeDiskBytes(ctx); err == nil {
		if freeBytes < HostEmergencyFreeDiskBytes || (incomingBytes > 0 && freeBytes-incomingBytes < HostEmergencyFreeDiskBytes) {
			return fmt.Errorf("server host storage protection active: free disk space is critically low (%s remaining, minimum 1.5 GB required). Writes are temporarily paused to protect database integrity", humanStorage(freeBytes))
		}
	}

	if owner.MaxStorageMB <= 0 {
		return nil
	}
	maxBytes := int64(owner.MaxStorageMB) * 1024 * 1024
	u := p.UserStorage(ctx, owner.ID, true)
	if used := u.Total(); used+incomingBytes > maxBytes {
		return fmt.Errorf("storage limit exceeded: using %s of %s (files %s, databases %s, volumes %s)",
			humanStorage(used), humanStorage(maxBytes), humanStorage(u.Files), humanStorage(u.Databases), humanStorage(u.Volumes))
	}
	return nil
}

func humanStorage(b int64) string { return workspace.FormatByteSize(b) }

// HumanStorage is the exported variant used by handlers/templates.
func HumanStorage(b int64) string { return workspace.FormatByteSize(b) }

// hostLimitError rejects per-user limits that the server cannot possibly honour, for example
// a 2 GB memory quota on a machine with 1 GB of RAM. It returns "" when the limits fit.
func hostLimitError(ctx context.Context, maxMemoryMB int, maxCPUs float64, maxStorageMB int) string {
	snap := sysinfo.Collect(ctx)
	if snap.MemTotalGB > 0 {
		hostMB := int(snap.MemTotalGB * 1024)
		if maxMemoryMB > hostMB {
			return fmt.Sprintf("Memory limit %d MB is more than this server has (%d MB RAM)", maxMemoryMB, hostMB)
		}
	}
	if snap.NumCPU > 0 && maxCPUs > float64(snap.NumCPU) {
		return fmt.Sprintf("CPU limit %.2f is more than this server has (%d CPUs)", maxCPUs, snap.NumCPU)
	}
	if snap.DiskTotalGB > 0 {
		hostMB := int(snap.DiskTotalGB * 1024)
		if maxStorageMB > hostMB {
			return fmt.Sprintf("Storage limit %d MB is more than this server's disk (%d MB)", maxStorageMB, hostMB)
		}
	}
	return ""
}

// clampLimitsToHost lowers limits to what the server really has (used for new accounts, whose
// database defaults may be larger than a small VPS).
func clampLimitsToHost(ctx context.Context, memMB int, cpus float64, storageMB int) (int, float64, int) {
	snap := sysinfo.Collect(ctx)
	if snap.MemTotalGB > 0 && memMB > int(snap.MemTotalGB*1024) {
		memMB = int(snap.MemTotalGB * 1024)
	}
	if snap.NumCPU > 0 && cpus > float64(snap.NumCPU) {
		cpus = float64(snap.NumCPU)
	}
	if snap.DiskTotalGB > 0 && storageMB > int(snap.DiskTotalGB*1024) {
		storageMB = int(snap.DiskTotalGB * 1024)
	}
	return memMB, cpus, storageMB
}

type quotaUsage struct {
	MemoryMB int64
	CPUCores float64
	At       time.Time
}

var quotaUsageCache sync.Map // user id -> quotaUsage

// userLiveUsage sums live memory and CPU of the user's own containers (cached briefly because
// it asks Docker for stats).
func (p *Panel) userLiveUsage(ctx context.Context, u db.User) (int64, float64) {
	if v, ok := quotaUsageCache.Load(u.ID); ok {
		q := v.(quotaUsage)
		if time.Since(q.At) >= 15*time.Second {
			// show the last reading at once and measure again in the background: an accurate CPU
			// reading takes a second or two and must not slow the page down
			if _, busy := quotaUsageBusy.LoadOrStore(u.ID, true); !busy {
				go func() {
					defer quotaUsageBusy.Delete(u.ID)
					bg, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					defer cancel()
					p.measureUserUsage(bg, u)
				}()
			}
		}
		return q.MemoryMB, q.CPUCores
	}
	return p.measureUserUsage(ctx, u)
}

var quotaUsageBusy sync.Map // user id -> true while a measurement runs

// measureUserUsage adds up the memory and (recent average) CPU of the user's own containers.
func (p *Panel) measureUserUsage(ctx context.Context, u db.User) (int64, float64) {
	apps := p.ownedApps(ctx, u.ID)
	projects := map[string]bool{}
	for _, proj := range p.composeProjectsForApps(ctx, apps) {
		projects[proj] = true
	}
	phpNames := map[string]bool{}
	if p.PHP != nil {
		for _, a := range apps {
			if site, err := p.DB.GetPHPSite(ctx, a.ID); err == nil && site.OwnerID == u.ID && site.PHPVersion != "" {
				phpNames[phphosting.RuntimeContainer(u.ID, site.PHPVersion)] = true
			}
		}
	}
	var mem uint64
	var cpu float64
	for _, r := range p.usageRows(ctx) {
		name := strings.TrimPrefix(r.Name, "/")
		if (r.ComposeProject != "" && projects[r.ComposeProject]) || phpNames[name] {
			mem += r.MemUsage
			cpu += r.CPUPercent
		}
	}
	q := quotaUsage{MemoryMB: int64(mem / (1024 * 1024)), CPUCores: cpu / 100, At: time.Now()}
	quotaUsageCache.Store(u.ID, q)
	return q.MemoryMB, q.CPUCores
}

// overcommitWarning tells the admin when the memory or storage limits of all users together exceed the
// server's hardware capacity. When overselling is active, limits act as ceilings guarded by runtime buffers.
func (p *Panel) overcommitWarning(ctx context.Context) string {
	snap := sysinfo.Collect(ctx)
	users, err := p.DB.ListUsers(ctx)
	if err != nil {
		return ""
	}
	totalMem := 0
	totalStorage := 0
	for _, u := range users {
		if u.Role != db.RoleAdmin && u.Status == db.UserStatusActive {
			totalMem += u.MaxMemoryMB
			totalStorage += u.MaxStorageMB
		}
	}

	var warnings []string
	if snap.MemTotalGB > 0 {
		hostMem := int(snap.MemTotalGB * 1024)
		if totalMem > hostMem {
			warnings = append(warnings, fmt.Sprintf("The memory limits of all users add up to %d MB, more than this server's %d MB of RAM.", totalMem, hostMem))
		}
	}

	hostDiskMB := p.HostDiskCapacityMB(ctx)
	if p.IsStorageOversellingEnabled(ctx) && hostDiskMB > 0 && totalStorage > hostDiskMB {
		warnings = append(warnings, fmt.Sprintf("User storage quotas add up to %s, exceeding physical disk (%s). Storage overselling is enabled; runtime writes are safeguarded by the 1.5 GB host emergency buffer.",
			HumanStorage(int64(totalStorage)*1024*1024),
			HumanStorage(int64(hostDiskMB)*1024*1024)))
	}

	if len(warnings) > 0 {
		return "Limits saved. " + strings.Join(warnings, " ")
	}
	return ""
}
