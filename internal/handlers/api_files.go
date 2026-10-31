package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"panel/internal/db"
	"panel/internal/sandbox"
	"panel/internal/workspace"

	"github.com/gofiber/fiber/v2"
)

// FileItem represents a single file or directory in API responses.
type FileItem struct {
	Name    string `json:"name"`
	RelPath string `json:"rel_path"`
	Path    string `json:"path"`
	IsDir   bool   `json:"is_dir"`
	Type    string `json:"type"`
	Size    int64  `json:"size"`
	ModTime int64  `json:"mod_time"`
	Perms   string `json:"perms,omitempty"`
}

// APIAppFilesList returns the list of files in an app workspace or git repo.
// maxRecursiveListEntries caps one recursive listing; folders deeper in a huge tree load when opened.
const maxRecursiveListEntries = 20000

// GET /api/v1/apps/:id/files?path=&recursive=true
func (p *Panel) APIAppFilesList(c *fiber.Ctx) error {
	ctx := c.UserContext()
	u, ok := currentUser(c)
	if !ok {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}

	appID := strings.TrimSpace(c.Params("id"))
	if appID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "app id required"})
	}

	allowed, err := p.CanAccessApp(ctx, u.ID, u.Role, appID, db.CollabRoleDeveloper)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	if !allowed {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "access denied"})
	}

	reqPath := ResolveSafePathQuery(c)
	recursive := c.Query("recursive") == "true" || c.Query("recursive") == "1"
	isGit := p.IsGitApp(ctx, appID)

	var out []FileItem

	if !recursive {
		var children []workspace.FileEntry
		var listErr error
		if isGit {
			children, listErr = p.Store.ListGitRepoChildren(appID, reqPath)
		} else {
			children, listErr = p.Store.ListChildren(appID, reqPath)
		}
		if listErr != nil {
			if errors.Is(listErr, os.ErrInvalid) {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid path"})
			}
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": listErr.Error()})
		}

		for _, ch := range children {
			fileType := "file"
			if ch.IsDir {
				fileType = "directory"
			}
			out = append(out, FileItem{
				Name:    ch.Name,
				RelPath: ch.RelPath,
				Path:    ch.RelPath,
				IsDir:   ch.IsDir,
				Type:    fileType,
				Size:    ch.Size,
				ModTime: ch.ModTime.Unix(),
				Perms:   ch.Perms,
			})
		}
		if out == nil {
			out = []FileItem{}
		}
		return c.JSON(out)
	}

	// Recursive file traversal
	base := p.Store.Path(appID)
	if isGit {
		base = filepath.Clean(filepath.Join(p.Store.ReservedPath(appID), "repo"))
	}
	targetDir := base
	cleanRel := filepath.ToSlash(strings.Trim(reqPath, "/"))
	if cleanRel != "" {
		var safe string
		var pathErr error
		if isGit {
			safe, pathErr = p.Store.SafeGitRepoFilePath(appID, cleanRel)
		} else {
			safe, pathErr = p.Store.SafeFilePath(appID, cleanRel)
		}
		if pathErr != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid path: " + pathErr.Error()})
		}
		targetDir = safe
	}

	walkErr := filepath.WalkDir(targetDir, func(pth string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, err := filepath.Rel(base, pth)
		if err != nil || rel == "." {
			return nil
		}
		relSlash := filepath.ToSlash(rel)
		name := d.Name()

		// Skip internal panel metadata and git directories, but allow .env and other dotfiles
		if name == ".panel-meta" || name == ".nextdeploy" || name == ".nextdeploy.generated.compose.yml" || name == ".git" {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return nil
		}

		fileType := "file"
		if d.IsDir() {
			fileType = "directory"
		}

		out = append(out, FileItem{
			Name:    name,
			RelPath: relSlash,
			Path:    relSlash,
			IsDir:   d.IsDir(),
			Type:    fileType,
			Size:    info.Size(),
			ModTime: info.ModTime().Unix(),
			Perms:   fmt.Sprintf("%03o", info.Mode().Perm()),
		})
		if len(out) >= maxRecursiveListEntries {
			return filepath.SkipAll // a huge tree must not balloon one response
		}
		return nil
	})

	if walkErr != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": walkErr.Error()})
	}

	if out == nil {
		out = []FileItem{}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].IsDir != out[j].IsDir {
			return out[i].IsDir
		}
		return out[i].RelPath < out[j].RelPath
	})

	return c.JSON(out)
}

// APIAppFileContent returns file contents directly (raw or json).
// GET /api/v1/apps/:id/files/content?path=&json=true
func (p *Panel) APIAppFileContent(c *fiber.Ctx) error {
	ctx := c.UserContext()
	u, ok := currentUser(c)
	if !ok {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}

	appID := strings.TrimSpace(c.Params("id"))
	if appID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "app id required"})
	}

	allowed, err := p.CanAccessApp(ctx, u.ID, u.Role, appID, db.CollabRoleDeveloper)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	if !allowed {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "access denied"})
	}

	reqPath := ResolveSafePathQuery(c)
	if reqPath == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "path query parameter required"})
	}

	var full string
	var pathErr error
	if p.IsGitApp(ctx, appID) {
		full, pathErr = p.Store.SafeGitRepoFilePath(appID, reqPath)
	} else if p.DB.IsPHPSite(ctx, appID) {
		clean := filepath.ToSlash(strings.Trim(reqPath, "/"))
		if !strings.HasPrefix(clean, "htdocs/") && clean != "htdocs" {
			clean = "htdocs/" + clean
		}
		full, pathErr = p.Store.SafeFilePath(appID, clean)
	} else {
		full, pathErr = p.Store.SafeFilePath(appID, reqPath)
	}
	if pathErr != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid path: " + pathErr.Error()})
	}

	st, err := os.Stat(full)
	if err != nil {
		if os.IsNotExist(err) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "file not found"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	if st.IsDir() {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "specified path is a directory, not a file"})
	}

	const maxFileSize = 50 * 1024 * 1024 // 50 MB
	if st.Size() > maxFileSize {
		return c.Status(fiber.StatusRequestEntityTooLarge).JSON(fiber.Map{
			"error": fmt.Sprintf("file size (%d bytes) exceeds 50MB limit", st.Size()),
		})
	}

	b, err := os.ReadFile(full)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	wantJSON := c.Query("json") == "true" || c.Query("json") == "1"
	if wantJSON {
		isBinary := false
		contentType := http.DetectContentType(b)
		if !strings.HasPrefix(contentType, "text/") && !strings.Contains(contentType, "json") && !strings.Contains(contentType, "xml") {
			isBinary = true
		}
		var contentStr string
		if !isBinary {
			contentStr = string(b)
		}
		return c.JSON(fiber.Map{
			"path":         reqPath,
			"size":         st.Size(),
			"is_binary":    isBinary,
			"content_type": contentType,
			"content":      contentStr,
			"mod_time":     st.ModTime().Unix(),
		})
	}

	// Stream raw file content with mime type
	contentType := http.DetectContentType(b)
	c.Set("Content-Type", contentType)
	c.Set("Content-Length", fmt.Sprintf("%d", len(b)))
	return c.Send(b)
}

// APIAppFileSave saves or updates a workspace file.
// POST /api/v1/apps/:id/files/content
func (p *Panel) APIAppFileSave(c *fiber.Ctx) error {
	ctx := c.UserContext()
	u, ok := currentUser(c)
	if !ok {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}

	appID := strings.TrimSpace(c.Params("id"))
	if appID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "app id required"})
	}

	allowed, err := p.CanAccessApp(ctx, u.ID, u.Role, appID, db.CollabRoleDeveloper)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	if !allowed {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "access denied: developer role required to modify workspace"})
	}

	app, err := p.DB.GetApp(ctx, appID)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "app not found"})
	}

	var reqPath string
	var content string

	// Check if JSON body
	if strings.Contains(c.Get("Content-Type"), "application/json") {
		var body struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		}
		if err := json.Unmarshal(c.Body(), &body); err == nil {
			reqPath = strings.TrimSpace(body.Path)
			content = body.Content
		}
	}

	if reqPath == "" {
		reqPath = ResolveSafePathQuery(c)
		if reqPath == "" {
			reqPath = strings.TrimSpace(c.FormValue("path"))
		}
		if content == "" && len(c.Body()) > 0 {
			content = string(c.Body())
		}
	}

	if reqPath == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "path required (in json body or query param)"})
	}

	baseName := filepath.Base(reqPath)
	if baseName == ".nextdeploy.generated.compose.yml" || baseName == ".panel-meta" || baseName == ".nextdeploy" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "cannot modify internal managed files"})
	}

	cleanRel := filepath.ToSlash(strings.Trim(reqPath, "/"))
	if cleanRel == "docker-compose.yml" || cleanRel == "docker-compose.yaml" || cleanRel == "compose.yml" || cleanRel == "compose.yaml" || cleanRel == app.ComposeFile {
		hostWs := ""
		if p.Store != nil {
			hostWs = p.Store.Path(appID)
		}
		if err := sandbox.CheckComposeSecurity([]byte(content), hostWs); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "compose security check failed: " + err.Error()})
		}
	}

	var full string
	var pathErr error
	if p.IsGitApp(ctx, appID) {
		full, pathErr = p.Store.SafeGitRepoFilePath(appID, reqPath)
	} else if p.DB.IsPHPSite(ctx, appID) {
		clean := filepath.ToSlash(strings.Trim(reqPath, "/"))
		if !strings.HasPrefix(clean, "htdocs/") && clean != "htdocs" {
			clean = "htdocs/" + clean
		}
		full, pathErr = p.Store.SafeFilePath(appID, clean)
	} else {
		full, pathErr = p.Store.SafeFilePath(appID, reqPath)
	}
	if pathErr != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid path: " + pathErr.Error()})
	}

	if err := os.MkdirAll(filepath.Dir(full), 0750); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed creating directory: " + err.Error()})
	}
	if err := os.WriteFile(full, []byte(content), 0640); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed writing file: " + err.Error()})
	}

	if p.DB.IsPHPSite(ctx, appID) && p.PHP != nil {
		p.PHP.DebounceFixOwnership(appID)
	}

	p.InvalidateAfterAppWorkspaceChange(appID)

	if cleanRel == "docker-compose.yml" || cleanRel == "docker-compose.yaml" || cleanRel == app.ComposeFile {
		_ = p.SyncAppCaddyOverrideCtx(ctx, appID)
	}
	if cleanRel == ".env" {
		_ = p.DB.UpdatePanelEnv(ctx, appID, content)
	}

	p.RecordAuditLog(c, "save_workspace_file", "app", appID, fmt.Sprintf("Updated %s via API", reqPath))

	return c.JSON(fiber.Map{
		"ok":      true,
		"path":    reqPath,
		"bytes":   len(content),
		"message": fmt.Sprintf("Successfully saved %s (%d bytes)", reqPath, len(content)),
	})
}

// APIAppFileDelete deletes a file or directory in the app workspace.
// DELETE /api/v1/apps/:id/files?path=
func (p *Panel) APIAppFileDelete(c *fiber.Ctx) error {
	ctx := c.UserContext()
	u, ok := currentUser(c)
	if !ok {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}

	appID := strings.TrimSpace(c.Params("id"))
	if appID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "app id required"})
	}

	allowed, err := p.CanAccessApp(ctx, u.ID, u.Role, appID, db.CollabRoleDeveloper)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	if !allowed {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "access denied: developer role required to modify workspace"})
	}

	reqPath := ResolveSafePathQuery(c)
	if reqPath == "" {
		var body struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(c.Body(), &body); err == nil {
			reqPath = strings.TrimSpace(body.Path)
		}
	}
	if reqPath == "" {
		reqPath = strings.TrimSpace(c.FormValue("path"))
	}
	if reqPath == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "path required"})
	}

	baseName := filepath.Base(reqPath)
	if baseName == ".nextdeploy.generated.compose.yml" || baseName == ".panel-meta" || baseName == ".nextdeploy" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "cannot delete internal managed files"})
	}

	var delErr error
	if p.IsGitApp(ctx, appID) {
		delErr = p.Store.RemoveGitRepoRel(appID, reqPath)
	} else if p.DB.IsPHPSite(ctx, appID) {
		clean := filepath.ToSlash(strings.Trim(reqPath, "/"))
		if !strings.HasPrefix(clean, "htdocs/") && clean != "htdocs" {
			clean = "htdocs/" + clean
		}
		delErr = p.Store.RemoveRel(appID, clean)
		if p.PHP != nil {
			p.PHP.DebounceFixOwnership(appID)
		}
	} else {
		delErr = p.Store.RemoveRel(appID, reqPath)
	}

	if delErr != nil {
		if errors.Is(delErr, os.ErrInvalid) {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid path"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "delete failed: " + delErr.Error()})
	}

	p.InvalidateAfterAppWorkspaceChange(appID)
	cleanDel := filepath.ToSlash(strings.Trim(reqPath, "/"))
	if cleanDel == ".env" {
		_ = p.DB.UpdatePanelEnv(ctx, appID, "")
	}
	p.RecordAuditLog(c, "delete_workspace_file", "app", appID, fmt.Sprintf("Deleted %s via API", reqPath))

	return c.JSON(fiber.Map{
		"ok":      true,
		"path":    reqPath,
		"message": fmt.Sprintf("Successfully deleted %s", reqPath),
	})
}
