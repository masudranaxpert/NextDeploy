package handlers

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"panel/internal/db"
	"panel/internal/workspace"

	"github.com/gofiber/fiber/v2"
)

const maxWorkspaceBlobJSON = 512 << 10 // inline JSON preview (aligned with git blob UI)

// maxWorkspaceFileSaveBytes caps a single workspace file save (JSON body "content") to limit memory and disk abuse.
const maxWorkspaceFileSaveBytes = 2 << 20 // 2 MiB

var errWorkspaceZipTooLarge = errors.New("workspace zip exceeds size limit")

func formValues(c *fiber.Ctx, key string) []string {
	var values []string
	if form, err := c.MultipartForm(); err == nil && form != nil {
		for _, raw := range form.Value[key] {
			raw = strings.TrimSpace(raw)
			if raw != "" {
				values = append(values, raw)
			}
		}
		if len(values) > 0 {
			return values
		}
	}
	c.Request().PostArgs().VisitAll(func(k, v []byte) {
		if string(k) != key {
			return
		}
		raw := strings.TrimSpace(string(v))
		if raw != "" {
			values = append(values, raw)
		}
	})
	return values
}

func (p *Panel) enforcePHPPanelScopedBase(c *fiber.Ctx, appID, baseRel string) error {
	user, ok := currentUser(c)
	if !ok || user.Role != db.RoleUser {
		return nil
	}
	app, err := p.DB.GetApp(c.UserContext(), appID)
	if err != nil {
		return err
	}
	if app.TemplateID != "php_panel" {
		return fiber.ErrForbidden
	}
	owner := phpPanelOwnerUser(c)
	if err := p.ensurePHPPanelOwnedBase(c.UserContext(), appID, owner.ID, baseRel); err != nil {
		return fiber.ErrForbidden
	}
	return nil
}

func (p *Panel) workspaceFilesGate(c *fiber.Ctx, appID string) int {
	ctx, cancel := context.WithTimeout(c.UserContext(), 30*time.Second)
	defer cancel()
	if _, err := p.DB.GetApp(ctx, appID); err != nil {
		return fiber.StatusNotFound
	}
	isGit, _, _ := p.appGitMetadata(ctx, appID)
	if isGit {
		return fiber.StatusBadRequest
	}
	return 0
}

// WorkspaceFilesTree returns directory listing JSON for the app workspace (non-git), same shape as GitRepoTree.
func (p *Panel) WorkspaceFilesTree(c *fiber.Ctx) error {
	appID := c.Params("id")
	if code := p.workspaceFilesGate(c, appID); code != 0 {
		msg := "not available"
		if code == fiber.StatusNotFound {
			msg = "app not found"
		}
		if code == fiber.StatusBadRequest {
			msg = "workspace file browser is only for non-git apps"
		}
		return c.Status(code).JSON(fiber.Map{"error": msg})
	}
	baseRel, err := normalizeWorkspaceScopeRel(c.Query("base", ""))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid base path"})
	}
	if err := p.enforcePHPPanelScopedBase(c, appID, baseRel); err != nil {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "base path not allowed"})
	}
	rel := c.Query("path", "")
	scopedRel, err := joinWorkspaceScope(baseRel, rel)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid path"})
	}
	children, err := p.Store.ListChildren(appID, scopedRel)
	if err != nil {
		if errors.Is(err, os.ErrInvalid) {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid path"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	type row struct {
		Name    string `json:"name"`
		RelPath string `json:"rel_path"`
		IsDir   bool   `json:"is_dir"`
		Size    int64  `json:"size"`
	}
	out := make([]row, 0, len(children))
	for _, ch := range children {
		out = append(out, row{Name: ch.Name, RelPath: trimWorkspaceScope(ch.RelPath, baseRel), IsDir: ch.IsDir, Size: ch.Size})
	}
	parent := trimWorkspaceScope(p.Store.ParentRel(scopedRel), baseRel)
	return c.JSON(fiber.Map{
		"path":    rel,
		"parent":  parent,
		"entries": out,
	})
}

// WorkspaceFilesBlob returns JSON with file text for the file browser / Monaco, or metadata for binary/oversized files.
func (p *Panel) WorkspaceFilesBlob(c *fiber.Ctx) error {
	appID := c.Params("id")
	if code := p.workspaceFilesGate(c, appID); code != 0 {
		return c.Status(code).JSON(fiber.Map{"error": "not available"})
	}
	baseRel, err := normalizeWorkspaceScopeRel(c.Query("base", ""))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid base path"})
	}
	if err := p.enforcePHPPanelScopedBase(c, appID, baseRel); err != nil {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "base path not allowed"})
	}
	rel := c.Query("path", "")
	if strings.TrimSpace(rel) == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "path required"})
	}
	fullRel, err := joinWorkspaceScope(baseRel, rel)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid path"})
	}
	full, err := p.Store.SafeFilePath(appID, fullRel)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid path"})
	}
	st, err := os.Stat(full)
	if err != nil {
		if os.IsNotExist(err) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "not found"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	if st.IsDir() {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "not a file"})
	}
	name := filepath.Base(rel)
	q := url.QueryEscape(rel)
	rawURL := fmt.Sprintf("/apps/%s/file?path=%s", appID, q)
	if baseRel != "" {
		rawURL += "&base=" + url.QueryEscape(baseRel)
	}
	downloadURL := rawURL + "&download=1"
	if st.Size() > int64(maxWorkspaceBlobJSON) {
		return c.JSON(fiber.Map{
			"path":          rel,
			"name":          name,
			"size":          st.Size(),
			"too_large":     true,
			"max_bytes":     maxWorkspaceBlobJSON,
			"raw_url":       rawURL,
			"download_url":  downloadURL,
		})
	}
	f, err := os.Open(full)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	defer f.Close()
	b, err := io.ReadAll(f)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	preview, isBinary := gitRepoBlobPreviewText(b)
	if isBinary {
		return c.JSON(fiber.Map{
			"path":          rel,
			"name":          name,
			"size":          st.Size(),
			"binary":        true,
			"raw_url":       rawURL,
			"download_url":  downloadURL,
		})
	}
	return c.JSON(fiber.Map{
		"path":          rel,
		"name":          name,
		"size":          st.Size(),
		"text":          preview,
		"truncated":     false,
		"binary":        false,
		"raw_url":       rawURL,
		"download_url":  downloadURL,
	})
}

type workspaceFileSaveBody struct {
	Content string `json:"content"`
}

// WorkspaceFileSave writes JSON body { "content": "..." } to the file at ?path= (non-git workspace only).
func (p *Panel) WorkspaceFileSave(c *fiber.Ctx) error {
	appID := c.Params("id")
	if code := p.workspaceFilesGate(c, appID); code != 0 {
		return c.Status(code).JSON(fiber.Map{"ok": false, "message": "save not allowed"})
	}
	baseRel, err := normalizeWorkspaceScopeRel(c.Query("base", ""))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"ok": false, "message": "invalid base path"})
	}
	if err := p.enforcePHPPanelScopedBase(c, appID, baseRel); err != nil {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"ok": false, "message": "base path not allowed"})
	}
	rel := c.Query("path", "")
	if strings.TrimSpace(rel) == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"ok": false, "message": "path required"})
	}
	var body workspaceFileSaveBody
	if err := json.Unmarshal(c.Body(), &body); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"ok": false, "message": "invalid JSON body"})
	}
	if len(body.Content) > maxWorkspaceFileSaveBytes {
		return c.Status(fiber.StatusRequestEntityTooLarge).JSON(fiber.Map{
			"ok": false, "message": fmt.Sprintf("content exceeds max size (%d bytes)", maxWorkspaceFileSaveBytes),
		})
	}
	fullRel, err := joinWorkspaceScope(baseRel, rel)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"ok": false, "message": "invalid path"})
	}
	full, err := p.Store.SafeFilePath(appID, fullRel)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"ok": false, "message": "invalid path"})
	}
	if err := os.MkdirAll(filepath.Dir(full), 0750); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"ok": false, "message": err.Error()})
	}
	p.ensurePHPPanelPublicReadable(appID, filepath.ToSlash(filepath.Dir(fullRel)), true)
	if err := os.WriteFile(full, []byte(body.Content), 0640); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"ok": false, "message": err.Error()})
	}
	p.ensurePHPPanelPublicReadable(appID, filepath.ToSlash(fullRel), false)
	return c.JSON(fiber.Map{"ok": true})
}

const maxWorkspaceZipBytes = 512 << 20 // same order as single-file download cap

// WorkspaceFilesDownloadZip streams a zip of the whole workspace or of ?paths= (comma-separated, URI-encoded rel paths).
func (p *Panel) WorkspaceFilesDownloadZip(c *fiber.Ctx) error {
	appID := c.Params("id")
	if code := p.workspaceFilesGate(c, appID); code != 0 {
		if code == fiber.StatusNotFound {
			return respondAppNotFound(c)
		}
		return c.Status(400).SendString("download zip is only for non-git apps")
	}
	baseRel, err := normalizeWorkspaceScopeRel(c.Query("base", ""))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("invalid base path")
	}
	if err := p.enforcePHPPanelScopedBase(c, appID, baseRel); err != nil {
		return c.Status(fiber.StatusForbidden).SendString("base path not allowed")
	}
	base := filepath.Clean(p.Store.Path(appID))
	if baseRel != "" {
		base, err = p.Store.SafeFilePath(appID, baseRel)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).SendString("invalid base path")
		}
	}
	c.Set("Content-Type", "application/zip")
	c.Set("Content-Disposition", `attachment; filename="workspace.zip"`)

	zw := zip.NewWriter(c.Response().BodyWriter())
	defer zw.Close()

	var written int64
	pathsParam := strings.TrimSpace(c.Query("paths"))
	if pathsParam == "" {
		err := filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(base, path)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			if rel == "." {
				return nil
			}
			if rel == ".panel-meta" || strings.HasPrefix(rel, ".panel-meta/") {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if rel == workspace.ReservedDir || strings.HasPrefix(rel, workspace.ReservedDir+"/") {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if d.IsDir() {
				return nil
			}
			n, err := zipAddFile(zw, path, rel, maxWorkspaceZipBytes-written)
			written += n
			if err != nil {
				return err
			}
			return nil
		})
		if err != nil {
			if errors.Is(err, errWorkspaceZipTooLarge) {
				return c.Status(fiber.StatusRequestEntityTooLarge).SendString("zip size limit exceeded")
			}
			return c.Status(500).SendString(err.Error())
		}
		return nil
	}

	for _, enc := range strings.Split(pathsParam, ",") {
		enc = strings.TrimSpace(enc)
		if enc == "" {
			continue
		}
		rel, err := url.QueryUnescape(enc)
		if err != nil {
			return c.Status(400).SendString("invalid paths parameter")
		}
		fullRel, err := joinWorkspaceScope(baseRel, rel)
		if err != nil {
			return c.Status(400).SendString("invalid path")
		}
		full, err := p.Store.SafeFilePath(appID, fullRel)
		if err != nil {
			return c.Status(400).SendString("invalid path")
		}
		st, err := os.Stat(full)
		if err != nil {
			return c.Status(404).SendString("path not found")
		}
		arcName := filepath.ToSlash(strings.Trim(rel, "/"))
		if st.IsDir() {
			err = filepath.WalkDir(full, func(path string, d os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if d.IsDir() {
					return nil
				}
				sub, err := filepath.Rel(full, path)
				if err != nil {
					return err
				}
				sub = filepath.ToSlash(sub)
				nameInZip := sub
				if arcName != "" {
					nameInZip = arcName + "/" + sub
				}
				n, err := zipAddFile(zw, path, nameInZip, maxWorkspaceZipBytes-written)
				written += n
				return err
			})
		} else {
			var n int64
			n, err = zipAddFile(zw, full, arcName, maxWorkspaceZipBytes-written)
			written += n
		}
		if err != nil {
			if errors.Is(err, errWorkspaceZipTooLarge) {
				return c.Status(fiber.StatusRequestEntityTooLarge).SendString("zip size limit exceeded")
			}
			return c.Status(500).SendString(err.Error())
		}
	}
	return nil
}

func zipAddFile(zw *zip.Writer, absPath, nameInZip string, budget int64) (written int64, err error) {
	st, err := os.Stat(absPath)
	if err != nil {
		return 0, err
	}
	if st.Size() > budget {
		return 0, errWorkspaceZipTooLarge
	}
	f, err := os.Open(absPath)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	w, err := zw.Create(nameInZip)
	if err != nil {
		return 0, err
	}
	n, err := io.Copy(w, f)
	return n, err
}

func (p *Panel) WorkspaceFilesCreateZip(c *fiber.Ctx) error {
	appID := c.Params("id")
	if code := p.workspaceFilesGate(c, appID); code != 0 {
		return c.Status(code).JSON(fiber.Map{"ok": false, "message": "zip not available"})
	}
	baseRel, err := normalizeWorkspaceScopeRel(c.Query("base", ""))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"ok": false, "message": "invalid base path"})
	}
	if err := p.enforcePHPPanelScopedBase(c, appID, baseRel); err != nil {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"ok": false, "message": "base path not allowed"})
	}
	zipName := strings.TrimSpace(c.FormValue("zip_name"))
	if zipName == "" {
		zipName = "archive.zip"
	}
	if !strings.HasSuffix(strings.ToLower(zipName), ".zip") {
		zipName += ".zip"
	}
	targetRel, err := joinWorkspaceScope(baseRel, c.FormValue("path"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"ok": false, "message": "invalid target path"})
	}
	paths := formValues(c, "paths")
	if len(paths) == 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"ok": false, "message": "select at least one file or folder"})
	}
	zipRel := strings.Trim(zipName, "/")
	if targetRel != "" {
		zipRel = strings.Trim(targetRel+"/"+zipRel, "/")
	}
	zipAbs, err := p.Store.SafeFilePath(appID, zipRel)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"ok": false, "message": "invalid zip name"})
	}
	if err := os.MkdirAll(filepath.Dir(zipAbs), 0750); err != nil {
		return c.Status(500).JSON(fiber.Map{"ok": false, "message": err.Error()})
	}
	out, err := os.OpenFile(zipAbs, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0640)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"ok": false, "message": err.Error()})
	}
	defer out.Close()
	zw := zip.NewWriter(out)
	var written int64
	for _, rel := range paths {
		fullRel, err := joinWorkspaceScope(baseRel, rel)
		if err != nil {
			_ = zw.Close()
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"ok": false, "message": "invalid selected path"})
		}
		full, err := p.Store.SafeFilePath(appID, fullRel)
		if err != nil {
			_ = zw.Close()
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"ok": false, "message": "invalid selected path"})
		}
		st, err := os.Stat(full)
		if err != nil {
			_ = zw.Close()
			return c.Status(404).JSON(fiber.Map{"ok": false, "message": "path not found"})
		}
		arcName := filepath.ToSlash(strings.Trim(rel, "/"))
		if st.IsDir() {
			err = filepath.WalkDir(full, func(path string, d os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if d.IsDir() {
					return nil
				}
				sub, err := filepath.Rel(full, path)
				if err != nil {
					return err
				}
				sub = filepath.ToSlash(sub)
				nameInZip := sub
				if arcName != "" {
					nameInZip = arcName + "/" + sub
				}
				n, err := zipAddFile(zw, path, nameInZip, maxWorkspaceZipBytes-written)
				written += n
				return err
			})
		} else {
			var n int64
			n, err = zipAddFile(zw, full, arcName, maxWorkspaceZipBytes-written)
			written += n
		}
		if err != nil {
			_ = zw.Close()
			if errors.Is(err, errWorkspaceZipTooLarge) {
				return c.Status(fiber.StatusRequestEntityTooLarge).JSON(fiber.Map{"ok": false, "message": "zip size limit exceeded"})
			}
			return c.Status(500).JSON(fiber.Map{"ok": false, "message": err.Error()})
		}
	}
	if err := zw.Close(); err != nil {
		return c.Status(500).JSON(fiber.Map{"ok": false, "message": err.Error()})
	}
	return c.JSON(fiber.Map{"ok": true, "message": "ZIP created.", "path": trimWorkspaceScope(zipRel, baseRel)})
}

func (p *Panel) WorkspaceFilesExtractZip(c *fiber.Ctx) error {
	appID := c.Params("id")
	if code := p.workspaceFilesGate(c, appID); code != 0 {
		return c.Status(code).JSON(fiber.Map{"ok": false, "message": "unzip not available"})
	}
	baseRel, err := normalizeWorkspaceScopeRel(c.Query("base", ""))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"ok": false, "message": "invalid base path"})
	}
	if err := p.enforcePHPPanelScopedBase(c, appID, baseRel); err != nil {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"ok": false, "message": "base path not allowed"})
	}
	zipPath := strings.TrimSpace(c.FormValue("zip_path"))
	if zipPath == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"ok": false, "message": "zip file required"})
	}
	targetRel, err := joinWorkspaceScope(baseRel, c.FormValue("path"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"ok": false, "message": "invalid target path"})
	}
	fullRel, err := joinWorkspaceScope(baseRel, zipPath)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"ok": false, "message": "invalid zip path"})
	}
	zipAbs, err := p.Store.SafeFilePath(appID, fullRel)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"ok": false, "message": "invalid zip path"})
	}
	st, err := os.Stat(zipAbs)
	if err != nil {
		if os.IsNotExist(err) {
			return c.Status(404).JSON(fiber.Map{"ok": false, "message": "zip not found"})
		}
		return c.Status(500).JSON(fiber.Map{"ok": false, "message": err.Error()})
	}
	if st.IsDir() {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"ok": false, "message": "zip path points to a folder"})
	}
	if !strings.HasSuffix(strings.ToLower(st.Name()), ".zip") {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"ok": false, "message": "only .zip files can be extracted"})
	}
	destBase := p.Store.Path(appID)
	if targetRel != "" {
		destBase, err = p.Store.SafeFilePath(appID, targetRel)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"ok": false, "message": "invalid target path"})
		}
	}
	if err := os.MkdirAll(destBase, 0750); err != nil {
		return c.Status(500).JSON(fiber.Map{"ok": false, "message": err.Error()})
	}
	p.ensurePHPPanelPublicReadable(appID, filepath.ToSlash(targetRel), true)
	f, err := os.Open(zipAbs)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"ok": false, "message": err.Error()})
	}
	defer f.Close()
	zr, err := zip.NewReader(f, st.Size())
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"ok": false, "message": "invalid zip file"})
	}
	cleanBase := filepath.Clean(destBase)
	for _, item := range zr.File {
		itemPath := filepath.Clean(item.Name)
		if itemPath == "." || strings.HasPrefix(itemPath, "..") {
			continue
		}
		dest := filepath.Join(destBase, itemPath)
		relToBase, err := filepath.Rel(cleanBase, filepath.Clean(dest))
		if err != nil || relToBase == ".." || strings.HasPrefix(relToBase, ".."+string(os.PathSeparator)) {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"ok": false, "message": "zip contains invalid paths"})
		}
		if item.FileInfo().IsDir() {
			if err := os.MkdirAll(dest, 0750); err != nil {
				return c.Status(500).JSON(fiber.Map{"ok": false, "message": err.Error()})
			}
			relDest, relErr := filepath.Rel(p.Store.Path(appID), dest)
			if relErr == nil {
				p.ensurePHPPanelPublicReadable(appID, filepath.ToSlash(relDest), true)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0750); err != nil {
			return c.Status(500).JSON(fiber.Map{"ok": false, "message": err.Error()})
		}
		rc, err := item.Open()
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"ok": false, "message": err.Error()})
		}
		out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0640)
		if err != nil {
			_ = rc.Close()
			return c.Status(500).JSON(fiber.Map{"ok": false, "message": err.Error()})
		}
		_, copyErr := io.Copy(out, rc)
		_ = out.Close()
		_ = rc.Close()
		if copyErr != nil {
			return c.Status(500).JSON(fiber.Map{"ok": false, "message": copyErr.Error()})
		}
		relDest, relErr := filepath.Rel(p.Store.Path(appID), dest)
		if relErr == nil {
			p.ensurePHPPanelPublicReadable(appID, filepath.ToSlash(relDest), false)
		}
	}
	return c.JSON(fiber.Map{"ok": true, "message": "ZIP extracted."})
}
