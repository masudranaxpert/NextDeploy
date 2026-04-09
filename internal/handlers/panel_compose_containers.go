package handlers

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"panel/internal/dockerx"
	"panel/internal/logview"

	"github.com/gofiber/fiber/v2"
)


func (p *Panel) ComposeFileView(c *fiber.Ctx) error {
	id := c.Params("id")
	app, err := p.DB.GetApp(c.UserContext(), id)
	if err != nil {
		return respondAppNotFound(c)
	}
	if err := p.syncAppCaddyOverride(c, id); err != nil {
		c.Type("text/plain; charset=utf-8")
		return c.Status(500).SendString(err.Error())
	}
	cp := p.composeFilePath(c.UserContext(), app, id)
	overridePath := p.composeOverridePath(c.UserContext(), id)
	b, err := os.ReadFile(overridePath)
	if err != nil {
		b, err = os.ReadFile(cp)
		if err != nil {
			c.Type("text/plain; charset=utf-8")
			return c.Status(404).SendString("Compose file not found. Set the path on Overview or upload the file under Files.")
		}
	}
	const maxPreview = 1024 * 1024
	suffix := ""
	if len(b) > maxPreview {
		b = b[:maxPreview]
		suffix = "\n\n... (truncated at 1 MB for this preview)\n"
	}
	c.Type("text/plain; charset=utf-8")
	return c.SendString(string(b) + suffix)
}

func (p *Panel) ComposeFileModal(c *fiber.Ctx) error {
	id := c.Params("id")
	app, err := p.DB.GetApp(c.UserContext(), id)
	if err != nil {
		return respondAppNotFound(c)
	}
	if err := p.syncAppCaddyOverride(c, id); err != nil {
		return c.Status(500).Render("partials/compose/compose_preview_modal", fiber.Map{
			"ComposePreview": err.Error(),
			"ComposeError":   true,
		})
	}
	cp := p.composeFilePath(c.UserContext(), app, id)
	overridePath := p.composeOverridePath(c.UserContext(), id)
	b, err := os.ReadFile(overridePath)
	if err != nil {
		b, err = os.ReadFile(cp)
		if err != nil {
			return c.Status(404).Render("partials/compose/compose_preview_modal", fiber.Map{
				"ComposePreview": "Compose file not found. Set the path on Overview or upload the file under Files.",
				"ComposeError":   true,
			})
		}
	}
	const maxPreview = 1024 * 1024
	suffix := ""
	if len(b) > maxPreview {
		b = b[:maxPreview]
		suffix = "\n\n... (truncated at 1 MB for this preview)\n"
	}
	return c.Render("partials/compose/compose_preview_modal", fiber.Map{
		"ComposePreview": string(b) + suffix,
		"ComposeError":   false,
	})
}

func (p *Panel) AppComposePartial(c *fiber.Ctx) error {
	return p.renderComposeTable(c, c.Params("id"))
}

func (p *Panel) renderComposeTable(c *fiber.Ctx, id string) error {
	app, err := p.DB.GetApp(c.UserContext(), id)
	if err != nil {
		return c.Status(404).SendString("not found")
	}
	dir := p.appSourcePath(c.UserContext(), id)
	cp := p.composeFilePath(c.UserContext(), app, id)
	if _, err := os.Stat(cp); err != nil {
		return c.Render("partials/compose/compose_table", fiber.Map{
			"ID":           id,
			"ComposeRows":  []dockerx.ComposePsRow(nil),
			"ComposePsMsg": "Compose file not found. Set the filename on Overview or upload it in Files.",
		})
	}
	if err := p.syncAppCaddyOverride(c, id); err != nil {
		return c.Render("partials/compose/compose_table", fiber.Map{
			"ID":           id,
			"ComposeRows":  []dockerx.ComposePsRow(nil),
			"ComposePsMsg": err.Error(),
		})
	}
	ctx, cancel := context.WithTimeout(c.UserContext(), 60*time.Second)
	defer cancel()
	project := p.activeComposeProjectName(ctx, app, id)
	rows, res := dockerx.ComposePS(ctx, dir, p.effectiveComposePaths(c.UserContext(), app, id), project, p.composeEnvFiles(ctx, id))
	errMsg := ""
	if !res.OK {
		errMsg = res.Output
		rows = nil
	}
	return c.Render("partials/compose/compose_table", fiber.Map{
		"ID":           id,
		"ComposeRows":  rows,
		"ComposePsMsg": errMsg,
	})
}

func (p *Panel) ContainerRestartOp(c *fiber.Ctx) error {
	id := c.Params("id")
	if _, err := p.DB.GetApp(c.UserContext(), id); err != nil {
		return c.Status(404).SendString("not found")
	}
	name := strings.TrimSpace(c.FormValue("container"))
	if !p.containerBelongsToApp(c.UserContext(), id, name) {
		return c.Status(400).SendString("invalid container")
	}
	ctx, cancel := context.WithTimeout(c.UserContext(), 3*time.Minute)
	defer cancel()
	_ = dockerx.ContainerRestart(ctx, name)
	return p.renderComposeTable(c, id)
}

func (p *Panel) ContainerRemoveOp(c *fiber.Ctx) error {
	id := c.Params("id")
	if _, err := p.DB.GetApp(c.UserContext(), id); err != nil {
		return c.Status(404).SendString("not found")
	}
	name := strings.TrimSpace(c.FormValue("container"))
	if !p.containerBelongsToApp(c.UserContext(), id, name) {
		return c.Status(400).SendString("invalid container")
	}
	ctx, cancel := context.WithTimeout(c.UserContext(), 3*time.Minute)
	defer cancel()
	_ = dockerx.ContainerRemove(ctx, name)
	return p.renderComposeTable(c, id)
}

func (p *Panel) ContainerRemoveSelectedOp(c *fiber.Ctx) error {
	id := c.Params("id")
	if _, err := p.DB.GetApp(c.UserContext(), id); err != nil {
		return c.Status(404).SendString("not found")
	}
	var names []string
	c.Request().PostArgs().VisitAll(func(key, val []byte) {
		if string(key) == "containers" {
			names = append(names, strings.TrimSpace(string(val)))
		}
	})
	ctx, cancel := context.WithTimeout(c.UserContext(), 5*time.Minute)
	defer cancel()
	for _, name := range names {
		if !p.containerBelongsToApp(c.UserContext(), id, name) {
			continue
		}
		_ = dockerx.ContainerRemove(ctx, name)
	}
	return p.renderComposeTable(c, id)
}

func logTailLines(q string) int {
	switch strings.TrimSpace(q) {
	case "100":
		return 100
	case "500":
		return 500
	case "1000":
		return 1000
	case "300":
		return 300
	default:
		return 300
	}
}

func (p *Panel) containerBelongsToApp(ctx context.Context, appID, containerName string) bool {
	containerName = strings.TrimSpace(containerName)
	if containerName == "" {
		return false
	}
	app, err := p.DB.GetApp(ctx, appID)
	if err != nil {
		return strings.Contains(containerName, appID)
	}
	candidates := p.composeProjectCandidates(ctx, app, appID)
	for _, project := range candidates {
		if project != "" && strings.Contains(containerName, project) {
			return true
		}
	}
	proj, workDir, ierr := dockerx.ContainerComposeLabels(ctx, containerName)
	if ierr != nil {
		return false
	}
	appRoot := filepath.Clean(p.appSourcePath(ctx, appID))
	if composeWorkspaceDirContainedInApp(appRoot, workDir) {
		return true
	}
	for _, c := range candidates {
		if proj != "" && proj == c {
			return true
		}
	}
	return false
}

// composeServiceBelongsToApp reports whether the compose service exists in this app's current compose ps.
// Prefer this over container Name for logs — Names embed ephemeral container id prefixes.
func (p *Panel) composeServiceBelongsToApp(ctx context.Context, appID, service string) bool {
	service = strings.TrimSpace(service)
	if service == "" {
		return false
	}
	app, err := p.DB.GetApp(ctx, appID)
	if err != nil {
		return false
	}
	_, rows, res := p.composeProjectAndPS(ctx, app, appID)
	if !res.OK {
		return false
	}
	for _, row := range rows {
		if strings.TrimSpace(row.Service) == service {
			return true
		}
	}
	return false
}

func (p *Panel) AppLogPartial(c *fiber.Ctx) error {
	id := c.Params("id")
	app, err := p.DB.GetApp(c.UserContext(), id)
	if err != nil {
		return c.Status(404).SendString("not found")
	}
	q := strings.TrimPrefix(strings.TrimSpace(c.Query("container")), "/")
	tail := logTailLines(c.Query("tail"))
	if q == "" {
		return c.Render(tmplPartialLogView, fiber.Map{
			"LogHTML": logview.FormatDockerLog("Select a container from the list."),
			"LogMeta": "",
		})
	}
	ctx, cancel := context.WithTimeout(c.UserContext(), 45*time.Second)
	defer cancel()
	byService := p.composeServiceBelongsToApp(ctx, id, q)
	if !byService && !p.containerBelongsToApp(ctx, id, q) {
		return c.Render(tmplPartialLogView, fiber.Map{
			"LogHTML": logview.FormatDockerLog("That service or container does not belong to this app."),
			"LogMeta": "",
		})
	}
	var res dockerx.Result
	if byService {
		project := p.activeComposeProjectName(ctx, app, id)
		dir := p.appSourcePath(ctx, id)
		res = dockerx.ComposeServiceLogs(ctx, dir, p.effectiveComposePaths(ctx, app, id), project, p.composeEnvFiles(ctx, id), q, tail)
	} else {
		res = dockerx.DockerLogs(ctx, q, tail)
	}
	raw := res.Output
	if !res.OK && strings.TrimSpace(raw) == "" {
		raw = "docker logs failed."
	}
	status := "ok"
	if !res.OK {
		status = "error"
	}
	meta := fmt.Sprintf("%s · %s · last %d lines", q, status, tail)
	html := logview.FormatDockerLog(raw)
	return c.Render(tmplPartialLogView, fiber.Map{
		"LogHTML": html,
		"LogMeta": meta,
	})
}
