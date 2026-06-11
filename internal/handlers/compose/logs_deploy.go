package compose

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"html"
	"io"
	"os"
	"panel/internal/handlers/utils"
	"strconv"
	"strings"
	"time"

	"panel/internal/dockerapi"
	"panel/internal/dockerx"

	"panel/internal/logview"
	"panel/internal/volumex"
	"panel/internal/workspace"

	"github.com/gofiber/fiber/v2"
)

func deleteAppHtmxErrorHTML(msg string) string {
	return `<div class="rounded-lg border border-rose-500/30 bg-rose-950/40 px-3 py-2 text-sm text-rose-200 whitespace-pre-wrap">` + html.EscapeString(msg) + `</div>`
}

func isHtmxRequest(c *fiber.Ctx) bool {
	return strings.EqualFold(c.Get("HX-Request"), "true")
}

func (h *Handler) AppExec(c *fiber.Ctx) error {
	id := c.Params("id")
	if _, err := h.P.DB.GetApp(c.UserContext(), id); err != nil {
		return c.Status(404).SendString("not found")
	}
	name := strings.TrimPrefix(strings.TrimSpace(c.FormValue("container")), "/")
	cmd := c.FormValue("command")
	if !h.P.ContainerBelongsToApp(c.UserContext(), id, name) {
		return c.Status(400).SendString("invalid container for this app")
	}
	ctx, cancel := context.WithTimeout(c.UserContext(), 3*time.Minute)
	defer cancel()
	res := dockerx.DockerExec(ctx, name, cmd)
	out := res.Output
	if strings.TrimSpace(out) == "" {
		out = "(no output — either the command produced nothing or the container has no such path)"
	} else if !res.OK {
		out = out + "\n[non-zero exit: " + res.Output + "]"
	}
	return c.Render("partials/terminal_out", fiber.Map{
		"ExecHTML": logview.FormatTerminalOutput(out),
		"ExecOK":   res.OK,
	})
}

func (h *Handler) ClearDeployLogs(c *fiber.Ctx) error {
	id := c.Params("id")
	if _, err := h.P.DB.GetApp(c.UserContext(), id); err != nil {
		return utils.RespondAppNotFound(c)
	}
	if err := h.P.DB.ClearDeployLogs(c.UserContext(), id); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	return c.Redirect(fmt.Sprintf("/apps/%s?tab=deployment", id))
}

func (h *Handler) DeployLogGet(c *fiber.Ctx) error {
	id := c.Params("id")
	if _, err := h.P.DB.GetApp(c.UserContext(), id); err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "app not found"})
	}
	logID, err := strconv.ParseInt(c.Params("logId"), 10, 64)
	if err != nil || logID < 1 {
		return c.Status(400).JSON(fiber.Map{"error": "invalid log id"})
	}
	d, err := h.P.DB.GetDeployLog(c.UserContext(), id, logID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return c.Status(404).JSON(fiber.Map{"error": "log not found"})
		}
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{
		"action":        d.Action,
		"ok":            d.OK,
		"output":        d.Output,
		"created_label": d.CreatedAt.Format("Jan 02, 2006 15:04:05"),
	})
}

func (h *Handler) DeployLogDelete(c *fiber.Ctx) error {
	id := c.Params("id")
	if _, err := h.P.DB.GetApp(c.UserContext(), id); err != nil {
		return utils.RespondAppNotFound(c)
	}
	logID, err := strconv.ParseInt(c.Params("logId"), 10, 64)
	if err != nil || logID < 1 {
		return c.Status(400).SendString("invalid log id")
	}
	deleted, err := h.P.DB.DeleteDeployLog(c.UserContext(), id, logID)
	if err != nil {
		return c.Status(500).SendString(err.Error())
	}
	if !deleted {
		return c.Status(404).SendString("log not found")
	}
	return c.Redirect(fmt.Sprintf("/apps/%s?tab=deployment", id))
}

func (h *Handler) DeleteApp(c *fiber.Ctx) error {
	htmx := isHtmxRequest(c)
	id := c.Params("id")
	app, err := h.P.DB.GetApp(c.UserContext(), id)
	if err != nil {
		if htmx {
			c.Set("Content-Type", "text/html; charset=utf-8")
			return c.Status(fiber.StatusOK).SendString(deleteAppHtmxErrorHTML("App not found."))
		}
		return utils.RespondAppNotFound(c)
	}
	confirm := strings.TrimSpace(c.FormValue("confirm_name"))
	if confirm != strings.TrimSpace(app.Name) {
		if htmx {
			c.Set("Content-Type", "text/html; charset=utf-8")
			return c.Status(fiber.StatusOK).SendString(deleteAppHtmxErrorHTML("Type the app name exactly in the confirmation field to delete this app."))
		}
		return c.Status(400).SendString("Type the app name exactly in the confirmation field to delete this app.")
	}
	dir := h.P.AppSourcePath(c.UserContext(), id)
	cp := h.P.ComposeFilePath(c.UserContext(), app, id)
	ctx, cancel := context.WithTimeout(c.UserContext(), 15*time.Minute)
	defer cancel()
	candidates := h.P.ComposeProjectCandidates(ctx, app, id)
	paths := h.P.EffectiveComposePaths(ctx, app, id)
	envFiles := h.P.ComposeEnvFiles(ctx, id)
	var cleanupErrs []string
	if _, err := os.Stat(cp); err == nil {
		for _, project := range candidates {
			if res := dockerx.ComposeDownDeleteProject(ctx, dir, paths, project, nil, envFiles); !res.OK && strings.TrimSpace(res.Output) != "" && !strings.Contains(strings.ToLower(res.Output), "no resource found") {
				cleanupErrs = append(cleanupErrs, res.Output)
			}
		}
	}
	for _, project := range candidates {
		if errs := dockerapi.RemoveContainersByComposeProject(ctx, project); len(errs) > 0 {
			cleanupErrs = append(cleanupErrs, errs...)
		}
		if errs := dockerapi.RemoveAppContainers(ctx, project); len(errs) > 0 {
			cleanupErrs = append(cleanupErrs, errs...)
		}
		if errs := dockerapi.RemoveAppImages(ctx, project); len(errs) > 0 {
			cleanupErrs = append(cleanupErrs, errs...)
		}
		if errs := dockerapi.RemoveAppNetworks(ctx, project); len(errs) > 0 {
			cleanupErrs = append(cleanupErrs, errs...)
		}
		if msg := volumex.RemoveMatching(ctx, project); msg != "" {
			cleanupErrs = append(cleanupErrs, msg)
		}
	}
	if len(cleanupErrs) > 0 {
		msg := strings.Join(cleanupErrs, "\n")
		if htmx {
			c.Set("Content-Type", "text/html; charset=utf-8")
			return c.Status(fiber.StatusOK).SendString(deleteAppHtmxErrorHTML(msg))
		}
		return c.Status(500).SendString(msg)
	}
	if err := h.P.DB.DeleteApp(c.UserContext(), id); err != nil {
		if htmx {
			c.Set("Content-Type", "text/html; charset=utf-8")
			return c.Status(fiber.StatusOK).SendString(deleteAppHtmxErrorHTML(err.Error()))
		}
		return c.Status(500).SendString(err.Error())
	}
	h.P.RecordAuditLog(c, "delete_app", "app", id, "Deleted app: "+app.Name)
	if err := os.RemoveAll(dir); err != nil {
		if htmx {
			c.Set("Content-Type", "text/html; charset=utf-8")
			return c.Status(fiber.StatusOK).SendString(deleteAppHtmxErrorHTML(err.Error()))
		}
		return c.Status(500).SendString(err.Error())
	}
	if htmx {
		c.Set("HX-Redirect", "/apps")
		return c.SendStatus(fiber.StatusOK)
	}
	return c.Redirect("/apps")
}

func (h *Handler) UploadZip(c *fiber.Ctx) error {
	id := c.Params("id")
	if _, err := h.P.DB.GetApp(c.UserContext(), id); err != nil {
		return utils.RespondAppNotFound(c)
	}
	if h.P.IsGitApp(c.UserContext(), id) {
		return c.Status(400).SendString("ZIP upload is disabled for git-backed apps")
	}
	app, _ := h.P.DB.GetApp(c.UserContext(), id)
	fh, err := c.FormFile("archive")
	if err != nil {
		return c.Status(400).SendString("missing archive field (zip)")
	}
	src, err := fh.Open()
	if err != nil {
		return c.Status(500).SendString(err.Error())
	}
	defer src.Close()
	tmp, err := os.CreateTemp("", "panel-upload-*.zip")
	if err != nil {
		return c.Status(500).SendString(err.Error())
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := io.Copy(tmp, src); err != nil {
		_ = tmp.Close()
		return c.Status(500).SendString(err.Error())
	}
	if err := tmp.Close(); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	st, err := os.Stat(tmpPath)
	if err != nil {
		return c.Status(500).SendString(err.Error())
	}
	f, err := os.Open(tmpPath)
	if err != nil {
		return c.Status(500).SendString(err.Error())
	}
	defer f.Close()
	if err := workspace.ValidateZipArchive(f, st.Size()); err != nil {
		return c.Status(400).SendString(err.Error())
	}
	if err := h.P.Store.ClearAllUserFiles(id); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	f2, err := os.Open(tmpPath)
	if err != nil {
		return c.Status(500).SendString(err.Error())
	}
	defer f2.Close()
	st2, err := f2.Stat()
	if err != nil {
		return c.Status(500).SendString(err.Error())
	}
	if err := h.P.Store.ExtractZip(id, f2, st2.Size()); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	if err := h.P.Store.WriteMeta(id, app.Name); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	if err := h.P.SyncAppCaddyOverride(c, id); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	return c.Redirect(fmt.Sprintf("/apps/%s?tab=files", id))
}

func (h *Handler) UploadFile(c *fiber.Ctx) error {
	id := c.Params("id")
	if _, err := h.P.DB.GetApp(c.UserContext(), id); err != nil {
		return utils.RespondAppNotFound(c)
	}
	if h.P.IsGitApp(c.UserContext(), id) {
		return c.Status(400).SendString("file upload is disabled for git-backed apps")
	}
	file, err := c.FormFile("file")
	if err != nil {
		return c.Status(400).SendString("missing file")
	}
	src, err := file.Open()
	if err != nil {
		return c.Status(500).SendString(err.Error())
	}
	defer src.Close()
	targetPath := file.Filename
	if pfx := c.FormValue("path"); pfx != "" {
		pfx = strings.Trim(strings.ReplaceAll(pfx, "\\", "/"), "/")
		if pfx != "" {
			targetPath = pfx + "/" + file.Filename
		}
	}
	if _, err := h.P.Store.SaveUploadedFile(id, targetPath, src); err != nil {
		return c.Status(400).SendString("invalid path")
	}
	return c.Redirect(fmt.Sprintf("/apps/%s?tab=files", id))
}
