package handlers

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"panel/internal/dockerx"
	"panel/internal/resmatch"

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

func (p *Panel) ContainerBelongsToApp(ctx context.Context, appID, containerName string) bool {
	containerName = strings.TrimSpace(containerName)
	containerName = strings.TrimPrefix(containerName, "/")
	if containerName == "" {
		return false
	}
	app, err := p.DB.GetApp(ctx, appID)
	if err != nil {
		return false
	}
	candidates := p.composeProjectCandidates(ctx, app, appID)
	allProjects := p.AllPanelComposeProjects(ctx)
	prefixMatch := false
	for _, project := range candidates {
		if project != "" && resmatch.MatchesComposeContainerName(containerName, project, allProjects) {
			prefixMatch = true
			break
		}
	}

	proj, workDir, ierr := dockerx.ContainerComposeLabels(ctx, containerName)
	if ierr != nil {
		return prefixMatch
	}
	if strings.TrimSpace(workDir) != "" {
		return composeWorkspaceDirContainedInApp(filepath.Clean(p.Store.Path(appID)), workDir)
	}
	if proj != "" {
		for _, c := range candidates {
			if proj == c {
				return true
			}
		}
		return false
	}
	return prefixMatch
}

func (p *Panel) ComposeServiceInRows(rows []dockerx.ComposePsRow, service string) bool {
	service = strings.TrimSpace(service)
	if service == "" {
		return false
	}
	for _, row := range rows {
		if strings.TrimSpace(row.Service) == service {
			return true
		}
	}
	return false
}

func (p *Panel) ComposeServiceBelongsToApp(ctx context.Context, appID, service string) bool {
	service = strings.TrimSpace(service)
	if service == "" {
		return false
	}
	app, err := p.DB.GetApp(ctx, appID)
	if err != nil {
		return false
	}
	_, rows, res := p.ComposeProjectAndPS(ctx, app, appID)
	if !res.OK {
		return false
	}
	return p.ComposeServiceInRows(rows, service)
}

// FriendlyComposeMsg turns noisy docker compose stderr into a short UI hint when ps fails.
func FriendlyComposeMsg(msg string) string {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return ""
	}
	lower := strings.ToLower(msg)
	if strings.Contains(lower, "postgres_password") && strings.Contains(lower, "required") {
		return "Add POSTGRES_PASSWORD (and any other required variables) in the Environment tab, save, then deploy again."
	}
	if strings.Contains(lower, "interpolating") && strings.Contains(lower, "missing") {
		return "Compose needs environment variables that are not set. Open Environment, set the missing keys, save, then deploy or refresh."
	}
	if strings.Contains(lower, "required variable") {
		return "A required variable is missing. Open Environment, add the missing keys, save, then try again."
	}
	if len(msg) > 400 {
		return msg[:400] + "…"
	}
	return msg
}
