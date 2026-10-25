package handlers

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"panel/internal/db"
	"panel/internal/phphosting"
	"panel/internal/workspace"
)

// Deleting a static PHP-hosting site must remove the app, its site row, its cron jobs
// (by cascade) and the files on disk, and must leave other sites alone.
func TestDeletePHPSiteResourcesRemovesEverything(t *testing.T) {
	dir := t.TempDir()
	store, err := db.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	p := &Panel{DB: store, Store: workspace.NewStore(dir), WorkspacesRoot: dir}
	p.PHP = phphosting.NewManager(store, dir)
	ctx := context.Background()
	owner, err := store.CreateUser(ctx, "owner", "hash", db.RoleUser)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"site-a", "site-b"} {
		site := &db.PHPSite{AppID: id, OwnerID: owner, UID: 20001, SiteType: phphosting.SiteTypeStatic, RootDir: "htdocs"}
		if err := store.CreateAppAtomic(ctx, id, id, owner, 0, site); err != nil {
			t.Fatalf("create %s: %v", id, err)
		}
		if err := os.MkdirAll(filepath.Join(p.Store.Path(id), "htdocs"), 0o750); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.CreatePHPCronJob(ctx, db.PHPCronJob{AppID: "site-a", Schedule: "* * * * *", Command: "php -v", Enabled: true}); err != nil {
		t.Fatalf("cron: %v", err)
	}

	if err := p.DeleteAppResources(ctx, "site-a"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := store.GetApp(ctx, "site-a"); err == nil {
		t.Error("app row still exists")
	}
	if _, err := store.GetPHPSite(ctx, "site-a"); err == nil {
		t.Error("php site row still exists")
	}
	if jobs, _ := store.ListPHPCronJobs(ctx, "site-a"); len(jobs) != 0 {
		t.Errorf("cron jobs left behind: %d", len(jobs))
	}
	if _, err := os.Stat(p.Store.Path("site-a")); !os.IsNotExist(err) {
		t.Error("site files still on disk")
	}
	if _, err := store.GetPHPSite(ctx, "site-b"); err != nil {
		t.Errorf("other site was touched: %v", err)
	}
}
