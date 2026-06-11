package handlers

import (
	"panel/internal/handlers/utils"
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
	sessionCookie  = "nd_session"
	sessionTTL     = 7 * 24 * time.Hour // Reduced from 30 to 7 days for better security
	contextUserKey = "auth_user"
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
	if user.Status == db.UserStatusSuspended {
		c.Cookie(&fiber.Cookie{Name: sessionCookie, Value: "", MaxAge: -1, HTTPOnly: true, SameSite: "Lax"})
		return c.Status(fiber.StatusForbidden).SendString("Your account has been suspended.")
	}
	c.Locals(contextUserKey, user)
	return c.Next()
}

// currentUser returns the authenticated user from the request context.
func currentUser(c *fiber.Ctx) (db.User, bool) {
	u, ok := c.Locals(contextUserKey).(db.User)
	return u, ok
}

func CurrentUser(c *fiber.Ctx) (db.User, bool) {
	return currentUser(c)
}

func (p *Panel) RequireAdminMiddleware(c *fiber.Ctx) error {
	u, ok := currentUser(c)
	if !ok || u.Role != db.RoleAdmin {
		return c.Status(fiber.StatusForbidden).SendString("forbidden")
	}
	return c.Next()
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
		"Error": utils.ReadFlashError(c),
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
		utils.SetFlashError(c, "Username must be at least 3 characters")
		return c.Redirect("/setup")
	}
	if len(password) < 8 {
		utils.SetFlashError(c, "Password must be at least 8 characters")
		return c.Redirect("/setup")
	}
	if password != confirm {
		utils.SetFlashError(c, "Passwords do not match")
		return c.Redirect("/setup")
	}

	hash, err := hashPassword(password)
	if err != nil {
		utils.SetFlashError(c, "Internal error")
		return c.Redirect("/setup")
	}

	userID, err := p.DB.CreateUser(ctx, username, hash, db.RoleAdmin)
	if err != nil {
		utils.SetFlashError(c, "Username already taken")
		return c.Redirect("/setup")
	}

	p.RecordAuditLog(c, "setup_complete", "system", username, "First-time setup completed, admin user created")
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
	// ?next= is navigation state (not a flash message), keep in URL.
	return c.Render("pages/login", fiber.Map{
		"Title": "Login",
		"Error": utils.ReadFlashError(c),
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
		p.RecordAuditLog(c, "login_failed", "user", username, "Failed login attempt")
		utils.SetFlashError(c, "Invalid username or password")
		// Keep ?next= in URL so the login form can re-submit to the right destination.
		if next != "" && next != "/" {
			return c.Redirect("/login?next=" + url.QueryEscape(next))
		}
		return c.Redirect("/login")
	}

	c.Locals(contextUserKey, user)
	p.RecordAuditLog(c, "login_success", "user", user.Username, "Successfully logged in")
	return p.createSessionAndRedirect(c, user.ID, next)
}

// Logout destroys the session and redirects to login.
func (p *Panel) Logout(c *fiber.Ctx) error {
	if u, ok := currentUser(c); ok {
		p.RecordAuditLog(c, "logout", "user", u.Username, "User logged out")
	}
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
		utils.SetFlashError(c, "Internal error")
		return c.Redirect("/login")
	}
	expiresAt := time.Now().Add(sessionTTL)
	if err := p.DB.CreateSession(c.UserContext(), token, userID, expiresAt); err != nil {
		utils.SetFlashError(c, "Internal error")
		return c.Redirect("/login")
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
