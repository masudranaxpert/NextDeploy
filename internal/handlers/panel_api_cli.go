package handlers

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"panel/internal/db"
	"panel/internal/wsmanifest"

	"github.com/gofiber/fiber/v2"
)

// ─── Apps & Manifest ────────────────────────────────────────────────────────

// APIAppsList returns all applications accessible to the authenticated user.
// GET /api/v1/apps
func (p *Panel) APIAppsList(c *fiber.Ctx) error {
	ctx := c.UserContext()
	u, ok := currentUser(c)
	if !ok {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}

	var apps []db.App
	var err error
	if u.Role == db.RoleAdmin {
		apps, err = p.DB.ListApps(ctx)
	} else {
		apps, err = p.DB.ListAppsForUser(ctx, u.ID)
	}
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	type appItem struct {
		ID        string   `json:"id"`
		Name      string   `json:"name"`
		Status    string   `json:"status"`
		CreatedAt string   `json:"created_at"`
		Domains   []string `json:"domains"`
		Services  []string `json:"services"`
	}
	out := make([]appItem, 0, len(apps))
	for _, a := range apps {
		domains, _ := p.DB.ListAppDomains(ctx, a.ID)
		dNames := make([]string, 0, len(domains))
		for _, d := range domains {
			dNames = append(dNames, d.Domain)
		}
		svcs := p.LoadComposeServices(ctx, a.ID)
		out = append(out, appItem{
			ID:        a.ID,
			Name:      a.Name,
			Status:    a.Status,
			CreatedAt: a.CreatedAt.Format(time.RFC3339),
			Domains:   dNames,
			Services:  svcs,
		})
	}
	return c.JSON(out)
}

// APIAppDelete deletes an application and all its Docker resources via API token.
// DELETE /api/v1/apps/:id
func (p *Panel) APIAppDelete(c *fiber.Ctx) error {
	ctx := c.UserContext()
	u, ok := currentUser(c)
	if !ok {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}

	appID := strings.TrimSpace(c.Params("id"))
	app, err := p.DB.GetApp(ctx, appID)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "app not found"})
	}

	if u.Role != db.RoleAdmin && app.OwnerID != u.ID {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "only app owner or admin can delete this app"})
	}

	if tok, ok := c.Locals("api_token").(db.APIToken); ok {
		if !tok.AllowAppDelete {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"error": "permission denied: app deletion is restricted. Enable 'Allow App Delete' for this API token in NextDeploy Panel under CLI Sessions (/cli-sessions) or MCP Settings (/mcp-docs)",
			})
		}
	}

	delCtx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()

	if err := p.DeleteAppResources(delCtx, appID); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	p.RecordAuditLog(c, "delete_app", "app", appID, "Deleted app via CLI/API: "+app.Name)
	return c.JSON(fiber.Map{"ok": true, "message": fmt.Sprintf("Application %s deleted successfully", appID)})
}

// APIManifest returns workspace file manifest for a given app.
// GET /api/v1/apps/:id/manifest?hash=true&full_hash=false&include_locks=true&depth=0
func (p *Panel) APIManifest(c *fiber.Ctx) error {
	ctx := c.UserContext()
	u, ok := currentUser(c)
	if !ok {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}

	appID := strings.TrimSpace(c.Params("id"))
	allowed, err := p.CanAccessApp(ctx, u.ID, u.Role, appID, db.CollabRoleViewer)
	if err != nil || !allowed {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "forbidden"})
	}

	computeHash := c.QueryBool("hash", true)
	fullHash := c.QueryBool("full_hash", false)
	// CLI sync requires lockfiles by default to maintain dependency consistency and avoid redundant re-uploads
	includeLocks := c.QueryBool("include_locks", true)
	depth := c.QueryInt("depth", 0)
	relPath := c.Query("path", "")

	wsRoot := p.Store.Path(appID)
	res, err := wsmanifest.BuildWorkspaceManifest(wsRoot, appID, relPath, depth, computeHash, includeLocks, fullHash, nil, 5000)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": fmt.Sprintf("manifest failed: %v", err)})
	}
	return c.JSON(res)
}

// ─── CLI Sessions ─────────────────────────────────────────────────────────────

// APICliHeartbeat registers or refreshes a CLI session.
// POST /api/v1/cli/sessions
func (p *Panel) APICliHeartbeat(c *fiber.Ctx) error {
	u, ok := currentUser(c)
	if !ok {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}

	var req struct {
		ID          string `json:"id"`
		Hostname    string `json:"hostname"`
		OS          string `json:"os"`
		Arch        string `json:"arch"`
		Version     string `json:"version"`
		TokenName   string `json:"token_name"`
		TokenPrefix string `json:"token_prefix"`
	}
	if err := c.BodyParser(&req); err != nil || strings.TrimSpace(req.ID) == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "id is required"})
	}

	tokenName := strings.TrimSpace(req.TokenName)
	tokenPrefix := strings.TrimSpace(req.TokenPrefix)
	if tok, ok := c.Locals("api_token").(db.APIToken); ok && tok.ID > 0 {
		if tokenName == "" {
			tokenName = tok.Name
		}
		if tokenPrefix == "" {
			tokenPrefix = tok.TokenPrefix
		}
	}

	now := time.Now()
	sess := db.CLISession{
		ID:          req.ID,
		UserID:      u.ID,
		Hostname:    req.Hostname,
		OS:          req.OS,
		Arch:        req.Arch,
		Version:     req.Version,
		TokenName:   tokenName,
		TokenPrefix: tokenPrefix,
		LastSeen:    now,
		CreatedAt:   now,
	}
	if err := p.DB.UpsertCLISession(sess); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{
		"ok":           true,
		"user":         u.Username,
		"role":         u.Role,
		"token_name":   tokenName,
		"token_prefix": tokenPrefix,
	})
}

// APICliWhoami returns authenticated user and token metadata for nd whoami.
// GET /api/v1/cli/whoami
func (p *Panel) APICliWhoami(c *fiber.Ctx) error {
	u, ok := currentUser(c)
	if !ok {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}

	res := fiber.Map{
		"ok":       true,
		"user_id":  u.ID,
		"username": u.Username,
		"role":     u.Role,
	}
	if tok, ok := c.Locals("api_token").(db.APIToken); ok && tok.ID > 0 {
		res["token_name"] = tok.Name
		res["token_prefix"] = tok.TokenPrefix
		res["allow_env_reveal"] = tok.AllowEnvReveal
		res["allow_server_exec"] = tok.AllowServerExec
		res["allow_container_exec"] = tok.AllowContainerExec
		if tok.CreatedAt.Unix() > 0 {
			res["created_at"] = tok.CreatedAt.Format("2006-01-02 15:04")
		}
	}
	return c.JSON(res)
}

// APICliSessionDelete removes a CLI session on logout/exit.
// DELETE /api/v1/cli/sessions/:id
func (p *Panel) APICliSessionDelete(c *fiber.Ctx) error {
	sessionID := strings.TrimSpace(c.Params("id"))
	if sessionID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "session id required"})
	}
	_ = p.DB.DeleteCLISession(sessionID)
	return c.JSON(fiber.Map{"ok": true})
}

// APICliSessionsList returns all active CLI sessions for the dashboard.
// GET /api/v1/cli/sessions  (admin only)
func (p *Panel) APICliSessionsList(c *fiber.Ctx) error {
	u, ok := currentUser(c)
	if !ok {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}
	if u.Role != db.RoleAdmin {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "admin only"})
	}
	sessions, err := p.DB.ListCLISessions()
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(sessions)
}

// CLISessionsPage renders the CLI sessions dashboard page.
// GET /cli-sessions
func (p *Panel) CLISessionsPage(c *fiber.Ctx) error {
	ctx := c.UserContext()
	u, _ := currentUser(c)

	sessions, err := p.DB.ListCLISessions()
	if err != nil {
		sessions = nil
	}

	var tokens []db.APIToken
	if u.ID > 0 {
		tokens, _ = p.DB.ListAPITokensForUser(ctx, u.ID)
	}

	proto := "http"
	if c.Protocol() == "https" || c.Get("X-Forwarded-Proto") == "https" {
		proto = "https"
	}
	baseURL := fmt.Sprintf("%s://%s", proto, c.Hostname())

	return c.Render("pages/cli_sessions", withUser(c, fiber.Map{
		"Nav":          "cli-sessions",
		"Title":        "CLI Sessions",
		"Sessions":     sessions,
		"Tokens":       tokens,
		"NewToken":     c.Query("new_token"),
		"NewTokenName": c.Query("token_name"),
		"BaseURL":      baseURL,
	}), "layouts/shell")
}

// CreateCLITokenPost generates a new API token for CLI usage directly from /cli-sessions.
// POST /cli-sessions/tokens
func (p *Panel) CreateCLITokenPost(c *fiber.Ctx) error {
	ctx := c.UserContext()
	u, ok := currentUser(c)
	if !ok {
		return c.Status(fiber.StatusUnauthorized).SendString("Unauthorized")
	}

	name := strings.TrimSpace(c.FormValue("name"))
	if name == "" {
		name = "CLI Device Token"
	}

	allowEnvReveal := c.FormValue("allow_env_reveal") == "1" || c.FormValue("allow_env_reveal") == "on"
	allowServerExec := c.FormValue("allow_server_exec") == "1" || c.FormValue("allow_server_exec") == "on"
	allowContainerExec := c.FormValue("allow_container_exec") == "1" || c.FormValue("allow_container_exec") == "on"
	allowAppDelete := c.FormValue("allow_app_delete") == "1" || c.FormValue("allow_app_delete") == "on"

	kind := strings.TrimSpace(c.FormValue("kind"))
	if kind != "cli" && kind != "mcp" {
		kind = "cli"
	}

	rawToken, _, err := p.DB.CreateAPIToken(ctx, u.ID, name, kind, nil, allowEnvReveal, allowServerExec, allowContainerExec, allowAppDelete)
	if err != nil {
		return c.Redirect("/cli-sessions?error=" + url.QueryEscape(err.Error()))
	}

	p.RecordAuditLog(c, "create_cli_token", "api_token", name, "Created API token from CLI Sessions page")
	return c.Redirect(fmt.Sprintf("/cli-sessions?new_token=%s&token_name=%s", rawToken, url.QueryEscape(name)))
}

// ToggleCLITokenEnvRevealPost toggles env_reveal permission for an API token.
// POST /cli-sessions/tokens/:id/toggle-reveal
func (p *Panel) ToggleCLITokenEnvRevealPost(c *fiber.Ctx) error {
	ctx := c.UserContext()
	u, ok := currentUser(c)
	if !ok {
		return c.Status(fiber.StatusUnauthorized).SendString("Unauthorized")
	}
	id, err := c.ParamsInt("id")
	if err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("invalid token id")
	}
	_, _ = p.DB.ToggleAPITokenEnvReveal(ctx, int64(id), u.ID)
	return c.Redirect("/cli-sessions")
}

// ToggleCLITokenContainerExecPost toggles container_exec permission for an API token.
// POST /cli-sessions/tokens/:id/toggle-container-exec
func (p *Panel) ToggleCLITokenContainerExecPost(c *fiber.Ctx) error {
	ctx := c.UserContext()
	u, ok := currentUser(c)
	if !ok {
		return c.Status(fiber.StatusUnauthorized).SendString("Unauthorized")
	}
	id, err := c.ParamsInt("id")
	if err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("invalid token id")
	}
	_, _ = p.DB.ToggleAPITokenContainerExec(ctx, int64(id), u.ID)
	return c.Redirect("/cli-sessions")
}

// ToggleCLITokenServerExecPost toggles server_exec permission for an API token.
// POST /cli-sessions/tokens/:id/toggle-exec
func (p *Panel) ToggleCLITokenServerExecPost(c *fiber.Ctx) error {
	ctx := c.UserContext()
	u, ok := currentUser(c)
	if !ok {
		return c.Status(fiber.StatusUnauthorized).SendString("Unauthorized")
	}
	id, err := c.ParamsInt("id")
	if err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("invalid token id")
	}
	_, _ = p.DB.ToggleAPITokenServerExec(ctx, int64(id), u.ID)
	return c.Redirect("/cli-sessions")
}

// ToggleCLITokenAppDeletePost toggles app_delete permission for an API token.
// POST /cli-sessions/tokens/:id/toggle-app-delete
func (p *Panel) ToggleCLITokenAppDeletePost(c *fiber.Ctx) error {
	ctx := c.UserContext()
	u, ok := currentUser(c)
	if !ok {
		return c.Status(fiber.StatusUnauthorized).SendString("Unauthorized")
	}
	id, err := c.ParamsInt("id")
	if err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("invalid token id")
	}
	enabled, err := p.DB.ToggleAPITokenAppDelete(ctx, int64(id), u.ID)
	if err != nil {
		return c.Redirect("/cli-sessions?error=" + url.QueryEscape(err.Error()))
	}
	stateStr := "disabled"
	if enabled {
		stateStr = "enabled"
	}
	p.RecordAuditLog(c, "toggle_cli_token_app_delete", "api_token", fmt.Sprintf("%d", id), "Toggled app_delete to "+stateStr)
	return c.Redirect("/cli-sessions")
}

// DeleteCLITokenPost revokes an API token from /cli-sessions.
// POST /cli-sessions/tokens/:id/delete
func (p *Panel) DeleteCLITokenPost(c *fiber.Ctx) error {
	ctx := c.UserContext()
	u, ok := currentUser(c)
	if !ok {
		return c.Status(fiber.StatusUnauthorized).SendString("Unauthorized")
	}

	id, err := c.ParamsInt("id")
	if err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("invalid token id")
	}

	_ = p.DB.DeleteAPIToken(ctx, int64(id), u.ID)
	p.RecordAuditLog(c, "delete_cli_token", "api_token", fmt.Sprintf("Token #%d", id), "Revoked API token from CLI Sessions page")
	return c.Redirect("/cli-sessions")
}
