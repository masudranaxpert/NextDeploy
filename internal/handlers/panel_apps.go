package handlers

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"panel/internal/db"
	"panel/internal/dockerx"
	"panel/internal/workspace"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

func (p *Panel) AppsPage(c *fiber.Ctx) error {
	list, err := p.DB.ListApps(c.UserContext())
	if err != nil {
		return c.Status(500).SendString(err.Error())
	}
	type appListItem struct {
		db.App
		State           string
		RunningCount    int
		ExitedCount     int
		ContainerCount  int
	}
	items := make([]appListItem, 0, len(list))
	for _, app := range list {
		project := p.activeComposeProjectName(c.UserContext(), app, app.ID)
		rows, _ := dockerx.ComposePS(c.UserContext(), p.appSourcePath(c.UserContext(), app.ID), p.effectiveComposePaths(c.UserContext(), app, app.ID), project, p.composeEnvFiles(c.UserContext(), app.ID))
		item := appListItem{App: app, State: "not deployed"}
		for _, row := range rows {
			item.ContainerCount++
			state := strings.ToLower(strings.TrimSpace(row.State))
			status := strings.ToLower(strings.TrimSpace(row.Status))
			switch state {
			case "running":
				item.RunningCount++
			case "exited":
				// exited(0) = completed successfully (migrate, init containers) — treat as ok
				if strings.Contains(status, "exited (0)") {
					item.RunningCount++
				} else {
					item.ExitedCount++
				}
			case "dead":
				item.ExitedCount++
			}
		}
		if item.ContainerCount > 0 {
			switch {
			case item.RunningCount == item.ContainerCount:
				item.State = "running"
			case item.RunningCount > 0:
				item.State = "degraded"
			case item.ExitedCount > 0:
				item.State = "failed"
			default:
				item.State = "stopped"
			}
		}
		items = append(items, item)
	}
	return c.Render("pages/apps", withUser(c, fiber.Map{
		"Nav":   "apps",
		"Title": "Apps",
		"Apps":  items,
	}), "layouts/shell")
}

func (p *Panel) CreateApp(c *fiber.Ctx) error {
	name := strings.TrimSpace(c.FormValue("name"))
	if name == "" {
		return c.Status(400).SendString("name required")
	}
	if sanitizeProjectName(name) == "" {
		return c.Status(400).SendString("use lowercase letters, numbers, spaces, hyphens, or underscores")
	}
	id := uuid.NewString()
	if err := os.MkdirAll(p.Store.Path(id), 0750); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	if err := p.Store.WriteMeta(id, name); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	if err := p.DB.CreateApp(c.UserContext(), id, name); err != nil {
		_ = os.RemoveAll(p.Store.Path(id))
		if strings.Contains(strings.ToLower(err.Error()), "already exists") {
			return c.Status(400).SendString(err.Error())
		}
		return c.Status(500).SendString(err.Error())
	}
	sourceType := strings.TrimSpace(c.FormValue("source_type"))
	if sourceType == "github" || sourceType == "git" {
		_ = p.DB.SetAppSourceType(c.UserContext(), id, "git")
		repoURL := strings.TrimSpace(c.FormValue("repo_url"))
		if repoURL != "" {
			cfg := db.AppGitConfig{
				AppID:         id,
				Provider:      "github",
				RepoURL:       normalizeRepoURL(repoURL),
				RepoFullName:  repoFullNameFromURL(repoURL),
				Branch:        normalizeBranch(c.FormValue("branch")),
				AuthMode:      strings.TrimSpace(c.FormValue("auth_mode")),
				Token:         strings.TrimSpace(c.FormValue("token")),
				WebhookSecret: randomSecret(),
				AutoDeploy:    true,
			}
			if cfg.AuthMode == "" {
				cfg.AuthMode = "public"
			}
			if err := p.DB.UpsertAppGitConfig(c.UserContext(), cfg); err != nil {
				return c.Status(500).SendString(err.Error())
			}
			if err := os.MkdirAll(filepath.Join(p.Store.ReservedPath(id), "repo"), 0750); err != nil {
				return c.Status(500).SendString(err.Error())
			}
		}
	}
	return c.Redirect(fmt.Sprintf("/apps/%s", id))
}

func (p *Panel) SaveAppCompose(c *fiber.Ctx) error {
	id := c.Params("id")
	if _, err := p.DB.GetApp(c.UserContext(), id); err != nil {
		return c.Status(404).SendString("app not found")
	}
	raw := workspace.NormalizeComposeRel(c.FormValue("compose_file"))
	if err := p.DB.UpdateComposeFile(c.UserContext(), id, raw); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	app, err := p.DB.GetApp(c.UserContext(), id)
	if err != nil {
		return c.Status(500).SendString(err.Error())
	}
	if err := p.syncAppCaddyOverride(c, id); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	if strings.EqualFold(c.Get("HX-Request"), "true") {
		return p.renderComposeFileCard(c, app, id, true)
	}
	return c.Redirect(fmt.Sprintf("/apps/%s?tab=overview", id))
}

func (p *Panel) renderComposeFileCard(c *fiber.Ctx, app db.App, id string, saved bool) error {
	composePath := p.composeFilePath(app, id)
	composeDisplay := workspace.NormalizeComposeRel(app.ComposeFile)
	hasComp := false
	if st, err := os.Stat(composePath); err == nil && !st.IsDir() {
		hasComp = true
	}
	return c.Render(tmplPartialComposeFileCard, fiber.Map{
		"ID":                 id,
		"ComposeFileSetting": composeDisplay,
		"HasCompose":         hasComp,
		"ComposePathSaved":   saved,
	})
}

func (p *Panel) SaveAppEnv(c *fiber.Ctx) error {
	id := c.Params("id")
	if _, err := p.DB.GetApp(c.UserContext(), id); err != nil {
		return c.Status(404).SendString("app not found")
	}
	content := c.FormValue("env")
	if err := p.DB.UpdatePanelEnv(c.UserContext(), id, content); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	if err := p.syncPanelEnvFileToDisk(id); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	return c.Redirect(fmt.Sprintf("/apps/%s?tab=environment", id))
}
