package handlers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"panel/internal/db"
	"panel/internal/workspace"

	"github.com/gofiber/fiber/v2"
)

// A logged-in user must not read another tenant's container logs, backup history or restore
// status by guessing the app id. These handlers used to only check that the app existed.
func TestAppScopedReadsRejectForeignTenants(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tenant_reads_*")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := db.Open(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatalf("db.Open failed: %v", err)
	}
	defer store.Close()

	p := &Panel{DB: store, Store: workspace.NewStore(tmpDir), WorkspacesRoot: tmpDir}
	ctx := context.Background()

	ownerID, err := store.CreateUser(ctx, "owner", "hash", db.RoleUser)
	if err != nil {
		t.Fatalf("CreateUser owner failed: %v", err)
	}
	intruderID, err := store.CreateUser(ctx, "intruder", "hash", db.RoleUser)
	if err != nil {
		t.Fatalf("CreateUser intruder failed: %v", err)
	}
	const appID = "secret-app"
	if err := store.CreateApp(ctx, appID, "Secret App", ownerID); err != nil {
		t.Fatalf("CreateApp failed: %v", err)
	}

	sessionSeq := 0
	sessionFor := func(t *testing.T, userID int64) *http.Cookie {
		t.Helper()
		sessionSeq++
		token := fmt.Sprintf("sess-%d-%d", userID, sessionSeq)
		if err := store.CreateSession(ctx, token, userID, time.Now().Add(time.Hour)); err != nil {
			t.Fatalf("CreateSession failed: %v", err)
		}
		return &http.Cookie{Name: sessionCookie, Value: token}
	}

	app := fiber.New()
	app.Use(p.AuthMiddleware)
	app.Get("/apps/:id/logs", p.AppLogsREST)
	app.Get("/apps/:id/backup/history", p.AppBackupHistory)
	app.Get("/apps/:id/backup/history/:historyid/log", p.AppBackupHistoryLog)
	app.Get("/apps/:id/backup/restore-status", p.AppBackupRestoreStatus)

	paths := []string{
		"/apps/" + appID + "/logs",
		"/apps/" + appID + "/backup/history",
		"/apps/" + appID + "/backup/history/1/log",
		"/apps/" + appID + "/backup/restore-status",
	}

	for _, path := range paths {
		t.Run("intruder "+path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.AddCookie(sessionFor(t, intruderID))
			resp, err := app.Test(req)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			if resp.StatusCode != http.StatusForbidden {
				t.Fatalf("expected 403 for a foreign app, got %d", resp.StatusCode)
			}
		})
	}

	// The owner still gets through to the handler (no 401/403).
	for _, path := range paths {
		t.Run("owner "+path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.AddCookie(sessionFor(t, ownerID))
			resp, err := app.Test(req)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized {
				t.Fatalf("owner was refused %s with %d", path, resp.StatusCode)
			}
		})
	}
}
