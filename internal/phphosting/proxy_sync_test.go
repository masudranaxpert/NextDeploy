package phphosting

import (
	"context"
	"path/filepath"
	"testing"

	"panel/internal/db"
	"panel/internal/proxy"
)

func openPHPTestStore(t *testing.T) *db.Store {
	t.Helper()
	store, err := db.Open(filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})
	return store
}

func TestIsNonCaddyEngine(t *testing.T) {
	var mgrNil *Manager
	if mgrNil.isNonCaddyEngine() {
		t.Fatal("nil Manager should report false")
	}

	m := &Manager{}
	if m.isNonCaddyEngine() {
		t.Fatal("unset ProxyEngineFunc should report false")
	}

	cases := []struct {
		engine string
		want   bool
	}{
		{engine: "", want: false},
		{engine: "caddy", want: false},
		{engine: " CADDY ", want: false},
		{engine: "angie", want: true},
		{engine: "ferron", want: true},
	}
	for _, tc := range cases {
		m.ProxyEngineFunc = func() string { return tc.engine }
		if got := m.isNonCaddyEngine(); got != tc.want {
			t.Errorf("engine=%q isNonCaddyEngine() = %v, want %v", tc.engine, got, tc.want)
		}
	}
}

func TestNonCaddyProxySyncHooksSkipHolders(t *testing.T) {
	ctx := context.Background()
	store := openPHPTestStore(t)

	uid, err := store.CreateUser(ctx, "owner1", "hash", db.RoleUser)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	site := &db.PHPSite{
		AppID:    "static-1",
		OwnerID:  uid,
		SiteType: SiteTypeStatic,
		RootDir:  "public",
	}
	if err := store.CreateAppAtomic(ctx, "static-1", "static-1", uid, 0, site); err != nil {
		t.Fatalf("CreateAppAtomic: %v", err)
	}

	var calls []proxy.Reason
	m := &Manager{
		DB:             store,
		WorkspacesRoot: t.TempDir(),
	}
	m.SetProxySync(
		func() string { return string(proxy.EngineAngie) },
		func(_ context.Context, reason proxy.Reason) error {
			calls = append(calls, reason)
			return nil
		},
	)

	if err := m.SyncRoutes(ctx, "static-1"); err != nil {
		t.Fatalf("SyncRoutes: %v", err)
	}
	sec := proxy.SiteSecurity{BasicAuthUser: "alice", BasicAuthHash: "$2y$10$abcdefghijklmnopqrstuv"}
	if err := m.SaveSiteSecurity(ctx, "static-1", sec); err != nil {
		t.Fatalf("SaveSiteSecurity: %v", err)
	}
	if got := m.SiteSecurity(ctx, "static-1"); got.BasicAuthUser != "alice" {
		t.Fatalf("SiteSecurity = %+v, want alice", got)
	}
	if err := m.PublishHolder(ctx, "nd-route-custom", map[string]string{"k": "v"}, nil); err != nil {
		t.Fatalf("PublishHolder: %v", err)
	}
	m.RemoveHolder(ctx, "nd-route-custom")
	m.ReloadCaddy(ctx)
	if err := m.TeardownSite(ctx, "static-1"); err != nil {
		t.Fatalf("TeardownSite: %v", err)
	}

	want := []proxy.Reason{
		{Kind: "php_site", ID: "static-1"},
		{Kind: "php_site", ID: "static-1"},
		{Kind: "route_holder", ID: "nd-route-custom"},
		{Kind: "route_holder", ID: "nd-route-custom"},
		{Kind: "php_reload"},
		{Kind: "php_site", ID: "static-1"},
	}
	if len(calls) != len(want) {
		t.Fatalf("got %d proxy sync calls (%+v), want %d (%+v)", len(calls), calls, len(want), want)
	}
	for i := range want {
		if calls[i].Kind != want[i].Kind || calls[i].ID != want[i].ID {
			t.Errorf("call[%d] = %+v, want %+v", i, calls[i], want[i])
		}
	}
}

func TestSiteSettingsValidateForSiteUsesProxyRouting(t *testing.T) {
	s := DefaultSettings()
	s.MaxChildren = 10
	s.Routing = proxy.SiteRouting{
		FallbackMode: proxy.FallbackDefault,
		Rules: []proxy.RoutingRule{
			{Kind: "rewrite", MatchType: "prefix", Pattern: "/app", Target: "/index.php", Enabled: true},
		},
	}
	if err := s.ValidateForSite(Ceilings{MemoryMB: 512}, false); err != nil {
		t.Fatalf("valid PHP routing rejected: %v", err)
	}

	bad := DefaultSettings()
	bad.MaxChildren = 10
	bad.Routing = proxy.SiteRouting{
		FallbackMode: proxy.FallbackDefault,
		Rules: []proxy.RoutingRule{
			{Kind: "rewrite", MatchType: "prefix", Pattern: "/app", Target: "/../.env", Enabled: true},
		},
	}
	if err := bad.ValidateForSite(Ceilings{MemoryMB: 512}, false); err == nil {
		t.Fatal("expected traversal/hidden rewrite target to be rejected")
	}
}
