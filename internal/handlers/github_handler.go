package handlers

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
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
	"unicode/utf16"
	"unicode/utf8"

	"panel/internal/db"
	"panel/internal/dockerx"
	"panel/internal/gitx"

	"github.com/gofiber/fiber/v2"
)

type githubPushPayload struct {
	Ref        string `json:"ref"`
	After      string `json:"after"`
	Repository struct {
		HTMLURL  string `json:"html_url"`
		Private  bool   `json:"private"`
		FullName string `json:"full_name"`
	} `json:"repository"`
}

type gitlabPushPayload struct {
	ObjectKind string `json:"object_kind"`
	Ref        string `json:"ref"`
	Project    struct {
		PathWithNamespace string `json:"path_with_namespace"`
	} `json:"project"`
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
		return respondAppNotFound(c)
	}
	authMode := strings.TrimSpace(c.FormValue("auth_mode"))
	switch authMode {
	case "public", "github_app", "gitlab_token":
	default:
		authMode = "public"
	}
	repoURL := normalizeRepoURL(c.FormValue("repo_url"))
	if repoURL == "" {
		return c.Status(400).SendString("repo url required")
	}
	// Determine base provider name from auth mode.
	providerName := "github"
	if authMode == "gitlab_token" {
		providerName = "gitlab"
	}
	cfg := db.AppGitConfig{
		AppID:          appID,
		Provider:       providerName,
		RepoURL:        repoURL,
		RepoFullName:   repoFullNameFromURL(repoURL),
		Branch:         normalizeBranch(c.FormValue("branch")),
		AuthMode:       authMode,
		Token:          "",
		AppGitID:       "",
		InstallationID: "",
		PrivateKeyPEM:  "",
		AutoDeploy:     c.FormValue("auto_deploy") == "on",
	}
	if pid := strings.TrimSpace(c.FormValue("git_provider_id")); pid != "" {
		if parsed, err := strconv.ParseInt(pid, 10, 64); err == nil && parsed > 0 {
			cfg.GitProviderID = parsed
		}
	}
	old, oldCfgErr := p.DB.GetAppGitConfig(c.UserContext(), appID)
	if oldCfgErr == nil && strings.TrimSpace(old.WebhookSecret) != "" {
		cfg.WebhookSecret = old.WebhookSecret
	} else {
		cfg.WebhookSecret = randomSecret()
	}
	if cfg.AuthMode == "github_app" && cfg.GitProviderID > 0 {
		detail, derr := p.DB.GetGitHubProviderDetail(c.UserContext(), cfg.GitProviderID)
		if derr != nil {
			p.setGitTabErrorCookie(c, appID, "Selected GitHub provider is not ready yet")
			return c.Redirect(fmt.Sprintf("/apps/%s?tab=git", appID))
		}
		cfg.Provider = "github"
		cfg.AppGitID = detail.GitHubAppID
		cfg.InstallationID = detail.InstallationID
		cfg.PrivateKeyPEM = detail.PrivateKeyPEM
		if strings.TrimSpace(detail.WebhookSecret) != "" {
			cfg.WebhookSecret = detail.WebhookSecret
		}
	}
	if cfg.AuthMode == "gitlab_token" && cfg.GitProviderID > 0 {
		glProvider, gerr := p.DB.GetGitProvider(c.UserContext(), cfg.GitProviderID)
		if gerr != nil || strings.TrimSpace(glProvider.Token) == "" {
			p.setGitTabErrorCookie(c, appID, "Selected GitLab provider has no token — reconnect from Git Providers page")
			return c.Redirect(fmt.Sprintf("/apps/%s?tab=git", appID))
		}
		cfg.Provider = "gitlab"
	}
	if err := p.DB.UpsertAppGitConfig(c.UserContext(), cfg); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	// First-time Git setup: remove file-upload workspace so the clone is the single source of truth.
	if oldCfgErr != nil {
		if err := p.Store.ClearUploadedProjectForGitSource(appID); err != nil {
			return c.Status(500).SendString(err.Error())
		}
	}
	if cfg.AuthMode == "github_app" && cfg.AutoDeploy {
		if err := p.ensureRepoWebhook(c.UserContext(), c, appID, cfg); err != nil {
			var apiErr *githubWebhookAPIError
			if errors.As(err, &apiErr) && apiErr.IsPermissionDenied() {
				cfg.AutoDeploy = false
				_ = p.DB.UpsertAppGitConfig(c.UserContext(), cfg)
				p.setGitTabErrorCookie(c, appID, friendlyGitHubWebhookSetupError(err))
			} else {
				p.setGitTabErrorCookie(c, appID, err.Error())
				return c.Redirect(fmt.Sprintf("/apps/%s?tab=git", appID))
			}
		}
	}
	// Mark app as git-sourced
	_ = p.DB.SetAppSourceType(c.UserContext(), appID, "git")

	// If the repository URL changed, drop the old checkout so the next sync clones the new remote.
	if oldCfgErr == nil && strings.TrimSpace(old.RepoURL) != "" &&
		normalizeRepoURL(old.RepoURL) != normalizeRepoURL(cfg.RepoURL) {
		_ = os.RemoveAll(p.appCheckoutPath(appID))
	}

	// Best practice: persist config then immediately materialize workspace (clone/fetch) so branch/URL changes apply.
	ctx, cancel := context.WithTimeout(c.UserContext(), 15*time.Minute)
	defer cancel()
	if _, err := p.syncGitAppSource(ctx, appID); err != nil {
		p.setGitTabErrorCookie(c, appID, "Configuration saved, but repository sync failed: "+err.Error())
		return c.Redirect(fmt.Sprintf("/apps/%s?tab=git", appID))
	}
	p.setGitTabFlashCookie(c, appID, "saved_synced")
	return c.Redirect(fmt.Sprintf("/apps/%s?tab=git", appID))
}

func (p *Panel) GitConfigDelete(c *fiber.Ctx) error {
	appID := c.Params("id")
	if _, err := p.DB.GetApp(c.UserContext(), appID); err != nil {
		return respondAppNotFound(c)
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
	if cfg.AuthMode == "gitlab_token" && cfg.GitProviderID > 0 {
		provider, err := p.DB.GetGitProvider(ctx, cfg.GitProviderID)
		if err != nil {
			return "", fmt.Errorf("GitLab provider not found: %w", err)
		}
		token := strings.TrimSpace(provider.Token)
		if token == "" {
			return "", fmt.Errorf("GitLab provider has no token — reconnect from Git Providers page")
		}
		return token, nil
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

type githubInstallationRepository struct {
	Name          string `json:"name"`
	FullName      string `json:"full_name"`
	CloneURL      string `json:"clone_url"`
	DefaultBranch string `json:"default_branch"`
	Private       bool   `json:"private"`
}

type githubInstallationRepositoriesResponse struct {
	Repositories []githubInstallationRepository `json:"repositories"`
}

type githubBranch struct {
	Name string `json:"name"`
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

type githubWebhookAPIError struct {
	Op     string
	Status int
	Body   string
}

func (e *githubWebhookAPIError) Error() string {
	return fmt.Sprintf("github webhook %s failed: HTTP %d: %s", e.Op, e.Status, strings.TrimSpace(e.Body))
}

func (e *githubWebhookAPIError) IsPermissionDenied() bool {
	if e == nil {
		return false
	}
	body := strings.ToLower(strings.TrimSpace(e.Body))
	return e.Status == http.StatusForbidden &&
		(strings.Contains(body, "resource not accessible by integration") || strings.Contains(body, "forbidden"))
}

func friendlyGitHubWebhookSetupError(err error) string {
	var apiErr *githubWebhookAPIError
	if !errors.As(err, &apiErr) || !apiErr.IsPermissionDenied() {
		return err.Error()
	}
	return "Configuration saved, but NextDeploy could not manage the repository webhook automatically. GitHub returned 403 \"Resource not accessible by integration\". This usually means the installed GitHub App does not have repository webhook/admin access for this repo. Auto deploy on push was disabled for now. Reinstall or update the provider with repository Administration write access, then save again."
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
		return &githubWebhookAPIError{Op: "list", Status: status, Body: string(body)}
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
				return &githubWebhookAPIError{Op: "update", Status: resStatus, Body: string(resBody)}
			}
			return nil
		}
	}
	resBody, resStatus, err := githubAPIRequest(ctx, http.MethodPost, listURL, token, payload)
	if err != nil {
		return err
	}
	if resStatus >= 300 {
		return &githubWebhookAPIError{Op: "create", Status: resStatus, Body: string(resBody)}
	}
	return nil
}

func (p *Panel) AppGitProviderRepos(c *fiber.Ctx) error {
	appID := c.Params("id")
	if _, err := p.DB.GetApp(c.UserContext(), appID); err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "app not found"})
	}
	pid, err := strconv.ParseInt(c.Params("pid"), 10, 64)
	if err != nil || pid <= 0 {
		return c.Status(400).JSON(fiber.Map{"error": "invalid provider"})
	}
	provider, err := p.DB.GetGitProvider(c.UserContext(), pid)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "provider not found"})
	}
	if provider.Provider == "gitlab" {
		return p.AppGitLabProviderRepos(c)
	}
	if provider.Provider != "github" {
		return c.Status(400).JSON(fiber.Map{"error": "repository picker is only available for GitHub App or GitLab providers"})
	}
	detail, err := p.DB.GetGitHubProviderDetail(c.UserContext(), pid)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "github provider details not found"})
	}
	if strings.TrimSpace(detail.InstallationID) == "" {
		return c.Status(400).JSON(fiber.Map{"error": "install the GitHub App first from the Git Providers page"})
	}
	token, err := gitx.MintGitHubInstallationToken(c.UserContext(), detail.GitHubAppID, detail.InstallationID, detail.PrivateKeyPEM)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	body, status, err := githubAPIRequest(c.UserContext(), http.MethodGet, "https://api.github.com/installation/repositories?per_page=100", token, nil)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	if status >= 300 {
		return c.Status(status).JSON(fiber.Map{"error": strings.TrimSpace(string(body))})
	}
	var payload githubInstallationRepositoriesResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"repos": payload.Repositories})
}

func (p *Panel) AppGitProviderBranches(c *fiber.Ctx) error {
	appID := c.Params("id")
	if _, err := p.DB.GetApp(c.UserContext(), appID); err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "app not found"})
	}
	pid, err := strconv.ParseInt(c.Params("pid"), 10, 64)
	if err != nil || pid <= 0 {
		return c.Status(400).JSON(fiber.Map{"error": "invalid provider"})
	}
	provider, err := p.DB.GetGitProvider(c.UserContext(), pid)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "provider not found"})
	}
	if provider.Provider == "gitlab" {
		return p.AppGitLabProviderBranches(c)
	}
	repoFullName := strings.TrimSpace(c.Query("repo"))
	parts := strings.SplitN(repoFullName, "/", 2)
	if len(parts) != 2 {
		return c.Status(400).JSON(fiber.Map{"error": "invalid repo"})
	}
	if provider.Provider != "github" {
		return c.Status(400).JSON(fiber.Map{"error": "branch picker is only available for GitHub App or GitLab providers"})
	}
	detail, err := p.DB.GetGitHubProviderDetail(c.UserContext(), pid)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "github provider details not found"})
	}
	token, err := gitx.MintGitHubInstallationToken(c.UserContext(), detail.GitHubAppID, detail.InstallationID, detail.PrivateKeyPEM)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	endpoint := fmt.Sprintf("https://api.github.com/repos/%s/%s/branches?per_page=100", parts[0], parts[1])
	body, status, err := githubAPIRequest(c.UserContext(), http.MethodGet, endpoint, token, nil)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	if status >= 300 {
		return c.Status(status).JSON(fiber.Map{"error": strings.TrimSpace(string(body))})
	}
	var branches []githubBranch
	if err := json.Unmarshal(body, &branches); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"branches": branches})
}

func (p *Panel) GitSync(c *fiber.Ctx) error {
	appID := c.Params("id")
	if _, err := p.DB.GetApp(c.UserContext(), appID); err != nil {
		return respondAppNotFound(c)
	}
	ctx, cancel := context.WithTimeout(c.UserContext(), 15*time.Minute)
	defer cancel()
	if _, err := p.syncGitAppSource(ctx, appID); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	p.setGitTabFlashCookie(c, appID, "synced")
	return c.Redirect(fmt.Sprintf("/apps/%s?tab=git", appID))
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

func verifyGitLabToken(secret, tokenHeader string) bool {
	s := strings.TrimSpace(secret)
	g := strings.TrimSpace(tokenHeader)
	if s == "" || g == "" {
		return false
	}
	if len(s) != len(g) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(s), []byte(g)) == 1
}

func webhookDeliveryID(c *fiber.Ctx, body []byte, githubMode bool) string {
	if githubMode {
		if id := strings.TrimSpace(c.Get("X-GitHub-Delivery")); id != "" {
			return id
		}
	} else {
		if id := strings.TrimSpace(c.Get("X-Gitlab-Event-UUID")); id != "" {
			return id
		}
	}
	sum := sha256.Sum256(body)
	return "anon-" + hex.EncodeToString(sum[:16])
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
	githubOK := verifyGitHubSignature(cfg.WebhookSecret, body, c.Get("X-Hub-Signature-256"))
	gitlabOK := verifyGitLabToken(cfg.WebhookSecret, c.Get("X-Gitlab-Token"))
	if !githubOK && !gitlabOK {
		return c.SendStatus(fiber.StatusUnauthorized)
	}
	githubMode := githubOK
	deliveryID := webhookDeliveryID(c, body, githubMode)
	if ok, err := p.DB.MarkWebhookDelivery(c.UserContext(), appID, deliveryID); err != nil || !ok {
		return c.SendStatus(fiber.StatusOK)
	}
	if githubMode {
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
	} else {
		if ev := strings.TrimSpace(c.Get("X-Gitlab-Event")); ev != "" && !strings.EqualFold(ev, "Push Hook") {
			return c.SendStatus(fiber.StatusOK)
		}
		var payload gitlabPushPayload
		if err := json.Unmarshal(body, &payload); err != nil {
			return c.SendStatus(fiber.StatusBadRequest)
		}
		if payload.ObjectKind != "" && payload.ObjectKind != "push" {
			return c.SendStatus(fiber.StatusOK)
		}
		if cfg.RepoFullName != "" && payload.Project.PathWithNamespace != "" &&
			!strings.EqualFold(cfg.RepoFullName, payload.Project.PathWithNamespace) {
			return c.SendStatus(fiber.StatusAccepted)
		}
		if normalizeBranch(payload.Ref) != normalizeBranch(cfg.Branch) {
			return c.SendStatus(fiber.StatusAccepted)
		}
	}
	if !cfg.AutoDeploy {
		return c.SendStatus(fiber.StatusAccepted)
	}
	go func() {
		bg := context.Background()
		gitOut, err := p.syncGitAppSource(bg, appID)
		if err != nil {
			_ = p.DB.InsertDeployLog(bg, appID, "Webhook sync", false, err.Error())
			return
		}
		gitPreamble := strings.TrimSpace(gitOut)
		if gitPreamble == "" {
			gitPreamble = "Repository sync completed."
		}
		if err := p.syncAppCaddyOverrideCtx(bg, appID); err != nil {
			_ = p.DB.InsertDeployLog(bg, appID, "Webhook deploy", false, err.Error())
			return
		}
		projCtx, projCancel := context.WithTimeout(bg, 90*time.Second)
		project := p.activeComposeProjectName(projCtx, app, appID)
		projCancel()
		stopCtx, stopCancel := context.WithTimeout(bg, 5*time.Minute)
		p.stopOtherComposeStacks(stopCtx, app, appID, project)
		stopCancel()
		_ = p.startComposeJob(appID, project, p.effectiveComposePaths(bg, app, appID), "Webhook redeploy", dockerx.ComposePullUp, gitPreamble)
	}()
	return c.SendStatus(fiber.StatusOK)
}

const (
	maxGitRepoBlobJSON     = 512 << 10 // JSON text preview in UI
	maxGitRepoBlobDownload = 32 << 20  // raw download limit
)

// decodeUTF16Bytes converts UTF-16 code units (no BOM) to a Go string.
func decodeUTF16Bytes(b []byte, bigEndian bool) string {
	if len(b) < 2 {
		return ""
	}
	if len(b)%2 == 1 {
		b = b[:len(b)-1]
	}
	n := len(b) / 2
	u := make([]uint16, n)
	for i := 0; i < n; i++ {
		if bigEndian {
			u[i] = uint16(b[2*i])<<8 | uint16(b[2*i+1])
		} else {
			u[i] = uint16(b[2*i]) | uint16(b[2*i+1])<<8
		}
	}
	return string(utf16.Decode(u))
}

func gitRepoBinaryMagic(b []byte) bool {
	if len(b) < 4 {
		return false
	}
	if bytes.HasPrefix(b, []byte("%PDF")) {
		return true
	}
	if len(b) >= 8 && bytes.HasPrefix(b, []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}) {
		return true
	}
	if len(b) >= 3 && b[0] == 0xFF && b[1] == 0xD8 && b[2] == 0xFF {
		return true
	}
	if len(b) >= 6 && (bytes.HasPrefix(b, []byte("GIF87a")) || bytes.HasPrefix(b, []byte("GIF89a"))) {
		return true
	}
	if bytes.HasPrefix(b, []byte("PK\x03\x04")) {
		return true
	}
	if len(b) >= 12 && bytes.HasPrefix(b, []byte{0x00, 0x00, 0x00, 0x18, 0x66, 0x74, 0x79, 0x70}) { // MP4
		return true
	}
	return false
}

func gitRepoLikelyTextDespiteInvalidUTF8(b []byte) bool {
	const maxSample = 256 * 1024
	n := len(b)
	if n > maxSample {
		n = maxSample
	}
	if n == 0 {
		return true
	}
	ok := 0
	for i := 0; i < n; i++ {
		c := b[i]
		switch {
		case c == 0x09 || c == 0x0A || c == 0x0D:
			ok++
		case c >= 0x20 && c <= 0x7E:
			ok++
		case c >= 0x80:
			ok++ // Latin-1 / partial UTF-8; common in mis-saved text files
		default:
			// control chars other than tab/newline reduce score slightly
		}
	}
	return float64(ok)/float64(n) >= 0.88
}

// gitRepoBlobPreviewText returns UTF-8 text for JSON preview, or binary=true for true binaries.
// Handles UTF-16 (BOM), UTF-8 BOM, and invalid UTF-8 that still looks like text (e.g. Latin-1 requirements.txt).
func gitRepoBlobPreviewText(b []byte) (text string, binary bool) {
	if len(b) == 0 {
		return "", false
	}
	// UTF-16 LE BOM
	if len(b) >= 2 && b[0] == 0xFF && b[1] == 0xFE {
		if len(b) == 2 {
			return "", false
		}
		return decodeUTF16Bytes(b[2:], false), false
	}
	// UTF-16 BE BOM
	if len(b) >= 2 && b[0] == 0xFE && b[1] == 0xFF {
		if len(b) == 2 {
			return "", false
		}
		return decodeUTF16Bytes(b[2:], true), false
	}
	trim := b
	if len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF {
		trim = b[3:]
	}
	if gitRepoBinaryMagic(trim) {
		return "", true
	}
	if bytes.IndexByte(trim, 0) >= 0 {
		return "", true
	}
	if utf8.Valid(trim) {
		return string(trim), false
	}
	if gitRepoLikelyTextDespiteInvalidUTF8(trim) {
		return strings.ToValidUTF8(string(trim), "\uFFFD"), false
	}
	return "", true
}

func (p *Panel) gitRepoBrowserGate(c *fiber.Ctx, appID string) int {
	if _, err := p.DB.GetApp(c.UserContext(), appID); err != nil {
		return fiber.StatusNotFound
	}
	if !p.isGitApp(c.UserContext(), appID) {
		return fiber.StatusBadRequest
	}
	if _, err := p.DB.GetAppGitConfig(c.UserContext(), appID); err != nil {
		return fiber.StatusNotFound
	}
	if !gitx.RepoExists(p.appCheckoutPath(appID)) {
		return fiber.StatusNotFound
	}
	return 0
}

// GitRepoTree returns directory listing JSON for the checked-out repository (read-only; .git hidden).
func (p *Panel) GitRepoTree(c *fiber.Ctx) error {
	appID := c.Params("id")
	if code := p.gitRepoBrowserGate(c, appID); code != 0 {
		msg := "not available"
		if code == fiber.StatusNotFound {
			msg = "repository not available; run Sync first"
		}
		return c.Status(code).JSON(fiber.Map{"error": msg})
	}
	rel := c.Query("path", "")
	children, err := p.Store.ListGitRepoChildren(appID, rel)
	if err != nil {
		if errors.Is(err, os.ErrInvalid) {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid path"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	type row struct {
		Name    string `json:"name"`
		RelPath string `json:"rel_path"`
		IsDir   bool   `json:"is_dir"`
		Size    int64  `json:"size"`
	}
	out := make([]row, 0, len(children))
	for _, ch := range children {
		out = append(out, row{Name: ch.Name, RelPath: ch.RelPath, IsDir: ch.IsDir, Size: ch.Size})
	}
	parent := p.Store.ParentRel(rel)
	return c.JSON(fiber.Map{
		"path":    rel,
		"parent":  parent,
		"entries": out,
	})
}

// GitRepoBlob returns JSON with file text for the UI preview, or metadata for binary/oversized files.
func (p *Panel) GitRepoBlob(c *fiber.Ctx) error {
	appID := c.Params("id")
	if code := p.gitRepoBrowserGate(c, appID); code != 0 {
		return c.Status(code).JSON(fiber.Map{"error": "repository not available"})
	}
	rel := c.Query("path", "")
	if strings.TrimSpace(rel) == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "path required"})
	}
	full, err := p.Store.SafeGitRepoFilePath(appID, rel)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid path"})
	}
	st, err := os.Stat(full)
	if err != nil {
		if os.IsNotExist(err) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "not found"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	if st.IsDir() {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "not a file"})
	}
	name := filepath.Base(rel)
	rawURL := fmt.Sprintf("/apps/%s/git/raw?path=%s", appID, url.QueryEscape(rel))
	if st.Size() > maxGitRepoBlobJSON {
		return c.JSON(fiber.Map{
			"path":       rel,
			"name":       name,
			"size":       st.Size(),
			"too_large":  true,
			"max_bytes":  maxGitRepoBlobJSON,
			"raw_url":    rawURL,
			"download_url": rawURL + "&download=1",
		})
	}
	f, err := os.Open(full)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	defer f.Close()
	b, err := io.ReadAll(f)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	preview, isBinary := gitRepoBlobPreviewText(b)
	if isBinary {
		return c.JSON(fiber.Map{
			"path":         rel,
			"name":         name,
			"size":         st.Size(),
			"binary":       true,
			"raw_url":      rawURL,
			"download_url": rawURL + "&download=1",
		})
	}
	return c.JSON(fiber.Map{
		"path":         rel,
		"name":         name,
		"size":         st.Size(),
		"text":         preview,
		"truncated":    false,
		"binary":       false,
		"raw_url":      rawURL,
		"download_url": rawURL + "&download=1",
	})
}

// GitRepoRaw serves a single file from the git checkout (inline or attachment).
func (p *Panel) GitRepoRaw(c *fiber.Ctx) error {
	appID := c.Params("id")
	if code := p.gitRepoBrowserGate(c, appID); code != 0 {
		return c.Status(code).SendString("repository not available")
	}
	rel := c.Query("path", "")
	if strings.TrimSpace(rel) == "" {
		return c.Status(fiber.StatusBadRequest).SendString("path required")
	}
	full, err := p.Store.SafeGitRepoFilePath(appID, rel)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("invalid path")
	}
	st, err := os.Stat(full)
	if err != nil {
		if os.IsNotExist(err) {
			return c.Status(fiber.StatusNotFound).SendString("not found")
		}
		return c.Status(fiber.StatusInternalServerError).SendString(err.Error())
	}
	if st.IsDir() {
		return c.Status(fiber.StatusBadRequest).SendString("not a file")
	}
	download := c.Query("download") == "1"
	maxSz := int64(maxGitRepoBlobJSON)
	if download {
		maxSz = maxGitRepoBlobDownload
	}
	if st.Size() > maxSz {
		if !download {
			return c.Status(fiber.StatusRequestEntityTooLarge).SendString("file too large for inline view; add ?download=1")
		}
		return c.Status(fiber.StatusRequestEntityTooLarge).SendString("file too large")
	}
	f, err := os.Open(full)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString(err.Error())
	}
	defer f.Close()
	buf, err := io.ReadAll(f)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString(err.Error())
	}
	head := buf
	if len(head) > 512 {
		head = head[:512]
	}
	ct := workspaceFileContentType(full, head)
	fn := safeContentDispositionFilename(rel)
	if download {
		c.Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, fn))
	} else {
		c.Set("Content-Disposition", fmt.Sprintf(`inline; filename="%s"`, fn))
	}
	c.Type(ct)
	return c.Send(buf)
}

