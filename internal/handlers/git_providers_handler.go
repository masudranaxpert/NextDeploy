package handlers

import (
	"strconv"
	"strings"

	"panel/internal/db"

	"github.com/gofiber/fiber/v2"
)

// GitProvidersPage renders the global Git providers settings page.
func (p *Panel) GitProvidersPage(c *fiber.Ctx) error {
	providers, err := p.DB.ListGitProviders(c.UserContext())
	if err != nil {
		providers = nil
	}
	ghDetails, _ := p.DB.ListGitHubProviderDetails(c.UserContext())
	ghMap := map[int64]db.GitHubProviderDetail{}
	for _, d := range ghDetails {
		ghMap[d.ProviderID] = d
	}
	return c.Render("pages/git_providers", withUser(c, fiber.Map{
		"Nav":                  "git",
		"Title":                "Git Providers",
		"Providers":            providers,
		"GitHubDetails":        ghMap,
		"Saved":                c.Query("saved") == "1",
		"Deleted":              c.Query("deleted") == "1",
		"Error":                c.Query("error"),
		"GitHubAppCreateURL":   "https://github.com/settings/apps/new",
		"GitLabAppCreateURL":   "https://gitlab.com/-/profile/applications",
		"GitHubManifestStart":  "/git/github/start",
	}), "layouts/shell")
}

// GitProviderCreate creates a new global Git provider credential.
func (p *Panel) GitProviderCreate(c *fiber.Ctx) error {
	name := strings.TrimSpace(c.FormValue("name"))
	provider := strings.TrimSpace(c.FormValue("provider"))
	token := strings.TrimSpace(c.FormValue("token"))
	notes := strings.TrimSpace(c.FormValue("notes"))

	if name == "" {
		return c.Redirect("/git?error=Name+is+required")
	}
	if provider == "" {
		provider = "github"
	}
	switch provider {
	case "github", "gitlab":
	default:
		provider = "github"
	}

	if _, err := p.DB.CreateGitProvider(c.UserContext(), name, provider, token, notes); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return c.Redirect("/git?error=A+provider+with+that+name+already+exists")
		}
		return c.Redirect("/git?error=" + err.Error())
	}
	return c.Redirect("/git?saved=1")
}

// GitProviderUpdate updates an existing global Git provider credential.
func (p *Panel) GitProviderUpdate(c *fiber.Ctx) error {
	idStr := c.Params("pid")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return c.Redirect("/git?error=Invalid+ID")
	}

	existing, err := p.DB.GetGitProvider(c.UserContext(), id)
	if err != nil {
		return c.Redirect("/git?error=Provider+not+found")
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
	// Keep existing token if new one is empty
	if token == "" {
		token = existing.Token
	}

	if err := p.DB.UpdateGitProvider(c.UserContext(), id, name, provider, token, notes); err != nil {
		return c.Redirect("/git?error=" + err.Error())
	}
	return c.Redirect("/git?saved=1")
}

// GitProviderDelete deletes a global Git provider credential.
func (p *Panel) GitProviderDelete(c *fiber.Ctx) error {
	idStr := c.Params("pid")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return c.Redirect("/git?error=Invalid+ID")
	}
	if err := p.DB.DeleteGitProvider(c.UserContext(), id); err != nil {
		return c.Redirect("/git?error=" + err.Error())
	}
	return c.Redirect("/git?deleted=1")
}

// AppSwitchSource switches an app between "files" and "git" source types.
// When switching to "files", it removes the git config.
// When switching to "git", it sets source_type to "git".
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
		// Remove git config when switching to files
		_ = p.DB.DeleteAppGitConfig(c.UserContext(), id)
	}
	if newSource == "git" {
		// Before flipping DB to git, clear uploaded workspace so stale files are not left behind.
		if err := p.Store.ClearUploadedProjectForGitSource(id); err != nil {
			return c.Redirect("/apps/" + id + "?tab=overview&switchError=clear")
		}
	}

	if err := p.DB.SetAppSourceType(c.UserContext(), id, newSource); err != nil {
		return c.Redirect("/apps/" + id + "?tab=overview&switchError=1")
	}

	if newSource == "git" {
		return c.Redirect("/apps/" + id + "?tab=git&sourceSwitched=1")
	}
	return c.Redirect("/apps/" + id + "?tab=files&sourceSwitched=1")
}
