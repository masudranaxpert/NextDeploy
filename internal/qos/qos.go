package qos

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"os/exec"
	"panel/internal/runutil"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types"

	"panel/internal/db"
	"panel/internal/dockerapi"
)

const (
	defaultRootBandwidth = "10gbit"
	defaultClassID       = "999"
	mangleChainName      = "ND_BANDWIDTH"
)

// UserMark computes a deterministic firewall mark and tc minor class ID for a user.
// Returns a 16-bit integer (> 1000 and <= 65000) that avoids collisions with 1:1 and 1:999.
func UserMark(userID int64) uint32 {
	if userID <= 0 {
		return 0
	}
	return uint32(1000 + (userID % 64000))
}

func defaultHelperImage() string {
	if img := strings.TrimSpace(os.Getenv("PANEL_QOS_HELPER_IMAGE")); img != "" {
		return img
	}
	if img := strings.TrimSpace(os.Getenv("PANEL_IMAGE")); img != "" {
		return img
	}
	return runutil.HelperImage()
}

// runHostViaDocker runs a shell script on the host network namespace via privileged nsenter helper.
func runHostViaDocker(ctx context.Context, script string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// If docker binary is available, run privileged nsenter container
	if _, err := exec.LookPath("docker"); err == nil {
		args := []string{
			"run", "--rm", "--privileged", "--pid=host",
			"--entrypoint", "nsenter",
			defaultHelperImage(),
			"-t", "1", "-m", "-u", "-n", "-i",
			"sh", "-c", script,
		}
		out, err := exec.CommandContext(cctx, "docker", args...).CombinedOutput()
		if err != nil {
			return string(out), fmt.Errorf("docker nsenter failed: %w: %s", err, strings.TrimSpace(string(out)))
		}
		return string(out), nil
	}

	// Fallback for direct host execution (dev/testing)
	out, err := exec.CommandContext(cctx, "sh", "-c", script).CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("host sh failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// Manager manages host traffic control (tc) and iptables mangle rules for aggregate per-user bandwidth shaping.
type Manager struct {
	mu            sync.Mutex
	wanInterface  string
	lastSignature string
	activeMarks   map[int64]uint32
	runner        func(ctx context.Context, script string) (string, error)
}

// NewManager creates a QoS manager. If wanInterface is empty, it will be detected on first Init.
func NewManager(wanInterface string) *Manager {
	if wanInterface == "" {
		wanInterface = strings.TrimSpace(os.Getenv("PANEL_WAN_INTERFACE"))
	}
	return &Manager{
		wanInterface: wanInterface,
		activeMarks:  make(map[int64]uint32),
		runner:       runHostViaDocker,
	}
}

// SetRunner overrides the command runner (primarily for unit tests).
func (m *Manager) SetRunner(fn func(ctx context.Context, script string) (string, error)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.runner = fn
}

// WANInterface returns the detected or configured WAN interface name.
func (m *Manager) WANInterface() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.wanInterface
}

// DetectWANInterface queries the host for its primary default route network interface.
func (m *Manager) DetectWANInterface(ctx context.Context) (string, error) {
	// Look specifically for the interface token following the 'dev' keyword in default routes
	script := "ip route show default | awk '{for(i=1;i<=NF;i++) if($i==\"dev\"){print $(i+1); exit}}'"
	out, err := m.runner(ctx, script)
	if err != nil {
		return "", err
	}
	iface := strings.TrimSpace(out)
	if iface == "" {
		// Fallback check for IPv4 default route
		out, err = m.runner(ctx, "ip -4 route list 0/0 | awk '{for(i=1;i<=NF;i++) if($i==\"dev\"){print $(i+1); exit}}'")
		if err == nil {
			iface = strings.TrimSpace(out)
		}
	}
	if iface == "" {
		// Fallback to first non-virtual up interface
		out, err = m.runner(ctx, "ip -o link show up | awk -F': ' '$2 !~ /^(lo|docker|br-|veth)/ {print $2; exit}'")
		if err == nil {
			iface = strings.TrimSpace(out)
		}
	}
	if iface == "" {
		iface = "eth0"
	}
	return iface, nil
}

// InitHost ensures the root HTB qdisc and iptables ND_BANDWIDTH chain exist on the host.
func (m *Manager) InitHost(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.wanInterface == "" {
		detected, err := m.DetectWANInterface(ctx)
		if err != nil {
			log.Printf("qos: failed to detect WAN interface, defaulting to eth0: %v", err)
			detected = "eth0"
		}
		m.wanInterface = detected
	}

	initScript := fmt.Sprintf(`set -e
# Root HTB qdisc and default unthrottled class
if ! tc qdisc show dev %s | grep -q "htb 1:"; then
  tc qdisc add dev %s root handle 1: htb default %s 2>/dev/null || tc qdisc replace dev %s root handle 1: htb default %s 2>/dev/null || true
fi
tc class replace dev %s parent 1: classid 1:1 htb rate %s ceil %s 2>/dev/null || true
tc class replace dev %s parent 1:1 classid 1:%s htb rate %s ceil %s 2>/dev/null || true

# Dedicated iptables mangle chain for NextDeploy user bandwidth marking
iptables -t mangle -N %s 2>/dev/null || true
iptables -t mangle -C PREROUTING -j %s 2>/dev/null || iptables -t mangle -I PREROUTING 1 -j %s 2>/dev/null || iptables -t mangle -A PREROUTING -j %s
`, m.wanInterface,
		m.wanInterface, defaultClassID,
		m.wanInterface, defaultClassID,
		m.wanInterface, defaultRootBandwidth, defaultRootBandwidth,
		m.wanInterface, defaultClassID, defaultRootBandwidth, defaultRootBandwidth,
		mangleChainName, mangleChainName, mangleChainName, mangleChainName)

	_, err := m.runner(ctx, initScript)
	if err != nil {
		return fmt.Errorf("init host qos: %w", err)
	}
	return nil
}

// BuildSyncScript constructs the idempotent shell script to sync tc classes, filters, and iptables rules.
func BuildSyncScript(wan string, userLimits map[int64]int, userIPs map[int64][]string, staleMarks []uint32) string {
	var sb strings.Builder
	sb.WriteString("#!/bin/sh\n")
	sb.WriteString("set -e\n\n")

	// Ensure root HTB and mangle chain
	sb.WriteString(fmt.Sprintf("if ! tc qdisc show dev %s | grep -q \"htb 1:\"; then\n  tc qdisc add dev %s root handle 1: htb default %s 2>/dev/null || tc qdisc replace dev %s root handle 1: htb default %s 2>/dev/null || true\nfi\n", wan, wan, defaultClassID, wan, defaultClassID))
	sb.WriteString(fmt.Sprintf("tc class replace dev %s parent 1: classid 1:1 htb rate %s ceil %s 2>/dev/null || true\n", wan, defaultRootBandwidth, defaultRootBandwidth))
	sb.WriteString(fmt.Sprintf("tc class replace dev %s parent 1:1 classid 1:%s htb rate %s ceil %s 2>/dev/null || true\n\n", wan, defaultClassID, defaultRootBandwidth, defaultRootBandwidth))

	sb.WriteString(fmt.Sprintf("iptables -t mangle -N %s 2>/dev/null || true\n", mangleChainName))
	sb.WriteString(fmt.Sprintf("iptables -t mangle -C PREROUTING -j %s 2>/dev/null || iptables -t mangle -I PREROUTING 1 -j %s 2>/dev/null || iptables -t mangle -A PREROUTING -j %s\n", mangleChainName, mangleChainName, mangleChainName))
	sb.WriteString(fmt.Sprintf("iptables -t mangle -F %s\n\n", mangleChainName))

	// Clean up stale tc classes for users whose limits were removed or reduced to 0
	for _, mark := range staleMarks {
		if mark > 0 {
			sb.WriteString(fmt.Sprintf("tc filter del dev %s protocol ip parent 1:0 prio 1 handle %d fw classid 1:%d 2>/dev/null || true\n", wan, mark, mark))
			sb.WriteString(fmt.Sprintf("tc class del dev %s classid 1:%d 2>/dev/null || true\n", wan, mark))
		}
	}

	// Apply active user limits
	var userIDs []int64
	for uid := range userLimits {
		userIDs = append(userIDs, uid)
	}
	sort.Slice(userIDs, func(i, j int) bool { return userIDs[i] < userIDs[j] })

	for _, uid := range userIDs {
		limit := userLimits[uid]
		if limit <= 0 {
			continue
		}
		mark := UserMark(uid)
		sb.WriteString(fmt.Sprintf("tc class replace dev %s parent 1:1 classid 1:%d htb rate %dmbit ceil %dmbit\n", wan, mark, limit, limit))
		sb.WriteString(fmt.Sprintf("tc filter replace dev %s protocol ip parent 1:0 prio 1 handle %d fw classid 1:%d 2>/dev/null || tc filter add dev %s protocol ip parent 1:0 prio 1 handle %d fw classid 1:%d 2>/dev/null || true\n", wan, mark, mark, wan, mark, mark))

		ips := userIPs[uid]
		sort.Strings(ips)
		for _, ip := range ips {
			ip = strings.TrimSpace(ip)
			if ip != "" {
				sb.WriteString(fmt.Sprintf("iptables -t mangle -A %s -s %s -j MARK --set-mark %d\n", mangleChainName, ip, mark))
			}
		}
	}

	return sb.String()
}

// computeSignature returns a hash of the current configuration and container IP state.
func computeSignature(userLimits map[int64]int, userIPs map[int64][]string) string {
	var parts []string
	var userIDs []int64
	for uid := range userLimits {
		userIDs = append(userIDs, uid)
	}
	sort.Slice(userIDs, func(i, j int) bool { return userIDs[i] < userIDs[j] })

	for _, uid := range userIDs {
		limit := userLimits[uid]
		ips := append([]string{}, userIPs[uid]...)
		sort.Strings(ips)
		parts = append(parts, fmt.Sprintf("%d:%d:%s", uid, limit, strings.Join(ips, ",")))
	}
	h := sha256.Sum256([]byte(strings.Join(parts, ";")))
	return hex.EncodeToString(h[:])
}

// Reconcile inspects active database users and running Docker containers, syncing tc classes and iptables marks.
func (m *Manager) Reconcile(ctx context.Context, store *db.Store, workspacesRoot string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if store == nil {
		return nil
	}

	if m.wanInterface == "" {
		detected, err := m.DetectWANInterface(ctx)
		if err != nil {
			log.Printf("qos: failed to detect WAN interface: %v", err)
			detected = "eth0"
		}
		m.wanInterface = detected
	}

	users, err := store.ListUsers(ctx)
	if err != nil {
		return fmt.Errorf("list users: %w", err)
	}

	userLimits := make(map[int64]int)
	hasAnyLimit := false
	for _, u := range users {
		if u.Role == db.RoleAdmin {
			userLimits[u.ID] = 0
			continue
		}
		userLimits[u.ID] = u.BandwidthLimitMbps
		if u.BandwidthLimitMbps > 0 {
			hasAnyLimit = true
		}
	}

	userIPs := make(map[int64][]string)

	// Only scan docker containers if at least one user has an active bandwidth limit
	if hasAnyLimit {
		apps, err := store.ListApps(ctx)
		if err != nil {
			log.Printf("qos: list apps: %v", err)
		} else {
			appOwners := make(map[string]int64)
			appNames := make(map[string]int64)
			for _, app := range apps {
				if app.OwnerID > 0 {
					appOwners[strings.ToLower(strings.TrimSpace(app.ID))] = app.OwnerID
					cleanName := strings.ToLower(strings.TrimSpace(app.Name))
					appNames[cleanName] = app.OwnerID
					appNames[strings.ReplaceAll(cleanName, " ", "-")] = app.OwnerID
					appNames[strings.ReplaceAll(cleanName, " ", "_")] = app.OwnerID
				}
			}

			cli, cerr := dockerapi.Client()
			if cerr == nil && cli != nil {
				containers, lerr := cli.ContainerList(ctx, types.ContainerListOptions{All: false})
				if lerr != nil {
					log.Printf("qos: container list: %v", lerr)
				} else {
					for _, c := range containers {
						matchedUID := resolveContainerOwner(c, appOwners, appNames)
						if matchedUID > 0 && userLimits[matchedUID] > 0 && c.NetworkSettings != nil {
							for _, netSetting := range c.NetworkSettings.Networks {
								if netSetting != nil {
									ip := strings.TrimSpace(netSetting.IPAddress)
									if ip != "" && ip != "127.0.0.1" {
										userIPs[matchedUID] = append(userIPs[matchedUID], ip)
									}
								}
							}
						}
					}
					// Deduplicate container IPs per user
					for uid, ips := range userIPs {
						seen := make(map[string]bool)
						var deduped []string
						for _, ip := range ips {
							if !seen[ip] {
								seen[ip] = true
								deduped = append(deduped, ip)
							}
						}
						userIPs[uid] = deduped
					}
				}
			}
		}
	}

	// Compute state fingerprint to avoid redundant host executions
	sig := computeSignature(userLimits, userIPs)
	if sig == m.lastSignature {
		return nil
	}

	// Identify stale marks from previous runs
	var staleMarks []uint32
	for uid, oldMark := range m.activeMarks {
		if userLimits[uid] <= 0 {
			staleMarks = append(staleMarks, oldMark)
		}
	}

	script := BuildSyncScript(m.wanInterface, userLimits, userIPs, staleMarks)
	if _, err := m.runner(ctx, script); err != nil {
		return fmt.Errorf("apply qos sync: %w", err)
	}

	// Update cached active marks and signature
	m.lastSignature = sig
	m.activeMarks = make(map[int64]uint32)
	for uid, limit := range userLimits {
		if limit > 0 {
			m.activeMarks[uid] = UserMark(uid)
		}
	}

	log.Printf("qos: synced bandwidth limits for %d users (wan=%s)", len(m.activeMarks), m.wanInterface)
	return nil
}

// resolveContainerOwner inspects labels and names of a container to match it to an app owner user ID.
func resolveContainerOwner(c types.Container, appOwners, appNames map[string]int64) int64 {
	// 1. Check working directory label (including git checkouts under .nextdeploy/repo)
	if workDir, ok := c.Labels["com.docker.compose.project.working_dir"]; ok && workDir != "" {
		normalized := filepath.ToSlash(filepath.Clean(workDir))
		base := strings.ToLower(filepath.Base(normalized))
		if base == "repo" {
			parentDir := filepath.Dir(normalized)
			if filepath.Base(parentDir) == ".nextdeploy" {
				base = strings.ToLower(filepath.Base(filepath.Dir(parentDir)))
			}
		}
		if uid, found := appOwners[base]; found {
			return uid
		}
		if uid, found := appNames[base]; found {
			return uid
		}
		for appID, uid := range appOwners {
			if strings.Contains("/"+normalized+"/", "/"+appID+"/") {
				return uid
			}
		}
	}

	// 2. Check compose project label (strip blue/green slots and normalize)
	if proj, ok := c.Labels["com.docker.compose.project"]; ok && proj != "" {
		projLower := strings.ToLower(strings.TrimSpace(proj))
		if uid, found := appOwners[projLower]; found {
			return uid
		}
		if uid, found := appNames[projLower]; found {
			return uid
		}
		cleanProj := strings.TrimSuffix(strings.TrimSuffix(projLower, "-blue"), "-green")
		cleanProj = strings.TrimSuffix(strings.TrimSuffix(cleanProj, "_blue"), "_green")
		if uid, found := appOwners[cleanProj]; found {
			return uid
		}
		if uid, found := appNames[cleanProj]; found {
			return uid
		}
		if uid, found := appNames[strings.ReplaceAll(cleanProj, "-", " ")]; found {
			return uid
		}
		if uid, found := appNames[strings.ReplaceAll(cleanProj, "_", " ")]; found {
			return uid
		}
	}

	// 3. Check container name prefixes
	for _, name := range c.Names {
		clean := strings.ToLower(strings.TrimPrefix(name, "/"))
		for appID, uid := range appOwners {
			if strings.HasPrefix(clean, appID+"-") ||
				strings.HasPrefix(clean, appID+"_") ||
				strings.HasPrefix(clean, strings.ReplaceAll(appID, "-", "_")+"_") {
				return uid
			}
		}
		for appName, uid := range appNames {
			if strings.HasPrefix(clean, appName+"-") ||
				strings.HasPrefix(clean, appName+"_") ||
				strings.HasPrefix(clean, strings.ReplaceAll(appName, "-", "_")+"_") ||
				strings.HasPrefix(clean, strings.ReplaceAll(appName, " ", "-")+"-") ||
				strings.HasPrefix(clean, strings.ReplaceAll(appName, " ", "_")+"_") {
				return uid
			}
		}
	}

	return 0
}

// StartReconcileLoop runs periodic QoS reconciliation in the background.
func (m *Manager) StartReconcileLoop(ctx context.Context, store *db.Store, workspacesRoot string, interval time.Duration) {
	if interval < 5*time.Second {
		interval = 30 * time.Second
	}
	// Run initial reconciliation
	_ = m.Reconcile(ctx, store, workspacesRoot)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = m.Reconcile(ctx, store, workspacesRoot)
		}
	}
}
