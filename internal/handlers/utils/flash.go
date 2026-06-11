package utils

import (
	"net/url"

	"github.com/gofiber/fiber/v2"
)

const (
	FlashCookieFlash = "p_flash"
	FlashCookieError = "p_error"
)

func SetFlash(c *fiber.Ctx, msg string) {
	c.Cookie(&fiber.Cookie{
		Name:     FlashCookieFlash,
		Value:    url.QueryEscape(msg),
		Path:     "/",
		MaxAge:   30,
		HTTPOnly: true,
		SameSite: "Lax",
	})
}

func SetFlashError(c *fiber.Ctx, msg string) {
	c.Cookie(&fiber.Cookie{
		Name:     FlashCookieError,
		Value:    url.QueryEscape(msg),
		Path:     "/",
		MaxAge:   30,
		HTTPOnly: true,
		SameSite: "Lax",
	})
}

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

func ReadFlash(c *fiber.Ctx) string {
	if raw := c.Cookies(FlashCookieFlash); raw != "" {
		clearFlashCookie(c, FlashCookieFlash)
		if decoded, err := url.QueryUnescape(raw); err == nil {
			return decoded
		}
		return raw
	}
	return c.Query("flash")
}

func ReadFlashError(c *fiber.Ctx) string {
	if raw := c.Cookies(FlashCookieError); raw != "" {
		clearFlashCookie(c, FlashCookieError)
		if decoded, err := url.QueryUnescape(raw); err == nil {
			return decoded
		}
		return raw
	}
	return c.Query("error")
}
