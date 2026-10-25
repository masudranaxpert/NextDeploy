package handlers

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"panel/internal/db"
	"panel/internal/workspace"
)

// Storage counts only the projects a user owns: a project shared with them is the owner's
// usage, and going over the limit refuses new data.
func TestUserStorageCountsOnlyOwnedProjects(t *testing.T) {
	dir := t.TempDir()
	store, err := db.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	p := &Panel{DB: store, Store: workspace.NewStore(dir), WorkspacesRoot: dir}
	ctx := context.Background()
	owner, _ := store.CreateUser(ctx, "owner", "h", db.RoleUser)
	guest, _ := store.CreateUser(ctx, "guest", "h", db.RoleUser)
	if err := store.CreateApp(ctx, "app-1", "app", owner); err != nil {
		t.Fatal(err)
	}
	if err := store.AddCollaborator(ctx, "app-1", guest, db.CollabRoleDeveloper); err != nil {
		t.Fatal(err)
	}
	ws := p.Store.Path("app-1")
	if err := os.MkdirAll(ws, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "data.bin"), []byte(strings.Repeat("x", 3000)), 0o640); err != nil {
		t.Fatal(err)
	}

	if got := p.UserStorage(ctx, owner, true).Files; got != 3000 {
		t.Errorf("owner files = %d, want 3000", got)
	}
	if got := p.UserStorage(ctx, guest, true).Total(); got != 0 {
		t.Errorf("guest storage = %d, want 0 (shared project is not theirs)", got)
	}

	u, _ := store.GetUserByID(ctx, owner)
	u.MaxStorageMB = 1
	if err := p.CheckStorageRoom(ctx, u, 0); err != nil {
		t.Errorf("under the limit was refused: %v", err)
	}
	if err := p.CheckStorageRoom(ctx, u, 2<<20); err == nil {
		t.Error("a write that goes over the limit was accepted")
	}
	u.MaxStorageMB = 0
	if err := p.CheckStorageRoom(ctx, u, 10<<20); err != nil {
		t.Errorf("unlimited user refused: %v", err)
	}
}

func TestCheckStorageRoomHostEmergencyBuffer(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store, err := db.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	p := &Panel{DB: store}
	normalUser := db.User{ID: 10, Role: db.RoleUser, MaxStorageMB: 5000}
	adminUser := db.User{ID: 1, Role: db.RoleAdmin, MaxStorageMB: 0}

	// Case 1: Host has only 1 GB free (critically low, buffer requires 1.5 GB).
	// Any write from normal user must be blocked.
	p.diskUsageFn = func(ctx context.Context) (int64, error) {
		return 1024 * 1024 * 1024, nil // 1 GB
	}
	if err := p.CheckStorageRoom(ctx, normalUser, 0); err == nil {
		t.Fatal("expected write to be blocked when host disk free is < 1.5 GB")
	}

	// Case 2: Admin writes are never blocked by host emergency buffer
	if err := p.CheckStorageRoom(ctx, adminUser, 1024*1024*1024); err != nil {
		t.Fatalf("admin write should never be blocked by emergency buffer: %v", err)
	}

	// Case 3: Host has 2 GB free, but incoming write is 600 MB (would leave 1.4 GB < 1.5 GB)
	p.diskUsageFn = func(ctx context.Context) (int64, error) {
		return 2048 * 1024 * 1024, nil // 2 GB
	}
	if err := p.CheckStorageRoom(ctx, normalUser, 600*1024*1024); err == nil {
		t.Fatal("expected write to be blocked when incoming write reduces free disk below 1.5 GB")
	}

	// Case 4: Host has 5 GB free, incoming write is 100 MB (leaves 4.9 GB > 1.5 GB)
	p.diskUsageFn = func(ctx context.Context) (int64, error) {
		return 5120 * 1024 * 1024, nil // 5 GB
	}
	if err := p.CheckStorageRoom(ctx, normalUser, 100*1024*1024); err != nil {
		t.Fatalf("expected write to succeed when sufficient host free disk: %v", err)
	}
}
