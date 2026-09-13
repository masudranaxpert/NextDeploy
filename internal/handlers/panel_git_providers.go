package handlers

import (
	"panel/internal/handlers/utils"
	"strconv"
	"strings"

	"panel/internal/db"

	"github.com/gofiber/fiber/v2"
)

// GitProvidersPage renders the global Git providers settings page.
func (p *Panel) GitProvidersPage(c *fiber.Ctx) error {
	u, ok := CurrentUser(c)
	if !ok {
		return c.Redirect("/login")
	}
	var userID *int64
	if u.Role != db.RoleAdmin {
		val := u.ID
		userID = &val
	}
	providers, err := p.DB.ListGitProviders(c.UserContext(), userID)
	if err != nil {
		providers = nil
	}
	ghDetails, _ := p.DB.ListGitHubProviderDetails(c.UserContext())
	ghMap := map[int64]db.GitHubProviderDetail{}
	for _, d := range ghDetails {
		ghMap[d.ProviderID] = d
	}
	flash := utils.ReadFlash(c)
	saved := flash == "saved" || c.Query("saved") == "1"
	deleted := flash == "deleted" || c.Query("deleted") == "1"
	errMsg := utils.ReadFlashError(c)
	return c.Render("pages/git_providers", withUser(c, fiber.Map{
		"Nav":                 "git",
		"Title":               "Git Providers",
		"Providers":           providers,
		"GitHubDetails":       ghMap,
		"Saved":               saved,
		"Deleted":             deleted,
		"Error":               errMsg,
		"GitHubAppCreateURL":  "https://github.com/settings/apps/new",
		"GitLabAppCreateURL":  "https://gitlab.com/-/user_settings/applications",
		"GitHubManifestStart": "/git/github/start",
		"GitLabCallbackURL":   p.gitlabCallbackURL(c),
	}), "layouts/shell")
}

// GitProviderUpdate updates an existing global Git provider credential.
func (p *Panel) GitProviderUpdate(c *fiber.Ctx) error {
	u, ok := CurrentUser(c)
	if !ok {
		return c.Redirect("/login")
	}
	idStr := c.Params("pid")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		utils.SetFlashError(c, "Invalid ID")
		return c.Redirect("/git")
	}

	existing, err := p.DB.GetGitProvider(c.UserContext(), id)
	if err != nil {
		utils.SetFlashError(c, "Provider not found")
		return c.Redirect("/git")
	}

	if u.Role != db.RoleAdmin && (existing.UserID == nil || *existing.UserID != u.ID) {
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
		utils.SetFlashError(c, err.Error())
		return c.Redirect("/git")
	}
	utils.SetFlash(c, "saved")
	return c.Redirect("/git")
}

// GitProviderDelete deletes a global Git provider credential.
func (p *Panel) GitProviderDelete(c *fiber.Ctx) error {
	u, ok := CurrentUser(c)
	if !ok {
		return c.Redirect("/login")
	}
	idStr := c.Params("pid")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		utils.SetFlashError(c, "Invalid ID")
		return c.Redirect("/git")
	}
	existing, err := p.DB.GetGitProvider(c.UserContext(), id)
	if err != nil {
		utils.SetFlashError(c, "Provider not found")
		return c.Redirect("/git")
	}
	if u.Role != db.RoleAdmin && (existing.UserID == nil || *existing.UserID != u.ID) {
		return c.Status(fiber.StatusForbidden).SendString("forbidden")
	}
	if err := p.DB.DeleteGitProvider(c.UserContext(), id); err != nil {
		utils.SetFlashError(c, err.Error())
		return c.Redirect("/git")
	}
	utils.SetFlash(c, "deleted")
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
		return c.Redirect("/apps/" + id + "?tab=overview")
	}

	if newSource == "files" {
		_ = p.DB.DeleteAppGitConfig(c.UserContext(), id)
	}
	if newSource == "git" {
		if err := p.Store.ClearUploadedProjectForGitSource(id); err != nil {
			utils.SetFlash(c, "switchError_clear")
			return c.Redirect("/apps/" + id + "?tab=overview")
		}
	}

	if err := p.DB.SetAppSourceType(c.UserContext(), id, newSource); err != nil {
		utils.SetFlashError(c, "Failed to switch source type")
		return c.Redirect("/apps/" + id + "?tab=overview")
	}

	utils.SetFlash(c, "sourceSwitched")
	if newSource == "git" {
		return c.Redirect("/apps/" + id + "?tab=git")
	}
	return c.Redirect("/apps/" + id + "?tab=files")
}
