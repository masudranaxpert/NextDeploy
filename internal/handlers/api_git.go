package handlers

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"panel/internal/db"
	"panel/internal/gitx"

	"github.com/gofiber/fiber/v2"
)

const defaultWordPressGitignore = `# NextDeploy default PHP/WordPress ignores
.env
.env.*
!.env.example
wp-config.php
wp-content/cache/
wp-content/upgrade/
*.log
.DS_Store
`

// APIGitStatus returns the Git configuration and sync status for an app.
// GET /api/v1/apps/:id/git
func (p *Panel) APIGitStatus(c *fiber.Ctx) error {
	ctx := c.UserContext()
	appID := strings.TrimSpace(c.Params("id"))
	if _, err := p.RequireAppAccess(c, appID, db.CollabRoleViewer); err != nil {
		return nil
	}
	st := p.GitSyncStatus(ctx, appID)
	cfg, cfgErr := p.DB.GetAppGitConfig(ctx, appID)
	res := fiber.Map{
		"ok":          true,
		"status":      st,
		"is_php_site": p.DB.IsPHPSite(ctx, appID),
	}
	if cfgErr == nil {
		res["auth_mode"] = cfg.AuthMode
		res["provider"] = cfg.Provider
		res["git_provider_id"] = cfg.GitProviderID
		res["auto_deploy"] = cfg.AutoDeploy
		res["last_deploy_ref"] = cfg.LastDeployRef
	}
	return c.JSON(res)
}

// APIGitConnect connects a Git repository to an app (or PHP/WordPress site) via JSON API.
// POST /api/v1/apps/:id/git/connect
func (p *Panel) APIGitConnect(c *fiber.Ctx) error {
	ctx := c.UserContext()
	appID := strings.TrimSpace(c.Params("id"))
	if _, err := p.RequireAppAccess(c, appID, db.CollabRoleDeveloper); err != nil {
		return nil
	}
	u, _ := currentUser(c)

	var req struct {
		RepoURL       string `json:"repo_url"`
		Branch        string `json:"branch"`
		AuthMode      string `json:"auth_mode"`
		GitProviderID int64  `json:"git_provider_id"`
		AutoDeploy    *bool  `json:"auto_deploy"`
		SkipSync      bool   `json:"skip_sync"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"ok": false, "error": "invalid JSON body"})
	}
	repoURL := NormalizeRepoURL(req.RepoURL)
	if repoURL == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"ok": false, "error": "repo_url is required"})
	}

	cfg, err := p.buildGitConfigFromAPI(ctx, u, appID, repoURL, req.Branch, req.AuthMode, req.GitProviderID, req.AutoDeploy)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"ok": false, "error": err.Error()})
	}

	_, oldCfgErr := p.DB.GetAppGitConfig(ctx, appID)
	if err := p.DB.UpsertAppGitConfig(ctx, cfg); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"ok": false, "error": err.Error()})
	}
	if oldCfgErr != nil && !p.DB.IsPHPSite(ctx, appID) {
		_ = p.Store.ClearUploadedProjectForGitSource(appID)
	}
	if !p.DB.IsPHPSite(ctx, appID) {
		_ = p.DB.SetAppSourceType(ctx, appID, "git")
	}

	if !req.SkipSync && !p.DB.IsPHPSite(ctx, appID) {
		syncCtx, cancel := context.WithTimeout(ctx, 15*time.Minute)
		defer cancel()
		if _, err := p.SyncGitAppSource(syncCtx, appID); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"ok":    false,
				"error": "Configuration saved, but repository sync failed: " + err.Error(),
			})
		}
		p.InvalidateAfterAppWorkspaceChange(appID)
	}

	p.RecordAuditLog(c, "git_connect", "app", appID, "Connected Git repository via CLI/API: "+gitx.SafeRepoURL(cfg.RepoURL))
	return c.JSON(fiber.Map{
		"ok":     true,
		"status": p.GitSyncStatus(ctx, appID),
	})
}

// APIGitSync pulls the latest commits for an app.
// POST /api/v1/apps/:id/git/sync
func (p *Panel) APIGitSync(c *fiber.Ctx) error {
	ctx := c.UserContext()
	appID := strings.TrimSpace(c.Params("id"))
	if _, err := p.RequireAppAccess(c, appID, db.CollabRoleDeveloper); err != nil {
		return nil
	}
	syncCtx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	out, err := p.SyncGitAppSource(syncCtx, appID)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"ok": false, "error": err.Error()})
	}
	p.InvalidateAfterAppWorkspaceChange(appID)
	return c.JSON(fiber.Map{
		"ok":     true,
		"output": strings.TrimSpace(out),
		"status": p.GitSyncStatus(ctx, appID),
	})
}

// APIGitPush commits and pushes the app workspace (or WordPress/PHP htdocs/) to the remote Git repository.
// POST /api/v1/apps/:id/git/push
func (p *Panel) APIGitPush(c *fiber.Ctx) error {
	ctx := c.UserContext()
	appID := strings.TrimSpace(c.Params("id"))
	if _, err := p.RequireAppAccess(c, appID, db.CollabRoleDeveloper); err != nil {
		return nil
	}
	u, _ := currentUser(c)

	var req struct {
		RepoURL       string `json:"repo_url"`
		Branch        string `json:"branch"`
		Message       string `json:"message"`
		AuthMode      string `json:"auth_mode"`
		GitProviderID int64  `json:"git_provider_id"`
		Force         bool   `json:"force"`
	}
	_ = c.BodyParser(&req)

	cfg, cfgErr := p.DB.GetAppGitConfig(ctx, appID)
	if cfgErr != nil || strings.TrimSpace(req.RepoURL) != "" {
		repoURL := NormalizeRepoURL(req.RepoURL)
		if repoURL == "" && cfgErr == nil {
			repoURL = cfg.RepoURL
		}
		if repoURL == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"ok":    false,
				"error": "no Git repository configured for this app; pass --repo <url> or run 'nd git connect <url>' first",
			})
		}
		branch := req.Branch
		if branch == "" && cfgErr == nil {
			branch = cfg.Branch
		}
		built, berr := p.buildGitConfigFromAPI(ctx, u, appID, repoURL, branch, req.AuthMode, req.GitProviderID, nil)
		if berr != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"ok": false, "error": berr.Error()})
		}
		cfg = built
		_ = p.DB.UpsertAppGitConfig(ctx, cfg)
	} else if strings.TrimSpace(req.Branch) != "" {
		cfg.Branch = NormalizeBranch(req.Branch)
	}

	token, err := p.resolveGitAuthToken(ctx, cfg)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"ok": false, "error": "git auth failed: " + err.Error()})
	}

	isPHP := p.DB.IsPHPSite(ctx, appID)
	workDir := p.Store.Path(appID)
	if isPHP {
		workDir = filepath.Join(workDir, "htdocs")
		// Ensure WordPress/PHP secrets are never accidentally pushed to GitHub.
		giPath := filepath.Join(workDir, ".gitignore")
		if _, statErr := os.Stat(giPath); os.IsNotExist(statErr) {
			_ = os.WriteFile(giPath, []byte(defaultWordPressGitignore), 0o644)
		}
	} else if p.IsGitApp(ctx, appID) && gitx.RepoExists(p.AppCheckoutPath(appID)) {
		workDir = p.AppCheckoutPath(appID)
	}

	if fi, serr := os.Stat(workDir); serr != nil || !fi.IsDir() {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"ok": false, "error": "workspace directory not found on disk"})
	}

	pushCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	authorName := u.Username
	if authorName == "" {
		authorName = "NextDeploy"
	}
	res := gitx.Push(pushCtx, workDir, cfg.RepoURL, cfg.Branch, cfg.AuthMode, token, req.Message, authorName, authorName+"@nextdeploy.local", req.Force)
	if isPHP && p.PHP != nil {
		if site, serr := p.DB.GetPHPSite(ctx, appID); serr == nil {
			p.PHP.FixOwnership(site)
		}
	}
	if !res.OK {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"ok":     false,
			"error":  "git push failed: " + strings.TrimSpace(res.Output),
			"output": res.Output,
		})
	}

	commit := gitx.CurrentCommit(pushCtx, workDir)
	if commit != "" {
		cfg.LastDeployRef = commit
		_ = p.DB.UpsertAppGitConfig(ctx, cfg)
	}
	p.RecordAuditLog(c, "git_push", "app", appID, "Pushed workspace to "+gitx.SafeRepoURL(cfg.RepoURL)+" ("+cfg.Branch+")")
	return c.JSON(fiber.Map{
		"ok":       true,
		"repo_url": gitx.SafeRepoURL(cfg.RepoURL),
		"branch":   cfg.Branch,
		"commit":   commit,
		"is_php":   isPHP,
		"output":   strings.TrimSpace(res.Output),
	})
}

// APIGitDisconnect removes the Git configuration from an app.
// DELETE /api/v1/apps/:id/git
func (p *Panel) APIGitDisconnect(c *fiber.Ctx) error {
	ctx := c.UserContext()
	appID := strings.TrimSpace(c.Params("id"))
	if _, err := p.RequireAppAccess(c, appID, db.CollabRoleDeveloper); err != nil {
		return nil
	}
	if err := p.DB.DeleteAppGitConfig(ctx, appID); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"ok": false, "error": err.Error()})
	}
	_ = p.DB.SetAppSourceType(ctx, appID, "files")
	p.RecordAuditLog(c, "git_disconnect", "app", appID, "Disconnected Git repository via CLI/API")
	return c.JSON(fiber.Map{"ok": true})
}

func (p *Panel) buildGitConfigFromAPI(ctx context.Context, u db.User, appID, repoURL, branch, authMode string, providerID int64, autoDeploy *bool) (db.AppGitConfig, error) {
	providerName := "github"
	if strings.Contains(strings.ToLower(repoURL), "gitlab") {
		providerName = "gitlab"
	}
	mode := strings.TrimSpace(authMode)
	if mode == "" {
		if providerName == "gitlab" {
			mode = "gitlab_token"
		} else {
			mode = "github_app"
		}
	}
	auto := true
	if autoDeploy != nil {
		auto = *autoDeploy
	}
	cfg := db.AppGitConfig{
		AppID:         appID,
		Provider:      providerName,
		RepoURL:       repoURL,
		RepoFullName:  RepoFullNameFromURL(repoURL),
		Branch:        NormalizeBranch(branch),
		AuthMode:      mode,
		GitProviderID: providerID,
		WebhookSecret: RandomHex(16),
		AutoDeploy:    auto,
	}
	if mode == "public" {
		cfg.GitProviderID = 0
		return cfg, nil
	}

	if cfg.GitProviderID == 0 {
		// Auto-select the user's matching connected provider if available.
		var uid *int64
		if u.ID > 0 {
			uid = &u.ID
		}
		providers, _ := p.DB.ListGitProviders(ctx, uid)
		for _, gp := range providers {
			if gp.Provider == providerName {
				cfg.GitProviderID = gp.ID
				break
			}
		}
		if cfg.GitProviderID == 0 && u.Role == db.RoleAdmin {
			all, _ := p.DB.ListGitProviders(ctx, nil)
			for _, gp := range all {
				if gp.Provider == providerName {
					cfg.GitProviderID = gp.ID
					break
				}
			}
		}
	}

	if cfg.GitProviderID == 0 {
		if authMode == "" {
			// Fall back to public if no provider is connected and caller didn't force an auth mode.
			cfg.AuthMode = "public"
			return cfg, nil
		}
		return cfg, fiber.NewError(fiber.StatusBadRequest, "no connected "+providerName+" provider found; connect one in the panel or use --public")
	}

	gp, err := p.DB.GetGitProvider(ctx, cfg.GitProviderID)
	if err != nil {
		return cfg, fiber.NewError(fiber.StatusBadRequest, "git provider not found")
	}
	if u.Role != db.RoleAdmin && (gp.UserID == nil || *gp.UserID != u.ID) {
		return cfg, fiber.NewError(fiber.StatusForbidden, "forbidden: you do not own this git provider")
	}
	if gp.Provider == "gitlab" {
		cfg.Provider = "gitlab"
		cfg.AuthMode = "gitlab_token"
		return cfg, nil
	}
	cfg.Provider = "github"
	cfg.AuthMode = "github_app"
	if strings.TrimSpace(gp.Token) != "" {
		cfg.Token = strings.TrimSpace(gp.Token)
	} else {
		detail, derr := p.DB.GetGitHubProviderDetail(ctx, gp.ID)
		if derr != nil {
			return cfg, fiber.NewError(fiber.StatusBadRequest, "GitHub App provider is not ready yet")
		}
		cfg.AppGitID = detail.GitHubAppID
		cfg.InstallationID = detail.InstallationID
		cfg.PrivateKeyPEM = detail.PrivateKeyPEM
		if strings.TrimSpace(detail.WebhookSecret) != "" {
			cfg.WebhookSecret = detail.WebhookSecret
		}
	}
	return cfg, nil
}
