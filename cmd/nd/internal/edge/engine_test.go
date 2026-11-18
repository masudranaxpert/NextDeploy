package edge

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"testing"
	"time"
)

type fakeVersionCaddyRunner struct {
	validateCalls int
	reloadCalls   int
	adminCalls    int
	versionCalls  int
	lastConfig    string
	lastAdmin     string
}

func (f *fakeVersionCaddyRunner) Validate(_ context.Context, configPath string) (string, error) {
	f.validateCalls++
	f.lastConfig = configPath
	return "Valid configuration", nil
}

func (f *fakeVersionCaddyRunner) Reload(_ context.Context, configPath, adminAddr string) (time.Duration, error) {
	f.reloadCalls++
	f.lastConfig = configPath
	f.lastAdmin = adminAddr
	return 12 * time.Millisecond, nil
}

func (f *fakeVersionCaddyRunner) CheckAdmin(_ context.Context, adminAddr string) error {
	f.adminCalls++
	f.lastAdmin = adminAddr
	return nil
}

func (f *fakeVersionCaddyRunner) Version(_ context.Context) (string, error) {
	f.versionCalls++
	return "v2.8.4", nil
}

func TestCaddyEngineParityWithLegacyRenderers(t *testing.T) {
	paths := DefaultPaths()
	cr := &fakeVersionCaddyRunner{}
	cfg := &Config{
		EdgeID:         "edge-1",
		ACMEEmail:      "ops@example.com",
		AdminAddr:      "127.0.0.1:2019",
		ShieldIPs:      []string{"107.189.6.222"},
		TrustedProxies: true,
		PreservedGlobal: "servers {\n\ttrusted_proxies static 103.21.244.0/22 5.175.220.216\n\tmetrics\n}",
	}

	eng := NewCaddyEngineWithConfig(paths, cfg, cr)
	if eng.Name() != EngineCaddy {
		t.Fatalf("expected engine name %q, got %q", EngineCaddy, eng.Name())
	}
	if eng.Paths().MainConfig != paths.Caddyfile || eng.Paths().PanelExt != ".caddy" {
		t.Fatalf("unexpected CaddyEnginePaths: %+v", eng.Paths())
	}

	for _, anyTP := range []bool{false, true} {
		files, err := eng.RenderGlobal(cfg, DefaultCloudflareRanges, anyTP)
		if err != nil {
			t.Fatalf("RenderGlobal(anyTP=%v): %v", anyTP, err)
		}
		want := RenderGlobalOptsForConfig(cfg, DefaultCloudflareRanges, anyTP)
		if got := files[paths.GlobalOpts]; got != want {
			t.Fatalf("global.opts mismatch (anyTP=%v):\nWANT:\n%s\nGOT:\n%s", anyTP, want, got)
		}
	}

	commonFiles, err := eng.RenderCommon()
	if err != nil {
		t.Fatalf("RenderCommon: %v", err)
	}
	if got, want := commonFiles[paths.CommonCaddy], RenderCommonCaddy(); got != want {
		t.Fatalf("common.caddy mismatch:\nWANT:\n%s\nGOT:\n%s", want, got)
	}

	in := RenderPanelInput{
		PanelID:    "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee",
		PanelName:  "vps-1",
		ETag:       "etag-1",
		OriginIPs:  []netip.Addr{netip.MustParseAddr("204.136.10.51")},
		OriginPort: 443,
		CAStatus:   CAStatusOK,
		Routes: []EdgeRoute{
			{
				ID:           "dom_12",
				Kind:         KindAppDomain,
				Hosts:        []string{"www.app.example.com", "app.example.com"},
				TrustedProxy: true,
				Options:      RouteOptions{Websocket: true, MaxBodyMB: 32},
			},
		},
	}
	gotPanel, err := eng.RenderPanel(in)
	if err != nil {
		t.Fatalf("RenderPanel: %v", err)
	}
	wantPanel, err := RenderPanelCaddy(in)
	if err != nil {
		t.Fatalf("RenderPanelCaddy: %v", err)
	}
	if gotPanel != wantPanel {
		t.Fatalf("panel mismatch:\nWANT:\n%s\nGOT:\n%s", wantPanel, gotPanel)
	}

	gotRegion := eng.RenderManagedRegion(cfg)
	wantRegion := RenderCaddyfileManagedRegion(paths, cfg.AdminAddr, cfg.ACMEEmail, paths.PanelsLink, paths.UserCaddyDir)
	if gotRegion != wantRegion {
		t.Fatalf("managed region mismatch:\nWANT:\n%s\nGOT:\n%s", wantRegion, gotRegion)
	}

	// Runner delegation.
	ctx := context.Background()
	runner := eng.Runner()
	if out, err := runner.Validate(ctx, ""); err != nil || out != "Valid configuration" {
		t.Fatalf("runner.Validate: out=%q err=%v", out, err)
	}
	if cr.lastConfig != paths.Caddyfile {
		t.Fatalf("expected default MainConfig %q, got %q", paths.Caddyfile, cr.lastConfig)
	}
	if dur, err := runner.Reload(ctx); err != nil || dur <= 0 {
		t.Fatalf("runner.Reload: dur=%v err=%v", dur, err)
	}
	if cr.lastAdmin != "127.0.0.1:2019" {
		t.Fatalf("expected adminAddr 127.0.0.1:2019, got %q", cr.lastAdmin)
	}
	if err := runner.CheckAlive(ctx); err != nil {
		t.Fatalf("runner.CheckAlive: %v", err)
	}
	if ver, err := runner.Version(ctx); err != nil || ver != "v2.8.4" {
		t.Fatalf("runner.Version: ver=%q err=%v", ver, err)
	}
}

func TestRunnerAndApplierAdapters(t *testing.T) {
	ctx := context.Background()
	paths := NewPaths(t.TempDir())

	cr := &fakeVersionCaddyRunner{}
	r := RunnerFromCaddyRunner(cr, paths.Caddyfile, DefaultAdminAddr)
	back := CaddyRunnerFromRunner(r)
	if back != cr {
		t.Fatalf("expected round-trip CaddyRunner unwrap to return original CaddyRunner")
	}

	// Generic Runner adapted into Applier via NewApplierWithRunner.
	angieRunner := &AngieRunner{
		MainConfig: "/tmp/angie.conf",
		runCmd: func(_ context.Context, name string, args ...string) ([]byte, error) {
			if name == "angie" && len(args) >= 1 && args[0] == "-t" {
				return []byte("syntax is ok"), nil
			}
			if name == "angie" && len(args) >= 2 && args[0] == "-s" && args[1] == "reload" {
				return []byte("reloaded"), nil
			}
			if name == "angie" && len(args) == 1 && args[0] == "-v" {
				return []byte("Angie version: Angie/1.12.4\n"), nil
			}
			return nil, errors.New("unexpected command")
		},
		lookPath: func(string) (string, error) { return "", errors.New("no systemctl") },
	}

	applier := NewApplierWithRunner(paths, angieRunner)
	if applier.Runner() != angieRunner {
		t.Fatalf("expected Applier.Runner() to return angieRunner")
	}
	res, err := applier.Apply(ctx, ApplyInput{
		GlobalOpts:  []byte(RenderGlobalOpts(DefaultCloudflareRanges)),
		CommonCaddy: []byte(RenderCommonCaddy()),
		Panels: []PanelRenderArtifact{
			{PanelID: "panel-a", CaddyBytes: []byte("# test\n"), Changed: true},
		},
	}, &ApplyMeta{}, time.Date(2026, 11, 18, 6, 0, 0, 0, time.UTC))
	if err != nil || !res.Applied {
		t.Fatalf("Applier with generic Runner failed: res=%+v err=%v", res, err)
	}
}

func TestEngineFactoryAndVersionParser(t *testing.T) {
	root := t.TempDir()

	for _, name := range []string{EngineCaddy, EngineAngie, EngineFerron} {
		if !IsValidEngineName(name) {
			t.Fatalf("expected IsValidEngineName(%q)=true", name)
		}
	}
	if IsValidEngineName("nginx") {
		t.Fatalf("expected IsValidEngineName(nginx)=false")
	}

	eDefault, err := NewEngine("", &Config{}, root)
	if err != nil || eDefault.Name() != EngineCaddy {
		t.Fatalf("expected default engine caddy, got %v err=%v", eDefault, err)
	}
	eAngie, err := NewEngine(EngineAngie, &Config{ACMEEmail: "ops@example.com"}, root)
	if err != nil || eAngie.Name() != EngineAngie {
		t.Fatalf("expected angie engine, got %v err=%v", eAngie, err)
	}
	if !strings.HasPrefix(eAngie.Paths().MainConfig, root) {
		t.Fatalf("expected rooted angie path under %q, got %q", root, eAngie.Paths().MainConfig)
	}
	eFerron, err := NewEngine(EngineFerron, &Config{ACMEEmail: "ops@example.com"}, root)
	if err != nil || eFerron.Name() != EngineFerron {
		t.Fatalf("expected ferron engine, got %v err=%v", eFerron, err)
	}
	if !strings.HasPrefix(eFerron.Paths().MainConfig, root) {
		t.Fatalf("expected rooted ferron path under %q, got %q", root, eFerron.Paths().MainConfig)
	}
	if _, err := NewEngine("haproxy", nil, root); err == nil {
		t.Fatalf("expected unknown engine to return error")
	}

	if got := parseCaddyVersionString("{\"level\":\"info\"}\nv2.8.4 h1:xyz\n"); got != "v2.8.4" {
		t.Fatalf("unexpected parseCaddyVersionString result: %q", got)
	}
}
