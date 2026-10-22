package handlers

import (
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
)

const (
	loginFailMaxAttempts = 5
	loginFailWindow      = 5 * time.Minute
	loginFailLockout     = 30 * time.Second
	maxLoginFailEntries  = 4096
	oneTimeTokenCookie   = "nd_new_token"
)

type loginFailEntry struct {
	count       int
	windowStart time.Time
	lockedUntil time.Time
}

func loginRateKey(ip, username string) string {
	ip = strings.TrimSpace(ip)
	username = strings.ToLower(strings.TrimSpace(username))
	if ip == "" {
		return username
	}
	return ip + "|" + username
}

func (p *Panel) evictStaleLoginFailsLocked(now time.Time) {
	if len(p.loginFails) < maxLoginFailEntries {
		return
	}
	for k, v := range p.loginFails {
		if now.After(v.lockedUntil) && now.Sub(v.windowStart) > loginFailWindow {
			delete(p.loginFails, k)
		}
	}
	// Hard cap fallback if still full
	if len(p.loginFails) >= maxLoginFailEntries {
		for k := range p.loginFails {
			delete(p.loginFails, k)
			if len(p.loginFails) < maxLoginFailEntries/2 {
				break
			}
		}
	}
}

func (p *Panel) checkLoginAllowed(username string, ips ...string) (bool, time.Duration) {
	ip := ""
	if len(ips) > 0 {
		ip = ips[0]
	}
	key := loginRateKey(ip, username)
	now := time.Now()
	p.loginFailMu.Lock()
	defer p.loginFailMu.Unlock()
	if p.loginFails == nil {
		p.loginFails = make(map[string]loginFailEntry)
	}
	p.evictStaleLoginFailsLocked(now)
	e := p.loginFails[key]
	if now.Before(e.lockedUntil) {
		return false, e.lockedUntil.Sub(now)
	}
	if e.windowStart.IsZero() || now.Sub(e.windowStart) > loginFailWindow {
		e.count = 0
		e.windowStart = now
	}
	p.loginFails[key] = e
	return true, 0
}

func (p *Panel) recordLoginFailure(username string, ips ...string) {
	ip := ""
	if len(ips) > 0 {
		ip = ips[0]
	}
	key := loginRateKey(ip, username)
	now := time.Now()
	p.loginFailMu.Lock()
	defer p.loginFailMu.Unlock()
	if p.loginFails == nil {
		p.loginFails = make(map[string]loginFailEntry)
	}
	p.evictStaleLoginFailsLocked(now)
	e := p.loginFails[key]
	if e.windowStart.IsZero() || now.Sub(e.windowStart) > loginFailWindow {
		e.count = 0
		e.windowStart = now
	}
	e.count++
	if e.count >= loginFailMaxAttempts {
		e.lockedUntil = now.Add(loginFailLockout)
		e.count = 0
		e.windowStart = now
	}
	p.loginFails[key] = e
}

func (p *Panel) clearLoginFailures(username string, ips ...string) {
	ip := ""
	if len(ips) > 0 {
		ip = ips[0]
	}
	key := loginRateKey(ip, username)
	p.loginFailMu.Lock()
	defer p.loginFailMu.Unlock()
	delete(p.loginFails, key)
	delete(p.loginFails, strings.ToLower(strings.TrimSpace(username)))
}

func sessionCookieClear() *fiber.Cookie {
	return &fiber.Cookie{
		Name:     sessionCookie,
		Value:    "",
		MaxAge:   -1,
		HTTPOnly: true,
		SameSite: "Lax",
		Path:     "/",
	}
}

func sessionCookieSet(c *fiber.Ctx, token string, expires time.Time) *fiber.Cookie {
	isSecure := false
	if c != nil && (c.Protocol() == "https" || strings.EqualFold(c.Get("X-Forwarded-Proto"), "https")) {
		isSecure = true
	}
	return &fiber.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Expires:  expires,
		HTTPOnly: true,
		Secure:   isSecure,
		SameSite: "Lax",
		Path:     "/",
	}
}

// SetOneTimeTokenCookie stores a newly generated API/CLI token in a short-lived HttpOnly cookie
// so it is never exposed in URL query parameters, browser history, or access logs.
func SetOneTimeTokenCookie(c *fiber.Ctx, rawToken, tokenName string) {
	isSecure := c.Protocol() == "https" || strings.EqualFold(c.Get("X-Forwarded-Proto"), "https")
	c.Cookie(&fiber.Cookie{
		Name:     oneTimeTokenCookie,
		Value:    url.QueryEscape(tokenName) + "|" + rawToken,
		MaxAge:   60,
		HTTPOnly: true,
		Secure:   isSecure,
		SameSite: "Strict",
		Path:     "/",
	})
}

// ReadOneTimeTokenCookie reads and immediately clears the one-time token cookie.
func ReadOneTimeTokenCookie(c *fiber.Ctx) (rawToken string, tokenName string) {
	val := strings.TrimSpace(c.Cookies(oneTimeTokenCookie))
	if val == "" {
		return "", ""
	}
	c.Cookie(&fiber.Cookie{
		Name:     oneTimeTokenCookie,
		Value:    "",
		MaxAge:   -1,
		HTTPOnly: true,
		SameSite: "Strict",
		Path:     "/",
	})
	idx := strings.IndexByte(val, '|')
	if idx < 0 {
		return val, ""
	}
	name, _ := url.QueryUnescape(val[:idx])
	return val[idx+1:], name
}

// SecurityHeadersMiddleware adds defensive HTTP headers and validates Origin on state-changing cookie requests.
func (p *Panel) SecurityHeadersMiddleware(c *fiber.Ctx) error {
	c.Set("X-Content-Type-Options", "nosniff")
	c.Set("X-Frame-Options", "SAMEORIGIN")
	c.Set("Referrer-Policy", "strict-origin-when-cross-origin")

	method := c.Method()
	if method == fiber.MethodPost || method == fiber.MethodPut || method == fiber.MethodDelete || method == fiber.MethodPatch {
		path := c.Path()
		// Skip Origin CSRF check for webhook endpoints and Bearer-token API/MCP routes
		if !strings.HasPrefix(path, "/webhooks/") &&
			!strings.HasPrefix(path, "/api/v1/") &&
			path != "/mcp" && !strings.HasPrefix(path, "/mcp/") {
			origin := strings.TrimSpace(c.Get("Origin"))
			if origin != "" && origin != "null" {
				u, err := url.Parse(origin)
				if err != nil || u.Hostname() == "" {
					return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"ok": false, "error": "forbidden: invalid Origin header"})
				}
				originHost := u.Hostname()
				reqHost := c.Hostname()
				if h, _, err := net.SplitHostPort(reqHost); err == nil {
					reqHost = h
				}
				isLocal := (originHost == "localhost" || originHost == "127.0.0.1") && (reqHost == "localhost" || reqHost == "127.0.0.1")
				if !isLocal && !strings.EqualFold(originHost, reqHost) {
					var panelDomain string
					if p != nil && p.DB != nil {
						panelDomain = strings.TrimSpace(p.DB.GetSetting(c.UserContext(), "panel_domain"))
						if h, _, err := net.SplitHostPort(panelDomain); err == nil {
							panelDomain = h
						}
					}
					if panelDomain == "" || !strings.EqualFold(originHost, panelDomain) {
						return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"ok": false, "error": "forbidden: cross-origin state-changing request"})
					}
				}
			}
		}
	}
	return c.Next()
}

// WantsJSON returns true if the client explicitly requested JSON or called /api/panel/*.
func WantsJSON(c *fiber.Ctx) bool {
	if strings.HasPrefix(c.Path(), "/api/") || c.Query("format") == "json" {
		return true
	}
	accept := strings.ToLower(c.Get("Accept"))
	if strings.Contains(accept, "application/json") {
		return true
	}
	return strings.EqualFold(c.Get("X-Requested-With"), "XMLHttpRequest") ||
		strings.EqualFold(c.Get("X-NextDeploy-Client"), "spa")
}
