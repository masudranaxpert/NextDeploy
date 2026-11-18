package edge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var angieIdentRe = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

func TestAngieEnginePaths(t *testing.T) {
	def := DefaultAngieEnginePaths()
	if def.MainConfig != "/etc/angie/angie.conf" ||
		def.ManagedDir != "/etc/angie/nd-edge" ||
		def.GlobalFile != "/etc/angie/nd-edge/global.conf" ||
		def.CommonFile != "/etc/angie/nd-edge/common.conf" ||
		def.PanelsLink != "/etc/angie/nd-edge/panels" ||
		def.OriginsLink != "/etc/angie/nd-edge/origins" ||
		def.UserDir != "/etc/angie/nd-edge/user" ||
		def.CertsDir != "/var/lib/angie/acme" ||
		def.ServiceUnit != "angie.service" ||
		def.ServiceUser != "angie" ||
		def.Binary != "angie" ||
		def.PanelExt != ".conf" {
		t.Fatalf("unexpected DefaultAngieEnginePaths: %+v", def)
	}

	root := t.TempDir()
	rooted := NewAngieEnginePaths(root)
	for _, p := range []string{
		rooted.MainConfig,
		rooted.ManagedDir,
		rooted.GlobalFile,
		rooted.CommonFile,
		rooted.PanelsLink,
		rooted.OriginsLink,
		rooted.UserDir,
		rooted.CertsDir,
	} {
		if !strings.HasPrefix(p, root) {
			t.Fatalf("expected path %q under root %q", p, root)
		}
	}
	if !strings.HasSuffix(rooted.PanelGenDir(7), "panels.gen-000007") {
		t.Fatalf("unexpected PanelGenDir: %s", rooted.PanelGenDir(7))
	}
}

func TestAngieRenderGlobalAndCommonGolden(t *testing.T) {
	paths := DefaultAngieEnginePaths()
	eng := NewAngieEngine(paths, nil)
	if eng.Name() != EngineAngie {
		t.Fatalf("expected engine name %q, got %q", EngineAngie, eng.Name())
	}

	cfgTrusted := &Config{
		EdgeID:    "edge-angie-1",
		ACMEEmail: "ops@example.com",
		ShieldIPs: []string{"107.189.6.222", "2001:db8::10"},
	}
	filesTrusted, err := eng.RenderGlobal(cfgTrusted, DefaultCloudflareRanges, true)
	if err != nil {
		t.Fatalf("RenderGlobal trusted: %v", err)
	}
	mainConf := filesTrusted[paths.MainConfig]
	globalTrusted := filesTrusted[paths.GlobalFile]
	assertGolden(t, "angie_global_main.golden", mainConf)
	assertGolden(t, "angie_global_trusted.golden", globalTrusted)

	for _, want := range []string{
		"worker_rlimit_nofile 1048576;",
		"worker_connections 65535;",
		"worker_shutdown_timeout 10m;",
		"include /etc/angie/nd-edge/global.conf;",
		"include /etc/angie/nd-edge/common.conf;",
		"include /etc/angie/nd-edge/user/*.conf;",
		"include /etc/angie/nd-edge/panels/*.conf;",
	} {
		if !strings.Contains(mainConf, want) {
			t.Fatalf("main angie.conf missing %q:\n%s", want, mainConf)
		}
	}
	for _, want := range []string{
		"client_header_timeout 10s;",
		"client_body_timeout 15s;",
		"keepalive_timeout 20s;",
		"send_timeout 60s;",
		"client_max_body_size 0;",
		"set_real_ip_from 107.189.6.222;",
		"set_real_ip_from 2001:db8::10;",
		"set_real_ip_from 173.245.48.0/20;",
		"set_real_ip_from 2400:cb00::/32;",
		"real_ip_header CF-Connecting-IP;",
		"real_ip_recursive on;",
	} {
		if !strings.Contains(globalTrusted, want) {
			t.Fatalf("global.conf missing %q:\n%s", want, globalTrusted)
		}
	}
	if strings.Contains(mainConf+globalTrusted, "127.0.0.11") {
		t.Fatalf("host nd-edge config must never emit Docker resolver 127.0.0.11")
	}

	filesUntrusted, err := eng.RenderGlobal(&Config{}, DefaultCloudflareRanges, false)
	if err != nil {
		t.Fatalf("RenderGlobal untrusted: %v", err)
	}
	globalUntrusted := filesUntrusted[paths.GlobalFile]
	assertGolden(t, "angie_global_untrusted.golden", globalUntrusted)
	if strings.Contains(globalUntrusted, "set_real_ip_from") || strings.Contains(globalUntrusted, "real_ip_header") {
		t.Fatalf("expected no real_ip directives when anyTrustedProxy=false:\n%s", globalUntrusted)
	}

	origResolv := resolvConfPath
	resolvConfPath = filepath.Join(t.TempDir(), "no-resolv.conf") // deterministic fallback resolvers
	defer func() { resolvConfPath = origResolv }()
	commonFiles, err := eng.RenderCommon()
	if err != nil {
		t.Fatalf("RenderCommon: %v", err)
	}
	commonConf := commonFiles[paths.CommonFile]
	assertGolden(t, "angie_common.golden", commonConf)
	if !strings.Contains(commonConf, "map $http_upgrade $nd_connection_upgrade") {
		t.Fatalf("common.conf missing upgrade map:\n%s", commonConf)
	}
}

func TestAngieRenderFromEdgeContractGolden(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "edge_contract.json"))
	if err != nil {
		t.Fatalf("read edge_contract.json: %v", err)
	}
	var doc edgeContractFixture
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("decode edge_contract.json: %v", err)
	}

	eng := NewAngieEngineWithConfig(DefaultAngieEnginePaths(), &Config{
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
	assertGolden(t, "angie_contract_panel.golden", got)
	assertAngieInvariants(t, got, true)
}

func TestAngieRenderPanelGoldenCases(t *testing.T) {
	panelID := "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
	eng := NewAngieEngine(DefaultAngieEnginePaths(), nil)

	cases := []struct {
		name     string
		golden   string
		in       RenderPanelInput
		expectCA bool
	}{
		{
			name:   "zero_routes",
			golden: "angie_panel_zero.golden",
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
			golden:   "angie_panel_many_h2.golden",
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
			golden:   "angie_panel_missing_ca.golden",
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
			golden:   "angie_panel_trusted_mixed.golden",
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
				assertAngieInvariants(t, got, tc.expectCA)
			}
		})
	}
}

func assertAngieInvariants(t *testing.T, conf string, expectCA bool) {
	t.Helper()
	if strings.Contains(conf, "acme_client nd_le") {
		t.Fatalf("must never use single shared acme_client nd_le:\n%s", conf)
	}
	if strings.Contains(conf, "127.0.0.11") {
		t.Fatalf("must never emit Docker resolver 127.0.0.11:\n%s", conf)
	}
	if strings.Contains(conf, "reuseport") {
		t.Fatalf("must never emit reuseport on per-site listen 443 quic:\n%s", conf)
	}
	for _, want := range []string{
		"account_key=/var/lib/angie/acme/nd_account.key;",
		"zone nd_up_",
		"64k;",
		"keepalive 64;",
		"status_zone ",
		"proxy_set_header Host $host;",
		"proxy_read_timeout 1h;",
		"proxy_buffering off;",
		"proxy_ssl_server_name on;",
		"proxy_ssl_name $host;",
	} {
		if !strings.Contains(conf, want) {
			t.Fatalf("rendered Angie panel config missing %q:\n%s", want, conf)
		}
	}
	if expectCA {
		if !strings.Contains(conf, "proxy_ssl_verify on;") || !strings.Contains(conf, "proxy_ssl_trusted_certificate ") {
			t.Fatalf("expected proxy_ssl_verify on + proxy_ssl_trusted_certificate when CAStatus=ok:\n%s", conf)
		}
	} else {
		if !strings.Contains(conf, "proxy_ssl_verify off;") {
			t.Fatalf("expected proxy_ssl_verify off when CA missing:\n%s", conf)
		}
	}
	for _, line := range strings.Split(conf, "\n") {
		tline := strings.TrimSpace(line)
		if strings.HasPrefix(tline, "acme_client ") {
			fields := strings.Fields(tline)
			if len(fields) < 3 {
				t.Fatalf("malformed acme_client line: %s", tline)
			}
			id := fields[1]
			if !angieIdentRe.MatchString(id) {
				t.Fatalf("acme_client identifier %q violates [A-Za-z0-9_]+", id)
			}
			if len("$acme_cert_key_"+id) > 64 {
				t.Fatalf("acme_client variable $acme_cert_key_%s exceeds safe bucket size", id)
			}
		}
	}
}

func TestAngieIdentifierSafetyAndDeterminism(t *testing.T) {
	eng := NewAngieEngine(DefaultAngieEnginePaths(), nil)
	panelID := "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"

	inA := RenderPanelInput{
		PanelID:   panelID,
		PanelName: "vps-1",
		ETag:      "etag-perm",
		OriginIPs: []netip.Addr{netip.MustParseAddr("204.136.10.51"), netip.MustParseAddr("204.136.10.52")},
		CAStatus:  CAStatusOK,
		Routes: []EdgeRoute{
			{ID: "r-1", Hosts: []string{"b.com", "a.com"}},
			{ID: "r_1", Hosts: []string{"d.com", "c.com"}},
		},
	}
	inB := RenderPanelInput{
		PanelID:   panelID,
		PanelName: "vps-1",
		ETag:      "etag-perm",
		OriginIPs: []netip.Addr{netip.MustParseAddr("204.136.10.52"), netip.MustParseAddr("204.136.10.51")},
		CAStatus:  CAStatusOK,
		Routes: []EdgeRoute{
			{ID: "r_1", Hosts: []string{"c.com", "d.com"}},
			{ID: "r-1", Hosts: []string{"a.com", "b.com"}},
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

	// Ensure r-1 and r_1 get distinct acme_client names and panel-11111111-a vs b don't collide.
	if id1, id2 := angieRouteIdent(panelID, "r-1"), angieRouteIdent(panelID, "r_1"); id1 == id2 {
		t.Fatalf("expected distinct route identifiers for r-1 and r_1, both got %q", id1)
	}
	if u1, u2 := angieUpstreamIdent("panel-11111111-a"), angieUpstreamIdent("panel-11111111-b"); u1 == u2 {
		t.Fatalf("expected distinct upstream identifiers for shared-prefix panels, both got %q", u1)
	}

	// Injection rejection.
	_, err = eng.RenderPanel(RenderPanelInput{
		PanelID:   panelID,
		OriginIPs: []netip.Addr{netip.MustParseAddr("204.136.10.51")},
		Routes:    []EdgeRoute{{ID: "r1", Hosts: []string{"evil.com; return 200"}}},
	})
	if err == nil {
		t.Fatalf("expected injection hostname to be rejected")
	}
}

func TestAngieRunner(t *testing.T) {
	ctx := context.Background()

	t.Run("validate_and_reload_systemd_vs_direct", func(t *testing.T) {
		var calls []string
		r := &AngieRunner{
			MainConfig:  "/etc/angie/angie.conf",
			ServiceUnit: "angie.service",
			lookPath:    func(name string) (string, error) { return "/usr/bin/" + name, nil },
			runCmd: func(_ context.Context, name string, args ...string) ([]byte, error) {
				calls = append(calls, name+" "+strings.Join(args, " "))
				if name == "angie" && len(args) == 3 && args[0] == "-t" {
					return []byte("angie: configuration file /etc/angie/angie.conf test is successful\n"), nil
				}
				if name == "systemctl" && len(args) == 2 && args[0] == "is-active" {
					return []byte("active\n"), nil
				}
				if name == "systemctl" && len(args) == 2 && args[0] == "reload" {
					return nil, nil
				}
				if name == "angie" && len(args) == 1 && args[0] == "-v" {
					return []byte("Angie version: Angie/1.12.4\n"), nil
				}
				return nil, fmt.Errorf("unexpected cmd %s %v", name, args)
			},
		}

		out, err := r.Validate(ctx, "")
		if err != nil || !strings.Contains(out, "test is successful") {
			t.Fatalf("Validate failed: out=%q err=%v", out, err)
		}
		if _, err := r.Reload(ctx); err != nil {
			t.Fatalf("Reload failed: %v", err)
		}
		if err := r.CheckAlive(ctx); err != nil {
			t.Fatalf("CheckAlive failed: %v", err)
		}
		ver, err := r.Version(ctx)
		if err != nil || ver != "1.12.4" {
			t.Fatalf("Version failed: ver=%q err=%v", ver, err)
		}

		joined := strings.Join(calls, "\n")
		if !strings.Contains(joined, "angie -t -c /etc/angie/angie.conf") ||
			!strings.Contains(joined, "systemctl reload angie.service") {
			t.Fatalf("unexpected commands:\n%s", joined)
		}

		// Non-systemd fallback runs `angie -s reload -c`.
		calls = nil
		useSys := false
		r.UseSystemd = &useSys
		r.runCmd = func(_ context.Context, name string, args ...string) ([]byte, error) {
			calls = append(calls, name+" "+strings.Join(args, " "))
			return nil, nil
		}
		if _, err := r.Reload(ctx); err != nil {
			t.Fatalf("direct Reload failed: %v", err)
		}
		if len(calls) != 1 || calls[0] != "angie -s reload -c /etc/angie/angie.conf" {
			t.Fatalf("expected direct angie -s reload, got %v", calls)
		}
	})

	t.Run("validate_error_and_version_parse_error", func(t *testing.T) {
		r := &AngieRunner{
			runCmd: func(_ context.Context, name string, args ...string) ([]byte, error) {
				if len(args) > 0 && args[0] == "-t" {
					return []byte("emerg: unknown directive"), errors.New("exit status 1")
				}
				return []byte("garbage output"), nil
			},
		}
		if _, err := r.Validate(ctx, "/tmp/bad.conf"); err == nil || !strings.Contains(err.Error(), "unknown directive") {
			t.Fatalf("expected wrapped validate error, got %v", err)
		}
		if _, err := r.Version(ctx); err == nil {
			t.Fatalf("expected error on unparseable angie -v output")
		}
	})

	t.Run("check_alive_status_url_tcp_and_pidfile", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"angie":{"version":"1.12.4"}}`))
		}))
		defer ts.Close()

		rStatus := &AngieRunner{StatusURL: ts.URL}
		if err := rStatus.CheckAlive(ctx); err != nil {
			t.Fatalf("StatusURL CheckAlive failed: %v", err)
		}

		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("listen: %v", err)
		}
		defer ln.Close()
		noSys := false
		rTCP := &AngieRunner{UseSystemd: &noSys, CheckAddr: ln.Addr().String()}
		if err := rTCP.CheckAlive(ctx); err != nil {
			t.Fatalf("TCP CheckAlive failed: %v", err)
		}

		pidPath := filepath.Join(t.TempDir(), "angie.pid")
		if err := os.WriteFile(pidPath, []byte(fmt.Sprintf("%d\n", os.Getpid())), 0o644); err != nil {
			t.Fatalf("write pid: %v", err)
		}
		rPID := &AngieRunner{UseSystemd: &noSys, PIDFile: pidPath}
		if err := rPID.CheckAlive(ctx); err != nil {
			t.Fatalf("PID CheckAlive failed: %v", err)
		}
	})
}

// Angie before 1.11 refuses ACME clients without a run-time resolver (Ubuntu 20.04 ships 1.10),
// and on IPv4-only Shield nodes an AAAA answer for the ACME directory cannot be reached.
func TestRenderCommonAngieHasResolver(t *testing.T) {
	dir := t.TempDir()
	orig := resolvConfPath
	defer func() { resolvConfPath = orig }()

	resolvConfPath = filepath.Join(dir, "resolv.conf")
	if err := os.WriteFile(resolvConfPath, []byte("# x\nnameserver 127.0.0.53\nnameserver fe80::1%eth0\nnameserver 2606:4700::1111\noptions edns0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := RenderCommonAngie(); !strings.Contains(got, "resolver 127.0.0.53 [2606:4700::1111] valid=300s ipv6=off;") {
		t.Fatalf("resolver from resolv.conf missing:\n%s", got)
	}

	resolvConfPath = filepath.Join(dir, "missing")
	if got := RenderCommonAngie(); !strings.Contains(got, "resolver 1.1.1.1 8.8.8.8 valid=300s ipv6=off;") {
		t.Fatalf("fallback resolver missing:\n%s", got)
	}
}
