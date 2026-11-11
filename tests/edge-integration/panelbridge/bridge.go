package panelbridge

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
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"panel/internal/db"
	"panel/internal/handlers"

	"github.com/gofiber/fiber/v2"
)

// DomainStatus mirrors db.ShieldDomainStatus for cross-module simulation tests.
type DomainStatus struct {
	Host   string
	State  string
	Detail string
}

// ShieldNodeInfo mirrors db.ShieldNode fields verified in simulation tests.
type ShieldNodeInfo struct {
	ID              int64
	Status          string
	LastAppliedETag string
	EdgeID          string
}

// SimPanel wraps a real handlers.Panel + Fiber app backed by an isolated SQLite DB
// and an httptest.NewTLSServer for end-to-end multi-panel Shield simulation.
type SimPanel struct {
	Name       string
	Store      *db.Store
	Panel      *handlers.Panel
	App        *fiber.App
	Server     *httptest.Server
	CASHA256   string
	NodeID     int64
	Token      string
	AdminID    int64
	SessionTok string
	AppID      string

	mu             sync.Mutex
	offline        bool
	originIP       string
	originCAPEM    string
	originCAStatus string
}

// NewSimPanel creates an isolated SQLite-backed Panel with real /api/v1/edge/* and
// domain edit handlers exposed over TLS.
func NewSimPanel(t *testing.T, name, shieldIP, originIP, originCAPEM string) *SimPanel {
	t.Helper()
	ctx := context.Background()

	store, err := db.Open(filepath.Join(t.TempDir(), name+".db"))
	if err != nil {
		t.Fatalf("open sqlite db for %s: %v", name, err)
	}
	t.Cleanup(func() { _ = store.Close() })

	adminID, err := store.CreateUser(ctx, "admin-"+name, "hash", db.RoleAdmin)
	if err != nil {
		t.Fatalf("CreateUser(%s): %v", name, err)
	}
	sessTok := "sess_" + name
	if err := store.CreateSession(ctx, sessTok, adminID, time.Now().Add(24*time.Hour)); err != nil {
		t.Fatalf("CreateSession(%s): %v", name, err)
	}
	appID := "app-" + name
	if err := store.CreateApp(ctx, appID, appID, adminID); err != nil {
		t.Fatalf("CreateApp(%s): %v", name, err)
	}

	tok := db.GenerateShieldToken()
	nodeID, err := store.CreateShieldNode(ctx, &db.ShieldNode{
		Name:   "shield-" + name,
		IP:     shieldIP,
		Token:  tok,
		Status: "connected",
	})
	if err != nil {
		t.Fatalf("CreateShieldNode(%s): %v", name, err)
	}
	if err := store.SetSetting(ctx, "panel_domain", name+".ctrl.demo.net"); err != nil {
		t.Fatalf("SetSetting panel_domain(%s): %v", name, err)
	}

	p := &handlers.Panel{
		DB: store,
		// Keep Store nil so AppDomainEdit in E-ORIGIN-6 tests doesn't invoke Docker.
	}

	app := fiber.New(fiber.Config{BodyLimit: 10 << 20})
	app.Post("/api/v1/edge/register", p.EdgeRegister)
	app.Get("/api/v1/edge/routes", p.EdgeRoutes)
	app.Post("/api/v1/edge/status", p.EdgeStatus)
	app.Post("/api/v1/edge/unregister", p.EdgeUnregister)
	app.Post("/apps/:id/domains/:did/edit", p.RequireAdminMiddleware, p.AppDomainEdit)

	sp := &SimPanel{
		Name:           name,
		Store:          store,
		Panel:          p,
		App:            app,
		NodeID:         nodeID,
		Token:          tok,
		AdminID:        adminID,
		SessionTok:     sessTok,
		AppID:          appID,
		originIP:       originIP,
		originCAPEM:    originCAPEM,
		originCAStatus: "ok",
	}

	srv := httptest.NewTLSServer(http.HandlerFunc(sp.serveHTTP))
	t.Cleanup(srv.Close)
	sp.Server = srv

	leafSum := sha256.Sum256(srv.Certificate().Raw)
	sp.CASHA256 = hex.EncodeToString(leafSum[:])

	return sp
}

func (sp *SimPanel) SetOffline(off bool) {
	sp.mu.Lock()
	sp.offline = off
	sp.mu.Unlock()
}

func (sp *SimPanel) SetOriginCA(pemData string) {
	sp.mu.Lock()
	sp.originCAPEM = pemData
	if pemData == "" {
		sp.originCAStatus = "missing"
	} else {
		sp.originCAStatus = "ok"
	}
	sp.mu.Unlock()
}

func (sp *SimPanel) CreateAppDomain(ctx context.Context, domain string, shielded, enableWWW bool) (int64, error) {
	var shieldPtr *int64
	if shielded {
		id := sp.NodeID
		shieldPtr = &id
	}
	return sp.Store.CreateAppDomain(ctx, db.AppDomain{
		AppID:        sp.AppID,
		Domain:       domain,
		Service:      "web",
		Port:         80,
		EnableHTTPS:  true,
		EnableWWW:    enableWWW,
		ShieldNodeID: shieldPtr,
	})
}

func (sp *SimPanel) DeleteAppDomain(ctx context.Context, domainID int64) error {
	return sp.Store.DeleteAppDomain(ctx, domainID)
}

func (sp *SimPanel) SaveShieldedProxyRoute(ctx context.Context, domain, target string, enabled bool) (int64, error) {
	id := sp.NodeID
	return sp.Store.SaveProxyRoute(ctx, db.ProxyRoute{
		Kind:         "proxy",
		Domain:       domain,
		Target:       target,
		Enabled:      enabled,
		ShieldNodeID: &id,
	})
}

func (sp *SimPanel) EditDomainAttachShield(domainID int64, domain string) (int, error) {
	form := fmt.Sprintf("domain=%s&service=web&port=80&enable_https=on&shield_node_id=%d", domain, sp.NodeID)
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/apps/%s/domains/%d/edit?json=1", sp.AppID, domainID), strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Cookie", "nd_session="+sp.SessionTok)
	resp, err := sp.App.Test(req, 5000)
	if err != nil {
		return 0, err
	}
	_ = resp.Body.Close()
	return resp.StatusCode, nil
}

func (sp *SimPanel) ClearPendingOriginApply(ctx context.Context) error {
	return sp.Store.ClearAppPendingOriginApply(ctx, sp.AppID)
}

func (sp *SimPanel) ListDomainStatuses(ctx context.Context) ([]DomainStatus, error) {
	rows, err := sp.Store.ListShieldDomainStatuses(ctx, sp.NodeID)
	if err != nil {
		return nil, err
	}
	out := make([]DomainStatus, 0, len(rows))
	for _, r := range rows {
		out = append(out, DomainStatus{
			Host:   r.Host,
			State:  r.State,
			Detail: r.Detail,
		})
	}
	return out, nil
}

func (sp *SimPanel) GetShieldNode(ctx context.Context) (*ShieldNodeInfo, error) {
	n, err := sp.Store.GetShieldNode(ctx, sp.NodeID)
	if err != nil || n == nil {
		return nil, err
	}
	return &ShieldNodeInfo{
		ID:              n.ID,
		Status:          n.Status,
		LastAppliedETag: n.LastAppliedETag,
		EdgeID:          n.EdgeID,
	}, nil
}

func (sp *SimPanel) serveHTTP(w http.ResponseWriter, r *http.Request) {
	sp.mu.Lock()
	offline := sp.offline
	originIP := sp.originIP
	caPEM := sp.originCAPEM
	caStatus := sp.originCAStatus
	sp.mu.Unlock()

	if offline {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":"panel_offline"}`))
		return
	}

	bodyBytes, _ := io.ReadAll(r.Body)
	req := httptest.NewRequest(r.Method, r.URL.RequestURI(), bytes.NewReader(bodyBytes))
	for k, vals := range r.Header {
		for _, v := range vals {
			req.Header.Add(k, v)
		}
	}

	ifNoneMatch := r.Header.Get("If-None-Match")
	if r.Method == http.MethodGet && r.URL.Path == "/api/v1/edge/routes" {
		req.Header.Del("If-None-Match")
	}

	resp, err := sp.App.Test(req, 15_000)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(err.Error()))
		return
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	if r.Method == http.MethodGet && r.URL.Path == "/api/v1/edge/routes" && resp.StatusCode == http.StatusOK {
		var er handlers.EdgeRoutesResponse
		if jErr := json.Unmarshal(respBody, &er); jErr == nil {
			er.Origin.IPs = []string{originIP}
			er.Origin.Port = 443
			er.Origin.CAPEM = caPEM
			er.Origin.CAStatus = caStatus
			if caPEM != "" {
				sum := sha256.Sum256([]byte(caPEM))
				er.Origin.CASHA256 = hex.EncodeToString(sum[:])
			} else {
				er.Origin.CASHA256 = ""
			}
			if etag, eErr := handlers.ComputeEdgeRoutesETag(er); eErr == nil {
				er.ETag = etag
			}

			for k, vals := range resp.Header {
				if strings.EqualFold(k, "Content-Length") {
					continue
				}
				for _, v := range vals {
					w.Header().Add(k, v)
				}
			}
			w.Header().Set("ETag", `"`+er.ETag+`"`)
			if ifNoneMatchMatchesETag(ifNoneMatch, er.ETag) {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			outBytes, _ := json.Marshal(er)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(outBytes)
			return
		}
	}

	for k, vals := range resp.Header {
		for _, v := range vals {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(respBody)
}

func ifNoneMatchMatchesETag(hdr, etag string) bool {
	if strings.TrimSpace(hdr) == "" || etag == "" {
		return false
	}
	for _, part := range strings.Split(hdr, ",") {
		p := strings.TrimSpace(part)
		if p == "*" {
			return true
		}
		p = strings.TrimPrefix(p, "W/")
		p = strings.Trim(p, `"`)
		if p == etag {
			return true
		}
	}
	return false
}
