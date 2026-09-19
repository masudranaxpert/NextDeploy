package handlers

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"panel/internal/db"

	"github.com/gofiber/fiber/v2"
)

// APIAuthMiddleware authenticates requests to /api/ routes using Bearer token, X-API-Key, or session cookie.
func (p *Panel) APIAuthMiddleware(c *fiber.Ctx) error {
	authHeader := c.Get("Authorization")
	var token string
	if strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
		token = strings.TrimSpace(authHeader[7:])
	}
	if token == "" {
		token = strings.TrimSpace(c.Get("X-API-Key"))
	}

	ctx := c.UserContext()
	if token != "" {
		user, apiToken, err := p.DB.ValidateAPIToken(ctx, token)
		if err == nil && user.ID > 0 {
			c.Locals(contextUserKey, user)
			c.Locals("api_token", apiToken)
			return c.Next()
		}
	}

	// Fallback: check web session cookie
	if sessToken := c.Cookies(sessionCookie); sessToken != "" {
		userID, expiresAt, err := p.DB.GetSession(ctx, sessToken)
		if err == nil && time.Now().Before(expiresAt) {
			user, err := p.DB.GetUserByID(ctx, userID)
			if err == nil && user.Status != db.UserStatusSuspended {
				c.Locals(contextUserKey, user)
				return c.Next()
			}
		}
	}

	return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
		"error": "unauthorized: valid Bearer token or active session required",
	})
}

// UploadWorkspaceArchive handles archive extraction directly into an app's workspace.
func (p *Panel) UploadWorkspaceArchive(c *fiber.Ctx) error {
	ctx := c.UserContext()
	u, ok := currentUser(c)
	if !ok {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}

	appID := strings.TrimSpace(c.Params("id"))
	if appID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "app_id is required"})
	}

	app, err := p.DB.GetApp(ctx, appID)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": fmt.Sprintf("app %q not found", appID)})
	}

	allowed, err := p.CanAccessApp(ctx, u.ID, u.Role, appID, db.CollabRoleDeveloper)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	if !allowed {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "forbidden: developer access required"})
	}

	if app.Status == db.AppStatusSuspended && u.Role != db.RoleAdmin {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "forbidden: app is suspended"})
	}

	var reader io.Reader
	var filename string

	// Handle multipart form file upload (-F archive=@...)
	fileHeader, err := c.FormFile("archive")
	if err == nil && fileHeader != nil {
		f, ferr := fileHeader.Open()
		if ferr != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": fmt.Sprintf("failed to read archive file: %v", ferr)})
		}
		defer f.Close()
		reader = f
		filename = fileHeader.Filename
	} else {
		// Fallback: handle raw request body upload
		body := c.Body()
		if len(body) == 0 {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "no archive uploaded; pass form field 'archive' or binary body"})
		}
		reader = bytes.NewReader(body)
		filename = "archive.tar.gz"
	}

	count, totalBytes, err := p.Store.ExtractArchive(appID, reader, filename)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": fmt.Sprintf("archive extraction failed: %v", err),
		})
	}

	p.InvalidateAfterAppWorkspaceChange(appID)
	_ = p.SyncAppCaddyOverrideCtx(ctx, appID)

	p.RecordAuditLog(c, "upload_workspace_archive", "app", appID,
		fmt.Sprintf("Extracted %d file(s) (%d bytes) from archive %s", count, totalBytes, filename))

	return c.JSON(fiber.Map{
		"ok":              true,
		"app_id":          appID,
		"files_extracted": count,
		"bytes":           totalBytes,
		"message":         fmt.Sprintf("Successfully extracted %d file(s) (%d bytes) to workspace", count, totalBytes),
	})
}

// DownloadWorkspaceArchive packages and streams the app's workspace as a tar.gz archive.
// GET /api/v1/apps/:id/workspace/archive?path=
func (p *Panel) DownloadWorkspaceArchive(c *fiber.Ctx) error {
	ctx := c.UserContext()
	u, ok := currentUser(c)
	if !ok {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}

	appID := strings.TrimSpace(c.Params("id"))
	if appID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "app_id is required"})
	}

	app, err := p.DB.GetApp(ctx, appID)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": fmt.Sprintf("app %q not found", appID)})
	}

	allowed, err := p.CanAccessApp(ctx, u.ID, u.Role, appID, db.CollabRoleViewer)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	if !allowed {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "forbidden: viewer access required"})
	}

	if app.Status == db.AppStatusSuspended && u.Role != db.RoleAdmin {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "forbidden: app is suspended"})
	}

	isGit := p.IsGitApp(ctx, appID)
	base := p.Store.Path(appID)
	if isGit {
		base = filepath.Clean(filepath.Join(p.Store.ReservedPath(appID), "repo"))
	}

	targetDir := base
	reqPath := strings.TrimSpace(c.Query("path"))
	if reqPath != "" {
		cleanRel := filepath.ToSlash(strings.Trim(reqPath, "/"))
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

	fi, err := os.Stat(targetDir)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "workspace directory not found on disk"})
	}

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	if !fi.IsDir() {
		// Single file download
		rel, rerr := filepath.Rel(base, targetDir)
		if rerr != nil {
			rel = filepath.Base(targetDir)
		}
		hdr := &tar.Header{
			Name:    filepath.ToSlash(rel),
			Mode:    int64(fi.Mode().Perm()),
			Size:    fi.Size(),
			ModTime: fi.ModTime(),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			_ = gz.Close()
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
		}
		f, err := os.Open(targetDir)
		if err != nil {
			_ = gz.Close()
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
		}
		_, _ = io.Copy(tw, f)
		_ = f.Close()
	} else {
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

			// Skip internal/hidden directories
			if d.IsDir() {
				if name == ".git" || name == ".panel-meta" || name == ".nextdeploy" || name == "node_modules" || name == "__pycache__" || name == ".venv" {
					return filepath.SkipDir
				}
				return nil
			}

			// Exclude sensitive environment secrets from public pull/archive
			if name == ".env" || strings.HasPrefix(name, ".env.") {
				return nil
			}

			info, err := d.Info()
			if err != nil {
				return nil
			}

			hdr := &tar.Header{
				Name:    relSlash,
				Mode:    int64(info.Mode().Perm()),
				Size:    info.Size(),
				ModTime: info.ModTime(),
			}
			if err := tw.WriteHeader(hdr); err != nil {
				return err
			}
			f, err := os.Open(pth)
			if err != nil {
				return nil
			}
			_, _ = io.Copy(tw, f)
			_ = f.Close()
			return nil
		})
		if walkErr != nil {
			_ = gz.Close()
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": walkErr.Error()})
		}
	}

	_ = tw.Close()
	_ = gz.Close()

	c.Set("Content-Type", "application/gzip")
	c.Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s-workspace.tar.gz\"", appID))
	return c.Send(buf.Bytes())
}
