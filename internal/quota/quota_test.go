package quota

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"panel/internal/db"
)

func openStore(t *testing.T) *db.Store {
	t.Helper()
	s, err := db.Open(filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestCreateAppIsAtomicUnderConcurrency(t *testing.T) {
	ctx := context.Background()
	s := openStore(t)
	uid, err := s.CreateUser(ctx, "alice", "x", db.RoleUser)
	if err != nil {
		t.Fatal(err)
	}
	owner := db.User{ID: uid, Role: db.RoleUser, MaxApps: 3}

	var ok, limited int32
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			err := CreateApp(ctx, s, owner, fmt.Sprintf("app%d-aaaa", i), fmt.Sprintf("app%d", i))
			switch {
			case err == nil:
				atomic.AddInt32(&ok, 1)
			case strings.Contains(err.Error(), "app limit reached"):
				atomic.AddInt32(&limited, 1)
			default:
				t.Errorf("unexpected error (must be a clean limit error, not a lock error): %v", err)
			}
		}(i)
	}
	wg.Wait()
	if ok != 3 || limited != 17 {
		t.Fatalf("expected 3 created and 17 rejected by the limit, got %d created, %d rejected", ok, limited)
	}
	n, _ := s.CountAppsOwnedByUser(ctx, uid)
	if n != 3 {
		t.Fatalf("db has %d apps, want 3", n)
	}
}

func TestAdminIsUnlimitedAndPHPSiteGetsUniqueUIDs(t *testing.T) {
	ctx := context.Background()
	s := openStore(t)
	uid, _ := s.CreateUser(ctx, "root", "x", db.RoleAdmin)
	owner := db.User{ID: uid, Role: db.RoleAdmin, MaxApps: 1}
	seen := map[int]bool{}
	for i := 0; i < 5; i++ {
		site := &db.PHPSite{SiteType: "php", PHPVersion: "8.3"}
		if err := CreatePHPSite(ctx, s, owner, fmt.Sprintf("s%d-bbbb", i), fmt.Sprintf("s%d", i), site); err != nil {
			t.Fatal(err)
		}
		if site.UID < 20000 || seen[site.UID] {
			t.Fatalf("bad or duplicate uid %d", site.UID)
		}
		seen[site.UID] = true
	}
	if !s.IsPHPSite(ctx, "s0-bbbb") {
		t.Fatal("php site row missing")
	}
}

func TestDuplicateNameAndDatabaseLimit(t *testing.T) {
	ctx := context.Background()
	s := openStore(t)
	uid, _ := s.CreateUser(ctx, "bob", "x", db.RoleUser)
	owner := db.User{ID: uid, Role: db.RoleUser, MaxApps: 5}
	if err := CreateApp(ctx, s, owner, "a-1111", "a"); err != nil {
		t.Fatal(err)
	}
	if err := CreateApp(ctx, s, owner, "a-2222", "A"); err == nil {
		t.Fatal("duplicate name (case-insensitive) must be rejected")
	}
	_ = s.SetPHPUserLimits(ctx, db.PHPUserLimits{UserID: uid, MaxDatabases: 1, MaxCronJobs: 1})
	if err := CheckDatabaseSlot(ctx, s, owner); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreatePHPDatabase(ctx, db.PHPDatabase{AppID: "a-1111", DBName: "u1_x", DBUser: "u1_x"}); err != nil {
		t.Fatal(err)
	}
	if err := CheckDatabaseSlot(ctx, s, owner); err == nil {
		t.Fatal("database limit must be enforced")
	}
}

// Names only need to be unique inside one account and one kind of app: another user, or the
// same user's PHP site, can reuse a name, and nobody can tell from the error that it exists.
func TestSameNameAcrossUsersAndKinds(t *testing.T) {
	ctx := context.Background()
	s := openStore(t)
	aid, _ := s.CreateUser(ctx, "alice", "x", db.RoleUser)
	bid, _ := s.CreateUser(ctx, "bob", "x", db.RoleUser)
	alice := db.User{ID: aid, Role: db.RoleUser, MaxApps: 5}
	bob := db.User{ID: bid, Role: db.RoleUser, MaxApps: 5}

	if err := CreateApp(ctx, s, alice, "blog-1111", "blog"); err != nil {
		t.Fatal(err)
	}
	if err := CreateApp(ctx, s, bob, "blog-2222", "blog"); err != nil {
		t.Fatalf("another user must be able to use the same name: %v", err)
	}
	site := db.PHPSite{AppID: "blog-3333", OwnerID: aid, SiteType: "php", PHPVersion: "8.3"}
	if err := CreatePHPSite(ctx, s, alice, "blog-3333", "blog", &site); err != nil {
		t.Fatalf("a PHP site may share a name with a project of the same user: %v", err)
	}
	site2 := db.PHPSite{AppID: "blog-4444", OwnerID: aid, SiteType: "php", PHPVersion: "8.3"}
	if err := CreatePHPSite(ctx, s, alice, "blog-4444", "Blog", &site2); err == nil {
		t.Fatal("two PHP sites of one user must not share a name")
	}
}
