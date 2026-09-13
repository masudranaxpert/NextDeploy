package handlers

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"panel/internal/handlers/utils"
	"path/filepath"
	"strings"
	"time"

	"panel/internal/db"
	"panel/internal/dev"
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

// SaveAppDevMode stores the development mode settings and regenerates the compose
// override so the workspace bind mount is added or removed right away.
func (p *Panel) SaveAppDevMode(c *fiber.Ctx) error {
	id := c.Params("id")
	if _, err := p.DB.GetApp(c.UserContext(), id); err != nil {
		return utils.RespondAppNotFound(c)
	}
	enabled := c.FormValue("dev_mode") == "on"
	service := strings.TrimSpace(c.FormValue("dev_service"))
	target := strings.TrimSpace(c.FormValue("dev_target"))
	command := strings.TrimSpace(c.FormValue("dev_command"))
	if target != "" && !dev.ValidTarget(target) {
		utils.SetFlash(c, "devTargetInvalid")
		return c.Redirect(fmt.Sprintf("/apps/%s?tab=dev", id))
	}
	if err := p.DB.UpdateAppDevMode(c.UserContext(), id, enabled, service, target, command); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	if enabled && service != "" {
		svcs := p.LoadComposeServices(c.UserContext(), id)
		found := false
		for _, s := range svcs {
			if s == service {
				found = true
				break
			}
		}
		if !found && len(svcs) > 0 {
			_ = p.DB.InsertDeployLog(c.UserContext(), id, "Dev mode update", true,
				fmt.Sprintf("Warning: configured dev service %q not found in compose services (%s). Dev mount is skipped until names match.",
					service, strings.Join(svcs, ", ")))
		}
	}
	if err := p.syncAndApplyBackground(c, id); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	utils.SetFlash(c, "devModeSaved")
	state := "disabled"
	if enabled {
		state = "enabled"
	}
	p.RecordAuditLog(c, "app_dev_mode", "app", id, "Development mode "+state)
	return c.Redirect(fmt.Sprintf("/apps/%s?tab=dev", id))
}

// ResetAppDevDependencies removes the app-scoped dev dependency named volumes (nddev_<appID>_*)
// and recreates the containers so fresh packages from the image are pulled, without touching databases.
func (p *Panel) ResetAppDevDependencies(c *fiber.Ctx) error {
	id := c.Params("id")
	app, err := p.DB.GetApp(c.UserContext(), id)
	if err != nil {
		return utils.RespondAppNotFound(c)
	}

	volPrefix := fmt.Sprintf("nddev_%s_", id)
	listCtx, listCancel := context.WithTimeout(context.Background(), 15*time.Second)
	cmd := exec.CommandContext(listCtx, "docker", "volume", "ls", "-q", "--filter", "name="+volPrefix)
	out, _ := cmd.Output()
	listCancel()

	var vols []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && strings.HasPrefix(line, volPrefix) {
			vols = append(vols, line)
		}
	}

	if len(vols) == 0 {
		utils.SetFlash(c, "devDepsNoVolumes")
		return c.Redirect(fmt.Sprintf("/apps/%s?tab=dev", id))
	}

	// Identify services to stop/restart (so databases keep running with zero downtime)
	var targetServices []string
	if s := strings.TrimSpace(app.DevService); s != "" {
		targetServices = []string{s}
	} else {
		// Forward-match volume names against known compose services using longest prefix match.
		// This avoids corrupting service names that contain underscores (e.g. "api_worker" vs "api").
		svcs := p.loadComposeServices(c.UserContext(), id)
		svcSet := make(map[string]bool)
		for _, v := range vols {
			if s := dev.MatchDevVolumeService(v, volPrefix, svcs); s != "" {
				svcSet[s] = true
			}
		}
		for s := range svcSet {
			targetServices = append(targetServices, s)
		}
		if len(targetServices) == 0 {
			targetServices = svcs
		}
	}

	p.RecordAuditLog(c, "app_dev_deps_reset", "app", id, fmt.Sprintf("Reset %d dev volumes", len(vols)))
	utils.SetFlash(c, "devDepsResetSuccess")

	// Run recreate in background with context.Background() so client disconnection does not abort the reset
	go func() {
		bgCtx, bgCancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer bgCancel()

		project := p.activeComposeProjectName(bgCtx, app, id)
		dir := p.appSourcePath(bgCtx, id)
		paths := p.effectiveComposePaths(bgCtx, app, id)
		envFiles := p.composeEnvFiles(bgCtx, id)

		// 1. Stop and remove only the targeted dev service containers (e.g. web, worker). Databases keep running!
		_ = dockerx.ComposeRmServices(bgCtx, dir, paths, project, nil, envFiles, targetServices...)

		// 2. Remove the app-scoped dev dependency named volumes, capturing errors
		var volErrs []string
		for _, v := range vols {
			if out, err := exec.CommandContext(bgCtx, "docker", "volume", "rm", "-f", v).CombinedOutput(); err != nil {
				msg := strings.TrimSpace(string(out))
				if msg == "" {
					msg = err.Error()
				}
				volErrs = append(volErrs, fmt.Sprintf("%s (%s)", v, msg))
			}
		}

		// 3. Recreate and start the targeted services with fresh volumes from image
		res := dockerx.ComposeApplyServices(bgCtx, dir, paths, project, nil, envFiles, targetServices...)

		ok := res.OK && len(volErrs) == 0
		statusMsg := fmt.Sprintf("Reset %d development dependency volume(s) for service(s) [%s]:\n%s\n\nContainers recreated with fresh image dependencies.",
			len(vols), strings.Join(targetServices, ", "), strings.Join(vols, "\n"))
		if len(volErrs) > 0 {
			statusMsg += "\n\n[error] Failed to delete volume(s):\n" + strings.Join(volErrs, "\n")
		}
		if !res.OK {
			statusMsg += "\n\n[error] Service start warning/failure:\n" + strings.TrimSpace(res.Output)
		}

		_ = p.DB.InsertDeployLog(bgCtx, id, "Reset dev dependencies", ok, statusMsg)
	}()

	return c.Redirect(fmt.Sprintf("/apps/%s?tab=dev", id))
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
