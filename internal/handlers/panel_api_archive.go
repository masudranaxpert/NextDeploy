package handlers

import (
	"bytes"
	"fmt"
	"io"
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
