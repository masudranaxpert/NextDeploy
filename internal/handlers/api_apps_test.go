package handlers

import (
	"bytes"
	"context"
	"encoding/json"
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

func setupAPIAppsTestServer(t *testing.T) (*Panel, *fiber.App, string, string, string) {
	t.Helper()
	tmpDir := t.TempDir()
	store, err := db.Open(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatalf("db.Open failed: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	p := &Panel{
		DB:             store,
		Store:          workspace.NewStore(tmpDir),
		WorkspacesRoot: tmpDir,
	}

	ctx := context.Background()
	adminID, err := store.CreateUser(ctx, "admin1", "hash", db.RoleAdmin)
	if err != nil {
		t.Fatalf("CreateUser admin failed: %v", err)
	}
	userID, err := store.CreateUser(ctx, "dev1", "hash", db.RoleUser)
	if err != nil {
		t.Fatalf("CreateUser user failed: %v", err)
	}
	otherID, err := store.CreateUser(ctx, "other1", "hash", db.RoleUser)
	if err != nil {
		t.Fatalf("CreateUser other failed: %v", err)
	}

	adminTok, _, err := store.CreateAPIToken(ctx, adminID, "admin-tok", "cli", nil, true, true, true, true)
	if err != nil {
		t.Fatalf("CreateAPIToken admin failed: %v", err)
	}
	userRestrictedTok, _, err := store.CreateAPIToken(ctx, userID, "user-safe", "cli", nil, false, false, false, false)
	if err != nil {
		t.Fatalf("CreateAPIToken user failed: %v", err)
	}
	otherTok, _, err := store.CreateAPIToken(ctx, otherID, "other-tok", "cli", nil, true, false, true, false)
	if err != nil {
		t.Fatalf("CreateAPIToken other failed: %v", err)
	}

	app := fiber.New()
	app.Get("/api/v1/cli/whoami", p.APIAuthMiddleware, p.APICliWhoami)
	app.Get("/api/v1/apps", p.APIAuthMiddleware, p.APIAppsList)
	app.Post("/api/v1/apps", p.APIAuthMiddleware, p.APIAppCreate)
	app.Get("/api/v1/apps/:id", p.APIAuthMiddleware, p.APIAppGet)
	app.Get("/api/v1/apps/:id/env", p.APIAuthMiddleware, p.APIAppEnvList)
	app.Post("/api/v1/apps/:id/env", p.APIAuthMiddleware, p.APIAppEnvSet)
	app.Post("/api/v1/apps/:id/exec", p.APIAuthMiddleware, p.APIAppExec)
	app.Post("/api/v1/apps/:id/workspace/apply", p.APIAuthMiddleware, p.APIAppWorkspaceApply)
	app.Get("/api/v1/apps/:id/domains", p.APIAuthMiddleware, p.APIAppDomainsList)
	app.Post("/api/v1/apps/:id/domains", p.APIAuthMiddleware, p.APIAppDomainAdd)
	app.Delete("/api/v1/apps/:id/domains", p.APIAuthMiddleware, p.APIAppDomainDelete)
	app.Post("/api/v1/system/exec", p.APIAuthMiddleware, p.RequireAdminMiddleware, p.APISystemExec)

	return p, app, adminTok, userRestrictedTok, otherTok
}

func doJSONReq(t *testing.T, app *fiber.App, method, path, token string, body interface{}) (*http.Response, map[string]interface{}) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := app.Test(req, 5000)
	if err != nil {
		t.Fatalf("%s %s failed: %v", method, path, err)
	}
	var parsed map[string]interface{}
	_ = json.NewDecoder(resp.Body).Decode(&parsed)
	return resp, parsed
}

func TestAPIAppsREST_RBACAndScopes(t *testing.T) {
	p, app, adminTok, userTok, otherTok := setupAPIAppsTestServer(t)

	// 1. User whoami returns user role + restricted scope booleans
	resp, who := doJSONReq(t, app, http.MethodGet, "/api/v1/cli/whoami", userTok, nil)
	if resp.StatusCode != http.StatusOK || who["role"] != db.RoleUser || who["username"] != "dev1" {
		t.Fatalf("unexpected whoami response (%d): %+v", resp.StatusCode, who)
	}
	if who["allow_env_reveal"] != false || who["allow_server_exec"] != false {
		t.Fatalf("expected restricted flags false for userTok, got %+v", who)
	}

	// 2. Non-admin cannot hit system/exec
	resp, _ = doJSONReq(t, app, http.MethodPost, "/api/v1/system/exec", userTok, map[string]string{"command": "echo hi"})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for non-admin system/exec, got %d", resp.StatusCode)
	}
	// Admin can run system/exec
	resp, execOut := doJSONReq(t, app, http.MethodPost, "/api/v1/system/exec", adminTok, map[string]string{"command": "echo ok_exec"})
	if resp.StatusCode != http.StatusOK || !strings.Contains(execOut["output"].(string), "ok_exec") {
		t.Fatalf("expected admin system/exec success, got %d %+v", resp.StatusCode, execOut)
	}

	// 3. Regular user creates an app via POST /api/v1/apps
	resp, created := doJSONReq(t, app, http.MethodPost, "/api/v1/apps", userTok, map[string]string{"name": "user-web"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 creating app, got %d %+v", resp.StatusCode, created)
	}
	appID := created["app_id"].(string)

	// 4. Another non-admin user cannot access dev1's app
	resp, _ = doJSONReq(t, app, http.MethodGet, "/api/v1/apps/"+appID, otherTok, nil)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for other user reading dev1 app, got %d", resp.StatusCode)
	}

	// 5. Env set succeeds for owner; list returns masked keys; reveal denied without allow_env_reveal
	resp, _ = doJSONReq(t, app, http.MethodPost, "/api/v1/apps/"+appID+"/env", userTok, map[string]interface{}{
		"variables": map[string]string{"SECRET_KEY": "supersecret"},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 setting env, got %d", resp.StatusCode)
	}
	resp, envList := doJSONReq(t, app, http.MethodGet, "/api/v1/apps/"+appID+"/env", userTok, nil)
	if resp.StatusCode != http.StatusOK || envList["count"] != float64(1) {
		t.Fatalf("expected 1 env key, got %d %+v", resp.StatusCode, envList)
	}
	resp, _ = doJSONReq(t, app, http.MethodGet, "/api/v1/apps/"+appID+"/env?reveal=true", userTok, nil)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for env reveal without token flag, got %d", resp.StatusCode)
	}
	resp, revealed := doJSONReq(t, app, http.MethodGet, "/api/v1/apps/"+appID+"/env?reveal=true", adminTok, nil)
	if resp.StatusCode != http.StatusOK || revealed["env"].(map[string]interface{})["SECRET_KEY"] != "supersecret" {
		t.Fatalf("expected admin token with allow_env_reveal to reveal secret, got %d %+v", resp.StatusCode, revealed)
	}

	// 6. Container exec denied when allow_container_exec=false
	resp, _ = doJSONReq(t, app, http.MethodPost, "/api/v1/apps/"+appID+"/exec", userTok, map[string]string{"command": "id"})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for container exec without allow_container_exec, got %d", resp.StatusCode)
	}

	// 7. Workspace apply rejects privileged compose & writes/deletes normal files
	resp, _ = doJSONReq(t, app, http.MethodPost, "/api/v1/apps/"+appID+"/workspace/apply", userTok, map[string]interface{}{
		"writes": []map[string]string{{"path": "docker-compose.yml", "content": "services:\n  web:\n    image: nginx\n    privileged: true\n"}},
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 on insecure compose write, got %d", resp.StatusCode)
	}
	resp, _ = doJSONReq(t, app, http.MethodPost, "/api/v1/apps/"+appID+"/workspace/apply", userTok, map[string]interface{}{
		"writes": []map[string]string{{"path": "hello.txt", "content": "world"}},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on safe file write, got %d", resp.StatusCode)
	}
	if _, err := os.Stat(filepath.Join(p.Store.Path(appID), "hello.txt")); err != nil {
		t.Fatalf("expected hello.txt to exist in workspace: %v", err)
	}

	// 8. Domains add/list/delete + non-admin cannot select custom shield node
	resp, _ = doJSONReq(t, app, http.MethodPost, "/api/v1/apps/"+appID+"/domains", userTok, map[string]interface{}{
		"domain":         "app.example.com",
		"shield_node_id": 99,
	})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for non-admin selecting custom shield node, got %d", resp.StatusCode)
	}
	resp, domAdd := doJSONReq(t, app, http.MethodPost, "/api/v1/apps/"+appID+"/domains", userTok, map[string]interface{}{
		"domain": "app.example.com",
		"port":   8080,
	})
	if resp.StatusCode != http.StatusOK || domAdd["domain"] != "app.example.com" {
		t.Fatalf("expected 200 adding domain, got %d %+v", resp.StatusCode, domAdd)
	}
	resp, _ = doJSONReq(t, app, http.MethodDelete, "/api/v1/apps/"+appID+"/domains?domain=app.example.com", userTok, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 deleting domain, got %d", resp.StatusCode)
	}
}
