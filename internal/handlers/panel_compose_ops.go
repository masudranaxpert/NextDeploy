package handlers

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"panel/internal/dockerapi"
	"panel/internal/dockerx"
	"panel/internal/logview"
	"panel/internal/perflog"

	"github.com/gofiber/fiber/v2"
)

func (p *Panel) ComposeFileView(c *fiber.Ctx) error {
	id := c.Params("id")
	app, err := p.DB.GetApp(c.UserContext(), id)
	if err != nil {
		return c.Status(fiber.StatusNotFound).SendString("app not found")
	}
	if err := p.SyncAppCaddyOverride(c, id); err != nil {
		c.Type("text/plain; charset=utf-8")
		return c.Status(500).SendString(err.Error())
	}
	cp := p.ComposeFilePath(c.UserContext(), app, id)
	overridePath := p.ComposeOverridePath(c.UserContext(), id)
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
		return c.Status(fiber.StatusNotFound).SendString("app not found")
	}
	if err := p.SyncAppCaddyOverride(c, id); err != nil {
		return c.Status(500).Render("partials/compose/compose_preview_modal", fiber.Map{
			"ComposePreview": err.Error(),
			"ComposeError":   true,
		})
	}
	cp := p.ComposeFilePath(c.UserContext(), app, id)
	overridePath := p.ComposeOverridePath(c.UserContext(), id)
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

func (p *Panel) TerminalContainersPartial(c *fiber.Ctx) error {
	m, err := p.appContainerPickData(c.Params("id"), c)
	if err != nil {
		return err
	}
	return c.Render("partials/app_show/terminal_containers_pick", m)
}

func (p *Panel) LogsContainersPartial(c *fiber.Ctx) error {
	m, err := p.appContainerPickData(c.Params("id"), c)
	if err != nil {
		return err
	}
	return c.Render("partials/app_show/logs_containers_pick", m)
}

func (p *Panel) appContainerPickData(id string, c *fiber.Ctx) (fiber.Map, error) {
	app, err := p.DB.GetApp(c.UserContext(), id)
	if err != nil {
		return nil, c.Status(404).SendString("not found")
	}
	cp := p.ComposeFilePath(c.UserContext(), app, id)
	if _, err := os.Stat(cp); err != nil {
		hasDockerfile, hasCompose := p.Store.HasDockerArtifacts(id)
		if !hasDockerfile || hasCompose {
			return fiber.Map{
				"ID":           id,
				"ComposeRows":  nil,
				"ComposePsMsg": "Compose file not found. Set the filename on Overview or upload it in Files.",
			}, nil
		}
	}
	if err := p.SyncAppCaddyOverride(c, id); err != nil {
		return fiber.Map{
			"ID":           id,
			"ComposeRows":  nil,
			"ComposePsMsg": err.Error(),
		}, nil
	}
	ctx, cancel := context.WithTimeout(c.UserContext(), 60*time.Second)
	defer cancel()
	_, rows, res := p.ComposeProjectAndPS(ctx, app, id)
	errMsg := ""
	if !res.OK {
		errMsg = res.Output
		rows = nil
	}
	return fiber.Map{
		"ID":           id,
		"ComposeRows":  rows,
		"ComposePsMsg": errMsg,
	}, nil
}

func (p *Panel) renderComposeTable(c *fiber.Ctx, id string) error {
	app, err := p.DB.GetApp(c.UserContext(), id)
	if err != nil {
		return c.Status(404).SendString("not found")
	}
	cp := p.ComposeFilePath(c.UserContext(), app, id)
	if _, err := os.Stat(cp); err != nil {
		// Dockerfile-only apps run from the auto-generated merged compose.
		hasDockerfile, hasCompose := p.Store.HasDockerArtifacts(id)
		if !hasDockerfile || hasCompose {
			return c.Render("partials/compose/compose_table", fiber.Map{
				"ID":           id,
				"ComposeRows":  []dockerx.ComposePsRow(nil),
				"ComposePsMsg": "Compose file not found. Set the filename on Overview or upload it in Files.",
			})
		}
	}
	if err := p.SyncAppCaddyOverride(c, id); err != nil {
		return c.Render("partials/compose/compose_table", fiber.Map{
			"ID":           id,
			"ComposeRows":  []dockerx.ComposePsRow(nil),
			"ComposePsMsg": err.Error(),
		})
	}
	ctx, cancel := context.WithTimeout(c.UserContext(), 60*time.Second)
	defer cancel()
	_, rows, res := p.ComposeProjectAndPS(ctx, app, id)
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

func (p *Panel) ContainerStartOp(c *fiber.Ctx) error {
	id := c.Params("id")
	if _, err := p.DB.GetApp(c.UserContext(), id); err != nil {
		return c.Status(404).SendString("not found")
	}
	name := strings.TrimSpace(c.FormValue("container"))
	if !p.ContainerBelongsToApp(c.UserContext(), id, name) {
		return c.Status(400).SendString("invalid container")
	}
	ctx, cancel := context.WithTimeout(c.UserContext(), 3*time.Minute)
	defer cancel()
	_ = dockerx.ContainerStart(ctx, name)
	p.InvalidateAfterDockerChange()
	return p.renderComposeTable(c, id)
}

func (p *Panel) ContainerStopOp(c *fiber.Ctx) error {
	id := c.Params("id")
	if _, err := p.DB.GetApp(c.UserContext(), id); err != nil {
		return c.Status(404).SendString("not found")
	}
	name := strings.TrimSpace(c.FormValue("container"))
	if !p.ContainerBelongsToApp(c.UserContext(), id, name) {
		return c.Status(400).SendString("invalid container")
	}
	ctx, cancel := context.WithTimeout(c.UserContext(), 3*time.Minute)
	defer cancel()
	_ = dockerx.ContainerStop(ctx, name)
	p.InvalidateAfterDockerChange()
	return p.renderComposeTable(c, id)
}

func (p *Panel) ContainerRestartOp(c *fiber.Ctx) error {
	id := c.Params("id")
	if _, err := p.DB.GetApp(c.UserContext(), id); err != nil {
		return c.Status(404).SendString("not found")
	}
	name := strings.TrimSpace(c.FormValue("container"))
	if !p.ContainerBelongsToApp(c.UserContext(), id, name) {
		return c.Status(400).SendString("invalid container")
	}
	ctx, cancel := context.WithTimeout(c.UserContext(), 3*time.Minute)
	defer cancel()
	_ = dockerx.ContainerRestart(ctx, name)
	p.InvalidateAfterDockerChange()
	return p.renderComposeTable(c, id)
}

func (p *Panel) ContainerRemoveOp(c *fiber.Ctx) error {
	id := c.Params("id")
	if _, err := p.DB.GetApp(c.UserContext(), id); err != nil {
		return c.Status(404).SendString("not found")
	}
	name := strings.TrimSpace(c.FormValue("container"))
	if !p.ContainerBelongsToApp(c.UserContext(), id, name) {
		return c.Status(400).SendString("invalid container")
	}
	ctx, cancel := context.WithTimeout(c.UserContext(), 3*time.Minute)
	defer cancel()
	_ = dockerx.ContainerRemove(ctx, name)
	p.InvalidateAfterDockerChange()
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
		if !p.ContainerBelongsToApp(c.UserContext(), id, name) {
			continue
		}
		_ = dockerx.ContainerRemove(ctx, name)
	}
	p.InvalidateAfterDockerChange()
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
	case "3000":
		return 3000
	case "5000":
		return 5000
	case "300":
		return 300
	default:
		return 300
	}
}

func (p *Panel) AppLogPartial(c *fiber.Ctx) error {
	id := c.Params("id")
	tr := perflog.Start("AppLogPartial")
	defer tr.Finish()
	tr.Field("app", id)

	mark := time.Now()
	app, err := p.DB.GetApp(c.UserContext(), id)
	tr.StepDur("db_get_app", mark)
	if err != nil {
		return c.Status(404).SendString("not found")
	}
	q := strings.TrimPrefix(strings.TrimSpace(c.Query("container")), "/")
	tail := logTailLines(c.Query("tail"))
	tr.Field("container", q)
	tr.Field("tail", fmt.Sprintf("%d", tail))
	if q == "" {
		return c.Render("partials/log_view", fiber.Map{
			"LogHTML": logview.FormatDockerLog("Select a container from the list."),
			"LogMeta": "",
		})
	}
	ctx, cancel := context.WithTimeout(c.UserContext(), 45*time.Second)
	defer cancel()
	mark = time.Now()
	project, composeRows, composeRes := p.ComposeProjectAndPS(ctx, app, id)
	byService := composeRes.OK && p.ComposeServiceInRows(composeRows, q)
	if !byService && !p.ContainerBelongsToApp(ctx, id, q) {
		tr.StepDur("access_check", mark)
		return c.Render("partials/log_view", fiber.Map{
			"LogHTML": logview.FormatDockerLog("That service or container does not belong to this app."),
			"LogMeta": "",
		})
	}
	tr.StepDur("access_check", mark)
	logRef := q
	if byService {
		tr.Field("project", project)
		mark = time.Now()
		cid, rerr := dockerapi.ContainerIDForComposeService(ctx, project, q)
		tr.StepDur("resolve_container_id", mark)
		if rerr != nil {
			return c.Render("partials/log_view", fiber.Map{
				"LogHTML": logview.FormatDockerLog("Could not resolve container for service " + q + ": " + rerr.Error()),
				"LogMeta": "",
			})
		}
		logRef = cid
	}
	mark = time.Now()
	raw, ferr := dockerapi.FetchContainerLogsText(ctx, logRef, tail)
	tr.StepDur("docker_logs", mark)
	status := "ok"
	if ferr != nil {
		if strings.TrimSpace(raw) == "" {
			raw = "docker logs failed: " + ferr.Error()
		}
		status = "error"
	}
	meta := fmt.Sprintf("%s · %s · last %d lines", q, status, tail)
	mark = time.Now()
	html := logview.FormatDockerLog(raw)
	tr.StepDur("format_log", mark)
	mark = time.Now()
	err = c.Render("partials/log_view", fiber.Map{
		"LogHTML": html,
		"LogMeta": meta,
	})
	tr.StepDur("render", mark)
	return err
}
