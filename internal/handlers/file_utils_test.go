package handlers

import (
	"encoding/base64"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func TestResolveSafePathQuery(t *testing.T) {
	app := fiber.New()

	var got string
	app.Get("/test", func(c *fiber.Ctx) error {
		got = ResolveSafePathQuery(c)
		return c.SendString("ok")
	})

	// 1. Standard path query parameter
	req := httptest.NewRequest("GET", "/test?path=htdocs/index.php", nil)
	_, _ = app.Test(req)
	if got != "htdocs/index.php" {
		t.Fatalf("expected htdocs/index.php, got %q", got)
	}

	// 2. Base64 encoded path (bypasses WAF on sensitive files like wp-config.php)
	raw := "htdocs/wp-config.php"
	b64 := base64.RawURLEncoding.EncodeToString([]byte(raw))
	req = httptest.NewRequest("GET", "/test?p_b64="+b64, nil)
	_, _ = app.Test(req)
	if got != raw {
		t.Fatalf("expected %q, got %q", raw, got)
	}

	// 3. Standard base64 with padding
	b64Std := base64.StdEncoding.EncodeToString([]byte(".env.production"))
	req = httptest.NewRequest("GET", "/test?p_b64="+b64Std, nil)
	_, _ = app.Test(req)
	if got != ".env.production" {
		t.Fatalf("expected .env.production, got %q", got)
	}

	// 4. Fallback when both empty
	req = httptest.NewRequest("GET", "/test", nil)
	_, _ = app.Test(req)
	if got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}
