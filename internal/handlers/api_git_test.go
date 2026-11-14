package handlers

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"panel/internal/db"

	"github.com/gofiber/fiber/v2"
)

func TestProviderGitHubWebhookMatchesAppWithoutFKError(t *testing.T) {
	p, appID := gitSyncTestPanel(t)
	ctx := context.Background()

	const secret = "whsec-test-123"
	if err := p.DB.UpsertAppGitConfig(ctx, db.AppGitConfig{
		AppID:         appID,
		Provider:      "github",
		RepoURL:       "https://github.com/acme/demo",
		RepoFullName:  "acme/demo",
		Branch:        "main",
		AuthMode:      "github_app",
		WebhookSecret: secret,
		AutoDeploy:    false,
	}); err != nil {
		t.Fatalf("UpsertAppGitConfig: %v", err)
	}

	payload := []byte(`{"ref":"refs/heads/main","after":"abc1234","repository":{"full_name":"acme/demo","clone_url":"https://github.com/acme/demo.git"}}`)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	app := fiber.New()
	app.Post("/webhooks/github/provider", p.ProviderGitHubWebhook)

	req := httptest.NewRequest("POST", "/webhooks/github/provider", bytes.NewReader(payload))
	req.Header.Set("X-GitHub-Event", "push")
	req.Header.Set("X-GitHub-Delivery", "deliv-1")
	req.Header.Set("X-Hub-Signature-256", sig)

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != 200 && resp.StatusCode != 202 {
		t.Fatalf("expected 200/202, got %d", resp.StatusCode)
	}

	deliveries, err := p.DB.ListWebhookDeliveries(ctx, 10)
	if err != nil {
		t.Fatalf("ListWebhookDeliveries: %v", err)
	}
	if len(deliveries) != 1 || deliveries[0].AppID != appID {
		t.Fatalf("expected 1 delivery for %s, got %+v", appID, deliveries)
	}
}

func TestGitHubWebhookPingEventRecordsDelivery(t *testing.T) {
	p, appID := gitSyncTestPanel(t)
	ctx := context.Background()

	const secret = "whsec-ping-test-123"
	if err := p.DB.UpsertAppGitConfig(ctx, db.AppGitConfig{
		AppID:         appID,
		Provider:      "github",
		RepoURL:       "https://github.com/acme/demo",
		RepoFullName:  "acme/demo",
		Branch:        "main",
		AuthMode:      "public",
		WebhookSecret: secret,
		AutoDeploy:    true,
	}); err != nil {
		t.Fatalf("UpsertAppGitConfig: %v", err)
	}

	payload := []byte(`{"zen":"Responsive is better than fast.","hook_id":12345,"repository":{"full_name":"acme/demo"}}`)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	app := fiber.New()
	app.Post("/webhooks/github/:id", p.GitHubWebhook)

	req := httptest.NewRequest("POST", "/webhooks/github/"+appID, bytes.NewReader(payload))
	req.Header.Set("X-GitHub-Event", "ping")
	req.Header.Set("X-GitHub-Delivery", "ping-deliv-1")
	req.Header.Set("X-Hub-Signature-256", sig)

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != 200 && resp.StatusCode != 202 {
		t.Fatalf("expected 200/202, got %d", resp.StatusCode)
	}

	deliveries, err := p.DB.ListWebhookDeliveries(ctx, 10)
	if err != nil {
		t.Fatalf("ListWebhookDeliveries: %v", err)
	}
	if len(deliveries) != 1 || deliveries[0].AppID != appID || deliveries[0].Event != "ping" {
		t.Fatalf("expected 1 ping delivery for %s, got %+v", appID, deliveries)
	}
}

func TestAPIGitPushWordPressCreatesGitignoreAndPushesToBareRemote(t *testing.T) {
	p, _ := gitSyncTestPanel(t)
	ctx := context.Background()

	u, err := p.DB.GetUserByUsername(ctx, "owner")
	if err != nil {
		t.Fatalf("GetUserByUsername: %v", err)
	}

	const wpID = "wp-site-1"
	if err := p.DB.CreateAppAtomic(ctx, wpID, "wp-site", u.ID, 0, &db.PHPSite{
		AppID:      wpID,
		OwnerID:    u.ID,
		SiteType:   "wordpress",
		PHPVersion: "8.3",
		RootDir:    filepath.Join(p.WorkspacesRoot, wpID, "htdocs"),
	}); err != nil {
		t.Fatalf("CreateAppAtomic: %v", err)
	}

	htdocs := filepath.Join(p.WorkspacesRoot, wpID, "htdocs")
	if err := os.MkdirAll(htdocs, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(htdocs, "index.php"), []byte("<?php echo 'hi';\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(htdocs, "wp-config.php"), []byte("<?php define('DB_PASSWORD','secret');\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(htdocs, ".env"), []byte("SECRET=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	bareDir := filepath.Join(t.TempDir(), "remote.git")
	if out, err := exec.Command("git", "init", "--bare", bareDir).CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v\n%s", err, out)
	}
	remoteURL := "file://" + filepath.ToSlash(bareDir)
	if !strings.HasPrefix(remoteURL, "file:///") && strings.Contains(remoteURL, ":/") {
		remoteURL = "file:///" + strings.TrimPrefix(remoteURL, "file://")
	}

	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals(contextUserKey, u)
		return c.Next()
	})
	app.Post("/api/v1/apps/:id/git/push", p.APIGitPush)

	body, _ := json.Marshal(map[string]any{
		"repo_url":  remoteURL,
		"branch":    "main",
		"auth_mode": "public",
		"message":   "push wp",
	})
	req := httptest.NewRequest("POST", "/api/v1/apps/"+wpID+"/git/push", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != 200 {
		var out map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&out)
		t.Fatalf("expected 200, got %d: %+v", resp.StatusCode, out)
	}

	if _, err := os.Stat(filepath.Join(htdocs, ".gitignore")); err != nil {
		t.Fatalf("expected .gitignore in htdocs: %v", err)
	}

	lsOut, err := exec.Command("git", "--git-dir", bareDir, "ls-tree", "-r", "--name-only", "main").CombinedOutput()
	if err != nil {
		t.Fatalf("git ls-tree: %v\n%s", err, lsOut)
	}
	files := string(lsOut)
	if !strings.Contains(files, "index.php") || !strings.Contains(files, ".gitignore") {
		t.Fatalf("expected index.php and .gitignore in remote, got:\n%s", files)
	}
	if strings.Contains(files, "wp-config.php") || strings.Contains(files, ".env") {
		t.Fatalf("secrets should be excluded by .gitignore, got:\n%s", files)
	}
}

func TestNormalizeRepoURLShorthandAndGitLab(t *testing.T) {
	if got := NormalizeRepoURL("acme/demo"); got != "https://github.com/acme/demo.git" {
		t.Fatalf("NormalizeRepoURL(acme/demo) = %q", got)
	}
	if got := NormalizeRepoURL("gitlab.com/acme/demo"); got != "https://gitlab.com/acme/demo.git" {
		t.Fatalf("NormalizeRepoURL(gitlab.com/acme/demo) = %q", got)
	}
	if got := NormalizeRepoURL("git@gitlab.com:acme/demo.git"); got != "https://gitlab.com/acme/demo.git" {
		t.Fatalf("NormalizeRepoURL(git@gitlab.com:acme/demo.git) = %q", got)
	}
}

func TestGitConfigSaveWordPressPreservesHtdocs(t *testing.T) {
	p, _ := gitSyncTestPanel(t)
	ctx := context.Background()

	u, err := p.DB.GetUserByUsername(ctx, "owner")
	if err != nil {
		t.Fatalf("GetUserByUsername: %v", err)
	}

	const wpID = "wp-save-1"
	htdocs := filepath.Join(p.WorkspacesRoot, wpID, "htdocs")
	if err := p.DB.CreateAppAtomic(ctx, wpID, "wp-save", u.ID, 0, &db.PHPSite{
		AppID:      wpID,
		OwnerID:    u.ID,
		SiteType:   "wordpress",
		PHPVersion: "8.3",
		RootDir:    htdocs,
	}); err != nil {
		t.Fatalf("CreateAppAtomic: %v", err)
	}
	if err := os.MkdirAll(htdocs, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(htdocs, "index.php"), []byte("<?php echo 'keep';\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(htdocs, "wp-config.php"), []byte("<?php define('DB_NAME','wp');\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	form := url.Values{
		"auth_mode": {"public"},
		"repo_url":  {"acme/demo"},
		"branch":    {"main"},
	}
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals(contextUserKey, u)
		return c.Next()
	})
	app.Post("/apps/:id/git", p.GitConfigSave)

	req := httptest.NewRequest("POST", "/apps/"+wpID+"/git", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != 302 && resp.StatusCode != 303 && resp.StatusCode != 200 {
		t.Fatalf("expected 302/303/200, got %d", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); strings.Contains(loc, "error=") {
		t.Fatalf("unexpected error redirect: %s", loc)
	}

	if _, err := os.Stat(filepath.Join(htdocs, "index.php")); err != nil {
		t.Fatalf("expected index.php to be preserved in htdocs: %v", err)
	}
	if _, err := os.Stat(filepath.Join(htdocs, "wp-config.php")); err != nil {
		t.Fatalf("expected wp-config.php to be preserved in htdocs: %v", err)
	}
}

func TestSyncGitAppSourceWordPressPreservesConfig(t *testing.T) {
	p, _ := gitSyncTestPanel(t)
	ctx := context.Background()

	u, err := p.DB.GetUserByUsername(ctx, "owner")
	if err != nil {
		t.Fatalf("GetUserByUsername: %v", err)
	}

	const wpID = "wp-sync-1"
	htdocs := filepath.Join(p.WorkspacesRoot, wpID, "htdocs")
	if err := p.DB.CreateAppAtomic(ctx, wpID, "wp-sync", u.ID, 0, &db.PHPSite{
		AppID:      wpID,
		OwnerID:    u.ID,
		SiteType:   "wordpress",
		PHPVersion: "8.3",
		RootDir:    htdocs,
	}); err != nil {
		t.Fatalf("CreateAppAtomic: %v", err)
	}
	if err := os.MkdirAll(htdocs, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(htdocs, "wp-config.php"), []byte("existing-prod-config"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(htdocs, ".env"), []byte("APP_ENV=prod\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tmp := t.TempDir()
	bareDir := filepath.Join(tmp, "remote.git")
	if out, err := exec.Command("git", "init", "--bare", bareDir).CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v\n%s", err, out)
	}
	seedDir := filepath.Join(tmp, "seed")
	if out, err := exec.Command("git", "init", "-b", "main", seedDir).CombinedOutput(); err != nil {
		t.Fatalf("git init seed: %v\n%s", err, out)
	}
	_ = exec.Command("git", "-C", seedDir, "config", "user.name", "Test").Run()
	_ = exec.Command("git", "-C", seedDir, "config", "user.email", "test@example.com").Run()
	if err := os.WriteFile(filepath.Join(seedDir, "index.php"), []byte("<?php echo 'synced';\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(seedDir, "wp-config.php"), []byte("repo-template-config"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(seedDir, ".env"), []byte("APP_ENV=dev\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", seedDir, "add", ".").CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
	if out, err := exec.Command("git", "-C", seedDir, "commit", "-m", "init").CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v\n%s", err, out)
	}
	remoteURL := "file://" + filepath.ToSlash(bareDir)
	if !strings.HasPrefix(remoteURL, "file:///") && strings.Contains(remoteURL, ":/") {
		remoteURL = "file:///" + strings.TrimPrefix(remoteURL, "file://")
	}
	if out, err := exec.Command("git", "-C", seedDir, "push", remoteURL, "HEAD:refs/heads/main").CombinedOutput(); err != nil {
		t.Fatalf("git push seed: %v\n%s", err, out)
	}

	if err := p.DB.UpsertAppGitConfig(ctx, db.AppGitConfig{
		AppID:        wpID,
		Provider:     "github",
		RepoURL:      remoteURL,
		RepoFullName: "acme/wp-sync",
		Branch:       "main",
		AuthMode:     "public",
	}); err != nil {
		t.Fatalf("UpsertAppGitConfig: %v", err)
	}

	if _, err := p.SyncGitAppSource(ctx, wpID); err != nil {
		t.Fatalf("SyncGitAppSource: %v", err)
	}

	idx, err := os.ReadFile(filepath.Join(htdocs, "index.php"))
	if err != nil || !strings.Contains(string(idx), "synced") {
		t.Fatalf("expected synced index.php in htdocs, got %q (err=%v)", string(idx), err)
	}
	wpc, err := os.ReadFile(filepath.Join(htdocs, "wp-config.php"))
	if err != nil || string(wpc) != "existing-prod-config" {
		t.Fatalf("expected existing wp-config.php preserved, got %q (err=%v)", string(wpc), err)
	}
	envBytes, err := os.ReadFile(filepath.Join(htdocs, ".env"))
	if err != nil || string(envBytes) != "APP_ENV=prod\n" {
		t.Fatalf("expected existing .env preserved, got %q (err=%v)", string(envBytes), err)
	}
}

func TestAPIWebhookDeliveriesLast100AndRepoFallback(t *testing.T) {
	p, appID := gitSyncTestPanel(t)
	ctx := context.Background()

	u, err := p.DB.GetUserByUsername(ctx, "owner")
	if err != nil {
		t.Fatalf("GetUserByUsername: %v", err)
	}

	if err := p.DB.UpsertAppGitConfig(ctx, db.AppGitConfig{
		AppID:        appID,
		Provider:     "github",
		RepoURL:      "https://github.com/acme/demo",
		RepoFullName: "acme/demo",
		Branch:       "main",
		AuthMode:     "github_app",
	}); err != nil {
		t.Fatalf("UpsertAppGitConfig: %v", err)
	}

	base := time.Now().UTC().Add(-2 * time.Hour)
	for i := 0; i < 105; i++ {
		repo := "acme/demo"
		if i == 104 {
			repo = "" // simulate MarkWebhookDelivery blank repo on newest delivery
		}
		if err := p.DB.RecordWebhookDelivery(ctx, db.WebhookDelivery{
			AppID:      appID,
			DeliveryID: fmt.Sprintf("deliv-%03d", i),
			Provider:   "GitHub",
			Repo:       repo,
			Event:      "push (main)",
			SHA:        "abc1234",
			Status:     "200 OK",
			Latency:    "12ms",
			CreatedAt:  base.Add(time.Duration(i) * time.Second).Format(time.RFC3339),
		}); err != nil {
			t.Fatalf("RecordWebhookDelivery(%d): %v", i, err)
		}
	}

	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals(contextUserKey, u)
		return c.Next()
	})
	app.Get("/api/v1/git/webhooks", p.APIWebhookDeliveries)

	req := httptest.NewRequest("GET", "/api/v1/git/webhooks?limit=100", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var got []db.WebhookDelivery
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 100 {
		t.Fatalf("expected 100 deliveries, got %d", len(got))
	}
	if got[0].DeliveryID != "deliv-104" {
		t.Fatalf("expected newest delivery deliv-104 first, got %s", got[0].DeliveryID)
	}
	if got[0].Repo != "acme/demo" {
		t.Fatalf("expected fallback repo acme/demo for blank row, got %q", got[0].Repo)
	}
}
