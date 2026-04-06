package handlers

import (
	"encoding/base64"

	"github.com/gofiber/fiber/v2"
)

const (
	gitFlashCookiePrefix = "nd_gf_"
	gitErrCookiePrefix   = "nd_ge_"
)

func gitFlashCookieName(appID string) string {
	return gitFlashCookiePrefix + appID
}

func gitErrCookieName(appID string) string {
	return gitErrCookiePrefix + appID
}

func clearPanelCookie(c *fiber.Ctx, name string) {
	c.Cookie(&fiber.Cookie{
		Name:     name,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HTTPOnly: true,
		SameSite: "Lax",
	})
}

// consumeGitTabFlash reads and clears one-time flash cookies after Git save/sync redirects.
func (p *Panel) consumeGitTabFlash(c *fiber.Ctx, appID string) (saved, synced bool, errMsg string) {
	fn := gitFlashCookieName(appID)
	flash := c.Cookies(fn)
	if flash != "" {
		clearPanelCookie(c, fn)
	}
	switch flash {
	case "saved":
		saved = true
	case "synced":
		synced = true
	case "saved_synced":
		saved, synced = true, true
	}
	en := gitErrCookieName(appID)
	if ev := c.Cookies(en); ev != "" {
		clearPanelCookie(c, en)
		raw, err := base64.RawURLEncoding.DecodeString(ev)
		if err == nil {
			errMsg = string(raw)
		}
	}
	return
}

func (p *Panel) setGitTabFlashCookie(c *fiber.Ctx, appID, flash string) {
	c.Cookie(&fiber.Cookie{
		Name:     gitFlashCookieName(appID),
		Value:    flash,
		Path:     "/",
		MaxAge:   120,
		HTTPOnly: true,
		SameSite: "Lax",
	})
}

func (p *Panel) setGitTabErrorCookie(c *fiber.Ctx, appID, msg string) {
	if msg == "" {
		return
	}
	b := []byte(msg)
	if len(b) > 3500 {
		b = b[:3500]
	}
	enc := base64.RawURLEncoding.EncodeToString(b)
	c.Cookie(&fiber.Cookie{
		Name:     gitErrCookieName(appID),
		Value:    enc,
		Path:     "/",
		MaxAge:   120,
		HTTPOnly: true,
		SameSite: "Lax",
	})
}
