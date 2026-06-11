package handlers

import (
	"panel/internal/handlers/utils"
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"

	"panel/internal/db"

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

// UsersPage renders the user management page (admin only).
func (p *Panel) UsersPage(c *fiber.Ctx) error {
	user, _ := currentUser(c)
	if user.Role != db.RoleAdmin {
		return c.Status(403).SendString("forbidden")
	}
	users, err := p.DB.ListUsers(c.UserContext())
	if err != nil {
		return c.Status(500).SendString(err.Error())
	}
	return c.Render("pages/users", fiber.Map{
		"Nav":         "users",
		"Title":       "Users",
		"Users":       users,
		"CurrentUser": user,
		"Flash":       utils.ReadFlash(c),
		"Error":       utils.ReadFlashError(c),
	}, "layouts/shell")
}

// UserCreate handles creating a new user (admin only).
func (p *Panel) UserCreate(c *fiber.Ctx) error {
	user, _ := currentUser(c)
	if user.Role != db.RoleAdmin {
		return c.Status(403).SendString("forbidden")
	}

	username := strings.TrimSpace(c.FormValue("username"))
	password := c.FormValue("password")
	role := c.FormValue("role")

	if username == "" || len(username) < 3 {
		utils.SetFlashError(c, "Username must be at least 3 characters")
		return c.Redirect("/users")
	}
	if len(password) < 8 {
		utils.SetFlashError(c, "Password must be at least 8 characters")
		return c.Redirect("/users")
	}
	if role != db.RoleAdmin && role != db.RoleUser {
		role = db.RoleUser
	}

	hash, err := hashPassword(password)
	if err != nil {
		utils.SetFlashError(c, "Internal error")
		return c.Redirect("/users")
	}
	if _, err := p.DB.CreateUser(c.UserContext(), username, hash, role); err != nil {
		utils.SetFlashError(c, "Username already taken")
		return c.Redirect("/users")
	}
	utils.SetFlash(c, "User created successfully")
	return c.Redirect("/users")
}

// UserDelete handles deleting a user (admin only, cannot delete self).
func (p *Panel) UserDelete(c *fiber.Ctx) error {
	current, _ := currentUser(c)
	if current.Role != db.RoleAdmin {
		return c.Status(403).SendString("forbidden")
	}

	idStr := c.Params("id")
	var id int64
	if _, err := parseID(idStr, &id); err != nil {
		return c.Status(400).SendString("invalid id")
	}
	if id == current.ID {
		utils.SetFlashError(c, "Cannot delete your own account")
		return c.Redirect("/users")
	}

	ctx := c.UserContext()
	adminCount, err := countAdminUsers(ctx, p.DB)
	if err != nil {
		return c.Status(500).SendString(err.Error())
	}
	target, err := p.DB.GetUserByID(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			utils.SetFlashError(c, "User not found")
			return c.Redirect("/users")
		}
		return c.Status(500).SendString(err.Error())
	}
	if target.Role == db.RoleAdmin && adminCount <= 1 {
		utils.SetFlashError(c, "Cannot delete the last admin")
		return c.Redirect("/users")
	}

	if err := p.DB.DeleteUser(ctx, id); err != nil {
		utils.SetFlashError(c, "Delete failed")
		return c.Redirect("/users")
	}
	utils.SetFlash(c, "User deleted")
	return c.Redirect("/users")
}

// UserChangePassword handles password change (admin changes any, user changes own).
func (p *Panel) UserChangePassword(c *fiber.Ctx) error {
	current, _ := currentUser(c)

	idStr := c.Params("id")
	var id int64
	if _, err := parseID(idStr, &id); err != nil {
		return c.Status(400).SendString("invalid id")
	}

	// Only admin can change others' passwords
	if id != current.ID && current.Role != db.RoleAdmin {
		return c.Status(403).SendString("forbidden")
	}

	password := c.FormValue("password")
	confirm := c.FormValue("confirm")
	if len(password) < 8 {
		utils.SetFlashError(c, "Password must be at least 8 characters")
		return c.Redirect("/users")
	}
	if password != confirm {
		utils.SetFlashError(c, "Passwords do not match")
		return c.Redirect("/users")
	}

	hash, err := hashPassword(password)
	if err != nil {
		utils.SetFlashError(c, "Internal error")
		return c.Redirect("/users")
	}
	if err := p.DB.UpdateUserPassword(c.UserContext(), id, hash); err != nil {
		utils.SetFlashError(c, "Update failed")
		return c.Redirect("/users")
	}
	utils.SetFlash(c, "Password updated")
	return c.Redirect("/users")
}

// UserChangeRole handles role changes (admin only).
func (p *Panel) UserChangeRole(c *fiber.Ctx) error {
	current, _ := currentUser(c)
	if current.Role != db.RoleAdmin {
		return c.Status(403).SendString("forbidden")
	}

	idStr := c.Params("id")
	var id int64
	if _, err := parseID(idStr, &id); err != nil {
		return c.Status(400).SendString("invalid id")
	}
	if id == current.ID {
		utils.SetFlashError(c, "Cannot change your own role")
		return c.Redirect("/users")
	}

	role := c.FormValue("role")
	if role != db.RoleAdmin && role != db.RoleUser {
		utils.SetFlashError(c, "Invalid role")
		return c.Redirect("/users")
	}

	ctx := c.UserContext()
	target, err := p.DB.GetUserByID(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			utils.SetFlashError(c, "User not found")
			return c.Redirect("/users")
		}
		return c.Status(500).SendString(err.Error())
	}

	if role == db.RoleUser && target.Role == db.RoleAdmin {
		adminCount, err := countAdminUsers(ctx, p.DB)
		if err != nil {
			return c.Status(500).SendString(err.Error())
		}
		if adminCount <= 1 {
			utils.SetFlashError(c, "Cannot demote the last admin")
			return c.Redirect("/users")
		}
	}

	if err := p.DB.UpdateUserRole(ctx, id, role); err != nil {
		utils.SetFlashError(c, "Update failed")
		return c.Redirect("/users")
	}
	utils.SetFlash(c, "Role updated")
	return c.Redirect("/users")
}

func parseID(s string, out *int64) (int64, error) {
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fiber.ErrBadRequest
	}
	*out = id
	return id, nil
}
