package db

import (
	"context"
	"path/filepath"
	"testing"
)

func TestListAllAppDomainsDeterministicOrder(t *testing.T) {
	ctx := context.Background()
	store, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	// Empty DB returns empty slice without error.
	empty, err := store.ListAllAppDomains(ctx)
	if err != nil {
		t.Fatalf("ListAllAppDomains(empty): %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("expected 0 domains, got %d", len(empty))
	}

	uid, err := store.CreateUser(ctx, "owner", "hash", RoleAdmin)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := store.CreateApp(ctx, "app-b", "App B", uid); err != nil {
		t.Fatalf("CreateApp app-b: %v", err)
	}
	if err := store.CreateApp(ctx, "app-a", "App A", uid); err != nil {
		t.Fatalf("CreateApp app-a: %v", err)
	}

	// Insert app-b domains first, then app-a domains, and promote the second domain of app-a to primary.
	if _, err := store.CreateAppDomain(ctx, AppDomain{AppID: "app-b", Domain: "b1.example.com", Service: "web", Port: 3000, EnableHTTPS: true}); err != nil {
		t.Fatalf("CreateAppDomain b1: %v", err)
	}
	if _, err := store.CreateAppDomain(ctx, AppDomain{AppID: "app-a", Domain: "a1-secondary.example.com", Service: "web", Port: 3000, EnableHTTPS: true}); err != nil {
		t.Fatalf("CreateAppDomain a1: %v", err)
	}
	if _, err := store.CreateAppDomain(ctx, AppDomain{AppID: "app-a", Domain: "a2-primary.example.com", Service: "web", Port: 3000, EnableHTTPS: true, IsPrimary: true}); err != nil {
		t.Fatalf("CreateAppDomain a2: %v", err)
	}
	if _, err := store.CreateAppDomain(ctx, AppDomain{AppID: "app-a", Domain: "a3-secondary.example.com", Service: "web", Port: 3000, EnableHTTPS: true}); err != nil {
		t.Fatalf("CreateAppDomain a3: %v", err)
	}

	all, err := store.ListAllAppDomains(ctx)
	if err != nil {
		t.Fatalf("ListAllAppDomains: %v", err)
	}
	wantDomains := []string{
		"a2-primary.example.com",   // app-a, is_primary=1
		"a1-secondary.example.com", // app-a, is_primary=0, lower id
		"a3-secondary.example.com", // app-a, is_primary=0, higher id
		"b1.example.com",           // app-b
	}
	if len(all) != len(wantDomains) {
		t.Fatalf("got %d domains, want %d: %+v", len(all), len(wantDomains), all)
	}
	for i, want := range wantDomains {
		if all[i].Domain != want {
			t.Fatalf("all[%d].Domain = %q, want %q", i, all[i].Domain, want)
		}
	}
}

func TestProxyApplyHistory(t *testing.T) {
	ctx := context.Background()
	store, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	// Empty state.
	latest, err := store.LatestProxyApply(ctx)
	if err != nil {
		t.Fatalf("LatestProxyApply(empty): %v", err)
	}
	if latest != nil {
		t.Fatalf("expected nil latest on empty table, got %+v", latest)
	}

	// Reject missing driver or status.
	if _, err := store.RecordProxyApply(ctx, "gen1", "", "ok", ""); err == nil {
		t.Fatal("expected error when driver is empty")
	}
	if _, err := store.RecordProxyApply(ctx, "gen1", "caddy", "", ""); err == nil {
		t.Fatal("expected error when status is empty")
	}

	id1, err := store.RecordProxyApply(ctx, "gen-111", "caddy", "ok", "")
	if err != nil || id1 <= 0 {
		t.Fatalf("RecordProxyApply 1 failed: id=%d err=%v", id1, err)
	}
	id2, err := store.RecordProxyApply(ctx, "gen-222", "angie", "rolled_back", "angie -t failed")
	if err != nil || id2 <= id1 {
		t.Fatalf("RecordProxyApply 2 failed: id=%d err=%v", id2, err)
	}
	id3, err := store.RecordProxyApply(ctx, "gen-333", "angie", "ok", "")
	if err != nil || id3 <= id2 {
		t.Fatalf("RecordProxyApply 3 failed: id=%d err=%v", id3, err)
	}

	latest, err = store.LatestProxyApply(ctx)
	if err != nil || latest == nil {
		t.Fatalf("LatestProxyApply: latest=%+v err=%v", latest, err)
	}
	if latest.ID != id3 || latest.Generation != "gen-333" || latest.Driver != "angie" || latest.Status != "ok" || latest.AppliedAt.IsZero() {
		t.Fatalf("unexpected latest proxy apply: %+v", latest)
	}

	list, err := store.ListProxyApplies(ctx, 2)
	if err != nil {
		t.Fatalf("ListProxyApplies: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(list))
	}
	if list[0].ID != id3 || list[1].ID != id2 || list[1].Error != "angie -t failed" {
		t.Fatalf("unexpected ListProxyApplies order/content: %+v", list)
	}
}

func TestProxyEngineSeedingAndPreservation(t *testing.T) {
	ctx := context.Background()

	cases := []struct {
		name string
		env  string
		want string
	}{
		{name: "unset defaults to caddy", env: "", want: "caddy"},
		{name: "valid caddy", env: "caddy", want: "caddy"},
		{name: "valid angie", env: "angie", want: "angie"},
		{name: "valid ferron trimmed uppercase", env: "  FERRON  ", want: "ferron"},
		{name: "invalid falls back to caddy", env: "haproxy", want: "caddy"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("ND_PROXY_ENGINE", tc.env)
			dbPath := filepath.Join(t.TempDir(), "panel.db")
			store, err := Open(dbPath)
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer store.Close()

			if got := store.GetSetting(ctx, SettingProxyEngine); got != tc.want {
				t.Fatalf("seeded proxy_engine = %q, want %q", got, tc.want)
			}
		})
	}

	t.Run("does not overwrite existing setting on reopen or upgrade", func(t *testing.T) {
		dbPath := filepath.Join(t.TempDir(), "upgrade.db")

		// Simulate an existing install seeded with caddy.
		t.Setenv("ND_PROXY_ENGINE", "")
		s1, err := Open(dbPath)
		if err != nil {
			t.Fatalf("Open 1: %v", err)
		}
		if got := s1.GetSetting(ctx, SettingProxyEngine); got != "caddy" {
			_ = s1.Close()
			t.Fatalf("initial proxy_engine = %q, want caddy", got)
		}
		if err := s1.Close(); err != nil {
			t.Fatalf("Close 1: %v", err)
		}

		// Reopen with ND_PROXY_ENGINE=angie: existing "caddy" must NOT be overwritten.
		t.Setenv("ND_PROXY_ENGINE", "angie")
		s2, err := Open(dbPath)
		if err != nil {
			t.Fatalf("Open 2: %v", err)
		}
		if got := s2.GetSetting(ctx, SettingProxyEngine); got != "caddy" {
			_ = s2.Close()
			t.Fatalf("existing caddy was overwritten to %q", got)
		}
		// Switch explicitly to angie, then reopen with ND_PROXY_ENGINE=ferron -> stays angie.
		if err := s2.SetSetting(ctx, SettingProxyEngine, "angie"); err != nil {
			_ = s2.Close()
			t.Fatalf("SetSetting angie: %v", err)
		}
		if err := s2.Close(); err != nil {
			t.Fatalf("Close 2: %v", err)
		}

		t.Setenv("ND_PROXY_ENGINE", "ferron")
		s3, err := Open(dbPath)
		if err != nil {
			t.Fatalf("Open 3: %v", err)
		}
		defer s3.Close()
		if got := s3.GetSetting(ctx, SettingProxyEngine); got != "angie" {
			t.Fatalf("existing angie was overwritten to %q", got)
		}
	})
}
