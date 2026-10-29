package handlers

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"panel/internal/db"

	"github.com/gofiber/fiber/v2"
)

func TestAuthMiddlewareStaticBypass(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "auth_test_*")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := db.Open(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatalf("db.Open failed: %v", err)
	}
	defer store.Close()

	p := &Panel{DB: store}
	app := fiber.New()
	app.Use(p.AuthMiddleware)
	app.Get("/*", func(c *fiber.Ctx) error {
		return c.SendString("ok")
	})

	// Public static assets should bypass auth and reach handler (200)
	req := httptest.NewRequest(http.MethodGet, "/static/css/app.css", nil)
	resp, err := app.Test(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("expected /static/css/app.css to bypass auth, got status %d, err %v", resp.StatusCode, err)
	}

	// Suffix .png outside /phpmyadmin should bypass auth (200)
	req = httptest.NewRequest(http.MethodGet, "/favicon.png", nil)
	resp, err = app.Test(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("expected /favicon.png to bypass auth, got status %d, err %v", resp.StatusCode, err)
	}

	// Assets under /phpmyadmin/ must NOT bypass AuthMiddleware!
	// Without session cookie, it should redirect (302) or fail auth, NOT return 200
	req = httptest.NewRequest(http.MethodGet, "/phpmyadmin/themes/pmahomme/img/logo_left.png", nil)
	resp, err = app.Test(req)
	if err != nil {
		t.Fatalf("unexpected test error: %v", err)
	}
	if resp.StatusCode == http.StatusOK {
		t.Fatalf("expected /phpmyadmin logo asset to require auth, but it bypassed AuthMiddleware and returned 200")
	}
}
