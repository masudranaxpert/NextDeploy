package perflog

import (
	"fmt"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
)

func Middleware() fiber.Handler {
	return func(c *fiber.Ctx) error {
		if !Enabled() {
			return c.Next()
		}
		start := time.Now()
		err := c.Next()
		var parts []string
		parts = append(parts, fmt.Sprintf("status=%d", c.Response().StatusCode()))
		if hx := strings.TrimSpace(c.Get("HX-Request")); hx != "" {
			parts = append(parts, "hx="+hx)
		}
		if target := strings.TrimSpace(c.Get("HX-Target")); target != "" {
			parts = append(parts, "hx-target="+target)
		}
		if tab := strings.TrimSpace(c.Query("tab")); tab != "" {
			parts = append(parts, "tab="+tab)
		}
		if partial := strings.TrimSpace(c.Query("partial")); partial != "" {
			parts = append(parts, "partial="+partial)
		}
		if path := strings.TrimSpace(c.Query("path")); path != "" {
			parts = append(parts, "path="+path)
		}
		query := string(c.Context().URI().QueryString())
		if query != "" && len(query) <= 120 {
			parts = append(parts, "query="+query)
		}
		Logf("HTTP %s %s total=%s %s", c.Method(), c.Path(), FormatDur(time.Since(start)), strings.Join(parts, " "))
		return err
	}
}
