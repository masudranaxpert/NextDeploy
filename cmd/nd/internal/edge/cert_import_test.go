package edge

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func generateTestLeafCertAndKey(t *testing.T, hosts []string, notBefore, notAfter time.Time) (certPEM, keyPEM []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate ecdsa key: %v", err)
	}
	cn := "localhost"
	if len(hosts) > 0 {
		cn = hosts[0]
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: cn},
		DNSNames:     hosts,
		NotBefore:    notBefore,
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create leaf cert: %v", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	return certPEM, keyPEM
}

func TestImportCaddyCertsAndManualExpiryFallback(t *testing.T) {
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	root := t.TempDir()
	caddyCertsDir := filepath.Join(root, "var", "lib", "caddy", ".local", "share", "caddy")
	caddyEtcDir := filepath.Join(root, "etc", "caddy")
	targetCertsDir := filepath.Join(root, "etc", "angie", "nd-edge", "certs")

	// 1. Valid cert in Caddy ACME dir (60 days remaining -> usable manual cert).
	certApp, keyApp := generateTestLeafCertAndKey(t, []string{"app.example.com"}, now.Add(-24*time.Hour), now.Add(60*24*time.Hour))
	appDir := filepath.Join(caddyCertsDir, "certificates", "acme-v02.api.letsencrypt.org-directory", "app.example.com")
	_ = os.MkdirAll(appDir, 0o755)
	_ = os.WriteFile(filepath.Join(appDir, "app.example.com.crt"), certApp, 0o644)
	_ = os.WriteFile(filepath.Join(appDir, "app.example.com.key"), keyApp, 0o600)

	// 2. Near-expiry cert in /etc/caddy (15 days remaining -> imported, but RenderPanel falls back to ACME!).
	certNear, keyNear := generateTestLeafCertAndKey(t, []string{"renew.example.com"}, now.Add(-60*24*time.Hour), now.Add(15*24*time.Hour))
	_ = os.MkdirAll(filepath.Join(caddyEtcDir, "custom-certs"), 0o755)
	_ = os.WriteFile(filepath.Join(caddyEtcDir, "custom-certs", "renew.example.com.crt"), certNear, 0o644)
	_ = os.WriteFile(filepath.Join(caddyEtcDir, "custom-certs", "renew.example.com.key"), keyNear, 0o600)

	// 3. Already-expired cert -> must NOT be imported.
	certExp, keyExp := generateTestLeafCertAndKey(t, []string{"expired.example.com"}, now.Add(-90*24*time.Hour), now.Add(-time.Hour))
	expDir := filepath.Join(caddyCertsDir, "certificates", "acme-v02.api.letsencrypt.org-directory", "expired.example.com")
	_ = os.MkdirAll(expDir, 0o755)
	_ = os.WriteFile(filepath.Join(expDir, "expired.example.com.crt"), certExp, 0o644)
	_ = os.WriteFile(filepath.Join(expDir, "expired.example.com.key"), keyExp, 0o600)

	imported, err := ImportCaddyCertsToDir(caddyCertsDir, caddyEtcDir, targetCertsDir, now)
	if err != nil {
		t.Fatalf("ImportCaddyCertsToDir: %v", err)
	}
	if len(imported) != 2 {
		t.Fatalf("expected 2 non-expired certs imported, got %d: %+v", len(imported), imported)
	}
	if _, err := os.Stat(filepath.Join(targetCertsDir, "expired.example.com.crt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expired cert should not be imported, err=%v", err)
	}
	if runtime.GOOS != "windows" {
		st, err := os.Stat(filepath.Join(targetCertsDir, "app.example.com.key"))
		if err != nil {
			t.Fatalf("stat imported key: %v", err)
		}
		if st.Mode().Perm() != 0o600 {
			t.Fatalf("expected imported key perm 0600, got %o", st.Mode().Perm())
		}
	}

	// LoadUsableManualCert checks 30d renewal window.
	if _, _, _, ok := LoadUsableManualCert(targetCertsDir, "app.example.com", now, DefaultManualCertRenewWindow); !ok {
		t.Fatalf("expected app.example.com manual cert to be usable at 60d left")
	}
	if _, _, _, ok := LoadUsableManualCert(targetCertsDir, "renew.example.com", now, DefaultManualCertRenewWindow); ok {
		t.Fatalf("expected renew.example.com manual cert (15d left) to fall back to ACME")
	}

	// RenderPanel mixes manual cert for app.example.com and ACME for renew.example.com!
	eng := NewAngieEngine(NewAngieEnginePaths(root), nil)
	eng.SetManualCertClock(func() time.Time { return now }, DefaultManualCertRenewWindow)

	conf, err := eng.RenderPanel(RenderPanelInput{
		PanelID:    "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee",
		PanelName:  "vps-1",
		ETag:       "etag-manual",
		OriginIPs:  []netip.Addr{netip.MustParseAddr("204.136.10.51")},
		OriginPort: 443,
		CAStatus:   CAStatusMissing,
		Routes: []EdgeRoute{
			{
				ID:    "dom_1",
				Kind:  KindAppDomain,
				Hosts: []string{"app.example.com", "renew.example.com"},
			},
		},
	})
	if err != nil {
		t.Fatalf("RenderPanel: %v", err)
	}
	wantManual := "ssl_certificate " + filepath.ToSlash(filepath.Join(targetCertsDir, "app.example.com.crt")) + ";"
	if !strings.Contains(conf, wantManual) {
		t.Fatalf("expected manual ssl_certificate directive %q in:\n%s", wantManual, conf)
	}
	if !strings.Contains(conf, "acme ") || !strings.Contains(conf, "server_name renew.example.com;") {
		t.Fatalf("expected near-expiry renew.example.com to use acme in:\n%s", conf)
	}

	// Advance clock past 30d window for app.example.com -> ACME takes over!
	eng.SetManualCertClock(func() time.Time { return now.Add(35 * 24 * time.Hour) }, DefaultManualCertRenewWindow)
	confAfter, err := eng.RenderPanel(RenderPanelInput{
		PanelID:    "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee",
		PanelName:  "vps-1",
		ETag:       "etag-manual",
		OriginIPs:  []netip.Addr{netip.MustParseAddr("204.136.10.51")},
		OriginPort: 443,
		CAStatus:   CAStatusMissing,
		Routes: []EdgeRoute{
			{
				ID:    "dom_1",
				Kind:  KindAppDomain,
				Hosts: []string{"app.example.com"},
			},
		},
	})
	if err != nil {
		t.Fatalf("RenderPanel after expiry window: %v", err)
	}
	if strings.Contains(confAfter, "app.example.com.crt") || !strings.Contains(confAfter, "acme ") {
		t.Fatalf("expected ACME takeover when <30d left, got:\n%s", confAfter)
	}
}

func TestAngieApplierReconcilerAndDaemonLifecycle(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	root := t.TempDir()
	paths := NewPaths(root)

	var validatedFiles []string
	var reloadAttempt int
	var failFirstReload bool
	runner := &AngieRunner{
		MainConfig:  paths.EnginePathsFor(EngineAngie).MainConfig,
		ServiceUnit: DefaultAngieServiceUnit,
		lookPath:    func(string) (string, error) { return "", errors.New("no systemctl") },
		runCmd: func(_ context.Context, name string, args ...string) ([]byte, error) {
			if name == "angie" && len(args) >= 3 && args[0] == "-t" {
				validatedFiles = append(validatedFiles, args[2])
				raw, _ := os.ReadFile(args[2])
				if strings.Contains(string(raw), ".angie.conf.validate.") {
					// Check panels inside validation dir for panel-bad.conf.
					for _, line := range strings.Split(string(raw), "\n") {
						if strings.Contains(line, ".panels.validate.") {
							glob := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(line), "include "), ";"))
							dir := filepath.Dir(filepath.FromSlash(glob))
							if _, err := os.Stat(filepath.Join(dir, "panel-bad.conf")); err == nil {
								return []byte("invalid panel-bad"), errors.New("exit 1")
							}
						}
					}
				}
				return []byte("angie: configuration file test is successful\n"), nil
			}
			if name == "angie" && len(args) >= 2 && args[0] == "-s" && args[1] == "reload" {
				reloadAttempt++
				if failFirstReload && reloadAttempt == 1 {
					return []byte("bind failed"), errors.New("exit 1")
				}
				return nil, nil
			}
			if name == "angie" && len(args) == 1 && args[0] == "-v" {
				return []byte("Angie version: Angie/1.12.4\n"), nil
			}
			return nil, nil
		},
	}

	eng := NewAngieEngine(paths.EnginePathsFor(EngineAngie), runner)
	applier := NewApplierForEngine(paths, eng)
	dnsGate := NewDNSGate(DNSGateOptions{Off: true})
	cfIPs := NewCFIPStore()
	systemd := &fakeSystemdRunner{active: true}
	rec := NewReconcilerWithEngine(paths, eng, dnsGate, applier, nil, systemd, cfIPs)

	cfg := &Config{
		EdgeID:      "edge-angie",
		ProxyEngine: EngineAngie,
		ACMEEmail:   "ops@example.com",
		DNSGateOff:  true,
		Panels: []PanelConfig{
			{PanelID: "panel-good", PanelName: "good", RegisteredAt: now},
		},
	}
	stGood := NewPanelState("panel-good", "good")
	stGood.LastGoodETag = "etag-1"
	stGood.LastGood = &RoutesResponse{
		APIVersion: 1,
		PanelID:    "panel-good",
		ETag:       "etag-1",
		Origin:     OriginInfo{IPs: []string{"204.136.10.51"}, Port: 443, CAStatus: CAStatusMissing},
		Routes:     []EdgeRoute{{ID: "dom_1", Hosts: []string{"good.example.com"}, TrustedProxy: true}},
	}
	states := map[string]*PanelState{"panel-good": stGood}
	claims := NewClaimsStore()
	meta := &ApplyMeta{}

	// 1. Apply gen 1 for Angie via Reconciler.
	out, err := rec.ReconcileOnce(ctx, cfg, states, claims, meta, false, false, now)
	if err != nil || !out.Applied || out.Generation != 1 {
		t.Fatalf("angie reconcile 1 failed: out=%+v err=%v", out, err)
	}
	if len(validatedFiles) == 0 {
		t.Fatalf("expected angie -t validation to run")
	}
	ep := eng.Paths()
	if _, err := os.Stat(ep.GlobalFile); err != nil {
		t.Fatalf("expected %s written: %v", ep.GlobalFile, err)
	}
	if _, err := os.Stat(ep.CommonFile); err != nil {
		t.Fatalf("expected %s written: %v", ep.CommonFile, err)
	}
	panelsDir, err := ReadDirLink(ep.PanelsLink)
	if err != nil {
		t.Fatalf("ReadDirLink(%s): %v", ep.PanelsLink, err)
	}
	if _, err := os.Stat(filepath.Join(panelsDir, "panel-good.conf")); err != nil {
		t.Fatalf("expected panel-good.conf in %s: %v", panelsDir, err)
	}

	// 2. RebuildClaimsFromDir supports .conf files.
	rebuilt, err := RebuildClaimsFromDir(panelsDir, ".conf", now)
	if err != nil {
		t.Fatalf("RebuildClaimsFromDir: %v", err)
	}
	if cl, ok := rebuilt.Claims["good.example.com"]; !ok || cl.PanelID != "panel-good" {
		t.Fatalf("expected rebuilt claim for good.example.com -> panel-good, got %+v", rebuilt.Claims)
	}

	// 3. Unchanged re-apply is NoOp.
	out2, err := rec.ReconcileOnce(ctx, cfg, states, claims, meta, false, false, now.Add(time.Minute))
	if err != nil || !out2.NoOp {
		t.Fatalf("expected NoOp on unchanged Angie reconcile, got out=%+v err=%v", out2, err)
	}

	// 4. Reload failure rolls back to previous generation.
	failFirstReload = true
	reloadAttempt = 0
	stGood.LastGoodETag = "etag-2"
	stGood.LastGood.ETag = "etag-2"
	stGood.LastGood.Routes[0].Hosts = []string{"good2.example.com"}
	_, err = rec.ReconcileOnce(ctx, cfg, states, claims, meta, false, false, now.Add(2*time.Minute))
	if err == nil {
		t.Fatalf("expected reload failure")
	}
	afterRB, _ := ReadDirLink(ep.PanelsLink)
	if afterRB != panelsDir {
		t.Fatalf("expected rollback to %s, got %s", panelsDir, afterRB)
	}
	failFirstReload = false
}

func TestDaemonReportsAngieVersionInStatusRequest(t *testing.T) {
	root := t.TempDir()
	paths := NewPaths(root)

	var mu sync.Mutex
	var statuses []StatusRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/edge/routes":
			_ = json.NewEncoder(w).Encode(RoutesResponse{
				APIVersion: 1,
				PanelID:    "panel-ang",
				ETag:       "etag-a",
				Origin:     OriginInfo{IPs: []string{"204.136.10.51"}, Port: 443, CAStatus: CAStatusMissing},
				Routes:     []EdgeRoute{{ID: "r1", Hosts: []string{"ang.example.com"}}},
				Node:       NodeInfo{ID: 1, IP: "107.189.6.222"},
			})
		case "/api/v1/edge/status":
			var st StatusRequest
			if err := json.NewDecoder(r.Body).Decode(&st); err == nil {
				mu.Lock()
				statuses = append(statuses, st)
				mu.Unlock()
			}
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	cfg := &Config{
		EdgeID:      "edge-ang-daemon",
		ProxyEngine: EngineAngie,
		ShieldIPs:   []string{"107.189.6.222"},
		DNSGateOff:  true,
		Panels: []PanelConfig{{
			PanelID:         "panel-ang",
			PanelName:       "ang",
			ShieldNodeID:    1,
			URL:             srv.URL,
			Token:           "tok",
			PollIntervalSec: 1,
			RegisteredAt:    time.Now().UTC(),
			InsecureHTTP:    true,
		}},
	}
	if err := SaveConfig(paths.ConfigFile, cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	runner := &AngieRunner{
		MainConfig:  paths.EnginePathsFor(EngineAngie).MainConfig,
		ServiceUnit: DefaultAngieServiceUnit,
		lookPath:    func(string) (string, error) { return "/usr/bin/systemctl", nil },
		runCmd: func(_ context.Context, name string, args ...string) ([]byte, error) {
			if name == "systemctl" && len(args) >= 2 && args[0] == "is-active" && args[1] == "angie.service" {
				return []byte("active\n"), nil
			}
			if name == "angie" && len(args) >= 1 && args[0] == "-v" {
				return []byte("Angie version: Angie/1.12.4\n"), nil
			}
			return []byte("ok\n"), nil
		},
	}
	eng := NewAngieEngineWithConfig(paths.EnginePathsFor(EngineAngie), cfg, runner)
	systemd := &fakeSystemdRunner{active: true}
	d, lock, err := NewDaemon(DaemonOptions{
		Paths:            paths,
		EdgeVersion:      "0.1.0",
		Engine:           eng,
		Systemd:          systemd,
		DNSResolver:      newFakeDNSResolver(),
		DebounceDelay:    15 * time.Millisecond,
		MinApplyInterval: 15 * time.Millisecond,
		StatusInterval:   40 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewDaemon: %v", err)
	}
	defer lock.Release()

	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()

	deadline := time.Now().Add(1200 * time.Millisecond)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(statuses)
		mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	<-done

	mu.Lock()
	defer mu.Unlock()
	if len(statuses) == 0 {
		t.Fatalf("expected at least one status post")
	}
	last := statuses[len(statuses)-1]
	if last.CaddyVersion != "1.12.4" || !last.CaddyRunning {
		t.Fatalf("expected active angie version 1.12.4 and running=true in StatusRequest, got version=%q running=%v", last.CaddyVersion, last.CaddyRunning)
	}
}
