package edgeintegration

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"nd/internal/edge"
	"panel/tests/edge-integration/panelbridge"

	"github.com/miekg/dns"
)

const testShieldIP = "107.189.6.222"

// statefulMockCaddyRunner implements edge.CaddyRunner, validating generated
// Caddyfile markers, balanced braces, snippet/PEM integrity, and duplicate-host
// safety, while recording active rendered hosts and per-panel Caddyfile contents.
type statefulMockCaddyRunner struct {
	paths edge.Paths

	mu                 sync.Mutex
	validateCalls      int
	reloadCalls        int
	activeHostsByPanel map[string][]string
	hostToUpstream     map[string]string
	panelCaddyContents map[string]string
	panelPEMContents   map[string]string
}

func newStatefulMockCaddyRunner(paths edge.Paths) *statefulMockCaddyRunner {
	return &statefulMockCaddyRunner{
		paths:              paths,
		activeHostsByPanel: make(map[string][]string),
		hostToUpstream:     make(map[string]string),
		panelCaddyContents: make(map[string]string),
		panelPEMContents:   make(map[string]string),
	}
}

func (m *statefulMockCaddyRunner) Validate(_ context.Context, configPath string) (string, error) {
	m.mu.Lock()
	m.validateCalls++
	m.mu.Unlock()

	raw, err := os.ReadFile(configPath)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", configPath, err)
	}
	content := string(raw)
	if !strings.Contains(content, edge.ManagedStartMarker) || !strings.Contains(content, edge.ManagedEndMarker) {
		return "missing managed markers", fmt.Errorf("Caddyfile missing nd-edge managed markers")
	}
	if !strings.Contains(content, "admin ") {
		return "missing admin directive", fmt.Errorf("Caddyfile missing admin directive")
	}
	if err := checkBalancedBraces(content); err != nil {
		return err.Error(), err
	}

	seenHosts := make(map[string]string)
	hasCommon := false
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if !strings.HasPrefix(trim, "import ") {
			continue
		}
		target := strings.TrimSpace(strings.TrimPrefix(trim, "import "))
		matches, gErr := filepath.Glob(filepath.FromSlash(target))
		if gErr != nil {
			return gErr.Error(), fmt.Errorf("invalid import glob %q: %w", target, gErr)
		}
		if len(matches) == 0 {
			return "glob matched 0 files", fmt.Errorf("import %q matched 0 files", target)
		}
		for _, mf := range matches {
			fb, rErr := os.ReadFile(mf)
			if rErr != nil {
				return rErr.Error(), rErr
			}
			txt := string(fb)
			if err := checkBalancedBraces(txt); err != nil {
				return err.Error(), fmt.Errorf("%s: %w", mf, err)
			}
			if strings.Contains(txt, "(nd_common)") {
				hasCommon = true
			}
			if strings.Contains(txt, "tls_trust_pool file ") {
				for _, l := range strings.Split(txt, "\n") {
					lt := strings.TrimSpace(l)
					if strings.HasPrefix(lt, "tls_trust_pool file ") {
						pemPath := strings.TrimSpace(strings.TrimPrefix(lt, "tls_trust_pool file "))
						pemBytes, pErr := os.ReadFile(filepath.FromSlash(pemPath))
						if pErr != nil {
							return pErr.Error(), fmt.Errorf("missing origin pem %s: %w", pemPath, pErr)
						}
						if err := edge.ValidateCAPEM(string(pemBytes)); err != nil {
							return err.Error(), fmt.Errorf("invalid origin pem %s: %w", pemPath, err)
						}
					}
				}
			}
			if strings.HasSuffix(mf, ".caddy") && filepath.Base(mf) != edge.PlaceholderFileName {
				hosts := parseSiteHosts(txt)
				for _, h := range hosts {
					if prev, dup := seenHosts[h]; dup {
						return "duplicate site block", fmt.Errorf("duplicate host %q in %s and %s", h, prev, mf)
					}
					seenHosts[h] = mf
				}
			}
		}
	}
	if !hasCommon {
		return "missing nd_common snippet", fmt.Errorf("imported common.caddy missing (nd_common)")
	}
	return "Valid configuration", nil
}

func (m *statefulMockCaddyRunner) Reload(_ context.Context, _ string, _ string) (time.Duration, error) {
	panelsDir, err := edge.ReadDirLink(m.paths.PanelsLink)
	if err != nil {
		return 0, fmt.Errorf("read panels link: %w", err)
	}
	originsDir, _ := edge.ReadDirLink(m.paths.OriginsLink)

	entries, err := os.ReadDir(panelsDir)
	if err != nil {
		return 0, fmt.Errorf("readdir panels %s: %w", panelsDir, err)
	}

	active := make(map[string][]string)
	upstreams := make(map[string]string)
	caddyFiles := make(map[string]string)
	pemFiles := make(map[string]string)

	for _, e := range entries {
		if e.IsDir() || e.Name() == edge.PlaceholderFileName || !strings.HasSuffix(e.Name(), ".caddy") {
			continue
		}
		panelID := strings.TrimSuffix(e.Name(), ".caddy")
		b, rErr := os.ReadFile(filepath.Join(panelsDir, e.Name()))
		if rErr != nil {
			return 0, rErr
		}
		txt := string(b)
		caddyFiles[panelID] = txt
		hosts := parseSiteHosts(txt)
		sort.Strings(hosts)
		active[panelID] = hosts

		up := parseFirstReverseProxyUpstream(txt)
		for _, h := range hosts {
			upstreams[h] = up
		}
	}

	if originsDir != "" {
		if oEntries, oErr := os.ReadDir(originsDir); oErr == nil {
			for _, e := range oEntries {
				if e.IsDir() || !strings.HasSuffix(e.Name(), ".pem") {
					continue
				}
				panelID := strings.TrimSuffix(e.Name(), ".pem")
				if b, rErr := os.ReadFile(filepath.Join(originsDir, e.Name())); rErr == nil {
					pemFiles[panelID] = string(b)
				}
			}
		}
	}

	m.mu.Lock()
	m.reloadCalls++
	m.activeHostsByPanel = active
	m.hostToUpstream = upstreams
	m.panelCaddyContents = caddyFiles
	m.panelPEMContents = pemFiles
	m.mu.Unlock()
	return 5 * time.Millisecond, nil
}

func (m *statefulMockCaddyRunner) CheckAdmin(_ context.Context, _ string) error {
	return nil
}

func (m *statefulMockCaddyRunner) snapshot() (reloads int, hostsByPanel map[string][]string, upstreams map[string]string, caddyFiles map[string]string, pemFiles map[string]string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	hostsByPanel = make(map[string][]string, len(m.activeHostsByPanel))
	for k, v := range m.activeHostsByPanel {
		hostsByPanel[k] = append([]string(nil), v...)
	}
	upstreams = make(map[string]string, len(m.hostToUpstream))
	for k, v := range m.hostToUpstream {
		upstreams[k] = v
	}
	caddyFiles = make(map[string]string, len(m.panelCaddyContents))
	for k, v := range m.panelCaddyContents {
		caddyFiles[k] = v
	}
	pemFiles = make(map[string]string, len(m.panelPEMContents))
	for k, v := range m.panelPEMContents {
		pemFiles[k] = v
	}
	return m.reloadCalls, hostsByPanel, upstreams, caddyFiles, pemFiles
}

func checkBalancedBraces(src string) error {
	depth := 0
	for i := 0; i < len(src); i++ {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth < 0 {
				return fmt.Errorf("unmatched closing brace")
			}
		}
	}
	if depth != 0 {
		return fmt.Errorf("unbalanced braces (depth=%d)", depth)
	}
	return nil
}

func parseSiteHosts(caddySrc string) []string {
	var hosts []string
	seen := make(map[string]struct{})
	sc := bufio.NewScanner(strings.NewReader(caddySrc))
	depth := 0
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if depth == 0 && strings.HasSuffix(line, "{") {
			hdr := strings.TrimSpace(strings.TrimSuffix(line, "{"))
			if !strings.HasPrefix(hdr, "(") && hdr != "" {
				for _, part := range strings.Split(hdr, ",") {
					h := strings.ToLower(strings.TrimSpace(part))
					if h != "" {
						if _, ok := seen[h]; !ok {
							seen[h] = struct{}{}
							hosts = append(hosts, h)
						}
					}
				}
			}
		}
		depth += strings.Count(line, "{") - strings.Count(line, "}")
		if depth < 0 {
			depth = 0
		}
	}
	return hosts
}

func parseFirstReverseProxyUpstream(caddySrc string) string {
	sc := bufio.NewScanner(strings.NewReader(caddySrc))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "reverse_proxy ") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				return fields[1]
			}
		}
	}
	return ""
}

type simDNSResolver struct {
	mu      sync.RWMutex
	records map[string][]string
}

func newSimDNSResolver() *simDNSResolver {
	return &simDNSResolver{records: make(map[string][]string)}
}

func (r *simDNSResolver) setA(host string, ips ...string) {
	r.mu.Lock()
	r.records[dns.Fqdn(strings.ToLower(host))] = append([]string(nil), ips...)
	r.mu.Unlock()
}

func (r *simDNSResolver) Exchange(_ context.Context, _ string, qname string, qtype uint16) (*dns.Msg, error) {
	fqdn := dns.Fqdn(strings.ToLower(qname))
	m := new(dns.Msg)
	r.mu.RLock()
	ips, ok := r.records[fqdn]
	r.mu.RUnlock()

	if !ok {
		m.Rcode = dns.RcodeNameError
		return m, nil
	}
	m.Rcode = dns.RcodeSuccess
	if qtype == dns.TypeA {
		for _, ipStr := range ips {
			b := parseIPv4(ipStr)
			m.Answer = append(m.Answer, &dns.A{
				Hdr: dns.RR_Header{Name: fqdn, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 60},
				A:   b[:],
			})
		}
	}
	return m, nil
}

func parseIPv4(s string) [4]byte {
	var out [4]byte
	var a, b, c, d int
	_, _ = fmt.Sscanf(s, "%d.%d.%d.%d", &a, &b, &c, &d)
	out[0], out[1], out[2], out[3] = byte(a), byte(b), byte(c), byte(d)
	return out
}

type simSystemdRunner struct{}

func (simSystemdRunner) IsActive(_ context.Context, _ string) (bool, string, error) {
	return true, "active", nil
}
func (simSystemdRunner) Start(_ context.Context, _ string) error { return nil }
func (simSystemdRunner) CaddyUser(_ context.Context) string      { return "caddy" }
func (simSystemdRunner) JournalCaddyErrors(_ context.Context, _ string) ([]byte, error) {
	return nil, nil
}

func generateTestCAPEM(t *testing.T, cn string) string {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: cn},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("CreateCertificate: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

func TestMultiPanelEndToEndSimulation(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 11, 11, 5, 0, 0, 0, time.UTC)

	caA1 := generateTestCAPEM(t, "origin-panel-a-v1")
	caB1 := generateTestCAPEM(t, "origin-panel-b-v1")

	panelA := panelbridge.NewSimPanel(t, "panel-a", testShieldIP, "204.136.10.51", caA1)
	panelB := panelbridge.NewSimPanel(t, "panel-b", testShieldIP, "204.136.10.52", caB1)

	paths := edge.NewPaths(t.TempDir())
	dnsRes := newSimDNSResolver()
	for _, h := range []string{
		"app-a.demo.net",
		"www.app-a.demo.net",
		"api-a.demo.net",
		"app-b.demo.net",
		"shared.demo.net",
		"e6.demo.net",
	} {
		dnsRes.setA(h, testShieldIP)
	}
	for i := 1; i <= 10; i++ {
		dnsRes.setA(fmt.Sprintf("mass-%d.demo.net", i), testShieldIP)
	}

	mockCaddy := newStatefulMockCaddyRunner(paths)
	systemd := simSystemdRunner{}
	cfIPs := edge.NewCFIPStore()
	dnsGate := edge.NewDNSGate(edge.DNSGateOptions{
		Resolver:  dnsRes,
		CFIPs:     cfIPs,
		CachePath: paths.DNSCacheFile,
	})
	applier := edge.NewApplier(paths, mockCaddy)
	probe := edge.NewCertProbe(paths.CaddyCertsDir, systemd)
	reconciler := edge.NewReconciler(paths, dnsGate, applier, probe, systemd, cfIPs)

	edgeID := "edge-sim-0001"
	clientA := edge.NewPanelClient(edge.PanelConfig{
		URL:           panelA.Server.URL,
		Token:         panelA.Token,
		PanelCASHA256: panelA.CASHA256,
	}, nil, nil)
	clientB := edge.NewPanelClient(edge.PanelConfig{
		URL:           panelB.Server.URL,
		Token:         panelB.Token,
		PanelCASHA256: panelB.CASHA256,
	}, nil, nil)

	// 1. Register both panels via real POST /api/v1/edge/register.
	regA, err := clientA.Register(ctx, edge.RegisterRequest{
		EdgeID:     edgeID,
		EdgeVersion: "1.2.0",
		CaddyVer:   "v2.8.4",
		Hostname:   "shield-sim",
		PublicIPv4: []string{testShieldIP},
	})
	if err != nil {
		t.Fatalf("Register Panel A failed: %v", err)
	}
	regB, err := clientB.Register(ctx, edge.RegisterRequest{
		EdgeID:     edgeID,
		EdgeVersion: "1.2.0",
		CaddyVer:   "v2.8.4",
		Hostname:   "shield-sim",
		PublicIPv4: []string{testShieldIP},
	})
	if err != nil {
		t.Fatalf("Register Panel B failed: %v", err)
	}
	if regA.PanelID == "" || regB.PanelID == "" || regA.PanelID == regB.PanelID {
		t.Fatalf("expected distinct non-empty panel IDs, got A=%q B=%q", regA.PanelID, regB.PanelID)
	}

	// Verify 409 token_in_use when another edge_id tries to register with Panel A's token.
	_, conflictErr := clientA.Register(ctx, edge.RegisterRequest{
		EdgeID:     "rogue-edge-id",
		EdgeVersion: "1.2.0",
		CaddyVer:   "v2.8.4",
		PublicIPv4: []string{testShieldIP},
	})
	if conflictErr == nil || !strings.Contains(conflictErr.Error(), "409") || !strings.Contains(conflictErr.Error(), "token_in_use") {
		t.Fatalf("expected 409 token_in_use for second edge_id, got %v", conflictErr)
	}

	cfg := &edge.Config{
		EdgeID:        edgeID,
		ShieldIPs:     []string{testShieldIP},
		PerPanelQuota: 2000,
		Panels: []edge.PanelConfig{
			{
				PanelID:         regA.PanelID,
				PanelName:       regA.PanelName,
				ShieldNodeID:    regA.ShieldNodeID,
				URL:             panelA.Server.URL,
				Token:           panelA.Token,
				PanelCASHA256:   panelA.CASHA256,
				PollIntervalSec: regA.PollIntervalSec,
				RegisteredAt:    now.Add(-2 * time.Hour),
			},
			{
				PanelID:         regB.PanelID,
				PanelName:       regB.PanelName,
				ShieldNodeID:    regB.ShieldNodeID,
				URL:             panelB.Server.URL,
				Token:           panelB.Token,
				PanelCASHA256:   panelB.CASHA256,
				PollIntervalSec: regB.PollIntervalSec,
				RegisteredAt:    now.Add(-1 * time.Hour),
			},
		},
	}
	if err := edge.SaveConfig(paths.ConfigFile, cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	states := map[string]*edge.PanelState{
		regA.PanelID: edge.NewPanelState(regA.PanelID, regA.PanelName),
		regB.PanelID: edge.NewPanelState(regB.PanelID, regB.PanelName),
	}
	claims := edge.NewClaimsStore()
	meta := &edge.ApplyMeta{}

	clients := map[string]*edge.PanelClient{
		regA.PanelID: clientA,
		regB.PanelID: clientB,
	}

	syncAndReport := func(ts time.Time, force bool) edge.ReconcileOutcome {
		t.Helper()
		for _, p := range cfg.Panels {
			pOut := edge.PollPanelOnce(ctx, clients[p.PanelID], states[p.PanelID], cfg.ShieldHostname, force, ts)
			if pOut.Err != nil {
				t.Fatalf("PollPanelOnce(%s) failed: %v", p.PanelName, pOut.Err)
			}
		}
		out, rErr := reconciler.ReconcileOnce(ctx, cfg, states, claims, meta, force, false, ts)
		if rErr != nil {
			t.Fatalf("ReconcileOnce failed: %v", rErr)
		}
		for _, p := range cfg.Panels {
			doc := edge.BuildStatusForPanel(cfg, states[p.PanelID], meta, "1.2.0", "v2.8.4", out.CaddyRunning, out.CaddyStatusError, 60, 0, paths)
			if pErr := clients[p.PanelID].PostStatus(ctx, doc); pErr != nil {
				t.Fatalf("PostStatus(%s) failed: %v", p.PanelName, pErr)
			}
		}
		return out
	}

	// 2. Seed hybrid Direct + Shielded domains on both Panels.
	if _, err := panelA.CreateAppDomain(ctx, "direct-a.demo.net", false, false); err != nil {
		t.Fatalf("create direct-a: %v", err)
	}
	if _, err := panelA.CreateAppDomain(ctx, "app-a.demo.net", true, true); err != nil {
		t.Fatalf("create app-a: %v", err)
	}
	sharedDomA, err := panelA.CreateAppDomain(ctx, "shared.demo.net", true, false)
	if err != nil {
		t.Fatalf("create shared on A: %v", err)
	}
	if _, err := panelA.SaveShieldedProxyRoute(ctx, "api-a.demo.net", "http://127.0.0.1:9000", true); err != nil {
		t.Fatalf("create api-a proxy: %v", err)
	}

	if _, err := panelB.CreateAppDomain(ctx, "direct-b.demo.net", false, false); err != nil {
		t.Fatalf("create direct-b: %v", err)
	}
	if _, err := panelB.CreateAppDomain(ctx, "app-b.demo.net", true, false); err != nil {
		t.Fatalf("create app-b: %v", err)
	}

	out := syncAndReport(now, false)
	if !out.Applied {
		t.Fatalf("expected initial apply=true, got %+v", out)
	}

	reloads, hostsByPanel, upstreams, caddyFiles, _ := mockCaddy.snapshot()
	if reloads != 1 {
		t.Fatalf("expected 1 reload, got %d", reloads)
	}
	wantA := []string{"api-a.demo.net", "app-a.demo.net", "shared.demo.net", "www.app-a.demo.net"}
	if strings.Join(hostsByPanel[regA.PanelID], ",") != strings.Join(wantA, ",") {
		t.Fatalf("panel A hosts mismatch: got %v want %v (hostStates=%+v caddy=%q)", hostsByPanel[regA.PanelID], wantA, states[regA.PanelID].HostStates, caddyFiles[regA.PanelID])
	}
	if strings.Join(hostsByPanel[regB.PanelID], ",") != "app-b.demo.net" {
		t.Fatalf("panel B hosts mismatch: got %v", hostsByPanel[regB.PanelID])
	}
	if upstreams["app-a.demo.net"] != "https://204.136.10.51:443" || upstreams["app-b.demo.net"] != "https://204.136.10.52:443" {
		t.Fatalf("unexpected upstreams: %+v", upstreams)
	}
	if _, directInCaddy := upstreams["direct-a.demo.net"]; directInCaddy {
		t.Fatalf("direct-a.demo.net must NOT be rendered on Shield")
	}
	if _, directInCaddy := upstreams["direct-b.demo.net"]; directInCaddy {
		t.Fatalf("direct-b.demo.net must NOT be rendered on Shield")
	}

	// Second poll with no changes must return 304 Not Modified and trigger ZERO reloads.
	out = syncAndReport(now.Add(30*time.Second), false)
	if !out.NoOp {
		t.Fatalf("expected NoOp on unchanged 304 poll, got %+v", out)
	}
	reloadsAfterNoOp, _, _, _, _ := mockCaddy.snapshot()
	if reloadsAfterNoOp != 1 {
		t.Fatalf("expected zero reloads on 304, got %d", reloadsAfterNoOp)
	}

	// 3. Cross-panel conflict: Panel B claims shared.demo.net (already owned by Panel A).
	if _, err := panelB.CreateAppDomain(ctx, "shared.demo.net", true, false); err != nil {
		t.Fatalf("create conflicting shared on B: %v", err)
	}
	syncAndReport(now.Add(1*time.Minute), false)

	_, hostsByPanel, upstreams, _, _ = mockCaddy.snapshot()
	if upstreams["shared.demo.net"] != "https://204.136.10.51:443" {
		t.Fatalf("shared.demo.net must continue routing to Panel A, got %q", upstreams["shared.demo.net"])
	}
	for _, h := range hostsByPanel[regB.PanelID] {
		if h == "shared.demo.net" {
			t.Fatalf("shared.demo.net must NOT be rendered in Panel B's Caddyfile while claimed by A")
		}
	}
	statusesB, err := panelB.ListDomainStatuses(ctx)
	if err != nil {
		t.Fatalf("ListDomainStatuses B: %v", err)
	}
	foundConflict := false
	for _, st := range statusesB {
		if st.Host == "shared.demo.net" && st.State == edge.StateConflict && strings.Contains(st.Detail, regA.PanelName) {
			foundConflict = true
		}
	}
	if !foundConflict {
		t.Fatalf("expected conflict status for shared.demo.net pointing to %q in Panel B DB, got %+v", regA.PanelName, statusesB)
	}

	// 4. Domain move Panel A -> Panel B: delete shared.demo.net from Panel A.
	if err := panelA.DeleteAppDomain(ctx, sharedDomA); err != nil {
		t.Fatalf("DeleteAppDomain shared on A: %v", err)
	}
	out = syncAndReport(now.Add(2*time.Minute), false)
	if !out.Applied {
		t.Fatalf("expected apply on domain move A -> B, got %+v", out)
	}
	_, hostsByPanel, upstreams, _, _ = mockCaddy.snapshot()
	if upstreams["shared.demo.net"] != "https://204.136.10.52:443" {
		t.Fatalf("expected shared.demo.net to move to Panel B upstream, got %q", upstreams["shared.demo.net"])
	}
	foundInB := false
	for _, h := range hostsByPanel[regB.PanelID] {
		if h == "shared.demo.net" {
			foundInB = true
		}
	}
	if !foundInB {
		t.Fatalf("expected shared.demo.net in Panel B hosts, got %v", hostsByPanel[regB.PanelID])
	}
	statusesB, _ = panelB.ListDomainStatuses(ctx)
	for _, st := range statusesB {
		if st.Host == "shared.demo.net" && st.State == edge.StateConflict {
			t.Fatalf("expected conflict cleared after domain move, still got %+v", st)
		}
	}

	// 5. E-ORIGIN-6 pending_origin_apply via real AppDomainEdit handler.
	e6ID, err := panelA.CreateAppDomain(ctx, "e6.demo.net", false, false)
	if err != nil {
		t.Fatalf("create direct e6 domain: %v", err)
	}
	statusCode, err := panelA.EditDomainAttachShield(e6ID, "e6.demo.net")
	if err != nil || statusCode != 200 {
		t.Fatalf("EditDomainAttachShield failed: status=%d err=%v", statusCode, err)
	}

	// While PendingOriginApply is true, e6.demo.net must NOT be in Panel A routes/Caddyfile.
	syncAndReport(now.Add(3*time.Minute), false)
	_, _, upstreams, _, _ = mockCaddy.snapshot()
	if _, exists := upstreams["e6.demo.net"]; exists {
		t.Fatalf("e6.demo.net must be omitted while pending_origin_apply=true")
	}

	// Once origin apply clears pending_origin_apply, e6.demo.net is exposed and rendered.
	if err := panelA.ClearPendingOriginApply(ctx); err != nil {
		t.Fatalf("ClearPendingOriginApply: %v", err)
	}
	out = syncAndReport(now.Add(4*time.Minute), false)
	if !out.Applied {
		t.Fatalf("expected apply after ClearPendingOriginApply, got %+v", out)
	}
	_, _, upstreams, _, _ = mockCaddy.snapshot()
	if upstreams["e6.demo.net"] != "https://204.136.10.51:443" {
		t.Fatalf("expected e6.demo.net rendered after pending_origin_apply cleared, got %+v", upstreams)
	}

	// 6. Origin CA rotation on Panel A: updates Panel A PEM while Panel B PEM stays untouched.
	_, _, _, _, pemsBefore := mockCaddy.snapshot()
	caA2 := generateTestCAPEM(t, "origin-panel-a-v2-rotated")
	panelA.SetOriginCA(caA2)
	out = syncAndReport(now.Add(5*time.Minute), false)
	if !out.Applied {
		t.Fatalf("expected apply on origin CA rotation, got %+v", out)
	}
	_, _, _, _, pemsAfter := mockCaddy.snapshot()
	if pemsAfter[regA.PanelID] != caA2 {
		t.Fatalf("expected Panel A PEM updated to rotated CA")
	}
	if pemsAfter[regB.PanelID] != pemsBefore[regB.PanelID] {
		t.Fatalf("expected Panel B PEM unchanged during Panel A CA rotation")
	}

	// 7. Panel B removal leaves Panel A's Caddyfile byte-for-byte untouched.
	_, _, _, caddyBeforeRemoval, _ := mockCaddy.snapshot()
	panelACaddyBefore := caddyBeforeRemoval[regA.PanelID]

	if err := clientB.Unregister(ctx, edge.UnregisterRequest{EdgeID: edgeID, Reason: "test_remove"}); err != nil {
		t.Fatalf("Unregister Panel B: %v", err)
	}
	nodeBAfterUnreg, _ := panelB.GetShieldNode(ctx)
	if nodeBAfterUnreg == nil || nodeBAfterUnreg.Status != "unregistered" {
		t.Fatalf("expected Panel B node status=unregistered, got %+v", nodeBAfterUnreg)
	}
	cfg.RemovePanel(regB.PanelID)
	delete(states, regB.PanelID)
	claims.ReleasePanelClaims(regB.PanelID)
	if err := edge.SaveConfig(paths.ConfigFile, cfg); err != nil {
		t.Fatalf("SaveConfig after remove B: %v", err)
	}

	out = syncAndReport(now.Add(6*time.Minute), false)
	if !out.Applied {
		t.Fatalf("expected apply after Panel B removal, got %+v", out)
	}
	_, hostsAfterRemoval, _, caddyAfterRemoval, pemAfterRemoval := mockCaddy.snapshot()
	if _, exists := caddyAfterRemoval[regB.PanelID]; exists {
		t.Fatalf("expected Panel B caddyfile removed")
	}
	if _, exists := pemAfterRemoval[regB.PanelID]; exists {
		t.Fatalf("expected Panel B pem removed")
	}
	if caddyAfterRemoval[regA.PanelID] != panelACaddyBefore {
		t.Fatalf("expected Panel A Caddyfile byte-for-byte untouched after removing Panel B")
	}
	if len(hostsAfterRemoval[regA.PanelID]) == 0 {
		t.Fatalf("expected Panel A hosts intact")
	}

	// 8. Mass-deletion guard on Panel A: add 10 domains, then delete 8 (>50% of 14 and >5).
	var massIDs []int64
	for i := 1; i <= 10; i++ {
		id, err := panelA.CreateAppDomain(ctx, fmt.Sprintf("mass-%d.demo.net", i), true, false)
		if err != nil {
			t.Fatalf("create mass-%d: %v", i, err)
		}
		massIDs = append(massIDs, id)
	}
	syncAndReport(now.Add(7*time.Minute), false)

	for i := 0; i < 8; i++ {
		if err := panelA.DeleteAppDomain(ctx, massIDs[i]); err != nil {
			t.Fatalf("delete mass-%d: %v", i+1, err)
		}
	}

	// Poll 1: held by mass-deletion guard -> all 14 remain rendered, held_mass_delete reported.
	poll1 := edge.PollPanelOnce(ctx, clientA, states[regA.PanelID], cfg.ShieldHostname, false, now.Add(8*time.Minute))
	if !poll1.HeldMassDelete {
		t.Fatalf("expected poll 1 HeldMassDelete=true, got %+v", poll1)
	}
	_, _ = reconciler.ReconcileOnce(ctx, cfg, states, claims, meta, false, false, now.Add(8*time.Minute))
	docHeld := edge.BuildStatusForPanel(cfg, states[regA.PanelID], meta, "1.2.0", "v2.8.4", true, "", 120, 0, paths)
	_ = clientA.PostStatus(ctx, docHeld)

	_, hostsDuringHold, _, _, _ := mockCaddy.snapshot()
	if len(hostsDuringHold[regA.PanelID]) < 14 {
		t.Fatalf("expected mass-deletion guard to keep all hosts rendered, got %d", len(hostsDuringHold[regA.PanelID]))
	}
	statusesA, _ := panelA.ListDomainStatuses(ctx)
	heldCount := 0
	for _, st := range statusesA {
		if st.State == edge.StateHeldMassDelete {
			heldCount++
		}
	}
	if heldCount != 8 {
		t.Fatalf("expected 8 held_mass_delete statuses in Panel A DB, got %d (%+v)", heldCount, statusesA)
	}

	// Poll 2 (+1m): still held.
	poll2 := edge.PollPanelOnce(ctx, clientA, states[regA.PanelID], cfg.ShieldHostname, false, now.Add(9*time.Minute))
	if !poll2.HeldMassDelete {
		t.Fatalf("expected poll 2 HeldMassDelete=true, got %+v", poll2)
	}
	// Poll 3 (+4m from first seen): 3rd consecutive poll spanning >=3m -> released!
	poll3 := edge.PollPanelOnce(ctx, clientA, states[regA.PanelID], cfg.ShieldHostname, false, now.Add(12*time.Minute))
	if poll3.HeldMassDelete || !poll3.SnapshotUpdated {
		t.Fatalf("expected poll 3 to release mass-deletion guard, got %+v", poll3)
	}
	out = syncAndReport(now.Add(12*time.Minute), false)
	if !out.Applied {
		t.Fatalf("expected apply after mass-deletion release")
	}
	_, hostsAfterRelease, _, _, _ := mockCaddy.snapshot()
	for _, h := range hostsAfterRelease[regA.PanelID] {
		if h == "mass-1.demo.net" || h == "mass-6.demo.net" {
			t.Fatalf("deleted host %q should no longer be rendered after guard release", h)
		}
	}

	// 9. Reboot recovery from disk while Panel A is completely offline.
	if err := edge.SavePanelState(paths.StateFile(regA.PanelID), states[regA.PanelID]); err != nil {
		t.Fatalf("SavePanelState: %v", err)
	}
	if err := edge.SaveClaims(paths.ClaimsFile, claims); err != nil {
		t.Fatalf("SaveClaims: %v", err)
	}
	if err := edge.SaveApplyMeta(paths.ApplyMeta, meta); err != nil {
		t.Fatalf("SaveApplyMeta: %v", err)
	}

	panelA.SetOffline(true)

	rebootCfg, err := edge.LoadConfig(paths.ConfigFile)
	if err != nil {
		t.Fatalf("reboot LoadConfig: %v", err)
	}
	rebootPanelsDir, _ := edge.ReadDirLink(paths.PanelsLink)
	rebootClaims, err := edge.LoadClaims(paths.ClaimsFile, rebootPanelsDir, now.Add(15*time.Minute))
	if err != nil {
		t.Fatalf("reboot LoadClaims: %v", err)
	}
	rebootMeta, err := edge.LoadApplyMeta(paths.ApplyMeta)
	if err != nil {
		t.Fatalf("reboot LoadApplyMeta: %v", err)
	}

	// 9a. Empty-state cold boot check (E-BOOT-1/2): even if state/<panel>.json were missing,
	// Reconciler must skip without touching rendered Caddy files on disk.
	coldStates := map[string]*edge.PanelState{
		regA.PanelID: edge.NewPanelState(regA.PanelID, regA.PanelName),
	}
	coldCaddy := newStatefulMockCaddyRunner(paths)
	coldRec := edge.NewReconciler(paths, edge.NewDNSGate(edge.DNSGateOptions{Resolver: dnsRes, CFIPs: cfIPs, CachePath: paths.DNSCacheFile}), edge.NewApplier(paths, coldCaddy), probe, systemd, cfIPs)
	coldOut, err := coldRec.ReconcileOnce(ctx, rebootCfg, coldStates, rebootClaims, rebootMeta, false, false, now.Add(15*time.Minute))
	if err != nil || !coldOut.SkippedEmptyBoot {
		t.Fatalf("expected SkippedEmptyBoot=true on cold boot with offline panel, got out=%+v err=%v", coldOut, err)
	}

	// 9b. Normal reboot loading persisted PanelState from disk with Panel A offline.
	rebootStA, err := edge.LoadPanelState(paths.StateFile(regA.PanelID), regA.PanelID, regA.PanelName, now.Add(15*time.Minute))
	if err != nil {
		t.Fatalf("reboot LoadPanelState: %v", err)
	}
	pollOff := edge.PollPanelOnce(ctx, clientA, rebootStA, rebootCfg.ShieldHostname, false, now.Add(15*time.Minute))
	if pollOff.Err == nil || rebootStA.SyncState != edge.SyncStateStale {
		t.Fatalf("expected stale sync state when panel offline, got poll=%+v syncState=%q", pollOff, rebootStA.SyncState)
	}
	rebootStates := map[string]*edge.PanelState{regA.PanelID: rebootStA}
	rebootOut, err := coldRec.ReconcileOnce(ctx, rebootCfg, rebootStates, rebootClaims, rebootMeta, false, false, now.Add(15*time.Minute))
	if err != nil || !rebootOut.NoOp {
		t.Fatalf("expected NoOp preserving persisted routes during offline reboot, got out=%+v err=%v", rebootOut, err)
	}
	reloadsCold, _, _, _, _ := coldCaddy.snapshot()
	if reloadsCold != 0 {
		t.Fatalf("expected 0 Caddy reloads on clean reboot with unchanged state, got %d", reloadsCold)
	}
	if _, err := os.Stat(filepath.Join(rebootPanelsDir, regA.PanelID+".caddy")); err != nil {
		t.Fatalf("expected %s.caddy to remain intact after offline reboot: %v", regA.PanelID, err)
	}
}
