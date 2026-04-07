package handlers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

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

type userPageRow struct {
	db.User
	PHPPanelEnabled bool
	SiteLimit       int
	DatabaseLimit   int
	SiteUsage       int
	DatabaseUsage   int
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
	phpApp, hasPHPApp := p.currentPHPPanelApp(c.UserContext())
	rows := make([]userPageRow, 0, len(users))
	for _, item := range users {
		account, err := p.DB.GetPHPPanelAccount(c.UserContext(), item.ID)
		if err != nil {
			account = db.PHPPanelAccount{
				UserID:        item.ID,
				Enabled:       item.Role == db.RoleAdmin,
				SiteLimit:     3,
				DatabaseLimit: 3,
			}
		}
		siteUsage := 0
		databaseUsage := 0
		if hasPHPApp {
			siteUsage, _ = p.DB.CountOwnedPHPPanelSites(c.UserContext(), phpApp.ID, item.ID)
			databaseUsage, _ = p.DB.CountOwnedPHPPanelDatabases(c.UserContext(), phpApp.ID, item.ID)
		}
		rows = append(rows, userPageRow{
			User:            item,
			PHPPanelEnabled: account.Enabled,
			SiteLimit:       account.SiteLimit,
			DatabaseLimit:   account.DatabaseLimit,
			SiteUsage:       siteUsage,
			DatabaseUsage:   databaseUsage,
		})
	}
	return c.Render("pages/users", fiber.Map{
		"Nav":         "users",
		"Title":       "Users",
		"Users":       rows,
		"CurrentUser": user,
		"PHPPanelApp": phpApp,
		"HasPHPPanel": hasPHPApp,
		"Flash":       c.Query("flash"),
		"Error":       c.Query("error"),
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
	siteLimit, _ := strconv.Atoi(strings.TrimSpace(c.FormValue("site_limit")))
	databaseLimit, _ := strconv.Atoi(strings.TrimSpace(c.FormValue("database_limit")))

	if username == "" || len(username) < 3 {
		return c.Redirect("/users?error=Username+must+be+at+least+3+characters")
	}
	if len(password) < 8 {
		return c.Redirect("/users?error=Password+must+be+at+least+8+characters")
	}
	if role != db.RoleAdmin && role != db.RoleUser {
		role = db.RoleUser
	}

	hash, err := hashPassword(password)
	if err != nil {
		return c.Redirect("/users?error=Internal+error")
	}
	if siteLimit <= 0 {
		siteLimit = 3
	}
	if databaseLimit <= 0 {
		databaseLimit = 3
	}
	if _, err := p.DB.CreateUser(c.UserContext(), username, hash, role); err != nil {
		return c.Redirect("/users?error=Username+already+taken")
	}
	created, err := p.DB.GetUserByUsername(c.UserContext(), username)
	if err == nil {
		enabled := c.FormValue("php_panel_enabled") == "on" || role == db.RoleAdmin
		_ = p.DB.EnsurePHPPanelAccount(c.UserContext(), created.ID, enabled, siteLimit, databaseLimit)
	}
	return c.Redirect("/users?flash=User+created+successfully")
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
		return c.Redirect("/users?error=Cannot+delete+your+own+account")
	}

	ctx := c.UserContext()
	adminCount, err := countAdminUsers(ctx, p.DB)
	if err != nil {
		return c.Status(500).SendString(err.Error())
	}
	target, err := p.DB.GetUserByID(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return c.Redirect("/users?error=User+not+found")
		}
		return c.Status(500).SendString(err.Error())
	}
	if target.Role == db.RoleAdmin && adminCount <= 1 {
		return c.Redirect("/users?error=Cannot+delete+the+last+admin")
	}

	// Clean up PHP Panel data for this user before deleting the account.
	if phpApp, hasPHPApp := p.currentPHPPanelApp(c.UserContext()); hasPHPApp {
		slugs, dbNames, dbUsers, _ := p.DB.DeleteAllPHPPanelDataForUser(c.UserContext(), phpApp.ID, id)

		// Drop MySQL databases and users (best-effort, errors logged but not fatal).
		if len(dbNames) > 0 || len(dbUsers) > 0 {
			var sql strings.Builder
			for _, dbName := range dbNames {
				fmt.Fprintf(&sql, "DROP DATABASE IF EXISTS %s;\n", mysqlQuotedIdentifier(dbName))
			}
			for _, u := range dbUsers {
				fmt.Fprintf(&sql, "DROP USER IF EXISTS '%s'@'%s';\n",
					strings.ReplaceAll(u.Username, "'", "\\'"),
					strings.ReplaceAll(u.Host, "'", "\\'"))
			}
			if sql.Len() > 0 {
				go func(sqlStr string) {
					ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					defer cancel()
					appSnap, err := p.DB.GetApp(ctx, phpApp.ID)
					if err == nil {
						_ = p.phpPanelMySQLExec(ctx, appSnap, sqlStr)
					}
				}(sql.String())
			}
		}

		// Remove site folders from disk.
		workspaceRoot := p.Store.Path(phpApp.ID)
		for _, slug := range slugs {
			_ = os.RemoveAll(filepath.Join(workspaceRoot, "sites", slug))
		}

		// Sync Caddy labels in background since domain rows were removed.
		if len(slugs) > 0 {
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
				defer cancel()
				appSnap, err := p.DB.GetApp(ctx, phpApp.ID)
				if err == nil {
					_ = p.syncAndApplyCaddyOverrideCtx(ctx, appSnap)
				}
			}()
		}
	}

	if err := p.DB.DeleteUser(ctx, id); err != nil {
		return c.Redirect("/users?error=Delete+failed")
	}
	return c.Redirect("/users?flash=User+deleted")
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
		return c.Redirect("/users?error=Password+must+be+at+least+8+characters")
	}
	if password != confirm {
		return c.Redirect("/users?error=Passwords+do+not+match")
	}

	hash, err := hashPassword(password)
	if err != nil {
		return c.Redirect("/users?error=Internal+error")
	}
	if err := p.DB.UpdateUserPassword(c.UserContext(), id, hash); err != nil {
		return c.Redirect("/users?error=Update+failed")
	}
	return c.Redirect("/users?flash=Password+updated")
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
		return c.Redirect("/users?error=Cannot+change+your+own+role")
	}

	role := c.FormValue("role")
	if role != db.RoleAdmin && role != db.RoleUser {
		return c.Redirect("/users?error=Invalid+role")
	}

	ctx := c.UserContext()
	target, err := p.DB.GetUserByID(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return c.Redirect("/users?error=User+not+found")
		}
		return c.Status(500).SendString(err.Error())
	}

	if role == db.RoleUser && target.Role == db.RoleAdmin {
		adminCount, err := countAdminUsers(ctx, p.DB)
		if err != nil {
			return c.Status(500).SendString(err.Error())
		}
		if adminCount <= 1 {
			return c.Redirect("/users?error=Cannot+demote+the+last+admin")
		}
	}

	if err := p.DB.UpdateUserRole(ctx, id, role); err != nil {
		return c.Redirect("/users?error=Update+failed")
	}
	account, err := p.DB.GetPHPPanelAccount(c.UserContext(), id)
	if err != nil {
		account = db.PHPPanelAccount{UserID: id, SiteLimit: 3, DatabaseLimit: 3}
	}
	if err := p.DB.EnsurePHPPanelAccount(c.UserContext(), id, account.Enabled || role == db.RoleAdmin, account.SiteLimit, account.DatabaseLimit); err != nil {
		return c.Redirect("/users?error=PHP+Panel+account+update+failed")
	}
	return c.Redirect("/users?flash=Role+updated")
}

func (p *Panel) UserPHPPanelSettings(c *fiber.Ctx) error {
	current, _ := currentUser(c)
	if current.Role != db.RoleAdmin {
		return c.Status(403).SendString("forbidden")
	}
	idStr := c.Params("id")
	var id int64
	if _, err := parseID(idStr, &id); err != nil {
		return c.Status(400).SendString("invalid id")
	}
	target, err := p.DB.GetUserByID(c.UserContext(), id)
	if err != nil {
		return c.Redirect("/users?error=User+not+found")
	}
	siteLimit, _ := strconv.Atoi(strings.TrimSpace(c.FormValue("site_limit")))
	databaseLimit, _ := strconv.Atoi(strings.TrimSpace(c.FormValue("database_limit")))
	if siteLimit <= 0 {
		siteLimit = 3
	}
	if databaseLimit <= 0 {
		databaseLimit = 3
	}
	enabled := c.FormValue("php_panel_enabled") == "on" || target.Role == db.RoleAdmin
	if err := p.DB.EnsurePHPPanelAccount(c.UserContext(), id, enabled, siteLimit, databaseLimit); err != nil {
		return c.Redirect("/users?error=Could+not+update+PHP+Panel+settings")
	}
	return c.Redirect("/users?flash=PHP+Panel+settings+updated")
}

func (p *Panel) UserPHPPanelOpen(c *fiber.Ctx) error {
	current, _ := currentUser(c)
	if current.Role != db.RoleAdmin {
		return c.Status(403).SendString("forbidden")
	}
	idStr := c.Params("id")
	var id int64
	if _, err := parseID(idStr, &id); err != nil {
		return c.Status(400).SendString("invalid id")
	}
	target, err := p.DB.GetUserByID(c.UserContext(), id)
	if err != nil {
		return c.Redirect("/users?error=User+not+found")
	}
	enabled, app, hasApp := p.userPHPPanelState(c.UserContext(), target)
	if !enabled || !hasApp {
		return c.Redirect("/users?error=PHP+Panel+is+not+available+for+that+user")
	}
	token, err := randomToken()
	if err != nil {
		return c.Redirect("/users?error=Could+not+create+access+token")
	}
	expiresAt := time.Now().Add(1 * time.Hour)
	if err := p.DB.CreatePHPPanelImpersonation(c.UserContext(), token, current.ID, target.ID, expiresAt); err != nil {
		return c.Redirect("/users?error=Could+not+create+access+token")
	}
	c.Cookie(&fiber.Cookie{
		Name:     phpPanelOwnerCookie,
		Value:    token,
		Expires:  expiresAt,
		HTTPOnly: true,
		SameSite: "Lax",
		Path:     "/",
	})
	return c.Redirect("/php-panel/" + app.ID)
}

// ExitPHPPanelImpersonation clears the impersonation cookie so the admin returns to their own full view.
func (p *Panel) ExitPHPPanelImpersonation(c *fiber.Ctx) error {
	current, _ := currentUser(c)
	token := strings.TrimSpace(c.Cookies(phpPanelOwnerCookie))
	if token != "" {
		_ = p.DB.DeletePHPPanelImpersonation(c.UserContext(), token)
	}
	// Expire the cookie regardless of role so stale cookies are always cleared.
	c.Cookie(&fiber.Cookie{
		Name:     phpPanelOwnerCookie,
		Value:    "",
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
		HTTPOnly: true,
		SameSite: "Lax",
		Path:     "/",
	})
	if current.Role == db.RoleAdmin {
		return c.Redirect("/users")
	}
	return c.Redirect("/overview")
}

func parseID(s string, out *int64) (int64, error) {
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fiber.ErrBadRequest
	}
	*out = id
	return id, nil
}
