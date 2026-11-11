package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"panel/internal/caddy"
	"panel/internal/db"
	"panel/internal/dockerx"
	"panel/internal/workspace"

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

	// 7. Direct domain myapp.com must return 404 for a Shield token (Phase 0 #7: node-scoped filter)
	req = httptest.NewRequest("GET", "/api/v1/edge/verify-domain?domain=myapp.com&token=test_token_123", nil)
	resp, err = app.Test(req)
	if err != nil || resp.StatusCode != 404 {
		t.Fatalf("expected 404 for direct domain with shield token, got %d", resp.StatusCode)
	}
	if resp.Header.Get("Deprecation") != "true" {
		t.Fatalf("expected Deprecation: true header on legacy verify-domain")
	}

	// 7b. Shielded domain assigned to this node -> 200 OK
	_, err = store.CreateAppDomain(ctx, db.AppDomain{
		AppID:        "app1",
		Domain:       "shielded.example.com",
		ShieldNodeID: &nodeID,
	})
	if err != nil {
		t.Fatalf("failed to create shielded domain: %v", err)
	}
	req = httptest.NewRequest("GET", "/api/v1/edge/verify-domain?domain=shielded.example.com&token=test_token_123", nil)
	resp, err = app.Test(req)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("expected 200 for node-assigned shielded domain, got %d", resp.StatusCode)
	}

	// 7c. Domain assigned to another node -> 404
	otherNodeID, _ := store.CreateShieldNode(ctx, &db.ShieldNode{
		Name:  "Other Shield",
		IP:    "185.220.101.6",
		Token: "other_token_456",
	})
	_, _ = store.CreateAppDomain(ctx, db.AppDomain{
		AppID:        "app1",
		Domain:       "other-node.example.com",
		ShieldNodeID: &otherNodeID,
	})
	req = httptest.NewRequest("GET", "/api/v1/edge/verify-domain?domain=other-node.example.com&token=test_token_123", nil)
	resp, err = app.Test(req)
	if err != nil || resp.StatusCode != 404 {
		t.Fatalf("expected 404 for domain on another shield node, got %d", resp.StatusCode)
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

func setupTempRootCompose(t *testing.T) string {
	t.Helper()
	composePath := filepath.Join(t.TempDir(), "docker-compose.yml")
	base := `services:
  caddy:
    image: lucaslorentz/caddy-docker-proxy:ci-alpine
  panel:
    image: nextdeploy-panel:latest
`
	if err := os.WriteFile(composePath, []byte(base), 0640); err != nil {
		t.Fatalf("write temp compose: %v", err)
	}
	t.Setenv("PANEL_STACK_COMPOSE_FILE", composePath)
	return composePath
}

func TestPhase0SyncShieldProxiesToCaddy(t *testing.T) {
	composePath := setupTempRootCompose(t)
	store, err := db.Open(filepath.Join(t.TempDir(), "phase0_sync.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	var applyCalls int
	p := &Panel{
		DB: store,
		applyRootStackComposeFn: func(file string, services ...string) {
			applyCalls++
		},
	}

	ctx := context.Background()
	// Pre-populate a user-defined custom proxy so we verify it is preserved
	seedOpts, _ := json.Marshal(caddy.GlobalOptions{
		Profile:        "custom",
		RealIP:         "custom",
		TrustedProxies: []string{"10.99.0.1"},
	})
	_ = store.SetCaddyConfig(ctx, caddyOptionsKey, string(seedOpts))

	n1, _ := store.CreateShieldNode(ctx, &db.ShieldNode{Name: "s1", IP: "107.189.6.222", Token: "t1"})
	p.syncShieldProxiesToCaddy(ctx)

	opts := p.caddyGlobalOptions(ctx)
	if opts.RealIP != "custom" {
		t.Fatalf("expected RealIP=custom, got %q", opts.RealIP)
	}
	if len(opts.TrustedProxies) != 2 || opts.TrustedProxies[0] != "10.99.0.1" || opts.TrustedProxies[1] != "107.189.6.222" {
		t.Fatalf("unexpected TrustedProxies after add: %v", opts.TrustedProxies)
	}
	if applyCalls != 1 {
		t.Fatalf("expected 1 background compose apply, got %d", applyCalls)
	}
	rawCompose, _ := os.ReadFile(composePath)
	if !bytes.Contains(rawCompose, []byte("107.189.6.222")) {
		t.Fatalf("expected root compose to contain shield IP, got:\n%s", string(rawCompose))
	}

	// Update IP of node 1 -> old IP must be pruned, new IP added
	_ = store.UpdateShieldNode(ctx, &db.ShieldNode{ID: n1, Name: "s1", IP: "107.189.6.223"})
	p.syncShieldProxiesToCaddy(ctx, "107.189.6.222")
	opts = p.caddyGlobalOptions(ctx)
	if len(opts.TrustedProxies) != 2 || opts.TrustedProxies[1] != "107.189.6.223" {
		t.Fatalf("expected old shield IP pruned and replaced: %v", opts.TrustedProxies)
	}

	// Delete last shield node (len(nodes) == 0) -> shield IP still pruned!
	_ = store.DeleteShieldNode(ctx, n1)
	p.syncShieldProxiesToCaddy(ctx, "107.189.6.223")
	opts = p.caddyGlobalOptions(ctx)
	if len(opts.TrustedProxies) != 1 || opts.TrustedProxies[0] != "10.99.0.1" {
		t.Fatalf("expected shield IP pruned when len(nodes)==0 while preserving user proxy: %v", opts.TrustedProxies)
	}

	// If no user proxy was present, deleting last shield resets RealIP to "none" cleanly
	_ = store.SetCaddyConfig(ctx, caddyOptionsKey, "")
	n2, _ := store.CreateShieldNode(ctx, &db.ShieldNode{Name: "s2", IP: "198.51.100.10", Token: "t2"})
	p.syncShieldProxiesToCaddy(ctx)
	_ = store.DeleteShieldNode(ctx, n2)
	p.syncShieldProxiesToCaddy(ctx, "198.51.100.10")
	opts = p.caddyGlobalOptions(ctx)
	if opts.RealIP != "none" || len(opts.TrustedProxies) != 0 {
		t.Fatalf("expected RealIP=none and empty TrustedProxies after deleting last node, got %+v", opts)
	}
}

func TestPhase0DeleteAndReassignShieldNode(t *testing.T) {
	setupTempRootCompose(t)
	wsRoot := t.TempDir()
	store, err := db.Open(filepath.Join(t.TempDir(), "phase0_reassign.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	var mu sync.Mutex
	var syncedProjects []string
	var reconciledRoutes int
	p := &Panel{
		DB:             store,
		Store:          workspace.NewStore(wsRoot),
		WorkspacesRoot: wsRoot,
		applyRootStackComposeFn: func(file string, services ...string) {
		},
		composeUpNoBuildFn: func(ctx context.Context, dir string, paths []string, project string, logW io.Writer, envFiles []string) dockerx.Result {
			mu.Lock()
			syncedProjects = append(syncedProjects, project)
			mu.Unlock()
			return dockerx.Result{OK: true}
		},
		reconcileProxyRoutesFn: func(ctx context.Context) {
			mu.Lock()
			reconciledRoutes++
			mu.Unlock()
		},
	}

	ctx := context.Background()
	adminID, _ := store.CreateUser(ctx, "admin", "hash", db.RoleAdmin)
	sessTok := "adm_sess"
	_ = store.CreateSession(ctx, sessTok, adminID, time.Now().Add(time.Hour))

	n1, _ := store.CreateShieldNode(ctx, &db.ShieldNode{Name: "s1", IP: "10.10.10.1", Token: "t1"})
	n2, _ := store.CreateShieldNode(ctx, &db.ShieldNode{Name: "s2", IP: "10.10.10.2", Token: "t2"})
	p.syncShieldProxiesToCaddy(ctx)

	_ = store.CreateApp(ctx, "app-migrate", "app-migrate", adminID)
	_ = os.MkdirAll(p.Store.Path("app-migrate"), 0750)
	_ = os.WriteFile(filepath.Join(p.Store.Path("app-migrate"), "docker-compose.yml"), []byte("services:\n  web:\n    image: nginx:alpine\n"), 0640)

	_ = store.CreateApp(ctx, "app-stay", "app-stay", adminID)
	_ = os.MkdirAll(p.Store.Path("app-stay"), 0750)
	_ = os.WriteFile(filepath.Join(p.Store.Path("app-stay"), "docker-compose.yml"), []byte("services:\n  web:\n    image: nginx:alpine\n"), 0640)

	// Domain on n1 (will be migrated to n2)
	migDomID, _ := store.CreateAppDomain(ctx, db.AppDomain{
		AppID:        "app-migrate",
		Domain:       "app-migrate-10-10-10-1.sslip.io",
		Service:      "web",
		Port:         80,
		ShieldNodeID: &n1,
	})
	// Domain already on n2 whose name happens to contain 10-10-10-1 — must NOT be rewritten by REPLACE!
	stayDomID, _ := store.CreateAppDomain(ctx, db.AppDomain{
		AppID:        "app-stay",
		Domain:       "keep-10-10-10-1.sslip.io",
		Service:      "web",
		Port:         80,
		ShieldNodeID: &n2,
	})
	_, _ = store.SaveProxyRoute(ctx, db.ProxyRoute{
		Kind: "proxy", Domain: "route1.example.com", Target: "http://127.0.0.1:9000", Enabled: true, ShieldNodeID: &n1,
	})

	app := fiber.New()
	app.Post("/api/v1/shield-nodes/:id/reassign", p.RequireAdminMiddleware, p.ReassignShieldNodeDomains)
	app.Delete("/api/v1/shield-nodes/:id", p.RequireAdminMiddleware, p.DeleteShieldNode)

	body, _ := json.Marshal(map[string]any{"target_shield_id": n2})
	req := httptest.NewRequest("POST", "/api/v1/shield-nodes/1/reassign", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", sessionCookie+"="+sessTok)
	resp, err := app.Test(req, 5000)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("reassign failed: err=%v status=%d", err, resp.StatusCode)
	}

	migDom, _ := store.GetAppDomain(ctx, migDomID)
	if migDom.Domain != "app-migrate-10-10-10-2.sslip.io" || migDom.ShieldNodeID == nil || *migDom.ShieldNodeID != n2 {
		t.Fatalf("migrated domain not updated properly: %+v", migDom)
	}
	stayDom, _ := store.GetAppDomain(ctx, stayDomID)
	if stayDom.Domain != "keep-10-10-10-1.sslip.io" {
		t.Fatalf("existing domain on target node was wrongly rewritten: %s", stayDom.Domain)
	}

	// Wait briefly for background composeUpNoBuildFn
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		gotApps := len(syncedProjects)
		gotRoutes := reconciledRoutes
		mu.Unlock()
		if gotApps >= 1 && gotRoutes >= 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	mu.Lock()
	if len(syncedProjects) == 0 || reconciledRoutes != 1 {
		t.Fatalf("expected resync of migrated app and proxy route, got projects=%v routes=%d", syncedProjects, reconciledRoutes)
	}
	syncedProjects = nil
	reconciledRoutes = 0
	mu.Unlock()

	// Now DeleteShieldNode n2 -> app-migrate and app-stay and route1 must resync
	req = httptest.NewRequest("DELETE", "/api/v1/shield-nodes/2", nil)
	req.Header.Set("Cookie", sessionCookie+"="+sessTok)
	resp, err = app.Test(req, 5000)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("delete shield node failed: err=%v status=%d", err, resp.StatusCode)
	}

	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		gotApps := len(syncedProjects)
		gotRoutes := reconciledRoutes
		mu.Unlock()
		if gotApps >= 2 && gotRoutes >= 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	mu.Lock()
	if len(syncedProjects) < 2 || reconciledRoutes != 1 {
		t.Fatalf("expected delete to resync 2 apps and proxy route, got projects=%v routes=%d", syncedProjects, reconciledRoutes)
	}
	mu.Unlock()
}

func TestPhase0ResolveUserShieldExplicitOnly(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "phase0_resolve.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	n1, _ := store.CreateShieldNode(ctx, &db.ShieldNode{Name: "s1", IP: "10.0.0.1", Token: "t1", Status: "connected"})
	p := &Panel{DB: store}

	userWithoutShield := &db.User{ID: 2, Username: "alice", Role: db.RoleUser, DefaultShieldID: nil}
	if got := p.ResolveUserShield(ctx, userWithoutShield); got != nil {
		t.Fatalf("expected nil shield for non-admin without default_shield_id, got %+v", got)
	}

	userWithShield := &db.User{ID: 3, Username: "bob", Role: db.RoleUser, DefaultShieldID: &n1}
	if got := p.ResolveUserShield(ctx, userWithShield); got == nil || got.ID != n1 {
		t.Fatalf("expected node %d for user with explicit default_shield_id, got %+v", n1, got)
	}
}

func TestPhase0PendingOriginApplyEOrigin6(t *testing.T) {
	wsRoot := t.TempDir()
	store, err := db.Open(filepath.Join(t.TempDir(), "phase0_eorigin6.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	proceedCh := make(chan struct{})
	doneCh := make(chan struct{})
	p := &Panel{
		DB:             store,
		Store:          workspace.NewStore(wsRoot),
		WorkspacesRoot: wsRoot,
		composeUpNoBuildFn: func(ctx context.Context, dir string, paths []string, project string, logW io.Writer, envFiles []string) dockerx.Result {
			<-proceedCh
			close(doneCh)
			return dockerx.Result{OK: true}
		},
	}

	ctx := context.Background()
	adminID, _ := store.CreateUser(ctx, "admin", "hash", db.RoleAdmin)
	sessTok := "adm_sess_e6"
	_ = store.CreateSession(ctx, sessTok, adminID, time.Now().Add(time.Hour))
	_ = store.CreateApp(ctx, "app-e6", "app-e6", adminID)
	_ = os.MkdirAll(p.Store.Path("app-e6"), 0750)
	_ = os.WriteFile(filepath.Join(p.Store.Path("app-e6"), "docker-compose.yml"), []byte("services:\n  web:\n    image: nginx:alpine\n"), 0640)

	n1, _ := store.CreateShieldNode(ctx, &db.ShieldNode{Name: "s1", IP: "10.0.0.1", Token: "t1", Status: "connected"})
	domID, _ := store.CreateAppDomain(ctx, db.AppDomain{AppID: "app-e6", Domain: "e6.example.com", Service: "web", Port: 80, EnableHTTPS: true})

	app := fiber.New()
	app.Post("/apps/:id/domains/:did/edit", p.RequireAdminMiddleware, p.AppDomainEdit)

	// Switch Direct -> Shield
	form := "domain=e6.example.com&service=web&port=80&enable_https=on&shield_node_id=1"
	req := httptest.NewRequest("POST", "/apps/app-e6/domains/1/edit?json=1", bytes.NewBufferString(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Cookie", sessionCookie+"="+sessTok)
	resp, err := app.Test(req, 5000)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("AppDomainEdit failed: err=%v status=%d", err, resp.StatusCode)
	}

	// While compose apply is still in progress, pending_origin_apply must be true!
	dom, _ := store.GetAppDomain(ctx, domID)
	if !dom.PendingOriginApply || dom.ShieldNodeID == nil || *dom.ShieldNodeID != n1 {
		t.Fatalf("expected pending_origin_apply=true before compose apply finishes, got %+v", dom)
	}

	// Release compose apply -> pending_origin_apply must clear
	close(proceedCh)
	<-doneCh
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		dom, _ = store.GetAppDomain(ctx, domID)
		if !dom.PendingOriginApply {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if dom.PendingOriginApply {
		t.Fatalf("expected pending_origin_apply=false after compose apply completed")
	}
}

func readRespBody(t *testing.T, body io.ReadCloser) string {
	t.Helper()
	defer body.Close()
	b, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(b)
}

func TestEdgeShieldSetupAndUninstallWrappers(t *testing.T) {
	setupTempRootCompose(t)
	store, err := db.Open(filepath.Join(t.TempDir(), "wrappers.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	p := &Panel{
		DB:                      store,
		applyRootStackComposeFn: func(file string, services ...string) {},
	}
	app := fiber.New()
	app.Get("/api/v1/edge/setup.sh", p.GetEdgeShieldSetupScript)
	app.Get("/api/v1/edge/uninstall.sh", p.GetEdgeShieldUninstallScript)
	app.Post("/api/v1/shield-nodes", p.RequireAdminMiddleware, p.CreateShieldNode)
	app.Get("/api/v1/shield-nodes", p.RequireAdminMiddleware, p.ListShieldNodes)

	ctx := context.Background()
	panelID, err := store.EnsurePanelID(ctx)
	if err != nil || panelID == "" {
		t.Fatalf("EnsurePanelID failed: id=%q err=%v", panelID, err)
	}

	adminID, _ := store.CreateUser(ctx, "admin", "hash", db.RoleAdmin)
	sessTok := "adm_sess_wrap"
	_ = store.CreateSession(ctx, sessTok, adminID, time.Now().Add(time.Hour))

	// 1. CreateShieldNode returns setup_command, upgrade_command, uninstall_command, and panel_id.
	createBody, _ := json.Marshal(map[string]string{
		"name": "SG Shield",
		"ip":   "107.189.6.222",
	})
	req := httptest.NewRequest("POST", "http://panel.example.com:8443/api/v1/shield-nodes", bytes.NewReader(createBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("Cookie", sessionCookie+"="+sessTok)
	resp, err := app.Test(req, 5000)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("CreateShieldNode failed: err=%v status=%d", err, resp.StatusCode)
	}
	var createRes struct {
		OK               bool          `json:"ok"`
		PanelID          string        `json:"panel_id"`
		Node             db.ShieldNode `json:"node"`
		SetupCommand     string        `json:"setup_command"`
		UpgradeCommand   string        `json:"upgrade_command"`
		UninstallCommand string        `json:"uninstall_command"`
	}
	if err := json.Unmarshal([]byte(readRespBody(t, resp.Body)), &createRes); err != nil {
		t.Fatalf("decode CreateShieldNode: %v", err)
	}
	if !createRes.OK || createRes.PanelID != panelID || createRes.Node.Token == "" {
		t.Fatalf("unexpected CreateShieldNode response: %+v", createRes)
	}
	wantSetup := `curl -sSL "https://panel.example.com:8443/api/v1/edge/setup.sh?token=` + createRes.Node.Token + `" | bash`
	wantUninstall := `curl -sSL "https://panel.example.com:8443/api/v1/edge/uninstall.sh?token=` + createRes.Node.Token + `" | bash`
	if createRes.SetupCommand != wantSetup {
		t.Fatalf("setup_command mismatch:\n got: %s\nwant: %s", createRes.SetupCommand, wantSetup)
	}
	if createRes.UpgradeCommand != "nd edge upgrade" {
		t.Fatalf("expected upgrade_command 'nd edge upgrade', got %q", createRes.UpgradeCommand)
	}
	if createRes.UninstallCommand != wantUninstall {
		t.Fatalf("uninstall_command mismatch:\n got: %s\nwant: %s", createRes.UninstallCommand, wantUninstall)
	}

	tok := createRes.Node.Token

	// 2. Auth validation for setup.sh and uninstall.sh.
	for _, path := range []string{"/api/v1/edge/setup.sh", "/api/v1/edge/uninstall.sh"} {
		req = httptest.NewRequest("GET", path, nil)
		resp, _ = app.Test(req)
		if resp.StatusCode != 400 {
			t.Fatalf("expected 400 without token on %s, got %d", path, resp.StatusCode)
		}
		req = httptest.NewRequest("GET", path+"?token=wrong_tok", nil)
		resp, _ = app.Test(req)
		if resp.StatusCode != 401 {
			t.Fatalf("expected 401 with invalid token on %s, got %d", path, resp.StatusCode)
		}
	}

	// 3. Default setup.sh over HTTPS: wrapper checks OS/arch, ND_DOWNLOAD_URL, uses --token-stdin + heredoc, no raw token in CLI flags.
	req = httptest.NewRequest("GET", "https://panel.example.com:8443/api/v1/edge/setup.sh?token="+tok, nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	resp, err = app.Test(req, 5000)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("default setup.sh failed: err=%v status=%d", err, resp.StatusCode)
	}
	if resp.Header.Get("Deprecation") != "" {
		t.Fatalf("default setup.sh should not set Deprecation header, got %q", resp.Header.Get("Deprecation"))
	}
	setupScript := readRespBody(t, resp.Body)
	for _, mustHave := range []string{
		"#!/usr/bin/env bash",
		"ND_DOWNLOAD_URL",
		`BINARY="nd-${OS}-${ARCH}"`,
		"releases/latest/download/${BINARY}",
		`"$ND_BIN" edge install --panel-url 'https://panel.example.com:8443' --token-stdin <<'EOF'`,
		"\n" + tok + "\nEOF\n",
	} {
		if !strings.Contains(setupScript, mustHave) {
			t.Fatalf("default setup.sh missing %q\nscript:\n%s", mustHave, setupScript)
		}
	}
	if strings.Contains(setupScript, "--insecure-http") {
		t.Fatalf("HTTPS setup.sh should not pass --insecure-http by default")
	}
	for _, line := range strings.Split(setupScript, "\n") {
		if strings.Contains(line, "edge install") && strings.Contains(line, tok) {
			t.Fatalf("raw token leaked in nd edge install CLI line: %s", line)
		}
	}
	if strings.Contains(setupScript, "on_demand_tls") {
		t.Fatalf("default setup.sh must not emit legacy on_demand_tls catch-all")
	}

	// 4. Default setup.sh over HTTP automatically passes --insecure-http, and optional query params map cleanly.
	sha := strings.Repeat("a", 64)
	q := "token=" + tok + "&adopt=1&acme_email=ops@example.com&force=1&insecure_http=1&dns_gate=off&panel_ca_sha256=" + sha
	req = httptest.NewRequest("GET", "http://127.0.0.1:8080/api/v1/edge/setup.sh?"+q, nil)
	resp, err = app.Test(req, 5000)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("setup.sh with options failed: err=%v status=%d", err, resp.StatusCode)
	}
	optScript := readRespBody(t, resp.Body)
	for _, flag := range []string{
		"--panel-url 'http://127.0.0.1:8080'",
		"--token-stdin",
		"--adopt-existing",
		"--acme-email 'ops@example.com'",
		"--insecure-http",
		"--force",
		"--panel-ca-sha256 '" + sha + "'",
		"--dns-gate off",
	} {
		if !strings.Contains(optScript, flag) {
			t.Fatalf("setup.sh with options missing %q\nscript:\n%s", flag, optScript)
		}
	}
	if strings.Count(optScript, "--insecure-http") != 1 {
		t.Fatalf("expected --insecure-http once, got count=%d", strings.Count(optScript, "--insecure-http"))
	}

	// 4b. Auto --insecure-http when scheme == http even without insecure_http=1.
	req = httptest.NewRequest("GET", "http://127.0.0.1:8080/api/v1/edge/setup.sh?token="+tok, nil)
	resp, _ = app.Test(req, 5000)
	httpAutoScript := readRespBody(t, resp.Body)
	if !strings.Contains(httpAutoScript, "--insecure-http") {
		t.Fatalf("expected HTTP setup.sh to pass --insecure-http automatically")
	}

	// 4c. Reject unsafe query param injection attempts.
	for _, bad := range []string{
		"acme_email=bad;rm+-rf+/",
		"panel_ca_sha256=nothex",
		"dns_gate=invalid",
	} {
		req = httptest.NewRequest("GET", "https://panel.example.com/api/v1/edge/setup.sh?token="+tok+"&"+bad, nil)
		resp, _ = app.Test(req, 5000)
		if resp.StatusCode != 400 {
			t.Fatalf("expected 400 for invalid query %q, got %d", bad, resp.StatusCode)
		}
	}

	// 5. ?legacy=1 setup.sh fallback emits legacy Caddy catch-all + Deprecation header & comment.
	req = httptest.NewRequest("GET", "https://panel.example.com/api/v1/edge/setup.sh?token="+tok+"&legacy=1", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	resp, err = app.Test(req, 5000)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("legacy setup.sh failed: err=%v status=%d", err, resp.StatusCode)
	}
	if resp.Header.Get("Deprecation") != "true" {
		t.Fatalf("expected Deprecation: true on ?legacy=1 setup.sh")
	}
	legacySetup := readRespBody(t, resp.Body)
	for _, mustHave := range []string{"# DEPRECATED:", "on_demand_tls", "NextDeploy Edge Shield Catch-all"} {
		if !strings.Contains(legacySetup, mustHave) {
			t.Fatalf("legacy setup.sh missing %q", mustHave)
		}
	}

	// 6. Default uninstall.sh wraps `nd edge remove-panel <panel_id>`, supports ?purge_certs=1 and ?all=1.
	req = httptest.NewRequest("GET", "https://panel.example.com/api/v1/edge/uninstall.sh?token="+tok, nil)
	resp, err = app.Test(req, 5000)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("default uninstall.sh failed: err=%v status=%d", err, resp.StatusCode)
	}
	if resp.Header.Get("Deprecation") != "" {
		t.Fatalf("default uninstall.sh should not set Deprecation header")
	}
	uninstallScript := readRespBody(t, resp.Body)
	if !strings.Contains(uninstallScript, `"$ND_BIN" edge remove-panel '`+panelID+`'`) {
		t.Fatalf("default uninstall.sh missing nd edge remove-panel:\n%s", uninstallScript)
	}
	if strings.Contains(uninstallScript, tok) {
		t.Fatalf("uninstall.sh must not expose raw token in script")
	}

	req = httptest.NewRequest("GET", "https://panel.example.com/api/v1/edge/uninstall.sh?token="+tok+"&purge_certs=1", nil)
	resp, _ = app.Test(req, 5000)
	if body := readRespBody(t, resp.Body); !strings.Contains(body, `"$ND_BIN" edge remove-panel '`+panelID+`' --purge-certs`) {
		t.Fatalf("expected --purge-certs on uninstall.sh, got:\n%s", body)
	}

	req = httptest.NewRequest("GET", "https://panel.example.com/api/v1/edge/uninstall.sh?token="+tok+"&all=1", nil)
	resp, _ = app.Test(req, 5000)
	if body := readRespBody(t, resp.Body); !strings.Contains(body, `"$ND_BIN" edge uninstall --all`) {
		t.Fatalf("expected nd edge uninstall --all on ?all=1, got:\n%s", body)
	}

	// 7. ?legacy=1 uninstall.sh fallback emits legacy Caddy uninstaller + Deprecation header & comment.
	req = httptest.NewRequest("GET", "https://panel.example.com/api/v1/edge/uninstall.sh?token="+tok+"&legacy=1", nil)
	resp, err = app.Test(req, 5000)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("legacy uninstall.sh failed: err=%v status=%d", err, resp.StatusCode)
	}
	if resp.Header.Get("Deprecation") != "true" {
		t.Fatalf("expected Deprecation: true on ?legacy=1 uninstall.sh")
	}
	legacyUninstall := readRespBody(t, resp.Body)
	for _, mustHave := range []string{"# DEPRECATED:", "Caddyfile.backup.*", "action=uninstall"} {
		if !strings.Contains(legacyUninstall, mustHave) {
			t.Fatalf("legacy uninstall.sh missing %q", mustHave)
		}
	}
}
