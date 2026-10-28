package agent

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"panel/internal/agent/omprpc"
)

func TestValidAppIDAndSiteDir(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "blog"), 0o755); err != nil {
		t.Fatal(err)
	}
	m := NewManager(nil, nil, root)
	for _, bad := range []string{"", "..", ".", "../etc", "a/b", ".hidden", "a b", strings.Repeat("x", 200)} {
		if ValidAppID(bad) {
			t.Errorf("ValidAppID(%q) = true", bad)
		}
		if _, err := m.siteDir(bad); err == nil {
			t.Errorf("siteDir(%q) succeeded", bad)
		}
	}
	if d, err := m.siteDir("blog"); err != nil || d != filepath.Join(root, "blog") {
		t.Errorf("siteDir(blog) = %q, %v", d, err)
	}
	if _, err := m.siteDir("missing"); err == nil {
		t.Error("siteDir(missing) should fail")
	}
}

func TestProviderEnv(t *testing.T) {
	env := ProviderEnv(map[string]string{
		"ai_openai_api_key":         "k1",
		"ai_github-copilot_api_key": "k2",
		"ai_ollama_host":            "http://x:11434",
		"ai_custom_base_url":        "http://y/v1",
		"ai_active_model":           "ignored",
		"ai_empty_api_key":          "",
		"other":                     "ignored",
	})
	got := map[string]bool{}
	for _, e := range env {
		got[e] = true
	}
	for _, want := range []string{"OPENAI_API_KEY=k1", "GITHUB_COPILOT_API_KEY=k2", "OLLAMA_HOST=http://x:11434", "OPENAI_BASE_URL=http://y/v1"} {
		if !got[want] {
			t.Errorf("missing %s in %v", want, env)
		}
	}
	if len(env) != 4 {
		t.Errorf("unexpected env: %v", env)
	}
}

func TestRequestApproval(t *testing.T) {
	m := NewManager(nil, nil, t.TempDir())
	ch, cancel := m.Subscribe()
	defer cancel()

	done := make(chan bool, 1)
	go func() { done <- m.requestApproval(context.Background(), "execute_bash", "t", "msg") }()

	select {
	case ev := <-ch:
		if ev.Type != "approval_required" || ev.Approval == nil || ev.Approval.Tool != "execute_bash" {
			t.Fatalf("unexpected event %+v", ev)
		}
		if !m.ConfirmApproval(ev.Approval.ID, false) {
			t.Fatal("ConfirmApproval did not resolve")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no approval event")
	}
	if <-done {
		t.Fatal("declined approval returned true")
	}

	m.SetAutonomyMode(AutonomyAutonomous)
	if !m.requestApproval(context.Background(), "x", "t", "m") {
		t.Fatal("autonomous mode should auto-approve")
	}
}

// Live check against the real omp binary: the subprocess must actually run in
// the selected directory (root, a site, then a fresh temp dir).
func TestWorkDirSwitchesRealOmp(t *testing.T) {
	if _, err := exec.LookPath("omp"); err != nil {
		t.Skip("omp not installed")
	}
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "blog"), 0o755); err != nil {
		t.Fatal(err)
	}
	m := NewManager(nil, nil, root)
	defer m.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pwd := func() string {
		c, err := m.EnsureStarted(ctx)
		if err != nil {
			t.Fatalf("start: %v", err)
		}
		res, err := c.Bash(ctx, omprpc.BashCommand{Command: "pwd -P"})
		if err != nil {
			t.Fatalf("bash: %v", err)
		}
		return strings.TrimSpace(res.Output)
	}
	real := func(p string) string { r, _ := filepath.EvalSymlinks(p); return r }

	if got := pwd(); got != real(root) {
		t.Fatalf("root cwd = %q, want %q", got, real(root))
	}
	if _, err := m.SetWorkDir(ctx, "site", "blog"); err != nil {
		t.Fatal(err)
	}
	if got := pwd(); got != real(filepath.Join(root, "blog")) {
		t.Fatalf("site cwd = %q", got)
	}
	if _, err := m.SetWorkDir(ctx, "site", "../etc"); err == nil {
		t.Fatal("traversal accepted")
	}
	wd, err := m.SetWorkDir(ctx, "temp", "")
	if err != nil {
		t.Fatal(err)
	}
	if got := pwd(); got != real(wd.Path) || !strings.Contains(got, "nextdeploy-ai-") {
		t.Fatalf("temp cwd = %q (want %q)", got, wd.Path)
	}
	tmp := wd.Path
	if _, err := m.SetWorkDir(ctx, "root", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Errorf("temp dir %s not cleaned up", tmp)
	}
}
