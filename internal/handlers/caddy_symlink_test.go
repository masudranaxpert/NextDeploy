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

func TestValidateAppFileServingPaths_SymlinksAndTraversal(t *testing.T) {
	tmpDir := t.TempDir()
	appID := "app-test-symlink"
	wsDir := filepath.Join(tmpDir, appID)
	if err := os.MkdirAll(wsDir, 0755); err != nil {
		t.Fatal(err)
	}

	outsideDir := t.TempDir()
	escapeLink := filepath.Join(wsDir, "escape_link")
	if err := os.Symlink(outsideDir, escapeLink); err != nil {
		t.Fatal(err)
	}

	brokenEscapeLink := filepath.Join(wsDir, "broken_link")
	if err := os.Symlink("/var/run/nonexistent_socket_target", brokenEscapeLink); err != nil {
		t.Fatal(err)
	}

	store := workspace.NewStore(tmpDir)
	database, err := db.Open(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	p := &Panel{
		Store: store,
		DB:    database,
	}

	ctx := context.Background()

	// 1. Safe relative path inside workspace
	dSafe := &db.AppDomain{
		ServeStatic: true,
		StaticPath:  "public",
	}
	if err := p.validateAppFileServingPaths(ctx, appID, dSafe); err != nil {
		t.Fatalf("expected safe relative path to pass, got: %v", err)
	}

	// 2. Safe absolute path inside workspace
	dSafeAbs := &db.AppDomain{
		ServeStatic: true,
		StaticPath:  "/data/workspaces/" + appID + "/public",
	}
	if err := p.validateAppFileServingPaths(ctx, appID, dSafeAbs); err != nil {
		t.Fatalf("expected safe absolute path inside workspace to pass, got: %v", err)
	}

	// 3. Cross-workspace access attempt
	dCrossWs := &db.AppDomain{
		ServeStatic: true,
		StaticPath:  "/data/workspaces/another-app/public",
	}
	if err := p.validateAppFileServingPaths(ctx, appID, dCrossWs); err == nil {
		t.Fatal("expected cross-workspace static path to be rejected, but it passed")
	} else if !strings.Contains(err.Error(), "cross-workspace access denied") {
		t.Fatalf("expected cross-workspace denial error, got: %v", err)
	}

	// 4. Path traversal attempt
	dTraversal := &db.AppDomain{
		ServeStatic: true,
		StaticPath:  "../secret",
	}
	if err := p.validateAppFileServingPaths(ctx, appID, dTraversal); err == nil {
		t.Fatal("expected path traversal to be rejected, but it passed")
	}

	// 5. Escaping symlink
	dSymlink := &db.AppDomain{
		ServeStatic: true,
		StaticPath:  "escape_link",
	}
	if err := p.validateAppFileServingPaths(ctx, appID, dSymlink); err == nil {
		t.Fatal("expected escaping symlink to be rejected, but it passed")
	} else if !strings.Contains(err.Error(), "resolves via symlink") {
		t.Fatalf("expected symlink escape error, got: %v", err)
	}

	// 6. Broken escaping symlink
	dBrokenSymlink := &db.AppDomain{
		ServeStatic: true,
		StaticPath:  "broken_link",
	}
	if err := p.validateAppFileServingPaths(ctx, appID, dBrokenSymlink); err == nil {
		t.Fatal("expected broken escaping symlink to be rejected, but it passed")
	} else if !strings.Contains(err.Error(), "resolves via symlink") {
		t.Fatalf("expected broken symlink escape error, got: %v", err)
	}
}
