package mcp

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"panel/internal/db"
)

func callGitSync(t *testing.T, srv *Server, user db.User, id int, args map[string]interface{}) CallToolResult {
	t.Helper()
	params, _ := json.Marshal(CallToolParams{Name: "git_sync", Arguments: args})
	resp := srv.ProcessRPC(context.Background(), user, JSONRPCRequest{JSONRPC: "2.0", ID: id, Method: "tools/call", Params: params})
	return resp.Result.(CallToolResult)
}

func TestMCP_GitSyncStatusOffOn(t *testing.T) {
	p, store, tmpDir, user := setupTestPanel(t)
	defer store.Close()
	defer os.RemoveAll(tmpDir)
	ctx := context.Background()

	const appID = "git-sync-mcp"
	if err := store.CreateApp(ctx, appID, "Git Sync MCP", user.ID); err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	srv := NewServer(p)

	// No repository: status works and says so; off/on explain what to do.
	res := callGitSync(t, srv, user, 1, map[string]interface{}{"app_id": appID})
	if res.IsError || !strings.Contains(res.Content[0].Text, `"configured":false`) {
		t.Fatalf("status without repo: %+v", res)
	}
	res = callGitSync(t, srv, user, 2, map[string]interface{}{"app_id": appID, "action": "off"})
	if !res.IsError || !strings.Contains(res.Content[0].Text, "no Git repository is configured") {
		t.Fatalf("off without repo should fail clearly, got %+v", res)
	}

	if err := store.UpsertAppGitConfig(ctx, db.AppGitConfig{
		AppID: appID, Provider: "github", RepoURL: "https://user:secret@github.com/x/y", Branch: "main", AuthMode: "public",
	}); err != nil {
		t.Fatal(err)
	}

	res = callGitSync(t, srv, user, 3, map[string]interface{}{"app_id": appID, "action": "status"})
	var st struct {
		Active  bool   `json:"active"`
		Paused  bool   `json:"paused"`
		Source  string `json:"source"`
		RepoURL string `json:"repo_url"`
	}
	if err := json.Unmarshal([]byte(res.Content[0].Text), &st); err != nil || !st.Active || st.Paused || st.Source != "git" {
		t.Fatalf("expected active git status, got %q (%v)", res.Content[0].Text, err)
	}
	if strings.Contains(st.RepoURL, "secret") {
		t.Errorf("repo credentials must be stripped from status, got %q", st.RepoURL)
	}

	res = callGitSync(t, srv, user, 4, map[string]interface{}{"app_id": appID, "action": "off"})
	if res.IsError || !strings.Contains(res.Content[0].Text, `"changed":true`) {
		t.Fatalf("off: %+v", res)
	}
	res = callGitSync(t, srv, user, 5, map[string]interface{}{"app_id": appID})
	if err := json.Unmarshal([]byte(res.Content[0].Text), &st); err != nil || st.Active || !st.Paused || st.Source != "files" {
		t.Fatalf("expected paused status, got %q", res.Content[0].Text)
	}

	// git_pull must refuse while Git sync is off instead of silently pulling.
	pullParams, _ := json.Marshal(CallToolParams{Name: "git_pull", Arguments: map[string]interface{}{"app_id": appID}})
	pull := srv.ProcessRPC(ctx, user, JSONRPCRequest{JSONRPC: "2.0", ID: 6, Method: "tools/call", Params: pullParams}).Result.(CallToolResult)
	if !pull.IsError || !strings.Contains(pull.Content[0].Text, "git sync is off") {
		t.Errorf("git_pull while off should be refused, got %+v", pull)
	}

	res = callGitSync(t, srv, user, 7, map[string]interface{}{"app_id": appID, "action": "on"})
	if res.IsError || !strings.Contains(res.Content[0].Text, `"active":true`) {
		t.Fatalf("on: %+v", res)
	}
	res = callGitSync(t, srv, user, 8, map[string]interface{}{"app_id": appID, "action": "bogus"})
	if !res.IsError {
		t.Errorf("unknown action must fail, got %+v", res)
	}
}
