package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"panel/internal/db"
	"panel/internal/dockerx"
	"panel/internal/handlers"
	"panel/internal/workspace"

	"github.com/gofiber/fiber/v2"
)

func setupTestPanel(t *testing.T) (*handlers.Panel, *db.Store, string, db.User) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "mcp_test_*")
	if err != nil {
		t.Fatalf("os.MkdirTemp failed: %v", err)
	}

	store, err := db.Open(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatalf("db.Open failed: %v", err)
	}

	wsStore := workspace.NewStore(tmpDir)

	ctx := context.Background()
	adminID, err := store.CreateUser(ctx, "admin", "hash", db.RoleAdmin)
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}
	adminUser, err := store.GetUserByID(ctx, adminID)
	if err != nil {
		t.Fatalf("GetUserByID failed: %v", err)
	}

	p := &handlers.Panel{
		DB:             store,
		Store:          wsStore,
		WorkspacesRoot: tmpDir,
	}
	p.InitDeployRuns()
	_ = store.SetSetting(ctx, "mcp_enabled", "1")

	return p, store, tmpDir, adminUser
}

func TestMCP_Initialize(t *testing.T) {
	p, store, tmpDir, user := setupTestPanel(t)
	defer store.Close()
	defer os.RemoveAll(tmpDir)

	srv := NewServer(p)
	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "initialize",
	}

	resp := srv.ProcessRPC(context.Background(), user, req)
	if resp.Error != nil {
		t.Fatalf("expected nil error, got %+v", resp.Error)
	}
	initRes, ok := resp.Result.(InitializeResult)
	if !ok {
		t.Fatalf("expected InitializeResult, got %T", resp.Result)
	}
	if initRes.ServerInfo.Name != "nextdeploy-mcp" {
		t.Errorf("unexpected server name: %s", initRes.ServerInfo.Name)
	}
}

func TestMCP_ToolsList(t *testing.T) {
	p, store, tmpDir, user := setupTestPanel(t)
	defer store.Close()
	defer os.RemoveAll(tmpDir)

	srv := NewServer(p)
	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      2,
		Method:  "tools/list",
	}

	resp := srv.ProcessRPC(context.Background(), user, req)
	if resp.Error != nil {
		t.Fatalf("expected nil error, got %+v", resp.Error)
	}
	listRes, ok := resp.Result.(ToolsListResult)
	if !ok {
		t.Fatalf("expected ToolsListResult, got %T", resp.Result)
	}
	if len(listRes.Tools) != 19 {
		t.Errorf("expected 19 tools, got %d", len(listRes.Tools))
	}

	// Verify required tool names exist
	toolSet := make(map[string]bool)
	for _, tool := range listRes.Tools {
		toolSet[tool.Name] = true
	}
	expectedTools := []string{
		"app_list", "app_get", "file_list", "file_read", "file_write", "file_delete",
		"env_list", "env_reveal", "env_set", "compose_get", "deploy", "redeploy", "restart",
		"stop", "deploy_status", "container_logs", "deploy_log_tail", "dev_mode_set", "reset_dev_deps",
	}
	for _, name := range expectedTools {
		if !toolSet[name] {
			t.Errorf("missing expected tool: %s", name)
		}
	}
}

func TestMCP_FileToolsAndSecurity(t *testing.T) {
	p, store, tmpDir, user := setupTestPanel(t)
	defer store.Close()
	defer os.RemoveAll(tmpDir)

	ctx := context.Background()
	appID := "testapp"
	if err := store.CreateApp(ctx, appID, "Test App", user.ID); err != nil {
		t.Fatalf("CreateApp failed: %v", err)
	}

	srv := NewServer(p)

	// 1. Write file
	writeParams, _ := json.Marshal(CallToolParams{
		Name: "file_write",
		Arguments: map[string]interface{}{
			"app_id":  appID,
			"path":    "src/index.js",
			"content": "console.log('hello world');",
		},
	})
	resp := srv.ProcessRPC(ctx, user, JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      10,
		Method:  "tools/call",
		Params:  writeParams,
	})
	if resp.Error != nil {
		t.Fatalf("file_write error: %+v", resp.Error)
	}

	// 2. Read file
	readParams, _ := json.Marshal(CallToolParams{
		Name: "file_read",
		Arguments: map[string]interface{}{
			"app_id": appID,
			"path":   "src/index.js",
		},
	})
	resp = srv.ProcessRPC(ctx, user, JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      11,
		Method:  "tools/call",
		Params:  readParams,
	})
	if resp.Error != nil {
		t.Fatalf("file_read error: %+v", resp.Error)
	}
	toolRes := resp.Result.(CallToolResult)
	if len(toolRes.Content) == 0 || toolRes.Content[0].Text != "console.log('hello world');" {
		t.Fatalf("unexpected read content: %+v", toolRes.Content)
	}

	// 3. Security: Attempt path traversal (must fail)
	traversalParams, _ := json.Marshal(CallToolParams{
		Name: "file_read",
		Arguments: map[string]interface{}{
			"app_id": appID,
			"path":   "../../etc/passwd",
		},
	})
	resp = srv.ProcessRPC(ctx, user, JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      12,
		Method:  "tools/call",
		Params:  traversalParams,
	})
	toolRes = resp.Result.(CallToolResult)
	if !toolRes.IsError {
		t.Errorf("expected path traversal to fail, got success: %+v", toolRes)
	}

	// 4. File list
	listParams, _ := json.Marshal(CallToolParams{
		Name: "file_list",
		Arguments: map[string]interface{}{
			"app_id": appID,
			"path":   "src",
		},
	})
	resp = srv.ProcessRPC(ctx, user, JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      13,
		Method:  "tools/call",
		Params:  listParams,
	})
	toolRes = resp.Result.(CallToolResult)
	if toolRes.IsError || !strings.Contains(toolRes.Content[0].Text, "index.js") {
		t.Errorf("file_list failed: %+v", toolRes)
	}

	// 5. File delete
	delParams, _ := json.Marshal(CallToolParams{
		Name: "file_delete",
		Arguments: map[string]interface{}{
			"app_id": appID,
			"path":   "src/index.js",
		},
	})
	resp = srv.ProcessRPC(ctx, user, JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      14,
		Method:  "tools/call",
		Params:  delParams,
	})
	toolRes = resp.Result.(CallToolResult)
	if toolRes.IsError {
		t.Errorf("file_delete failed: %+v", toolRes)
	}
}

func TestMCP_EnvTools(t *testing.T) {
	p, store, tmpDir, user := setupTestPanel(t)
	defer store.Close()
	defer os.RemoveAll(tmpDir)

	ctx := context.Background()
	appID := "envapp"
	if err := store.CreateApp(ctx, appID, "Env App", user.ID); err != nil {
		t.Fatalf("CreateApp failed: %v", err)
	}

	srv := NewServer(p)

	// Set env var
	setParams, _ := json.Marshal(CallToolParams{
		Name: "env_set",
		Arguments: map[string]interface{}{
			"app_id": appID,
			"key":    "PORT",
			"value":  "8080",
		},
	})
	resp := srv.ProcessRPC(ctx, user, JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      20,
		Method:  "tools/call",
		Params:  setParams,
	})
	if resp.Error != nil {
		t.Fatalf("env_set error: %+v", resp.Error)
	}

	// List env vars (should only return keys, not values)
	listParams, _ := json.Marshal(CallToolParams{
		Name: "env_list",
		Arguments: map[string]interface{}{
			"app_id": appID,
		},
	})
	resp = srv.ProcessRPC(ctx, user, JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      21,
		Method:  "tools/call",
		Params:  listParams,
	})
	toolRes := resp.Result.(CallToolResult)
	if toolRes.IsError || !strings.Contains(toolRes.Content[0].Text, "PORT") {
		t.Errorf("env_list failed to return key: %+v", toolRes)
	}
	if strings.Contains(toolRes.Content[0].Text, "8080") {
		t.Errorf("env_list leaked secret value 8080: %+v", toolRes)
	}

	// 3. Test env_reveal without permission
	regularUserID, _ := store.CreateUser(ctx, "devuser", "hash", db.RoleUser)
	regUser, _ := store.GetUserByID(ctx, regularUserID)
	_ = store.AddCollaborator(ctx, appID, regularUserID, "developer")

	rawSafeToken, safeToken, err := store.CreateAPIToken(ctx, regUser.ID, "Safe Token", nil, false)
	if err != nil {
		t.Fatalf("CreateAPIToken failed: %v", err)
	}
	_ = rawSafeToken

	safeCtx := context.WithValue(ctx, apiTokenContextKey{}, safeToken)
	revealParams, _ := json.Marshal(CallToolParams{
		Name: "env_reveal",
		Arguments: map[string]interface{}{
			"app_id": appID,
		},
	})
	resp = srv.ProcessRPC(safeCtx, regUser, JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      22,
		Method:  "tools/call",
		Params:  revealParams,
	})
	toolRes = resp.Result.(CallToolResult)
	if !toolRes.IsError || !strings.Contains(toolRes.Content[0].Text, "permission denied") {
		t.Errorf("env_reveal without permission should fail, got: %+v", toolRes)
	}

	// 4. Toggle permission on the token and test env_reveal again
	_, err = store.ToggleAPITokenEnvReveal(ctx, safeToken.ID, regUser.ID)
	if err != nil {
		t.Fatalf("ToggleAPITokenEnvReveal failed: %v", err)
	}
	safeToken.AllowEnvReveal = true
	permittedCtx := context.WithValue(ctx, apiTokenContextKey{}, safeToken)

	resp = srv.ProcessRPC(permittedCtx, regUser, JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      23,
		Method:  "tools/call",
		Params:  revealParams,
	})
	toolRes = resp.Result.(CallToolResult)
	if toolRes.IsError || !strings.Contains(toolRes.Content[0].Text, "8080") {
		t.Errorf("env_reveal with permission failed: %+v", toolRes)
	}

	// 5. Test that even an ADMIN cannot env_reveal if their API token does not have AllowEnvReveal
	rawAdminSafeToken, adminSafeToken, err := store.CreateAPIToken(ctx, user.ID, "Admin Safe Token", nil, false)
	if err != nil {
		t.Fatalf("CreateAPIToken failed: %v", err)
	}
	_ = rawAdminSafeToken
	adminSafeCtx := context.WithValue(ctx, apiTokenContextKey{}, adminSafeToken)
	resp = srv.ProcessRPC(adminSafeCtx, user, JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      24,
		Method:  "tools/call",
		Params:  revealParams,
	})
	toolRes = resp.Result.(CallToolResult)
	if !toolRes.IsError || !strings.Contains(toolRes.Content[0].Text, "permission denied") {
		t.Errorf("admin env_reveal without AllowEnvReveal MUST fail, got: %+v", toolRes)
	}
}

func TestMCP_DeployJobTracking(t *testing.T) {
	p, store, tmpDir, user := setupTestPanel(t)
	defer store.Close()
	defer os.RemoveAll(tmpDir)

	ctx := context.Background()
	appID := "deployapp"
	if err := store.CreateApp(ctx, appID, "Deploy App", user.ID); err != nil {
		t.Fatalf("CreateApp failed: %v", err)
	}

	// Create fake compose file in workspace
	appDir := filepath.Join(tmpDir, appID)
	_ = os.MkdirAll(appDir, 0750)
	_ = os.WriteFile(filepath.Join(appDir, "docker-compose.yml"), []byte("services:\n  web:\n    image: nginx\n"), 0640)

	jobID, err := p.StartComposeJob(appID, "testproj", []string{filepath.Join(appDir, "docker-compose.yml")}, "Test deploy", func(ctx context.Context, dir string, paths []string, project string, w io.Writer, envs []string) dockerx.Result {
		_, _ = w.Write([]byte("Step 1: building\nStep 2: done\n"))
		return dockerx.Result{OK: true}
	}, "")
	if err != nil {
		t.Fatalf("StartComposeJob failed: %v", err)
	}
	if !strings.HasPrefix(jobID, "job_deployapp_") {
		t.Errorf("unexpected job ID format: %s", jobID)
	}

	// Wait briefly for goroutine to finish
	time.Sleep(100 * time.Millisecond)

	jobRec, found := p.GetDeployJob(jobID)
	if !found {
		t.Fatalf("GetDeployJob did not find job %s", jobID)
	}
	if jobRec.JobID != jobID || jobRec.AppID != appID || !jobRec.OK {
		t.Errorf("unexpected job record: %+v", jobRec)
	}
	if !strings.Contains(jobRec.Output, "Step 2: done") {
		t.Errorf("job output missing logged steps: %s", jobRec.Output)
	}

	// Poll deploy_status via MCP
	srv := NewServer(p)
	statusParams, _ := json.Marshal(CallToolParams{
		Name: "deploy_status",
		Arguments: map[string]interface{}{
			"job_id": jobID,
		},
	})
	resp := srv.ProcessRPC(ctx, user, JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      30,
		Method:  "tools/call",
		Params:  statusParams,
	})
	toolRes := resp.Result.(CallToolResult)
	if toolRes.IsError || !strings.Contains(toolRes.Content[0].Text, jobID) {
		t.Errorf("deploy_status failed: %+v", toolRes)
	}
}

func TestMCP_ServerHTTPAndAuth(t *testing.T) {
	p, store, tmpDir, user := setupTestPanel(t)
	defer store.Close()
	defer os.RemoveAll(tmpDir)

	ctx := context.Background()
	rawToken, _, err := store.CreateAPIToken(ctx, user.ID, "Test Token", nil, false)
	if err != nil {
		t.Fatalf("CreateAPIToken failed: %v", err)
	}

	app := fiber.New()
	srv := NewServer(p)
	srv.RegisterRoutes(app)

	// 1. Unauthenticated request -> 401
	req := httptest.NewRequest("GET", "/mcp", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}

	// 2. Authenticated GET /mcp with Bearer token
	req = httptest.NewRequest("GET", "/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+rawToken)
	resp, err = app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	// 3. Authenticated POST /mcp with JSON-RPC initialize
	body := `{"jsonrpc":"2.0","id":1,"method":"initialize"}`
	req = httptest.NewRequest("POST", "/mcp", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+rawToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err = app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	respBytes, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(respBytes), "nextdeploy-mcp") {
		t.Errorf("response missing server name: %s", string(respBytes))
	}

	// 4. Authenticated GET /mcp with X-API-Key header -> 200
	req = httptest.NewRequest("GET", "/mcp", nil)
	req.Header.Set("X-API-Key", rawToken)
	resp, err = app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 with X-API-Key, got %d", resp.StatusCode)
	}

	// 5. Session cookie MUST be rejected -> 401 Unauthorized
	sessionToken := "test_session_token_xyz"
	if err := store.CreateSession(ctx, sessionToken, user.ID, time.Now().Add(24*time.Hour)); err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	req = httptest.NewRequest("GET", "/mcp", nil)
	req.AddCookie(&http.Cookie{Name: "nd_session", Value: sessionToken})
	resp, err = app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 for session cookie, got %d", resp.StatusCode)
	}

	// 6. Query string ?token= MUST be rejected -> 401 Unauthorized
	req = httptest.NewRequest("GET", "/mcp?token="+rawToken, nil)
	resp, err = app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 for query param ?token, got %d", resp.StatusCode)
	}
}

func TestMCP_ServerDisabledByDefault(t *testing.T) {
	p, store, tmpDir, user := setupTestPanel(t)
	defer store.Close()
	defer os.RemoveAll(tmpDir)

	ctx := context.Background()
	// Disable MCP explicitly
	_ = store.SetSetting(ctx, "mcp_enabled", "0")

	rawToken, _, err := store.CreateAPIToken(ctx, user.ID, "Test Token", nil, false)
	if err != nil {
		t.Fatalf("CreateAPIToken failed: %v", err)
	}

	app := fiber.New()
	srv := NewServer(p)
	srv.RegisterRoutes(app)

	// Authenticated request when MCP is disabled -> 503 Service Unavailable
	req := httptest.NewRequest("GET", "/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+rawToken)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("expected 503 when MCP disabled, got %d", resp.StatusCode)
	}
}

func TestMCP_CollaboratorRBAC(t *testing.T) {
	p, store, tmpDir, owner := setupTestPanel(t)
	defer store.Close()
	defer os.RemoveAll(tmpDir)

	ctx := context.Background()
	appID := "rbacapp"
	if err := store.CreateApp(ctx, appID, "RBAC App", owner.ID); err != nil {
		t.Fatalf("CreateApp failed: %v", err)
	}

	// Create a collaborator with viewer role
	viewerID, err := store.CreateUser(ctx, "viewer_user", "hash", db.RoleUser)
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}
	viewerUser, _ := store.GetUserByID(ctx, viewerID)
	if err := store.AddCollaborator(ctx, appID, viewerID, db.CollabRoleViewer); err != nil {
		t.Fatalf("AddCollaborator failed: %v", err)
	}

	srv := NewServer(p)

	// 1. Viewer can call READ tools (app_get, env_list)
	readParams, _ := json.Marshal(CallToolParams{
		Name: "app_get",
		Arguments: map[string]interface{}{
			"app_id": appID,
		},
	})
	resp := srv.ProcessRPC(ctx, viewerUser, JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      50,
		Method:  "tools/call",
		Params:  readParams,
	})
	toolRes := resp.Result.(CallToolResult)
	if toolRes.IsError {
		t.Errorf("viewer should be allowed to call app_get: %+v", toolRes)
	}

	// 2. Viewer CANNOT call WRITE tools (file_write, env_set, deploy)
	writeParams, _ := json.Marshal(CallToolParams{
		Name: "file_write",
		Arguments: map[string]interface{}{
			"app_id":  appID,
			"path":    "exploit.txt",
			"content": "malicious write attempt",
		},
	})
	resp = srv.ProcessRPC(ctx, viewerUser, JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      51,
		Method:  "tools/call",
		Params:  writeParams,
	})
	toolRes = resp.Result.(CallToolResult)
	if !toolRes.IsError || !strings.Contains(toolRes.Content[0].Text, "developer access") {
		t.Errorf("viewer MUST be blocked from file_write: %+v", toolRes)
	}

	// 3. Promote collaborator to developer role
	_ = store.RemoveCollaborator(ctx, appID, viewerID)
	_ = store.AddCollaborator(ctx, appID, viewerID, db.CollabRoleDeveloper)

	// Developer can now call file_write
	resp = srv.ProcessRPC(ctx, viewerUser, JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      52,
		Method:  "tools/call",
		Params:  writeParams,
	})
	toolRes = resp.Result.(CallToolResult)
	if toolRes.IsError {
		t.Errorf("developer should be allowed to call file_write: %+v", toolRes)
	}
}
