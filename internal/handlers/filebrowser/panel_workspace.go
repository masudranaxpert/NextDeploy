package filebrowser

import (
	"fmt"
	"io"
	"os"
	"panel/internal/handlers/utils"
	"path/filepath"
	"strings"

	"github.com/gofiber/fiber/v2"
)

func (h *Handler) BrowsePartial(c *fiber.Ctx) error {
	id := c.Params("id")
	if _, err := h.p.DB.GetApp(c.UserContext(), id); err != nil {
		return c.Status(404).SendString("not found")
	}
	if h.p.IsGitApp(c.UserContext(), id) {
		return c.Status(400).SendString("files browser is disabled for git-backed apps")
	}
	return h.renderBrowse(c, id, c.Query("path", ""), "")
}

func (h *Handler) renderBrowse(c *fiber.Ctx, id, rel, flash string) error {
	children, err := h.p.Store.ListChildren(id, rel)
	if err != nil {
		return c.Status(500).SendString(err.Error())
	}
	parent := h.p.Store.ParentRel(rel)
	return c.Render("partials/browser", fiber.Map{
		"ID":           id,
		"Path":         rel,
		"ParentPath":   parent,
		"Children":     children,
		"BrowseFlash":  flash,
		"DeleteTarget": fmt.Sprintf("/apps/%s/files/delete", id),
	})
}

func (h *Handler) BrowseCreate(c *fiber.Ctx) error {
	id := c.Params("id")
	wantJSON := strings.EqualFold(c.Query("format"), "json")
	respond := func(status int, ok bool, message string) error {
		if wantJSON {
			return c.Status(status).JSON(fiber.Map{"ok": ok, "message": message})
		}
		if status >= 400 && status != 404 {
			return c.Redirect("/apps/" + id + "?tab=files")
		}
		if status == 404 {
			return c.Status(404).SendString(message)
		}
		return c.Redirect("/apps/" + id + "?tab=files")
	}

	if _, err := h.p.DB.GetApp(c.UserContext(), id); err != nil {
		return respond(404, false, "not found")
	}
	if h.p.IsGitApp(c.UserContext(), id) {
		return respond(400, false, "file creation is disabled for git-backed apps")
	}

	name := strings.TrimSpace(c.FormValue("filename"))
	if name == "" {
		return respond(400, false, "filename required")
	}
	if isGeneratedComposeRel(name) {
		return respond(400, false, generatedComposeManagedMsg())
	}

	rel := filepath.ToSlash(name)
	if strings.HasSuffix(name, "/") {
		rel += "/"
	}

	full, err := h.p.Store.SafeFilePath(id, rel)
	if err != nil {
		return respond(400, false, "invalid path")
	}

	if strings.HasSuffix(name, "/") {
		if err := os.MkdirAll(full, 0750); err != nil {
			return respond(500, false, err.Error())
		}
		return respond(200, true, "Folder created")
	}

	if err := os.MkdirAll(filepath.Dir(full), 0750); err != nil {
		return respond(500, false, err.Error())
	}
	if err := os.WriteFile(full, nil, 0640); err != nil {
		return respond(500, false, err.Error())
	}
	return respond(200, true, "File created")
}

func (h *Handler) BrowseDelete(c *fiber.Ctx) error {
	id := c.Params("id")
	if _, err := h.p.DB.GetApp(c.UserContext(), id); err != nil {
		if strings.EqualFold(c.Query("format"), "json") {
			return c.Status(404).JSON(fiber.Map{"ok": false, "message": "not found"})
		}
		return c.Status(404).SendString("not found")
	}
	if h.p.IsGitApp(c.UserContext(), id) {
		if strings.EqualFold(c.Query("format"), "json") {
			return c.Status(400).JSON(fiber.Map{"ok": false, "message": "files delete is disabled for git-backed apps"})
		}
		return c.Status(400).SendString("files delete is disabled for git-backed apps")
	}
	wantJSON := strings.EqualFold(c.Query("format"), "json")
	returnPath := c.FormValue("path")
	var paths []string
	c.Request().PostArgs().VisitAll(func(key, val []byte) {
		if string(key) == "paths" {
			paths = append(paths, string(val))
		}
	})
	if len(paths) == 0 {
		if wantJSON {
			return c.Status(400).JSON(fiber.Map{"ok": false, "message": "Select at least one file or folder."})
		}
		return h.renderBrowse(c, id, returnPath, "Select at least one file or folder.")
	}
	var errs []string
	for _, pth := range paths {
		if isGeneratedComposeRel(pth) {
			errs = append(errs, fmt.Sprintf("%s: %s", pth, generatedComposeManagedMsg()))
			continue
		}
		if err := h.p.Store.RemoveRel(id, pth); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", pth, err))
		}
	}
	h.p.InvalidateAppStorageCache(id)
	if wantJSON {
		if len(errs) > 0 {
			return c.JSON(fiber.Map{"ok": false, "message": strings.Join(errs, "; ")})
		}
		return c.JSON(fiber.Map{"ok": true, "message": "Selected items removed."})
	}
	flash := ""
	if len(errs) > 0 {
		flash = "Some deletions failed: " + strings.Join(errs, " · ")
	}
	return h.renderBrowse(c, id, returnPath, flash)
}

const (
	maxWorkspaceFileInline   = 8 << 20
	maxWorkspaceFileDownload = 512 << 20
)

func (h *Handler) WorkspaceFile(c *fiber.Ctx) error {
	id := c.Params("id")
	if _, err := h.p.DB.GetApp(c.UserContext(), id); err != nil {
		return utils.RespondAppNotFound(c)
	}
	if h.p.IsGitApp(c.UserContext(), id) {
		return c.Status(400).SendString("file view is disabled for git-backed apps")
	}
	rel := c.Query("path", "")
	full, err := h.p.Store.SafeFilePath(id, rel)
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
	ct := utils.WorkspaceFileContentType(full, head)
	fn := utils.SafeContentDispositionFilename(rel)
	if download {
		c.Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, fn))
	} else {
		c.Set("Content-Disposition", fmt.Sprintf(`inline; filename="%s"`, fn))
	}
	c.Type(ct)
	return c.Send(b)
}

func (h *Handler) WorkspaceFileModal(c *fiber.Ctx) error {
	id := c.Params("id")
	if _, err := h.p.DB.GetApp(c.UserContext(), id); err != nil {
		return utils.RespondAppNotFound(c)
	}
	if h.p.IsGitApp(c.UserContext(), id) {
		return c.Status(400).Render("partials/file_preview_modal", fiber.Map{
			"PreviewError": "Files preview is disabled for git-backed apps. Use the Git tab and redeploy from repository source.",
		})
	}
	rel := c.Query("path", "")
	full, err := h.p.Store.SafeFilePath(id, rel)
	if err != nil {
		return c.Status(400).Render("partials/file_preview_modal", fiber.Map{
			"PreviewError": "invalid path",
		})
	}
	st, err := os.Stat(full)
	if err != nil {
		if os.IsNotExist(err) {
			return c.Status(404).Render("partials/file_preview_modal", fiber.Map{
				"PreviewError": "file not found",
			})
		}
		return c.Status(500).Render("partials/file_preview_modal", fiber.Map{
			"PreviewError": err.Error(),
		})
	}
	if st.IsDir() {
		return c.Status(400).Render("partials/file_preview_modal", fiber.Map{
			"PreviewError": "cannot preview a directory",
		})
	}
	if st.Size() > maxWorkspaceFileInline {
		return c.Status(413).Render("partials/file_preview_modal", fiber.Map{
			"PreviewName":   utils.SafeContentDispositionFilename(rel),
			"PreviewPath":   rel,
			"PreviewTooBig": true,
		})
	}
	b, err := os.ReadFile(full)
	if err != nil {
		return c.Status(500).Render("partials/file_preview_modal", fiber.Map{
			"PreviewError": err.Error(),
		})
	}
	head := b
	if len(head) > 512 {
		head = head[:512]
	}
	ct := utils.WorkspaceFileContentType(full, head)
	textTypes := []string{"text/", "application/json", "application/javascript", "application/xml", "text/yaml"}
	isText := false
	for _, tt := range textTypes {
		if strings.HasPrefix(ct, tt) {
			isText = true
			break
		}
	}
	if !isText {
		ct = "application/octet-stream"
	}
	return c.Render("partials/file_preview_modal", fiber.Map{
		"PreviewName":    utils.SafeContentDispositionFilename(rel),
		"PreviewPath":    rel,
		"PreviewContent": string(b),
		"PreviewCT":      ct,
		"PreviewBinary":  !isText,
	})
}

func (h *Handler) BrowseRename(c *fiber.Ctx) error {
	id := c.Params("id")
	if _, err := h.p.DB.GetApp(c.UserContext(), id); err != nil {
		return c.Status(404).JSON(fiber.Map{"ok": false, "message": "not found"})
	}
	if h.p.IsGitApp(c.UserContext(), id) {
		return c.Status(400).JSON(fiber.Map{"ok": false, "message": "files renaming is disabled for git-backed apps"})
	}
	oldPath := strings.TrimSpace(c.FormValue("old_path"))
	newPath := strings.TrimSpace(c.FormValue("new_path"))
	if oldPath == "" || newPath == "" {
		return c.Status(400).JSON(fiber.Map{"ok": false, "message": "both old_path and new_path are required"})
	}
	if isGeneratedComposeRel(oldPath) || isGeneratedComposeRel(newPath) {
		return c.Status(400).JSON(fiber.Map{"ok": false, "message": generatedComposeManagedMsg()})
	}
	oldFull, err := h.p.Store.SafeFilePath(id, oldPath)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"ok": false, "message": "invalid old path"})
	}
	newFull, err := h.p.Store.SafeFilePath(id, newPath)
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
	return c.JSON(fiber.Map{"ok": true, "message": "renamed successfully"})
}
