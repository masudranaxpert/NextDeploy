package filebrowser

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"panel/internal/handlers/utils"
	"path/filepath"
	"strings"
	"time"

	"panel/internal/workspace"

	"github.com/gofiber/fiber/v2"
)

const maxWorkspaceBlobJSON = 512 << 10

const maxWorkspaceFileSaveBytes = 2 << 20

var errWorkspaceZipTooLarge = errors.New("workspace zip exceeds size limit")

func (h *Handler) workspaceFilesGate(c *fiber.Ctx, appID string) int {
	ctx, cancel := context.WithTimeout(c.UserContext(), 30*time.Second)
	defer cancel()
	if _, err := h.p.DB.GetApp(ctx, appID); err != nil {
		return fiber.StatusNotFound
	}
	isGit, _, _ := h.p.AppGitMetadata(ctx, appID)
	if isGit {
		return fiber.StatusBadRequest
	}
	return 0
}

func (h *Handler) WorkspaceFilesTree(c *fiber.Ctx) error {
	appID := c.Params("id")
	if code := h.workspaceFilesGate(c, appID); code != 0 {
		msg := "not available"
		if code == fiber.StatusNotFound {
			msg = "app not found"
		}
		if code == fiber.StatusBadRequest {
			msg = "workspace file browser is only for non-git apps"
		}
		return c.Status(code).JSON(fiber.Map{"error": msg})
	}
	rel := c.Query("path", "")
	children, err := h.p.Store.ListChildren(appID, rel)
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
		ModTime int64  `json:"mod_ts"`
		Perms   string `json:"perms"`
	}
	out := make([]row, 0, len(children))
	for _, ch := range children {
		out = append(out, row{
			Name:    ch.Name,
			RelPath: ch.RelPath,
			IsDir:   ch.IsDir,
			Size:    ch.Size,
			ModTime: ch.ModTime.Unix() * 1000,
			Perms:   ch.Perms,
		})
	}
	parent := h.p.Store.ParentRel(rel)
	return c.JSON(fiber.Map{
		"path":    rel,
		"parent":  parent,
		"entries": out,
	})
}

func (h *Handler) WorkspaceFilesBlob(c *fiber.Ctx) error {
	appID := c.Params("id")
	if code := h.workspaceFilesGate(c, appID); code != 0 {
		return c.Status(code).JSON(fiber.Map{"error": "not available"})
	}
	rel := c.Query("path", "")
	if strings.TrimSpace(rel) == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "path required"})
	}
	full, err := h.p.Store.SafeFilePath(appID, rel)
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
	downloadURL := rawURL + "&download=1"
	if st.Size() > int64(maxWorkspaceBlobJSON) {
		return c.JSON(fiber.Map{
			"path":         rel,
			"name":         name,
			"size":         st.Size(),
			"too_large":    true,
			"max_bytes":    maxWorkspaceBlobJSON,
			"raw_url":      rawURL,
			"download_url": downloadURL,
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
	preview, isBinary := utils.GitRepoBlobPreviewText(b)
	if isBinary {
		return c.JSON(fiber.Map{
			"path":         rel,
			"name":         name,
			"size":         st.Size(),
			"binary":       true,
			"raw_url":      rawURL,
			"download_url": downloadURL,
		})
	}
	return c.JSON(fiber.Map{
		"path":         rel,
		"name":         name,
		"size":         st.Size(),
		"text":         preview,
		"truncated":    false,
		"binary":       false,
		"raw_url":      rawURL,
		"download_url": downloadURL,
	})
}

type workspaceFileSaveBody struct {
	Content string `json:"content"`
}

func (h *Handler) WorkspaceFileSave(c *fiber.Ctx) error {
	appID := c.Params("id")
	if code := h.workspaceFilesGate(c, appID); code != 0 {
		return c.Status(code).JSON(fiber.Map{"ok": false, "message": "save not allowed"})
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
	full, err := h.p.Store.SafeFilePath(appID, rel)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"ok": false, "message": "invalid path"})
	}
	if err := os.MkdirAll(filepath.Dir(full), 0750); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"ok": false, "message": err.Error()})
	}
	if err := os.WriteFile(full, []byte(body.Content), 0640); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"ok": false, "message": err.Error()})
	}
	return c.JSON(fiber.Map{"ok": true})
}

const maxWorkspaceZipBytes = 512 << 20

func (h *Handler) WorkspaceFilesDownloadZip(c *fiber.Ctx) error {
	appID := c.Params("id")
	if code := h.workspaceFilesGate(c, appID); code != 0 {
		if code == fiber.StatusNotFound {
			return utils.RespondAppNotFound(c)
		}
		return c.Status(400).SendString("download zip is only for non-git apps")
	}
	base := filepath.Clean(h.p.Store.Path(appID))
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
		full, err := h.p.Store.SafeFilePath(appID, rel)
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
