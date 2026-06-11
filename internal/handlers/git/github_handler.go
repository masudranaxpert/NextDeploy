package git

import (
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
	"panel/internal/handlers"
	"panel/internal/handlers/utils"
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

// commitPageURL builds a web URL to the commit on GitHub or GitLab (hosted or self-managed).
func commitPageURL(cfg db.AppGitConfig, fullSHA string) string {
	fullSHA = strings.TrimSpace(fullSHA)
	if fullSHA == "" {
		return ""
	}
	fn := strings.TrimSpace(cfg.RepoFullName)
	if fn == "" {
		fn = repoFullNameFromURL(cfg.RepoURL)
	}
	if fn == "" {
		return ""
	}
	raw := strings.TrimSpace(cfg.RepoURL)
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}
	host := strings.ToLower(u.Host)
	prov := strings.ToLower(strings.TrimSpace(cfg.Provider))
	// GitHub.com, GitHub Enterprise (custom host), and explicit github provider use /commit/SHA.
	if prov == "github" || strings.Contains(host, "github") {
		return fmt.Sprintf("https://%s/%s/commit/%s", u.Host, fn, fullSHA)
	}
	if prov == "gitlab" || strings.Contains(host, "gitlab") {
		return fmt.Sprintf("https://%s/%s/-/commit/%s", u.Host, fn, fullSHA)
	}
	return ""
}

// gitDeployedSummary returns short SHA, subject line, and optional host commit URL for UI (Overview / Deploy / Git).
func (h *Handler) gitDeployedSummary(ctx context.Context, appID string, cfg db.AppGitConfig) (shortSHA, subject, pageURL string) {
	sha := strings.TrimSpace(cfg.LastDeployRef)
	if sha == "" {
		return "", "", ""
	}
	if len(sha) >= 7 {
		shortSHA = sha[:7]
	} else {
		shortSHA = sha
	}
	pageURL = commitPageURL(cfg, sha)
	repoDir := h.appCheckoutPath(appID)
	if gitx.RepoExists(repoDir) {
		if s := gitx.CurrentCommitSubject(ctx, repoDir); s != "" {
			subject = s
		}
	}
	return shortSHA, subject, pageURL
}

func (h *Handler) GitConfigSave(c *fiber.Ctx) error {
	appID := c.Params("id")
	if _, err := h.p.DB.GetApp(c.UserContext(), appID); err != nil {
		return utils.RespondAppNotFound(c)
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
	if cfg.GitProviderID > 0 {
		u, ok := handlers.CurrentUser(c)
		if !ok {
			return c.Status(401).SendString("unauthorized")
		}
		provider, perr := h.p.DB.GetGitProvider(c.UserContext(), cfg.GitProviderID)
		if perr != nil {
			h.p.SetGitTabErrorCookie(c, appID, "Selected Git provider not found")
			return c.Redirect(fmt.Sprintf("/apps/%s?tab=git", appID))
		}
		if u.Role != db.RoleAdmin && (provider.UserID == nil || *provider.UserID != u.ID) {
			return c.Status(403).SendString("forbidden")
		}
	}
	old, oldCfgErr := h.p.DB.GetAppGitConfig(c.UserContext(), appID)
	if oldCfgErr == nil && strings.TrimSpace(old.WebhookSecret) != "" {
		cfg.WebhookSecret = old.WebhookSecret
	} else {
		cfg.WebhookSecret = randomSecret()
	}
	if cfg.AuthMode == "github_app" && cfg.GitProviderID > 0 {
		detail, derr := h.p.DB.GetGitHubProviderDetail(c.UserContext(), cfg.GitProviderID)
		if derr != nil {
			h.p.SetGitTabErrorCookie(c, appID, "Selected GitHub provider is not ready yet")
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
		glProvider, gerr := h.p.DB.GetGitProvider(c.UserContext(), cfg.GitProviderID)
		if gerr != nil || strings.TrimSpace(glProvider.Token) == "" {
			h.p.SetGitTabErrorCookie(c, appID, "Selected GitLab provider has no token — reconnect from Git Providers page")
			return c.Redirect(fmt.Sprintf("/apps/%s?tab=git", appID))
		}
		cfg.Provider = "gitlab"
	}
	if err := h.p.DB.UpsertAppGitConfig(c.UserContext(), cfg); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	// First-time Git setup: remove file-upload workspace so the clone is the single source of truth.
	if oldCfgErr != nil {
		if err := h.p.Store.ClearUploadedProjectForGitSource(appID); err != nil {
			return c.Status(500).SendString(err.Error())
		}
	}
	if cfg.AuthMode == "github_app" && cfg.AutoDeploy {
		if err := h.ensureRepoWebhook(c.UserContext(), c, appID, cfg); err != nil {
			var apiErr *githubWebhookAPIError
			if errors.As(err, &apiErr) && apiErr.IsPermissionDenied() {
				cfg.AutoDeploy = false
				_ = h.p.DB.UpsertAppGitConfig(c.UserContext(), cfg)
				h.p.SetGitTabErrorCookie(c, appID, friendlyGitHubWebhookSetupError(err))
			} else {
				h.p.SetGitTabErrorCookie(c, appID, err.Error())
				return c.Redirect(fmt.Sprintf("/apps/%s?tab=git", appID))
			}
		}
	}
	// Mark app as git-sourced
	_ = h.p.DB.SetAppSourceType(c.UserContext(), appID, "git")

	// If the repository URL changed, drop the old checkout so the next sync clones the new remote.
	if oldCfgErr == nil && strings.TrimSpace(old.RepoURL) != "" &&
		normalizeRepoURL(old.RepoURL) != normalizeRepoURL(cfg.RepoURL) {
		_ = os.RemoveAll(h.appCheckoutPath(appID))
	}

	// Best practice: persist config then immediately materialize workspace (clone/fetch) so branch/URL changes apply.
	ctx, cancel := context.WithTimeout(c.UserContext(), 15*time.Minute)
	defer cancel()
	if _, err := h.syncGitAppSource(ctx, appID); err != nil {
		h.p.SetGitTabErrorCookie(c, appID, "Configuration saved, but repository sync failed: "+err.Error())
		return c.Redirect(fmt.Sprintf("/apps/%s?tab=git", appID))
	}
	h.p.SetGitTabFlashCookie(c, appID, "saved_synced")
	return c.Redirect(fmt.Sprintf("/apps/%s?tab=git", appID))
}

func (h *Handler) GitConfigDelete(c *fiber.Ctx) error {
	appID := c.Params("id")
	if _, err := h.p.DB.GetApp(c.UserContext(), appID); err != nil {
		return utils.RespondAppNotFound(c)
	}
	if err := h.p.DB.DeleteAppGitConfig(c.UserContext(), appID); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	// Revert to files source
	_ = h.p.DB.SetAppSourceType(c.UserContext(), appID, "files")
	return c.Redirect(fmt.Sprintf("/apps/%s?tab=overview", appID))
}

func (h *Handler) appCheckoutPath(appID string) string {
	return filepath.Join(h.p.Store.ReservedPath(appID), "repo")
}

func (h *Handler) ensureGitWorkspace(appID string) error {
	return os.MkdirAll(h.p.Store.ReservedPath(appID), 0o750)
}

func (h *Handler) syncGitAppSource(ctx context.Context, appID string) (string, error) {
	cfg, err := h.p.DB.GetAppGitConfig(ctx, appID)
	if err != nil {
		return "", err
	}
	if err := h.ensureGitWorkspace(appID); err != nil {
		return "", err
	}
	repoDir := h.appCheckoutPath(appID)

	_ = os.Remove(filepath.Join(repoDir, ".git", "index.lock"))

	token, err := h.resolveGitAuthToken(ctx, cfg)
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
		out := res.Output
		lowerOut := strings.ToLower(out)
		if strings.Contains(lowerOut, "could not read username") ||
			strings.Contains(lowerOut, "authentication failed") ||
			strings.Contains(lowerOut, "not found") ||
			strings.Contains(lowerOut, "terminal prompts disabled") {

			friendly := fmt.Sprintf("Repository sync failed. This repository appears to be private or requires authorization. Please configure the correct 'Access mode' (e.g. GitHub App or Access Token) instead of 'Public', or verify your credentials. Git error:\n%s", out)
			return out, errors.New(friendly)
		}
		return res.Output, errors.New(res.Output)
	}
	// Restore workspace .env from the panel DB after clone/pull so git checkout/clean cannot drop or replace it
	// (e.g. tracked .env in the repo, or untracked .env removed by older clean rules).
	panelEnv, _ := h.p.DB.GetPanelEnv(ctx, appID)
	_ = h.p.SyncWorkspaceEnvFromPanel(appID, repoDir, panelEnv)
	commit := gitx.CurrentCommit(ctx, repoDir)
	cfg.LastDeployRef = commit
	_ = h.p.DB.UpsertAppGitConfig(ctx, cfg)
	return res.Output, nil
}

func (h *Handler) resolveGitAuthToken(ctx context.Context, cfg db.AppGitConfig) (string, error) {
	if cfg.AuthMode == "public" {
		return "", nil
	}
	if cfg.AuthMode == "github_app" {
		if cfg.GitProviderID > 0 {
			detail, err := h.p.DB.GetGitHubProviderDetail(ctx, cfg.GitProviderID)
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
		return h.EnsureFreshGitLabToken(ctx, cfg.GitProviderID)
	}
	if cfg.GitProviderID > 0 && strings.TrimSpace(cfg.Token) == "" {
		provider, err := h.p.DB.GetGitProvider(ctx, cfg.GitProviderID)
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

func (h *Handler) ensureRepoWebhook(ctx context.Context, c *fiber.Ctx, appID string, cfg db.AppGitConfig) error {
	if cfg.Provider != "github" || cfg.AuthMode != "github_app" || cfg.RepoFullName == "" {
		return nil
	}
	token, err := h.resolveGitAuthToken(ctx, cfg)
	if err != nil {
		return err
	}
	parts := strings.SplitN(cfg.RepoFullName, "/", 2)
	if len(parts) != 2 {
		return nil
	}
	hookURL := h.appWebhookURL(c, appID)
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

func (h *Handler) AppGitProviderRepos(c *fiber.Ctx) error {
	u, ok := handlers.CurrentUser(c)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"error": "unauthorized"})
	}
	appID := c.Params("id")
	if _, err := h.p.DB.GetApp(c.UserContext(), appID); err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "app not found"})
	}
	pid, err := strconv.ParseInt(c.Params("pid"), 10, 64)
	if err != nil || pid <= 0 {
		return c.Status(400).JSON(fiber.Map{"error": "invalid provider"})
	}
	provider, err := h.p.DB.GetGitProvider(c.UserContext(), pid)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "provider not found"})
	}
	if u.Role != db.RoleAdmin && (provider.UserID == nil || *provider.UserID != u.ID) {
		return c.Status(403).JSON(fiber.Map{"error": "forbidden"})
	}
	if provider.Provider == "gitlab" {
		return h.AppGitLabProviderRepos(c)
	}
	if provider.Provider != "github" {
		return c.Status(400).JSON(fiber.Map{"error": "repository picker is only available for GitHub App or GitLab providers"})
	}
	detail, err := h.p.DB.GetGitHubProviderDetail(c.UserContext(), pid)
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

func (h *Handler) AppGitProviderBranches(c *fiber.Ctx) error {
	u, ok := handlers.CurrentUser(c)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"error": "unauthorized"})
	}
	appID := c.Params("id")
	if _, err := h.p.DB.GetApp(c.UserContext(), appID); err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "app not found"})
	}
	pid, err := strconv.ParseInt(c.Params("pid"), 10, 64)
	if err != nil || pid <= 0 {
		return c.Status(400).JSON(fiber.Map{"error": "invalid provider"})
	}
	provider, err := h.p.DB.GetGitProvider(c.UserContext(), pid)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "provider not found"})
	}
	if u.Role != db.RoleAdmin && (provider.UserID == nil || *provider.UserID != u.ID) {
		return c.Status(403).JSON(fiber.Map{"error": "forbidden"})
	}
	if provider.Provider == "gitlab" {
		return h.AppGitLabProviderBranches(c)
	}
	repoFullName := strings.TrimSpace(c.Query("repo"))
	parts := strings.SplitN(repoFullName, "/", 2)
	if len(parts) != 2 {
		return c.Status(400).JSON(fiber.Map{"error": "invalid repo"})
	}
	if provider.Provider != "github" {
		return c.Status(400).JSON(fiber.Map{"error": "branch picker is only available for GitHub App or GitLab providers"})
	}
	detail, err := h.p.DB.GetGitHubProviderDetail(c.UserContext(), pid)
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

func (h *Handler) GitSync(c *fiber.Ctx) error {
	appID := c.Params("id")
	if _, err := h.p.DB.GetApp(c.UserContext(), appID); err != nil {
		return utils.RespondAppNotFound(c)
	}
	ctx, cancel := context.WithTimeout(c.UserContext(), 15*time.Minute)
	defer cancel()
	if _, err := h.syncGitAppSource(ctx, appID); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	h.p.SetGitTabFlashCookie(c, appID, "synced")
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

func (h *Handler) GitHubWebhook(c *fiber.Ctx) error {
	appID := c.Params("id")
	app, err := h.p.DB.GetApp(c.UserContext(), appID)
	if err != nil {
		return c.SendStatus(fiber.StatusNotFound)
	}
	cfg, err := h.p.DB.GetAppGitConfig(c.UserContext(), appID)
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
	if ok, err := h.p.DB.MarkWebhookDelivery(c.UserContext(), appID, deliveryID); err != nil || !ok {
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
		gitOut, err := h.syncGitAppSource(bg, appID)
		if err != nil {
			_ = h.p.DB.InsertDeployLog(bg, appID, "Webhook sync", false, err.Error())
			return
		}
		gitPreamble := strings.TrimSpace(gitOut)
		if gitPreamble == "" {
			gitPreamble = "Repository sync completed."
		}
		if err := h.p.SyncAppCaddyOverrideCtx(bg, appID); err != nil {
			_ = h.p.DB.InsertDeployLog(bg, appID, "Webhook deploy", false, err.Error())
			return
		}
		projCtx, projCancel := context.WithTimeout(bg, 90*time.Second)
		project := h.p.ActiveComposeProjectName(projCtx, app, appID)
		projCancel()
		stopCtx, stopCancel := context.WithTimeout(bg, 5*time.Minute)
		h.p.StopOtherComposeStacks(stopCtx, app, appID, project)
		stopCancel()
		_ = h.p.StartComposeJob(appID, project, h.p.EffectiveComposePaths(bg, app, appID), "Webhook redeploy", dockerx.ComposePullUp, gitPreamble)
	}()
	return c.SendStatus(fiber.StatusOK)
}

const (
	maxGitRepoBlobJSON     = 512 << 10 // JSON text preview in UI
	maxGitRepoBlobDownload = 32 << 20  // raw download limit
)

func (h *Handler) gitRepoBrowserGate(c *fiber.Ctx, appID string) int {
	if _, err := h.p.DB.GetApp(c.UserContext(), appID); err != nil {
		return fiber.StatusNotFound
	}
	if !h.p.IsGitApp(c.UserContext(), appID) {
		return fiber.StatusBadRequest
	}
	if _, err := h.p.DB.GetAppGitConfig(c.UserContext(), appID); err != nil {
		return fiber.StatusNotFound
	}
	if !gitx.RepoExists(h.appCheckoutPath(appID)) {
		return fiber.StatusNotFound
	}
	return 0
}

// GitRepoTree returns directory listing JSON for the checked-out repository (read-only; .git hidden).
func (h *Handler) GitRepoTree(c *fiber.Ctx) error {
	appID := c.Params("id")
	if code := h.gitRepoBrowserGate(c, appID); code != 0 {
		msg := "not available"
		if code == fiber.StatusNotFound {
			msg = "repository not available; run Sync first"
		}
		return c.Status(code).JSON(fiber.Map{"error": msg})
	}
	rel := c.Query("path", "")
	children, err := h.p.Store.ListGitRepoChildren(appID, rel)
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
		ModTime int64  `json:"mod_ts"`
		Perms   string `json:"perms"`
	}
	out := make([]row, 0, len(children))
	for _, ch := range children {
		out = append(out, row{
			Name:    ch.Name,
			RelPath: ch.RelPath,
			IsDir:   ch.IsDir,
			Size:    ch.Size,
			ModTime: ch.ModTime.Unix() * 1000,
			Perms:   ch.Perms,
		})
	}
	parent := h.p.Store.ParentRel(rel)
	return c.JSON(fiber.Map{
		"path":    rel,
		"parent":  parent,
		"entries": out,
	})
}

// GitRepoBlob returns JSON with file text for the UI preview, or metadata for binary/oversized files.
func (h *Handler) GitRepoBlob(c *fiber.Ctx) error {
	appID := c.Params("id")
	if code := h.gitRepoBrowserGate(c, appID); code != 0 {
		return c.Status(code).JSON(fiber.Map{"error": "repository not available"})
	}
	rel := c.Query("path", "")
	if strings.TrimSpace(rel) == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "path required"})
	}
	full, err := h.p.Store.SafeGitRepoFilePath(appID, rel)
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
			"path":         rel,
			"name":         name,
			"size":         st.Size(),
			"too_large":    true,
			"max_bytes":    maxGitRepoBlobJSON,
			"raw_url":      rawURL,
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
	preview, isBinary := utils.GitRepoBlobPreviewText(b)
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
func (h *Handler) GitRepoRaw(c *fiber.Ctx) error {
	appID := c.Params("id")
	if code := h.gitRepoBrowserGate(c, appID); code != 0 {
		return c.Status(code).SendString("repository not available")
	}
	rel := c.Query("path", "")
	if strings.TrimSpace(rel) == "" {
		return c.Status(fiber.StatusBadRequest).SendString("path required")
	}
	full, err := h.p.Store.SafeGitRepoFilePath(appID, rel)
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
	ct := utils.WorkspaceFileContentType(full, head)
	fn := utils.SafeContentDispositionFilename(rel)
	if download {
		c.Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, fn))
	} else {
		c.Set("Content-Disposition", fmt.Sprintf(`inline; filename="%s"`, fn))
	}
	c.Type(ct)
	return c.Send(buf)
}
