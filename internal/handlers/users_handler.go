package handlers

import (
	"panel/internal/handlers/utils"
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

	// Per-user workspace storage usage (sizes come from a short-TTL cache).
	storageByUser := make(map[int64]string, len(users))
	if apps, aerr := p.DB.ListApps(c.UserContext()); aerr == nil {
		usage := make(map[int64]int64)
		for _, app := range apps {
			if app.OwnerID > 0 {
				usage[app.OwnerID] += p.AppStorageBytes(app.ID)
			}
		}
		for _, u := range users {
			storageByUser[u.ID] = HumanStorage(usage[u.ID])
		}
	}

	return c.Render("pages/users", fiber.Map{
		"Nav":           "users",
		"Title":         "Users",
		"Users":         users,
		"StorageByUser": storageByUser,
		"CurrentUser":   user,
		"Flash":         utils.ReadFlash(c),
		"Error":         utils.ReadFlashError(c),
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
	p.InvalidateAfterUserChange()
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
	p.InvalidateAfterUserChange()
	if count, err := p.DB.UserCount(ctx); err == nil && count == 0 {
		p.ClearSetupComplete()
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
		return c.Redirect("/users/" + idStr + "/edit")
	}
	if password != confirm {
		utils.SetFlashError(c, "Passwords do not match")
		return c.Redirect("/users/" + idStr + "/edit")
	}

	hash, err := hashPassword(password)
	if err != nil {
		utils.SetFlashError(c, "Internal error")
		return c.Redirect("/users/" + idStr + "/edit")
	}
	if err := p.DB.UpdateUserPassword(c.UserContext(), id, hash); err != nil {
		utils.SetFlashError(c, "Update failed")
		return c.Redirect("/users/" + idStr + "/edit")
	}
	utils.SetFlash(c, "Password updated")
	return c.Redirect("/users/" + idStr + "/edit")
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
		return c.Redirect("/users/" + idStr + "/edit")
	}

	role := c.FormValue("role")
	if role != db.RoleAdmin && role != db.RoleUser {
		utils.SetFlashError(c, "Invalid role")
		return c.Redirect("/users/" + idStr + "/edit")
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
			return c.Redirect("/users/" + idStr + "/edit")
		}
	}

	if err := p.DB.UpdateUserRole(ctx, id, role); err != nil {
		utils.SetFlashError(c, "Update failed")
		return c.Redirect("/users/" + idStr + "/edit")
	}
	p.InvalidateAfterUserChange()
	utils.SetFlash(c, "Role updated")
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
		return c.Status(403).SendString("forbidden")
	}

	idStr := c.Params("id")
	var id int64
	if _, err := parseID(idStr, &id); err != nil {
		return c.Status(400).SendString("invalid id")
	}
	if id == current.ID {
		utils.SetFlashError(c, "Cannot suspend your own account")
		return c.Redirect("/users/" + idStr + "/edit")
	}

	status := strings.TrimSpace(c.FormValue("status"))
	if status != db.UserStatusActive && status != db.UserStatusSuspended {
		utils.SetFlashError(c, "Invalid status")
		return c.Redirect("/users/" + idStr + "/edit")
	}

	if err := p.DB.UpdateUserStatus(c.UserContext(), id, status); err != nil {
		utils.SetFlashError(c, "Update failed")
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
	utils.SetFlash(c, "User status updated")
	return c.Redirect("/users/" + idStr + "/edit")
}

func (p *Panel) UserChangeLimits(c *fiber.Ctx) error {
	current, _ := currentUser(c)
	if current.Role != db.RoleAdmin {
		return c.Status(403).SendString("forbidden")
	}

	idStr := c.Params("id")
	var id int64
	if _, err := parseID(idStr, &id); err != nil {
		return c.Status(400).SendString("invalid id")
	}

	maxApps, _ := strconv.Atoi(c.FormValue("max_apps"))
	maxMemory, _ := strconv.Atoi(c.FormValue("max_memory_mb"))
	maxCPUs, _ := strconv.ParseFloat(c.FormValue("max_cpus"), 64)
	maxStorage, _ := strconv.Atoi(c.FormValue("max_storage_mb"))

	target, terr := p.DB.GetUserByID(c.UserContext(), id)
	if terr != nil {
		utils.SetFlashError(c, "User not found")
		return c.Redirect("/users")
	}
	if target.Role == db.RoleAdmin {
		utils.SetFlashError(c, "Admin accounts have unlimited resources")
		return c.Redirect("/users/" + idStr + "/edit")
	}

	if maxApps <= 0 || maxMemory <= 0 || maxCPUs <= 0 || maxStorage <= 0 {
		utils.SetFlashError(c, "Invalid limits")
		return c.Redirect("/users/" + idStr + "/edit")
	}

	allowFileServer := c.FormValue("allow_domain_file_server") == "on"
	if err := p.DB.UpdateUserLimits(c.UserContext(), id, maxApps, maxMemory, maxCPUs, maxStorage, allowFileServer); err != nil {
		utils.SetFlashError(c, "Update failed")
		return c.Redirect("/users/" + idStr + "/edit")
	}

	// Push the new limits onto the user's cgroup slice so already-running
	// containers are re-limited immediately (kernel applies it live).
	if p.UserSliceSupported(c.UserContext()) {
		if serr := p.EnsureUserSliceLimits(c.UserContext(), id, maxMemory, maxCPUs); serr != nil {
			utils.SetFlash(c, "Limits saved, but applying the cgroup limit failed: "+serr.Error())
			return c.Redirect("/users/" + idStr + "/edit")
		}
	}

	p.InvalidateAfterUserChange()
	utils.SetFlash(c, "Limits updated")
	return c.Redirect("/users/" + idStr + "/edit")
}

func (p *Panel) UserEditPage(c *fiber.Ctx) error {
	current, _ := currentUser(c)
	idStr := c.Params("id")
	var id int64
	if _, err := parseID(idStr, &id); err != nil {
		return c.Status(400).SendString("invalid id")
	}

	if id != current.ID && current.Role != db.RoleAdmin {
		return c.Status(403).SendString("forbidden")
	}

	target, err := p.DB.GetUserByID(c.UserContext(), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			utils.SetFlashError(c, "User not found")
			return c.Redirect("/users")
		}
		return c.Status(500).SendString(err.Error())
	}

	usedBytes := p.UserStorageBytes(c.UserContext(), target.ID)
	storageUnlimited := target.Role == db.RoleAdmin
	maxBytes := int64(target.MaxStorageMB) * 1024 * 1024
	var storagePct float64
	if !storageUnlimited && maxBytes > 0 {
		storagePct = (float64(usedBytes) / float64(maxBytes)) * 100.0
		if storagePct > 100 {
			storagePct = 100
		}
	}

	return c.Render("pages/user_edit", fiber.Map{
		"Nav":               "users",
		"Title":             "Edit User",
		"TargetUser":        target,
		"CurrentUser":       current,
		"StorageUsed":       HumanStorage(usedBytes),
		"StorageMax":        HumanStorage(maxBytes),
		"StoragePercent":    storagePct,
		"StorageUnlimited":  storageUnlimited,
		"Flash":          utils.ReadFlash(c),
		"Error":          utils.ReadFlashError(c),
	}, "layouts/shell")
}
