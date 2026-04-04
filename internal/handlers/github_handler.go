package handlers

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"panel/internal/db"
	"panel/internal/dockerx"
	"panel/internal/gitx"

	"github.com/gofiber/fiber/v2"
)

type githubPushPayload struct {
	Ref        string `json:"ref"`
	After      string `json:"after"`
	Repository struct {
		HTMLURL string `json:"html_url"`
		Private bool   `json:"private"`
		FullName string `json:"full_name"`
	} `json:"repository"`
}

func randomSecret() string {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("nd-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}

func normalizeBranch(branch string) string {
	branch = strings.TrimSpace(branch)
	branch = strings.TrimPrefix(branch, "refs/heads/")
	if branch == "" {
		return "main"
	}
	return branch
}

func normalizeRepoURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "git@github.com:") {
		raw = strings.TrimPrefix(raw, "git@github.com:")
		raw = strings.TrimSuffix(raw, ".git")
		return "https://github.com/" + raw + ".git"
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	u.User = nil
	if u.Scheme == "" {
		return raw
	}
	if !strings.HasSuffix(u.Path, ".git") {
		u.Path += ".git"
	}
	return u.String()
}

func repoFullNameFromURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	p := strings.Trim(strings.TrimSuffix(u.Path, ".git"), "/")
	if p == "" {
		return ""
	}
	return p
}

func (p *Panel) GitConfigSave(c *fiber.Ctx) error {
	appID := c.Params("id")
	if _, err := p.DB.GetApp(c.UserContext(), appID); err != nil {
		return c.Status(404).SendString("app not found")
	}
	authMode := strings.TrimSpace(c.FormValue("auth_mode"))
	switch authMode {
	case "public", "token", "github_app":
	default:
		authMode = "public"
	}
	repoURL := normalizeRepoURL(c.FormValue("repo_url"))
	if repoURL == "" {
		return c.Status(400).SendString("repo url required")
	}
	cfg := db.AppGitConfig{
		AppID:          appID,
		Provider:       strings.TrimSpace(c.FormValue("provider")),
		RepoURL:        repoURL,
		RepoFullName:   repoFullNameFromURL(repoURL),
		Branch:         normalizeBranch(c.FormValue("branch")),
		AuthMode:       authMode,
		Token:          strings.TrimSpace(c.FormValue("token")),
		AppGitID:       strings.TrimSpace(c.FormValue("github_app_id")),
		InstallationID: strings.TrimSpace(c.FormValue("installation_id")),
		PrivateKeyPEM:  strings.TrimSpace(c.FormValue("private_key_pem")),
		AutoDeploy:     c.FormValue("auto_deploy") == "on",
	}
	if cfg.Provider == "" {
		cfg.Provider = "github"
	}
	if pid := strings.TrimSpace(c.FormValue("git_provider_id")); pid != "" {
		if parsed, err := strconv.ParseInt(pid, 10, 64); err == nil && parsed > 0 {
			cfg.GitProviderID = parsed
		}
	}
	old, err := p.DB.GetAppGitConfig(c.UserContext(), appID)
	if strings.TrimSpace(cfg.Token) == "" && err == nil {
		cfg.Token = old.Token
	}
	if strings.TrimSpace(cfg.AppGitID) == "" && err == nil {
		cfg.AppGitID = old.AppGitID
	}
	if strings.TrimSpace(cfg.InstallationID) == "" && err == nil {
		cfg.InstallationID = old.InstallationID
	}
	if strings.TrimSpace(cfg.PrivateKeyPEM) == "" && err == nil {
		cfg.PrivateKeyPEM = old.PrivateKeyPEM
	}
	if err == nil && strings.TrimSpace(old.WebhookSecret) != "" {
		cfg.WebhookSecret = old.WebhookSecret
	} else {
		cfg.WebhookSecret = randomSecret()
	}
	if cfg.GitProviderID == 0 && err == nil {
		cfg.GitProviderID = old.GitProviderID
	}
	if cfg.AuthMode == "github_app" && cfg.GitProviderID == 0 {
		return c.Redirect(fmt.Sprintf("/apps/%s?tab=git&error=%s", appID, url.QueryEscape("Select a GitHub provider first")))
	}
	if cfg.AuthMode == "github_app" && cfg.GitProviderID > 0 {
		detail, derr := p.DB.GetGitHubProviderDetail(c.UserContext(), cfg.GitProviderID)
		if derr != nil {
			return c.Redirect(fmt.Sprintf("/apps/%s?tab=git&error=%s", appID, url.QueryEscape("Selected GitHub provider is not ready yet")))
		}
		cfg.Provider = "github"
		cfg.AppGitID = detail.GitHubAppID
		cfg.InstallationID = detail.InstallationID
		cfg.PrivateKeyPEM = detail.PrivateKeyPEM
		if strings.TrimSpace(detail.WebhookSecret) != "" {
			cfg.WebhookSecret = detail.WebhookSecret
		}
	}
	if err := p.DB.UpsertAppGitConfig(c.UserContext(), cfg); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	if cfg.AuthMode == "github_app" {
		if err := p.ensureRepoWebhook(c.UserContext(), c, appID, cfg); err != nil {
			return c.Redirect(fmt.Sprintf("/apps/%s?tab=git&error=%s", appID, url.QueryEscape(err.Error())))
		}
	}
	// Mark app as git-sourced
	_ = p.DB.SetAppSourceType(c.UserContext(), appID, "git")
	return c.Redirect(fmt.Sprintf("/apps/%s?tab=git&saved=1", appID))
}

func (p *Panel) GitConfigDelete(c *fiber.Ctx) error {
	appID := c.Params("id")
	if _, err := p.DB.GetApp(c.UserContext(), appID); err != nil {
		return c.Status(404).SendString("app not found")
	}
	if err := p.DB.DeleteAppGitConfig(c.UserContext(), appID); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	// Revert to files source
	_ = p.DB.SetAppSourceType(c.UserContext(), appID, "files")
	return c.Redirect(fmt.Sprintf("/apps/%s?tab=overview", appID))
}

func (p *Panel) appCheckoutPath(appID string) string {
	return filepath.Join(p.Store.ReservedPath(appID), "repo")
}

func (p *Panel) ensureGitWorkspace(appID string) error {
	return os.MkdirAll(p.Store.ReservedPath(appID), 0o750)
}

func (p *Panel) syncGitAppSource(ctx context.Context, appID string) (string, error) {
	cfg, err := p.DB.GetAppGitConfig(ctx, appID)
	if err != nil {
		return "", err
	}
	if err := p.ensureGitWorkspace(appID); err != nil {
		return "", err
	}
	repoDir := p.appCheckoutPath(appID)
	token, err := p.resolveGitAuthToken(ctx, cfg)
	if err != nil {
		return "", err
	}
	var res gitx.Result
	if gitx.RepoExists(repoDir) {
		res = gitx.Pull(ctx, repoDir, cfg.Branch, cfg.AuthMode, token)
	} else {
		_ = os.RemoveAll(repoDir)
		res = gitx.Clone(ctx, repoDir, cfg.RepoURL, cfg.Branch, cfg.AuthMode, token)
	}
	if !res.OK {
		return res.Output, errors.New(res.Output)
	}
	commit := gitx.CurrentCommit(ctx, repoDir)
	cfg.LastDeployRef = commit
	_ = p.DB.UpsertAppGitConfig(ctx, cfg)
	return res.Output, nil
}

func (p *Panel) resolveGitAuthToken(ctx context.Context, cfg db.AppGitConfig) (string, error) {
	if cfg.AuthMode == "public" {
		return "", nil
	}
	if cfg.AuthMode == "github_app" {
		if cfg.GitProviderID > 0 {
			detail, err := p.DB.GetGitHubProviderDetail(ctx, cfg.GitProviderID)
			if err != nil {
				return "", err
			}
			if strings.TrimSpace(detail.InstallationID) != "" {
				cfg.InstallationID = detail.InstallationID
			}
			if strings.TrimSpace(detail.GitHubAppID) != "" {
				cfg.AppGitID = detail.GitHubAppID
			}
			if strings.TrimSpace(detail.PrivateKeyPEM) != "" {
				cfg.PrivateKeyPEM = detail.PrivateKeyPEM
			}
		}
		return gitx.MintGitHubInstallationToken(ctx, cfg.AppGitID, cfg.InstallationID, cfg.PrivateKeyPEM)
	}
	if cfg.GitProviderID > 0 && strings.TrimSpace(cfg.Token) == "" {
		provider, err := p.DB.GetGitProvider(ctx, cfg.GitProviderID)
		if err == nil {
			return strings.TrimSpace(provider.Token), nil
		}
	}
	return strings.TrimSpace(cfg.Token), nil
}

type githubRepoHook struct {
	ID     int64 `json:"id"`
	Config struct {
		URL string `json:"url"`
	} `json:"config"`
}

func githubAPIRequest(ctx context.Context, method, rawURL, token string, body any) ([]byte, int, error) {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, 0, err
		}
		reader = strings.NewReader(string(b))
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, reader)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return data, resp.StatusCode, nil
}

func (p *Panel) ensureRepoWebhook(ctx context.Context, c *fiber.Ctx, appID string, cfg db.AppGitConfig) error {
	if cfg.Provider != "github" || cfg.AuthMode != "github_app" || cfg.RepoFullName == "" {
		return nil
	}
	token, err := p.resolveGitAuthToken(ctx, cfg)
	if err != nil {
		return err
	}
	parts := strings.SplitN(cfg.RepoFullName, "/", 2)
	if len(parts) != 2 {
		return nil
	}
	hookURL := p.appWebhookURL(c, appID)
	listURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/hooks", parts[0], parts[1])
	body, status, err := githubAPIRequest(ctx, http.MethodGet, listURL, token, nil)
	if err != nil {
		return err
	}
	if status >= 300 {
		return fmt.Errorf("github webhook list failed: HTTP %d: %s", status, strings.TrimSpace(string(body)))
	}
	var hooks []githubRepoHook
	if err := json.Unmarshal(body, &hooks); err != nil {
		return err
	}
	payload := map[string]any{
		"active": true,
		"events": []string{"push"},
		"config": map[string]any{
			"url":          hookURL,
			"content_type": "json",
			"secret":       cfg.WebhookSecret,
			"insecure_ssl": "0",
		},
	}
	for _, hook := range hooks {
		if strings.EqualFold(strings.TrimSpace(hook.Config.URL), hookURL) {
			updateURL := fmt.Sprintf("%s/%d", listURL, hook.ID)
			resBody, resStatus, err := githubAPIRequest(ctx, http.MethodPatch, updateURL, token, payload)
			if err != nil {
				return err
			}
			if resStatus >= 300 {
				return fmt.Errorf("github webhook update failed: HTTP %d: %s", resStatus, strings.TrimSpace(string(resBody)))
			}
			return nil
		}
	}
	resBody, resStatus, err := githubAPIRequest(ctx, http.MethodPost, listURL, token, payload)
	if err != nil {
		return err
	}
	if resStatus >= 300 {
		return fmt.Errorf("github webhook create failed: HTTP %d: %s", resStatus, strings.TrimSpace(string(resBody)))
	}
	return nil
}

func (p *Panel) GitSync(c *fiber.Ctx) error {
	appID := c.Params("id")
	if _, err := p.DB.GetApp(c.UserContext(), appID); err != nil {
		return c.Status(404).SendString("app not found")
	}
	ctx, cancel := context.WithTimeout(c.UserContext(), 15*time.Minute)
	defer cancel()
	if _, err := p.syncGitAppSource(ctx, appID); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	return c.Redirect(fmt.Sprintf("/apps/%s?tab=git&synced=1", appID))
}

func verifyGitHubSignature(secret string, body []byte, got string) bool {
	if secret == "" || got == "" || !strings.HasPrefix(got, "sha256=") {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(got))
}

func (p *Panel) GitHubWebhook(c *fiber.Ctx) error {
	appID := c.Params("id")
	app, err := p.DB.GetApp(c.UserContext(), appID)
	if err != nil {
		return c.SendStatus(fiber.StatusNotFound)
	}
	cfg, err := p.DB.GetAppGitConfig(c.UserContext(), appID)
	if err != nil {
		return c.SendStatus(fiber.StatusNotFound)
	}
	body := c.Body()
	if !verifyGitHubSignature(cfg.WebhookSecret, body, c.Get("X-Hub-Signature-256")) {
		return c.SendStatus(fiber.StatusUnauthorized)
	}
	deliveryID := strings.TrimSpace(c.Get("X-GitHub-Delivery"))
	if ok, err := p.DB.MarkWebhookDelivery(c.UserContext(), appID, deliveryID); err != nil || !ok {
		return c.SendStatus(fiber.StatusOK)
	}
	if c.Get("X-GitHub-Event") != "push" {
		return c.SendStatus(fiber.StatusOK)
	}
	var payload githubPushPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return c.SendStatus(fiber.StatusBadRequest)
	}
	if cfg.RepoFullName != "" && payload.Repository.FullName != "" && !strings.EqualFold(cfg.RepoFullName, payload.Repository.FullName) {
		return c.SendStatus(fiber.StatusAccepted)
	}
	if normalizeBranch(payload.Ref) != normalizeBranch(cfg.Branch) {
		return c.SendStatus(fiber.StatusAccepted)
	}
	if !cfg.AutoDeploy {
		return c.SendStatus(fiber.StatusAccepted)
	}
	go func() {
		bg := context.Background()
		if _, err := p.syncGitAppSource(bg, appID); err != nil {
			_ = p.DB.InsertDeployLog(bg, appID, "Webhook sync", false, err.Error())
			return
		}
		if err := p.syncAppCaddyOverride(c, appID); err != nil {
			_ = p.DB.InsertDeployLog(bg, appID, "Webhook deploy", false, err.Error())
			return
		}
		project := p.composeProjectName(app, appID)
		_ = p.startComposeJob(appID, project, p.effectiveComposePaths(bg, app, appID), "Webhook redeploy", dockerx.ComposePullUp)
	}()
	return c.SendStatus(fiber.StatusOK)
}

