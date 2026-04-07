package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"strings"
	"time"

	"panel/internal/db"

	"github.com/gofiber/fiber/v2"
	"golang.org/x/crypto/bcrypt"
)

const (
	sessionCookie          = "nd_session"
	phpPanelOwnerCookie    = "nd_php_panel_owner"
	sessionTTL             = 30 * 24 * time.Hour
	contextUserKey         = "auth_user"
)

// hashPassword hashes a plaintext password using bcrypt.
func hashPassword(plain string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
	return string(b), err
}

// checkPassword verifies a plaintext password against a bcrypt hash.
func checkPassword(hash, plain string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil
}

// randomToken generates a cryptographically random hex token.
func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func validateRedirectPath(next string) string {
	next = strings.TrimSpace(next)
	if next == "" {
		return "/overview"
	}
	if strings.Contains(next, "://") || strings.HasPrefix(next, "//") {
		return "/overview"
	}
	if !strings.HasPrefix(next, "/") {
		return "/overview"
	}
	if len(next) > 1 && (next[1] <= ' ' || next[1] == 0x7F) {
		return "/overview"
	}
	if strings.Contains(next, "\\") || strings.Contains(next, "@") {
		return "/overview"
	}
	return next
}

func (p *Panel) AuthMiddleware(c *fiber.Ctx) error {
	path := c.Path()

	// Always allow static assets and auth routes
	if strings.HasPrefix(path, "/static/") ||
		path == "/login" || path == "/setup" {
		return c.Next()
	}

	ctx := c.UserContext()

	// First-time setup: no users exist yet
	count, err := p.DB.UserCount(ctx)
	if err != nil {
		return c.Status(500).SendString("db error: " + err.Error())
	}
	if count == 0 {
		return c.Redirect("/setup")
	}

	// Validate session cookie
	token := c.Cookies(sessionCookie)
	if token == "" {
		return c.Redirect("/login?next=" + c.Path())
	}
	userID, expiresAt, err := p.DB.GetSession(ctx, token)
	if err != nil || time.Now().After(expiresAt) {
		c.Cookie(&fiber.Cookie{Name: sessionCookie, Value: "", MaxAge: -1, HTTPOnly: true, SameSite: "Lax"})
		return c.Redirect("/login?next=" + c.Path())
	}
	user, err := p.DB.GetUserByID(ctx, userID)
	if err != nil {
		return c.Redirect("/login")
	}
	c.Locals(contextUserKey, user)
	if user.Role == db.RoleAdmin {
		// Set PHP Panel nav link for admin sidebar
		if phpApp, ok := p.currentPHPPanelApp(ctx); ok {
			c.Locals("PHPPanelNavAppID", phpApp.ID)
			c.Locals("PHPPanelNavName", phpApp.Name)
		}
		// Check if admin has an active impersonation session for a user's PHP Panel view
		ownerToken := strings.TrimSpace(c.Cookies(phpPanelOwnerCookie))
		if ownerToken != "" {
			imp, err := p.DB.GetPHPPanelImpersonation(ctx, ownerToken)
			if err == nil && time.Now().Before(imp.ExpiresAt) {
				owner, ownerErr := p.DB.GetUserByID(ctx, imp.UserID)
				if ownerErr == nil && owner.ID != user.ID {
					// Valid impersonation: show scoped view as that user
					c.Locals("php_panel_owner", owner)
					c.Locals("ScopedPHPPanelOnly", true)
				}
			} else {
				// Expired or invalid — clear cookie silently
				_ = p.DB.DeletePHPPanelImpersonation(ctx, ownerToken)
				c.Cookie(&fiber.Cookie{Name: phpPanelOwnerCookie, Value: "", MaxAge: -1, HTTPOnly: true, SameSite: "Lax", Path: "/"})
			}
		}
		// If scoped impersonation view is active, redirect non-PHP-Panel routes back to PHP Panel
		if scopedOnly, _ := c.Locals("ScopedPHPPanelOnly").(bool); scopedOnly {
			if !p.allowPHPPanelScopedAppRoute(ctx, path) {
				if app, ok := p.currentPHPPanelApp(ctx); ok {
					return c.Redirect("/php-panel/" + app.ID)
				}
			}
		}
	}
	if user.Role == db.RoleUser {
		if app, ok := p.currentPHPPanelApp(ctx); ok {
			c.Locals("PHPPanelNavAppID", app.ID)
			c.Locals("PHPPanelNavName", app.Name)
		}
		if !p.DB.PHPPanelEnabledForUser(ctx, user.ID) {
			if path != "/php-panel-blocked" && path != "/logout" {
				return c.Redirect("/php-panel-blocked")
			}
			return c.Next()
		}
		app, hasApp := p.currentPHPPanelApp(ctx)
		if !hasApp {
			if path != "/php-panel-blocked" && path != "/logout" {
				return c.Redirect("/php-panel-blocked")
			}
			return c.Next()
		}
		_ = app
		if p.allowPHPPanelScopedAppRoute(ctx, path) {
			return c.Next()
		}
		return c.Redirect("/php-panel/" + app.ID)
	}
	return c.Next()
}

// currentUser returns the authenticated user from the request context.
func currentUser(c *fiber.Ctx) (db.User, bool) {
	u, ok := c.Locals(contextUserKey).(db.User)
	return u, ok
}

// SetupPage renders the first-time setup page.
func (p *Panel) SetupPage(c *fiber.Ctx) error {
	ctx := c.UserContext()
	count, _ := p.DB.UserCount(ctx)
	if count > 0 {
		return c.Redirect("/login")
	}
	return c.Render("pages/setup", fiber.Map{
		"Title": "Setup",
		"Error": c.Query("error"),
	}, "layouts/auth")
}

// SetupPost handles first-time admin account creation.
func (p *Panel) SetupPost(c *fiber.Ctx) error {
	ctx := c.UserContext()
	count, _ := p.DB.UserCount(ctx)
	if count > 0 {
		return c.Redirect("/login")
	}

	username := strings.TrimSpace(c.FormValue("username"))
	password := c.FormValue("password")
	confirm := c.FormValue("confirm")

	if username == "" || len(username) < 3 {
		return c.Redirect("/setup?error=Username+must+be+at+least+3+characters")
	}
	if len(password) < 8 {
		return c.Redirect("/setup?error=Password+must+be+at+least+8+characters")
	}
	if password != confirm {
		return c.Redirect("/setup?error=Passwords+do+not+match")
	}

	hash, err := hashPassword(password)
	if err != nil {
		return c.Redirect("/setup?error=Internal+error")
	}

	userID, err := p.DB.CreateUser(ctx, username, hash, db.RoleAdmin)
	if err != nil {
		return c.Redirect("/setup?error=Username+already+taken")
	}
	_ = p.DB.EnsurePHPPanelAccount(ctx, userID, true, 3, 3)

	// Auto-login after setup
	return p.createSessionAndRedirect(c, userID, "/overview")
}

// LoginPage renders the login form.
func (p *Panel) LoginPage(c *fiber.Ctx) error {
	ctx := c.UserContext()
	count, _ := p.DB.UserCount(ctx)
	if count == 0 {
		return c.Redirect("/setup")
	}
	return c.Render("pages/login", fiber.Map{
		"Title": "Login",
		"Error": c.Query("error"),
		"Next":  c.Query("next"),
	}, "layouts/auth")
}

func (p *Panel) LoginPost(c *fiber.Ctx) error {
	ctx := c.UserContext()
	username := strings.TrimSpace(c.FormValue("username"))
	password := c.FormValue("password")
	next := validateRedirectPath(c.FormValue("next"))

	user, err := p.DB.GetUserByUsername(ctx, username)
	if err != nil || !checkPassword(user.PasswordHash, password) {
		q := url.Values{}
		q.Set("error", "Invalid username or password")
		q.Set("next", next)
		return c.Redirect("/login?" + q.Encode())
	}
	if user.Role == db.RoleUser {
		if !p.DB.PHPPanelEnabledForUser(ctx, user.ID) {
			next = "/php-panel-blocked"
		} else if app, ok := p.currentPHPPanelApp(ctx); ok {
			next = "/php-panel/" + app.ID
		} else {
			next = "/php-panel-blocked"
		}
	}

	return p.createSessionAndRedirect(c, user.ID, next)
}

// Logout destroys the session and redirects to login.
func (p *Panel) Logout(c *fiber.Ctx) error {
	token := c.Cookies(sessionCookie)
	if token != "" {
		_ = p.DB.DeleteSession(c.UserContext(), token)
	}
	c.Cookie(&fiber.Cookie{
		Name:     sessionCookie,
		Value:    "",
		MaxAge:   -1,
		HTTPOnly: true,
		SameSite: "Lax",
	})
	return c.Redirect("/login")
}

func (p *Panel) createSessionAndRedirect(c *fiber.Ctx, userID int64, next string) error {
	token, err := randomToken()
	if err != nil {
		return c.Redirect("/login?error=Internal+error")
	}
	expiresAt := time.Now().Add(sessionTTL)
	if err := p.DB.CreateSession(c.UserContext(), token, userID, expiresAt); err != nil {
		return c.Redirect("/login?error=Internal+error")
	}
	c.Cookie(&fiber.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Expires:  expiresAt,
		HTTPOnly: true,
		SameSite: "Lax",
		Path:     "/",
	})
	return c.Redirect(next)
}
