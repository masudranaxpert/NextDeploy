package handlers

import (
	"fmt"
	"strings"

	"panel/internal/db"
	"panel/internal/handlers/utils"

	"github.com/gofiber/fiber/v2"
)

func (p *Panel) AddCollaborator(c *fiber.Ctx) error {
	ctx := c.UserContext()
	appID := c.Params("id")
	u, ok := c.Locals("auth_user").(db.User)
	if !ok {
		return c.Status(fiber.StatusUnauthorized).SendString("Unauthorized")
	}

	app, err := p.DB.GetApp(ctx, appID)
	if err != nil {
		return c.Status(fiber.StatusNotFound).SendString("App not found")
	}

	if u.Role != db.RoleAdmin && app.OwnerID != u.ID {
		return c.Status(fiber.StatusForbidden).SendString("Forbidden")
	}

	collabUsername := strings.TrimSpace(c.FormValue("username"))
	role := strings.TrimSpace(c.FormValue("role"))
	if role != db.CollabRoleDeveloper && role != db.CollabRoleViewer {
		role = db.CollabRoleViewer
	}

	targetUser, err := p.DB.GetUserByUsername(ctx, collabUsername)
	if err != nil {
		if WantsJSON(c) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"ok": false, "error": "User not found"})
		}
		utils.SetFlash(c, "Error: User not found.")
		return c.Redirect(fmt.Sprintf("/apps/%s?tab=collaborators", appID))
	}

	if targetUser.ID == app.OwnerID {
		if WantsJSON(c) {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"ok": false, "error": "User is already the owner of this app"})
		}
		utils.SetFlash(c, "Error: User is already the owner of this app.")
		return c.Redirect(fmt.Sprintf("/apps/%s?tab=collaborators", appID))
	}

	if err := p.DB.AddCollaborator(ctx, appID, targetUser.ID, role); err != nil {
		if WantsJSON(c) {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"ok": false, "error": err.Error()})
		}
		utils.SetFlash(c, "Error adding collaborator: "+err.Error())
		return c.Redirect(fmt.Sprintf("/apps/%s?tab=collaborators", appID))
	}

	p.RecordAuditLog(c, "add_collaborator", "app", appID, fmt.Sprintf("Added collaborator %s with role %s", collabUsername, role))
	if WantsJSON(c) {
		return c.JSON(fiber.Map{"ok": true})
	}
	utils.SetFlash(c, "Collaborator added successfully.")
	return c.Redirect(fmt.Sprintf("/apps/%s?tab=collaborators", appID))
}

func (p *Panel) DeleteCollaborator(c *fiber.Ctx) error {
	ctx := c.UserContext()
	appID := c.Params("id")
	collabUserID, err := c.ParamsInt("uid")
	if err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("Invalid user ID")
	}

	u, ok := c.Locals("auth_user").(db.User)
	if !ok {
		return c.Status(fiber.StatusUnauthorized).SendString("Unauthorized")
	}

	app, err := p.DB.GetApp(ctx, appID)
	if err != nil {
		return c.Status(fiber.StatusNotFound).SendString("App not found")
	}

	if u.Role != db.RoleAdmin && app.OwnerID != u.ID {
		return c.Status(fiber.StatusForbidden).SendString("Forbidden")
	}

	if err := p.DB.RemoveCollaborator(ctx, appID, int64(collabUserID)); err != nil {
		if WantsJSON(c) {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"ok": false, "error": err.Error()})
		}
		utils.SetFlash(c, "Error removing collaborator: "+err.Error())
		return c.Redirect(fmt.Sprintf("/apps/%s?tab=collaborators", appID))
	}

	p.RecordAuditLog(c, "delete_collaborator", "app", appID, fmt.Sprintf("Removed collaborator user ID %d", collabUserID))
	if WantsJSON(c) {
		return c.JSON(fiber.Map{"ok": true})
	}
	utils.SetFlash(c, "Collaborator removed successfully.")
	return c.Redirect(fmt.Sprintf("/apps/%s?tab=collaborators", appID))
}

func (p *Panel) TransferAppOwnership(c *fiber.Ctx) error {
	ctx := c.UserContext()
	appID := c.Params("id")
	u, ok := c.Locals("auth_user").(db.User)
	if !ok {
		return c.Status(fiber.StatusUnauthorized).SendString("Unauthorized")
	}

	app, err := p.DB.GetApp(ctx, appID)
	if err != nil {
		return c.Status(fiber.StatusNotFound).SendString("App not found")
	}

	if u.Role != db.RoleAdmin && app.OwnerID != u.ID {
		return c.Status(fiber.StatusForbidden).SendString("Forbidden")
	}

	newOwnerUsername := strings.TrimSpace(c.FormValue("username"))
	targetUser, err := p.DB.GetUserByUsername(ctx, newOwnerUsername)
	if err != nil {
		if WantsJSON(c) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"ok": false, "error": "User not found"})
		}
		utils.SetFlash(c, "Error: User not found.")
		return c.Redirect(fmt.Sprintf("/apps/%s?tab=collaborators", appID))
	}

	if targetUser.ID == app.OwnerID {
		if WantsJSON(c) {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"ok": false, "error": "User is already the owner"})
		}
		utils.SetFlash(c, "Error: User is already the owner.")
		return c.Redirect(fmt.Sprintf("/apps/%s?tab=collaborators", appID))
	}

	collabs, err := p.DB.ListCollaborators(ctx, appID)
	if err != nil {
		if WantsJSON(c) {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"ok": false, "error": "Error loading collaborators"})
		}
		utils.SetFlash(c, "Error loading collaborators.")
		return c.Redirect(fmt.Sprintf("/apps/%s?tab=collaborators", appID))
	}
	isCollab := false
	for _, cb := range collabs {
		if cb.UserID == targetUser.ID {
			isCollab = true
			break
		}
	}
	if !isCollab {
		if WantsJSON(c) {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"ok": false, "error": "Target user must be a collaborator of this app first"})
		}
		utils.SetFlash(c, "Error: Target user must be a collaborator of this app first.")
		return c.Redirect(fmt.Sprintf("/apps/%s?tab=collaborators", appID))
	}

	if err := p.DB.TransferAppOwnership(ctx, appID, targetUser.ID); err != nil {
		if WantsJSON(c) {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"ok": false, "error": err.Error()})
		}
		utils.SetFlash(c, "Error transferring ownership: "+err.Error())
		return c.Redirect(fmt.Sprintf("/apps/%s?tab=collaborators", appID))
	}

	_ = p.DB.RemoveCollaborator(ctx, appID, targetUser.ID)

	p.RecordAuditLog(c, "transfer_ownership", "app", appID, fmt.Sprintf("Transferred ownership of app %s to %s", appID, newOwnerUsername))
	if WantsJSON(c) {
		return c.JSON(fiber.Map{"ok": true})
	}
	utils.SetFlash(c, "Ownership transferred successfully.")

	if u.Role != db.RoleAdmin {
		return c.Redirect("/apps")
	}
	return c.Redirect(fmt.Sprintf("/apps/%s?tab=collaborators", appID))
}
