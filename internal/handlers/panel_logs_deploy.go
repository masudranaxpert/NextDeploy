package handlers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"panel/internal/dockerapi"
	"panel/internal/dockerx"
	"panel/internal/logview"
	"panel/internal/volumex"

	"github.com/gofiber/fiber/v2"
)


func (p *Panel) AppExec(c *fiber.Ctx) error {
	id := c.Params("id")
	if _, err := p.DB.GetApp(c.UserContext(), id); err != nil {
		return c.Status(404).SendString("not found")
	}
	name := strings.TrimPrefix(strings.TrimSpace(c.FormValue("container")), "/")
	cmd := c.FormValue("command")
	if !p.containerBelongsToApp(id, name) {
		return c.Status(400).SendString("invalid container for this app")
	}
	ctx, cancel := context.WithTimeout(c.UserContext(), 3*time.Minute)
	defer cancel()
	res := dockerx.DockerExec(ctx, name, cmd)
	out := res.Output
	if strings.TrimSpace(out) == "" {
		out = "(no output — either the command produced nothing or the container has no such path)"
	} else if !res.OK {
		// non-zero exit — append note
		out = out + "\n[non-zero exit: " + res.Output + "]"
	}
	return c.Render(tmplPartialTerminalOut, fiber.Map{
		"ExecHTML": logview.FormatTerminalOutput(out),
		"ExecOK":   res.OK,
	})
}

func (p *Panel) ClearDeployLogs(c *fiber.Ctx) error {
	id := c.Params("id")
	if _, err := p.DB.GetApp(c.UserContext(), id); err != nil {
		return c.Status(404).SendString("app not found")
	}
	if err := p.DB.ClearDeployLogs(c.UserContext(), id); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	return c.Redirect(fmt.Sprintf("/apps/%s?tab=deployment", id))
}

// DeployLogGet returns one deployment log as JSON (used by the deployment tab modal).
func (p *Panel) DeployLogGet(c *fiber.Ctx) error {
	id := c.Params("id")
	if _, err := p.DB.GetApp(c.UserContext(), id); err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "app not found"})
	}
	logID, err := strconv.ParseInt(c.Params("logId"), 10, 64)
	if err != nil || logID < 1 {
		return c.Status(400).JSON(fiber.Map{"error": "invalid log id"})
	}
	d, err := p.DB.GetDeployLog(c.UserContext(), id, logID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return c.Status(404).JSON(fiber.Map{"error": "log not found"})
		}
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{
		"action":         d.Action,
		"ok":             d.OK,
		"output":         d.Output,
		"created_label":  d.CreatedAt.Format("Jan 02, 2006 15:04:05"),
	})
}

// DeployLogDelete removes a single deploy log row (form POST from deployment tab).
func (p *Panel) DeployLogDelete(c *fiber.Ctx) error {
	id := c.Params("id")
	if _, err := p.DB.GetApp(c.UserContext(), id); err != nil {
		return c.Status(404).SendString("app not found")
	}
	logID, err := strconv.ParseInt(c.Params("logId"), 10, 64)
	if err != nil || logID < 1 {
		return c.Status(400).SendString("invalid log id")
	}
	deleted, err := p.DB.DeleteDeployLog(c.UserContext(), id, logID)
	if err != nil {
		return c.Status(500).SendString(err.Error())
	}
	if !deleted {
		return c.Status(404).SendString("log not found")
	}
	return c.Redirect(fmt.Sprintf("/apps/%s?tab=deployment", id))
}

func (p *Panel) DeleteApp(c *fiber.Ctx) error {
	id := c.Params("id")
	app, err := p.DB.GetApp(c.UserContext(), id)
	if err != nil {
		return c.Status(404).SendString("app not found")
	}
	dir := p.appSourcePath(c.UserContext(), id)
	cp := p.composeFilePath(app, id)
	ctx, cancel := context.WithTimeout(c.UserContext(), 15*time.Minute)
	defer cancel()
	var cleanupErrs []string
	if _, err := os.Stat(cp); err == nil {
		for _, project := range p.legacyProjectNames(app, id) {
			if res := dockerx.ComposeDownDeleteProject(ctx, dir, p.effectiveComposePaths(c.UserContext(), app, id), project, nil, p.composeEnvFiles(ctx, id)); !res.OK && strings.TrimSpace(res.Output) != "" && !strings.Contains(strings.ToLower(res.Output), "no resource found") {
				cleanupErrs = append(cleanupErrs, res.Output)
			}
		}
	}
	for _, project := range p.legacyProjectNames(app, id) {
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
		return c.Status(500).SendString(strings.Join(cleanupErrs, "\n"))
	}
	if err := os.RemoveAll(dir); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	if err := p.DB.DeleteApp(c.UserContext(), id); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	return c.Redirect("/apps")
}

func (p *Panel) UploadZip(c *fiber.Ctx) error {
	id := c.Params("id")
	if _, err := p.DB.GetApp(c.UserContext(), id); err != nil {
		return c.Status(404).SendString("app not found")
	}
	if p.isGitApp(c.UserContext(), id) {
		return c.Status(400).SendString("ZIP upload is disabled for git-backed apps")
	}
	app, _ := p.DB.GetApp(c.UserContext(), id)
	fh, err := c.FormFile("archive")
	if err != nil {
		return c.Status(400).SendString("missing archive field (zip)")
	}
	if fh.Size > p.uploadMaxBytes(c.UserContext()) {
		return c.Status(fiber.StatusRequestEntityTooLarge).SendString(fmt.Sprintf("upload exceeds %d MB limit", p.uploadMaxMB(c.UserContext())))
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
	if err := p.Store.ClearAllUserFiles(id); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	if err := p.Store.ExtractZip(id, f, st.Size()); err != nil {
		return c.Status(400).SendString(err.Error())
	}
	if err := p.Store.WriteMeta(id, app.Name); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	if err := p.syncAppCaddyOverride(c, id); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	return c.Redirect(fmt.Sprintf("/apps/%s?tab=files", id))
}

func (p *Panel) UploadFile(c *fiber.Ctx) error {
	id := c.Params("id")
	if _, err := p.DB.GetApp(c.UserContext(), id); err != nil {
		return jsonOrText(c, 404, "app not found")
	}
	if p.isGitApp(c.UserContext(), id) {
		return jsonOrText(c, 400, "file upload is disabled for git-backed apps")
	}
	baseRel, err := normalizeWorkspaceScopeRel(c.Query("base", ""))
	if err != nil {
		return jsonOrText(c, 400, "invalid base path")
	}
	if err := p.enforcePHPPanelScopedBase(c, id, baseRel); err != nil {
		return jsonOrText(c, 403, "base path not allowed: "+err.Error())
	}
	targetRel, err := joinWorkspaceScope(baseRel, c.FormValue("path"))
	if err != nil {
		return jsonOrText(c, 400, "invalid path")
	}
	form, err := c.MultipartForm()
	if err != nil {
		return jsonOrText(c, 400, "missing file")
	}
	files := form.File["file"]
	if len(files) == 0 {
		return jsonOrText(c, 400, "no files provided")
	}
	limit := p.uploadMaxBytes(c.UserContext())
	for _, file := range files {
		if file.Size > limit {
			return jsonOrText(c, fiber.StatusRequestEntityTooLarge,
				fmt.Sprintf("file '%s' exceeds %d MB limit", file.Filename, p.uploadMaxMB(c.UserContext())))
		}
	}
	var saved []string
	for _, file := range files {
		src, err := file.Open()
		if err != nil {
			return jsonOrText(c, 500, err.Error())
		}
		destRel := file.Filename
		if targetRel != "" {
			destRel = strings.Trim(targetRel+"/"+strings.TrimLeft(strings.ReplaceAll(file.Filename, "\\", "/"), "/"), "/")
		}
		if _, err := p.Store.SaveUploadedFile(id, destRel, src); err != nil {
			_ = src.Close()
			return jsonOrText(c, 400, "could not save '"+file.Filename+"': "+err.Error())
		}
		_ = src.Close()
		p.ensurePHPPanelPublicReadable(id, filepath.ToSlash(destRel), false)
		saved = append(saved, file.Filename)
	}
	// Return JSON if the caller was the JS file browser (fetch), plain redirect otherwise.
	if isJSONRequest(c) {
		return c.JSON(fiber.Map{"ok": true, "message": fmt.Sprintf("Uploaded %d file(s).", len(saved))})
	}
	redirectURL := fmt.Sprintf("/apps/%s?tab=files", id)
	if baseRel != "" {
		redirectURL += "&base=" + url.QueryEscape(baseRel)
	}
	return c.Redirect(redirectURL)
}

// jsonOrText sends a JSON error if the request wants JSON, otherwise plain text.
func jsonOrText(c *fiber.Ctx, status int, msg string) error {
	if isJSONRequest(c) {
		return c.Status(status).JSON(fiber.Map{"ok": false, "message": msg})
	}
	return c.Status(status).SendString(msg)
}

// isJSONRequest returns true when the caller expects JSON (fetch XHR from the file browser).
func isJSONRequest(c *fiber.Ctx) bool {
	accept := c.Get("Accept")
	ct := c.Get("Content-Type")
	// fetch() with FormData does NOT set Accept:application/json by default,
	// but we can check if it is NOT a normal browser form submit (no full-page referer match).
	// Simplest reliable signal: fetch sets "Accept: */*" and does NOT send a full HTML Accept header.
	if strings.Contains(accept, "text/html") {
		return false
	}
	_ = ct
	// Treat anything that is NOT a classic browser form navigation as JSON.
	return true
}
