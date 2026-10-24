package qos

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/docker/docker/api/types"
)

func TestUserMark(t *testing.T) {
	if m0 := UserMark(0); m0 != 0 {
		t.Errorf("expected 0 for user 0, got %d", m0)
	}
	if mNeg := UserMark(-5); mNeg != 0 {
		t.Errorf("expected 0 for negative user, got %d", mNeg)
	}

	m1 := UserMark(1)
	m2 := UserMark(2)
	if m1 <= 1000 || m2 <= 1000 {
		t.Errorf("expected marks > 1000, got m1=%d, m2=%d", m1, m2)
	}
	if m1 == m2 {
		t.Errorf("expected distinct marks for user 1 and user 2, got %d", m1)
	}
	if m1 != 1001 {
		t.Errorf("expected UserMark(1) == 1001, got %d", m1)
	}
	if m2 != 1002 {
		t.Errorf("expected UserMark(2) == 1002, got %d", m2)
	}
}

func TestBuildSyncScript(t *testing.T) {
	userLimits := map[int64]int{
		1: 50,  // 50 Mbps
		2: 100, // 100 Mbps
		3: 0,   // Unlimited
	}
	userIPs := map[int64][]string{
		1: {"172.18.0.5", "172.18.0.6"},
		2: {"172.18.0.10"},
		3: {"172.18.0.20"},
	}
	staleMarks := []uint32{1099}

	script := BuildSyncScript("eth0", userLimits, userIPs, staleMarks)

	// Verify root HTB
	if !strings.Contains(script, "tc qdisc replace dev eth0 root handle 1: htb default 999") && !strings.Contains(script, "tc qdisc add dev eth0 root handle 1: htb default 999") {
		t.Errorf("script missing root HTB qdisc")
	}
	// Verify iptables chain setup
	if !strings.Contains(script, "iptables -t mangle -N ND_BANDWIDTH") {
		t.Errorf("script missing ND_BANDWIDTH chain creation")
	}
	if !strings.Contains(script, "iptables -t mangle -C PREROUTING -j ND_BANDWIDTH") {
		t.Errorf("script missing PREROUTING hook")
	}
	if !strings.Contains(script, "iptables -t mangle -F ND_BANDWIDTH") {
		t.Errorf("script missing ND_BANDWIDTH flush")
	}

	// Verify User 1 (mark 1001, 50mbit)
	if !strings.Contains(script, "tc class replace dev eth0 parent 1:1 classid 1:1001 htb rate 50mbit ceil 50mbit") {
		t.Errorf("script missing User 1 tc class")
	}
	if !strings.Contains(script, "classid 1:1001") {
		t.Errorf("script missing User 1 filter")
	}
	if !strings.Contains(script, "iptables -t mangle -A ND_BANDWIDTH -s 172.18.0.5 -j MARK --set-mark 1001") {
		t.Errorf("script missing User 1 IP 172.18.0.5 mark")
	}
	if !strings.Contains(script, "iptables -t mangle -A ND_BANDWIDTH -s 172.18.0.6 -j MARK --set-mark 1001") {
		t.Errorf("script missing User 1 IP 172.18.0.6 mark")
	}

	// Verify User 2 (mark 1002, 100mbit)
	if !strings.Contains(script, "tc class replace dev eth0 parent 1:1 classid 1:1002 htb rate 100mbit ceil 100mbit") {
		t.Errorf("script missing User 2 tc class")
	}
	if !strings.Contains(script, "iptables -t mangle -A ND_BANDWIDTH -s 172.18.0.10 -j MARK --set-mark 1002") {
		t.Errorf("script missing User 2 IP mark")
	}

	// Verify User 3 (unlimited 0) has NO class or mark
	if strings.Contains(script, "classid 1:1003") {
		t.Errorf("unlimited user 3 should not have a tc class")
	}
	if strings.Contains(script, "172.18.0.20") {
		t.Errorf("unlimited user 3 container IP should not be marked")
	}

	// Verify stale mark cleanup
	if !strings.Contains(script, "tc class del dev eth0 classid 1:1099") {
		t.Errorf("script missing stale mark 1099 cleanup")
	}
}

func TestResolveContainerOwner(t *testing.T) {
	appOwners := map[string]int64{
		"my-app":   42,
		"blog-app": 55,
		"app-777":  88,
	}
	appNames := map[string]int64{
		"my app":   42,
		"blog app": 55,
		"my store": 99,
	}

	// 1. Working dir matching (standard)
	c1 := types.Container{
		Labels: map[string]string{
			"com.docker.compose.project.working_dir": "/data/workspaces/my-app",
		},
	}
	if uid := resolveContainerOwner(c1, appOwners, appNames); uid != 42 {
		t.Errorf("expected working dir to resolve to user 42, got %d", uid)
	}

	// 2. Git repo working dir matching (/data/workspaces/<app-id>/.nextdeploy/repo)
	cGit := types.Container{
		Labels: map[string]string{
			"com.docker.compose.project.working_dir": "/data/workspaces/app-777/.nextdeploy/repo",
		},
	}
	if uid := resolveContainerOwner(cGit, appOwners, appNames); uid != 88 {
		t.Errorf("expected git repo working dir to resolve to user 88, got %d", uid)
	}

	// 3. Compose project label matching (with slot suffix and name with space/hyphen)
	c2 := types.Container{
		Labels: map[string]string{
			"com.docker.compose.project": "my-store-green",
		},
	}
	if uid := resolveContainerOwner(c2, appOwners, appNames); uid != 99 {
		t.Errorf("expected slot project to resolve to user 99, got %d", uid)
	}

	// 4. Container name prefix matching
	c3 := types.Container{
		Names: []string{"/my-app-web-1"},
	}
	if uid := resolveContainerOwner(c3, appOwners, appNames); uid != 42 {
		t.Errorf("expected container name to resolve to user 42, got %d", uid)
	}

	// 5. Unmatched container
	c4 := types.Container{
		Names: []string{"/unrelated-service"},
	}
	if uid := resolveContainerOwner(c4, appOwners, appNames); uid != 0 {
		t.Errorf("expected unrelated container to resolve to 0, got %d", uid)
	}
}

func TestDetectWANInterface(t *testing.T) {
	m := NewManager("")

	// Case 1: Standard 'default via 192.168.1.1 dev eth0 proto dhcp'
	m.SetRunner(func(ctx context.Context, script string) (string, error) {
		if strings.Contains(script, "ip route show default") {
			return "eth0", nil
		}
		return "", nil
	})
	iface, err := m.DetectWANInterface(context.Background())
	if err != nil || iface != "eth0" {
		t.Errorf("expected eth0, got %s (err: %v)", iface, err)
	}

	// Case 2: PTP / Cloud route 'default dev ens3 metric 100'
	m.SetRunner(func(ctx context.Context, script string) (string, error) {
		if strings.Contains(script, "ip route show default") {
			return "ens3", nil
		}
		return "", nil
	})
	iface, err = m.DetectWANInterface(context.Background())
	if err != nil || iface != "ens3" {
		t.Errorf("expected ens3, got %s (err: %v)", iface, err)
	}
}

func TestManager_StateCaching(t *testing.T) {
	m := NewManager("eth0")

	var runCount int32
	m.SetRunner(func(ctx context.Context, script string) (string, error) {
		atomic.AddInt32(&runCount, 1)
		return "", nil
	})

	userLimits := map[int64]int{1: 50}
	userIPs := map[int64][]string{1: {"172.18.0.5"}}

	sig1 := computeSignature(userLimits, userIPs)
	sig2 := computeSignature(userLimits, userIPs)
	if sig1 != sig2 {
		t.Errorf("expected identical signatures for identical state")
	}

	// Changing IP changes signature
	userIPs2 := map[int64][]string{1: {"172.18.0.6"}}
	if sig1 == computeSignature(userLimits, userIPs2) {
		t.Errorf("expected different signature when IP changes")
	}

	// Changing limit changes signature
	userLimits2 := map[int64]int{1: 100}
	if sig1 == computeSignature(userLimits2, userIPs) {
		t.Errorf("expected different signature when limit changes")
	}
}
