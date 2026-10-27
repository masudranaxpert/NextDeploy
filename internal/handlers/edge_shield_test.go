package handlers

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"panel/internal/db"

	"github.com/gofiber/fiber/v2"
)

func TestEdgeShieldEndpoints(t *testing.T) {
	tmpDB := filepath.Join(t.TempDir(), "test_edge_shield.db")

	store, err := db.Open(tmpDB)
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	defer store.Close()

	p := &Panel{DB: store}
	app := fiber.New()

	app.Get("/api/v1/edge/verify-domain", p.EdgeVerifyDomain)
	app.Get("/api/v1/edge/setup.sh", p.GetEdgeShieldSetupScript)
	app.Get("/api/v1/edge/origin-ca.crt", p.GetOriginCaddyRootCA)
	app.Post("/api/v1/edge/shield-heartbeat", p.EdgeShieldHeartbeat)
	app.Get("/api/v1/shield-nodes", p.RequireAdminMiddleware, p.ListShieldNodes)
	app.Post("/api/v1/shield-nodes", p.RequireAdminMiddleware, p.CreateShieldNode)

	ctx := context.Background()

	// 1. Initially verify-domain without domain param -> 400
	req := httptest.NewRequest("GET", "/api/v1/edge/verify-domain", nil)
	resp, err := app.Test(req)
	if err != nil || resp.StatusCode != 400 {
		t.Fatalf("expected 400 for empty domain, got %v (status %d)", err, resp.StatusCode)
	}

	// 2. Verify an unconfigured domain -> 404
	req = httptest.NewRequest("GET", "/api/v1/edge/verify-domain?domain=unknownsite.com", nil)
	resp, err = app.Test(req)
	if err != nil || resp.StatusCode != 404 {
		t.Fatalf("expected 404 for unknown domain, got %v (status %d)", err, resp.StatusCode)
	}

	// 3. Add an app and domain
	_, _ = store.ExecRaw(ctx, `INSERT INTO apps (id, name, created_at) VALUES ('app1', 'App One', CURRENT_TIMESTAMP)`)
	_, err = store.CreateAppDomain(ctx, db.AppDomain{
		AppID:  "app1",
		Domain: "myapp.com",
	})
	if err != nil {
		t.Fatalf("failed to create test app domain: %v", err)
	}

	// 4. Now verify-domain for myapp.com -> 200 OK
	req = httptest.NewRequest("GET", "/api/v1/edge/verify-domain?domain=myapp.com", nil)
	resp, err = app.Test(req)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("expected 200 for registered app domain, got %v (status %d)", err, resp.StatusCode)
	}

	// 5. Register a Shield Node
	nodeID, err := store.CreateShieldNode(ctx, &db.ShieldNode{
		Name:  "Test Shield",
		IP:    "185.220.101.5",
		Token: "test_token_123",
	})
	if err != nil {
		t.Fatalf("failed to create shield node: %v", err)
	}
	if nodeID <= 0 {
		t.Fatalf("expected positive node ID, got %d", nodeID)
	}

	// 6. With shield node present, call without token -> 401 Unauthorized
	req = httptest.NewRequest("GET", "/api/v1/edge/verify-domain?domain=myapp.com", nil)
	resp, err = app.Test(req)
	if err != nil || resp.StatusCode != 401 {
		t.Fatalf("expected 401 when token missing with registered shield nodes, got %d", resp.StatusCode)
	}

	// 7. Call with valid token -> 200 OK
	req = httptest.NewRequest("GET", "/api/v1/edge/verify-domain?domain=myapp.com&token=test_token_123", nil)
	resp, err = app.Test(req)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("expected 200 with valid token, got %d", resp.StatusCode)
	}

	// 8. Test setup script endpoint
	req = httptest.NewRequest("GET", "/api/v1/edge/setup.sh?token=test_token_123", nil)
	resp, err = app.Test(req, 5000)
	if err != nil {
		t.Fatalf("setup script request failed: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200 for setup script, got %d", resp.StatusCode)
	}

	// 8b. Test origin-ca.crt endpoint (without token -> 400, invalid token -> 401)
	req = httptest.NewRequest("GET", "/api/v1/edge/origin-ca.crt", nil)
	resp, err = app.Test(req)
	if err != nil || resp.StatusCode != 400 {
		t.Fatalf("expected 400 for origin-ca.crt without token, got %v (status %d)", err, resp.StatusCode)
	}

	req = httptest.NewRequest("GET", "/api/v1/edge/origin-ca.crt?token=invalid_token", nil)
	resp, err = app.Test(req)
	if err != nil || resp.StatusCode != 401 {
		t.Fatalf("expected 401 for origin-ca.crt with invalid token, got %v (status %d)", err, resp.StatusCode)
	}

	// 9. Test heartbeat endpoint
	req = httptest.NewRequest("POST", "/api/v1/edge/shield-heartbeat?token=test_token_123", nil)
	resp, err = app.Test(req)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("expected 200 for heartbeat, got %d", resp.StatusCode)
	}

	// 10. Check that the node status transitioned to 'connected'
	node, err := store.GetShieldNode(ctx, nodeID)
	if err != nil || node.Status != "connected" {
		t.Fatalf("expected status connected after heartbeat, got %v (err: %v)", node.Status, err)
	}

	// 11. Test that calling /api/v1/shield-nodes without session cookie returns 401
	req = httptest.NewRequest("GET", "/api/v1/shield-nodes", nil)
	resp, err = app.Test(req)
	if err != nil || resp.StatusCode != 401 {
		t.Fatalf("expected 401 without session cookie, got %d", resp.StatusCode)
	}

	// 12. Create admin user and session, then call with session cookie -> 200 OK
	adminID, err := store.CreateUser(ctx, "admin_user", "hash", db.RoleAdmin)
	if err != nil {
		t.Fatalf("failed to create admin user: %v", err)
	}
	sessToken := "admin_session_test_token"
	err = store.CreateSession(ctx, sessToken, adminID, time.Now().Add(24*time.Hour))
	if err != nil {
		t.Fatalf("failed to create admin session: %v", err)
	}

	req = httptest.NewRequest("GET", "/api/v1/shield-nodes", nil)
	req.Header.Set("Cookie", sessionCookie+"="+sessToken)
	resp, err = app.Test(req)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("expected 200 with admin session cookie on /api/v1/shield-nodes, got %d", resp.StatusCode)
	}
}
