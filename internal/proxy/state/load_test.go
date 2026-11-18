package state

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"panel/internal/db"
	"panel/internal/proxy"
)

func TestLoadBuildInputAndBuildEndToEnd(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)
	panelID := seedFullLoadTestDB(t, ctx, store)

	opts := LoadOptions{
		ResolvePublicIP: func(context.Context) string { return "203.0.113.50" },
		ResolveAppTarget: func(_ context.Context, app db.App) (AppTarget, error) {
			if app.ID == "node-app" {
				return AppTarget{
					ServiceContainers: map[string]string{"web": "custom-node-web"},
					GatewayBindings:   []proxy.HostPortBinding{{HostPort: 4013, ContainerPort: 3000, Service: "web"}},
				}, nil
			}
			return AppTarget{}, nil
		},
	}

	in, err := LoadBuildInput(ctx, store, opts)
	if err != nil {
		t.Fatalf("LoadBuildInput: %v", err)
	}
	if in.ProxyEngine != "angie" || in.AdminAPI != "http://127.0.0.1:2019" || in.PanelID != panelID {
		t.Fatalf("unexpected panel metadata: engine=%q admin=%q panelID=%q", in.ProxyEngine, in.AdminAPI, in.PanelID)
	}

	st, err := Build(in)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	if !st.Global.DirectCloudflare || !reflect.DeepEqual(st.Global.ShieldProxies, []string{"198.51.100.10", "198.51.100.20"}) {
		t.Fatalf("unexpected Global shield/CF flags: %+v", st.Global)
	}
	if st.ACMEEmail != "ops@example.com" {
		t.Fatalf("ACMEEmail = %q", st.ACMEEmail)
	}
	if st.Panel == nil || !st.Panel.BehindShield || st.Panel.TLS != proxy.TLSInternal || !st.Panel.TrustedProxy {
		t.Fatalf("unexpected Panel site: %+v", st.Panel)
	}
	if len(st.SharedMounts) != 2 || st.SharedMounts[0].VolumeName != "vol_a" || st.SharedMounts[1].VolumeName != "vol_b" {
		t.Fatalf("unexpected SharedMounts: %+v", st.SharedMounts)
	}
	if len(st.Gateways) != 1 || st.Gateways[0].Port != 4013 || st.Gateways[0].ContainerPort != 3000 || st.Gateways[0].Upstream.Container != "custom-node-web" {
		t.Fatalf("unexpected Gateways: %+v", st.Gateways)
	}

	byID := make(map[string]proxy.Site, len(st.Sites))
	for _, s := range st.Sites {
		byID[s.ID] = s
	}

	// Verify dummy app_domain entries for PHP/static sites were excluded from app_proxy.
	for id := range byID {
		if id == "app:wp-site:dom:3" || id == "app:static-site:dom:4" || id == "app:susp-site:dom:5" {
			t.Fatalf("PHP dummy app_domain leaked as Docker site: %s", id)
		}
	}
	if _, ok := byID["route:2"]; ok {
		t.Fatal("disabled proxy route must not be emitted")
	}

	wp := byID["php:wp-site"]
	if wp.Kind != proxy.SiteKindPHP || wp.PHP.Suspended || wp.Security == nil || !wp.Security.BlockXMLRPC {
		t.Fatalf("unexpected wp-site IR: %+v", wp)
	}
	stat := byID["php:static-site"]
	if stat.Kind != proxy.SiteKindStatic || stat.PHP.Socket != "" || stat.PHP.DocRoot != "/data/workspaces/static-site/htdocs/dist" {
		t.Fatalf("unexpected static-site IR: %+v", stat)
	}
	susp := byID["php:susp-site"]
	if susp.PHP == nil || !susp.PHP.Suspended {
		t.Fatalf("expected suspended owner PHP site Suspended=true, got %+v", susp)
	}
	pv := byID["slot:node-app:green"]
	if !pv.IsPreview || pv.BehindShield || pv.Hosts[0] != "green-node-app-203-0-113-50.sslip.io" {
		t.Fatalf("unexpected direct preview site: %+v", pv)
	}
	spv := byID["slot:shield-app:blue"]
	if !spv.IsPreview || !spv.BehindShield || spv.TLS != proxy.TLSInternal {
		t.Fatalf("unexpected shielded preview site: %+v", spv)
	}
	if nodeDom := byID["app:node-app:dom:1"]; len(nodeDom.Paths) != 1 || nodeDom.Paths[0].Handler.Static.Root != "/data/workspaces/node-app/dist" {
		t.Fatalf("expected relative static path resolved against workspace, got %+v", nodeDom.Paths)
	}

	// Legacy CSV fallback for caddy_shared_volume_names must produce identical Generation.
	if err := store.SetSetting(ctx, "caddy_shared_volume_names", "vol_b, vol_a"); err != nil {
		t.Fatalf("SetSetting CSV: %v", err)
	}
	stCSV, err := LoadAndBuild(ctx, store, opts)
	if err != nil {
		t.Fatalf("LoadAndBuild CSV: %v", err)
	}
	if stCSV.Generation != st.Generation {
		t.Fatalf("CSV vs JSON Generation mismatch: %s != %s", stCSV.Generation, st.Generation)
	}
}

func TestLoadBuildInputDefaultsAndErrors(t *testing.T) {
	ctx := context.Background()
	if _, err := LoadBuildInput(ctx, nil, LoadOptions{}); err == nil {
		t.Fatal("expected error for nil store")
	}

	store := openTestDB(t)
	uid, err := store.CreateUser(ctx, "admin", "hash", db.RoleAdmin)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := store.SetCaddyConfig(ctx, "global_options", "{broken-json"); err != nil {
		t.Fatalf("SetCaddyConfig: %v", err)
	}
	if err := store.CreateApp(ctx, "a1", "a1", uid); err != nil {
		t.Fatalf("CreateApp: %v", err)
	}

	in, err := LoadBuildInput(ctx, store, LoadOptions{})
	if err != nil {
		t.Fatalf("LoadBuildInput with malformed global_options: %v", err)
	}
	if in.Global.Profile != "default" || in.Global.RealIP != "none" || !in.PanelEnableHTTPS || in.PanelEnableWWW {
		t.Fatalf("unexpected defaults: %+v", in)
	}
	if in.ProxyEngine != "caddy" {
		t.Fatalf("expected default proxy_engine=caddy, got %q", in.ProxyEngine)
	}

	wantErr := errors.New("resolve boom")
	if _, err := LoadBuildInput(ctx, store, LoadOptions{
		ResolveAppTarget: func(context.Context, db.App) (AppTarget, error) {
			return AppTarget{}, wantErr
		},
	}); !errors.Is(err, wantErr) {
		t.Fatalf("expected ResolveAppTarget error %v, got %v", wantErr, err)
	}
}
