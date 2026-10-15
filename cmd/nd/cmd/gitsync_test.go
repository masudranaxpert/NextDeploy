package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"nd/internal/client"
	"nd/internal/config"
)

// gitSyncServer answers every /mcp git_sync call with the given tool text (or a tool error).
func gitSyncServer(t *testing.T, text string, isError bool) *client.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{"result": map[string]interface{}{
			"isError": isError,
			"content": []map[string]string{{"type": "text", "text": text}},
		}}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	return client.New(config.Config{ServerURL: srv.URL, Token: "t"}, "sess")
}

func TestGuardGitSourceForPush(t *testing.T) {
	active := `{"app_id":"a","configured":true,"active":true,"paused":false,"source":"git","repo_url":"https://github.com/x/y","branch":"main"}`
	paused := `{"app_id":"a","configured":true,"active":false,"paused":true,"source":"files","repo_url":"https://github.com/x/y","branch":"main"}`
	files := `{"app_id":"a","configured":false,"active":false,"paused":false,"source":"files"}`

	err := guardGitSourceForPush(gitSyncServer(t, active, false), "a")
	if err == nil || !strings.Contains(err.Error(), "nd git off a") {
		t.Fatalf("active Git source must block push with a hint, got %v", err)
	}
	if err := guardGitSourceForPush(gitSyncServer(t, paused, false), "a"); err != nil {
		t.Errorf("paused Git must allow push, got %v", err)
	}
	if err := guardGitSourceForPush(gitSyncServer(t, files, false), "a"); err != nil {
		t.Errorf("file app must allow push, got %v", err)
	}
	// Older servers (tool missing / error) must never block a push.
	if err := guardGitSourceForPush(gitSyncServer(t, "unknown tool", true), "a"); err != nil {
		t.Errorf("tool error must not block push, got %v", err)
	}
}

func TestRunGitOffReportsChange(t *testing.T) {
	res := `{"status":{"app_id":"a","configured":true,"active":false,"paused":true,"source":"files","repo_url":"u","branch":"main"},` +
		`"changed":true,"files_copied":2,"notes":["Copied 2 file(s)"],"next_step":"Git sync is off."}`
	cl := gitSyncServer(t, res, false)
	if err := RunGit(cl, []string{"off", "a"}); err != nil {
		t.Fatalf("RunGit off: %v", err)
	}
}

func TestRunGitSurfacesToolError(t *testing.T) {
	cl := gitSyncServer(t, "the Git checkout has uncommitted local edits", true)
	err := RunGit(cl, []string{"on", "a"})
	if err == nil || !strings.Contains(err.Error(), "uncommitted") {
		t.Fatalf("tool error must surface, got %v", err)
	}
}
