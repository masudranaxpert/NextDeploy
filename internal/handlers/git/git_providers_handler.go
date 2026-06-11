package git

import (
	"panel/internal/handlers/utils"
	"strconv"
	"strings"

	"panel/internal/db"
	"panel/internal/handlers"

	"github.com/gofiber/fiber/v2"
)

// GitProvidersPage renders the global Git providers settings page.
func (h *Handler) GitProvidersPage(c *fiber.Ctx) error {
	providers, err := h.p.DB.ListGitProviders(c.UserContext())
	if err != nil {
		providers = nil
	}
	ghDetails, _ := h.p.DB.ListGitHubProviderDetails(c.UserContext())
	ghMap := map[int64]db.GitHubProviderDetail{}
	for _, d := range ghDetails {
		ghMap[d.ProviderID] = d
	}
	flash := utils.ReadFlash(c)
	saved := flash == "saved" || c.Query("saved") == "1"
	deleted := flash == "deleted" || c.Query("deleted") == "1"
	errMsg := utils.ReadFlashError(c)
	return c.Render("pages/git_providers", handlers.WithUser(c, fiber.Map{
		"Nav":                  "git",
		"Title":                "Git Providers",
		"Providers":            providers,
		"GitHubDetails":        ghMap,
		"Saved":                saved,
		"Deleted":              deleted,
		"Error":                errMsg,
		"GitHubAppCreateURL":   "https://github.com/settings/apps/new",
		"GitLabAppCreateURL":   "https://gitlab.com/-/user_settings/applications",
		"GitHubManifestStart":  "/git/github/start",
		"GitLabCallbackURL":    h.gitlabCallbackURL(c),
	}), "layouts/shell")
}

// GitProviderUpdate updates an existing global Git provider credential.
func (h *Handler) GitProviderUpdate(c *fiber.Ctx) error {
	idStr := c.Params("pid")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		utils.SetFlashError(c, "Invalid ID")
		return c.Redirect("/git")
	}

	existing, err := h.p.DB.GetGitProvider(c.UserContext(), id)
	if err != nil {
		utils.SetFlashError(c, "Provider not found")
		return c.Redirect("/git")
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

	if err := h.p.DB.UpdateGitProvider(c.UserContext(), id, name, provider, token, notes); err != nil {
		utils.SetFlashError(c, err.Error())
		return c.Redirect("/git")
	}
	utils.SetFlash(c, "saved")
	return c.Redirect("/git")
}

// GitProviderDelete deletes a global Git provider credential.
func (h *Handler) GitProviderDelete(c *fiber.Ctx) error {
	idStr := c.Params("pid")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		utils.SetFlashError(c, "Invalid ID")
		return c.Redirect("/git")
	}
	if err := h.p.DB.DeleteGitProvider(c.UserContext(), id); err != nil {
		utils.SetFlashError(c, err.Error())
		return c.Redirect("/git")
	}
	utils.SetFlash(c, "deleted")
	return c.Redirect("/git")
}

// AppSwitchSource switches an app between "files" and "git" source types.
func (h *Handler) AppSwitchSource(c *fiber.Ctx) error {
	id := c.Params("id")
	if _, err := h.p.DB.GetApp(c.UserContext(), id); err != nil {
		return c.Status(404).SendString("not found")
	}

	newSource := strings.TrimSpace(c.FormValue("source_type"))
	if newSource != "files" && newSource != "git" {
		return c.Redirect("/apps/" + id + "?tab=overview")
	}

	if newSource == "files" {
		_ = h.p.DB.DeleteAppGitConfig(c.UserContext(), id)
	}
	if newSource == "git" {
		if err := h.p.Store.ClearUploadedProjectForGitSource(id); err != nil {
			utils.SetFlash(c, "switchError_clear")
			return c.Redirect("/apps/" + id + "?tab=overview")
		}
	}

	if err := h.p.DB.SetAppSourceType(c.UserContext(), id, newSource); err != nil {
		utils.SetFlashError(c, "Failed to switch source type")
		return c.Redirect("/apps/" + id + "?tab=overview")
	}

	utils.SetFlash(c, "sourceSwitched")
	if newSource == "git" {
		return c.Redirect("/apps/" + id + "?tab=git")
	}
	return c.Redirect("/apps/" + id + "?tab=files")
}
