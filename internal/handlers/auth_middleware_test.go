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

	// Root path / must bypass AuthMiddleware directly to avoid 302 redirects
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	resp, err = app.Test(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("expected / to bypass auth, got status %d, err %v", resp.StatusCode, err)
	}
}

func TestServeSPAEtagAnd304(t *testing.T) {
	p := &Panel{SPAIndexHTML: []byte("<!DOCTYPE html><html><head><title>NextDeploy</title></head><body>Root</body></html>")}
	app := fiber.New()
	app.Get("/", p.ServeSPA)

	// First request: should return 200 with Cache-Control: no-cache and ETag
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
	cc := resp.Header.Get("Cache-Control")
	if cc != "no-cache" {
		t.Fatalf("expected Cache-Control no-cache, got %q", cc)
	}
	etag := resp.Header.Get("ETag")
	if etag == "" || len(etag) != 16 { // W/" + 12 hex chars + "
		t.Fatalf("expected valid 12-char hex weak ETag, got %q", etag)
	}

	// Second request with If-None-Match matching etag: should return 304 Not Modified
	req304 := httptest.NewRequest(http.MethodGet, "/", nil)
	req304.Header.Set("If-None-Match", etag)
	resp304, err := app.Test(req304)
	if err != nil {
		t.Fatalf("unexpected error on 304 request: %v", err)
	}
	if resp304.StatusCode != http.StatusNotModified {
		t.Fatalf("expected status 304 Not Modified, got %d", resp304.StatusCode)
	}

	// Third request with strong quoted format: should also return 304 (RFC 7232 weak comparison)
	rawTag := etag[2:] // removes W/
	reqStrong := httptest.NewRequest(http.MethodGet, "/", nil)
	reqStrong.Header.Set("If-None-Match", rawTag)
	respStrong, err := app.Test(reqStrong)
	if err != nil || respStrong.StatusCode != http.StatusNotModified {
		t.Fatalf("expected status 304 for strong quoted ETag, got %d, err %v", respStrong.StatusCode, err)
	}

	// Fourth request with If-None-Match: *
	reqStar := httptest.NewRequest(http.MethodGet, "/", nil)
	reqStar.Header.Set("If-None-Match", "*")
	respStar, err := app.Test(reqStar)
	if err != nil || respStar.StatusCode != http.StatusNotModified {
		t.Fatalf("expected status 304 for wildcard *, got %d, err %v", respStar.StatusCode, err)
	}
}

func TestLoggedInCookieLifecycle(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "cookie_test_*")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := db.Open(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatalf("db.Open failed: %v", err)
	}
	defer store.Close()

	ctx := t.Context()
	userID, err := store.CreateUser(ctx, "testadmin", "somehash", db.RoleAdmin)
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	p := &Panel{DB: store, SPAIndexHTML: []byte("<html>SPA</html>")}
	app := fiber.New()
	app.Use(p.AuthMiddleware)
	app.Get("/", p.ServeSPA)
	app.Post("/login", func(c *fiber.Ctx) error {
		return p.createSessionAndRedirect(c, userID, "/")
	})
	app.Get("/api/panel/bootstrap", p.SPABootstrap)
	app.Post("/logout", p.Logout)

	// Create session: verify nd_logged_in=1 cookie is present
	req := httptest.NewRequest(http.MethodPost, "/login", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("session create request failed: %v", err)
	}
	var foundLoggedInCookie bool
	var sessionToken string
	for _, cookie := range resp.Cookies() {
		if cookie.Name == "nd_logged_in" && cookie.Value == "1" && !cookie.HttpOnly && cookie.SameSite == http.SameSiteLaxMode {
			foundLoggedInCookie = true
		}
		if cookie.Name == "nd_session" {
			sessionToken = cookie.Value
		}
	}
	if !foundLoggedInCookie || sessionToken == "" {
		t.Fatalf("expected nd_logged_in=1 and nd_session, got cookies: %+v", resp.Cookies())
	}

	// Request root path with nd_session but WITHOUT nd_logged_in:
	// ServeSPA on / should auto-sync and set nd_logged_in=1
	rootReq := httptest.NewRequest(http.MethodGet, "/", nil)
	rootReq.AddCookie(&http.Cookie{Name: "nd_session", Value: sessionToken})
	rootResp, err := app.Test(rootReq)
	if err != nil {
		t.Fatalf("root request failed: %v", err)
	}
	var rootSyncLoggedInCookie bool
	for _, cookie := range rootResp.Cookies() {
		if cookie.Name == "nd_logged_in" && cookie.Value == "1" {
			rootSyncLoggedInCookie = true
		}
	}
	if !rootSyncLoggedInCookie {
		t.Fatalf("expected ServeSPA on / to auto-sync nd_logged_in=1 when nd_session is present, got cookies: %+v", rootResp.Cookies())
	}

	// Request protected API with empty nd_session: should clear nd_logged_in
	unauthReq := httptest.NewRequest(http.MethodGet, "/api/panel/bootstrap", nil)
	unauthReq.Header.Set("Accept", "application/json")
	unauthReq.AddCookie(&http.Cookie{Name: "nd_logged_in", Value: "1"})
	unauthResp, err := app.Test(unauthReq)
	if err != nil {
		t.Fatalf("unauth request failed: %v", err)
	}
	var unauthClearedCookie bool
	for _, cookie := range unauthResp.Cookies() {
		if cookie.Name == "nd_logged_in" && (cookie.MaxAge < 0 || cookie.Value == "") {
			unauthClearedCookie = true
		}
	}
	if !unauthClearedCookie {
		t.Fatalf("expected AuthMiddleware to clear nd_logged_in when token is missing, got: %+v", unauthResp.Cookies())
	}

	// Logout: verify nd_logged_in cookie is cleared
	logoutReq := httptest.NewRequest(http.MethodPost, "/logout", nil)
	logoutReq.AddCookie(&http.Cookie{Name: "nd_session", Value: sessionToken})
	logoutResp, err := app.Test(logoutReq)
	if err != nil {
		t.Fatalf("logout request failed: %v", err)
	}
	var clearedLoggedInCookie bool
	for _, cookie := range logoutResp.Cookies() {
		if cookie.Name == "nd_logged_in" && (cookie.MaxAge < 0 || cookie.Value == "") {
			clearedLoggedInCookie = true
		}
	}
	if !clearedLoggedInCookie {
		t.Fatalf("expected nd_logged_in cookie to be cleared on logout, got: %+v", logoutResp.Cookies())
	}
}
