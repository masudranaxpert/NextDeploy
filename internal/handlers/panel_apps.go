package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"panel/internal/handlers/utils"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"panel/internal/db"
	"panel/internal/dockerapi"
	"panel/internal/dockerx"
	"panel/internal/workspace"

	"github.com/gofiber/fiber/v2"
)

type appListItem struct {
	db.App
	State          string
	RunningCount   int
	ExitedCount    int
	PausedCount    int
	ContainerCount int
	OwnerName      string
	OwnerIsAdmin   bool
}

const appsListOverallTimeout = 10 * time.Second

type composeContainerIndex map[string][]dockerx.ComposePsRow

func buildComposeContainerIndex(containers []dockerapi.ComposeContainerRow) composeContainerIndex {
	idx := make(composeContainerIndex)
	for _, c := range containers {
		if c.Project == "" {
			continue
		}
		idx[c.Project] = append(idx[c.Project], dockerx.ComposePsRow{
			Name:       c.Name,
			State:      c.State,
			Status:     c.Status,
			WorkingDir: c.WorkingDir,
		})
	}
	return idx
}

func appListItemFromRows(app db.App, rows []dockerx.ComposePsRow) appListItem {
	item := appListItem{App: app, State: "not deployed"}
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

func (p *Panel) appListItemFromIndex(app db.App, index composeContainerIndex) appListItem {
	item := appListItem{App: app, State: "not deployed"}
	for _, proj := range p.legacyProjectNames(app, app.ID) {
		rows := index[proj]
		if len(rows) == 0 {
			continue
		}
		if !p.composeRowsBelongToApp(app.ID, rows) {
			continue
		}
		return appListItemFromRows(app, rows)
	}
	return item
}

func (p *Panel) AppsPage(c *fiber.Ctx) error {
	u, ok := currentUser(c)
	if !ok {
		return c.Redirect("/login")
	}
	var list []db.App
	var err error
	if u.Role == db.RoleAdmin {
		list, err = p.DB.ListApps(c.UserContext())
	} else {
		list, err = p.DB.ListAppsForUser(c.UserContext(), u.ID)
	}
	if err != nil {
		return c.Status(500).SendString(err.Error())
	}

	listCtx, cancel := context.WithTimeout(c.UserContext(), appsListOverallTimeout)
	defer cancel()

	containers, _ := dockerapi.ListComposeContainers(listCtx)
	index := buildComposeContainerIndex(containers)

	items := make([]appListItem, len(list))
	for i, app := range list {
		items[i] = p.appListItemFromIndex(app, index)
	}

	isAdmin := u.Role == db.RoleAdmin
	if isAdmin {
		if users, uerr := p.DB.ListUsers(listCtx); uerr == nil {
			type ownerInfo struct {
				name    string
				isAdmin bool
			}
			byID := make(map[int64]ownerInfo, len(users))
			for _, usr := range users {
				byID[usr.ID] = ownerInfo{name: usr.Username, isAdmin: usr.Role == db.RoleAdmin}
			}
			for i := range items {
				if info, ok := byID[items[i].OwnerID]; ok {
					items[i].OwnerName = info.name
					items[i].OwnerIsAdmin = info.isAdmin
				}
			}
		}
	}

	return c.Render("pages/apps", withUser(c, fiber.Map{
		"Nav":     "apps",
		"Title":   "Apps",
		"Apps":    items,
		"IsAdmin": isAdmin,
	}), "layouts/shell")
}

func randomAppSuffix() string {
	buf := make([]byte, 2)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%04x", time.Now().UnixNano()%65536)
	}
	return hex.EncodeToString(buf)
}

func (p *Panel) CreateApp(c *fiber.Ctx) error {
	ctx := c.UserContext()
	u, ok := currentUser(c)
	if !ok {
		return c.Status(401).SendString("unauthorized")
	}
	slug, err := validateAppSlug(c.FormValue("name"))
	if err != nil {
		return c.Status(400).SendString(err.Error())
	}
	exists, err := p.DB.AppNameExistsForUser(ctx, slug, u.ID)
	if err != nil {
		return c.Status(500).SendString(err.Error())
	}
	if exists {
		return c.Status(400).SendString("an app with this name already exists")
	}
	if u.Role != db.RoleAdmin {
		count, err := p.DB.CountAppsOwnedByUser(ctx, u.ID)
		if err == nil && u.MaxApps > 0 && count >= u.MaxApps {
			return c.Status(400).SendString("maximum app limit reached")
		}
	}
	var id string
	for {
		suffix := randomAppSuffix()
		id = fmt.Sprintf("%s-%s", slug, suffix)
		if _, err := p.DB.GetApp(ctx, id); errors.Is(err, sql.ErrNoRows) {
			break
		}
	}
	name := slug
	if err := os.MkdirAll(p.Store.Path(id), 0750); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	if err := p.Store.WriteMeta(id, name); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	if err := p.DB.CreateApp(ctx, id, name, u.ID); err != nil {
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
				RepoURL:       utils.NormalizeRepoURL(repoURL),
				RepoFullName:  utils.RepoFullNameFromURL(repoURL),
				Branch:        utils.NormalizeBranch(c.FormValue("branch")),
				AuthMode:      strings.TrimSpace(c.FormValue("auth_mode")),
				Token:         strings.TrimSpace(c.FormValue("token")),
				WebhookSecret: utils.RandomSecret(),
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
	p.RecordAuditLog(c, "create_app", "app", id, "Created app: "+name+" with source: "+sourceType)
	return c.Redirect(fmt.Sprintf("/apps/%s", id))
}

func (p *Panel) SaveAppCompose(c *fiber.Ctx) error {
	id := c.Params("id")
	if _, err := p.DB.GetApp(c.UserContext(), id); err != nil {
		return utils.RespondAppNotFound(c)
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
	hasDF, _ := p.Store.HasDockerArtifacts(id)
	return c.Render(utils.TmplPartialComposeFileCard, fiber.Map{
		"ID":                 id,
		"ComposeFileSetting": composeDisplay,
		"HasCompose":         hasComp,
		"HasDockerfile":      hasDF,
		"ComposePathSaved":   saved,
	})
}

func (p *Panel) SaveAppEnv(c *fiber.Ctx) error {
	id := c.Params("id")
	if _, err := p.DB.GetApp(c.UserContext(), id); err != nil {
		return utils.RespondAppNotFound(c)
	}
	content := c.FormValue("env")
	if err := p.DB.UpdatePanelEnv(c.UserContext(), id, content); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	root := p.composeWorkspaceRoot(c.UserContext(), id)
	if err := p.SyncWorkspaceEnvFromPanel(id, root, content); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	_ = p.syncAppCaddyOverride(c, id)
	return c.Redirect(fmt.Sprintf("/apps/%s?tab=environment", id))
}
