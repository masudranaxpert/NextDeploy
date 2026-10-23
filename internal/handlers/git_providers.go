package handlers

import (
	"strconv"
	"strings"

	"panel/internal/db"

	"github.com/gofiber/fiber/v2"
)

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
		} else {
			notes = "Deploy Token"
		}
	}

	var userID *int64
	if u.Role != db.RoleAdmin {
		uid := u.ID
		userID = &uid
	}

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

// GitProvidersList returns all git providers for the current user or admin.
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
	return c.JSON(fiber.Map{
		"ok":        true,
		"providers": providers,
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
	_, ok := CurrentUser(c)
	if !ok {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}
	limit := 10
	if l := c.QueryInt("limit", 10); l > 0 && l <= 50 {
		limit = l
	}
	list, err := p.DB.ListWebhookDeliveries(c.UserContext(), limit)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	if list == nil {
		list = []db.WebhookDelivery{}
	}
	return c.JSON(list)
}
