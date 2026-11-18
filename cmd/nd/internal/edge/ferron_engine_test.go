package edge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestFerronEnginePaths(t *testing.T) {
	def := DefaultFerronEnginePaths()
	if def.MainConfig != "/etc/ferron/nd-edge/ferron.conf" ||
		def.ManagedDir != "/etc/ferron/nd-edge" ||
		def.GlobalFile != "/etc/ferron/nd-edge/global.conf" ||
		def.CommonFile != "/etc/ferron/nd-edge/common.conf" ||
		def.PanelsLink != "/etc/ferron/nd-edge/panels" ||
		def.OriginsLink != "/etc/ferron/nd-edge/origins" ||
		def.UserDir != "/etc/ferron/nd-edge/user" ||
		def.CertsDir != "/var/cache/ferron-acme" ||
		def.ManualCertsDir != "/etc/ferron/nd-edge/certs" ||
		def.ServiceUnit != "ferron.service" ||
		def.ServiceUser != "ferron" ||
		def.Binary != "ferron" ||
		def.PanelExt != ".conf" {
		t.Fatalf("unexpected DefaultFerronEnginePaths: %+v", def)
	}

	root := t.TempDir()
	rooted := NewFerronEnginePaths(root)
	for _, p := range []string{
		rooted.MainConfig,
		rooted.ManagedDir,
		rooted.GlobalFile,
		rooted.CommonFile,
		rooted.PanelsLink,
		rooted.OriginsLink,
		rooted.UserDir,
		rooted.CertsDir,
		rooted.ManualCertsDir,
	} {
		if !strings.HasPrefix(p, root) {
			t.Fatalf("expected path %q under root %q", p, root)
		}
	}
	if !strings.HasSuffix(rooted.PanelGenDir(4), "panels.gen-000004") {
		t.Fatalf("unexpected PanelGenDir: %s", rooted.PanelGenDir(4))
	}
}

func TestFerronRenderGlobalAndCommonGolden(t *testing.T) {
	paths := DefaultFerronEnginePaths()
	eng := NewFerronEngine(paths, nil)
	if eng.Name() != EngineFerron {
		t.Fatalf("expected engine name %q, got %q", EngineFerron, eng.Name())
	}

	cfgTrusted := &Config{
		EdgeID:    "edge-ferron-1",
		ACMEEmail: "ops@example.com",
		ShieldIPs: []string{"107.189.6.222", "2001:db8::10"},
	}
	filesTrusted, err := eng.RenderGlobal(cfgTrusted, DefaultCloudflareRanges, true)
	if err != nil {
		t.Fatalf("RenderGlobal trusted: %v", err)
	}
	mainConf := filesTrusted[paths.MainConfig]
	globalTrusted := filesTrusted[paths.GlobalFile]
	assertGolden(t, "ferron_global_main.golden", mainConf)
	assertGolden(t, "ferron_global_trusted.golden", globalTrusted)

	for _, want := range []string{
		`include "/etc/ferron/nd-edge/global.conf"`,
		`include "/etc/ferron/nd-edge/common.conf"`,
		`include "/etc/ferron/nd-edge/user/*.conf"`,
		`include "/etc/ferron/nd-edge/panels/*.conf"`,
	} {
		if !strings.Contains(mainConf, want) {
			t.Fatalf("ferron.conf missing %q:\n%s", want, mainConf)
		}
	}
	for _, want := range []string{
		`listen "127.0.0.1:8081"`,
		`auth_token "{{env.ND_FERRON_ADMIN_TOKEN}}"`,
		"protocols h1 h2",
		`timeout "3660s"`,
		"client_ip_from_header x-forwarded-for",
		`trusted_proxy "107.189.6.222"`,
		`trusted_proxy "2001:db8::10"`,
		`trusted_proxy "173.245.48.0/20"`,
		`trusted_proxy "2400:cb00::/32"`,
		"strict_hosts",
	} {
		if !strings.Contains(globalTrusted, want) {
			t.Fatalf("global.conf missing %q:\n%s", want, globalTrusted)
		}
	}

	filesUntrusted, err := eng.RenderGlobal(&Config{}, DefaultCloudflareRanges, false)
	if err != nil {
		t.Fatalf("RenderGlobal untrusted: %v", err)
	}
	globalUntrusted := filesUntrusted[paths.GlobalFile]
	assertGolden(t, "ferron_global_untrusted.golden", globalUntrusted)
	if strings.Contains(globalUntrusted, "client_ip_from_header") || strings.Contains(globalUntrusted, "trusted_proxy") {
		t.Fatalf("expected no client_ip_from_header when anyTrustedProxy=false:\n%s", globalUntrusted)
	}

	commonFiles, err := eng.RenderCommon()
	if err != nil {
		t.Fatalf("RenderCommon: %v", err)
	}
	commonConf := commonFiles[paths.CommonFile]
	assertGolden(t, "ferron_common.golden", commonConf)
	if !strings.Contains(commonConf, "snippet nd_common") {
		t.Fatalf("common.conf missing nd_common snippet:\n%s", commonConf)
	}
}

func TestFerronRenderFromEdgeContractGolden(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "edge_contract.json"))
	if err != nil {
		t.Fatalf("read edge_contract.json: %v", err)
	}
	var doc edgeContractFixture
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("decode edge_contract.json: %v", err)
	}

	eng := NewFerronEngineWithConfig(DefaultFerronEnginePaths(), &Config{
		ACMEEmail: "ops@example.com",
	}, nil)

	in := RenderPanelInput{
		PanelID:    doc.RoutesResponse.PanelID,
		PanelName:  doc.RegisterResponse.PanelName,
		ETag:       doc.RoutesResponse.ETag,
		OriginIPs:  ParseIPStrings(doc.RoutesResponse.Origin.IPs),
		OriginPort: doc.RoutesResponse.Origin.Port,
		CAStatus:   doc.RoutesResponse.Origin.CAStatus,
		Routes:     doc.RoutesResponse.Routes,
	}
	got, err := eng.RenderPanel(in)
	if err != nil {
		t.Fatalf("RenderPanel from edge_contract.json: %v", err)
	}
	assertGolden(t, "ferron_contract_panel.golden", got)
	assertFerronInvariants(t, got, true)
}

func TestFerronRenderPanelGoldenCases(t *testing.T) {
	panelID := "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
	eng := NewFerronEngine(DefaultFerronEnginePaths(), nil)

	cases := []struct {
		name     string
		golden   string
		in       RenderPanelInput
		expectCA bool
	}{
		{
			name:   "zero_routes",
			golden: "ferron_panel_zero.golden",
			in: RenderPanelInput{
				PanelID:    panelID,
				PanelName:  "vps-1",
				ETag:       "etag-zero",
				OriginIPs:  []netip.Addr{netip.MustParseAddr("204.136.10.51")},
				OriginPort: 443,
				CAStatus:   CAStatusOK,
			},
		},
		{
			name:     "many_routes_multi_ip_h2",
			golden:   "ferron_panel_many_h2.golden",
			expectCA: true,
			in: RenderPanelInput{
				PanelID:    panelID,
				PanelName:  "vps-1",
				ETag:       "etag-many",
				OriginIPs:  []netip.Addr{netip.MustParseAddr("204.136.10.52"), netip.MustParseAddr("204.136.10.51")},
				OriginPort: 443,
				CAStatus:   CAStatusOK,
				Routes: []EdgeRoute{
					{
						ID:      "proxy_9",
						Kind:    KindProxyRoute,
						Hosts:   []string{"grpc.example.com"},
						Options: RouteOptions{UpstreamH2: true, MaxBodyMB: 64},
					},
					{
						ID:    "dom_2",
						Kind:  KindAppDomain,
						Hosts: []string{"zeta.example.com", "alpha.example.com"},
					},
				},
			},
		},
		{
			name:     "missing_ca",
			golden:   "ferron_panel_missing_ca.golden",
			expectCA: false,
			in: RenderPanelInput{
				PanelID:    panelID,
				PanelName:  "vps-1",
				ETag:       "etag-noca",
				OriginIPs:  []netip.Addr{netip.MustParseAddr("204.136.10.51")},
				OriginPort: 443,
				CAStatus:   CAStatusMissing,
				Routes: []EdgeRoute{
					{
						ID:    "dom_1",
						Kind:  KindAppDomain,
						Hosts: []string{"noca.example.com"},
					},
				},
			},
		},
		{
			name:     "trusted_proxy_mixed",
			golden:   "ferron_panel_trusted_mixed.golden",
			expectCA: true,
			in: RenderPanelInput{
				PanelID:    panelID,
				PanelName:  "vps-1",
				ETag:       "etag-tp",
				OriginIPs:  []netip.Addr{netip.MustParseAddr("204.136.10.51")},
				OriginPort: 443,
				CAStatus:   CAStatusOK,
				Routes: []EdgeRoute{
					{
						ID:           "r_direct",
						Kind:         KindAppDomain,
						Hosts:        []string{"direct.example.com"},
						TrustedProxy: false,
						Options:      RouteOptions{Websocket: true},
					},
					{
						ID:           "r_cf",
						Kind:         KindAppDomain,
						Hosts:        []string{"cf.example.com"},
						TrustedProxy: true,
						Options:      RouteOptions{Websocket: true, MaxBodyMB: 128},
					},
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := eng.RenderPanel(tc.in)
			if err != nil {
				t.Fatalf("RenderPanel: %v", err)
			}
			assertGolden(t, tc.golden, got)
			if len(tc.in.Routes) > 0 {
				assertFerronInvariants(t, got, tc.expectCA)
			}
		})
	}
}

func assertFerronInvariants(t *testing.T, conf string, expectCA bool) {
	t.Helper()
	for _, want := range []string{
		"use nd_common",
		"provider acme",
		"challenge http-01",
		`upstream "https://`,
		"keepalive",
		`request_header X-Forwarded-Proto "https"`,
	} {
		if !strings.Contains(conf, want) {
			t.Fatalf("rendered Ferron panel config missing %q:\n%s", want, conf)
		}
	}
	if expectCA {
		if !strings.Contains(conf, `ca "/etc/ferron/nd-edge/origins/`) {
			t.Fatalf("expected ca directive when CAStatus=ok:\n%s", conf)
		}
		if strings.Contains(conf, "no_verification") {
			t.Fatalf("unexpected no_verification when CAStatus=ok:\n%s", conf)
		}
	} else {
		if !strings.Contains(conf, "no_verification") {
			t.Fatalf("expected no_verification when CA missing:\n%s", conf)
		}
	}
}

func TestFerronManualCertAndNearExpiryFallback(t *testing.T) {
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	root := t.TempDir()
	ep := NewFerronEnginePaths(root)
	if err := os.MkdirAll(ep.ManualCertsDir, 0o755); err != nil {
		t.Fatalf("mkdir ManualCertsDir: %v", err)
	}

	// Imported valid cert (>30d left) vs near-expiry cert (<=30d left)
	validCert, validKey := generateTestLeafCertAndKey(t, []string{"manual.example.com"}, now.Add(-time.Hour), now.Add(60*24*time.Hour))
	_ = os.WriteFile(filepath.Join(ep.ManualCertsDir, "manual.example.com.crt"), validCert, 0o644)
	_ = os.WriteFile(filepath.Join(ep.ManualCertsDir, "manual.example.com.key"), validKey, 0o600)

	nearCert, nearKey := generateTestLeafCertAndKey(t, []string{"renew.example.com"}, now.Add(-time.Hour), now.Add(10*24*time.Hour))
	_ = os.WriteFile(filepath.Join(ep.ManualCertsDir, "renew.example.com.crt"), nearCert, 0o644)
	_ = os.WriteFile(filepath.Join(ep.ManualCertsDir, "renew.example.com.key"), nearKey, 0o600)

	eng := NewFerronEngineWithConfig(ep, &Config{ACMEEmail: "ops@example.com"}, nil)
	eng.SetManualCertClock(func() time.Time { return now }, DefaultManualCertRenewWindow)

	out, err := eng.RenderPanel(RenderPanelInput{
		PanelID:    "panel-ferron-1",
		PanelName:  "vps-1",
		ETag:       "etag-man",
		OriginIPs:  []netip.Addr{netip.MustParseAddr("204.136.10.51")},
		OriginPort: 443,
		CAStatus:   CAStatusOK,
		Routes: []EdgeRoute{
			{ID: "dom_1", Hosts: []string{"manual.example.com", "renew.example.com"}},
		},
	})
	if err != nil {
		t.Fatalf("RenderPanel: %v", err)
	}
	if !strings.Contains(out, "provider manual") || !strings.Contains(out, "manual.example.com.crt") {
		t.Fatalf("expected manual.example.com to use imported manual cert:\n%s", out)
	}
	if !strings.Contains(out, "renew.example.com {\n") || !strings.Contains(out, "provider acme") {
		t.Fatalf("expected near-expiry renew.example.com to fall back to ACME:\n%s", out)
	}
}

func TestFerronDeterminismInjectionAndClaimsRebuild(t *testing.T) {
	root := t.TempDir()
	ep := NewFerronEnginePaths(root)
	eng := NewFerronEngine(ep, nil)
	panelID := "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"

	inA := RenderPanelInput{
		PanelID:   panelID,
		PanelName: "vps-1",
		ETag:      "etag-1",
		OriginIPs: []netip.Addr{netip.MustParseAddr("204.136.10.52"), netip.MustParseAddr("204.136.10.51")},
		CAStatus:  CAStatusOK,
		Routes: []EdgeRoute{
			{ID: "r2", Hosts: []string{"z.example.com", "a.example.com"}},
			{ID: "r1", Hosts: []string{"m.example.com"}},
		},
	}
	inB := RenderPanelInput{
		PanelID:   panelID,
		PanelName: "vps-1",
		ETag:      "etag-1",
		OriginIPs: []netip.Addr{netip.MustParseAddr("204.136.10.51"), netip.MustParseAddr("204.136.10.52")},
		CAStatus:  CAStatusOK,
		Routes: []EdgeRoute{
			{ID: "r1", Hosts: []string{"m.example.com"}},
			{ID: "r2", Hosts: []string{"a.example.com", "z.example.com"}},
		},
	}
	outA, err := eng.RenderPanel(inA)
	if err != nil {
		t.Fatalf("RenderPanel A: %v", err)
	}
	outB, err := eng.RenderPanel(inB)
	if err != nil {
		t.Fatalf("RenderPanel B: %v", err)
	}
	if outA != outB {
		t.Fatalf("expected deterministic output across permutations")
	}

	if _, err := eng.RenderPanel(RenderPanelInput{
		PanelID:   panelID,
		OriginIPs: []netip.Addr{netip.MustParseAddr("204.136.10.51")},
		Routes:    []EdgeRoute{{ID: "r1", Hosts: []string{"evil.com { status 200 }"}}},
	}); err == nil {
		t.Fatalf("expected injection hostname to fail validation")
	}

	panelsDir := ep.PanelGenDir(1)
	if err := os.MkdirAll(panelsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	_ = os.WriteFile(filepath.Join(panelsDir, PlaceholderFerronFileName), []byte(RenderPlaceholderFerron()), 0o644)
	_ = os.WriteFile(filepath.Join(panelsDir, panelID+".conf"), []byte(outA), 0o644)

	claims, err := RebuildClaimsFromDir(panelsDir, ".conf", time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("RebuildClaimsFromDir: %v", err)
	}
	for _, wantHost := range []string{"a.example.com", "m.example.com", "z.example.com"} {
		cl, ok := claims.Claims[wantHost]
		if !ok || cl.PanelID != panelID {
			t.Fatalf("missing rebuilt claim for %q: %+v", wantHost, claims.Claims)
		}
	}
}

func TestFerronRunner(t *testing.T) {
	ctx := context.Background()

	t.Run("validate_json_pass_and_fail", func(t *testing.T) {
		var gotArgs []string
		r := &FerronRunner{
			MainConfig: "/etc/ferron/nd-edge/ferron.conf",
			runCmd: func(_ context.Context, name string, args ...string) ([]byte, error) {
				gotArgs = append([]string{name}, args...)
				if len(args) >= 3 && args[2] == "/bad.conf" {
					return []byte(`{"valid":false,"diagnostics":[{"kind":"Invalid configuration","message":"syntax error"}]}`), errors.New("exit 1")
				}
				return []byte(`{"valid":true,"diagnostics":[]}`), nil
			},
		}
		if out, err := r.Validate(ctx, ""); err != nil || !strings.Contains(out, `"valid":true`) {
			t.Fatalf("Validate failed: out=%q err=%v", out, err)
		}
		wantCmd := "ferron validate -c /etc/ferron/nd-edge/ferron.conf --json --config-adapter ferronconf"
		if strings.Join(gotArgs, " ") != wantCmd {
			t.Fatalf("expected cmd %q, got %q", wantCmd, strings.Join(gotArgs, " "))
		}
		if _, err := r.Validate(ctx, "/bad.conf"); err == nil || !strings.Contains(err.Error(), "syntax error") {
			t.Fatalf("expected validate diagnostic error, got %v", err)
		}
	})

	t.Run("reload_admin_api_generation_and_error", func(t *testing.T) {
		var gen atomic.Int64
		gen.Store(1)
		var lastAuth string
		var failGetReload bool
		var failPostReload bool

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/health" {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("OK"))
				return
			}
			lastAuth = r.Header.Get("Authorization")
			if lastAuth != "Bearer secret-tok" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			switch {
			case r.Method == http.MethodPost && r.URL.Path == "/reload":
				if failPostReload {
					w.WriteHeader(http.StatusInternalServerError)
					_, _ = w.Write([]byte(`{"status":"reload_failed","error":"bad config"}`))
					return
				}
				gen.Add(1)
				_, _ = w.Write([]byte(`{"status":"reload_initiated","error":null}`))
			case r.Method == http.MethodGet && r.URL.Path == "/reload":
				if failGetReload {
					_, _ = fmt.Fprintf(w, `{"last_reload_time":"2026-11-18T12:00:00Z","last_reload_error":"parse fail","active_generation":%d}`, gen.Load())
					return
				}
				_, _ = fmt.Fprintf(w, `{"last_reload_time":"2026-11-18T12:00:00Z","last_reload_error":null,"active_generation":%d}`, gen.Load())
			default:
				http.NotFound(w, r)
			}
		}))
		defer srv.Close()

		useSys := false
		r := &FerronRunner{
			AdminURL:   srv.URL,
			AdminToken: "secret-tok",
			UseSystemd: &useSys,
		}
		if err := r.CheckAlive(ctx); err != nil {
			t.Fatalf("CheckAlive failed: %v", err)
		}
		if dur, err := r.Reload(ctx); err != nil || dur < 0 {
			t.Fatalf("Admin Reload failed: dur=%v err=%v", dur, err)
		}
		if lastAuth != "Bearer secret-tok" || gen.Load() != 2 {
			t.Fatalf("expected Bearer auth & gen=2, got auth=%q gen=%d", lastAuth, gen.Load())
		}

		failGetReload = true
		if _, err := r.Reload(ctx); err == nil || !strings.Contains(err.Error(), "parse fail") {
			t.Fatalf("expected GET /reload error propagation, got %v", err)
		}
		failGetReload = false

		failPostReload = true
		if _, err := r.Reload(ctx); err == nil || !strings.Contains(err.Error(), "bad config") {
			t.Fatalf("expected POST /reload error propagation, got %v", err)
		}
	})

	t.Run("reload_systemd_fallback_and_version", func(t *testing.T) {
		var calls []string
		r := &FerronRunner{
			ServiceUnit: "ferron.service",
			lookPath:    func(name string) (string, error) { return "/usr/bin/" + name, nil },
			runCmd: func(_ context.Context, name string, args ...string) ([]byte, error) {
				calls = append(calls, name+" "+strings.Join(args, " "))
				if name == "systemctl" && len(args) >= 2 && args[0] == "is-active" {
					return []byte("active\n"), nil
				}
				if name == "ferron" && len(args) == 1 && args[0] == "version" {
					return []byte("Ferron 3.0.0-rc.11\n  Compiled on: 2026-11-08\n"), nil
				}
				return nil, nil
			},
		}
		if _, err := r.Reload(ctx); err != nil {
			t.Fatalf("systemd Reload failed: %v", err)
		}
		if !strings.Contains(strings.Join(calls, "\n"), "systemctl reload ferron.service") {
			t.Fatalf("expected systemctl reload call, got %v", calls)
		}
		ver, err := r.Version(ctx)
		if err != nil || ver != "3.0.0-rc.11" {
			t.Fatalf("Version failed: ver=%q err=%v", ver, err)
		}
	})
}

func TestFerronApplierLifecycle(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	paths := NewPaths(root)
	ep := paths.EnginePathsFor(EngineFerron)
	cfg := &Config{EdgeID: "edge-f1", ProxyEngine: EngineFerron, ACMEEmail: "ops@example.com"}

	var validated []string
	var reloads int
	runner := &FerronRunner{
		MainConfig:  ep.MainConfig,
		ServiceUnit: ep.ServiceUnit,
		lookPath:    func(name string) (string, error) { return "/usr/bin/" + name, nil },
		runCmd: func(_ context.Context, name string, args ...string) ([]byte, error) {
			if name == "ferron" && len(args) > 0 && args[0] == "validate" {
				validated = append(validated, strings.Join(args, " "))
				return []byte(`{"valid":true,"diagnostics":[]}`), nil
			}
			if name == "systemctl" && len(args) >= 1 && args[0] == "is-active" {
				return []byte("active\n"), nil
			}
			if name == "systemctl" && len(args) >= 1 && args[0] == "reload" {
				reloads++
				return nil, nil
			}
			return nil, nil
		},
	}

	eng := NewFerronEngineWithConfig(ep, cfg, runner)
	applier := NewApplierForEngine(paths, eng)

	globalFiles, err := eng.RenderGlobal(cfg, DefaultCloudflareRanges, true)
	if err != nil {
		t.Fatalf("RenderGlobal: %v", err)
	}
	commonFiles, err := eng.RenderCommon()
	if err != nil {
		t.Fatalf("RenderCommon: %v", err)
	}
	panelConf, err := eng.RenderPanel(RenderPanelInput{
		PanelID:     "p-ferron",
		PanelName:   "vps-ferron",
		ETag:        "e1",
		OriginIPs:   []netip.Addr{netip.MustParseAddr("204.136.10.51")},
		OriginPort:  443,
		CAStatus:    CAStatusOK,
		OriginsLink: ep.OriginsLink,
		Routes:      []EdgeRoute{{ID: "r1", Hosts: []string{"ferron.example.com"}, TrustedProxy: true}},
	})
	if err != nil {
		t.Fatalf("RenderPanel: %v", err)
	}

	meta := &ApplyMeta{}
	now := time.Date(2026, 11, 18, 13, 0, 0, 0, time.UTC)
	in := ApplyInput{
		GlobalFiles: globalFiles,
		CommonFiles: commonFiles,
		Panels: []PanelRenderArtifact{
			{PanelID: "p-ferron", CaddyBytes: []byte(panelConf), PEMBytes: []byte("CERT"), Changed: true},
		},
	}

	res1, err := applier.Apply(ctx, in, meta, now)
	if err != nil || !res1.Applied || meta.CurrentGen != 1 || len(validated) != 1 || reloads != 1 {
		t.Fatalf("first Apply failed: res=%+v err=%v meta=%+v validated=%v reloads=%d", res1, err, meta, validated, reloads)
	}
	panelsDir, err := ReadDirLink(ep.PanelsLink)
	if err != nil {
		t.Fatalf("ReadDirLink(%s): %v", ep.PanelsLink, err)
	}
	if _, err := os.Stat(filepath.Join(panelsDir, "p-ferron.conf")); err != nil {
		t.Fatalf("expected p-ferron.conf via panels link %s: %v", panelsDir, err)
	}

	// Idempotent no-op
	res2, err := applier.Apply(ctx, in, meta, now.Add(time.Minute))
	if err != nil || !res2.NoOp || reloads != 1 {
		t.Fatalf("expected NoOp on unchanged apply, got res=%+v err=%v reloads=%d", res2, err, reloads)
	}
}
