package handlers

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gofiber/fiber/v2"
)


func (p *Panel) BrowsePartial(c *fiber.Ctx) error {
	id := c.Params("id")
	if _, err := p.DB.GetApp(c.UserContext(), id); err != nil {
		return c.Status(404).SendString("not found")
	}
	if p.isGitApp(c.UserContext(), id) {
		return c.Status(400).SendString("files browser is disabled for git-backed apps")
	}
	return p.renderBrowse(c, id, c.Query("path", ""), "")
}

func (p *Panel) renderBrowse(c *fiber.Ctx, id, rel, flash string) error {
	children, err := p.Store.ListChildren(id, rel)
	if err != nil {
		return c.Status(500).SendString(err.Error())
	}
	parent := p.Store.ParentRel(rel)
	return c.Render(tmplPartialBrowser, fiber.Map{
		"ID":           id,
		"Path":         rel,
		"ParentPath":   parent,
		"Children":     children,
		"BrowseFlash":  flash,
		"DeleteTarget": fmt.Sprintf("/apps/%s/files/delete", id),
	})
}

func (p *Panel) BrowseDelete(c *fiber.Ctx) error {
	id := c.Params("id")
	if _, err := p.DB.GetApp(c.UserContext(), id); err != nil {
		if strings.EqualFold(c.Query("format"), "json") {
			return c.Status(404).JSON(fiber.Map{"ok": false, "message": "not found"})
		}
		return c.Status(404).SendString("not found")
	}
	if p.isGitApp(c.UserContext(), id) {
		if strings.EqualFold(c.Query("format"), "json") {
			return c.Status(400).JSON(fiber.Map{"ok": false, "message": "files delete is disabled for git-backed apps"})
		}
		return c.Status(400).SendString("files delete is disabled for git-backed apps")
	}
	wantJSON := strings.EqualFold(c.Query("format"), "json")
	baseRel, err := normalizeWorkspaceScopeRel(c.Query("base", ""))
	if err != nil {
		if wantJSON {
			return c.Status(400).JSON(fiber.Map{"ok": false, "message": "invalid base path"})
		}
		return c.Status(400).SendString("invalid base path")
	}
	if err := p.enforcePHPPanelScopedBase(c, id, baseRel); err != nil {
		if wantJSON {
			return c.Status(403).JSON(fiber.Map{"ok": false, "message": "base path not allowed"})
		}
		return c.Status(403).SendString("base path not allowed")
	}
	returnPath := c.FormValue("path")
	paths := formValues(c, "paths")
	if len(paths) == 0 {
		if wantJSON {
			return c.Status(400).JSON(fiber.Map{"ok": false, "message": "Select at least one file or folder."})
		}
		return p.renderBrowse(c, id, returnPath, "Select at least one file or folder.")
	}
	var errs []string
	for _, pth := range paths {
		fullPath, err := joinWorkspaceScope(baseRel, pth)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: invalid path", pth))
			continue
		}
		if err := p.Store.RemoveRel(id, fullPath); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", pth, err))
		}
	}
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
	return p.renderBrowse(c, id, returnPath, flash)
}

const (
	maxWorkspaceFileInline   = 8 << 20  // 8 MiB
	maxWorkspaceFileDownload = 512 << 20 // 512 MiB
)

func workspaceFileContentType(abs string, head []byte) string {
	t := http.DetectContentType(head)
	if t != "application/octet-stream" && t != "" {
		return t
	}
	switch strings.ToLower(filepath.Ext(abs)) {
	case ".html", ".htm":
		return "text/html; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".js", ".mjs":
		return "text/javascript; charset=utf-8"
	case ".json":
		return "application/json; charset=utf-8"
	case ".yml", ".yaml":
		return "text/yaml; charset=utf-8"
	case ".txt", ".conf", ".env", ".md", ".log", ".ini", ".sh":
		return "text/plain; charset=utf-8"
	default:
		return "application/octet-stream"
	}
}

func safeContentDispositionFilename(rel string) string {
	base := filepath.Base(strings.ReplaceAll(rel, "\\", "/"))
	base = strings.ReplaceAll(base, `"`, "")
	if base == "." || base == "/" || base == "" {
		return "file"
	}
	return base
}

// WorkspaceFile serves a single file from the app workspace (?path=relative). Use download=1 for attachment.
func (p *Panel) WorkspaceFile(c *fiber.Ctx) error {
	id := c.Params("id")
	if _, err := p.DB.GetApp(c.UserContext(), id); err != nil {
		return c.Status(404).SendString("app not found")
	}
	if p.isGitApp(c.UserContext(), id) {
		return c.Status(400).SendString("file view is disabled for git-backed apps")
	}
	baseRel, err := normalizeWorkspaceScopeRel(c.Query("base", ""))
	if err != nil {
		return c.Status(400).SendString("invalid base path")
	}
	if err := p.enforcePHPPanelScopedBase(c, id, baseRel); err != nil {
		return c.Status(403).SendString("base path not allowed")
	}
	rel := c.Query("path", "")
	fullRel, err := joinWorkspaceScope(baseRel, rel)
	if err != nil {
		return c.Status(400).SendString("invalid path")
	}
	full, err := p.Store.SafeFilePath(id, fullRel)
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
	ct := workspaceFileContentType(full, head)
	fn := safeContentDispositionFilename(rel)
	if download {
		c.Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, fn))
	} else {
		c.Set("Content-Disposition", fmt.Sprintf(`inline; filename="%s"`, fn))
	}
	c.Type(ct)
	return c.Send(b)
}

func (p *Panel) WorkspaceFileModal(c *fiber.Ctx) error {
	id := c.Params("id")
	if _, err := p.DB.GetApp(c.UserContext(), id); err != nil {
		return c.Status(404).SendString("app not found")
	}
	if p.isGitApp(c.UserContext(), id) {
		return c.Status(400).Render(tmplPartialFilePreviewModal, fiber.Map{
			"PreviewError": "Files preview is disabled for git-backed apps. Use the Git tab and redeploy from repository source.",
		})
	}
	baseRel, err := normalizeWorkspaceScopeRel(c.Query("base", ""))
	if err != nil {
		return c.Status(400).Render(tmplPartialFilePreviewModal, fiber.Map{
			"PreviewError": "invalid base path",
		})
	}
	if err := p.enforcePHPPanelScopedBase(c, id, baseRel); err != nil {
		return c.Status(403).Render(tmplPartialFilePreviewModal, fiber.Map{
			"PreviewError": "base path not allowed",
		})
	}
	rel := c.Query("path", "")
	fullRel, err := joinWorkspaceScope(baseRel, rel)
	if err != nil {
		return c.Status(400).Render(tmplPartialFilePreviewModal, fiber.Map{
			"PreviewError": "invalid path",
		})
	}
	full, err := p.Store.SafeFilePath(id, fullRel)
	if err != nil {
		return c.Status(400).Render(tmplPartialFilePreviewModal, fiber.Map{
			"PreviewError": "invalid path",
		})
	}
	st, err := os.Stat(full)
	if err != nil {
		if os.IsNotExist(err) {
			return c.Status(404).Render(tmplPartialFilePreviewModal, fiber.Map{
				"PreviewError": "file not found",
			})
		}
		return c.Status(500).Render(tmplPartialFilePreviewModal, fiber.Map{
			"PreviewError": err.Error(),
		})
	}
	if st.IsDir() {
		return c.Status(400).Render(tmplPartialFilePreviewModal, fiber.Map{
			"PreviewError": "cannot preview a directory",
		})
	}
	if st.Size() > maxWorkspaceFileInline {
		return c.Status(413).Render(tmplPartialFilePreviewModal, fiber.Map{
			"PreviewName":  safeContentDispositionFilename(rel),
			"PreviewPath":  rel,
			"PreviewTooBig": true,
		})
	}
	b, err := os.ReadFile(full)
	if err != nil {
		return c.Status(500).Render(tmplPartialFilePreviewModal, fiber.Map{
			"PreviewError": err.Error(),
		})
	}
	head := b
	if len(head) > 512 {
		head = head[:512]
	}
	ct := workspaceFileContentType(full, head)
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
	return c.Render(tmplPartialFilePreviewModal, fiber.Map{
		"PreviewName":    safeContentDispositionFilename(rel),
		"PreviewPath":    rel,
		"PreviewContent": string(b),
		"PreviewCT":      ct,
		"PreviewBinary":  !isText,
	})
}
