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

func (h *Handler) renderComposeTable(c *fiber.Ctx, id string) error {
	app, err := h.P.DB.GetApp(c.UserContext(), id)
	if err != nil {
		return c.Status(404).SendString("not found")
	}
	dir := h.P.AppSourcePath(c.UserContext(), id)
	cp := h.P.ComposeFilePath(c.UserContext(), app, id)
	if _, err := os.Stat(cp); err != nil {
		return c.Render("partials/compose/compose_table", fiber.Map{
			"ID":           id,
			"ComposeRows":  []dockerx.ComposePsRow(nil),
			"ComposePsMsg": "Compose file not found. Set the filename on Overview or upload it in Files.",
		})
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
	project := h.P.ActiveComposeProjectName(ctx, app, id)
	rows, res := dockerx.ComposePS(ctx, dir, h.P.EffectiveComposePaths(c.UserContext(), app, id), project, h.P.ComposeEnvFiles(ctx, id))
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
	app, err := h.P.DB.GetApp(c.UserContext(), id)
	if err != nil {
		return c.Status(404).SendString("not found")
	}
	q := strings.TrimPrefix(strings.TrimSpace(c.Query("container")), "/")
	tail := logTailLines(c.Query("tail"))
	if q == "" {
		return c.Render("partials/log_view", fiber.Map{
			"LogHTML": logview.FormatDockerLog("Select a container from the list."),
			"LogMeta": "",
		})
	}
	ctx, cancel := context.WithTimeout(c.UserContext(), 45*time.Second)
	defer cancel()
	byService := h.P.ComposeServiceBelongsToApp(ctx, id, q)
	if !byService && !h.P.ContainerBelongsToApp(ctx, id, q) {
		return c.Render("partials/log_view", fiber.Map{
			"LogHTML": logview.FormatDockerLog("That service or container does not belong to this app."),
			"LogMeta": "",
		})
	}
	logRef := q
	if byService {
		project := h.P.ActiveComposeProjectName(ctx, app, id)
		cid, rerr := dockerapi.ContainerIDForComposeService(ctx, project, q)
		if rerr != nil {
			return c.Render("partials/log_view", fiber.Map{
				"LogHTML": logview.FormatDockerLog("Could not resolve container for service " + q + ": " + rerr.Error()),
				"LogMeta": "",
			})
		}
		logRef = cid
	}
	raw, ferr := dockerapi.FetchContainerLogsText(ctx, logRef, tail)
	status := "ok"
	if ferr != nil {
		if strings.TrimSpace(raw) == "" {
			raw = "docker logs failed: " + ferr.Error()
		}
		status = "error"
	}
	meta := fmt.Sprintf("%s · %s · last %d lines", q, status, tail)
	html := logview.FormatDockerLog(raw)
	return c.Render("partials/log_view", fiber.Map{
		"LogHTML": html,
		"LogMeta": meta,
	})
}
