package handlers

import (
	"net/url"

	"github.com/gofiber/fiber/v2"
)

// Flash cookie names — one-time messages passed across redirects without
// polluting the URL. Each page reads and immediately clears its own cookie.
const (
	flashCookieFlash = "p_flash" // success / info message
	flashCookieError = "p_error" // error message
)

// setFlash sets a short-lived success flash cookie.
func setFlash(c *fiber.Ctx, msg string) {
	c.Cookie(&fiber.Cookie{
		Name:     flashCookieFlash,
		Value:    url.QueryEscape(msg),
		Path:     "/",
		MaxAge:   30,
		HTTPOnly: true,
		SameSite: "Lax",
	})
}

// setFlashError sets a short-lived error flash cookie.
func setFlashError(c *fiber.Ctx, msg string) {
	c.Cookie(&fiber.Cookie{
		Name:     flashCookieError,
		Value:    url.QueryEscape(msg),
		Path:     "/",
		MaxAge:   30,
		HTTPOnly: true,
		SameSite: "Lax",
	})
}

// clearFlashCookie expires a flash cookie immediately.
func clearFlashCookie(c *fiber.Ctx, name string) {
	c.Cookie(&fiber.Cookie{
		Name:     name,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HTTPOnly: true,
		SameSite: "Lax",
	})
}

// readFlash reads and clears the success flash cookie.
// Falls back to the legacy ?flash= query param so old bookmarks still work.
func readFlash(c *fiber.Ctx) string {
	if raw := c.Cookies(flashCookieFlash); raw != "" {
		clearFlashCookie(c, flashCookieFlash)
		if decoded, err := url.QueryUnescape(raw); err == nil {
			return decoded
		}
		return raw
	}
	return c.Query("flash")
}

// readFlashError reads and clears the error flash cookie.
// Falls back to the legacy ?error= query param.
func readFlashError(c *fiber.Ctx) string {
	if raw := c.Cookies(flashCookieError); raw != "" {
		clearFlashCookie(c, flashCookieError)
		if decoded, err := url.QueryUnescape(raw); err == nil {
			return decoded
		}
		return raw
	}
	return c.Query("error")
}
