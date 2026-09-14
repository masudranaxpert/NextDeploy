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

	// Full-permission token sees all 25 tools.
	fullTok := db.APIToken{ID: 1, AllowEnvReveal: true, AllowServerExec: true, AllowContainerExec: true}
	fullCtx := context.WithValue(context.Background(), apiTokenContextKey{}, fullTok)
	resp := srv.ProcessRPC(fullCtx, user, req)
	if resp.Error != nil {
		t.Fatalf("expected nil error, got %+v", resp.Error)
	}
	listRes, ok := resp.Result.(ToolsListResult)
	if !ok {
		t.Fatalf("expected ToolsListResult, got %T", resp.Result)
	}
	if len(listRes.Tools) != 24 {
		t.Errorf("expected 24 tools with full perms, got %d", len(listRes.Tools))
	}

	// Verify required tool names exist
	toolSet := make(map[string]bool)
	for _, tool := range listRes.Tools {
		toolSet[tool.Name] = true
	}
	expectedTools := []string{
		"app_list", "app_get", "app_create", "app_delete", "workspace_manifest", "file_read", "file_write", "file_delete",
		"file_patch", "file_search", "workspace_apply", "env_list", "env_reveal", "env_set", "compose_get",
		"deploy", "restart", "stop", "deploy_status", "container_logs", "deploy_log_tail",
		"container_exec", "server_exec", "git_pull",
	}
	for _, name := range expectedTools {
		if !toolSet[name] {
			t.Errorf("missing expected tool: %s", name)
		}
	}

	for _, tool := range listRes.Tools {
		if tool.Name == "deploy" {
			if _, ok := tool.InputSchema.Properties["git_pull"]; !ok {
				t.Errorf("tool %s missing git_pull property in schema", tool.Name)
			}
			if _, ok := tool.InputSchema.Properties["rebuild"]; !ok {
				t.Errorf("tool %s missing rebuild property in schema", tool.Name)
			}
			if _, ok := tool.InputSchema.Properties["wait_seconds"]; !ok {
				t.Errorf("tool %s missing wait_seconds property in schema", tool.Name)
			}
		}
		if tool.Name == "workspace_manifest" {
			if _, ok := tool.InputSchema.Properties["depth"]; !ok {
				t.Errorf("tool %s missing depth property in schema", tool.Name)
			}
		}
		if tool.Name == "app_get" {
			if _, ok := tool.InputSchema.Properties["include_health"]; !ok {
				t.Errorf("tool %s missing include_health property in schema", tool.Name)
			}
		}
		if tool.Name == "env_set" {
			if _, ok := tool.InputSchema.Properties["variables"]; !ok {
				t.Errorf("tool %s missing variables property in schema", tool.Name)
			}
		}
	}

	// No-permission token hides restricted tools (21 tools).
	noPermResp := srv.ProcessRPC(context.Background(), user, req)
	noPermList := noPermResp.Result.(ToolsListResult)
	if len(noPermList.Tools) != 21 {
		t.Errorf("expected 21 tools with no perms, got %d", len(noPermList.Tools))
	}
	for _, tool := range noPermList.Tools {
		if tool.Name == "env_reveal" || tool.Name == "server_exec" || tool.Name == "container_exec" {
			t.Errorf("restricted tool %q must not appear without permission", tool.Name)
		}
	}

	// Token with only AllowContainerExec sees container_exec but NOT server_exec or env_reveal (22 tools).
	containerOnlyTok := db.APIToken{ID: 2, AllowContainerExec: true}
	containerOnlyCtx := context.WithValue(context.Background(), apiTokenContextKey{}, containerOnlyTok)
	containerResp := srv.ProcessRPC(containerOnlyCtx, user, req)
	containerList := containerResp.Result.(ToolsListResult)
	if len(containerList.Tools) != 22 {
		t.Errorf("expected 22 tools with container-only perms, got %d", len(containerList.Tools))
	}
	hasContainerExec := false
	for _, tool := range containerList.Tools {
		if tool.Name == "server_exec" || tool.Name == "env_reveal" {
			t.Errorf("unpermitted tool %q appeared in container-only list", tool.Name)
		}
		if tool.Name == "container_exec" {
			hasContainerExec = true
		}
	}
	if !hasContainerExec {
		t.Errorf("expected container_exec to be present for AllowContainerExec token")
	}

	// Token with only AllowServerExec sees server_exec but NOT container_exec or env_reveal (22 tools).
	serverOnlyTok := db.APIToken{ID: 3, AllowServerExec: true}
	serverOnlyCtx := context.WithValue(context.Background(), apiTokenContextKey{}, serverOnlyTok)
	serverResp := srv.ProcessRPC(serverOnlyCtx, user, req)
	serverList := serverResp.Result.(ToolsListResult)
	if len(serverList.Tools) != 22 {
		t.Errorf("expected 22 tools with server-only perms, got %d", len(serverList.Tools))
	}
	hasServerExec := false
	for _, tool := range serverList.Tools {
		if tool.Name == "container_exec" || tool.Name == "env_reveal" {
			t.Errorf("unpermitted tool %q appeared in server-only list", tool.Name)
		}
		if tool.Name == "server_exec" {
			hasServerExec = true
		}
	}
	if !hasServerExec {
		t.Errorf("expected server_exec to be present for AllowServerExec token")
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

	rawSafeToken, safeToken, err := store.CreateAPIToken(ctx, regUser.ID, "Safe Token", "mcp", nil, false, false, false)
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
	rawAdminSafeToken, adminSafeToken, err := store.CreateAPIToken(ctx, user.ID, "Admin Safe Token", "mcp", nil, false, false, false)
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

// TestMCP_DeployGitPullFlag verifies that:
// 1. deploy/redeploy succeed with git_pull:true on non-git app (no-op).
// 2. deploy without git_pull succeeds (dirty-check path, no git repo = clean = skip gracefully).
func TestMCP_DeployGitPullFlag(t *testing.T) {
	p, store, tmpDir, user := setupTestPanel(t)
	defer store.Close()
	defer os.RemoveAll(tmpDir)

	ctx := context.Background()
	createTestApp := func(id string) {
		if err := store.CreateApp(ctx, id, id, user.ID); err != nil {
			t.Fatalf("CreateApp %s failed: %v", id, err)
		}
		dir := filepath.Join(tmpDir, id)
		_ = os.MkdirAll(dir, 0750)
		_ = os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte("services:\n  web:\n    image: nginx\n"), 0640)
	}

	app1 := "deployapp_git1"
	app2 := "deployapp_git2"
	app3 := "deployapp_git3"
	createTestApp(app1)
	createTestApp(app2)
	createTestApp(app3)

	srv := NewServer(p)

	// 1. deploy with git_pull:true on a non-git app should succeed (git sync block not entered).
	deployParams, _ := json.Marshal(CallToolParams{
		Name: "deploy",
		Arguments: map[string]interface{}{
			"app_id":   app1,
			"git_pull": true,
		},
	})
	resp := srv.ProcessRPC(ctx, user, JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      35,
		Method:  "tools/call",
		Params:  deployParams,
	})
	toolRes := resp.Result.(CallToolResult)
	if toolRes.IsError {
		t.Fatalf("deploy with git_pull:true failed: %+v", toolRes)
	}
	var out map[string]interface{}
	if err := json.Unmarshal([]byte(toolRes.Content[0].Text), &out); err != nil {
		t.Fatalf("failed to unmarshal deploy response: %v", err)
	}
	if out["status"] != "started" {
		t.Errorf("expected status 'started', got %v", out["status"])
	}

	// 2. deploy without git_pull on non-git app should also succeed.
	deployParams2, _ := json.Marshal(CallToolParams{
		Name:      "deploy",
		Arguments: map[string]interface{}{"app_id": app2},
	})
	resp2 := srv.ProcessRPC(ctx, user, JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      36,
		Method:  "tools/call",
		Params:  deployParams2,
	})
	toolRes2 := resp2.Result.(CallToolResult)
	if toolRes2.IsError {
		t.Fatalf("deploy without git_pull failed: %+v", toolRes2)
	}

	// 3. redeploy with git_pull:true should also succeed.
	redeployParams, _ := json.Marshal(CallToolParams{
		Name: "redeploy",
		Arguments: map[string]interface{}{
			"app_id":   app3,
			"git_pull": true,
		},
	})
	redeployResp := srv.ProcessRPC(ctx, user, JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      37,
		Method:  "tools/call",
		Params:  redeployParams,
	})
	redeployRes := redeployResp.Result.(CallToolResult)
	if redeployRes.IsError {
		t.Fatalf("redeploy with git_pull:true failed: %+v", redeployRes)
	}
}

func TestMCP_ServerHTTPAndAuth(t *testing.T) {
	p, store, tmpDir, user := setupTestPanel(t)
	defer store.Close()
	defer os.RemoveAll(tmpDir)

	ctx := context.Background()
	rawToken, _, err := store.CreateAPIToken(ctx, user.ID, "Test Token", "cli", nil, false, false, false)
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

	rawToken, _, err := store.CreateAPIToken(ctx, user.ID, "Test Token", "mcp", nil, false, false, false)
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

	// Header spoofing attempt: client claiming to be CLI cannot bypass disabled MCP for an MCP token
	reqSpoof := httptest.NewRequest("GET", "/mcp", nil)
	reqSpoof.Header.Set("Authorization", "Bearer "+rawToken)
	reqSpoof.Header.Set("X-NextDeploy-Client", "cli")
	reqSpoof.Header.Set("User-Agent", "nd/1.0.8")
	respSpoof, err := app.Test(reqSpoof)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if respSpoof.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("expected 503 despite CLI header spoofing when MCP disabled, got %d", respSpoof.StatusCode)
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

func TestMCP_DocsPageAuth(t *testing.T) {
	p, store, tmpDir, user := setupTestPanel(t)
	defer store.Close()
	defer os.RemoveAll(tmpDir)

	ctx := context.Background()
	app := fiber.New()
	srv := NewServer(p)

	// Register routes like main.go
	srv.RegisterRoutes(app)
	app.Use(p.AuthMiddleware)
	app.Get("/mcp-docs", srv.MCPDocsPage)
	app.Post("/mcp-docs/toggle-status", srv.ToggleMCPStatusPost)

	// 1. Unauthenticated request to /mcp-docs should redirect to /login
	req := httptest.NewRequest("GET", "/mcp-docs", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if resp.StatusCode != http.StatusFound {
		t.Errorf("expected 302 redirect for unauthenticated /mcp-docs, got %d", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); !strings.Contains(loc, "/login") {
		t.Errorf("expected redirect to /login, got %s", loc)
	}

	// 2. Authenticated admin session toggles status
	sessionToken := "admin_session_test_xyz"
	if err := store.CreateSession(ctx, sessionToken, user.ID, time.Now().Add(24*time.Hour)); err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	req = httptest.NewRequest("POST", "/mcp-docs/toggle-status", nil)
	req.AddCookie(&http.Cookie{Name: "nd_session", Value: sessionToken})
	resp, err = app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if resp.StatusCode != http.StatusFound {
		t.Errorf("expected 302 redirect after toggle, got %d", resp.StatusCode)
	}

	// Verify setting was toggled in DB (from 1 to 0)
	if enabled := store.GetSetting(ctx, "mcp_enabled"); enabled != "0" {
		t.Errorf("expected mcp_enabled to be 0 after toggle, got %s", enabled)
	}

	// Toggle again from 0 to 1
	req = httptest.NewRequest("POST", "/mcp-docs/toggle-status", nil)
	req.AddCookie(&http.Cookie{Name: "nd_session", Value: sessionToken})
	resp, err = app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if enabled := store.GetSetting(ctx, "mcp_enabled"); enabled != "1" {
		t.Errorf("expected mcp_enabled to be 1 after second toggle, got %s", enabled)
	}
}

func TestMCP_TerminalTools(t *testing.T) {
	p, store, tmpDir, adminUser := setupTestPanel(t)
	defer store.Close()
	defer os.RemoveAll(tmpDir)

	ctx := context.Background()
	regularUserID, err := store.CreateUser(ctx, "regular", "hash", db.RoleUser)
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}
	regularUser, err := store.GetUserByID(ctx, regularUserID)
	if err != nil {
		t.Fatalf("GetUserByID failed: %v", err)
	}

	appID := "terminal-test-app"
	if err := store.CreateApp(ctx, appID, "Terminal Test App", regularUserID); err != nil {
		t.Fatalf("CreateApp failed: %v", err)
	}

	srv := NewServer(p)

	// 1. Non-admin calls server_exec -> must be forbidden
	serverParams, _ := json.Marshal(CallToolParams{
		Name: "server_exec",
		Arguments: map[string]interface{}{
			"command": "echo forbidden_test",
		},
	})
	resp := srv.ProcessRPC(ctx, regularUser, JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      101,
		Method:  "tools/call",
		Params:  serverParams,
	})
	if resp.Result == nil {
		t.Fatalf("expected CallToolResult, got nil")
	}
	callRes := resp.Result.(CallToolResult)
	if !callRes.IsError || len(callRes.Content) == 0 || !strings.Contains(callRes.Content[0].Text, "forbidden") {
		t.Errorf("expected forbidden error for regular user calling server_exec, got %+v", callRes)
	}

	// 2. Admin calls server_exec with AllowServerExec token -> must succeed
	adminToken := db.APIToken{ID: 999, UserID: adminUser.ID, AllowServerExec: true}
	adminExecCtx := context.WithValue(ctx, apiTokenContextKey{}, adminToken)
	adminServerParams, _ := json.Marshal(CallToolParams{
		Name: "server_exec",
		Arguments: map[string]interface{}{
			"command": "echo admin_server_exec_ok",
		},
	})
	respAdmin := srv.ProcessRPC(adminExecCtx, adminUser, JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      102,
		Method:  "tools/call",
		Params:  adminServerParams,
	})
	if respAdmin.Result == nil {
		t.Fatalf("expected CallToolResult for admin, got nil")
	}
	callResAdmin := respAdmin.Result.(CallToolResult)
	if callResAdmin.IsError {
		t.Errorf("expected success for admin server_exec, got error: %+v", callResAdmin)
	}
	if len(callResAdmin.Content) == 0 || !strings.Contains(callResAdmin.Content[0].Text, "admin_server_exec_ok") {
		t.Errorf("expected admin_server_exec_ok in output, got %+v", callResAdmin.Content)
	}

	// 3. User with viewer role calling container_exec -> must be forbidden
	viewerUserID, err := store.CreateUser(ctx, "viewer", "hash", db.RoleUser)
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}
	viewerUser, err := store.GetUserByID(ctx, viewerUserID)
	if err != nil {
		t.Fatalf("GetUserByID failed: %v", err)
	}
	if err := store.AddCollaborator(ctx, appID, viewerUserID, db.CollabRoleViewer); err != nil {
		t.Fatalf("AddCollaborator failed: %v", err)
	}

	viewerParams, _ := json.Marshal(CallToolParams{
		Name: "container_exec",
		Arguments: map[string]interface{}{
			"app_id":  appID,
			"command": "ls -la",
		},
	})
	respViewer := srv.ProcessRPC(ctx, viewerUser, JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      103,
		Method:  "tools/call",
		Params:  viewerParams,
	})
	callResViewer := respViewer.Result.(CallToolResult)
	// viewer is blocked by developer access check before token gate
	if !callResViewer.IsError || len(callResViewer.Content) == 0 || !strings.Contains(callResViewer.Content[0].Text, "access") {
		t.Errorf("expected access error for viewer user calling container_exec, got %+v", callResViewer)
	}

	// App owner (developer access) with AllowContainerExec token calls container_exec -> no running container
	ownerToken := db.APIToken{ID: 998, UserID: regularUser.ID, AllowContainerExec: true}
	ownerExecCtx := context.WithValue(ctx, apiTokenContextKey{}, ownerToken)
	ownerParams, _ := json.Marshal(CallToolParams{
		Name: "container_exec",
		Arguments: map[string]interface{}{
			"app_id":  appID,
			"command": "ls -la",
		},
	})
	respOwner := srv.ProcessRPC(ownerExecCtx, regularUser, JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      104,
		Method:  "tools/call",
		Params:  ownerParams,
	})
	callResOwner := respOwner.Result.(CallToolResult)
	if !callResOwner.IsError || len(callResOwner.Content) == 0 || !strings.Contains(callResOwner.Content[0].Text, "no running container found") {
		t.Errorf("expected 'no running container found' error, got %+v", callResOwner)
	}

	// App owner with token WITHOUT AllowContainerExec (even if AllowServerExec is true) calling container_exec -> must be permission denied
	noContainerToken := db.APIToken{ID: 997, UserID: regularUser.ID, AllowContainerExec: false, AllowServerExec: true}
	noContainerCtx := context.WithValue(ctx, apiTokenContextKey{}, noContainerToken)
	respNoContainer := srv.ProcessRPC(noContainerCtx, regularUser, JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      105,
		Method:  "tools/call",
		Params:  ownerParams,
	})
	callResNoContainer := respNoContainer.Result.(CallToolResult)
	if !callResNoContainer.IsError || len(callResNoContainer.Content) == 0 || !strings.Contains(callResNoContainer.Content[0].Text, "permission denied: container_exec is restricted") {
		t.Errorf("expected permission denied for container_exec without AllowContainerExec, got %+v", callResNoContainer)
	}
}

func TestMCP_NewTools(t *testing.T) {
	p, store, tmpDir, user := setupTestPanel(t)
	defer store.Close()
	defer os.RemoveAll(tmpDir)

	ctx := context.Background()
	appID := "newtoolsapp"
	if err := store.CreateApp(ctx, appID, "New Tools App", user.ID); err != nil {
		t.Fatalf("CreateApp failed: %v", err)
	}

	srv := NewServer(p)

	// 1. Test file_write_batch
	batchParams, _ := json.Marshal(CallToolParams{
		Name: "file_write_batch",
		Arguments: map[string]interface{}{
			"app_id": appID,
			"files": []map[string]string{
				{"path": "batch1.txt", "content": "hello batch 1\nend\n"},
				{"path": "nested/batch2.txt", "content": "hello batch 2\n"},
			},
		},
	})
	resp := srv.ProcessRPC(ctx, user, JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      201,
		Method:  "tools/call",
		Params:  batchParams,
	})
	if resp.Error != nil {
		t.Fatalf("unexpected RPC error: %+v", resp.Error)
	}
	res := resp.Result.(CallToolResult)
	if res.IsError {
		t.Fatalf("file_write_batch failed: %+v", res)
	}
	var batchOut map[string]interface{}
	if err := json.Unmarshal([]byte(res.Content[0].Text), &batchOut); err != nil {
		t.Fatalf("failed to parse batch output JSON: %v", err)
	}
	if batchOut["count"].(float64) != 2 {
		t.Errorf("expected count 2, got %v", batchOut["count"])
	}

	// Read one of the written files to verify disk persistence
	readParams, _ := json.Marshal(CallToolParams{
		Name: "file_read",
		Arguments: map[string]interface{}{
			"app_id": appID,
			"path":   "nested/batch2.txt",
		},
	})
	readResp := srv.ProcessRPC(ctx, user, JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      202,
		Method:  "tools/call",
		Params:  readParams,
	})
	readRes := readResp.Result.(CallToolResult)
	if readRes.IsError || readRes.Content[0].Text != "hello batch 2\n" {
		t.Errorf("expected 'hello batch 2\\n', got %+v", readRes)
	}

	// 2. Test file_patch
	patch := "--- a/batch1.txt\n+++ b/batch1.txt\n@@ -1,2 +1,2 @@\n-hello batch 1\n+hello patched 1\n end\n"
	patchParams, _ := json.Marshal(CallToolParams{
		Name: "file_patch",
		Arguments: map[string]interface{}{
			"app_id": appID,
			"patch":  patch,
		},
	})
	patchResp := srv.ProcessRPC(ctx, user, JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      203,
		Method:  "tools/call",
		Params:  patchParams,
	})
	patchRes := patchResp.Result.(CallToolResult)
	if patchRes.IsError {
		t.Fatalf("file_patch failed: %+v", patchRes)
	}

	// Verify the patch was applied
	readPatchParams, _ := json.Marshal(CallToolParams{
		Name: "file_read",
		Arguments: map[string]interface{}{
			"app_id": appID,
			"path":   "batch1.txt",
		},
	})
	readPatchResp := srv.ProcessRPC(ctx, user, JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      204,
		Method:  "tools/call",
		Params:  readPatchParams,
	})
	readPatchRes := readPatchResp.Result.(CallToolResult)
	if readPatchRes.IsError || !strings.Contains(readPatchRes.Content[0].Text, "hello patched 1") {
		t.Errorf("patch not reflected in file content, got: %+v", readPatchRes)
	}

	// 3. Test git_pull on non-git app returns expected error
	gitParams, _ := json.Marshal(CallToolParams{
		Name: "git_pull",
		Arguments: map[string]interface{}{
			"app_id": appID,
		},
	})
	gitResp := srv.ProcessRPC(ctx, user, JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      205,
		Method:  "tools/call",
		Params:  gitParams,
	})
	gitRes := gitResp.Result.(CallToolResult)
	if !gitRes.IsError || !strings.Contains(gitRes.Content[0].Text, "no git repository configured") {
		t.Errorf("expected 'no git repository configured' error, got %+v", gitRes)
	}

	// 4. Test app_health_check
	healthParams, _ := json.Marshal(CallToolParams{
		Name: "app_health_check",
		Arguments: map[string]interface{}{
			"app_id": appID,
		},
	})
	healthResp := srv.ProcessRPC(ctx, user, JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      206,
		Method:  "tools/call",
		Params:  healthParams,
	})
	healthRes := healthResp.Result.(CallToolResult)
	if healthRes.IsError {
		t.Fatalf("app_health_check failed: %+v", healthRes)
	}
	var healthOut map[string]interface{}
	if err := json.Unmarshal([]byte(healthRes.Content[0].Text), &healthOut); err != nil {
		t.Fatalf("failed to parse health output JSON: %v", err)
	}
	if healthOut["app_id"] != appID {
		t.Errorf("expected app_id %s, got %v", appID, healthOut["app_id"])
	}

	// 5. Test file_search by content query (grep)
	searchGrepParams, _ := json.Marshal(CallToolParams{
		Name: "file_search",
		Arguments: map[string]interface{}{
			"app_id": appID,
			"query":  "patched",
		},
	})
	grepResp := srv.ProcessRPC(ctx, user, JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      207,
		Method:  "tools/call",
		Params:  searchGrepParams,
	})
	grepRes := grepResp.Result.(CallToolResult)
	if grepRes.IsError {
		t.Fatalf("file_search (grep) failed: %+v", grepRes)
	}
	var grepOut map[string]interface{}
	if err := json.Unmarshal([]byte(grepRes.Content[0].Text), &grepOut); err != nil {
		t.Fatalf("failed to parse grep output JSON: %v", err)
	}
	if grepOut["total_matches"].(float64) < 1 {
		t.Errorf("expected at least 1 match for 'patched', got %v", grepOut["total_matches"])
	}

	// 6. Test file_search by glob pattern (find)
	searchFindParams, _ := json.Marshal(CallToolParams{
		Name: "file_search",
		Arguments: map[string]interface{}{
			"app_id":  appID,
			"pattern": "*.txt",
		},
	})
	findResp := srv.ProcessRPC(ctx, user, JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      208,
		Method:  "tools/call",
		Params:  searchFindParams,
	})
	findRes := findResp.Result.(CallToolResult)
	if findRes.IsError {
		t.Fatalf("file_search (find) failed: %+v", findRes)
	}
	var findOut map[string]interface{}
	if err := json.Unmarshal([]byte(findRes.Content[0].Text), &findOut); err != nil {
		t.Fatalf("failed to parse find output JSON: %v", err)
	}
	if findOut["total_files"].(float64) < 2 {
		t.Errorf("expected at least 2 files matching '*.txt', got %v", findOut["total_files"])
	}
}

func TestMCP_WorkspaceManifest_And_FileEnhancements(t *testing.T) {
	p, store, tmpDir, user := setupTestPanel(t)
	defer store.Close()
	defer os.RemoveAll(tmpDir)

	ctx := context.Background()
	appID := "manifest-test-app"
	if err := store.CreateApp(ctx, appID, "Manifest App", user.ID); err != nil {
		t.Fatalf("CreateApp failed: %v", err)
	}
	appDir := p.Store.Path(appID)
	_ = os.MkdirAll(filepath.Join(appDir, "src"), 0750)
	_ = os.MkdirAll(filepath.Join(appDir, "node_modules", "pkg"), 0750)
	_ = os.MkdirAll(filepath.Join(appDir, ".git"), 0750)
	_ = os.WriteFile(filepath.Join(appDir, "src", "index.js"), []byte("console.log('hello');\nline2\nline3\nline4\n"), 0640)
	_ = os.WriteFile(filepath.Join(appDir, "node_modules", "pkg", "ignored.js"), []byte("bad"), 0640)
	_ = os.WriteFile(filepath.Join(appDir, ".gitignore"), []byte("*.log\nsecret/\n"), 0640)
	_ = os.WriteFile(filepath.Join(appDir, "test.log"), []byte("log data"), 0640)

	srv := NewServer(p)

	// 1. workspace_manifest (default: hash=true)
	maniCall, _ := json.Marshal(CallToolParams{
		Name: "workspace_manifest",
		Arguments: map[string]interface{}{
			"app_id": appID,
		},
	})
	resp := srv.ProcessRPC(ctx, user, JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      301,
		Method:  "tools/call",
		Params:  maniCall,
	})
	res := resp.Result.(CallToolResult)
	if res.IsError {
		t.Fatalf("workspace_manifest failed: %+v", res)
	}
	var maniRes ManifestResult
	if err := json.Unmarshal([]byte(res.Content[0].Text), &maniRes); err != nil {
		t.Fatalf("failed unmarshaling manifest: %v", err)
	}

	foundIndex := false
	for _, f := range maniRes.Files {
		if strings.Contains(f.Path, "node_modules") {
			t.Errorf("manifest included ignored directory: %s", f.Path)
		}
		if strings.HasSuffix(f.Path, ".log") {
			t.Errorf("manifest included .gitignore match: %s", f.Path)
		}
		if f.Path == "src/index.js" {
			foundIndex = true
			if f.SHA256 == "" {
				t.Errorf("expected sha256 to be computed for src/index.js")
			}
		}
	}
	if !foundIndex {
		t.Errorf("expected src/index.js in manifest")
	}

	// 2. workspace_manifest with hash=false
	maniNoHashCall, _ := json.Marshal(CallToolParams{
		Name: "workspace_manifest",
		Arguments: map[string]interface{}{
			"app_id": appID,
			"hash":   false,
		},
	})
	respNoHash := srv.ProcessRPC(ctx, user, JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      302,
		Method:  "tools/call",
		Params:  maniNoHashCall,
	})
	var maniNoHashRes ManifestResult
	_ = json.Unmarshal([]byte(respNoHash.Result.(CallToolResult).Content[0].Text), &maniNoHashRes)
	for _, f := range maniNoHashRes.Files {
		if f.Path == "src/index.js" && f.SHA256 != "" {
			t.Errorf("expected empty sha256 when hash=false, got %s", f.SHA256)
		}
	}
	// 2b. workspace_manifest with depth=1 (ls directory listing mode)
	maniDepthCall, _ := json.Marshal(CallToolParams{
		Name: "workspace_manifest",
		Arguments: map[string]interface{}{
			"app_id": appID,
			"depth":  1,
			"hash":   false,
		},
	})
	respDepth := srv.ProcessRPC(ctx, user, JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      3022,
		Method:  "tools/call",
		Params:  maniDepthCall,
	})
	var maniDepthRes ManifestResult
	_ = json.Unmarshal([]byte(respDepth.Result.(CallToolResult).Content[0].Text), &maniDepthRes)
	hasSrcDir := false
	for _, f := range maniDepthRes.Files {
		if f.Path == "src" && f.IsDir {
			hasSrcDir = true
		}
		if f.Path == "src/index.js" {
			t.Errorf("expected depth=1 not to traverse inside src/")
		}
	}
	if !hasSrcDir {
		t.Errorf("expected src directory entry with IsDir=true in depth=1 manifest")
	}

	// 3. file_list with recursive=true
	flCall, _ := json.Marshal(CallToolParams{
		Name: "file_list",
		Arguments: map[string]interface{}{
			"app_id":    appID,
			"recursive": true,
		},
	})
	flResp := srv.ProcessRPC(ctx, user, JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      303,
		Method:  "tools/call",
		Params:  flCall,
	})
	flRes := flResp.Result.(CallToolResult)
	if flRes.IsError {
		t.Fatalf("recursive file_list failed: %+v", flRes)
	}
	if !strings.Contains(flRes.Content[0].Text, "src/index.js") {
		t.Errorf("expected recursive file_list to include src/index.js")
	}

	// 4. file_read with offset and limit
	frCall, _ := json.Marshal(CallToolParams{
		Name: "file_read",
		Arguments: map[string]interface{}{
			"app_id": appID,
			"path":   "src/index.js",
			"offset": 2,
			"limit":  2,
		},
	})
	frResp := srv.ProcessRPC(ctx, user, JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      304,
		Method:  "tools/call",
		Params:  frCall,
	})
	frRes := frResp.Result.(CallToolResult)
	if frRes.IsError {
		t.Fatalf("file_read with offset/limit failed: %+v", frRes)
	}
	expectedLines := "line2\nline3"
	if frRes.Content[0].Text != expectedLines {
		t.Errorf("expected %q, got %q", expectedLines, frRes.Content[0].Text)
	}
}

func TestMCP_WorkspaceApply_And_EnvSetBatch(t *testing.T) {
	p, store, tmpDir, user := setupTestPanel(t)
	defer store.Close()
	defer os.RemoveAll(tmpDir)

	ctx := context.Background()
	appID := "apply-batch-app"
	if err := store.CreateApp(ctx, appID, "Apply Batch App", user.ID); err != nil {
		t.Fatalf("CreateApp failed: %v", err)
	}
	appDir := p.Store.Path(appID)
	_ = os.MkdirAll(appDir, 0750)
	_ = os.WriteFile(filepath.Join(appDir, "delete_me.txt"), []byte("bye"), 0640)

	srv := NewServer(p)

	// 1. workspace_apply dry_run: true
	applyDryParams, _ := json.Marshal(CallToolParams{
		Name: "workspace_apply",
		Arguments: map[string]interface{}{
			"app_id": appID,
			"writes": []map[string]string{
				{"path": "new_file.txt", "content": "hello world"},
			},
			"deletes": []string{"delete_me.txt"},
			"dry_run": true,
		},
	})
	dryResp := srv.ProcessRPC(ctx, user, JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      401,
		Method:  "tools/call",
		Params:  applyDryParams,
	})
	dryRes := dryResp.Result.(CallToolResult)
	if dryRes.IsError {
		t.Fatalf("dry_run workspace_apply failed: %+v", dryRes)
	}
	// Verify file was NOT created or deleted during dry run
	if _, err := os.Stat(filepath.Join(appDir, "new_file.txt")); !os.IsNotExist(err) {
		t.Errorf("new_file.txt should not exist after dry run")
	}
	if _, err := os.Stat(filepath.Join(appDir, "delete_me.txt")); err != nil {
		t.Errorf("delete_me.txt should still exist after dry run")
	}

	// 2. workspace_apply execution
	applyParams, _ := json.Marshal(CallToolParams{
		Name: "workspace_apply",
		Arguments: map[string]interface{}{
			"app_id": appID,
			"writes": []map[string]string{
				{"path": "new_file.txt", "content": "hello world"},
			},
			"deletes": []string{"delete_me.txt"},
			"dry_run": false,
		},
	})
	applyResp := srv.ProcessRPC(ctx, user, JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      402,
		Method:  "tools/call",
		Params:  applyParams,
	})
	applyRes := applyResp.Result.(CallToolResult)
	if applyRes.IsError {
		t.Fatalf("workspace_apply execution failed: %+v", applyRes)
	}
	// Verify disk state
	if b, err := os.ReadFile(filepath.Join(appDir, "new_file.txt")); err != nil || string(b) != "hello world" {
		t.Errorf("new_file.txt not created with expected content: %v", err)
	}
	if _, err := os.Stat(filepath.Join(appDir, "delete_me.txt")); !os.IsNotExist(err) {
		t.Errorf("delete_me.txt was not deleted")
	}

	// 3. env_set_batch
	envBatchParams, _ := json.Marshal(CallToolParams{
		Name: "env_set_batch",
		Arguments: map[string]interface{}{
			"app_id": appID,
			"variables": map[string]string{
				"PORT":     "8080",
				"NODE_ENV": "production",
			},
		},
	})
	envResp := srv.ProcessRPC(ctx, user, JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      403,
		Method:  "tools/call",
		Params:  envBatchParams,
	})
	envRes := envResp.Result.(CallToolResult)
	if envRes.IsError {
		t.Fatalf("env_set_batch failed: %+v", envRes)
	}
	savedEnv, _ := store.GetPanelEnv(ctx, appID)
	if !strings.Contains(savedEnv, "PORT=8080") || !strings.Contains(savedEnv, "NODE_ENV=production") {
		t.Errorf("saved env missing keys: %s", savedEnv)
	}

	// 4. env_set with batch variables map
	envSetBatchMap, _ := json.Marshal(CallToolParams{
		Name: "env_set",
		Arguments: map[string]interface{}{
			"app_id": appID,
			"variables": map[string]string{
				"API_KEY": "secret123",
			},
		},
	})
	respSetBatch := srv.ProcessRPC(ctx, user, JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      404,
		Method:  "tools/call",
		Params:  envSetBatchMap,
	})
	if respSetBatch.Result.(CallToolResult).IsError {
		t.Fatalf("env_set with variables failed: %+v", respSetBatch.Result)
	}
	savedEnv, _ = store.GetPanelEnv(ctx, appID)
	if !strings.Contains(savedEnv, "API_KEY=secret123") {
		t.Errorf("expected API_KEY in saved env: %s", savedEnv)
	}
}

func TestMCP_AppCreate_And_DeployFeatures(t *testing.T) {
	p, store, tmpDir, user := setupTestPanel(t)
	defer store.Close()
	defer os.RemoveAll(tmpDir)

	ctx := context.Background()
	srv := NewServer(p)

	// 1. app_create tool
	createParams, _ := json.Marshal(CallToolParams{
		Name: "app_create",
		Arguments: map[string]interface{}{
			"name":            "my-new-app",
			"compose_content": "services:\n  web:\n    image: nginx:alpine\n",
		},
	})
	createResp := srv.ProcessRPC(ctx, user, JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      501,
		Method:  "tools/call",
		Params:  createParams,
	})
	createRes := createResp.Result.(CallToolResult)
	if createRes.IsError {
		t.Fatalf("app_create failed: %+v", createRes)
	}
	var createOut map[string]interface{}
	_ = json.Unmarshal([]byte(createRes.Content[0].Text), &createOut)
	newAppID := createOut["app_id"].(string)
	if newAppID == "" {
		t.Fatalf("expected non-empty app_id from app_create")
	}

	app, err := store.GetApp(ctx, newAppID)
	if err != nil {
		t.Fatalf("GetApp failed for created app %s: %v", newAppID, err)
	}
	if app.Name != "my-new-app" {
		t.Errorf("expected name 'my-new-app', got %s", app.Name)
	}

	// 2. Start a compose job and test busy error check
	wsPath := p.Store.Path(newAppID)
	blockCh := make(chan struct{})
	jobID, err := p.StartComposeJob(newAppID, "testproj", []string{filepath.Join(wsPath, "docker-compose.yml")}, "Deploy", func(ctx context.Context, dir string, paths []string, project string, w io.Writer, envs []string) dockerx.Result {
		_, _ = w.Write([]byte("line 1\nline 2\nline 3\n"))
		<-blockCh
		return dockerx.Result{OK: true}
	}, "")
	if err != nil {
		t.Fatalf("StartComposeJob failed: %v", err)
	}

	// Verify second deploy attempt while running returns busy error with job_id
	_, busyErr := p.StartComposeJob(newAppID, "testproj", []string{filepath.Join(wsPath, "docker-compose.yml")}, "Redeploy", func(ctx context.Context, dir string, paths []string, project string, w io.Writer, envs []string) dockerx.Result {
		return dockerx.Result{OK: true}
	}, "")
	if busyErr == nil {
		t.Errorf("expected busy error on concurrent job start")
	} else {
		if !strings.Contains(busyErr.Error(), jobID) || !strings.Contains(busyErr.Error(), "Poll deploy_status") {
			t.Errorf("expected busy error to contain job_id and 'Poll deploy_status', got: %v", busyErr)
		}
	}

	// Allow goroutine to start and write logs
	time.Sleep(50 * time.Millisecond)

	// 3. deploy_log_tail with since_offset while running
	tailParams, _ := json.Marshal(CallToolParams{
		Name: "deploy_log_tail",
		Arguments: map[string]interface{}{
			"job_id":       jobID,
			"since_offset": 0,
		},
	})
	tailResp := srv.ProcessRPC(ctx, user, JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      502,
		Method:  "tools/call",
		Params:  tailParams,
	})
	tailRes := tailResp.Result.(CallToolResult)
	if tailRes.IsError {
		t.Fatalf("deploy_log_tail failed: %+v", tailRes)
	}
	var tailOut map[string]interface{}
	_ = json.Unmarshal([]byte(tailRes.Content[0].Text), &tailOut)
	if !strings.Contains(tailOut["new_output"].(string), "line 2") || !strings.Contains(tailOut["new_output"].(string), "line 3") {
		t.Errorf("expected tail output to contain 'line 2' and 'line 3', got %q", tailOut["new_output"])
	}

	close(blockCh)
	time.Sleep(50 * time.Millisecond)

	// 4. app_get with include_health:true
	appGetParams, _ := json.Marshal(CallToolParams{
		Name: "app_get",
		Arguments: map[string]interface{}{
			"app_id":         newAppID,
			"include_health": true,
		},
	})
	appGetResp := srv.ProcessRPC(ctx, user, JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      503,
		Method:  "tools/call",
		Params:  appGetParams,
	})
	appGetRes := appGetResp.Result.(CallToolResult)
	if appGetRes.IsError {
		t.Fatalf("app_get with include_health failed: %+v", appGetRes)
	}
	var appGetOut map[string]interface{}
	_ = json.Unmarshal([]byte(appGetRes.Content[0].Text), &appGetOut)
	if _, ok := appGetOut["health"]; !ok {
		t.Errorf("expected 'health' key in app_get result when include_health:true")
	}

	// 5. deploy with wait_seconds and rebuild
	deployParams, _ := json.Marshal(CallToolParams{
		Name: "deploy",
		Arguments: map[string]interface{}{
			"app_id":       newAppID,
			"rebuild":      true,
			"wait_seconds": 1,
		},
	})
	deployResp := srv.ProcessRPC(ctx, user, JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      504,
		Method:  "tools/call",
		Params:  deployParams,
	})
	deployRes := deployResp.Result.(CallToolResult)
	if deployRes.IsError {
		t.Fatalf("deploy with rebuild & wait_seconds failed: %+v", deployRes)
	}
	var deployOut map[string]interface{}
	_ = json.Unmarshal([]byte(deployRes.Content[0].Text), &deployOut)
	if deployOut["job_id"] == nil || deployOut["job_id"] == "" {
		t.Errorf("expected job_id in deploy result, got: %+v", deployOut)
	}
}

// TestMCP_TokenAndContextOptimizations verifies the token reduction mechanisms:
// 1. Minified JSON serialization.
// 2. Short 16-hex SHA256 hashes by default, full SHA with full_hash:true.
// 3. Exclusion of lock files and *.map by default, inclusion with include_locks:true.
// 4. Differential sync check via workspace_manifest(local_files: {...}).
// 5. Deploy summary_only mode suppressing successful build logs.
// 6. file_read safety guard truncating >256KB files without full:true.
func TestMCP_TokenAndContextOptimizations(t *testing.T) {
	p, store, tmpDir, user := setupTestPanel(t)
	defer store.Close()
	defer os.RemoveAll(tmpDir)

	ctx := context.Background()
	srv := NewServer(p)
	appID := "app_tokenopt"

	if err := store.CreateApp(ctx, appID, "Token Opt App", user.ID); err != nil {
		t.Fatalf("CreateApp failed: %v", err)
	}

	appDir := filepath.Join(tmpDir, appID)
	_ = os.MkdirAll(filepath.Join(appDir, "src"), 0750)
	_ = os.WriteFile(filepath.Join(appDir, "src", "index.js"), []byte("console.log('optimized');"), 0640)
	_ = os.WriteFile(filepath.Join(appDir, "package-lock.json"), []byte(`{"name":"lock","version":"1.0"}`), 0640)
	_ = os.WriteFile(filepath.Join(appDir, "app.min.js"), []byte("/*minified*/"), 0640)
	_ = os.WriteFile(filepath.Join(appDir, "app.js.map"), []byte(`{"version":3}`), 0640)
	_ = os.WriteFile(filepath.Join(appDir, "old_remote.txt"), []byte("to delete"), 0640)

	// 1. Minified JSON and default exclusions / short SHA
	maniParams, _ := json.Marshal(CallToolParams{
		Name: "workspace_manifest",
		Arguments: map[string]interface{}{
			"app_id": appID,
		},
	})
	resp := srv.ProcessRPC(ctx, user, JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      601,
		Method:  "tools/call",
		Params:  maniParams,
	})
	res := resp.Result.(CallToolResult)
	if res.IsError {
		t.Fatalf("workspace_manifest failed: %+v", res)
	}
	respText := res.Content[0].Text

	// Verify Minified JSON: should not contain indented newline-spaces like "\n  \""
	if strings.Contains(respText, "\n  \"") {
		t.Errorf("expected minified JSON without indentation, got formatted: %s", respText)
	}

	var maniRes ManifestResult
	if err := json.Unmarshal([]byte(respText), &maniRes); err != nil {
		t.Fatalf("failed unmarshaling manifest: %v", err)
	}

	var indexEntry *ManifestEntry
	foundMinJs := false
	for _, f := range maniRes.Files {
		if f.Path == "package-lock.json" {
			t.Errorf("expected package-lock.json to be excluded by default")
		}
		if f.Path == "app.js.map" {
			t.Errorf("expected build/map files to be excluded by default, found: %s", f.Path)
		}
		if f.Path == "app.min.js" {
			foundMinJs = true
		}
		if f.Path == "src/index.js" {
			copyF := f
			indexEntry = &copyF
		}
	}
	if !foundMinJs {
		t.Errorf("expected production asset app.min.js to NOT be excluded")
	}
	if indexEntry == nil {
		t.Fatalf("expected src/index.js in manifest files")
	}
	if len(indexEntry.SHA256) != 16 {
		t.Errorf("expected short 16-hex SHA256 by default, got len=%d (%s)", len(indexEntry.SHA256), indexEntry.SHA256)
	}

	// 2. full_hash: true
	maniFullHashParams, _ := json.Marshal(CallToolParams{
		Name: "workspace_manifest",
		Arguments: map[string]interface{}{
			"app_id":    appID,
			"full_hash": true,
		},
	})
	respFH := srv.ProcessRPC(ctx, user, JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      602,
		Method:  "tools/call",
		Params:  maniFullHashParams,
	})
	var maniFHRes ManifestResult
	_ = json.Unmarshal([]byte(respFH.Result.(CallToolResult).Content[0].Text), &maniFHRes)
	for _, f := range maniFHRes.Files {
		if f.Path == "src/index.js" && len(f.SHA256) != 64 {
			t.Errorf("expected full 64-hex SHA256 with full_hash:true, got len=%d (%s)", len(f.SHA256), f.SHA256)
		}
	}

	// 3. include_locks: true
	maniLocksParams, _ := json.Marshal(CallToolParams{
		Name: "workspace_manifest",
		Arguments: map[string]interface{}{
			"app_id":        appID,
			"include_locks": true,
		},
	})
	respLocks := srv.ProcessRPC(ctx, user, JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      603,
		Method:  "tools/call",
		Params:  maniLocksParams,
	})
	var maniLocksRes ManifestResult
	_ = json.Unmarshal([]byte(respLocks.Result.(CallToolResult).Content[0].Text), &maniLocksRes)
	hasLock := false
	for _, f := range maniLocksRes.Files {
		if f.Path == "package-lock.json" {
			hasLock = true
			break
		}
	}
	if !hasLock {
		t.Errorf("expected package-lock.json to be included when include_locks:true")
	}

	// 4. Differential sync check via local_files
	diffParams, _ := json.Marshal(CallToolParams{
		Name: "workspace_manifest",
		Arguments: map[string]interface{}{
			"app_id": appID,
			"local_files": map[string]string{
				"src/index.js":   indexEntry.SHA256,
				"src/newfile.js": "aabbccddeeff0011",
			},
		},
	})
	diffResp := srv.ProcessRPC(ctx, user, JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      604,
		Method:  "tools/call",
		Params:  diffParams,
	})
	var diffRes ManifestDiffResult
	if err := json.Unmarshal([]byte(diffResp.Result.(CallToolResult).Content[0].Text), &diffRes); err != nil {
		t.Fatalf("failed unmarshaling diff response: %v", err)
	}
	if !diffRes.Diff {
		t.Errorf("expected diff: true in differential sync response")
	}
	if diffRes.InSyncCount != 1 {
		t.Errorf("expected 1 in_sync file (src/index.js), got %d", diffRes.InSyncCount)
	}
	if len(diffRes.ToUpload) != 1 || diffRes.ToUpload[0] != "src/newfile.js" {
		t.Errorf("expected to_upload: ['src/newfile.js'], got %+v", diffRes.ToUpload)
	}
	hasOldRemote := false
	for _, d := range diffRes.ToDelete {
		if d == "old_remote.txt" {
			hasOldRemote = true
		}
	}
	if !hasOldRemote {
		t.Errorf("expected old_remote.txt in to_delete list, got %+v", diffRes.ToDelete)
	}

	// 5. file_read safety guard (>128KB)
	bigFilePath := filepath.Join(appDir, "large_file.txt")
	bigData := strings.Repeat("0123456789abcdef\n", 10000) // ~170KB
	_ = os.WriteFile(bigFilePath, []byte(bigData), 0640)

	frTruncParams, _ := json.Marshal(CallToolParams{
		Name: "file_read",
		Arguments: map[string]interface{}{
			"app_id": appID,
			"path":   "large_file.txt",
		},
	})
	frTruncResp := srv.ProcessRPC(ctx, user, JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      605,
		Method:  "tools/call",
		Params:  frTruncParams,
	})
	truncText := frTruncResp.Result.(CallToolResult).Content[0].Text
	if !strings.HasPrefix(truncText, "[File truncated: showing first 128KB") {
		t.Errorf("expected truncation notice for large file read, got: %s", truncText[:100])
	}

	frFullParams, _ := json.Marshal(CallToolParams{
		Name: "file_read",
		Arguments: map[string]interface{}{
			"app_id": appID,
			"path":   "large_file.txt",
			"full":   true,
		},
	})
	frFullResp := srv.ProcessRPC(ctx, user, JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      606,
		Method:  "tools/call",
		Params:  frFullParams,
	})
	fullText := frFullResp.Result.(CallToolResult).Content[0].Text
	if strings.HasPrefix(fullText, "[File truncated:") {
		t.Errorf("did not expect truncation when full:true was passed")
	}
	if len(fullText) != len(bigData) {
		t.Errorf("expected full read length %d, got %d", len(bigData), len(fullText))
	}

	// 6. deploy_status summary_only mode
	jobID, err := p.StartComposeJob(appID, "tokenopt_proj", []string{}, "Deploy", func(ctx context.Context, dir string, paths []string, project string, w io.Writer, envs []string) dockerx.Result {
		_, _ = w.Write([]byte("Step 1/10: downloading base image...\nStep 2/10: npm install complete\nStep 10/10: done\n"))
		return dockerx.Result{OK: true}
	}, "")
	if err != nil {
		t.Fatalf("StartComposeJob failed: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	// summary_only: true (default)
	statusSummaryParams, _ := json.Marshal(CallToolParams{
		Name: "deploy_status",
		Arguments: map[string]interface{}{
			"job_id": jobID,
		},
	})
	statSummResp := srv.ProcessRPC(ctx, user, JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      607,
		Method:  "tools/call",
		Params:  statusSummaryParams,
	})
	var summOut map[string]interface{}
	_ = json.Unmarshal([]byte(statSummResp.Result.(CallToolResult).Content[0].Text), &summOut)
	if summOut["output"] != nil {
		t.Errorf("expected raw output to be suppressed in summary_only mode on success, got: %+v", summOut["output"])
	}
	if summOut["message"] != "Deployment completed successfully" {
		t.Errorf("expected success message in summary output, got: %+v", summOut["message"])
	}

	// summary_only: false (full raw output)
	statusFullParams, _ := json.Marshal(CallToolParams{
		Name: "deploy_status",
		Arguments: map[string]interface{}{
			"job_id":       jobID,
			"summary_only": false,
		},
	})
	statFullResp := srv.ProcessRPC(ctx, user, JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      608,
		Method:  "tools/call",
		Params:  statusFullParams,
	})
	var fullOut map[string]interface{}
	_ = json.Unmarshal([]byte(statFullResp.Result.(CallToolResult).Content[0].Text), &fullOut)
	if fullOut["output"] == nil || !strings.Contains(fullOut["output"].(string), "Step 1/10") {
		t.Errorf("expected full raw output when summary_only:false, got: %+v", fullOut)
	}

	// 7. compose_get summary:true and service:"web"
	composeContent := "services:\n  web:\n    image: nginx:alpine\n    ports:\n      - \"8080:80\"\n  db:\n    image: postgres:15\n"
	_ = os.WriteFile(filepath.Join(appDir, "docker-compose.yml"), []byte(composeContent), 0640)

	compSummParams, _ := json.Marshal(CallToolParams{
		Name: "compose_get",
		Arguments: map[string]interface{}{
			"app_id":  appID,
			"summary": true,
		},
	})
	compSummResp := srv.ProcessRPC(ctx, user, JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      609,
		Method:  "tools/call",
		Params:  compSummParams,
	})
	var compSummOut map[string]interface{}
	_ = json.Unmarshal([]byte(compSummResp.Result.(CallToolResult).Content[0].Text), &compSummOut)
	svcs, ok := compSummOut["services"].(map[string]interface{})
	if !ok || svcs["web"] == nil || svcs["db"] == nil {
		t.Errorf("expected services summary in compose_get, got: %+v", compSummOut)
	}

	compSvcParams, _ := json.Marshal(CallToolParams{
		Name: "compose_get",
		Arguments: map[string]interface{}{
			"app_id":  appID,
			"service": "web",
		},
	})
	compSvcResp := srv.ProcessRPC(ctx, user, JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      610,
		Method:  "tools/call",
		Params:  compSvcParams,
	})
	svcText := compSvcResp.Result.(CallToolResult).Content[0].Text
	if !strings.Contains(svcText, "nginx:alpine") || strings.Contains(svcText, "postgres:15") {
		t.Errorf("expected only web service in compose_get, got: %s", svcText)
	}

	// 8. file_search names_only:true
	searchParams, _ := json.Marshal(CallToolParams{
		Name: "file_search",
		Arguments: map[string]interface{}{
			"app_id":     appID,
			"query":      "optimized",
			"names_only": true,
		},
	})
	searchResp := srv.ProcessRPC(ctx, user, JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      611,
		Method:  "tools/call",
		Params:  searchParams,
	})
	var searchOut map[string]interface{}
	_ = json.Unmarshal([]byte(searchResp.Result.(CallToolResult).Content[0].Text), &searchOut)
	if searchOut["matches"] != nil {
		t.Errorf("expected no code snippet matches when names_only:true, got: %+v", searchOut["matches"])
	}
	filesList, _ := searchOut["files"].([]interface{})
	if len(filesList) == 0 || filesList[0] != "src/index.js" {
		t.Errorf("expected ['src/index.js'] in files list, got: %+v", searchOut)
	}
}



