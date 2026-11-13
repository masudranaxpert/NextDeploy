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

func TestRunGitSubcommands(t *testing.T) {
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == "GET" && r.URL.Path == "/api/v1/git/providers":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": true,
				"providers": []map[string]any{{
					"id":               "gp-1",
					"provider":         "GitHub",
					"name":             "octocat",
					"auth_method":      "GitHub App",
					"status":           "connected",
					"repos_accessible": 12,
				}},
			})
		case r.Method == "POST" && r.URL.Path == "/api/v1/apps/app-1/git/connect":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["repo_url"] != "https://github.com/octocat/demo" {
				t.Fatalf("unexpected repo_url: %v", body["repo_url"])
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		case r.Method == "POST" && r.URL.Path == "/api/v1/apps/app-1/git/sync":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "output": "Already up to date."})
		case r.Method == "POST" && r.URL.Path == "/api/v1/apps/app-1/git/push":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["message"] != "deploy wp" {
				t.Fatalf("unexpected message: %v", body["message"])
			}
			if body["force"] != true {
				t.Fatalf("expected force=true in push body, got %v", body["force"])
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "repo_url": "https://github.com/octocat/demo", "branch": "main", "commit": "abc1234", "is_php": true})
		case r.Method == "DELETE" && r.URL.Path == "/api/v1/apps/app-1/git":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		case r.Method == "GET" && r.URL.Path == "/api/v1/apps/app-1/git/providers/1/repos":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"repos": []map[string]any{{
					"full_name":      "octocat/demo",
					"default_branch": "main",
					"private":        false,
					"clone_url":      "https://github.com/octocat/demo.git",
				}},
			})
		case r.Method == "GET" && r.URL.Path == "/api/v1/apps/app-1/git/branches":
			if r.URL.Query().Get("url") != "https://github.com/octocat/demo" {
				t.Fatalf("unexpected url query: %s", r.URL.Query().Get("url"))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "branches": []string{"main", "dev"}})
		case r.Method == "GET" && r.URL.Path == "/api/v1/git/webhooks":
			_ = json.NewEncoder(w).Encode([]map[string]any{{
				"app_id":     "app-1",
				"provider":   "github",
				"repo":       "octocat/demo",
				"event":      "push",
				"sha":        "abc123456789",
				"status":     "synced",
				"created_at": "2026-11-13T08:00:00Z",
			}})
		default:
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "not found"})
		}
	}))
	defer srv.Close()

	cl := client.New(config.Config{ServerURL: srv.URL, Token: "tok"}, "test")

	tests := [][]string{
		{"providers"},
		{"repos", "1", "app-1"},
		{"branches", "app-1", "--repo", "https://github.com/octocat/demo"},
		{"webhooks", "--limit", "5"},
		{"connect", "app-1", "https://github.com/octocat/demo", "--branch", "main"},
		{"sync", "app-1"},
		{"push", "app-1", "--force", "-m", "deploy wp"},
		{"disconnect", "app-1"},
	}
	for _, args := range tests {
		if err := RunGit(cl, args); err != nil {
			t.Fatalf("RunGit(%v) failed: %v", args, err)
		}
	}

	got := strings.Join(calls, " | ")
	for _, want := range []string{
		"GET /api/v1/git/providers",
		"GET /api/v1/apps/app-1/git/providers/1/repos",
		"GET /api/v1/apps/app-1/git/branches",
		"GET /api/v1/git/webhooks",
		"POST /api/v1/apps/app-1/git/connect",
		"POST /api/v1/apps/app-1/git/sync",
		"POST /api/v1/apps/app-1/git/push",
		"DELETE /api/v1/apps/app-1/git",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected call %q in %q", want, got)
		}
	}
}
