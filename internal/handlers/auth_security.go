package handlers

import (
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
)

const (
	loginFailMaxAttempts = 5
	loginFailWindow      = 5 * time.Minute
	loginFailLockout     = 30 * time.Second
)

type loginFailEntry struct {
	count       int
	windowStart time.Time
	lockedUntil time.Time
}

func (p *Panel) checkLoginAllowed(username string) (bool, time.Duration) {
	username = strings.ToLower(strings.TrimSpace(username))
	now := time.Now()
	p.loginFailMu.Lock()
	defer p.loginFailMu.Unlock()
	if p.loginFails == nil {
		p.loginFails = make(map[string]loginFailEntry)
	}
	e := p.loginFails[username]
	if now.Before(e.lockedUntil) {
		return false, e.lockedUntil.Sub(now)
	}
	if e.windowStart.IsZero() || now.Sub(e.windowStart) > loginFailWindow {
		e.count = 0
		e.windowStart = now
	}
	p.loginFails[username] = e
	return true, 0
}

func (p *Panel) recordLoginFailure(username string) {
	username = strings.ToLower(strings.TrimSpace(username))
	now := time.Now()
	p.loginFailMu.Lock()
	defer p.loginFailMu.Unlock()
	if p.loginFails == nil {
		p.loginFails = make(map[string]loginFailEntry)
	}
	e := p.loginFails[username]
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
	p.loginFails[username] = e
}

func (p *Panel) clearLoginFailures(username string) {
	username = strings.ToLower(strings.TrimSpace(username))
	p.loginFailMu.Lock()
	defer p.loginFailMu.Unlock()
	delete(p.loginFails, username)
}

func sessionCookieClear() *fiber.Cookie {
	return &fiber.Cookie{
		Name:     sessionCookie,
		Value:    "",
		MaxAge:   -1,
		HTTPOnly: true,
		SameSite: "Lax",
	}
}

func sessionCookieSet(token string, expires time.Time) *fiber.Cookie {
	return &fiber.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Expires:  expires,
		HTTPOnly: true,
		SameSite: "Lax",
		Path:     "/",
	}
}
