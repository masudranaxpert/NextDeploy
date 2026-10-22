package handlers

import (
	"context"
	"os"
	"strings"
	"time"

	"panel/internal/dockerx"

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

func (p *Panel) ContainerStartOp(c *fiber.Ctx) error {
	id := c.Params("id")
	if _, err := p.DB.GetApp(c.UserContext(), id); err != nil {
		return c.Status(404).JSON(fiber.Map{"ok": false, "error": "not found"})
	}
	name := strings.TrimSpace(c.FormValue("container"))
	if !p.ContainerBelongsToApp(c.UserContext(), id, name) {
		return c.Status(400).JSON(fiber.Map{"ok": false, "error": "invalid container"})
	}
	ctx, cancel := context.WithTimeout(c.UserContext(), 3*time.Minute)
	defer cancel()
	res := dockerx.ContainerStart(ctx, name)
	p.InvalidateAfterDockerChange()
	if WantsJSON(c) {
		return c.JSON(fiber.Map{"ok": res.OK, "output": res.Output})
	}
	return c.Redirect("/apps/" + id)
}

func (p *Panel) ContainerStopOp(c *fiber.Ctx) error {
	id := c.Params("id")
	if _, err := p.DB.GetApp(c.UserContext(), id); err != nil {
		return c.Status(404).JSON(fiber.Map{"ok": false, "error": "not found"})
	}
	name := strings.TrimSpace(c.FormValue("container"))
	if !p.ContainerBelongsToApp(c.UserContext(), id, name) {
		return c.Status(400).JSON(fiber.Map{"ok": false, "error": "invalid container"})
	}
	ctx, cancel := context.WithTimeout(c.UserContext(), 3*time.Minute)
	defer cancel()
	res := dockerx.ContainerStop(ctx, name)
	p.InvalidateAfterDockerChange()
	if WantsJSON(c) {
		return c.JSON(fiber.Map{"ok": res.OK, "output": res.Output})
	}
	return c.Redirect("/apps/" + id)
}

func (p *Panel) ContainerRestartOp(c *fiber.Ctx) error {
	id := c.Params("id")
	if _, err := p.DB.GetApp(c.UserContext(), id); err != nil {
		return c.Status(404).JSON(fiber.Map{"ok": false, "error": "not found"})
	}
	name := strings.TrimSpace(c.FormValue("container"))
	if !p.ContainerBelongsToApp(c.UserContext(), id, name) {
		return c.Status(400).JSON(fiber.Map{"ok": false, "error": "invalid container"})
	}
	ctx, cancel := context.WithTimeout(c.UserContext(), 3*time.Minute)
	defer cancel()
	res := dockerx.ContainerRestart(ctx, name)
	p.InvalidateAfterDockerChange()
	if WantsJSON(c) {
		return c.JSON(fiber.Map{"ok": res.OK, "output": res.Output})
	}
	return c.Redirect("/apps/" + id)
}

func (p *Panel) ContainerRemoveOp(c *fiber.Ctx) error {
	id := c.Params("id")
	if _, err := p.DB.GetApp(c.UserContext(), id); err != nil {
		return c.Status(404).JSON(fiber.Map{"ok": false, "error": "not found"})
	}
	name := strings.TrimSpace(c.FormValue("container"))
	if !p.ContainerBelongsToApp(c.UserContext(), id, name) {
		return c.Status(400).JSON(fiber.Map{"ok": false, "error": "invalid container"})
	}
	ctx, cancel := context.WithTimeout(c.UserContext(), 3*time.Minute)
	defer cancel()
	res := dockerx.ContainerRemove(ctx, name)
	p.InvalidateAfterDockerChange()
	if WantsJSON(c) {
		return c.JSON(fiber.Map{"ok": res.OK, "output": res.Output})
	}
	return c.Redirect("/apps/" + id)
}

func (p *Panel) ContainerRemoveSelectedOp(c *fiber.Ctx) error {
	id := c.Params("id")
	if _, err := p.DB.GetApp(c.UserContext(), id); err != nil {
		return c.Status(404).JSON(fiber.Map{"ok": false, "error": "not found"})
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
	if WantsJSON(c) {
		return c.JSON(fiber.Map{"ok": true})
	}
	return c.Redirect("/apps/" + id)
}
