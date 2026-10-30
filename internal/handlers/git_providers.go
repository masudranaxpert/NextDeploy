package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"panel/internal/db"
	"panel/internal/gitx"

	"github.com/gofiber/fiber/v2"
)

// fetchAccessibleReposCount returns the number of accessible repositories for a provider.
// repoCountCache keeps the "repositories accessible" figure per provider. It is shown on every
// page load, but asking GitHub for it takes hundreds of milliseconds, so the page gets the last
// known value at once and a fresh one is fetched in the background when it is older than 5 minutes.
var repoCountCache sync.Map // provider id -> repoCountEntry

type repoCountEntry struct {
	n          int
	at         time.Time
	refreshing *int32
}

func (p *Panel) fetchAccessibleReposCount(ctx context.Context, gp db.GitProvider, d db.GitHubProviderDetail) int {
	if v, ok := repoCountCache.Load(gp.ID); ok {
		e := v.(repoCountEntry)
		if time.Since(e.at) > 5*time.Minute && atomic.CompareAndSwapInt32(e.refreshing, 0, 1) {
			go func() {
				defer atomic.StoreInt32(e.refreshing, 0)
				bg, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				defer cancel()
				n := p.fetchAccessibleReposCountLive(bg, gp, d)
				repoCountCache.Store(gp.ID, repoCountEntry{n: n, at: time.Now(), refreshing: e.refreshing})
			}()
		}
		return e.n
	}
	flag := new(int32)
	atomic.StoreInt32(flag, 1)
	// Seed with 0 and fetch asynchronously in background to ensure instant page bootstrap.
	repoCountCache.Store(gp.ID, repoCountEntry{n: 0, at: time.Now(), refreshing: flag})
	go func() {
		defer atomic.StoreInt32(flag, 0)
		bg, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		n := p.fetchAccessibleReposCountLive(bg, gp, d)
		repoCountCache.Store(gp.ID, repoCountEntry{n: n, at: time.Now(), refreshing: flag})
	}()
	return 0
}

func (p *Panel) fetchAccessibleReposCountLive(ctx context.Context, gp db.GitProvider, d db.GitHubProviderDetail) int {
	if gp.Provider == "github" {
		var token string
		var endpoint string
		if strings.TrimSpace(gp.Token) != "" {
			token = strings.TrimSpace(gp.Token)
			endpoint = "https://api.github.com/user/repos?per_page=1"
		} else if strings.TrimSpace(d.InstallationID) != "" {
			minted, err := gitx.MintGitHubInstallationToken(ctx, d.GitHubAppID, d.InstallationID, d.PrivateKeyPEM)
			if err != nil {
				return 0
			}
			token = minted
			endpoint = "https://api.github.com/installation/repositories?per_page=1"
		} else {
			return 0
		}

		body, status, err := githubAPIRequest(ctx, http.MethodGet, endpoint, token, nil)
		if err != nil || status >= 300 {
			return 0
		}
		if strings.TrimSpace(gp.Token) != "" {
			var list []githubInstallationRepository
			if err := json.Unmarshal(body, &list); err == nil {
				return len(list)
			}
			return 0
		}
		var payload githubInstallationRepositoriesResponse
		if err := json.Unmarshal(body, &payload); err == nil {
			if payload.TotalCount > 0 {
				return payload.TotalCount
			}
			return len(payload.Repositories)
		}
		return 0
	} else if gp.Provider == "gitlab" {
		token, err := p.EnsureFreshGitLabToken(ctx, gp.ID)
		if err != nil || token == "" {
			return 0
		}
		endpoint := gitlabAPIBase + "/api/v4/projects?membership=true&per_page=100"
		body, status, header, err := gitlabAPIRequest(ctx, http.MethodGet, endpoint, token)
		if err != nil || status >= 300 {
			return 0
		}
		if header != nil && header.Get("X-Total") != "" {
			if total, err := strconv.Atoi(header.Get("X-Total")); err == nil {
				return total
			}
		}
		var projects []gitlabProject
		if err := json.Unmarshal(body, &projects); err == nil {
			return len(projects)
		}
	}
	return 0
}

// GitProviderCreate creates a new Git provider credential using a PAT or Deploy Token.
func (p *Panel) GitProviderCreate(c *fiber.Ctx) error {
	u, ok := CurrentUser(c)
	if !ok {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}

	var req struct {
		Name     string `json:"name"`
		Provider string `json:"provider"`
		Token    string `json:"token"`
		Notes    string `json:"notes"`
	}
	if err := c.BodyParser(&req); err != nil {
		req.Name = c.FormValue("name")
		req.Provider = c.FormValue("provider")
		req.Token = c.FormValue("token")
		req.Notes = c.FormValue("notes")
	}

	name := strings.TrimSpace(req.Name)
	provider := strings.ToLower(strings.TrimSpace(req.Provider))
	token := strings.TrimSpace(req.Token)
	notes := strings.TrimSpace(req.Notes)

	if provider != "github" && provider != "gitlab" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "provider must be 'github' or 'gitlab'"})
	}
	if token == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "token is required"})
	}
	if name == "" {
		name = provider + "-token"
	}
	if notes == "" {
		if provider == "github" {
			notes = "Personal Access Token"
		} else if strings.HasPrefix(token, "glpat-") {
			notes = "Personal Access Token"
		} else {
			notes = "Deploy Token"
		}
	}

	if provider == "gitlab" {
		testURL := gitlabAPIBase + "/api/v4/projects?membership=true&per_page=1"
		body, status, _, err := gitlabAPIRequest(c.UserContext(), http.MethodGet, testURL, token)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Failed to connect to GitLab: " + err.Error()})
		}
		if status == http.StatusUnauthorized {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid GitLab token (401 Unauthorized). Please check your token."})
		}
		if status == http.StatusForbidden {
			var errObj struct {
				Error            string `json:"error"`
				ErrorDescription string `json:"error_description"`
			}
			_ = json.Unmarshal(body, &errObj)
			msg := errObj.ErrorDescription
			if msg == "" {
				msg = errObj.Error
			}
			if msg == "" {
				msg = "Insufficient token permissions."
			}
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": fmt.Sprintf("GitLab permission denied (403): %s Make sure your token has 'read_api' and 'read_repository' (or 'Project: Read') permissions.", msg),
			})
		}
		if status >= 400 {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": fmt.Sprintf("GitLab API error (%d): %s", status, string(body))})
		}
	}

	uid := u.ID
	userID := &uid // owned by its creator, admins included

	pid, err := p.DB.CreateGitProvider(c.UserContext(), userID, name, provider, token, notes)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(fiber.Map{
		"ok":       true,
		"id":       pid,
		"name":     name,
		"provider": provider,
		"notes":    notes,
	})
}

// GitProvidersList returns all git providers with auth_method and install details.
func (p *Panel) GitProvidersList(c *fiber.Ctx) error {
	u, ok := CurrentUser(c)
	if !ok {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}
	var ownerPtr *int64
	if u.Role != db.RoleAdmin {
		uid := u.ID
		ownerPtr = &uid
	}
	providers, err := p.DB.ListGitProviders(c.UserContext(), ownerPtr)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	if providers == nil {
		providers = []db.GitProvider{}
	}

	details, _ := p.DB.ListGitHubProviderDetails(c.UserContext())
	detailMap := make(map[int64]db.GitHubProviderDetail)
	for _, d := range details {
		detailMap[d.ProviderID] = d
	}

	type providerRes struct {
		db.GitProvider
		AuthMethod      string `json:"auth_method"`
		InstallationID  string `json:"installation_id,omitempty"`
		AppSlug         string `json:"app_slug,omitempty"`
		AccountLogin    string `json:"account_login,omitempty"`
		InstallURL      string `json:"install_url,omitempty"`
		NeedsInstall    bool   `json:"needs_install,omitempty"`
		ReposAccessible int    `json:"repos_accessible"`
	}

	var res []providerRes
	for _, gp := range providers {
		pr := providerRes{GitProvider: gp}
		d := detailMap[gp.ID]
		if gp.Provider == "github" {
			if d.GitHubAppID != "" || d.AppSlug != "" {
				pr.AuthMethod = "GitHub App"
				pr.InstallationID = d.InstallationID
				pr.AppSlug = d.AppSlug
				pr.AccountLogin = d.AccountLogin
				if strings.TrimSpace(d.InstallationID) == "" {
					pr.NeedsInstall = true
				}
				pr.InstallURL = "/git/" + strconv.FormatInt(gp.ID, 10) + "/github/install"
			} else {
				pr.AuthMethod = "Fine-Grained PAT"
			}
		} else if gp.Provider == "gitlab" {
			if gp.RefreshToken != "" || gp.ExpiresAt > 0 {
				pr.AuthMethod = "OAuth 2.0"
			} else if strings.HasPrefix(gp.Token, "glpat-") {
				pr.AuthMethod = "Personal Access Token"
			} else {
				pr.AuthMethod = "Deploy Token"
			}
		} else {
			pr.AuthMethod = "Token"
		}
		if !pr.NeedsInstall {
			pr.ReposAccessible = p.fetchAccessibleReposCount(c.UserContext(), gp, d)
		}
		res = append(res, pr)
	}

	return c.JSON(fiber.Map{
		"ok":        true,
		"providers": res,
	})
}

// GitProviderUpdate updates an existing global Git provider credential.
func (p *Panel) GitProviderUpdate(c *fiber.Ctx) error {
	u, ok := CurrentUser(c)
	if !ok {
		if WantsJSON(c) {
			return c.Status(401).JSON(fiber.Map{"ok": false, "error": "unauthorized"})
		}
		return c.Redirect("/login")
	}
	idStr := c.Params("pid")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		if WantsJSON(c) {
			return c.Status(400).JSON(fiber.Map{"ok": false, "error": "Invalid ID"})
		}
		SetFlashError(c, "Invalid ID")
		return c.Redirect("/git")
	}

	existing, err := p.DB.GetGitProvider(c.UserContext(), id)
	if err != nil {
		if WantsJSON(c) {
			return c.Status(404).JSON(fiber.Map{"ok": false, "error": "Provider not found"})
		}
		SetFlashError(c, "Provider not found")
		return c.Redirect("/git")
	}

	if u.Role != db.RoleAdmin && (existing.UserID == nil || *existing.UserID != u.ID) {
		if WantsJSON(c) {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"ok": false, "error": "forbidden"})
		}
		return c.Status(fiber.StatusForbidden).SendString("forbidden")
	}

	name := strings.TrimSpace(c.FormValue("name"))
	provider := strings.TrimSpace(c.FormValue("provider"))
	token := strings.TrimSpace(c.FormValue("token"))
	notes := strings.TrimSpace(c.FormValue("notes"))

	if name == "" {
		name = existing.Name
	}
	if provider == "" {
		provider = existing.Provider
	}
	switch provider {
	case "github", "gitlab":
	default:
		provider = existing.Provider
	}
	if token == "" {
		token = existing.Token
	}

	if err := p.DB.UpdateGitProvider(c.UserContext(), id, name, provider, token, notes); err != nil {
		if WantsJSON(c) {
			return c.Status(500).JSON(fiber.Map{"ok": false, "error": err.Error()})
		}
		SetFlashError(c, err.Error())
		return c.Redirect("/git")
	}

	installID := strings.TrimSpace(c.FormValue("installation_id"))
	if installID != "" && (existing.Provider == "github" || provider == "github") {
		if detail, derr := p.DB.GetGitHubProviderDetail(c.UserContext(), id); derr == nil {
			detail.InstallationID = installID
			_ = p.DB.UpsertGitHubProviderDetail(c.UserContext(), detail)
		}
	}

	if WantsJSON(c) {
		return c.JSON(fiber.Map{"ok": true})
	}
	SetFlash(c, "saved")
	return c.Redirect("/git")
}

// GitProviderDelete deletes a global Git provider credential.
func (p *Panel) GitProviderDelete(c *fiber.Ctx) error {
	u, ok := CurrentUser(c)
	if !ok {
		if WantsJSON(c) {
			return c.Status(401).JSON(fiber.Map{"ok": false, "error": "unauthorized"})
		}
		return c.Redirect("/login")
	}
	idStr := c.Params("pid")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		if WantsJSON(c) {
			return c.Status(400).JSON(fiber.Map{"ok": false, "error": "Invalid ID"})
		}
		SetFlashError(c, "Invalid ID")
		return c.Redirect("/git")
	}
	existing, err := p.DB.GetGitProvider(c.UserContext(), id)
	if err != nil {
		if WantsJSON(c) {
			return c.Status(404).JSON(fiber.Map{"ok": false, "error": "Provider not found"})
		}
		SetFlashError(c, "Provider not found")
		return c.Redirect("/git")
	}
	if u.Role != db.RoleAdmin && (existing.UserID == nil || *existing.UserID != u.ID) {
		if WantsJSON(c) {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"ok": false, "error": "forbidden"})
		}
		return c.Status(fiber.StatusForbidden).SendString("forbidden")
	}
	if err := p.DB.DeleteGitProvider(c.UserContext(), id); err != nil {
		if WantsJSON(c) {
			return c.Status(500).JSON(fiber.Map{"ok": false, "error": err.Error()})
		}
		SetFlashError(c, err.Error())
		return c.Redirect("/git")
	}
	if WantsJSON(c) {
		return c.JSON(fiber.Map{"ok": true})
	}
	SetFlash(c, "deleted")
	return c.Redirect("/git")
}

// AppSwitchSource switches an app between "files" and "git" source types.
func (p *Panel) AppSwitchSource(c *fiber.Ctx) error {
	id := c.Params("id")
	if _, err := p.DB.GetApp(c.UserContext(), id); err != nil {
		return c.Status(404).SendString("not found")
	}

	newSource := strings.TrimSpace(c.FormValue("source_type"))
	if newSource != "files" && newSource != "git" {
		if WantsJSON(c) {
			return c.Status(400).JSON(fiber.Map{"ok": false, "error": "invalid source_type"})
		}
		return c.Redirect("/apps/" + id + "?tab=overview")
	}

	if newSource == "files" {
		_ = p.DB.DeleteAppGitConfig(c.UserContext(), id)
	}
	if newSource == "git" {
		// Re-selecting Git as the source also turns a paused Git sync back on.
		if cfg, cerr := p.DB.GetAppGitConfig(c.UserContext(), id); cerr == nil && cfg.SyncPaused {
			_ = p.DB.SetAppGitSyncPaused(c.UserContext(), id, false)
		}
		if err := p.Store.ClearUploadedProjectForGitSource(id); err != nil {
			if WantsJSON(c) {
				return c.Status(500).JSON(fiber.Map{"ok": false, "error": err.Error()})
			}
			SetFlash(c, "switchError_clear")
			return c.Redirect("/apps/" + id + "?tab=overview")
		}
	}

	if err := p.DB.SetAppSourceType(c.UserContext(), id, newSource); err != nil {
		if WantsJSON(c) {
			return c.Status(500).JSON(fiber.Map{"ok": false, "error": "Failed to switch source type"})
		}
		SetFlashError(c, "Failed to switch source type")
		return c.Redirect("/apps/" + id + "?tab=overview")
	}

	if WantsJSON(c) {
		return c.JSON(fiber.Map{"ok": true, "source_type": newSource})
	}
	SetFlash(c, "sourceSwitched")
	if newSource == "git" {
		return c.Redirect("/apps/" + id + "?tab=git")
	}
	return c.Redirect("/apps/" + id + "?tab=files")
}

// APIWebhookDeliveries returns the recent real webhook deliveries (last 10).
func (p *Panel) APIWebhookDeliveries(c *fiber.Ctx) error {
	u, ok := CurrentUser(c)
	if !ok {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}
	limit := 10
	if l := c.QueryInt("limit", 10); l > 0 && l <= 50 {
		limit = l
	}
	list := p.visibleWebhookDeliveries(c.UserContext(), u, limit)
	if list == nil {
		list = []db.WebhookDelivery{}
	}
	return c.JSON(list)
}
