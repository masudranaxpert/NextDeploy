package state

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strconv"
	"testing"

	"panel/internal/db"
	"panel/internal/proxy"
)

func openTestDB(t *testing.T) *db.Store {
	t.Helper()
	store, err := db.Open(filepath.Join(t.TempDir(), "state_load_test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func seedFullLoadTestDB(t *testing.T, ctx context.Context, store *db.Store) (panelID string) {
	t.Helper()

	adminID, err := store.CreateUser(ctx, "admin", "hash", db.RoleAdmin)
	if err != nil {
		t.Fatalf("CreateUser admin: %v", err)
	}
	userID, err := store.CreateUser(ctx, "alice", "hash", db.RoleUser)
	if err != nil {
		t.Fatalf("CreateUser alice: %v", err)
	}
	suspID, err := store.CreateUser(ctx, "bob", "hash", db.RoleUser)
	if err != nil {
		t.Fatalf("CreateUser bob: %v", err)
	}
	if err := store.UpdateUserStatus(ctx, suspID, db.UserStatusSuspended); err != nil {
		t.Fatalf("UpdateUserStatus: %v", err)
	}

	s1ID, err := store.CreateShieldNode(ctx, &db.ShieldNode{Name: "edge-1", IP: "198.51.100.10", Token: "shld_1"})
	if err != nil {
		t.Fatalf("CreateShieldNode 1: %v", err)
	}
	s2ID, err := store.CreateShieldNode(ctx, &db.ShieldNode{Name: "edge-2", IP: "198.51.100.20", Token: "shld_2"})
	if err != nil {
		t.Fatalf("CreateShieldNode 2: %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `UPDATE users SET default_shield_id = ? WHERE id = ?`, s2ID, userID); err != nil {
		t.Fatalf("set user default_shield_id: %v", err)
	}

	panelID, err = store.EnsurePanelID(ctx)
	if err != nil {
		t.Fatalf("EnsurePanelID: %v", err)
	}
	for k, v := range map[string]string{
		"panel_domain":              "panel.example.com",
		"panel_enable_https":        "true",
		"panel_enable_www":          "true",
		"panel_shield_node_id":      strconv.FormatInt(s1ID, 10),
		"caddy_shared_mount_prefix": "/mnt/shared",
		"caddy_shared_volume_names": `["vol_b","vol_a"]`,
		db.SettingProxyEngine:       "angie",
	} {
		if err := store.SetSetting(ctx, k, v); err != nil {
			t.Fatalf("SetSetting(%s): %v", k, err)
		}
	}

	globalJSON, _ := json.Marshal(proxy.GlobalOptions{
		Profile:       "custom",
		ReadHeader:    "15s",
		Idle:          "2m",
		RealIP:        "cloudflare",
		TrustedStrict: true,
	})
	for k, v := range map[string]string{
		"global_options": string(globalJSON),
		"email":          "ops@example.com",
		"admin_api":      "http://127.0.0.1:2019",
	} {
		if err := store.SetCaddyConfig(ctx, k, v); err != nil {
			t.Fatalf("SetCaddyConfig(%s): %v", k, err)
		}
	}

	if err := store.CreateApp(ctx, "node-app", "node-app", adminID); err != nil {
		t.Fatalf("CreateApp node-app: %v", err)
	}
	if err := store.UpdateAppSlots(ctx, "node-app", "blue", "green"); err != nil {
		t.Fatalf("UpdateAppSlots: %v", err)
	}
	if err := store.UpdateAppPinnedSlot(ctx, "node-app", "blue"); err != nil {
		t.Fatalf("UpdateAppPinnedSlot: %v", err)
	}
	pvJSON, _ := json.Marshal(AppPreviewConfig{Provider: "sslip.io", Service: "web", Port: 3000})
	if err := store.SetSetting(ctx, "preview:node-app", string(pvJSON)); err != nil {
		t.Fatalf("SetSetting preview:node-app: %v", err)
	}

	if err := store.CreateApp(ctx, "shield-app", "shield-app", userID); err != nil {
		t.Fatalf("CreateApp shield-app: %v", err)
	}
	if err := store.UpdateAppSlots(ctx, "shield-app", "green", "blue"); err != nil {
		t.Fatalf("UpdateAppSlots shield-app: %v", err)
	}
	if err := store.UpdateAppPinnedSlot(ctx, "shield-app", "green"); err != nil {
		t.Fatalf("UpdateAppPinnedSlot shield-app: %v", err)
	}
	if err := store.SetSetting(ctx, "preview:shield-app", string(pvJSON)); err != nil {
		t.Fatalf("SetSetting preview:shield-app: %v", err)
	}

	if _, err := store.CreateAppDomain(ctx, db.AppDomain{
		AppID: "node-app", Domain: "app.example.com", Service: "web", Port: 3000,
		EnableHTTPS: true, EnableWWW: true, TrustedProxy: true,
		ServeStatic: true, StaticPath: "dist", StaticURLPrefix: "/assets",
	}); err != nil {
		t.Fatalf("CreateAppDomain node-app: %v", err)
	}
	if _, err := store.CreateAppDomain(ctx, db.AppDomain{
		AppID: "shield-app", Domain: "edge.example.com", Service: "web", Port: 3000,
		EnableHTTPS: true, ShieldNodeID: &s2ID,
	}); err != nil {
		t.Fatalf("CreateAppDomain shield-app: %v", err)
	}

	wpSite := &db.PHPSite{
		SiteType:     "wordpress",
		PHPVersion:   "8.3",
		SettingsJSON: `{"routing":{"fallback_mode":"custom","fallback_entry":"/index.php","rules":[{"id":"r1","kind":"block","match_type":"exact","pattern":"/secret","status":403,"enabled":true}]}}`,
	}
	if err := store.CreateAppAtomic(ctx, "wp-site", "wp-site", adminID, 0, wpSite); err != nil {
		t.Fatalf("CreateAppAtomic wp-site: %v", err)
	}
	if _, err := store.CreateAppDomain(ctx, db.AppDomain{
		AppID: "wp-site", Domain: "wp.example.com", Service: "php", Port: 80, EnableHTTPS: true,
	}); err != nil {
		t.Fatalf("CreateAppDomain wp-site: %v", err)
	}
	secJSON, _ := json.Marshal(proxy.SecuritySpec{
		BlockedIPs:    []string{"1.2.3.4"},
		BlockXMLRPC:   true,
		BasicAuthUser: "admin",
		BasicAuthHash: "$2y$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy",
	})
	if err := store.SetPHPSecurityJSON(ctx, "wp-site", string(secJSON)); err != nil {
		t.Fatalf("SetPHPSecurityJSON: %v", err)
	}

	staticSite := &db.PHPSite{SiteType: "static", RootDir: "dist", SettingsJSON: `{}`}
	if err := store.CreateAppAtomic(ctx, "static-site", "static-site", adminID, 0, staticSite); err != nil {
		t.Fatalf("CreateAppAtomic static-site: %v", err)
	}
	if _, err := store.CreateAppDomain(ctx, db.AppDomain{
		AppID: "static-site", Domain: "static.example.com", Service: "php", Port: 80, EnableHTTPS: true,
	}); err != nil {
		t.Fatalf("CreateAppDomain static-site: %v", err)
	}

	suspSite := &db.PHPSite{SiteType: "php", PHPVersion: "8.2", SettingsJSON: `{}`}
	if err := store.CreateAppAtomic(ctx, "susp-site", "susp-site", suspID, 0, suspSite); err != nil {
		t.Fatalf("CreateAppAtomic susp-site: %v", err)
	}
	if _, err := store.CreateAppDomain(ctx, db.AppDomain{
		AppID: "susp-site", Domain: "susp.example.com", Service: "php", Port: 80, EnableHTTPS: true,
	}); err != nil {
		t.Fatalf("CreateAppDomain susp-site: %v", err)
	}

	if _, err := store.SaveProxyRoute(ctx, db.ProxyRoute{
		Kind: "proxy", Domain: "ext.example.com", Target: "https://10.0.0.50:8443",
		EnableHTTPS: true, SkipTLSVerify: true, Enabled: true,
	}); err != nil {
		t.Fatalf("SaveProxyRoute 1: %v", err)
	}
	if _, err := store.SaveProxyRoute(ctx, db.ProxyRoute{
		Kind: "redirect", Domain: "off.example.com", Target: "https://example.com",
		RedirectCode: 301, EnableHTTPS: true, Enabled: false,
	}); err != nil {
		t.Fatalf("SaveProxyRoute 2: %v", err)
	}

	if err := store.SyncAppPorts(ctx, "node-app", []db.AllocatedPort{
		{Port: 4013, BindIP: "0.0.0.0", AppID: "node-app", Service: "web", Protocol: "tcp", Direct: false},
		{Port: 5432, BindIP: "0.0.0.0", AppID: "node-app", Service: "db", Protocol: "tcp", Direct: true},
	}); err != nil {
		t.Fatalf("SyncAppPorts: %v", err)
	}

	return panelID
}
