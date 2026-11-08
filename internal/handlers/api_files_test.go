package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"panel/internal/db"
	"panel/internal/workspace"

	"github.com/gofiber/fiber/v2"
)

func TestAPIAppFilesEndpoints(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "api_files_test_*")
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
	adminID, err := store.CreateUser(ctx, "adminuser", "hash", db.RoleAdmin)
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	rawToken, _, err := store.CreateAPIToken(ctx, adminID, "Files Test Token", "cli", nil, false, false, false, false)
	if err != nil {
		t.Fatalf("CreateAPIToken failed: %v", err)
	}

	appID := "files-app"
	if err := store.CreateApp(ctx, appID, "Files App", adminID); err != nil {
		t.Fatalf("CreateApp failed: %v", err)
	}
	wsPath := p.Store.Path(appID)
	_ = os.MkdirAll(wsPath, 0750)

	// Create some initial files
	_ = os.WriteFile(filepath.Join(wsPath, "hello.txt"), []byte("Hello, world!"), 0640)
	subDir := filepath.Join(wsPath, "subdir")
	_ = os.MkdirAll(subDir, 0750)
	_ = os.WriteFile(filepath.Join(subDir, "nested.go"), []byte("package main"), 0640)

	app := fiber.New()
	app.Get("/api/v1/apps/:id/files", p.APIAuthMiddleware, p.APIAppFilesList)
	app.Get("/api/v1/apps/:id/files/content", p.APIAuthMiddleware, p.APIAppFileContent)
	app.Post("/api/v1/apps/:id/files/content", p.APIAuthMiddleware, p.APIAppFileSave)
	app.Delete("/api/v1/apps/:id/files", p.APIAuthMiddleware, p.APIAppFileDelete)

	// 1. Unauthenticated request
	unauthReq := httptest.NewRequest("GET", "/api/v1/apps/"+appID+"/files", nil)
	unauthResp, err := app.Test(unauthReq)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if unauthResp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized, got %d", unauthResp.StatusCode)
	}

	// 2. List files (non-recursive)
	reqList := httptest.NewRequest("GET", "/api/v1/apps/"+appID+"/files", nil)
	reqList.Header.Set("Authorization", "Bearer "+rawToken)
	respList, err := app.Test(reqList)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if respList.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", respList.StatusCode)
	}
	var items []FileItem
	b, _ := io.ReadAll(respList.Body)
	if err := json.Unmarshal(b, &items); err != nil {
		t.Fatalf("unmarshal failed: %v, body: %s", err, string(b))
	}
	if len(items) != 2 {
		t.Errorf("expected 2 items, got %d", len(items))
	}

	// 3. List files (recursive)
	reqRec := httptest.NewRequest("GET", "/api/v1/apps/"+appID+"/files?recursive=true", nil)
	reqRec.Header.Set("Authorization", "Bearer "+rawToken)
	respRec, err := app.Test(reqRec)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if respRec.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", respRec.StatusCode)
	}
	var recItems []FileItem
	b, _ = io.ReadAll(respRec.Body)
	_ = json.Unmarshal(b, &recItems)
	if len(recItems) < 3 {
		t.Errorf("expected at least 3 items in recursive listing, got %d", len(recItems))
	}

	// 4. Read file content (raw)
	reqRead := httptest.NewRequest("GET", "/api/v1/apps/"+appID+"/files/content?path=hello.txt", nil)
	reqRead.Header.Set("Authorization", "Bearer "+rawToken)
	respRead, err := app.Test(reqRead)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if respRead.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", respRead.StatusCode)
	}
	contentBytes, _ := io.ReadAll(respRead.Body)
	if string(contentBytes) != "Hello, world!" {
		t.Errorf("expected 'Hello, world!', got %q", string(contentBytes))
	}

	// 5. Read file content (json)
	reqJSON := httptest.NewRequest("GET", "/api/v1/apps/"+appID+"/files/content?path=hello.txt&json=true", nil)
	reqJSON.Header.Set("Authorization", "Bearer "+rawToken)
	respJSON, err := app.Test(reqJSON)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if respJSON.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", respJSON.StatusCode)
	}
	var jsonResp struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	b, _ = io.ReadAll(respJSON.Body)
	_ = json.Unmarshal(b, &jsonResp)
	if jsonResp.Content != "Hello, world!" {
		t.Errorf("expected JSON content 'Hello, world!', got %q", jsonResp.Content)
	}

	// 6. Write new file
	writePayload := []byte(`{"path": "newfile.txt", "content": "Created via API test"}`)
	reqWrite := httptest.NewRequest("POST", "/api/v1/apps/"+appID+"/files/content", bytes.NewReader(writePayload))
	reqWrite.Header.Set("Authorization", "Bearer "+rawToken)
	reqWrite.Header.Set("Content-Type", "application/json")
	respWrite, err := app.Test(reqWrite)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if respWrite.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", respWrite.StatusCode)
	}

	// Verify file was written to disk
	writtenBytes, err := os.ReadFile(filepath.Join(wsPath, "newfile.txt"))
	if err != nil || string(writtenBytes) != "Created via API test" {
		t.Errorf("file was not written correctly to disk: %v", err)
	}

	// 7. Delete file
	reqDel := httptest.NewRequest("DELETE", "/api/v1/apps/"+appID+"/files?path=newfile.txt", nil)
	reqDel.Header.Set("Authorization", "Bearer "+rawToken)
	respDel, err := app.Test(reqDel)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if respDel.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", respDel.StatusCode)
	}

	// Verify file was deleted from disk
	if _, err := os.Stat(filepath.Join(wsPath, "newfile.txt")); !os.IsNotExist(err) {
		t.Errorf("expected newfile.txt to be deleted")
	}

	// 8. Delete directory
	reqDelDir := httptest.NewRequest("DELETE", "/api/v1/apps/"+appID+"/files?path=subdir", nil)
	reqDelDir.Header.Set("Authorization", "Bearer "+rawToken)
	respDelDir, err := app.Test(reqDelDir)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if respDelDir.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", respDelDir.StatusCode)
	}
	if _, err := os.Stat(subDir); !os.IsNotExist(err) {
		t.Errorf("expected subdir to be deleted")
	}
}

func TestAPIAppEnvDelete(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "api_env_test_*")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := db.Open(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatalf("db.Open failed: %v", err)
	}
	defer store.Close()

	p := &Panel{
		DB:             store,
		Store:          workspace.NewStore(tmpDir),
		WorkspacesRoot: tmpDir,
	}

	ctx := context.Background()
	adminID, _ := store.CreateUser(ctx, "adminuser", "hash", db.RoleAdmin)
	rawToken, _, _ := store.CreateAPIToken(ctx, adminID, "Env Test Token", "cli", nil, false, false, false, false)

	appID := "env-app"
	_ = store.CreateApp(ctx, appID, "Env App", adminID)
	_ = os.MkdirAll(p.Store.Path(appID), 0750)

	// Set initial env
	_ = store.UpdatePanelEnv(ctx, appID, "PORT=8080\nDB_PASS=secret\nFEATURE=true\n")

	fiberApp := fiber.New()
	fiberApp.Delete("/api/v1/apps/:id/env/:key", p.APIAuthMiddleware, p.APIAppEnvDelete)
	fiberApp.Post("/api/v1/apps/:id/env/unset", p.APIAuthMiddleware, p.APIAppEnvDelete)

	// 1. Delete single key via URL param
	req := httptest.NewRequest("DELETE", "/api/v1/apps/"+appID+"/env/DB_PASS", nil)
	req.Header.Set("Authorization", "Bearer "+rawToken)
	resp, err := fiberApp.Test(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("DELETE env/:key failed: status=%d err=%v", resp.StatusCode, err)
	}

	cur, _ := store.GetPanelEnv(ctx, appID)
	if strings.Contains(cur, "DB_PASS") {
		t.Errorf("expected DB_PASS to be removed, got: %s", cur)
	}
	if !strings.Contains(cur, "PORT=8080") || !strings.Contains(cur, "FEATURE=true") {
		t.Errorf("PORT and FEATURE should remain, got: %s", cur)
	}

	// 2. Unset batch keys via POST JSON body
	body, _ := json.Marshal(map[string]interface{}{"keys": []string{"PORT", "FEATURE"}})
	reqBatch := httptest.NewRequest("POST", "/api/v1/apps/"+appID+"/env/unset", bytes.NewReader(body))
	reqBatch.Header.Set("Authorization", "Bearer "+rawToken)
	reqBatch.Header.Set("Content-Type", "application/json")
	respBatch, err := fiberApp.Test(reqBatch)
	if err != nil || respBatch.StatusCode != http.StatusOK {
		t.Fatalf("POST env/unset failed: status=%d err=%v", respBatch.StatusCode, err)
	}

	curAfter, _ := store.GetPanelEnv(ctx, appID)
	if strings.Contains(curAfter, "PORT") || strings.Contains(curAfter, "FEATURE") {
		t.Errorf("expected PORT and FEATURE to be removed, got: %s", curAfter)
	}
}
