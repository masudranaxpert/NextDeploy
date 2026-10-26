package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"panel/internal/db"
	"panel/internal/workspace"

	"github.com/gofiber/fiber/v2"
)

func TestCLIEndpoints_BackupsAndProxy(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "cli_test_*")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := db.Open(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatalf("db.Open failed: %v", err)
	}
	defer store.Close()

	wsStore := workspace.NewStore(tmpDir)
	p := &Panel{
		DB:             store,
		Store:          wsStore,
		WorkspacesRoot: tmpDir,
	}

	ctx := context.Background()
	adminID, err := store.CreateUser(ctx, "cliadmin", "hash", db.RoleAdmin)
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	rawToken, _, err := store.CreateAPIToken(ctx, adminID, "CLI Admin Token", "cli", nil, true, true, true, true)
	if err != nil {
		t.Fatalf("CreateAPIToken failed: %v", err)
	}

	appID := "cli-app"
	if err := store.CreateApp(ctx, appID, "CLI App", adminID); err != nil {
		t.Fatalf("CreateApp failed: %v", err)
	}

	destID, err := store.CreateBackupDestination(ctx, &adminID, "Test Dest", "s3", `{"bucket":"test"}`)
	if err != nil {
		t.Fatalf("CreateBackupDestination failed: %v", err)
	}

	schedID, err := store.CreateBackupSchedule(ctx, appID, destID, "app", "", "0 2 * * *", 7, false)
	if err != nil {
		t.Fatalf("CreateBackupSchedule failed: %v", err)
	}

	histID, err := store.CreateBackupHistory(ctx, appID, destID, "app", "backup-1.tar.gz", "success", "", 1024)
	if err != nil {
		t.Fatalf("CreateBackupHistory failed: %v", err)
	}
	_ = store.UpdateBackupHistoryStatusWithLog(ctx, histID, "completed", "", "Test backup finished successfully")

	app := fiber.New()
	app.Get("/api/v1/backups/history/:id/log", p.APIAuthMiddleware, p.APIBackupHistoryLog)
	app.Delete("/api/v1/backups/schedules/:id", p.APIAuthMiddleware, p.APIBackupScheduleDelete)
	app.Post("/api/v1/proxy/routes", p.APIAuthMiddleware, p.CaddyRouteSave)

	// 1. Test APIBackupHistoryLog
	reqLog := httptest.NewRequest("GET", "/api/v1/backups/history/"+strconv.FormatInt(histID, 10)+"/log", nil)
	reqLog.Header.Set("Authorization", "Bearer "+rawToken)
	respLog, err := app.Test(reqLog)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if respLog.StatusCode != http.StatusOK {
		t.Errorf("APIBackupHistoryLog returned %d, want 200", respLog.StatusCode)
	}
	var logData struct {
		Log    string `json:"log"`
		Status string `json:"status"`
	}
	_ = json.NewDecoder(respLog.Body).Decode(&logData)
	if logData.Log != "Test backup finished successfully" {
		t.Errorf("unexpected log content: %q", logData.Log)
	}

	// 2. Test APIBackupScheduleDelete
	reqDel := httptest.NewRequest("DELETE", "/api/v1/backups/schedules/"+strconv.FormatInt(schedID, 10), nil)
	reqDel.Header.Set("Authorization", "Bearer "+rawToken)
	respDel, err := app.Test(reqDel)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if respDel.StatusCode != http.StatusOK {
		t.Errorf("APIBackupScheduleDelete returned %d, want 200", respDel.StatusCode)
	}

	// Verify schedule is deleted
	if _, err := store.GetBackupSchedule(ctx, schedID); err == nil {
		t.Errorf("schedule was not deleted from database")
	}

	// 3. Test CaddyRouteSave with upstream and route_type aliases
	proxyPayload := map[string]interface{}{
		"domain":     "test.example.com",
		"upstream":   "http://127.0.0.1:9090",
		"route_type": "proxy",
		"enabled":    true,
	}
	payloadBytes, _ := json.Marshal(proxyPayload)
	reqProxy := httptest.NewRequest("POST", "/api/v1/proxy/routes", bytes.NewReader(payloadBytes))
	reqProxy.Header.Set("Authorization", "Bearer "+rawToken)
	reqProxy.Header.Set("Content-Type", "application/json")
	respProxy, err := app.Test(reqProxy)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if respProxy.StatusCode != http.StatusOK {
		t.Errorf("CaddyRouteSave returned %d, want 200", respProxy.StatusCode)
	}
	var proxyResp struct {
		OK    bool          `json:"ok"`
		Route db.ProxyRoute `json:"route"`
	}
	_ = json.NewDecoder(respProxy.Body).Decode(&proxyResp)
	if proxyResp.Route.Target != "http://127.0.0.1:9090" || proxyResp.Route.Kind != "proxy" {
		t.Errorf("expected Target=http://127.0.0.1:9090 and Kind=proxy, got Target=%q Kind=%q", proxyResp.Route.Target, proxyResp.Route.Kind)
	}
}
