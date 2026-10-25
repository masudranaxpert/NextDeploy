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
	if err := p.CheckStorageRoom(ctx, u, 1<<40); err != nil {
		t.Errorf("unlimited user refused: %v", err)
	}
}
