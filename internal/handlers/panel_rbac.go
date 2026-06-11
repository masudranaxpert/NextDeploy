package handlers

import (
	"context"
	"database/sql"
	"errors"
	"panel/internal/db"
	"strings"

	"github.com/gofiber/fiber/v2"
)

func (p *Panel) CanAccessApp(ctx context.Context, userID int64, userRole string, appID string, requiredRole string) (bool, error) {
	if userRole == db.RoleAdmin {
		return true, nil
	}
	app, err := p.DB.GetApp(ctx, appID)
	if err != nil {
		return false, err
	}
	if app.OwnerID == userID {
		return true, nil
	}
	collab, err := p.DB.GetCollaborator(ctx, appID, userID)
	if err == nil {
		if requiredRole == "admin" {
			return false, nil
		}
		if requiredRole == "developer" && collab.Role != db.CollabRoleDeveloper {
			return false, nil
		}
		return true, nil
	}
	return false, nil
}

func (p *Panel) RequireAppAccess(c *fiber.Ctx, appID string, requiredRole string) (db.App, error) {
	ctx := c.UserContext()
	u, ok := currentUser(c)
	if !ok {
		return db.App{}, c.Status(401).SendString("unauthorized")
	}
	app, err := p.DB.GetApp(ctx, appID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return db.App{}, c.Status(404).SendString("app not found")
		}
		return db.App{}, c.Status(500).SendString(err.Error())
	}
	allowed, err := p.CanAccessApp(ctx, u.ID, u.Role, appID, requiredRole)
	if err != nil {
		return db.App{}, c.Status(500).SendString(err.Error())
	}
	if !allowed {
		return db.App{}, c.Status(403).SendString("forbidden")
	}
	return app, nil
}

func (p *Panel) AppAccessMiddleware(c *fiber.Ctx) error {
	id := c.Params("id")
	if id == "" {
		return c.Next()
	}
	u, ok := currentUser(c)
	if !ok {
		return c.Status(401).SendString("unauthorized")
	}

	requiredRole := db.CollabRoleViewer
	if c.Method() != "GET" {
		requiredRole = db.CollabRoleDeveloper
	}

	path := strings.ToLower(c.Path())
	if strings.HasSuffix(path, "/delete") && !strings.Contains(path, "/domains/") && !strings.Contains(path, "/files/") && !strings.Contains(path, "/deploy-logs/") {
		if path == "/apps/"+strings.ToLower(id)+"/delete" || path == "/apps/"+strings.ToLower(id) {
			requiredRole = "admin"
		}
	}

	allowed, err := p.CanAccessApp(c.UserContext(), u.ID, u.Role, id, requiredRole)
	if err != nil {
		return c.Status(500).SendString(err.Error())
	}
	if !allowed {
		return c.Status(403).SendString("forbidden")
	}

	if requiredRole != db.CollabRoleViewer {
		app, err := p.DB.GetApp(c.UserContext(), id)
		if err == nil && app.Status == db.AppStatusSuspended && u.Role != db.RoleAdmin {
			return c.Status(403).SendString("This app has been suspended and cannot be modified.")
		}
	}

	return c.Next()
}
