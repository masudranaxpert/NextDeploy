package edge

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/miekg/dns"
)

type fakeSystemdRunner struct {
	active       bool
	statusDetail string
	startErr     error
	startCalls   int
	journalData  []byte
}

func (f *fakeSystemdRunner) IsActive(_ context.Context, _ string) (bool, string, error) {
	return f.active, f.statusDetail, nil
}

func (f *fakeSystemdRunner) Start(_ context.Context, _ string) error {
	f.startCalls++
	if f.startErr == nil {
		f.active = true
	}
	return f.startErr
}

func (f *fakeSystemdRunner) CaddyUser(_ context.Context) string { return "caddy" }

func (f *fakeSystemdRunner) JournalCaddyErrors(_ context.Context, _ string) ([]byte, error) {
	return f.journalData, nil
}

func TestReconcilerStartupPreservationAndMultiPanelLifecycle(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 11, 11, 4, 0, 0, 0, time.UTC)
	paths := NewPaths(t.TempDir())

	// Seed an existing gen-1 on disk to simulate reboot (E-BOOT-1 / E-BOOT-2).
	gen1Panels := paths.PanelGenDir(1)
	gen1Origins := paths.OriginGenDir(1)
	_ = os.MkdirAll(gen1Panels, 0o750)
	_ = os.MkdirAll(gen1Origins, 0o750)
	_ = WriteFileAtomic(filepath.Join(gen1Panels, PlaceholderFileName), []byte(RenderPlaceholderCaddy()), 0o640)
	_ = WriteFileAtomic(filepath.Join(gen1Panels, "panel-a.caddy"), []byte("boot.example.com {\n\timport nd_common\n}\n"), 0o640)
	_ = SwapDirLinkAtomic(paths.PanelsLink, gen1Panels)
	_ = SwapDirLinkAtomic(paths.OriginsLink, gen1Origins)

	res := newFakeDNSResolver()
	for _, h := range []string{"a.example.com", "shared.example.com", "b.example.com", "boot.example.com"} {
		res.setBoth(h, dns.TypeA, msgA(h, "107.189.6.222"), nil)
	}

	caddy := &fakeCaddyRunner{}
	systemd := &fakeSystemdRunner{active: true}
	cfIPs := NewCFIPStore()
	dnsGate := NewDNSGate(DNSGateOptions{Resolver: res, CFIPs: cfIPs, CachePath: paths.DNSCacheFile})
	applier := NewApplier(paths, caddy)
	probe := NewCertProbe(paths.CaddyCertsDir, systemd)
	rec := NewReconciler(paths, dnsGate, applier, probe, systemd, cfIPs)

	cfg := &Config{
		EdgeID:        "edge-1",
		ShieldIPs:     []string{"107.189.6.222"},
		PerPanelQuota: 2000,
		Panels: []PanelConfig{
			{PanelID: "panel-a", PanelName: "vps-a", RegisteredAt: now.Add(-2 * time.Hour)},
			{PanelID: "panel-b", PanelName: "vps-b", RegisteredAt: now.Add(-1 * time.Hour)},
		},
	}
	states := map[string]*PanelState{
		"panel-a": NewPanelState("panel-a", "vps-a"),
		"panel-b": NewPanelState("panel-b", "vps-b"),
	}
	claims := NewClaimsStore()
	meta := &ApplyMeta{CurrentGen: 1}

	// E-BOOT-1/2: all panels have LastGood==nil at boot -> must NOT wipe existing gen-1 files.
	out, err := rec.ReconcileOnce(ctx, cfg, states, claims, meta, false, false, now)
	if err != nil || !out.SkippedEmptyBoot || caddy.reloadCalls != 0 {
		t.Fatalf("expected empty-boot skip preserving Caddy files, got out=%+v err=%v reloads=%d", out, err, caddy.reloadCalls)
	}

	// E-API-7, E-MP-1: panel-a gets valid + invalid hosts; panel-b shares shared.example.com.
	caPEM := generateTestCertPEM(t, true)
	states["panel-a"].LastGoodETag = "etag-a1"
	states["panel-a"].LastGood = &RoutesResponse{
		APIVersion: 1,
		PanelID:    "panel-a",
		ETag:       "etag-a1",
		Origin:     OriginInfo{IPs: []string{"204.136.10.51"}, Port: 443, CAPEM: caPEM, CAStatus: CAStatusOK},
		Routes: []EdgeRoute{
			{ID: "r1", Hosts: []string{"a.example.com", "shared.example.com", "*.wild.com", "bad.internal"}},
		},
	}
	states["panel-b"].LastGoodETag = "etag-b1"
	states["panel-b"].LastGood = &RoutesResponse{
		APIVersion: 1,
		PanelID:    "panel-b",
		ETag:       "etag-b1",
		Origin:     OriginInfo{IPs: []string{"204.136.10.52"}, Port: 443, CAPEM: caPEM, CAStatus: CAStatusOK},
		Routes: []EdgeRoute{
			{ID: "r1", Hosts: []string{"b.example.com", "shared.example.com"}},
		},
	}

	out, err = rec.ReconcileOnce(ctx, cfg, states, claims, meta, false, false, now.Add(time.Minute))
	if err != nil || !out.Applied {
		t.Fatalf("reconcile failed: out=%+v err=%v", out, err)
	}

	// Verify E-API-7: invalid hosts excluded with exact states while valid hosts applied.
	if states["panel-a"].HostStates["*.wild.com"].State != StateUnsupportedWildcard {
		t.Fatalf("expected wildcard state, got %+v", states["panel-a"].HostStates["*.wild.com"])
	}
	if states["panel-a"].HostStates["bad.internal"].State != StateExcludedInternal {
		t.Fatalf("expected excluded_internal state, got %+v", states["panel-a"].HostStates["bad.internal"])
	}

	// Verify E-MP-1: panel-a owns shared.example.com; panel-b has conflict.
	if len(states["panel-b"].Conflicts) != 1 || states["panel-b"].Conflicts[0].Host != "shared.example.com" || states["panel-b"].Conflicts[0].OwnerPanel != "vps-a" {
		t.Fatalf("expected conflict on panel-b for shared.example.com, got %+v", states["panel-b"].Conflicts)
	}

	// Verify Status scoping (§3.6 / §8.1): panel-b status never leaks panel-a's a.example.com.
	statusB := BuildStatusForPanel(cfg, states["panel-b"], meta, "0.1.0", "v2.8.4", out.CaddyRunning, out.CaddyStatusError, 60, 0, paths)
	for _, h := range statusB.Hosts {
		if h.Host == "a.example.com" {
			t.Fatalf("panel-b status leaked panel-a host a.example.com!")
		}
	}

	// E-ORIGIN-2: Origin CA rotation updates PEM file on disk.
	caPEM2 := generateTestCertPEM(t, true)
	states["panel-a"].LastGoodETag = "etag-a2"
	states["panel-a"].LastGood.ETag = "etag-a2"
	states["panel-a"].LastGood.Origin.CAPEM = caPEM2
	out, err = rec.ReconcileOnce(ctx, cfg, states, claims, meta, false, false, now.Add(2*time.Minute))
	if err != nil || !out.Applied {
		t.Fatalf("expected CA rotation apply, got out=%+v err=%v", out, err)
	}
	originsDir, _ := ReadDirLink(paths.OriginsLink)
	pemDisk, _ := os.ReadFile(filepath.Join(originsDir, "panel-a.pem"))
	if string(pemDisk) != caPEM2 {
		t.Fatalf("expected rotated CA PEM on disk")
	}

	// E-MP-2 & E-MP-3: Remove panel-a -> panel-a.caddy/pem removed, panel-b untouched and inherits shared.example.com.
	cfg.RemovePanel("panel-a")
	delete(states, "panel-a")
	claims.ReleasePanelClaims("panel-a")
	out, err = rec.ReconcileOnce(ctx, cfg, states, claims, meta, false, false, now.Add(3*time.Minute))
	if err != nil || !out.Applied {
		t.Fatalf("expected apply after panel-a removal, got out=%+v err=%v", out, err)
	}
	panelsDir, _ := ReadDirLink(paths.PanelsLink)
	if _, statErr := os.Stat(filepath.Join(panelsDir, "panel-a.caddy")); !os.IsNotExist(statErr) {
		t.Fatalf("expected panel-a.caddy removed")
	}
	bCaddy, _ := os.ReadFile(filepath.Join(panelsDir, "panel-b.caddy"))
	if !strings.Contains(string(bCaddy), "shared.example.com") || !strings.Contains(string(bCaddy), "b.example.com") {
		t.Fatalf("expected panel-b.caddy to now include shared.example.com and b.example.com, got:\n%s", string(bCaddy))
	}
}

func TestReconcilerThrottleQuotaAndBoot4(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 11, 11, 4, 0, 0, 0, time.UTC)
	paths := NewPaths(t.TempDir())

	// Quota = 65, dns-gate=off so no DNS queries needed.
	caddy := &fakeCaddyRunner{}
	systemd := &fakeSystemdRunner{
		active:       false,
		statusDetail: "caddy.service: bind :80 address already in use (nginx)",
		startErr:     errors.New("start failed"),
	}
	dnsGate := NewDNSGate(DNSGateOptions{Off: true})
	rec := NewReconciler(paths, dnsGate, NewApplier(paths, caddy), NewCertProbe(paths.CaddyCertsDir, systemd), systemd, NewCFIPStore())

	cfg := &Config{
		EdgeID:        "edge-1",
		ShieldIPs:     []string{"107.189.6.222"},
		PerPanelQuota: 65,
		DNSGateOff:    true,
		Panels: []PanelConfig{
			{PanelID: "panel-flood", PanelName: "flood", RegisteredAt: now},
		},
	}

	var hosts []string
	for i := 0; i < 70; i++ {
		hosts = append(hosts, fmt.Sprintf("host%03d.example.com", i))
	}
	st := NewPanelState("panel-flood", "flood")
	st.LastGoodETag = "etag-flood"
	st.LastGood = &RoutesResponse{
		APIVersion: 1,
		PanelID:    "panel-flood",
		ETag:       "etag-flood",
		Origin:     OriginInfo{IPs: []string{"204.136.10.51"}, Port: 443, CAStatus: CAStatusMissing},
		Routes:     []EdgeRoute{{ID: "r1", Hosts: hosts}},
	}
	states := map[string]*PanelState{"panel-flood": st}
	claims := NewClaimsStore()
	meta := &ApplyMeta{}

	out, err := rec.ReconcileOnce(ctx, cfg, states, claims, meta, false, false, now)
	if err != nil || !out.Applied {
		t.Fatalf("reconcile failed: out=%+v err=%v", out, err)
	}
	// E-BOOT-4: caddy_running:false + systemd status detail reported.
	if out.CaddyRunning || !strings.Contains(out.CaddyStatusError, "nginx") {
		t.Fatalf("expected caddy_running:false with nginx status detail, got running=%v detail=%q", out.CaddyRunning, out.CaddyStatusError)
	}

	// Count states: 50 admitted (issuing), 15 queued_rate_limit (51..65), 5 excluded_quota (66..70).
	var issuingCnt, queuedCnt, quotaCnt int
	for _, hs := range st.HostStates {
		switch hs.State {
		case StateIssuing:
			issuingCnt++
		case StateQueuedRateLimit:
			queuedCnt++
		case StateExcludedQuota:
			quotaCnt++
		}
	}
	if issuingCnt != 50 || queuedCnt != 15 || quotaCnt != 5 {
		t.Fatalf("unexpected counts: issuing=%d queued=%d quota=%d", issuingCnt, queuedCnt, quotaCnt)
	}
}

func TestCertProbeAndJournalClassification(t *testing.T) {
	now := time.Now().UTC()
	// Generate a CA and leaf cert for probe.example.com expiring in 5 days (<14d warning!).
	caKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	caTmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Test ACME Root"},
		NotBefore:             now.Add(-24 * time.Hour),
		NotAfter:              now.Add(365 * 24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	caDER, _ := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	caCert, _ := x509.ParseCertificate(caDER)

	leafKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	leafTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "probe.example.com"},
		DNSNames:     []string{"probe.example.com"},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.Add(5 * 24 * time.Hour), // < 14 days -> cert_renew_overdue
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	leafDER, _ := x509.CreateCertificate(rand.Reader, leafTmpl, caCert, &leafKey.PublicKey, caKey)

	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
		Certificates: []tls.Certificate{{
			Certificate: [][]byte{leafDER, caDER},
			PrivateKey:  leafKey,
		}},
	})
	if err != nil {
		t.Fatalf("tls.Listen: %v", err)
	}
	defer ln.Close()

	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				if tc, ok := conn.(*tls.Conn); ok {
					_ = tc.Handshake()
				}
			}(c)
		}
	}()

	pool := x509.NewCertPool()
	pool.AddCert(caCert)

	systemd := &fakeSystemdRunner{
		journalData: []byte(`{"MESSAGE":"{\"logger\":\"tls.obtain\",\"identifier\":\"missing.example.com\",\"error\":\"403 urn:ietf:params:acme:error:unauthorized\"}"}` + "\n"),
	}
	probe := &CertProbe{
		TargetAddr: ln.Addr().String(),
		RootCAs:    pool,
		Systemd:    systemd,
		DialTimout: 2 * time.Second,
	}

	st := NewPanelState("p1", "vps-1")
	hs := probe.UpdateHostStatusFromProbe(context.Background(), st, "probe.example.com", false, "", false, now)
	if hs.State != StateActive || !strings.Contains(hs.Detail, WarningCertRenewOverdue) || hs.Issuer != "Test ACME Root" {
		t.Fatalf("expected active with cert_renew_overdue warning, got %+v", hs)
	}

	// Host failing verification transitions issuing -> cert_error after 15m with journal classification.
	hs1 := probe.UpdateHostStatusFromProbe(context.Background(), st, "missing.example.com", false, "", false, now)
	if hs1.State != StateIssuing {
		t.Fatalf("expected issuing initially, got %+v", hs1)
	}
	hs2 := probe.UpdateHostStatusFromProbe(context.Background(), st, "missing.example.com", false, "", false, now.Add(16*time.Minute))
	if hs2.State != StateCertError || !strings.Contains(hs2.Detail, "unauthorized/403") {
		t.Fatalf("expected cert_error with unauthorized/403 detail, got %+v", hs2)
	}

	// Verify HasValidStoredCert in Caddy storage dir.
	certsDir := t.TempDir()
	certFile := filepath.Join(certsDir, "certificates", "acme-v02.api.letsencrypt.org-directory", "probe.example.com", "probe.example.com.crt")
	_ = os.MkdirAll(filepath.Dir(certFile), 0o755)
	_ = os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER}), 0o644)
	probe.CertsDir = certsDir
	if !probe.HasValidStoredCert("probe.example.com", now) {
		t.Fatalf("expected HasValidStoredCert=true for valid stored leaf cert")
	}
}

func TestDaemonEndToEndDebouncedSyncAndScopedStatus(t *testing.T) {
	paths := NewPaths(t.TempDir())
	var mu sync.Mutex
	var postedStatus []StatusRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/edge/routes":
			_ = json.NewEncoder(w).Encode(RoutesResponse{
				APIVersion: 1,
				PanelID:    "panel-e2e",
				ETag:       "etag-e2e",
				Origin:     OriginInfo{IPs: []string{"204.136.10.51"}, Port: 443, CAStatus: CAStatusMissing},
				Routes:     []EdgeRoute{{ID: "r1", Kind: KindAppDomain, Hosts: []string{"e2e-107-189-6-222.sslip.io"}}},
				Node:       NodeInfo{ID: 1, IP: "107.189.6.222"},
			})
		case "/api/v1/edge/status":
			var st StatusRequest
			if err := json.NewDecoder(r.Body).Decode(&st); err == nil {
				mu.Lock()
				postedStatus = append(postedStatus, st)
				mu.Unlock()
			}
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	cfg := &Config{
		EdgeID:    "edge-e2e",
		ShieldIPs: []string{"107.189.6.222"},
		Panels: []PanelConfig{{
			PanelID:         "panel-e2e",
			PanelName:       "e2e",
			ShieldNodeID:    1,
			URL:             srv.URL,
			Token:           "shld_tok",
			PollIntervalSec: 1,
			RegisteredAt:    time.Now().UTC(),
			InsecureHTTP:    true,
		}},
	}
	if err := SaveConfig(paths.ConfigFile, cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	caddy := &fakeCaddyRunner{}
	systemd := &fakeSystemdRunner{active: true}
	d, lock, err := NewDaemon(DaemonOptions{
		Paths:            paths,
		EdgeVersion:      "0.1.0",
		CaddyVersion:     "v2.8.4",
		Caddy:            caddy,
		Systemd:          systemd,
		DNSResolver:      newFakeDNSResolver(),
		DebounceDelay:    20 * time.Millisecond,
		MinApplyInterval: 20 * time.Millisecond,
		StatusInterval:   50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewDaemon: %v", err)
	}
	defer lock.Release()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	doneCh := make(chan error, 1)
	go func() {
		doneCh <- d.Run(ctx)
	}()

	deadline := time.Now().Add(1500 * time.Millisecond)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(postedStatus)
		var hasHost bool
		if n > 0 {
			for _, h := range postedStatus[n-1].Hosts {
				if h.Host == "e2e-107-189-6-222.sslip.io" {
					hasHost = true
				}
			}
		}
		mu.Unlock()
		if n > 0 && hasHost && caddy.reloadCalls > 0 {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}

	cancel()
	if runErr := <-doneCh; runErr != nil {
		t.Fatalf("Daemon.Run returned error: %v", runErr)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(postedStatus) == 0 || caddy.reloadCalls == 0 {
		t.Fatalf("expected daemon poll+reconcile+status post; statusPosts=%d reloads=%d", len(postedStatus), caddy.reloadCalls)
	}
}

func TestReconcilerDNS4ApexThenWWWAndInvalidCAPreservesClaim(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 11, 11, 4, 0, 0, 0, time.UTC)
	paths := NewPaths(t.TempDir())

	res := newFakeDNSResolver()
	res.setBoth("apex.example.com", dns.TypeA, msgA("apex.example.com", "107.189.6.222"), nil)
	res.setBoth("www.apex.example.com", dns.TypeA, msgNXDOMAIN(), nil)
	res.setBoth("www.apex.example.com", dns.TypeAAAA, msgNXDOMAIN(), nil)

	caddy := &fakeCaddyRunner{}
	systemd := &fakeSystemdRunner{active: true}
	cfIPs := NewCFIPStore()
	dnsGate := NewDNSGate(DNSGateOptions{Resolver: res, CFIPs: cfIPs, CachePath: paths.DNSCacheFile})
	rec := NewReconciler(paths, dnsGate, NewApplier(paths, caddy), NewCertProbe(paths.CaddyCertsDir, systemd), systemd, cfIPs)

	caPEM := generateTestCertPEM(t, true)
	cfg := &Config{
		EdgeID:        "edge-1",
		ShieldIPs:     []string{"107.189.6.222"},
		PerPanelQuota: 2000,
		Panels: []PanelConfig{
			{PanelID: "panel-a", PanelName: "vps-a", RegisteredAt: now.Add(-2 * time.Hour)},
			{PanelID: "panel-b", PanelName: "vps-b", RegisteredAt: now.Add(-1 * time.Hour)},
		},
	}
	states := map[string]*PanelState{
		"panel-a": NewPanelState("panel-a", "vps-a"),
		"panel-b": NewPanelState("panel-b", "vps-b"),
	}
	states["panel-a"].LastGoodETag = "etag-a1"
	states["panel-a"].LastGood = &RoutesResponse{
		APIVersion: 1,
		PanelID:    "panel-a",
		ETag:       "etag-a1",
		Origin:     OriginInfo{IPs: []string{"204.136.10.51"}, Port: 443, CAPEM: caPEM, CAStatus: CAStatusOK},
		Routes:     []EdgeRoute{{ID: "r1", Hosts: []string{"apex.example.com", "www.apex.example.com"}}},
	}
	claims := NewClaimsStore()
	meta := &ApplyMeta{}

	// E-DNS-4 step 1: apex rendered while www is pending_dns.
	out, err := rec.ReconcileOnce(ctx, cfg, states, claims, meta, false, false, now)
	if err != nil || !out.Applied {
		t.Fatalf("reconcile step 1 failed: out=%+v err=%v", out, err)
	}
	if states["panel-a"].HostStates["apex.example.com"].State != StateIssuing {
		t.Fatalf("expected apex issuing, got %+v", states["panel-a"].HostStates["apex.example.com"])
	}
	if states["panel-a"].HostStates["www.apex.example.com"].State != StatePendingDNS {
		t.Fatalf("expected www pending_dns, got %+v", states["panel-a"].HostStates["www.apex.example.com"])
	}
	panelsDir, _ := ReadDirLink(paths.PanelsLink)
	aCaddy, _ := os.ReadFile(filepath.Join(panelsDir, "panel-a.caddy"))
	if !strings.Contains(string(aCaddy), "apex.example.com") || strings.Contains(string(aCaddy), "www.apex.example.com") {
		t.Fatalf("expected only apex in Caddyfile initially, got:\n%s", string(aCaddy))
	}

	// E-DNS-4 step 2: www DNS resolves -> www added in next reconcile.
	res.setBoth("www.apex.example.com", dns.TypeA, msgA("www.apex.example.com", "107.189.6.222"), nil)
	out, err = rec.ReconcileOnce(ctx, cfg, states, claims, meta, false, false, now.Add(2*time.Minute))
	if err != nil || !out.Applied {
		t.Fatalf("reconcile step 2 failed: out=%+v err=%v", out, err)
	}
	panelsDir, _ = ReadDirLink(paths.PanelsLink)
	aCaddy, _ = os.ReadFile(filepath.Join(panelsDir, "panel-a.caddy"))
	if !strings.Contains(string(aCaddy), "www.apex.example.com") {
		t.Fatalf("expected www.apex.example.com added once DNS resolved, got:\n%s", string(aCaddy))
	}

	// Defect #3 regression: panel-a gets an invalid CA PEM while panel-b asks for apex.example.com.
	// panel-a preserves its disk file AND its claim, so panel-b must get conflict instead of stealing apex.example.com.
	states["panel-a"].LastGood.Origin.CAPEM = "CORRUPT-PEM"
	states["panel-b"].LastGoodETag = "etag-b1"
	states["panel-b"].LastGood = &RoutesResponse{
		APIVersion: 1,
		PanelID:    "panel-b",
		ETag:       "etag-b1",
		Origin:     OriginInfo{IPs: []string{"204.136.10.52"}, Port: 443, CAPEM: caPEM, CAStatus: CAStatusOK},
		Routes:     []EdgeRoute{{ID: "r1", Hosts: []string{"apex.example.com"}}},
	}
	_, err = rec.ReconcileOnce(ctx, cfg, states, claims, meta, false, false, now.Add(3*time.Minute))
	if err != nil {
		t.Fatalf("reconcile with invalid CA on panel-a failed: %v", err)
	}
	if len(states["panel-b"].Conflicts) != 1 || states["panel-b"].Conflicts[0].Host != "apex.example.com" {
		t.Fatalf("expected panel-b to get conflict for apex.example.com while panel-a preserves disk, got %+v", states["panel-b"].Conflicts)
	}

	// Defect #4 regression: held_mass_delete surfaced on hosts slated for removal in PendingSnapshot.
	states["panel-a"].LastGood.Origin.CAPEM = caPEM
	states["panel-a"].PendingSnapshot = &RoutesResponse{
		APIVersion: 1,
		PanelID:    "panel-a",
		ETag:       "etag-empty",
		Routes:     nil,
	}
	states["panel-a"].PendingETag = "etag-empty"
	states["panel-a"].PendingCount = 1
	_, err = rec.ReconcileOnce(ctx, cfg, states, claims, meta, false, false, now.Add(4*time.Minute))
	if err != nil {
		t.Fatalf("reconcile with pending mass delete failed: %v", err)
	}
	if states["panel-a"].HostStates["apex.example.com"].State != StateHeldMassDelete {
		t.Fatalf("expected held_mass_delete state on apex.example.com, got %+v", states["panel-a"].HostStates["apex.example.com"])
	}
}
