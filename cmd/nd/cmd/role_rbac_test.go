package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"nd/internal/client"
	"nd/internal/config"
)

func TestCLI_UserRoleSkipsHostExecOnContainersAndImages(t *testing.T) {
	systemExecCalled := false

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/system/exec":
			systemExecCalled = true
			http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/apps":
			_ = json.NewEncoder(w).Encode([]map[string]interface{}{
				{"id": "my-app", "name": "my-app", "status": "running"},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/apps/my-app":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"app": map[string]string{"ID": "my-app", "Name": "my-app", "Status": "running"},
				"ps": []map[string]string{
					{"Name": "my-app-web-1", "Service": "web", "State": "running", "Status": "Up", "Image": "nginx:alpine"},
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/apps/my-app/compose":
			_ = json.NewEncoder(w).Encode(map[string]string{
				"app_id":  "my-app",
				"content": "services:\n  web:\n    image: nginx:alpine\n",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	cfg := config.Config{
		CurrentAccount: "dev@panel",
		ServerURL:      ts.URL,
		Token:          "tok_user",
		Role:           "user",
		Accounts: map[string]config.AccountConfig{
			"dev@panel": {ServerURL: ts.URL, Token: "tok_user", Username: "dev1", Role: "user"},
		},
	}
	cl := client.New(cfg, "sess-user")

	if err := RunContainers(cl, []string{"--json"}); err != nil {
		t.Fatalf("RunContainers as user failed: %v", err)
	}
	if err := RunImages(cl, []string{"--json"}); err != nil {
		t.Fatalf("RunImages as user failed: %v", err)
	}
	if systemExecCalled {
		t.Fatalf("expected user RunContainers/RunImages NOT to hit /api/v1/system/exec")
	}
}

func TestCLI_UserBlockedOnAdminOnlyCommands(t *testing.T) {
	serverHit := false
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverHit = true
		http.Error(w, "should not be reached", http.StatusForbidden)
	}))
	defer ts.Close()

	cfg := config.Config{
		CurrentAccount: "dev@panel",
		ServerURL:      ts.URL,
		Token:          "tok_user",
		Role:           "user",
		Accounts: map[string]config.AccountConfig{
			"dev@panel": {ServerURL: ts.URL, Token: "tok_user", Username: "dev1", Role: "user"},
		},
	}
	cl := client.New(cfg, "sess-user")

	checks := []struct {
		name string
		fn   func() error
	}{
		{"clean", func() error { return RunClean(cl, nil) }},
		{"server-exec", func() error { return RunServerExec(cl, []string{"id"}) }},
		{"server-shell", func() error { return RunServerShell(cl, nil) }},
		{"volume prune", func() error { return RunVolume(cl, []string{"prune"}) }},
		{"proxy reload", func() error { return RunProxy(cl, []string{"reload"}) }},
		{"proxy engine", func() error { return RunProxy(cl, []string{"engine", "status"}) }},
		{"shield add", func() error { return RunShield(cl, []string{"add", "s1", "1.2.3.4"}) }},
		{"shield rm", func() error { return RunShield(cl, []string{"rm", "1"}) }},
		{"users", func() error { return RunUsers(cl, nil) }},
	}

	for _, c := range checks {
		err := c.fn()
		if err == nil || !strings.Contains(err.Error(), "administrator privileges") {
			t.Fatalf("%s: expected admin privilege error, got %v", c.name, err)
		}
	}
	if serverHit {
		t.Fatalf("expected pre-flight role guard to stop non-admin commands before network calls")
	}
}

func TestCLI_AdminAndQuotaCommands(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/cli/whoami":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"ok":       true,
				"username": "dev1",
				"role":     "user",
				"quota": map[string]interface{}{
					"unlimited":      false,
					"apps_used":      2,
					"max_apps":       5,
					"max_cpus":       2.0,
					"max_memory_mb":  2048,
					"max_storage_mb": 10240,
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/admin/users":
			_ = json.NewEncoder(w).Encode([]map[string]interface{}{
				{"id": 1, "username": "admin1", "role": "admin", "status": "active"},
				{"id": 2, "username": "dev1", "role": "user", "status": "active", "max_apps": 5, "max_cpus": 2.0, "max_memory_mb": 2048, "max_storage_mb": 10240},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	userCl := client.New(config.Config{
		CurrentAccount: "dev",
		ServerURL:      ts.URL,
		Token:          "tok_user",
		Role:           "user",
		Accounts:       map[string]config.AccountConfig{"dev": {ServerURL: ts.URL, Token: "tok_user", Role: "user"}},
	}, "s1")
	if err := RunQuota(userCl, nil); err != nil {
		t.Fatalf("RunQuota failed: %v", err)
	}

	adminCl := client.New(config.Config{
		CurrentAccount: "adm",
		ServerURL:      ts.URL,
		Token:          "tok_adm",
		Role:           "admin",
		Accounts:       map[string]config.AccountConfig{"adm": {ServerURL: ts.URL, Token: "tok_adm", Role: "admin"}},
	}, "s2")
	if err := RunUsers(adminCl, nil); err != nil {
		t.Fatalf("RunUsers as admin failed: %v", err)
	}
}

func TestCLI_HelpUserAdminSections(t *testing.T) {
	capture := func(section string) string {
		old := os.Stderr
		r, w, _ := os.Pipe()
		os.Stderr = w
		var buf bytes.Buffer
		done := make(chan struct{})
		go func() {
			_, _ = io.Copy(&buf, r)
			close(done)
		}()
		PrintHelpSection(section)
		_ = w.Close()
		os.Stderr = old
		<-done
		_ = r.Close()
		return buf.String()
	}

	userHelp := capture("user")
	if !strings.Contains(userHelp, "USER COMMANDS") || strings.Contains(userHelp, "ADMIN COMMANDS") {
		t.Fatalf("expected user help to only contain USER COMMANDS, got:\n%s", userHelp)
	}
	adminHelp := capture("admin")
	if !strings.Contains(adminHelp, "ADMIN COMMANDS") || strings.Contains(adminHelp, "USER COMMANDS") {
		t.Fatalf("expected admin help to only contain ADMIN COMMANDS, got:\n%s", adminHelp)
	}
}
