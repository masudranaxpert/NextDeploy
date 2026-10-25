package handlers

import (
	"context"
	"path/filepath"
	"testing"

	"panel/internal/db"
)

func TestValidateAppLimitsStaysInsideTheAccount(t *testing.T) {
	ctx := context.Background()
	user := db.User{Role: db.RoleUser, MaxMemoryMB: 1024, MaxCPUs: 2}
	if err := validateAppLimits(ctx, user, 512, 1); err != nil {
		t.Errorf("a limit inside the account was refused: %v", err)
	}
	if err := validateAppLimits(ctx, user, 0, 0); err != nil {
		t.Errorf("clearing the limits was refused: %v", err)
	}
	if err := validateAppLimits(ctx, user, 2048, 1); err == nil {
		t.Error("memory above the account limit was accepted")
	}
	if err := validateAppLimits(ctx, user, 512, 4); err == nil {
		t.Error("CPU above the account limit was accepted")
	}
	if err := validateAppLimits(ctx, user, 8, 0); err == nil {
		t.Error("a tiny memory limit was accepted")
	}
	if err := validateAppLimits(ctx, user, -1, 0); err == nil {
		t.Error("a negative limit was accepted")
	}
	admin := db.User{Role: db.RoleAdmin}
	if err := validateAppLimits(ctx, admin, 1<<30, 0); err == nil {
		t.Error("an admin limit above the server's RAM was accepted")
	}
}

func TestAppLimitsStore(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	owner, _ := store.CreateUser(ctx, "o", "h", db.RoleUser)
	_ = store.CreateApp(ctx, "a-1", "a", owner)
	if l, _ := store.GetAppLimits(ctx, "a-1"); l.MemoryMB != 0 || l.CPUs != 0 {
		t.Fatalf("new project has limits: %+v", l)
	}
	_ = store.SetAppLimits(ctx, db.AppLimits{AppID: "a-1", MemoryMB: 256, CPUs: 0.5})
	if l, _ := store.GetAppLimits(ctx, "a-1"); l.MemoryMB != 256 || l.CPUs != 0.5 {
		t.Fatalf("saved limits lost: %+v", l)
	}
	_ = store.SetAppLimits(ctx, db.AppLimits{AppID: "a-1"})
	if list, _ := store.ListAppLimits(ctx); len(list) != 0 {
		t.Fatalf("cleared limits still listed: %+v", list)
	}
	_ = store.SetAppLimits(ctx, db.AppLimits{AppID: "a-1", MemoryMB: 128})
	_ = store.DeleteApp(ctx, "a-1")
	if list, _ := store.ListAppLimits(ctx); len(list) != 0 {
		t.Fatalf("limits survived project deletion: %+v", list)
	}
}
