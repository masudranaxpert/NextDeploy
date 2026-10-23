package handlers

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/gofiber/fiber/v2"
)

func (p *Panel) BrowseCreate(c *fiber.Ctx) error {
	id := c.Params("id")
	wantJSON := strings.EqualFold(c.Query("format"), "json") || WantsJSON(c)
	respond := func(status int, ok bool, message string) error {
		if wantJSON {
			return c.Status(status).JSON(fiber.Map{"ok": ok, "message": message})
		}
		if status == 404 {
			return c.Status(404).SendString(message)
		}
		return c.Redirect("/apps/" + id + "?tab=files")
	}

	if _, err := p.DB.GetApp(c.UserContext(), id); err != nil {
		return respond(404, false, "not found")
	}
	if p.IsGitApp(c.UserContext(), id) {
		return respond(400, false, "file creation is disabled for git-backed apps")
	}

	var req struct {
		Path     string `json:"path"`
		Filename string `json:"filename"`
		IsDir    bool   `json:"is_dir"`
	}
	_ = c.BodyParser(&req)

	name := strings.TrimSpace(c.FormValue("filename"))
	if name == "" {
		name = strings.TrimSpace(c.FormValue("path"))
	}
	if name == "" {
		name = strings.TrimSpace(req.Filename)
	}
	if name == "" {
		name = strings.TrimSpace(req.Path)
	}
	if name == "" {
		return respond(400, false, "filename required")
	}
	if req.IsDir && !strings.HasSuffix(name, "/") {
		name += "/"
	}
	if isGeneratedComposeRel(name) {
		return respond(400, false, generatedComposeManagedMsg())
	}

	rel := filepath.ToSlash(name)
	if strings.HasSuffix(name, "/") {
		rel += "/"
	}

	full, err := p.Store.SafeFilePath(id, rel)
	if err != nil {
		return respond(400, false, "invalid path")
	}

	if strings.HasSuffix(name, "/") {
		if err := os.MkdirAll(full, 0750); err != nil {
			return respond(500, false, err.Error())
		}
		p.InvalidateAfterAppWorkspaceChange(id)
		return respond(200, true, "Folder created")
	}

	if err := os.MkdirAll(filepath.Dir(full), 0750); err != nil {
		return respond(500, false, err.Error())
	}
	if err := os.WriteFile(full, nil, 0640); err != nil {
		return respond(500, false, err.Error())
	}
	p.InvalidateAfterAppWorkspaceChange(id)
	return respond(200, true, "File created")
}

func (p *Panel) BrowseDelete(c *fiber.Ctx) error {
	id := c.Params("id")
	wantJSON := strings.EqualFold(c.Query("format"), "json") || WantsJSON(c)
	if _, err := p.DB.GetApp(c.UserContext(), id); err != nil {
		if wantJSON {
			return c.Status(404).JSON(fiber.Map{"ok": false, "message": "not found"})
		}
		return c.Status(404).SendString("not found")
	}
	if p.IsGitApp(c.UserContext(), id) {
		if wantJSON {
			return c.Status(400).JSON(fiber.Map{"ok": false, "message": "files delete is disabled for git-backed apps"})
		}
		return c.Status(400).SendString("files delete is disabled for git-backed apps")
	}
	var paths []string
	form, formErr := c.MultipartForm()
	if formErr == nil && form != nil {
		paths = append(paths, form.Value["paths"]...)
		paths = append(paths, form.Value["paths[]"]...)
	}
	c.Request().PostArgs().VisitAll(func(key, val []byte) {
		if string(key) == "paths" || string(key) == "paths[]" {
			paths = append(paths, string(val))
		}
	})
	if len(paths) == 0 {
		if wantJSON {
			return c.Status(400).JSON(fiber.Map{"ok": false, "message": "Select at least one file or folder."})
		}
		return c.Redirect("/apps/" + id + "?tab=files")
	}
	var errs []string
	for _, pth := range paths {
		if isGeneratedComposeRel(pth) {
			errs = append(errs, fmt.Sprintf("%s: %s", pth, generatedComposeManagedMsg()))
			continue
		}
		if err := p.Store.RemoveRel(id, pth); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", pth, err))
		}
	}
	p.InvalidateAfterAppWorkspaceChange(id)
	if wantJSON {
		if len(errs) > 0 {
			return c.JSON(fiber.Map{"ok": false, "message": strings.Join(errs, "; ")})
		}
		return c.JSON(fiber.Map{"ok": true, "message": "Selected items removed."})
	}
	return c.Redirect("/apps/" + id + "?tab=files")
}

const (
	maxWorkspaceFileInline   = 8 << 20
	maxWorkspaceFileDownload = 512 << 20
)

func (p *Panel) WorkspaceFile(c *fiber.Ctx) error {
	id := c.Params("id")
	if _, err := p.DB.GetApp(c.UserContext(), id); err != nil {
		return c.Status(fiber.StatusNotFound).SendString("app not found")
	}
	if p.IsGitApp(c.UserContext(), id) {
		return c.Status(400).SendString("file view is disabled for git-backed apps")
	}
	rel := c.Query("path", "")
	full, err := p.Store.SafeFilePath(id, rel)
	if err != nil {
		return c.Status(400).SendString("invalid path")
	}
	st, err := os.Stat(full)
	if err != nil {
		if os.IsNotExist(err) {
			return c.Status(404).SendString("not found")
		}
		return c.Status(500).SendString(err.Error())
	}
	if st.IsDir() {
		return c.Status(400).SendString("not a file")
	}
	download := c.Query("download") == "1"
	maxSz := int64(maxWorkspaceFileInline)
	if download {
		maxSz = maxWorkspaceFileDownload
	}
	if st.Size() > maxSz {
		if !download {
			return c.Status(413).SendString("file too large for inline view; add ?download=1")
		}
		return c.Status(413).SendString("file too large")
	}
	f, err := os.Open(full)
	if err != nil {
		return c.Status(500).SendString(err.Error())
	}
	defer f.Close()
	b, err := io.ReadAll(f)
	if err != nil {
		return c.Status(500).SendString(err.Error())
	}
	head := b
	if len(head) > 512 {
		head = head[:512]
	}
	ct := WorkspaceFileContentType(full, head)
	fn := SafeContentDispositionFilename(rel)
	if download {
		c.Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, fn))
	} else {
		c.Set("Content-Disposition", fmt.Sprintf(`inline; filename="%s"`, fn))
	}
	c.Type(ct)
	return c.Send(b)
}

func (p *Panel) BrowseRename(c *fiber.Ctx) error {
	id := c.Params("id")
	if _, err := p.DB.GetApp(c.UserContext(), id); err != nil {
		return c.Status(404).JSON(fiber.Map{"ok": false, "message": "not found"})
	}
	if p.IsGitApp(c.UserContext(), id) {
		return c.Status(400).JSON(fiber.Map{"ok": false, "message": "files renaming is disabled for git-backed apps"})
	}
	var req struct {
		OldPath string `json:"old_path"`
		NewPath string `json:"new_path"`
	}
	_ = c.BodyParser(&req)
	oldPath := strings.TrimSpace(c.FormValue("old_path"))
	if oldPath == "" {
		oldPath = strings.TrimSpace(req.OldPath)
	}
	newPath := strings.TrimSpace(c.FormValue("new_path"))
	if newPath == "" {
		newPath = strings.TrimSpace(req.NewPath)
	}
	if oldPath == "" || newPath == "" {
		return c.Status(400).JSON(fiber.Map{"ok": false, "message": "both old_path and new_path are required"})
	}
	if isGeneratedComposeRel(oldPath) || isGeneratedComposeRel(newPath) {
		return c.Status(400).JSON(fiber.Map{"ok": false, "message": generatedComposeManagedMsg()})
	}
	oldFull, err := p.Store.SafeFilePath(id, oldPath)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"ok": false, "message": "invalid old path"})
	}
	newFull, err := p.Store.SafeFilePath(id, newPath)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"ok": false, "message": "invalid new path"})
	}
	if _, err := os.Stat(oldFull); os.IsNotExist(err) {
		return c.Status(404).JSON(fiber.Map{"ok": false, "message": "file or folder not found"})
	}
	if _, err := os.Stat(newFull); err == nil {
		return c.Status(400).JSON(fiber.Map{"ok": false, "message": "destination path already exists"})
	}
	if err := os.MkdirAll(filepath.Dir(newFull), 0750); err != nil {
		return c.Status(500).JSON(fiber.Map{"ok": false, "message": err.Error()})
	}
	if err := os.Rename(oldFull, newFull); err != nil {
		return c.Status(500).JSON(fiber.Map{"ok": false, "message": err.Error()})
	}
	p.InvalidateAfterAppWorkspaceChange(id)
	return c.JSON(fiber.Map{"ok": true, "message": "renamed successfully"})
}
