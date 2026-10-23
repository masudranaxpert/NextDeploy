package handlers

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"strconv"
	"strings"

	"panel/internal/db"
	"panel/internal/dockerx"

	"github.com/gofiber/fiber/v2"
)

// countAdminUsers returns how many users have the admin role.
func countAdminUsers(ctx context.Context, dbStore *db.Store) (int, error) {
	users, err := dbStore.ListUsers(ctx)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, u := range users {
		if u.Role == db.RoleAdmin {
			n++
		}
	}
	return n, nil
}

// UserCreate handles creating a new user (admin only).
func (p *Panel) UserCreate(c *fiber.Ctx) error {
	user, _ := currentUser(c)
	if user.Role != db.RoleAdmin {
		if WantsJSON(c) {
			return c.Status(403).JSON(fiber.Map{"ok": false, "error": "forbidden"})
		}
		return c.Status(403).SendString("forbidden")
	}

	username := strings.TrimSpace(c.FormValue("username"))
	password := c.FormValue("password")
	role := c.FormValue("role")

	if username == "" || len(username) < 3 {
		if WantsJSON(c) {
			return c.Status(400).JSON(fiber.Map{"ok": false, "error": "Username must be at least 3 characters"})
		}
		SetFlashError(c, "Username must be at least 3 characters")
		return c.Redirect("/users")
	}
	if len(password) < 8 {
		if WantsJSON(c) {
			return c.Status(400).JSON(fiber.Map{"ok": false, "error": "Password must be at least 8 characters"})
		}
		SetFlashError(c, "Password must be at least 8 characters")
		return c.Redirect("/users")
	}
	if role != db.RoleAdmin && role != db.RoleUser {
		role = db.RoleUser
	}

	hash, err := hashPassword(password)
	if err != nil {
		if WantsJSON(c) {
			return c.Status(500).JSON(fiber.Map{"ok": false, "error": "Internal error"})
		}
		SetFlashError(c, "Internal error")
		return c.Redirect("/users")
	}
	createdUser, err := p.DB.CreateUser(c.UserContext(), username, hash, role)
	if err != nil {
		if WantsJSON(c) {
			return c.Status(409).JSON(fiber.Map{"ok": false, "error": "Username already taken"})
		}
		SetFlashError(c, "Username already taken")
		return c.Redirect("/users")
	}
	p.InvalidateAfterUserChange()
	if WantsJSON(c) {
		return c.JSON(fiber.Map{"ok": true, "user": createdUser})
	}
	SetFlash(c, "User created successfully")
	return c.Redirect("/users")
}

// UserDelete handles deleting a user (admin only, cannot delete self).
func (p *Panel) UserDelete(c *fiber.Ctx) error {
	current, _ := currentUser(c)
	if current.Role != db.RoleAdmin {
		if WantsJSON(c) {
			return c.Status(403).JSON(fiber.Map{"ok": false, "error": "forbidden"})
		}
		return c.Status(403).SendString("forbidden")
	}

	idStr := c.Params("id")
	var id int64
	if _, err := parseID(idStr, &id); err != nil {
		if WantsJSON(c) {
			return c.Status(400).JSON(fiber.Map{"ok": false, "error": "invalid id"})
		}
		return c.Status(400).SendString("invalid id")
	}
	if id == current.ID {
		if WantsJSON(c) {
			return c.Status(400).JSON(fiber.Map{"ok": false, "error": "Cannot delete your own account"})
		}
		SetFlashError(c, "Cannot delete your own account")
		return c.Redirect("/users")
	}

	ctx := c.UserContext()
	adminCount, err := countAdminUsers(ctx, p.DB)
	if err != nil {
		if WantsJSON(c) {
			return c.Status(500).JSON(fiber.Map{"ok": false, "error": err.Error()})
		}
		return c.Status(500).SendString(err.Error())
	}
	target, err := p.DB.GetUserByID(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			if WantsJSON(c) {
				return c.Status(404).JSON(fiber.Map{"ok": false, "error": "User not found"})
			}
			SetFlashError(c, "User not found")
			return c.Redirect("/users")
		}
		if WantsJSON(c) {
			return c.Status(500).JSON(fiber.Map{"ok": false, "error": err.Error()})
		}
		return c.Status(500).SendString(err.Error())
	}
	if target.Role == db.RoleAdmin && adminCount <= 1 {
		if WantsJSON(c) {
			return c.Status(400).JSON(fiber.Map{"ok": false, "error": "Cannot delete the last admin"})
		}
		SetFlashError(c, "Cannot delete the last admin")
		return c.Redirect("/users")
	}

	if err := p.DB.DeleteUser(ctx, id); err != nil {
		if WantsJSON(c) {
			return c.Status(500).JSON(fiber.Map{"ok": false, "error": "Delete failed"})
		}
		SetFlashError(c, "Delete failed")
		return c.Redirect("/users")
	}
	p.InvalidateAfterUserChange()
	if count, err := p.DB.UserCount(ctx); err == nil && count == 0 {
		p.ClearSetupComplete()
	}
	if WantsJSON(c) {
		return c.JSON(fiber.Map{"ok": true})
	}
	SetFlash(c, "User deleted")
	return c.Redirect("/users")
}

// UserChangePassword handles password change (admin changes any, user changes own).
func (p *Panel) UserChangePassword(c *fiber.Ctx) error {
	current, _ := currentUser(c)

	idStr := c.Params("id")
	var id int64
	if _, err := parseID(idStr, &id); err != nil {
		if WantsJSON(c) {
			return c.Status(400).JSON(fiber.Map{"ok": false, "error": "invalid id"})
		}
		return c.Status(400).SendString("invalid id")
	}

	// Only admin can change others' passwords
	if id != current.ID && current.Role != db.RoleAdmin {
		if WantsJSON(c) {
			return c.Status(403).JSON(fiber.Map{"ok": false, "error": "forbidden"})
		}
		return c.Status(403).SendString("forbidden")
	}

	password := c.FormValue("password")
	confirm := c.FormValue("confirm")
	if len(password) < 8 {
		if WantsJSON(c) {
			return c.Status(400).JSON(fiber.Map{"ok": false, "error": "Password must be at least 8 characters"})
		}
		SetFlashError(c, "Password must be at least 8 characters")
		return c.Redirect("/users/" + idStr + "/edit")
	}
	if password != confirm {
		if WantsJSON(c) {
			return c.Status(400).JSON(fiber.Map{"ok": false, "error": "Passwords do not match"})
		}
		SetFlashError(c, "Passwords do not match")
		return c.Redirect("/users/" + idStr + "/edit")
	}

	hash, err := hashPassword(password)
	if err != nil {
		if WantsJSON(c) {
			return c.Status(500).JSON(fiber.Map{"ok": false, "error": "Internal error"})
		}
		SetFlashError(c, "Internal error")
		return c.Redirect("/users/" + idStr + "/edit")
	}
	if err := p.DB.UpdateUserPassword(c.UserContext(), id, hash); err != nil {
		if WantsJSON(c) {
			return c.Status(500).JSON(fiber.Map{"ok": false, "error": "Update failed"})
		}
		SetFlashError(c, "Update failed")
		return c.Redirect("/users/" + idStr + "/edit")
	}
	if WantsJSON(c) {
		return c.JSON(fiber.Map{"ok": true})
	}
	SetFlash(c, "Password updated")
	return c.Redirect("/users/" + idStr + "/edit")
}

// UserChangeRole handles role changes (admin only).
func (p *Panel) UserChangeRole(c *fiber.Ctx) error {
	current, _ := currentUser(c)
	if current.Role != db.RoleAdmin {
		if WantsJSON(c) {
			return c.Status(403).JSON(fiber.Map{"ok": false, "error": "forbidden"})
		}
		return c.Status(403).SendString("forbidden")
	}

	idStr := c.Params("id")
	var id int64
	if _, err := parseID(idStr, &id); err != nil {
		if WantsJSON(c) {
			return c.Status(400).JSON(fiber.Map{"ok": false, "error": "invalid id"})
		}
		return c.Status(400).SendString("invalid id")
	}
	if id == current.ID {
		if WantsJSON(c) {
			return c.Status(400).JSON(fiber.Map{"ok": false, "error": "Cannot change your own role"})
		}
		SetFlashError(c, "Cannot change your own role")
		return c.Redirect("/users/" + idStr + "/edit")
	}

	role := c.FormValue("role")
	if role != db.RoleAdmin && role != db.RoleUser {
		if WantsJSON(c) {
			return c.Status(400).JSON(fiber.Map{"ok": false, "error": "Invalid role"})
		}
		SetFlashError(c, "Invalid role")
		return c.Redirect("/users/" + idStr + "/edit")
	}

	ctx := c.UserContext()
	target, err := p.DB.GetUserByID(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			if WantsJSON(c) {
				return c.Status(404).JSON(fiber.Map{"ok": false, "error": "User not found"})
			}
			SetFlashError(c, "User not found")
			return c.Redirect("/users")
		}
		if WantsJSON(c) {
			return c.Status(500).JSON(fiber.Map{"ok": false, "error": err.Error()})
		}
		return c.Status(500).SendString(err.Error())
	}

	if role == db.RoleUser && target.Role == db.RoleAdmin {
		adminCount, err := countAdminUsers(ctx, p.DB)
		if err != nil {
			if WantsJSON(c) {
				return c.Status(500).JSON(fiber.Map{"ok": false, "error": err.Error()})
			}
			return c.Status(500).SendString(err.Error())
		}
		if adminCount <= 1 {
			if WantsJSON(c) {
				return c.Status(400).JSON(fiber.Map{"ok": false, "error": "Cannot demote the last admin"})
			}
			SetFlashError(c, "Cannot demote the last admin")
			return c.Redirect("/users/" + idStr + "/edit")
		}
	}

	if err := p.DB.UpdateUserRole(ctx, id, role); err != nil {
		if WantsJSON(c) {
			return c.Status(500).JSON(fiber.Map{"ok": false, "error": "Update failed"})
		}
		SetFlashError(c, "Update failed")
		return c.Redirect("/users/" + idStr + "/edit")
	}
	p.InvalidateAfterUserChange()
	if WantsJSON(c) {
		return c.JSON(fiber.Map{"ok": true, "role": role})
	}
	SetFlash(c, "Role updated")
	return c.Redirect("/users/" + idStr + "/edit")
}

func parseID(s string, out *int64) (int64, error) {
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fiber.ErrBadRequest
	}
	*out = id
	return id, nil
}

func (p *Panel) UserChangeStatus(c *fiber.Ctx) error {
	current, _ := currentUser(c)
	if current.Role != db.RoleAdmin {
		if WantsJSON(c) {
			return c.Status(403).JSON(fiber.Map{"ok": false, "error": "forbidden"})
		}
		return c.Status(403).SendString("forbidden")
	}

	idStr := c.Params("id")
	var id int64
	if _, err := parseID(idStr, &id); err != nil {
		if WantsJSON(c) {
			return c.Status(400).JSON(fiber.Map{"ok": false, "error": "invalid id"})
		}
		return c.Status(400).SendString("invalid id")
	}
	if id == current.ID {
		if WantsJSON(c) {
			return c.Status(400).JSON(fiber.Map{"ok": false, "error": "Cannot suspend your own account"})
		}
		SetFlashError(c, "Cannot suspend your own account")
		return c.Redirect("/users/" + idStr + "/edit")
	}

	status := strings.TrimSpace(c.FormValue("status"))
	if status != db.UserStatusActive && status != db.UserStatusSuspended {
		if WantsJSON(c) {
			return c.Status(400).JSON(fiber.Map{"ok": false, "error": "Invalid status"})
		}
		SetFlashError(c, "Invalid status")
		return c.Redirect("/users/" + idStr + "/edit")
	}

	if err := p.DB.UpdateUserStatus(c.UserContext(), id, status); err != nil {
		if WantsJSON(c) {
			return c.Status(500).JSON(fiber.Map{"ok": false, "error": "Update failed"})
		}
		SetFlashError(c, "Update failed")
		return c.Redirect("/users/" + idStr + "/edit")
	}

	if status == db.UserStatusSuspended {
		ctx := c.UserContext()
		_ = p.DB.DeleteUserSessions(ctx, id)
		apps, err := p.DB.ListAppsForUser(ctx, id)
		if err == nil {
			for _, app := range apps {
				_ = p.DB.UpdateAppStatus(ctx, app.ID, db.AppStatusSuspended)
				files := p.EffectiveComposePaths(ctx, app, app.ID)
				dir := p.AppSourcePath(ctx, app.ID)
				project := p.ActiveComposeProjectName(ctx, app, app.ID)
				var logW io.Writer
				_ = dockerx.ComposeDown(ctx, dir, files, project, logW, p.ComposeEnvFiles(ctx, app.ID))
			}
		}
	}

	p.InvalidateAfterUserChange()
	if WantsJSON(c) {
		return c.JSON(fiber.Map{"ok": true, "status": status})
	}
	SetFlash(c, "User status updated")
	return c.Redirect("/users/" + idStr + "/edit")
}

func (p *Panel) UserChangeLimits(c *fiber.Ctx) error {
	current, _ := currentUser(c)
	if current.Role != db.RoleAdmin {
		if WantsJSON(c) {
			return c.Status(403).JSON(fiber.Map{"ok": false, "error": "forbidden"})
		}
		return c.Status(403).SendString("forbidden")
	}

	idStr := c.Params("id")
	var id int64
	if _, err := parseID(idStr, &id); err != nil {
		if WantsJSON(c) {
			return c.Status(400).JSON(fiber.Map{"ok": false, "error": "invalid id"})
		}
		return c.Status(400).SendString("invalid id")
	}

	maxApps, _ := strconv.Atoi(c.FormValue("max_apps"))
	maxMemory, _ := strconv.Atoi(c.FormValue("max_memory_mb"))
	maxCPUs, _ := strconv.ParseFloat(c.FormValue("max_cpus"), 64)
	maxStorage, _ := strconv.Atoi(c.FormValue("max_storage_mb"))

	target, terr := p.DB.GetUserByID(c.UserContext(), id)
	if terr != nil {
		if WantsJSON(c) {
			return c.Status(404).JSON(fiber.Map{"ok": false, "error": "User not found"})
		}
		SetFlashError(c, "User not found")
		return c.Redirect("/users")
	}
	if target.Role == db.RoleAdmin {
		if WantsJSON(c) {
			return c.Status(400).JSON(fiber.Map{"ok": false, "error": "Admin accounts have unlimited resources"})
		}
		SetFlashError(c, "Admin accounts have unlimited resources")
		return c.Redirect("/users/" + idStr + "/edit")
	}

	if maxApps <= 0 || maxMemory <= 0 || maxCPUs <= 0 || maxStorage <= 0 {
		if WantsJSON(c) {
			return c.Status(400).JSON(fiber.Map{"ok": false, "error": "Invalid limits"})
		}
		SetFlashError(c, "Invalid limits")
		return c.Redirect("/users/" + idStr + "/edit")
	}

	allowFileServer := c.FormValue("allow_domain_file_server") == "on"
	if err := p.DB.UpdateUserLimits(c.UserContext(), id, maxApps, maxMemory, maxCPUs, maxStorage, allowFileServer); err != nil {
		if WantsJSON(c) {
			return c.Status(500).JSON(fiber.Map{"ok": false, "error": "Update failed"})
		}
		SetFlashError(c, "Update failed")
		return c.Redirect("/users/" + idStr + "/edit")
	}

	// Push the new limits onto the user's cgroup slice so already-running
	// containers are re-limited immediately (kernel applies it live).
	if p.UserSliceSupported(c.UserContext()) {
		if serr := p.EnsureUserSliceLimits(c.UserContext(), id, maxMemory, maxCPUs); serr != nil {
			if WantsJSON(c) {
				return c.JSON(fiber.Map{"ok": true, "warning": "Limits saved, but applying the cgroup limit failed: " + serr.Error()})
			}
			SetFlash(c, "Limits saved, but applying the cgroup limit failed: "+serr.Error())
			return c.Redirect("/users/" + idStr + "/edit")
		}
	}

	p.InvalidateAfterUserChange()
	if WantsJSON(c) {
		return c.JSON(fiber.Map{"ok": true})
	}
	SetFlash(c, "Limits updated")
	return c.Redirect("/users/" + idStr + "/edit")
}
