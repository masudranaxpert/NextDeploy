package main

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"flag"
	"io"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type edgeRegisterRequest struct {
	EdgeID       string   `json:"edge_id"`
	EdgeVersion  string   `json:"edge_version"`
	CaddyVersion string   `json:"caddy_version"`
	Hostname     string   `json:"hostname"`
	PublicIPv4   []string `json:"public_ipv4"`
	PublicIPv6   []string `json:"public_ipv6"`
	Force        bool     `json:"force"`
}

type edgeRegisterResponse struct {
	PanelID         string `json:"panel_id"`
	PanelName       string `json:"panel_name"`
	ShieldNodeID    int64  `json:"shield_node_id"`
	PollIntervalSec int    `json:"poll_interval_sec"`
	APIVersion      int    `json:"api_version"`
}

type edgeRoutesResponse struct {
	APIVersion int            `json:"api_version"`
	PanelID    string         `json:"panel_id"`
	ETag       string         `json:"etag"`
	Origin     edgeOriginInfo `json:"origin"`
	Routes     []edgeRoute    `json:"routes"`
	Node       edgeNodeInfo   `json:"node"`
}

type edgeOriginInfo struct {
	IPs      []string `json:"ips"`
	Port     int      `json:"port"`
	CAPEM    string   `json:"ca_pem"`
	CASHA256 string   `json:"ca_sha256"`
	CAStatus string   `json:"ca_status"`
}

type edgeRoute struct {
	ID      string           `json:"id"`
	Kind    string           `json:"kind"`
	Hosts   []string         `json:"hosts"`
	Options edgeRouteOptions `json:"options"`
}

type edgeRouteOptions struct {
	Websocket  bool `json:"websocket"`
	MaxBodyMB  int  `json:"max_body_mb"`
	UpstreamH2 bool `json:"upstream_h2"`
}

type edgeNodeInfo struct {
	ID      int64  `json:"id"`
	IP      string `json:"ip"`
	Deleted bool   `json:"deleted"`
}

type edgeHostStatus struct {
	Host         string `json:"host"`
	State        string `json:"state"`
	Detail       string `json:"detail"`
	CertNotAfter string `json:"cert_not_after,omitempty"`
	Issuer       string `json:"issuer,omitempty"`
}

type edgeConflictStatus struct {
	Host       string `json:"host"`
	OwnerPanel string `json:"owner_panel"`
}

type edgeStatusRequest struct {
	EdgeID         string               `json:"edge_id"`
	EdgeVersion    string               `json:"edge_version"`
	CaddyVersion   string               `json:"caddy_version"`
	CaddyRunning   bool                 `json:"caddy_running"`
	AppliedETag    string               `json:"applied_etag"`
	LastApplyAt    string               `json:"last_apply_at"`
	LastApplyError string               `json:"last_apply_error"`
	UptimeSec      int64                `json:"uptime_sec"`
	DiskFreePct    float64              `json:"disk_free_pct"`
	MemFreeMB      int64                `json:"mem_free_mb"`
	ClockSkewSec   int64                `json:"clock_skew_sec"`
	Hosts          []edgeHostStatus     `json:"hosts"`
	Conflicts      []edgeConflictStatus `json:"conflicts"`
}

type faultConfig struct {
	StatusCode int  `json:"status_code"`
	Deleted    bool `json:"deleted"`
}

type panelState struct {
	mu              sync.Mutex
	panelID         string
	panelName       string
	nodeID          int64
	shieldIP        string
	token           string
	pollIntervalSec int

	boundEdgeID     string
	lastHeartbeatAt time.Time
	unregistered    bool

	originIPs    []string
	originPort   int
	originCAPEM  string
	originCAFile string
	originStatus string

	routes      []edgeRoute
	fault       faultConfig
	lastStatus  *edgeStatusRequest
	statusCount int
	routesCount int
}

func envOr(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func envIntOr(key string, def int) int {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func main() {
	listen := flag.String("listen", envOr("LISTEN_ADDR", ":8080"), "HTTP listen address")
	flag.Parse()

	var initRoutes []edgeRoute
	if raw := strings.TrimSpace(os.Getenv("INITIAL_ROUTES_JSON")); raw != "" {
		if err := json.Unmarshal([]byte(raw), &initRoutes); err != nil {
			log.Fatalf("invalid INITIAL_ROUTES_JSON: %v", err)
		}
	}

	originIPsRaw := envOr("ORIGIN_IPS", "172.30.0.21")
	var originIPs []string
	for _, s := range strings.Split(originIPsRaw, ",") {
		if t := strings.TrimSpace(s); t != "" {
			originIPs = append(originIPs, t)
		}
	}

	ps := &panelState{
		panelID:         envOr("PANEL_ID", "11111111-1111-4111-8111-111111111111"),
		panelName:       envOr("PANEL_NAME", "panel-a"),
		nodeID:          int64(envIntOr("SHIELD_NODE_ID", 1)),
		shieldIP:        envOr("SHIELD_IP", "172.30.0.10"),
		token:           envOr("SHIELD_TOKEN", "shld_panel_a_token"),
		pollIntervalSec: envIntOr("POLL_INTERVAL_SEC", 5),
		originIPs:       originIPs,
		originPort:      envIntOr("ORIGIN_PORT", 443),
		originCAFile:    strings.TrimSpace(os.Getenv("ORIGIN_CA_FILE")),
		routes:          initRoutes,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})
	mux.HandleFunc("/api/v1/edge/register", ps.handleRegister)
	mux.HandleFunc("/api/v1/edge/routes", ps.handleRoutes)
	mux.HandleFunc("/api/v1/edge/status", ps.handleStatus)
	mux.HandleFunc("/api/v1/edge/unregister", ps.handleUnregister)

	mux.HandleFunc("/_control/state", ps.handleControlState)
	mux.HandleFunc("/_control/routes", ps.handleControlRoutes)
	mux.HandleFunc("/_control/origin", ps.handleControlOrigin)
	mux.HandleFunc("/_control/fault", ps.handleControlFault)

	srv := &http.Server{
		Addr:              *listen,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("fakepanel %q (%s) listening on %s", ps.panelName, ps.panelID, *listen)
	log.Fatal(srv.ListenAndServe())
}

func (ps *panelState) authOK(r *http.Request) bool {
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(auth) < 7 || !strings.EqualFold(auth[:7], "bearer ") {
		return false
	}
	got := strings.TrimSpace(auth[7:])
	return subtle.ConstantTimeCompare([]byte(got), []byte(ps.token)) == 1
}

func (ps *panelState) resolveOriginLocked() edgeOriginInfo {
	caPEM := ps.originCAPEM
	caStatus := ps.originStatus
	if caPEM == "" && ps.originCAFile != "" {
		if b, err := os.ReadFile(ps.originCAFile); err == nil && strings.Contains(string(b), "-----BEGIN CERTIFICATE-----") {
			caPEM = strings.TrimSpace(string(b)) + "\n"
			caStatus = "ok"
		}
	}
	if caStatus == "" {
		if caPEM != "" {
			caStatus = "ok"
		} else {
			caStatus = "missing"
		}
	}
	caSHA := ""
	if caPEM != "" {
		sum := sha256.Sum256([]byte(caPEM))
		caSHA = hex.EncodeToString(sum[:])
	}
	return edgeOriginInfo{
		IPs:      append([]string(nil), ps.originIPs...),
		Port:     ps.originPort,
		CAPEM:    caPEM,
		CASHA256: caSHA,
		CAStatus: caStatus,
	}
}

func (ps *panelState) buildRoutesLocked() edgeRoutesResponse {
	routes := make([]edgeRoute, 0, len(ps.routes))
	for _, rt := range ps.routes {
		hosts := make([]string, 0, len(rt.Hosts))
		for _, h := range rt.Hosts {
			norm := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(h)), ".")
			if norm != "" {
				hosts = append(hosts, norm)
			}
		}
		sort.Strings(hosts)
		rt.Hosts = hosts
		if rt.Kind == "" {
			rt.Kind = "app_domain"
		}
		routes = append(routes, rt)
	}
	sort.Slice(routes, func(i, j int) bool { return routes[i].ID < routes[j].ID })

	resp := edgeRoutesResponse{
		APIVersion: 1,
		PanelID:    ps.panelID,
		Origin:     ps.resolveOriginLocked(),
		Routes:     routes,
		Node: edgeNodeInfo{
			ID:      ps.nodeID,
			IP:      ps.shieldIP,
			Deleted: false,
		},
	}
	resp.ETag = computeETag(resp)
	return resp
}

func computeETag(resp edgeRoutesResponse) string {
	sortedRoutes := make([]map[string]any, 0, len(resp.Routes))
	for _, r := range resp.Routes {
		hosts := append([]string(nil), r.Hosts...)
		sort.Strings(hosts)
		sortedRoutes = append(sortedRoutes, map[string]any{
			"hosts": hosts,
			"id":    r.ID,
			"kind":  r.Kind,
			"options": map[string]any{
				"max_body_mb": r.Options.MaxBodyMB,
				"upstream_h2": r.Options.UpstreamH2,
				"websocket":   r.Options.Websocket,
			},
		})
	}
	canonical := map[string]any{
		"api_version": resp.APIVersion,
		"node": map[string]any{
			"deleted": resp.Node.Deleted,
			"id":      resp.Node.ID,
			"ip":      resp.Node.IP,
		},
		"origin": map[string]any{
			"ca_pem":    resp.Origin.CAPEM,
			"ca_sha256": resp.Origin.CASHA256,
			"ca_status": resp.Origin.CAStatus,
			"ips":       resp.Origin.IPs,
			"port":      resp.Origin.Port,
		},
		"panel_id": resp.PanelID,
		"routes":   sortedRoutes,
	}
	raw, _ := json.Marshal(canonical)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (ps *panelState) handleRegister(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-ND-Edge-API", "1")
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
		return
	}
	if !ps.authOK(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	var req edgeRegisterRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
		return
	}
	req.EdgeID = strings.TrimSpace(req.EdgeID)
	if req.EdgeID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "edge_id_required"})
		return
	}

	ps.mu.Lock()
	defer ps.mu.Unlock()
	if !ps.unregistered && ps.boundEdgeID != "" && ps.boundEdgeID != req.EdgeID && !req.Force {
		if time.Since(ps.lastHeartbeatAt) < 10*time.Minute {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "token_in_use"})
			return
		}
	}
	ps.boundEdgeID = req.EdgeID
	ps.unregistered = false
	ps.lastHeartbeatAt = time.Now().UTC()

	writeJSON(w, http.StatusOK, edgeRegisterResponse{
		PanelID:         ps.panelID,
		PanelName:       ps.panelName,
		ShieldNodeID:    ps.nodeID,
		PollIntervalSec: ps.pollIntervalSec,
		APIVersion:      1,
	})
}

func (ps *panelState) handleRoutes(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-ND-Edge-API", "1")
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
		return
	}
	if !ps.authOK(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.routesCount++

	if ps.fault.Deleted {
		writeJSON(w, http.StatusGone, map[string]string{"reason": "node_deleted"})
		return
	}
	if ps.fault.StatusCode >= 400 {
		writeJSON(w, ps.fault.StatusCode, map[string]string{"error": "injected_fault"})
		return
	}

	ps.lastHeartbeatAt = time.Now().UTC()
	resp := ps.buildRoutesLocked()
	w.Header().Set("ETag", `"`+resp.ETag+`"`)
	w.Header().Set("Cache-Control", "no-store")
	if ifNoneMatch(r.Header.Get("If-None-Match"), resp.ETag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (ps *panelState) handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-ND-Edge-API", "1")
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
		return
	}
	if !ps.authOK(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	var req edgeStatusRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 2<<20)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
		return
	}
	ps.mu.Lock()
	ps.lastStatus = &req
	ps.statusCount++
	ps.lastHeartbeatAt = time.Now().UTC()
	ps.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (ps *panelState) handleUnregister(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-ND-Edge-API", "1")
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
		return
	}
	if !ps.authOK(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	ps.mu.Lock()
	ps.unregistered = true
	ps.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": "unregistered"})
}

func (ps *panelState) handleControlState(w http.ResponseWriter, _ *http.Request) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	snap := ps.buildRoutesLocked()
	writeJSON(w, http.StatusOK, map[string]any{
		"panel_id":     ps.panelID,
		"panel_name":   ps.panelName,
		"etag":         snap.ETag,
		"routes":       snap.Routes,
		"origin":       snap.Origin,
		"last_status":  ps.lastStatus,
		"status_count": ps.statusCount,
		"routes_count": ps.routesCount,
		"unregistered": ps.unregistered,
		"fault":        ps.fault,
	})
}

func (ps *panelState) handleControlRoutes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut && r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
		return
	}
	var body struct {
		Routes []edgeRoute `json:"routes"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4<<20)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	ps.mu.Lock()
	ps.routes = body.Routes
	snap := ps.buildRoutesLocked()
	ps.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "etag": snap.ETag, "routes": snap.Routes})
}

func (ps *panelState) handleControlOrigin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut && r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
		return
	}
	var body struct {
		IPs      []string `json:"ips"`
		Port     int      `json:"port"`
		CAPEM    *string  `json:"ca_pem"`
		CAStatus string   `json:"ca_status"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	ps.mu.Lock()
	if len(body.IPs) > 0 {
		ps.originIPs = body.IPs
	}
	if body.Port > 0 {
		ps.originPort = body.Port
	}
	if body.CAPEM != nil {
		ps.originCAPEM = *body.CAPEM
	}
	if body.CAStatus != "" {
		ps.originStatus = body.CAStatus
	}
	snap := ps.buildRoutesLocked()
	ps.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "etag": snap.ETag, "origin": snap.Origin})
}

func (ps *panelState) handleControlFault(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut && r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
		return
	}
	var fc faultConfig
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&fc); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	ps.mu.Lock()
	ps.fault = fc
	ps.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "fault": fc})
}

func ifNoneMatch(hdr, etag string) bool {
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

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
