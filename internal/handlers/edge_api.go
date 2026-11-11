package handlers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"panel/internal/db"

	"github.com/gofiber/fiber/v2"
	"golang.org/x/net/idna"
)

const (
	EdgeAPIVersion          = 1
	edgePollIntervalSec     = 30
	edgeRateLimitPerMinute  = 60
	edgeMaxRateMapEntries   = 4096
	edgeStatusMaxBodyBytes  = 2 << 20   // 2 MB
	edgeStatusMaxStoreBytes = 256 << 10 // 256 KB
	edgeTokenRotateGrace    = 10 * time.Minute
)

// EdgeRegisterRequest is the payload sent by `nd edge` to POST /api/v1/edge/register.
type EdgeRegisterRequest struct {
	EdgeID       string   `json:"edge_id"`
	EdgeVersion  string   `json:"edge_version"`
	CaddyVersion string   `json:"caddy_version"`
	Hostname     string   `json:"hostname"`
	PublicIPv4   []string `json:"public_ipv4"`
	PublicIPv6   []string `json:"public_ipv6"`
	Force        bool     `json:"force"`
}

// EdgeRegisterResponse is returned by POST /api/v1/edge/register.
type EdgeRegisterResponse struct {
	PanelID         string `json:"panel_id"`
	PanelName       string `json:"panel_name"`
	ShieldNodeID    int64  `json:"shield_node_id"`
	PollIntervalSec int    `json:"poll_interval_sec"`
	APIVersion      int    `json:"api_version"`
}

// EdgeRoutesResponse is returned by GET /api/v1/edge/routes.
type EdgeRoutesResponse struct {
	APIVersion int            `json:"api_version"`
	PanelID    string         `json:"panel_id"`
	ETag       string         `json:"etag"`
	Origin     EdgeOriginInfo `json:"origin"`
	Routes     []EdgeRoute    `json:"routes"`
	Node       EdgeNodeInfo   `json:"node"`
}

// EdgeOriginInfo describes the origin panel host & local CA for Shield -> Origin TLS.
type EdgeOriginInfo struct {
	IPs      []string `json:"ips"`
	Port     int      `json:"port"`
	CAPEM    string   `json:"ca_pem"`
	CASHA256 string   `json:"ca_sha256"`
	CAStatus string   `json:"ca_status"` // "ok" | "missing"
}

// EdgeRoute describes one route owned by this panel on the Shield node.
type EdgeRoute struct {
	ID      string           `json:"id"`
	Kind    string           `json:"kind"` // "app_domain" | "php_site" | "proxy_route" | "preview" | "panel"
	Hosts   []string         `json:"hosts"`
	Options EdgeRouteOptions `json:"options"`
}

// EdgeRouteOptions holds per-route proxy behaviour flags.
type EdgeRouteOptions struct {
	Websocket  bool `json:"websocket"`
	MaxBodyMB  int  `json:"max_body_mb"`
	UpstreamH2 bool `json:"upstream_h2"`
}

// EdgeNodeInfo identifies the Shield node record on this panel.
type EdgeNodeInfo struct {
	ID      int64  `json:"id"`
	IP      string `json:"ip"`
	Deleted bool   `json:"deleted"`
}

// EdgeStatusRequest is the payload sent by `nd edge` to POST /api/v1/edge/status.
type EdgeStatusRequest struct {
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
	Hosts          []EdgeHostStatus     `json:"hosts"`
	Conflicts      []EdgeConflictStatus `json:"conflicts"`
}

// EdgeHostStatus represents a single host's status reported by `nd edge`.
type EdgeHostStatus struct {
	Host         string `json:"host"`
	State        string `json:"state"`
	Detail       string `json:"detail"`
	CertNotAfter string `json:"cert_not_after,omitempty"`
	Issuer       string `json:"issuer,omitempty"`
}

// EdgeConflictStatus describes a host claimed by another panel on the same Shield.
type EdgeConflictStatus struct {
	Host       string `json:"host"`
	OwnerPanel string `json:"owner_panel"`
}

// EdgeUnregisterRequest is the payload sent to POST /api/v1/edge/unregister.
type EdgeUnregisterRequest struct {
	EdgeID string `json:"edge_id"`
	Reason string `json:"reason"`
}

func (p *Panel) allowEdgeRateLimit(key string) bool {
	now := time.Now()
	p.edgeRateMu.Lock()
	defer p.edgeRateMu.Unlock()
	if p.edgeRateMap == nil {
		p.edgeRateMap = make(map[string]*edgeRateWindow)
	}
	if len(p.edgeRateMap) >= edgeMaxRateMapEntries {
		for k, w := range p.edgeRateMap {
			if now.Sub(w.windowStart) > time.Minute {
				delete(p.edgeRateMap, k)
			}
		}
		if len(p.edgeRateMap) >= edgeMaxRateMapEntries {
			for k := range p.edgeRateMap {
				delete(p.edgeRateMap, k)
				if len(p.edgeRateMap) < edgeMaxRateMapEntries/2 {
					break
				}
			}
		}
	}
	w, ok := p.edgeRateMap[key]
	if !ok || now.Sub(w.windowStart) >= time.Minute {
		p.edgeRateMap[key] = &edgeRateWindow{count: 1, windowStart: now}
		return true
	}
	if w.count >= edgeRateLimitPerMinute {
		return false
	}
	w.count++
	return true
}

func extractBearerToken(c *fiber.Ctx) string {
	auth := strings.TrimSpace(c.Get("Authorization"))
	if len(auth) < 7 || !strings.EqualFold(auth[:7], "bearer ") {
		return ""
	}
	return strings.TrimSpace(auth[7:])
}

// authenticateEdgeRequest validates the Bearer token, enforces per-token rate limiting,
// detects deleted node tombstones (410 Gone), and sets X-ND-Edge-API: 1.
func (p *Panel) authenticateEdgeRequest(c *fiber.Ctx) (*db.ShieldNode, bool) {
	c.Set("X-ND-Edge-API", "1")
	token := extractBearerToken(c)
	if token == "" {
		_ = c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
		return nil, false
	}

	ctx := c.UserContext()
	node, err := p.DB.GetShieldNodeByToken(ctx, token)
	if err != nil || node == nil {
		if deleted, derr := p.DB.IsDeletedShieldToken(ctx, token); derr == nil && deleted {
			_ = c.Status(fiber.StatusGone).JSON(fiber.Map{"reason": "node_deleted"})
			return nil, false
		}
		if !p.allowEdgeRateLimit("ip:" + c.IP()) {
			c.Set("Retry-After", "60")
			_ = c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{"error": "rate_limit_exceeded"})
			return nil, false
		}
		_ = c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
		return nil, false
	}

	if !p.allowEdgeRateLimit(fmt.Sprintf("node:%d", node.ID)) {
		c.Set("Retry-After", "60")
		_ = c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{"error": "rate_limit_exceeded"})
		return nil, false
	}
	return node, true
}

func (p *Panel) panelDisplayName(ctx context.Context) string {
	if pd := strings.TrimSpace(p.DB.GetSetting(ctx, settingPanelDomain)); pd != "" {
		return pd
	}
	if hn, err := os.Hostname(); err == nil && strings.TrimSpace(hn) != "" {
		return strings.TrimSpace(hn)
	}
	return "nextdeploy-panel"
}

// EdgeRegister handles POST /api/v1/edge/register.
func (p *Panel) EdgeRegister(c *fiber.Ctx) error {
	node, ok := p.authenticateEdgeRequest(c)
	if !ok {
		return nil
	}

	var req EdgeRegisterRequest
	if err := json.Unmarshal(c.Body(), &req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid json"})
	}
	req.EdgeID = strings.TrimSpace(req.EdgeID)
	if req.EdgeID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "edge_id required"})
	}

	// Enforce token_in_use protection when bound to another edge_id with a recent heartbeat.
	if node.Status != "unregistered" && node.EdgeID != "" && node.EdgeID != req.EdgeID && !req.Force {
		now := time.Now().UTC()
		recent := (node.LastHeartbeatAt != nil && now.Sub(*node.LastHeartbeatAt) < 10*time.Minute) ||
			(node.LastSyncAt != nil && now.Sub(*node.LastSyncAt) < 10*time.Minute)
		if recent {
			return c.Status(fiber.StatusConflict).JSON(fiber.Map{"error": "token_in_use"})
		}
	}

	srcIP := strings.TrimSpace(c.IP())
	if srcIP != "" && !ipMatchesNodeOrAdvertised(srcIP, node.IP, req.PublicIPv4, req.PublicIPv6) {
		log.Printf("edge/register warning: node=%d (%s) request source IP %s not in node.IP=%s or advertised IPs", node.ID, node.Name, srcIP, node.IP)
	}

	ctx := c.UserContext()
	if err := p.DB.RegisterShieldNode(ctx, node.ID, req.EdgeID, strings.TrimSpace(req.EdgeVersion), strings.TrimSpace(req.CaddyVersion)); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	panelID, err := p.DB.EnsurePanelID(ctx)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(EdgeRegisterResponse{
		PanelID:         panelID,
		PanelName:       p.panelDisplayName(ctx),
		ShieldNodeID:    node.ID,
		PollIntervalSec: edgePollIntervalSec,
		APIVersion:      EdgeAPIVersion,
	})
}

func ipMatchesNodeOrAdvertised(srcIP, nodeIP string, ipv4, ipv6 []string) bool {
	parsedSrc := net.ParseIP(srcIP)
	check := func(candidate string) bool {
		c := strings.TrimSpace(candidate)
		if c == "" {
			return false
		}
		if c == srcIP {
			return true
		}
		if parsedSrc != nil {
			if pc := net.ParseIP(c); pc != nil && pc.Equal(parsedSrc) {
				return true
			}
		}
		return false
	}
	if check(nodeIP) {
		return true
	}
	for _, v := range ipv4 {
		if check(v) {
			return true
		}
	}
	for _, v := range ipv6 {
		if check(v) {
			return true
		}
	}
	return false
}

func normalizeEdgeHost(raw string) (string, bool) {
	h := strings.TrimSuffix(strings.TrimSpace(strings.ToLower(raw)), ".")
	if h == "" {
		return "", false
	}
	ascii, err := idna.Lookup.ToASCII(h)
	if err != nil {
		return "", false
	}
	ascii = strings.TrimSuffix(strings.TrimSpace(strings.ToLower(ascii)), ".")
	if ascii == "" {
		return "", false
	}
	return ascii, true
}

func sanitizeEdgeRouteID(raw string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(raw)) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
		if b.Len() >= 64 {
			break
		}
	}
	return b.String()
}

// BuildEdgeRoutesResponse builds the canonical, normalized, deduplicated EdgeRoutesResponse
// for the given Shield node (shared collector for GET /api/v1/edge/routes).
func (p *Panel) BuildEdgeRoutesResponse(ctx context.Context, node *db.ShieldNode) (EdgeRoutesResponse, error) {
	panelID, err := p.DB.EnsurePanelID(ctx)
	if err != nil {
		return EdgeRoutesResponse{}, err
	}

	originIP := p.originPublicIP(ctx)
	originIPs := []string{}
	if originIP != "" {
		originIPs = append(originIPs, originIP)
	}
	caPEM, caSHA, caStatus := p.ReadOriginCaddyRootCA(ctx)
	if caStatus == "" {
		caStatus = "missing"
	}

	var candidates []EdgeRoute

	// 1. app_domains assigned to this Shield node (omitting E-ORIGIN-6 pending_origin_apply rows)
	rows, err := p.DB.DB().QueryContext(ctx, `
		SELECT d.id, d.app_id, d.domain, d.enable_www,
		       CASE WHEN EXISTS (SELECT 1 FROM php_sites ps WHERE ps.app_id = d.app_id) THEN 1 ELSE 0 END AS is_php
		FROM app_domains d
		WHERE d.shield_node_id = ? AND COALESCE(d.pending_origin_apply, 0) = 0
		ORDER BY d.id ASC
	`, node.ID)
	if err != nil {
		return EdgeRoutesResponse{}, err
	}
	for rows.Next() {
		var id int64
		var appID, dom string
		var enableWWW, isPHP int
		if err := rows.Scan(&id, &appID, &dom, &enableWWW, &isPHP); err != nil {
			_ = rows.Close()
			return EdgeRoutesResponse{}, err
		}
		kind := "app_domain"
		if isPHP != 0 {
			kind = "php_site"
		}
		hosts := []string{dom}
		if enableWWW != 0 {
			clean := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(dom)), "www.")
			if clean != "" {
				hosts = append(hosts, "www."+clean)
			}
		}
		candidates = append(candidates, EdgeRoute{
			ID:      fmt.Sprintf("dom_%d", id),
			Kind:    kind,
			Hosts:   hosts,
			Options: EdgeRouteOptions{Websocket: true, MaxBodyMB: 0, UpstreamH2: false},
		})
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return EdgeRoutesResponse{}, err
	}
	_ = rows.Close()

	// 2. enabled proxy_routes assigned to this Shield node (omitting pending_origin_apply rows)
	pRows, err := p.DB.DB().QueryContext(ctx, `
		SELECT id, domain, enable_www
		FROM proxy_routes
		WHERE enabled = 1 AND shield_node_id = ? AND COALESCE(pending_origin_apply, 0) = 0
		ORDER BY id ASC
	`, node.ID)
	if err != nil {
		return EdgeRoutesResponse{}, err
	}
	for pRows.Next() {
		var id int64
		var dom string
		var enableWWW int
		if err := pRows.Scan(&id, &dom, &enableWWW); err != nil {
			_ = pRows.Close()
			return EdgeRoutesResponse{}, err
		}
		hosts := []string{dom}
		if enableWWW != 0 {
			clean := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(dom)), "www.")
			if clean != "" {
				hosts = append(hosts, "www."+clean)
			}
		}
		candidates = append(candidates, EdgeRoute{
			ID:      fmt.Sprintf("proxy_%d", id),
			Kind:    "proxy_route",
			Hosts:   hosts,
			Options: EdgeRouteOptions{Websocket: true, MaxBodyMB: 0, UpstreamH2: false},
		})
	}
	if err := pRows.Err(); err != nil {
		_ = pRows.Close()
		return EdgeRoutesResponse{}, err
	}
	_ = pRows.Close()

	// 3. active preview hosts whose resolved shield is this node
	for _, pv := range p.activePreviewRoutesForShield(ctx, &node.ID) {
		candidates = append(candidates, EdgeRoute{
			ID:      sanitizeEdgeRouteID("preview_" + pv.AppID + "_" + pv.Slot),
			Kind:    "preview",
			Hosts:   []string{pv.Domain},
			Options: EdgeRouteOptions{Websocket: true, MaxBodyMB: 0, UpstreamH2: false},
		})
	}

	// 4. panel domain only if admin explicitly shielded it on this node
	panelDomain := strings.TrimSpace(p.DB.GetSetting(ctx, settingPanelDomain))
	if panelDomain != "" && p.panelShieldNodeID(ctx) == node.ID {
		hosts := []string{panelDomain}
		if settingBool(p.DB.GetSetting(ctx, settingPanelEnableWWW), false) {
			clean := strings.TrimPrefix(strings.ToLower(panelDomain), "www.")
			if clean != "" {
				hosts = append(hosts, "www."+clean)
			}
		}
		candidates = append(candidates, EdgeRoute{
			ID:      "panel",
			Kind:    "panel",
			Hosts:   hosts,
			Options: EdgeRouteOptions{Websocket: true, MaxBodyMB: 0, UpstreamH2: false},
		})
	}

	// Normalize, convert IDN -> punycode, dedupe across rows (keep first), sort hosts & routes.
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].ID < candidates[j].ID
	})

	seenHosts := make(map[string]bool)
	routes := make([]EdgeRoute, 0, len(candidates))
	for _, cand := range candidates {
		uniqueHosts := make([]string, 0, len(cand.Hosts))
		for _, rawHost := range cand.Hosts {
			norm, ok := normalizeEdgeHost(rawHost)
			if !ok {
				continue
			}
			if seenHosts[norm] {
				log.Printf("edge/routes: duplicate host %q dropped from route %s", norm, cand.ID)
				continue
			}
			seenHosts[norm] = true
			uniqueHosts = append(uniqueHosts, norm)
		}
		if len(uniqueHosts) == 0 {
			continue
		}
		sort.Strings(uniqueHosts)
		cand.Hosts = uniqueHosts
		routes = append(routes, cand)
	}

	resp := EdgeRoutesResponse{
		APIVersion: EdgeAPIVersion,
		PanelID:    panelID,
		Origin: EdgeOriginInfo{
			IPs:      originIPs,
			Port:     443,
			CAPEM:    caPEM,
			CASHA256: caSHA,
			CAStatus: caStatus,
		},
		Routes: routes,
		Node: EdgeNodeInfo{
			ID:      node.ID,
			IP:      node.IP,
			Deleted: false,
		},
	}
	etag, err := ComputeEdgeRoutesETag(resp)
	if err != nil {
		return EdgeRoutesResponse{}, err
	}
	resp.ETag = etag
	return resp, nil
}

// ComputeEdgeRoutesETag computes the sha256 hex digest of the canonical sorted JSON
// representation of EdgeRoutesResponse (sorted object keys, sorted routes by id, sorted hosts).
func ComputeEdgeRoutesETag(resp EdgeRoutesResponse) (string, error) {
	sortedRoutes := make([]map[string]interface{}, 0, len(resp.Routes))
	routesCopy := append([]EdgeRoute(nil), resp.Routes...)
	sort.Slice(routesCopy, func(i, j int) bool {
		return routesCopy[i].ID < routesCopy[j].ID
	})
	for _, r := range routesCopy {
		hosts := append([]string(nil), r.Hosts...)
		sort.Strings(hosts)
		sortedRoutes = append(sortedRoutes, map[string]interface{}{
			"hosts": hosts,
			"id":    r.ID,
			"kind":  r.Kind,
			"options": map[string]interface{}{
				"max_body_mb": r.Options.MaxBodyMB,
				"upstream_h2": r.Options.UpstreamH2,
				"websocket":   r.Options.Websocket,
			},
		})
	}
	ips := append([]string(nil), resp.Origin.IPs...)
	canonical := map[string]interface{}{
		"api_version": resp.APIVersion,
		"node": map[string]interface{}{
			"deleted": resp.Node.Deleted,
			"id":      resp.Node.ID,
			"ip":      resp.Node.IP,
		},
		"origin": map[string]interface{}{
			"ca_pem":    resp.Origin.CAPEM,
			"ca_sha256": resp.Origin.CASHA256,
			"ca_status": resp.Origin.CAStatus,
			"ips":       ips,
			"port":      resp.Origin.Port,
		},
		"panel_id": resp.PanelID,
		"routes":   sortedRoutes,
	}
	raw, err := json.Marshal(canonical)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func ifNoneMatchMatches(headerVal, etag string) bool {
	if strings.TrimSpace(headerVal) == "" || etag == "" {
		return false
	}
	for _, part := range strings.Split(headerVal, ",") {
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

// EdgeRoutes handles GET /api/v1/edge/routes.
func (p *Panel) EdgeRoutes(c *fiber.Ctx) error {
	node, ok := p.authenticateEdgeRequest(c)
	if !ok {
		return nil
	}
	ctx := c.UserContext()
	resp, err := p.BuildEdgeRoutesResponse(ctx, node)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	_ = p.DB.UpdateShieldNodeHeartbeat(ctx, node.ID)
	c.Set("ETag", `"`+resp.ETag+`"`)
	c.Set("Cache-Control", "no-store")
	if ifNoneMatchMatches(c.Get("If-None-Match"), resp.ETag) {
		return c.SendStatus(fiber.StatusNotModified)
	}
	return c.JSON(resp)
}

func capEdgeStatusJSON(req EdgeStatusRequest, rawBody []byte) string {
	if len(rawBody) <= edgeStatusMaxStoreBytes {
		return string(rawBody)
	}
	trimmed := req
	for len(trimmed.Hosts) > 0 {
		trimmed.Hosts = trimmed.Hosts[:len(trimmed.Hosts)/2]
		if b, err := json.Marshal(trimmed); err == nil && len(b) <= edgeStatusMaxStoreBytes {
			return string(b)
		}
	}
	trimmed.Hosts = nil
	trimmed.Conflicts = nil
	b, _ := json.Marshal(trimmed)
	if len(b) > edgeStatusMaxStoreBytes {
		return ""
	}
	return string(b)
}

// EdgeStatus handles POST /api/v1/edge/status.
func (p *Panel) EdgeStatus(c *fiber.Ctx) error {
	node, ok := p.authenticateEdgeRequest(c)
	if !ok {
		return nil
	}
	raw := c.Body()
	if len(raw) > edgeStatusMaxBodyBytes || c.Request().Header.ContentLength() > edgeStatusMaxBodyBytes {
		return c.Status(fiber.StatusRequestEntityTooLarge).JSON(fiber.Map{"error": "body exceeds 2 MB limit"})
	}
	var req EdgeStatusRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid json"})
	}

	statuses := make([]db.ShieldDomainStatus, 0, len(req.Hosts)+len(req.Conflicts))
	seen := make(map[string]bool, len(req.Hosts))
	for _, h := range req.Hosts {
		host := strings.TrimSuffix(strings.TrimSpace(strings.ToLower(h.Host)), ".")
		if host == "" {
			continue
		}
		var certExp *time.Time
		if s := strings.TrimSpace(h.CertNotAfter); s != "" {
			if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
				u := t.UTC()
				certExp = &u
			} else if t, err := time.Parse(time.RFC3339, s); err == nil {
				u := t.UTC()
				certExp = &u
			}
		}
		seen[host] = true
		statuses = append(statuses, db.ShieldDomainStatus{
			NodeID:       node.ID,
			Host:         host,
			State:        strings.TrimSpace(h.State),
			Detail:       strings.TrimSpace(h.Detail),
			CertNotAfter: certExp,
		})
	}
	for _, cf := range req.Conflicts {
		host := strings.TrimSuffix(strings.TrimSpace(strings.ToLower(cf.Host)), ".")
		if host == "" || seen[host] {
			continue
		}
		seen[host] = true
		statuses = append(statuses, db.ShieldDomainStatus{
			NodeID: node.ID,
			Host:   host,
			State:  "conflict",
			Detail: strings.TrimSpace(cf.OwnerPanel),
		})
	}

	storedJSON := capEdgeStatusJSON(req, raw)
	ctx := c.UserContext()
	if err := p.DB.UpdateShieldNodeEdgeStatus(
		ctx,
		node.ID,
		strings.TrimSpace(req.EdgeVersion),
		strings.TrimSpace(req.CaddyVersion),
		strings.TrimSpace(req.AppliedETag),
		strings.TrimSpace(req.LastApplyError),
		storedJSON,
		statuses,
	); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(fiber.Map{"ok": true})
}

// EdgeUnregister handles POST /api/v1/edge/unregister.
func (p *Panel) EdgeUnregister(c *fiber.Ctx) error {
	node, ok := p.authenticateEdgeRequest(c)
	if !ok {
		return nil
	}
	var req EdgeUnregisterRequest
	if len(c.Body()) > 0 {
		_ = json.Unmarshal(c.Body(), &req)
	}
	if err := p.DB.UnregisterShieldNode(c.UserContext(), node.ID); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"ok": true, "status": "unregistered"})
}

// RotateShieldNodeToken rotates a Shield node's token while keeping token_prev valid for 10 minutes.
// POST /api/v1/shield-nodes/:id/rotate-token
func (p *Panel) RotateShieldNodeToken(c *fiber.Ctx) error {
	id, err := c.ParamsInt("id")
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"ok": false, "error": "Invalid ID"})
	}
	ctx := c.UserContext()
	node, err := p.DB.RotateShieldNodeToken(ctx, int64(id), edgeTokenRotateGrace)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"ok": false, "error": "Shield node not found"})
	}

	panelHost := c.Hostname()
	scheme := "http"
	if c.Secure() || c.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	setupCmd := fmt.Sprintf("curl -sSL \"%s://%s/api/v1/edge/setup.sh?token=%s\" | bash", scheme, panelHost, node.Token)
	uninstallCmd := fmt.Sprintf("curl -sSL \"%s://%s/api/v1/edge/uninstall.sh?token=%s\" | bash", scheme, panelHost, node.Token)

	p.RecordAuditLog(c, "shield_token_rotate", "shield_node", strconv.Itoa(id), "Rotated token for Shield node "+node.Name)
	return c.JSON(fiber.Map{
		"ok":                    true,
		"node":                  node,
		"token":                 node.Token,
		"token_prev_expires_at": node.TokenPrevExpiresAt,
		"setup_command":         setupCmd,
		"uninstall_command":     uninstallCmd,
	})
}

var errCFRedirectLoop = errors.New("cloudflare redirect loop")

// isSafeCFCheckHost validates that host is a public FQDN (no IP literals, ports, localhost, or internal suffixes)
// before making any outbound HTTP request (prevents SSRF).
func isSafeCFCheckHost(raw string) (string, bool) {
	h := strings.TrimSpace(strings.ToLower(raw))
	if h == "" || len(h) > 253 {
		return "", false
	}
	if strings.ContainsAny(h, ":/?#@*\\ \t\r\n") {
		return "", false
	}
	ascii, ok := normalizeEdgeHost(h)
	if !ok || len(ascii) > 253 || !strings.Contains(ascii, ".") {
		return "", false
	}
	if net.ParseIP(ascii) != nil {
		return "", false
	}
	if ascii == "localhost" {
		return "", false
	}
	for _, sfx := range []string{".local", ".localhost", ".internal", ".test", ".example", ".invalid", ".lan", ".home.arpa"} {
		if strings.HasSuffix(ascii, sfx) {
			return "", false
		}
	}
	for _, b := range []byte(ascii) {
		if !((b >= 'a' && b <= 'z') || (b >= '0' && b <= '9') || b == '.' || b == '-') {
			return "", false
		}
	}
	return ascii, true
}

// CheckCloudflareFlexibleLoop fetches http://<host>/ following up to 10 redirects to detect
// Cloudflare Flexible SSL redirect loops (E-CF-4).
func CheckCloudflareFlexibleLoop(ctx context.Context, host string, customClient ...*http.Client) (bool, string) {
	safeHost, ok := isSafeCFCheckHost(host)
	if !ok {
		return false, ""
	}

	targetURL := "http://" + safeHost + "/"
	return checkRedirectLoopURL(ctx, targetURL, customClient...)
}

func checkRedirectLoopURL(ctx context.Context, targetURL string, customClient ...*http.Client) (bool, string) {
	visited := map[string]int{targetURL: 1}
	var baseTransport http.RoundTripper
	timeout := 4 * time.Second
	if len(customClient) > 0 && customClient[0] != nil {
		baseTransport = customClient[0].Transport
		if customClient[0].Timeout > 0 {
			timeout = customClient[0].Timeout
		}
	}

	client := &http.Client{
		Timeout:   timeout,
		Transport: baseTransport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return errCFRedirectLoop
			}
			u := req.URL.Scheme + "://" + strings.ToLower(req.URL.Host) + req.URL.Path
			visited[u]++
			if visited[u] >= 3 {
				return errCFRedirectLoop
			}
			return nil
		},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return false, ""
	}
	resp, err := client.Do(req)
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if errors.Is(err, errCFRedirectLoop) {
		return true, "Cloudflare Flexible SSL mode detected (redirect loop). Set SSL mode to Full or Full (strict) and turn Always Use HTTPS off."
	}
	return false, ""
}

// ShieldNodeCFCheck runs the E-CF-4 Flexible SSL redirect-loop probe for a domain.
// GET /api/v1/shield-nodes/:id/cf-check?domain=example.com
func (p *Panel) ShieldNodeCFCheck(c *fiber.Ctx) error {
	domain := strings.TrimSpace(c.Query("domain"))
	safeDomain, ok := isSafeCFCheckHost(domain)
	if !ok {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"ok": false, "error": "valid public domain required"})
	}
	ctx, cancel := context.WithTimeout(c.UserContext(), 5*time.Second)
	defer cancel()
	loop, hint := CheckCloudflareFlexibleLoop(ctx, safeDomain)
	return c.JSON(fiber.Map{
		"ok":               true,
		"domain":           safeDomain,
		"cf_flexible_loop": loop,
		"cf_hint":          hint,
	})
}
