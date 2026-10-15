package handlers

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"panel/internal/db"
	"panel/internal/workspace"
)

func gitSyncTestPanel(t *testing.T) (*Panel, string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	tmp := t.TempDir()
	store, err := db.Open(filepath.Join(tmp, "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	p := &Panel{DB: store, Store: workspace.NewStore(tmp), WorkspacesRoot: tmp}

	ctx := context.Background()
	uid, err := store.CreateUser(ctx, "owner", "hash", db.RoleAdmin)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	const appID = "git-sync-app"
	if err := store.CreateApp(ctx, appID, "Git Sync App", uid); err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	if err := store.UpsertAppGitConfig(ctx, db.AppGitConfig{
		AppID: appID, Provider: "github", RepoURL: "https://github.com/example/repo", Branch: "main", AuthMode: "public",
	}); err != nil {
		t.Fatalf("UpsertAppGitConfig: %v", err)
	}
	_ = store.SetAppSourceType(ctx, appID, "git")
	return p, appID
}

func mustGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.email=t@t", "-c", "user.name=t"}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func writeFile(t *testing.T, path, content string, mod time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o640); err != nil {
		t.Fatal(err)
	}
	if !mod.IsZero() {
		if err := os.Chtimes(path, mod, mod); err != nil {
			t.Fatal(err)
		}
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func makeCheckout(t *testing.T, p *Panel, appID string) string {
	t.Helper()
	repo := p.AppCheckoutPath(appID)
	if err := os.MkdirAll(repo, 0o750); err != nil {
		t.Fatal(err)
	}
	mustGit(t, repo, "init", "-q", "-b", "main")
	writeFile(t, filepath.Join(repo, "app.py"), "from-git\n", time.Time{})
	writeFile(t, filepath.Join(repo, "only_in_git.txt"), "git-only\n", time.Time{})
	writeFile(t, filepath.Join(repo, "sub", "deep.txt"), "deep\n", time.Time{})
	writeFile(t, filepath.Join(repo, ".env"), "SECRET=checkout\n", time.Time{})
	mustGit(t, repo, "add", "-A")
	mustGit(t, repo, "commit", "-q", "-m", "init")
	return repo
}

func TestGitSyncOffUsesWorkspaceAndKeepsConfig(t *testing.T) {
	p, appID := gitSyncTestPanel(t)
	ctx := context.Background()
	repo := makeCheckout(t, p, appID)
	ws := p.Store.Path(appID)

	if !p.IsGitApp(ctx, appID) {
		t.Fatal("app should start as a Git app")
	}
	if got := p.ComposeWorkspaceRoot(ctx, appID); got != repo {
		t.Fatalf("compose root while Git is on = %s, want checkout %s", got, repo)
	}

	// Workspace already holds a pushed file that is newer than the checkout, plus an older copy and a workspace-only file.
	now := time.Now()
	writeFile(t, filepath.Join(ws, "app.py"), "pushed-newer\n", now.Add(time.Hour))
	writeFile(t, filepath.Join(ws, "sub", "deep.txt"), "stale\n", now.Add(-48*time.Hour))
	writeFile(t, filepath.Join(ws, "workspace_only.txt"), "keep me\n", now)

	res, err := p.SetGitSync(ctx, appID, false, false)
	if err != nil {
		t.Fatalf("SetGitSync(off): %v", err)
	}
	if !res.Changed || !res.Status.Paused || res.Status.Active {
		t.Fatalf("unexpected status after off: %+v", res)
	}
	if got := readFile(t, filepath.Join(ws, "app.py")); got != "pushed-newer\n" {
		t.Errorf("newer workspace file must win, got %q", got)
	}
	if got := readFile(t, filepath.Join(ws, "only_in_git.txt")); got != "git-only\n" {
		t.Errorf("git-only file must be carried over, got %q", got)
	}
	if got := readFile(t, filepath.Join(ws, "sub", "deep.txt")); got != "deep\n" {
		t.Errorf("older workspace file should be refreshed from checkout, got %q", got)
	}
	if got := readFile(t, filepath.Join(ws, "workspace_only.txt")); got != "keep me\n" {
		t.Errorf("workspace-only file must be kept, got %q", got)
	}
	if _, err := os.Stat(filepath.Join(ws, ".git")); err == nil {
		t.Error(".git must not be copied into the workspace")
	}
	if _, err := os.Stat(filepath.Join(ws, ".env")); err == nil {
		t.Error("checkout .env must not be copied into the workspace")
	}

	if p.IsGitApp(ctx, appID) {
		t.Error("IsGitApp must be false while Git sync is off")
	}
	if got := p.ComposeWorkspaceRoot(ctx, appID); got != ws {
		t.Errorf("compose root while Git is off = %s, want workspace %s", got, ws)
	}
	cfg, err := p.DB.GetAppGitConfig(ctx, appID)
	if err != nil || cfg.RepoURL == "" {
		t.Fatalf("repo config must be kept while paused: %v %+v", err, cfg)
	}
	if _, err := p.SyncGitAppSource(ctx, appID); !errors.Is(err, ErrGitSyncPaused) {
		t.Errorf("SyncGitAppSource while paused = %v, want ErrGitSyncPaused", err)
	}

	// Saving the repo settings must not silently turn sync back on.
	cfg.Branch = "develop"
	if err := p.DB.UpsertAppGitConfig(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	if again, _ := p.DB.GetAppGitConfig(ctx, appID); !again.SyncPaused || again.Branch != "develop" {
		t.Errorf("upsert must keep sync_paused and apply edits, got %+v", again)
	}

	// Idempotent.
	res, err = p.SetGitSync(ctx, appID, false, false)
	if err != nil || res.Changed {
		t.Errorf("second off should be a no-op, got changed=%v err=%v", res.Changed, err)
	}
}

func TestGitSyncOnRefusesDirtyCheckoutWithoutForce(t *testing.T) {
	p, appID := gitSyncTestPanel(t)
	ctx := context.Background()
	repo := makeCheckout(t, p, appID)

	if _, err := p.SetGitSync(ctx, appID, false, false); err != nil {
		t.Fatalf("off: %v", err)
	}
	writeFile(t, filepath.Join(repo, "app.py"), "edited in checkout\n", time.Time{})

	if _, err := p.SetGitSync(ctx, appID, true, false); err == nil {
		t.Fatal("turning Git sync on over a dirty checkout must be refused without force")
	}
	if p.IsGitApp(ctx, appID) {
		t.Fatal("refused turn-on must leave Git sync off")
	}

	res, err := p.SetGitSync(ctx, appID, true, true)
	if err != nil {
		t.Fatalf("on with force: %v", err)
	}
	if !res.Changed || !res.Status.Active || !p.IsGitApp(ctx, appID) {
		t.Fatalf("expected Git sync on, got %+v", res)
	}
	if p.DB.GetAppSourceType(ctx, appID) != "git" {
		t.Error("source type should be git again")
	}
}

func TestGitSyncOffWithoutCheckoutOnlyFlipsFlag(t *testing.T) {
	p, appID := gitSyncTestPanel(t)
	ctx := context.Background()
	res, err := p.SetGitSync(ctx, appID, false, false)
	if err != nil {
		t.Fatalf("off without checkout: %v", err)
	}
	if !res.Changed || res.Copied != 0 || p.IsGitApp(ctx, appID) {
		t.Fatalf("unexpected result %+v", res)
	}
}

func TestGitSyncRequiresConfig(t *testing.T) {
	p, appID := gitSyncTestPanel(t)
	ctx := context.Background()
	if err := p.DB.DeleteAppGitConfig(ctx, appID); err != nil {
		t.Fatal(err)
	}
	if _, err := p.SetGitSync(ctx, appID, false, false); err == nil {
		t.Fatal("expected an error when no repository is configured")
	}
}
