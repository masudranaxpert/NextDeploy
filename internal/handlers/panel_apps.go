package handlers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"panel/internal/db"
	"panel/internal/workspace"

	"github.com/gofiber/fiber/v2"
	"golang.org/x/sync/errgroup"
)

// appListItem holds per-app state for the apps list page.
type appListItem struct {
	db.App
	State            string
	RunningCount     int
	ExitedCount      int
	PausedCount      int
	ContainerCount   int
}

const (
	// appsListOverallTimeout caps the entire /apps request (batch DB + all compose probes).
	appsListOverallTimeout = 90 * time.Second
	// appsListComposePerAppTimeout bounds docker compose ps work per app so one hung daemon
	// cannot block siblings when the list is loaded concurrently.
	appsListComposePerAppTimeout = 10 * time.Second
)

// fetchAppStateForAppsList resolves container state for one app. Safe for concurrent use:
// each goroutine writes only to items[i] on the list page. hint comes from BatchAppComposeHints
// so we avoid N+1 DB queries. Docker state uses composeProjectAndPSHint, which runs at most
// one compose ps when project slug equals app ID, and at most two when a legacy ID fallback exists.
func (p *Panel) fetchAppStateForAppsList(ctx context.Context, app db.App, hint db.AppComposeHint) appListItem {
	item := appListItem{App: app, State: "not deployed"}

	tctx, cancel := context.WithTimeout(ctx, appsListComposePerAppTimeout)
	defer cancel()

	_, rows, res := p.composeProjectAndPSHint(tctx, app, app.ID, hint)
	if !res.OK {
		return item
	}

	for _, row := range rows {
		item.ContainerCount++
		state := strings.ToLower(strings.TrimSpace(row.State))
		status := strings.ToLower(strings.TrimSpace(row.Status))
		switch state {
		case "running":
			item.RunningCount++
		case "paused":
			item.PausedCount++
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
		case item.PausedCount == item.ContainerCount && item.RunningCount == 0 && item.ExitedCount == 0:
			item.State = "paused"
		case item.RunningCount == item.ContainerCount:
			item.State = "running"
		case item.RunningCount > 0 || item.PausedCount > 0:
			item.State = "degraded"
		case item.ExitedCount > 0:
			item.State = "failed"
		default:
			item.State = "stopped"
		}
	}
	return item
}

// AppsPage renders the /apps list. Compose probes run concurrently (errgroup); each app has its
// own deadline under appsListComposePerAppTimeout. errgroup is used without WithContext so one
// failing/slow app does not cancel others; goroutines always return nil and errors show as
// "not deployed" via fetchAppStateForAppsList.
func (p *Panel) AppsPage(c *fiber.Ctx) error {
	list, err := p.DB.ListApps(c.UserContext())
	if err != nil {
		return c.Status(500).SendString(err.Error())
	}

	listCtx, cancel := context.WithTimeout(c.UserContext(), appsListOverallTimeout)
	defer cancel()

	ids := make([]string, 0, len(list))
	for _, a := range list {
		ids = append(ids, a.ID)
	}
	hints, err := p.DB.BatchAppComposeHints(listCtx, ids)
	if err != nil {
		return c.Status(500).SendString(err.Error())
	}

	items := make([]appListItem, len(list))
	var eg errgroup.Group

	for i, app := range list {
		i, app := i, app
		hint := hints[app.ID]
		eg.Go(func() error {
			items[i] = p.fetchAppStateForAppsList(listCtx, app, hint)
			return nil
		})
	}

	_ = eg.Wait()

	return c.Render("pages/apps", withUser(c, fiber.Map{
		"Nav":   "apps",
		"Title": "Apps",
		"Apps":  items,
	}), "layouts/shell")
}

func (p *Panel) CreateApp(c *fiber.Ctx) error {
	ctx := c.UserContext()
	slug, err := validateAppSlug(c.FormValue("name"))
	if err != nil {
		return c.Status(400).SendString(err.Error())
	}
	if _, err := p.DB.GetApp(ctx, slug); err == nil {
		return c.Status(400).SendString("an app with this name already exists")
	} else if !errors.Is(err, sql.ErrNoRows) {
		return c.Status(500).SendString(err.Error())
	}
	id := slug
	name := slug
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
	appRow := db.App{ID: id, Name: name, ComposeFile: "docker-compose.yml"}
	if err := p.seedComposeProjectNameInPanelEnv(c.UserContext(), id, appRow); err != nil {
		log.Printf("seed COMPOSE_PROJECT_NAME for app %s: %v", id, err)
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
		return respondAppNotFound(c)
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
	composePath := p.composeFilePath(c.UserContext(), app, id)
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
		return respondAppNotFound(c)
	}
	content := c.FormValue("env")
	if err := p.DB.UpdatePanelEnv(c.UserContext(), id, content); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	root := p.composeWorkspaceRoot(c.UserContext(), id)
	if err := p.syncWorkspaceEnvFromPanel(id, root, content); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	_ = p.syncAppCaddyOverride(c, id)
	return c.Redirect(fmt.Sprintf("/apps/%s?tab=environment", id))
}
