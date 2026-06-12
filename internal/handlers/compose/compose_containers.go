package compose

import (
	"context"
	"fmt"
	"os"
	"panel/internal/handlers/utils"
	"strings"
	"time"

	"panel/internal/dockerapi"
	"panel/internal/dockerx"
	"panel/internal/logview"
	"panel/internal/perflog"

	"github.com/gofiber/fiber/v2"
)

func (h *Handler) ComposeFileView(c *fiber.Ctx) error {
	id := c.Params("id")
	app, err := h.P.DB.GetApp(c.UserContext(), id)
	if err != nil {
		return utils.RespondAppNotFound(c)
	}
	if err := h.P.SyncAppCaddyOverride(c, id); err != nil {
		c.Type("text/plain; charset=utf-8")
		return c.Status(500).SendString(err.Error())
	}
	cp := h.P.ComposeFilePath(c.UserContext(), app, id)
	overridePath := h.P.ComposeOverridePath(c.UserContext(), id)
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

func (h *Handler) ComposeFileModal(c *fiber.Ctx) error {
	id := c.Params("id")
	app, err := h.P.DB.GetApp(c.UserContext(), id)
	if err != nil {
		return utils.RespondAppNotFound(c)
	}
	if err := h.P.SyncAppCaddyOverride(c, id); err != nil {
		return c.Status(500).Render("partials/compose/compose_preview_modal", fiber.Map{
			"ComposePreview": err.Error(),
			"ComposeError":   true,
		})
	}
	cp := h.P.ComposeFilePath(c.UserContext(), app, id)
	overridePath := h.P.ComposeOverridePath(c.UserContext(), id)
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

func (h *Handler) AppComposePartial(c *fiber.Ctx) error {
	return h.renderComposeTable(c, c.Params("id"))
}

func (h *Handler) TerminalContainersPartial(c *fiber.Ctx) error {
	id := c.Params("id")
	app, err := h.P.DB.GetApp(c.UserContext(), id)
	if err != nil {
		return c.Status(404).SendString("not found")
	}
	cp := h.P.ComposeFilePath(c.UserContext(), app, id)
	if _, err := os.Stat(cp); err != nil {
		hasDockerfile, hasCompose := h.P.Store.HasDockerArtifacts(id)
		if !hasDockerfile || hasCompose {
			return c.Render("partials/app_show/terminal_containers_pick", fiber.Map{
				"ComposeRows":  nil,
				"ComposePsMsg": "Compose file not found. Set the filename on Overview or upload it in Files.",
			})
		}
	}
	if err := h.P.SyncAppCaddyOverride(c, id); err != nil {
		return c.Render("partials/app_show/terminal_containers_pick", fiber.Map{
			"ComposeRows":  nil,
			"ComposePsMsg": err.Error(),
		})
	}
	ctx, cancel := context.WithTimeout(c.UserContext(), 60*time.Second)
	defer cancel()
	_, rows, res := h.P.ComposeProjectAndPS(ctx, app, id)
	errMsg := ""
	if !res.OK {
		errMsg = res.Output
		rows = nil
	}
	return c.Render("partials/app_show/terminal_containers_pick", fiber.Map{
		"ComposeRows":  rows,
		"ComposePsMsg": errMsg,
	})
}

func (h *Handler) renderComposeTable(c *fiber.Ctx, id string) error {
	app, err := h.P.DB.GetApp(c.UserContext(), id)
	if err != nil {
		return c.Status(404).SendString("not found")
	}
	cp := h.P.ComposeFilePath(c.UserContext(), app, id)
	if _, err := os.Stat(cp); err != nil {
		// Dockerfile-only apps run from the auto-generated merged compose.
		hasDockerfile, hasCompose := h.P.Store.HasDockerArtifacts(id)
		if !hasDockerfile || hasCompose {
			return c.Render("partials/compose/compose_table", fiber.Map{
				"ID":           id,
				"ComposeRows":  []dockerx.ComposePsRow(nil),
				"ComposePsMsg": "Compose file not found. Set the filename on Overview or upload it in Files.",
			})
		}
	}
	if err := h.P.SyncAppCaddyOverride(c, id); err != nil {
		return c.Render("partials/compose/compose_table", fiber.Map{
			"ID":           id,
			"ComposeRows":  []dockerx.ComposePsRow(nil),
			"ComposePsMsg": err.Error(),
		})
	}
	ctx, cancel := context.WithTimeout(c.UserContext(), 60*time.Second)
	defer cancel()
	_, rows, res := h.P.ComposeProjectAndPS(ctx, app, id)
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

func (h *Handler) ContainerStartOp(c *fiber.Ctx) error {
	id := c.Params("id")
	if _, err := h.P.DB.GetApp(c.UserContext(), id); err != nil {
		return c.Status(404).SendString("not found")
	}
	name := strings.TrimSpace(c.FormValue("container"))
	if !h.P.ContainerBelongsToApp(c.UserContext(), id, name) {
		return c.Status(400).SendString("invalid container")
	}
	ctx, cancel := context.WithTimeout(c.UserContext(), 3*time.Minute)
	defer cancel()
	_ = dockerx.ContainerStart(ctx, name)
	return h.renderComposeTable(c, id)
}

func (h *Handler) ContainerStopOp(c *fiber.Ctx) error {
	id := c.Params("id")
	if _, err := h.P.DB.GetApp(c.UserContext(), id); err != nil {
		return c.Status(404).SendString("not found")
	}
	name := strings.TrimSpace(c.FormValue("container"))
	if !h.P.ContainerBelongsToApp(c.UserContext(), id, name) {
		return c.Status(400).SendString("invalid container")
	}
	ctx, cancel := context.WithTimeout(c.UserContext(), 3*time.Minute)
	defer cancel()
	_ = dockerx.ContainerStop(ctx, name)
	return h.renderComposeTable(c, id)
}

func (h *Handler) ContainerRestartOp(c *fiber.Ctx) error {
	id := c.Params("id")
	if _, err := h.P.DB.GetApp(c.UserContext(), id); err != nil {
		return c.Status(404).SendString("not found")
	}
	name := strings.TrimSpace(c.FormValue("container"))
	if !h.P.ContainerBelongsToApp(c.UserContext(), id, name) {
		return c.Status(400).SendString("invalid container")
	}
	ctx, cancel := context.WithTimeout(c.UserContext(), 3*time.Minute)
	defer cancel()
	_ = dockerx.ContainerRestart(ctx, name)
	return h.renderComposeTable(c, id)
}

func (h *Handler) ContainerRemoveOp(c *fiber.Ctx) error {
	id := c.Params("id")
	if _, err := h.P.DB.GetApp(c.UserContext(), id); err != nil {
		return c.Status(404).SendString("not found")
	}
	name := strings.TrimSpace(c.FormValue("container"))
	if !h.P.ContainerBelongsToApp(c.UserContext(), id, name) {
		return c.Status(400).SendString("invalid container")
	}
	ctx, cancel := context.WithTimeout(c.UserContext(), 3*time.Minute)
	defer cancel()
	_ = dockerx.ContainerRemove(ctx, name)
	return h.renderComposeTable(c, id)
}

func (h *Handler) ContainerRemoveSelectedOp(c *fiber.Ctx) error {
	id := c.Params("id")
	if _, err := h.P.DB.GetApp(c.UserContext(), id); err != nil {
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
		if !h.P.ContainerBelongsToApp(c.UserContext(), id, name) {
			continue
		}
		_ = dockerx.ContainerRemove(ctx, name)
	}
	return h.renderComposeTable(c, id)
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

func (h *Handler) AppLogPartial(c *fiber.Ctx) error {
	id := c.Params("id")
	tr := perflog.Start("AppLogPartial")
	defer tr.Finish()
	tr.Field("app", id)

	mark := time.Now()
	app, err := h.P.DB.GetApp(c.UserContext(), id)
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
	project, composeRows, composeRes := h.P.ComposeProjectAndPS(ctx, app, id)
	byService := composeRes.OK && h.P.ComposeServiceInRows(composeRows, q)
	if !byService && !h.P.ContainerBelongsToApp(ctx, id, q) {
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
