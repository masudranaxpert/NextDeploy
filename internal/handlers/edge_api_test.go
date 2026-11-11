package handlers

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"panel/internal/db"
	"panel/internal/dockerx"
	"panel/internal/workspace"

	"github.com/gofiber/fiber/v2"
)

func newTestEdgeApp(p *Panel) *fiber.App {
	app := fiber.New(fiber.Config{
		BodyLimit: 10 << 20,
	})
	app.Post("/api/v1/edge/register", p.EdgeRegister)
	app.Get("/api/v1/edge/routes", p.EdgeRoutes)
	app.Post("/api/v1/edge/status", p.EdgeStatus)
	app.Post("/api/v1/edge/unregister", p.EdgeUnregister)
	app.Get("/api/v1/shield-nodes", p.APIAuthMiddleware, p.ListShieldNodes)
	app.Delete("/api/v1/shield-nodes/:id", p.RequireAdminMiddleware, p.DeleteShieldNode)
	app.Post("/api/v1/shield-nodes/:id/rotate-token", p.RequireAdminMiddleware, p.RotateShieldNodeToken)
	app.Get("/caddy/routes", p.RequireAdminMiddleware, p.CaddyRoutesList)
	return app
}

func TestEdgeRegisterRoutesStatusUnregister(t *testing.T) {
	setupTempRootCompose(t)
	wsRoot := t.TempDir()
	store, err := db.Open(filepath.Join(t.TempDir(), "edge_api.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	caPEM := "-----BEGIN CERTIFICATE-----\nMIIBTEST\n-----END CERTIFICATE-----\n"
	caSum := sha256.Sum256([]byte(caPEM))
	caSHA := hex.EncodeToString(caSum[:])
	caStatus := "ok"

	p := &Panel{
		DB:             store,
		Store:          workspace.NewStore(wsRoot),
		WorkspacesRoot: wsRoot,
		applyRootStackComposeFn: func(composeFile string, services ...string) {},
		composeUpNoBuildFn: func(ctx context.Context, dir string, paths []string, project string, logW io.Writer, envFiles []string) dockerx.Result {
			return dockerx.Result{OK: true}
		},
		reconcileProxyRoutesFn: func(ctx context.Context) {},
		originIPFn: func(ctx context.Context) string {
			return "204.136.10.51"
		},
		originCAFn: func(ctx context.Context) (string, string, string) {
			if caStatus == "missing" {
				return "", "", "missing"
			}
			return caPEM, caSHA, "ok"
		},
	}
	app := newTestEdgeApp(p)
	ctx := context.Background()

	adminID, _ := store.CreateUser(ctx, "admin", "hash", db.RoleAdmin)
	sessTok := "edge_api_admin_sess"
	_ = store.CreateSession(ctx, sessTok, adminID, time.Now().Add(time.Hour))

	n1, _ := store.CreateShieldNode(ctx, &db.ShieldNode{Name: "shield-1", IP: "107.189.6.222", Token: "shld_tok_1"})
	n2, _ := store.CreateShieldNode(ctx, &db.ShieldNode{Name: "shield-2", IP: "107.189.6.223", Token: "shld_tok_2"})

	// 1. Query-string token must NOT authenticate on new edge endpoints
	req := httptest.NewRequest("GET", "/api/v1/edge/routes?token=shld_tok_1", nil)
	resp, err := app.Test(req)
	if err != nil || resp.StatusCode != 401 {
		t.Fatalf("expected 401 when Bearer header missing, got status=%d err=%v", resp.StatusCode, err)
	}
	if resp.Header.Get("X-ND-Edge-API") != "1" {
		t.Fatalf("expected X-ND-Edge-API: 1 header even on 401")
	}

	// 2. POST /api/v1/edge/register
	regBody, _ := json.Marshal(EdgeRegisterRequest{
		EdgeID:       "edge-uuid-1",
		EdgeVersion:  "1.2.0",
		CaddyVersion: "v2.8.4",
		Hostname:     "shield-sg",
		PublicIPv4:   []string{"107.189.6.222"},
	})
	req = httptest.NewRequest("POST", "/api/v1/edge/register", bytes.NewReader(regBody))
	req.Header.Set("Authorization", "Bearer shld_tok_1")
	req.Header.Set("Content-Type", "application/json")
	resp, err = app.Test(req)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("register failed: status=%d err=%v", resp.StatusCode, err)
	}
	if resp.Header.Get("X-ND-Edge-API") != "1" {
		t.Fatalf("expected X-ND-Edge-API: 1 on register")
	}
	var regResp EdgeRegisterResponse
	_ = json.NewDecoder(resp.Body).Decode(&regResp)
	if regResp.ShieldNodeID != n1 || regResp.APIVersion != 1 || regResp.PollIntervalSec != 30 || regResp.PanelID == "" {
		t.Fatalf("unexpected register response: %+v", regResp)
	}

	// 2b. Idempotent re-register with same edge_id -> 200
	req = httptest.NewRequest("POST", "/api/v1/edge/register", bytes.NewReader(regBody))
	req.Header.Set("Authorization", "Bearer shld_tok_1")
	req.Header.Set("Content-Type", "application/json")
	resp, _ = app.Test(req)
	if resp.StatusCode != 200 {
		t.Fatalf("expected idempotent register 200, got %d", resp.StatusCode)
	}

	// 2c. Different edge_id within 10 min -> 409 token_in_use
	regOther, _ := json.Marshal(EdgeRegisterRequest{EdgeID: "edge-uuid-2", EdgeVersion: "1.2.0", CaddyVersion: "v2.8.4"})
	req = httptest.NewRequest("POST", "/api/v1/edge/register", bytes.NewReader(regOther))
	req.Header.Set("Authorization", "Bearer shld_tok_1")
	req.Header.Set("Content-Type", "application/json")
	resp, _ = app.Test(req)
	if resp.StatusCode != 409 {
		t.Fatalf("expected 409 token_in_use, got %d", resp.StatusCode)
	}

	// 2d. Force: true overrides binding -> 200
	regForce, _ := json.Marshal(EdgeRegisterRequest{EdgeID: "edge-uuid-2", EdgeVersion: "1.2.1", CaddyVersion: "v2.8.4", Force: true})
	req = httptest.NewRequest("POST", "/api/v1/edge/register", bytes.NewReader(regForce))
	req.Header.Set("Authorization", "Bearer shld_tok_1")
	req.Header.Set("Content-Type", "application/json")
	resp, _ = app.Test(req)
	if resp.StatusCode != 200 {
		t.Fatalf("expected force register 200, got %d", resp.StatusCode)
	}

	// 3. Seed apps, domains (direct, n1, n2, pending_origin_apply, punycode, duplicate), php site, proxy routes, preview, panel domain
	_ = store.CreateApp(ctx, "app-1", "app-1", adminID)
	_ = store.UpdateAppSlots(ctx, "app-1", "blue", "green")
	_ = store.UpdateAppPinnedSlot(ctx, "app-1", "blue")
	_ = store.SetSetting(ctx, previewSettingKey("app-1"), `{"provider":"custom","base":"pv.example.com","service":"web","port":3000}`)
	// Set admin's default shield to n1 so preview resolves to n1
	_ = store.UpdateUserShield(ctx, adminID, &n1)

	_, _ = store.CreateAppDomain(ctx, db.AppDomain{AppID: "app-1", Domain: "direct.example.com", Service: "web", Port: 80})
	_, _ = store.CreateAppDomain(ctx, db.AppDomain{AppID: "app-1", Domain: "App.Example.com.", Service: "web", Port: 80, EnableWWW: true, ShieldNodeID: &n1})
	_, _ = store.CreateAppDomain(ctx, db.AppDomain{AppID: "app-1", Domain: "münchen.de", Service: "web", Port: 80, ShieldNodeID: &n1})
	// Duplicate host across rows: must be deduped keeping first
	_, _ = store.CreateAppDomain(ctx, db.AppDomain{AppID: "app-1", Domain: "app.example.com", Service: "web", Port: 80, ShieldNodeID: &n1})
	// Pending origin apply: must be omitted until cleared
	_, _ = store.CreateAppDomain(ctx, db.AppDomain{AppID: "app-1", Domain: "pending.example.com", Service: "web", Port: 80, ShieldNodeID: &n1, PendingOriginApply: true})
	// Assigned to node 2: must NOT appear in node 1's routes
	_, _ = store.CreateAppDomain(ctx, db.AppDomain{AppID: "app-1", Domain: "node2.example.com", Service: "web", Port: 80, ShieldNodeID: &n2})

	// Proxy routes: one enabled on n1, one disabled on n1
	_, _ = store.SaveProxyRoute(ctx, db.ProxyRoute{Kind: "proxy", Domain: "api.example.com", Target: "http://127.0.0.1:9000", Enabled: true, ShieldNodeID: &n1})
	_, _ = store.SaveProxyRoute(ctx, db.ProxyRoute{Kind: "proxy", Domain: "off.example.com", Target: "http://127.0.0.1:9001", Enabled: false, ShieldNodeID: &n1})

	// Explicitly shielded panel domain
	_ = store.SetSetting(ctx, settingPanelDomain, "panel.example.com")
	_ = store.SetSetting(ctx, settingPanelShieldNodeID, fmt.Sprintf("%d", n1))

	req = httptest.NewRequest("GET", "/api/v1/edge/routes", nil)
	req.Header.Set("Authorization", "Bearer shld_tok_1")
	resp, err = app.Test(req)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("GET /routes failed: status=%d err=%v", resp.StatusCode, err)
	}
	var routesResp EdgeRoutesResponse
	_ = json.NewDecoder(resp.Body).Decode(&routesResp)
	if routesResp.APIVersion != 1 || routesResp.ETag == "" || routesResp.Origin.CAStatus != "ok" || routesResp.Origin.CASHA256 != caSHA {
		t.Fatalf("unexpected routes metadata: %+v", routesResp)
	}

	gotHosts := map[string]bool{}
	for _, r := range routesResp.Routes {
		for _, h := range r.Hosts {
			if gotHosts[h] {
				t.Fatalf("duplicate host %q found in routes response", h)
			}
			gotHosts[h] = true
		}
	}
	for _, want := range []string{"app.example.com", "www.app.example.com", "xn--mnchen-3ya.de", "api.example.com", "green.pv.example.com", "panel.example.com"} {
		if !gotHosts[want] {
			t.Fatalf("expected host %q in routes, got %v", want, gotHosts)
		}
	}
	for _, forbid := range []string{"direct.example.com", "pending.example.com", "node2.example.com", "off.example.com"} {
		if gotHosts[forbid] {
			t.Fatalf("forbidden host %q appeared in node 1 routes", forbid)
		}
	}

	// 3b. ETag stability & 304 Not Modified
	req = httptest.NewRequest("GET", "/api/v1/edge/routes", nil)
	req.Header.Set("Authorization", "Bearer shld_tok_1")
	req.Header.Set("If-None-Match", `"`+routesResp.ETag+`"`)
	resp, _ = app.Test(req)
	if resp.StatusCode != 304 {
		t.Fatalf("expected 304 Not Modified for matching ETag, got %d", resp.StatusCode)
	}

	// 3c. Clear pending_origin_apply -> pending.example.com now appears and ETag changes
	_ = store.ClearAppPendingOriginApply(ctx, "app-1")
	req = httptest.NewRequest("GET", "/api/v1/edge/routes", nil)
	req.Header.Set("Authorization", "Bearer shld_tok_1")
	req.Header.Set("If-None-Match", `"`+routesResp.ETag+`"`)
	resp, _ = app.Test(req)
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200 after ETag change, got %d", resp.StatusCode)
	}

	// 3d. Missing CA status
	caStatus = "missing"
	req = httptest.NewRequest("GET", "/api/v1/edge/routes", nil)
	req.Header.Set("Authorization", "Bearer shld_tok_1")
	resp, _ = app.Test(req)
	var missingCAResp EdgeRoutesResponse
	_ = json.NewDecoder(resp.Body).Decode(&missingCAResp)
	if missingCAResp.Origin.CAStatus != "missing" || missingCAResp.Origin.CAPEM != "" {
		t.Fatalf("expected ca_status=missing, got %+v", missingCAResp.Origin)
	}
	caStatus = "ok"

	// 4. POST /api/v1/edge/status + body limit + 256 KB cap + cross-node isolation
	statusPayload, _ := json.Marshal(EdgeStatusRequest{
		EdgeID:       "edge-uuid-2",
		EdgeVersion:  "1.2.1",
		CaddyVersion: "v2.8.4",
		CaddyRunning: true,
		AppliedETag:  routesResp.ETag,
		Hosts: []EdgeHostStatus{
			{Host: "app.example.com", State: "active", CertNotAfter: "2027-01-01T00:00:00Z", Issuer: "Let's Encrypt"},
			{Host: "api.example.com", State: "issuing"},
		},
		Conflicts: []EdgeConflictStatus{
			{Host: "conflict.example.com", OwnerPanel: "other-panel"},
		},
	})
	req = httptest.NewRequest("POST", "/api/v1/edge/status", bytes.NewReader(statusPayload))
	req.Header.Set("Authorization", "Bearer shld_tok_1")
	req.Header.Set("Content-Type", "application/json")
	resp, _ = app.Test(req)
	if resp.StatusCode != 200 {
		t.Fatalf("POST /status failed: %d", resp.StatusCode)
	}

	// Verify node 2 status untouched
	s2, _ := store.ListShieldDomainStatuses(ctx, n2)
	if len(s2) != 0 {
		t.Fatalf("node 2 statuses should remain empty, got %+v", s2)
	}

	// Verify oversized (>2MB) status returns 413
	huge := append([]byte(`{"edge_id":"x","last_apply_error":"`), bytes.Repeat([]byte("a"), (2<<20)+100)...)
	huge = append(huge, []byte(`"}`)...)
	req = httptest.NewRequest("POST", "/api/v1/edge/status", bytes.NewReader(huge))
	req.Header.Set("Authorization", "Bearer shld_tok_1")
	req.Header.Set("Content-Type", "application/json")
	resp, _ = app.Test(req, 5000)
	if resp.StatusCode != 413 {
		t.Fatalf("expected 413 for >2MB /status body, got %d", resp.StatusCode)
	}

	// Verify >256KB but <2MB payload caps stored edge_status_json <= 256KB
	var bigHosts []EdgeHostStatus
	for i := 0; i < 4000; i++ {
		bigHosts = append(bigHosts, EdgeHostStatus{
			Host:   fmt.Sprintf("host-%04d.example.com", i),
			State:  "active",
			Detail: strings.Repeat("d", 64),
		})
	}
	bigPayload, _ := json.Marshal(EdgeStatusRequest{
		EdgeID:       "edge-uuid-2",
		EdgeVersion:  "1.2.1",
		CaddyVersion: "v2.8.4",
		Hosts:        bigHosts,
	})
	if len(bigPayload) <= edgeStatusMaxStoreBytes || len(bigPayload) >= edgeStatusMaxBodyBytes {
		t.Fatalf("test fixture size out of expected range: %d", len(bigPayload))
	}
	req = httptest.NewRequest("POST", "/api/v1/edge/status", bytes.NewReader(bigPayload))
	req.Header.Set("Authorization", "Bearer shld_tok_1")
	req.Header.Set("Content-Type", "application/json")
	resp, _ = app.Test(req, 5000)
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200 for 300KB /status, got %d", resp.StatusCode)
	}
	n1Row, _ := store.GetShieldNode(ctx, n1)
	if len(n1Row.EdgeStatusJSON) > edgeStatusMaxStoreBytes {
		t.Fatalf("expected stored edge_status_json <= 256KB, got %d", len(n1Row.EdgeStatusJSON))
	}

	// 5. Token rotation endpoint + grace period on /routes
	req = httptest.NewRequest("POST", fmt.Sprintf("/api/v1/shield-nodes/%d/rotate-token", n1), nil)
	req.Header.Set("Cookie", sessionCookie+"="+sessTok)
	resp, _ = app.Test(req)
	if resp.StatusCode != 200 {
		t.Fatalf("rotate-token failed: %d", resp.StatusCode)
	}
	var rotBody struct {
		OK    bool   `json:"ok"`
		Token string `json:"token"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&rotBody)
	if !rotBody.OK || rotBody.Token == "" || rotBody.Token == "shld_tok_1" {
		t.Fatalf("unexpected rotate response: %+v", rotBody)
	}

	// Old token still works during 10m grace
	req = httptest.NewRequest("GET", "/api/v1/edge/routes", nil)
	req.Header.Set("Authorization", "Bearer shld_tok_1")
	resp, _ = app.Test(req)
	if resp.StatusCode != 200 {
		t.Fatalf("expected old token to work during grace period, got %d", resp.StatusCode)
	}

	// 6. POST /api/v1/edge/unregister
	unregBody, _ := json.Marshal(EdgeUnregisterRequest{EdgeID: "edge-uuid-2", Reason: "uninstall"})
	req = httptest.NewRequest("POST", "/api/v1/edge/unregister", bytes.NewReader(unregBody))
	req.Header.Set("Authorization", "Bearer "+rotBody.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, _ = app.Test(req)
	if resp.StatusCode != 200 {
		t.Fatalf("unregister failed: %d", resp.StatusCode)
	}
	n1After, _ := store.GetShieldNode(ctx, n1)
	if n1After.Status != "unregistered" {
		t.Fatalf("expected status=unregistered, got %q", n1After.Status)
	}

	// 7. Delete shield node -> /routes returns 410 Gone {"reason":"node_deleted"}
	req = httptest.NewRequest("DELETE", fmt.Sprintf("/api/v1/shield-nodes/%d", n1), nil)
	req.Header.Set("Cookie", sessionCookie+"="+sessTok)
	resp, _ = app.Test(req, 5000)
	if resp.StatusCode != 200 {
		t.Fatalf("delete node failed: %d", resp.StatusCode)
	}

	req = httptest.NewRequest("GET", "/api/v1/edge/routes", nil)
	req.Header.Set("Authorization", "Bearer "+rotBody.Token)
	resp, _ = app.Test(req)
	if resp.StatusCode != 410 {
		t.Fatalf("expected 410 Gone after node deleted, got %d", resp.StatusCode)
	}
	raw410, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(raw410), "node_deleted") {
		t.Fatalf("expected node_deleted reason in 410 body, got %s", string(raw410))
	}
}

func TestEdgeRateLimit(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "edge_ratelimit.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	p := &Panel{
		DB:         store,
		originIPFn: func(ctx context.Context) string { return "1.2.3.4" },
		originCAFn: func(ctx context.Context) (string, string, string) { return "", "", "missing" },
	}
	app := newTestEdgeApp(p)
	ctx := context.Background()
	_, _ = store.CreateShieldNode(ctx, &db.ShieldNode{Name: "rl", IP: "10.0.0.1", Token: "shld_rl_tok"})

	for i := 0; i < edgeRateLimitPerMinute; i++ {
		req := httptest.NewRequest("GET", "/api/v1/edge/routes", nil)
		req.Header.Set("Authorization", "Bearer shld_rl_tok")
		resp, err := app.Test(req, 5000)
		if err != nil || resp == nil || resp.StatusCode != 200 {
			t.Fatalf("request %d failed: resp=%v err=%v", i, resp, err)
		}
	}
	req := httptest.NewRequest("GET", "/api/v1/edge/routes", nil)
	req.Header.Set("Authorization", "Bearer shld_rl_tok")
	resp, err := app.Test(req, 5000)
	if err != nil || resp == nil || resp.StatusCode != 429 {
		t.Fatalf("expected 429 after %d requests/min, got resp=%v err=%v", edgeRateLimitPerMinute, resp, err)
	}
}

func TestCheckCloudflareFlexibleLoop(t *testing.T) {
	// Redirect loop server: / -> / (302)
	var loopSrv *httptest.Server
	loopSrv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, loopSrv.URL+"/", http.StatusFound)
	}))
	defer loopSrv.Close()

	loop, hint := checkRedirectLoopURL(context.Background(), loopSrv.URL+"/")
	if !loop || !strings.Contains(hint, "Full") {
		t.Fatalf("expected CF flexible loop detected with Full/Full(strict) hint, got loop=%v hint=%q", loop, hint)
	}

	// Normal 200 server: no loop
	okSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer okSrv.Close()

	loop, hint = checkRedirectLoopURL(context.Background(), okSrv.URL+"/")
	if loop || hint != "" {
		t.Fatalf("expected no loop for 200 response, got loop=%v hint=%q", loop, hint)
	}

	// SSRF guard: reject IP literals, ports, localhost, and internal suffixes
	for _, unsafe := range []string{"127.0.0.1", "169.254.169.254", "localhost", "svc.internal", "app.local", "example.com:8080", "http://example.com/path", "10.0.0.1"} {
		if _, ok := isSafeCFCheckHost(unsafe); ok {
			t.Fatalf("expected isSafeCFCheckHost(%q) == false", unsafe)
		}
		if l, h := CheckCloudflareFlexibleLoop(context.Background(), unsafe); l || h != "" {
			t.Fatalf("expected unsafe host %q to be skipped", unsafe)
		}
	}
	if got, ok := isSafeCFCheckHost("App.Example.COM"); !ok || got != "app.example.com" {
		t.Fatalf("expected valid public host normalized, got %q ok=%v", got, ok)
	}
}

func TestEdgeContractFixture(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "edge_contract.json"))
	if err != nil {
		t.Fatalf("read contract fixture: %v", err)
	}
	var fixture struct {
		RegisterRequest   EdgeRegisterRequest   `json:"register_request"`
		RegisterResponse  EdgeRegisterResponse  `json:"register_response"`
		RoutesResponse    EdgeRoutesResponse    `json:"routes_response"`
		StatusRequest     EdgeStatusRequest     `json:"status_request"`
		UnregisterRequest EdgeUnregisterRequest `json:"unregister_request"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatalf("unmarshal contract fixture: %v", err)
	}
	if fixture.RegisterRequest.EdgeID == "" || fixture.RegisterResponse.APIVersion != 1 {
		t.Fatalf("invalid register contract: %+v %+v", fixture.RegisterRequest, fixture.RegisterResponse)
	}
	if len(fixture.RoutesResponse.Routes) != 3 || fixture.RoutesResponse.Origin.CAStatus != "ok" {
		t.Fatalf("invalid routes contract: %+v", fixture.RoutesResponse)
	}
	etag, err := ComputeEdgeRoutesETag(fixture.RoutesResponse)
	if err != nil || etag == "" {
		t.Fatalf("ComputeEdgeRoutesETag failed: %v", err)
	}
	if fixture.RoutesResponse.ETag != etag {
		t.Fatalf("contract fixture routes_response.etag mismatch: fixture=%q computed=%q", fixture.RoutesResponse.ETag, etag)
	}
	if len(fixture.StatusRequest.Hosts) != 1 || len(fixture.StatusRequest.Conflicts) != 1 {
		t.Fatalf("invalid status contract: %+v", fixture.StatusRequest)
	}
	if fixture.UnregisterRequest.EdgeID == "" {
		t.Fatalf("invalid unregister contract: %+v", fixture.UnregisterRequest)
	}
}

func TestShieldAndDomainStatusExposureForUI(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "edge_ui_exposure.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	p := &Panel{DB: store, originIPFn: func(ctx context.Context) string { return "204.136.10.51" }}
	app := newTestEdgeApp(p)
	ctx := context.Background()

	adminID, _ := store.CreateUser(ctx, "admin", "hash", db.RoleAdmin)
	admTok := "adm_ui_tok"
	_ = store.CreateSession(ctx, admTok, adminID, time.Now().Add(time.Hour))

	userID, _ := store.CreateUser(ctx, "user1", "hash", db.RoleUser)
	usrTok := "usr_ui_tok"
	_ = store.CreateSession(ctx, usrTok, userID, time.Now().Add(time.Hour))

	n1, _ := store.CreateShieldNode(ctx, &db.ShieldNode{Name: "s1", IP: "107.189.6.222", Token: "shld_ui_1"})
	_ = store.UpdateUserShield(ctx, userID, &n1)
	_ = store.RegisterShieldNode(ctx, n1, "edge-1", "1.2.0", "v2.8.4")
	exp := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	_ = store.UpdateShieldNodeEdgeStatus(ctx, n1, "1.2.0", "v2.8.4", "etag_x", "", `{"ok":true}`, []db.ShieldDomainStatus{
		{Host: "route-ui.example.com", State: "active", CertNotAfter: &exp},
		{Host: "xn--mnchen-3ya.de", State: "issuing", Detail: "obtaining cert"},
		{Host: "secret-other.example.com", State: "conflict", Detail: "panel-2"},
	})
	_, _ = store.SaveProxyRoute(ctx, db.ProxyRoute{
		Kind: "proxy", Domain: "route-ui.example.com", Target: "http://127.0.0.1:9000", Enabled: true, ShieldNodeID: &n1,
	})
	_ = store.CreateApp(ctx, "app-idn", "app-idn", adminID)
	_, _ = store.CreateAppDomain(ctx, db.AppDomain{
		AppID: "app-idn", Domain: "münchen.de", Service: "web", Port: 80, ShieldNodeID: &n1,
	})
	idnDoms, _ := store.ListAppDomains(ctx, "app-idn")
	p.attachDomainEdgeStatuses(ctx, idnDoms)
	if len(idnDoms) != 1 || idnDoms[0].EdgeStatus == nil || idnDoms[0].EdgeStatus.State != "issuing" {
		t.Fatalf("expected Unicode domain münchen.de to match punycode shield_domain_status, got %+v", idnDoms)
	}

	// Admin gets full edge fields & domain_statuses
	req := httptest.NewRequest("GET", "/api/v1/shield-nodes", nil)
	req.Header.Set("Cookie", sessionCookie+"="+admTok)
	resp, err := app.Test(req)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("admin list shield nodes failed: status=%d err=%v", resp.StatusCode, err)
	}
	var admList struct {
		OK      bool            `json:"ok"`
		PanelID string          `json:"panel_id"`
		Nodes   []db.ShieldNode `json:"nodes"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&admList)
	if !admList.OK || admList.PanelID == "" || len(admList.Nodes) != 1 {
		t.Fatalf("unexpected admin list: %+v", admList)
	}
	gotNode := admList.Nodes[0]
	if gotNode.EdgeVersion != "1.2.0" || gotNode.CaddyVersion != "v2.8.4" || gotNode.SyncState != "ok" || gotNode.ConflictsCount != 1 || len(gotNode.DomainStatuses) != 3 {
		t.Fatalf("missing edge fields for admin: %+v", gotNode)
	}

	// Proxy routes list exposes edge_status
	req = httptest.NewRequest("GET", "/caddy/routes", nil)
	req.Header.Set("Cookie", sessionCookie+"="+admTok)
	resp, _ = app.Test(req)
	var routesList struct {
		OK     bool            `json:"ok"`
		Routes []db.ProxyRoute `json:"routes"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&routesList)
	if len(routesList.Routes) != 1 || routesList.Routes[0].EdgeStatus == nil || routesList.Routes[0].EdgeStatus.State != "active" {
		t.Fatalf("expected proxy route edge_status=active, got %+v", routesList.Routes)
	}

	// Non-admin user does NOT receive token, edge_status_json, last_error, or all domain_statuses
	req = httptest.NewRequest("GET", "/api/v1/shield-nodes", nil)
	req.Header.Set("Cookie", sessionCookie+"="+usrTok)
	resp, err = app.Test(req)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("non-admin list shield nodes failed: status=%d err=%v", resp.StatusCode, err)
	}
	var usrList struct {
		Nodes []db.ShieldNode `json:"nodes"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&usrList)
	if len(usrList.Nodes) != 1 || usrList.Nodes[0].Token != "" || usrList.Nodes[0].EdgeStatusJSON != "" || usrList.Nodes[0].LastError != "" || len(usrList.Nodes[0].DomainStatuses) != 0 {
		t.Fatalf("non-admin leak or missing node: %+v", usrList.Nodes)
	}
}
