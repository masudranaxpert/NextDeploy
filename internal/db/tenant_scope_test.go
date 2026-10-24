package db

import (
	"context"
	"path/filepath"
	"testing"
)

// Resources are owned by whoever created them: a regular user's lists contain only their own
// rows (rows without an owner are admin-only), while the admin lists (nil owner) see everything.
func TestListsAreScopedToTheOwner(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	admin, _ := s.CreateUser(ctx, "admin", "x", RoleAdmin)
	alice, _ := s.CreateUser(ctx, "alice", "x", RoleUser)
	bob, _ := s.CreateUser(ctx, "bob", "x", RoleUser)

	for _, owner := range []*int64{&admin, &alice, nil} { // nil = legacy row created before ownership was explicit
		name := "dest"
		if owner != nil {
			name = "dest-" + map[int64]string{admin: "admin", alice: "alice"}[*owner]
		} else {
			name = "dest-legacy"
		}
		if _, err := s.CreateBackupDestination(ctx, owner, name, "r2", "{}"); err != nil {
			t.Fatal(err)
		}
		if _, err := s.CreateGitProvider(ctx, owner, "gp-"+name, "github", "tok", ""); err != nil {
			t.Fatal(err)
		}
	}

	// registries: owner-only too; legacy ownerless rows move to the first admin
	if _, err := s.db.Exec(`INSERT INTO private_registries (user_id, name, server_address, username, password_encrypted, created_at) VALUES (NULL,'legacy','r.io','u','x','2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if err := s.ClaimOrphanedRegistries(ctx); err != nil {
		t.Fatal(err)
	}
	if r, _ := s.ListPrivateRegistries(ctx, &alice); len(r) != 0 {
		t.Fatalf("alice must not see the legacy shared registry: %v", r)
	}
	if r, _ := s.ListPrivateRegistries(ctx, &admin); len(r) != 1 || r[0].Name != "legacy" {
		t.Fatalf("legacy registry must now belong to the admin: %v", r)
	}

	names := func(ownerID *int64) (dests, provs []string) {
		d, _ := s.ListBackupDestinations(ctx, ownerID)
		for _, x := range d {
			dests = append(dests, x.Name)
		}
		p, _ := s.ListGitProviders(ctx, ownerID)
		for _, x := range p {
			provs = append(provs, x.Name)
		}
		return
	}
	if d, p := names(&alice); len(d) != 1 || d[0] != "dest-alice" || len(p) != 1 || p[0] != "gp-dest-alice" {
		t.Fatalf("alice must see only her own rows: %v %v", d, p)
	}
	if d, p := names(&bob); len(d) != 0 || len(p) != 0 {
		t.Fatalf("bob owns nothing and must see nothing (not the admin's, not legacy rows): %v %v", d, p)
	}
	if d, p := names(nil); len(d) != 3 || len(p) != 3 {
		t.Fatalf("admin (nil) sees everything: %v %v", d, p)
	}
}

// A user's audit trail holds their own actions and whatever was done to their apps (by anyone);
// nothing about other users.
func TestAuditLogsAreScopedToTheUser(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	add := func(uid int64, who, action, ttype, tid string) {
		if err := s.CreateAuditLog(ctx, AuditLog{UserID: uid, Username: who, Action: action, TargetType: ttype, TargetID: tid}); err != nil {
			t.Fatal(err)
		}
	}
	add(1, "admin", "login", "user", "1")
	add(2, "alice", "deploy", "app", "alice-app")
	add(1, "admin", "suspend_app", "app", "alice-app") // admin acting on alice's app: alice may see it
	add(3, "bob", "deploy", "app", "bob-app")
	add(1, "admin", "prune_images", "system", "docker")

	got, err := s.ListAuditLogsForUser(ctx, 2, []string{"alice-app"}, 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("alice must see exactly her own action and the admin action on her app, got %d: %+v", len(got), got)
	}
	for _, l := range got {
		if l.Username == "bob" || l.Action == "prune_images" || l.Action == "login" {
			t.Fatalf("leaked entry: %+v", l)
		}
	}
	if n, _ := s.CountAuditLogsForUser(ctx, 2, []string{"alice-app"}); n != 2 {
		t.Fatalf("count = %d", n)
	}
	if none, _ := s.ListAuditLogsForUser(ctx, 9, nil, 50, 0); len(none) != 0 {
		t.Fatalf("a user with no activity sees nothing: %+v", none)
	}
}

// The first PHP schema forced one user per database; it must upgrade to databases and users
// joined by grants without losing the existing pair.
func TestPHPDatabaseUsersMigration(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "panel.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	uid, _ := s.CreateUser(ctx, "u", "x", RoleUser)
	if err := s.CreateApp(ctx, "app-1", "app", uid); err != nil {
		t.Fatal(err)
	}
	// recreate the legacy layout with one pair in it
	for _, q := range []string{
		`DROP TABLE php_db_grants`, `DROP TABLE php_db_users`, `DROP TABLE php_databases`,
		`CREATE TABLE php_databases (id INTEGER PRIMARY KEY AUTOINCREMENT, app_id TEXT NOT NULL, db_name TEXT NOT NULL UNIQUE, db_user TEXT NOT NULL UNIQUE, created_at TEXT NOT NULL, FOREIGN KEY(app_id) REFERENCES apps(id) ON DELETE CASCADE)`,
		`INSERT INTO php_databases (app_id, db_name, db_user, created_at) VALUES ('app-1','u1_shop','u1_shop','2026-01-01T00:00:00Z')`,
	} {
		if _, err := s.db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	s.Close()

	s, err = Open(path) // runs the migration
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	dbs, _ := s.ListPHPDatabases(ctx, "app-1")
	users, _ := s.ListPHPDBUsers(ctx, "app-1")
	grants, _ := s.ListPHPGrants(ctx, "app-1")
	if len(dbs) != 1 || len(users) != 1 || users[0].UserName != "u1_shop" || len(grants) != 1 || grants[0].Access != "all" {
		t.Fatalf("legacy pair not migrated: dbs=%v users=%v grants=%v", dbs, users, grants)
	}
	// databases can now exist without a user, several at once
	for _, n := range []string{"u1_a", "u1_b"} {
		if _, err := s.CreatePHPDatabase(ctx, PHPDatabase{AppID: "app-1", DBName: n}); err != nil {
			t.Fatalf("user-less database must be allowed: %v", err)
		}
	}
}
