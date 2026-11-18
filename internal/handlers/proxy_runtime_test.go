package handlers

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"panel/internal/db"
	"panel/internal/proxy"
	"panel/internal/proxy/angie"
	"panel/internal/proxy/ferron"

	"gopkg.in/yaml.v3"
)

func openProxyTestStore(t *testing.T) *db.Store {
	t.Helper()
	store, err := db.Open(filepath.Join(t.TempDir(), "proxy_rt.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

// testDriverFactory avoids touching /data/proxy/* during tests.
func testDriverFactory(name string) (proxy.Driver, error) {
	switch normalizeProxyEngine(name) {
	case "angie":
		return angie.New(), nil
	case "ferron":
		return ferron.New(), nil
	}
	return newProxyDriver(name)
}

func TestProxyEngineDefaultsToCaddyAndKeepsLabelFlow(t *testing.T) {
	t.Setenv("ND_PROXY_ENGINE", "")
	p := &Panel{DB: openProxyTestStore(t)}
	if got := p.ProxyEngine(); got != "caddy" {
		t.Fatalf("ProxyEngine() = %q, want caddy", got)
	}
	if p.proxyAppLabeler() != nil {
		t.Fatal("Caddy must keep the historic label generator (nil labeler)")
	}
	if p.proxyServiceKey() != "caddy" {
		t.Fatalf("proxyServiceKey() = %q, want caddy", p.proxyServiceKey())
	}
	if p.proxySyncer() != nil {
		t.Fatal("Caddy must not create a file-rendering Syncer")
	}
	res, err := p.ProxySyncNow(context.Background(), proxy.Reason{Kind: "test"}, true)
	if err != nil || res.Driver != "caddy" {
		t.Fatalf("ProxySyncNow on caddy = %+v, %v", res, err)
	}
}

func TestProxyEngineReadsSetting(t *testing.T) {
	store := openProxyTestStore(t)
	if err := store.SetSetting(context.Background(), db.SettingProxyEngine, "angie"); err != nil {
		t.Fatal(err)
	}
	p := &Panel{DB: store}
	p.proxyRT.newDriverFn = testDriverFactory
	if got := p.ProxyEngine(); got != "angie" {
		t.Fatalf("ProxyEngine() = %q, want angie", got)
	}
	if p.proxyServiceKey() != "nd-proxy" {
		t.Fatalf("proxyServiceKey() = %q, want nd-proxy", p.proxyServiceKey())
	}
	if p.proxyAppLabeler() == nil {
		t.Fatal("Angie must provide an app labeler (upstream discovery labels)")
	}
}

const installerAngieCompose = `services:
    nd-proxy:
        container_name: nd-proxy
        image: docker.angie.software/angie:1.12.4
        environment:
            KEEP_ME: "1"
    panel:
        image: masudranaxpert/nextdeploy:latest
        labels:
            caddy: panel.example.com
networks:
    nextdeploy:
        name: NextDeploy
`

func writeRootCompose(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "docker-compose.yml")
	if err := os.WriteFile(path, []byte(content), 0o640); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PANEL_STACK_COMPOSE_FILE", path)
	return path
}

func composeServices(t *testing.T, raw []byte) map[string]map[string]any {
	t.Helper()
	var doc struct {
		Services map[string]map[string]any `yaml:"services"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("yaml: %v\n%s", err, raw)
	}
	return doc.Services
}

// Regression: with Angie the root stack has no "caddy" service; the root sync used to fail
// with "compose file has no caddy service" (panel domain, options and gateway ports broken).
func TestRootComposeSyncForAngieEngine(t *testing.T) {
	path := writeRootCompose(t, installerAngieCompose)
	store := openProxyTestStore(t)
	ctx := context.Background()
	_ = store.SetSetting(ctx, db.SettingProxyEngine, "angie")
	p := &Panel{DB: store, applyRootStackComposeFn: func(string, ...string) {}}
	p.proxyRT.newDriverFn = testDriverFactory

	if err := p.syncRootStackCompose(ctx); err != nil {
		t.Fatalf("syncRootStackCompose(angie): %v", err)
	}
	raw, _ := os.ReadFile(path)
	svcs := composeServices(t, raw)
	if _, ok := svcs["caddy"]; ok {
		t.Fatalf("caddy service must not be added for angie:\n%s", raw)
	}
	np, ok := svcs["nd-proxy"]
	if !ok {
		t.Fatalf("nd-proxy service missing:\n%s", raw)
	}
	if img, _ := np["image"].(string); !strings.Contains(img, "angie") {
		t.Fatalf("nd-proxy image = %q, want angie", img)
	}
	if !strings.Contains(string(raw), "KEEP_ME") {
		t.Fatalf("existing proxy environment must be preserved:\n%s", raw)
	}
	if strings.Contains(string(raw), "panel.example.com") {
		t.Fatalf("Caddy labels must be stripped from panel for non-Caddy engines:\n%s", raw)
	}
}

func TestRewriteRootComposeBackToCaddy(t *testing.T) {
	path := writeRootCompose(t, installerAngieCompose)
	store := openProxyTestStore(t)
	ctx := context.Background()
	_ = store.SetSetting(ctx, db.SettingProxyEngine, "caddy")
	p := &Panel{DB: store, applyRootStackComposeFn: func(string, ...string) {}}
	p.proxyRT.newDriverFn = testDriverFactory

	if err := p.rewriteRootComposeForEngine(ctx, "caddy"); err != nil {
		t.Fatalf("rewriteRootComposeForEngine(caddy): %v", err)
	}
	raw, _ := os.ReadFile(path)
	svcs := composeServices(t, raw)
	if _, ok := svcs["nd-proxy"]; ok {
		t.Fatalf("nd-proxy must be removed when switching back to caddy:\n%s", raw)
	}
	c, ok := svcs["caddy"]
	if !ok {
		t.Fatalf("caddy service missing:\n%s", raw)
	}
	if img, _ := c["image"].(string); !strings.Contains(img, "caddy") {
		t.Fatalf("caddy image = %q", img)
	}
}

// fakeDriver records which lifecycle steps the Syncer invoked.
type fakeDriver struct {
	renders, validates, applies atomic.Int32
}

func (f *fakeDriver) Name() string                                                { return "angie" }
func (f *fakeDriver) ContainerName() string                                       { return "nd-proxy" }
func (f *fakeDriver) Capabilities() proxy.Capabilities                            { return proxy.Capabilities{} }
func (f *fakeDriver) AppLabels([]proxy.Site, proxy.UpstreamRef) map[string]string { return nil }
func (f *fakeDriver) StackService(proxy.StackOptions) (map[string]any, map[string]any, error) {
	return map[string]any{"image": "fake"}, nil, nil
}
func (f *fakeDriver) Render(*proxy.State) (proxy.Files, error) {
	f.renders.Add(1)
	return proxy.Files{"angie.conf": "events {}\n"}, nil
}
func (f *fakeDriver) Validate(context.Context, string) error { f.validates.Add(1); return nil }
func (f *fakeDriver) Apply(context.Context, proxy.Files) (proxy.ApplyResult, error) {
	f.applies.Add(1)
	return proxy.ApplyResult{Driver: "angie", Changed: true, Reloaded: true}, nil
}
func (f *fakeDriver) Status(context.Context) (proxy.Status, error) { return proxy.Status{}, nil }
func (f *fakeDriver) Logs(context.Context, int) ([]string, error)  { return nil, nil }
func (f *fakeDriver) Restart(context.Context) error                { return nil }

func TestProxySyncNowRunsDriverPipelineForAngie(t *testing.T) {
	store := openProxyTestStore(t)
	ctx := context.Background()
	_ = store.SetSetting(ctx, db.SettingProxyEngine, "angie")
	fd := &fakeDriver{}
	p := &Panel{DB: store}
	p.proxyRT.newDriverFn = func(string) (proxy.Driver, error) { return fd, nil }
	p.proxyRT.buildStateFn = func(context.Context) (*proxy.State, error) { return &proxy.State{}, nil }

	if _, err := p.ProxySyncNow(ctx, proxy.Reason{Kind: "test"}, true); err != nil {
		t.Fatalf("ProxySyncNow: %v", err)
	}
	if fd.renders.Load() != 1 || fd.validates.Load() != 1 || fd.applies.Load() != 1 {
		t.Fatalf("render/validate/apply = %d/%d/%d, want 1/1/1", fd.renders.Load(), fd.validates.Load(), fd.applies.Load())
	}
	rec, err := store.LatestProxyApply(ctx)
	if err != nil || rec == nil {
		t.Fatalf("expected a proxy_apply record, got %v, %v", rec, err)
	}
	// Unchanged state must be skipped.
	if _, err := p.ProxySyncNow(ctx, proxy.Reason{Kind: "test"}, false); err != nil {
		t.Fatal(err)
	}
	if fd.applies.Load() != 1 {
		t.Fatalf("unchanged generation re-applied (applies=%d)", fd.applies.Load())
	}
}

// Switching to Ferron from the panel must not leave the driver's "${ND_FERRON_ADMIN_TOKEN:?…}"
// placeholder in the root compose (docker compose would refuse to start the service).
func TestFerronRootComposeGetsConcreteAdminToken(t *testing.T) {
	path := writeRootCompose(t, installerAngieCompose)
	store := openProxyTestStore(t)
	ctx := context.Background()
	_ = store.SetSetting(ctx, db.SettingProxyEngine, "ferron")
	p := &Panel{DB: store, applyRootStackComposeFn: func(string, ...string) {}}
	p.proxyRT.newDriverFn = testDriverFactory
	if err := p.syncRootStackCompose(ctx); err != nil {
		t.Fatalf("syncRootStackCompose(ferron): %v", err)
	}
	raw, _ := os.ReadFile(path)
	tok := existingComposeEnv(raw, "nd-proxy", ferronAdminTokenEnv)
	if tok == "" || strings.HasPrefix(tok, "${") {
		t.Fatalf("expected a concrete admin token, got %q:\n%s", tok, raw)
	}
	if got := store.GetSetting(ctx, settingFerronAdminToken); got != tok {
		t.Fatalf("token not persisted (setting %q, compose %q)", got, tok)
	}
	// A second sync keeps the same token.
	if err := p.syncRootStackCompose(ctx); err != nil {
		t.Fatal(err)
	}
	raw2, _ := os.ReadFile(path)
	if tok2 := existingComposeEnv(raw2, "nd-proxy", ferronAdminTokenEnv); tok2 != tok {
		t.Fatalf("token changed between syncs: %q -> %q", tok, tok2)
	}
}

func TestHostPathMismatches(t *testing.T) {
	orig := inspectSelfMounts
	defer func() { inspectSelfMounts = orig }()
	ctx := context.Background()

	inspectSelfMounts = func(context.Context) ([]selfMount, error) {
		return []selfMount{{Source: "/opt/nextdeploy/data", Destination: "/data"}, {Source: "/var/run/docker.sock", Destination: "/var/run/docker.sock"}}, nil
	}
	p := &Panel{}
	bad, remapped := p.hostPathMismatches(ctx, []string{"/data/proxy/angie", "/data/workspaces"})
	if len(bad) != 0 || len(remapped) != 2 || !strings.Contains(remapped[0], "/opt/nextdeploy/data/proxy/angie") {
		t.Fatalf("legacy ./data layout must be remapped, got bad=%v remapped=%v", bad, remapped)
	}
	if got := p.hostPathOf("/data/proxy/angie/staged/x"); got != "/opt/nextdeploy/data/proxy/angie/staged/x" {
		t.Fatalf("hostPathOf = %q", got)
	}
	svc := map[string]any{"volumes": []any{"/var/run/docker.sock:/var/run/docker.sock", "/data/proxy/angie:/etc/angie/nd:ro", "angie_acme:/var/lib/angie/acme"}}
	translateServiceVolumes(svc, p.hostPathOf)
	vols := svc["volumes"].([]any)
	if vols[1] != "/opt/nextdeploy/data/proxy/angie:/etc/angie/nd:ro" || vols[2] != "angie_acme:/var/lib/angie/acme" || vols[0] != "/var/run/docker.sock:/var/run/docker.sock" {
		t.Fatalf("volumes not translated: %v", vols)
	}

	if bad, _ := p.hostPathMismatches(ctx, []string{"/etc/panel-only"}); len(bad) != 1 {
		t.Fatalf("a path on no mount must be reported, got %v", bad)
	}

	inspectSelfMounts = func(context.Context) ([]selfMount, error) {
		return []selfMount{{Source: "/data", Destination: "/data"}}, nil
	}
	p = &Panel{}
	if bad, remapped := p.hostPathMismatches(ctx, []string{"/data/proxy/angie", "/data/workspaces"}); len(bad) != 0 || len(remapped) != 0 {
		t.Fatalf("standard /data:/data layout must pass silently, got %v %v", bad, remapped)
	}

	inspectSelfMounts = func(context.Context) ([]selfMount, error) { return nil, nil }
	p = &Panel{}
	if bad, _ := p.hostPathMismatches(ctx, []string{"/data/proxy/angie"}); len(bad) != 0 {
		t.Fatalf("outside a container the check is skipped, got %v", bad)
	}
	if got := p.hostPathOf("/data/x"); got != "/data/x" {
		t.Fatalf("outside a container paths stay as-is, got %q", got)
	}
}
