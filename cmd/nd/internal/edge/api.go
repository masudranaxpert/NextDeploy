package edge

import (
	"fmt"
	"time"
)

const (
	// EdgeAPIVersion is the current /api/v1/edge protocol version.
	EdgeAPIVersion = 1

	// MinSupportedAPIVersion and MaxSupportedAPIVersion define the panel API
	// major versions supported by this nd-edge build (E-MP-6).
	MinSupportedAPIVersion = 1
	MaxSupportedAPIVersion = 1

	// HeaderEdgeAPI is returned by panel /api/v1/edge/* responses.
	HeaderEdgeAPI = "X-ND-Edge-API"
)

// Route kinds returned by GET /api/v1/edge/routes.
const (
	KindAppDomain  = "app_domain"
	KindPHPSite    = "php_site"
	KindProxyRoute = "proxy_route"
	KindPreview    = "preview"
	KindPanel      = "panel"
)

// Origin CA status values.
const (
	CAStatusOK      = "ok"
	CAStatusMissing = "missing"
)

// Per-host status states reported in POST /api/v1/edge/status (§3.6).
const (
	StateActive              = "active"
	StateIssuing             = "issuing"
	StateCertError           = "cert_error"
	StatePendingDNS          = "pending_dns"
	StatePendingCAA          = "pending_caa"
	StateDNSDrift            = "dns_drift"
	StateConflict            = "conflict"
	StateQueuedRateLimit     = "queued_rate_limit"
	StateUnsupportedWildcard = "unsupported_wildcard"
	StateExcludedInternal    = "excluded_internal"
	StateExcludedLoop        = "excluded_loop"
	StateExcludedQuota       = "excluded_quota"
	StateHeldMassDelete      = "held_mass_delete"
)

// Warning codes surfaced in host detail or panel state (§3.7, §5.4).
const (
	WarningOriginCAMissing  = "origin_ca_missing"
	WarningCertRenewOverdue = "cert_renew_overdue"
)

// Panel sync states tracked locally by nd-edge and reported in CLI/status.
const (
	SyncStatePending      = "pending"
	SyncStateOK           = "ok"
	SyncStateStale        = "stale"
	SyncStateAuthFailed   = "auth_failed"
	SyncStatePanelOutdate = "panel_outdated"
	SyncStateBlockedWAF   = "panel_blocked_by_waf"
	SyncStateError        = "error"
	SyncStateDeleted      = "deleted"
)

// RegisterRequest is the body for POST /api/v1/edge/register.
type RegisterRequest struct {
	EdgeID      string   `json:"edge_id"`
	EdgeVersion string   `json:"edge_version"`
	CaddyVer    string   `json:"caddy_version"`
	Hostname    string   `json:"hostname"`
	PublicIPv4  []string `json:"public_ipv4"`
	PublicIPv6  []string `json:"public_ipv6"`
	Force       bool     `json:"force"`
}

// RegisterResponse is the 200 OK body from POST /api/v1/edge/register.
type RegisterResponse struct {
	PanelID         string `json:"panel_id"`
	PanelName       string `json:"panel_name"`
	ShieldNodeID    int64  `json:"shield_node_id"`
	PollIntervalSec int    `json:"poll_interval_sec"`
	APIVersion      int    `json:"api_version"`
}

// RoutesResponse is the 200 OK body from GET /api/v1/edge/routes.
type RoutesResponse struct {
	APIVersion int         `json:"api_version"`
	PanelID    string      `json:"panel_id"`
	ETag       string      `json:"etag"`
	Origin     OriginInfo  `json:"origin"`
	Routes     []EdgeRoute `json:"routes"`
	Node       NodeInfo    `json:"node"`
}

// OriginInfo describes the panel origin Caddy instance.
type OriginInfo struct {
	IPs      []string `json:"ips"`
	Port     int      `json:"port"`
	CAPEM    string   `json:"ca_pem"`
	CASHA256 string   `json:"ca_sha256"`
	CAStatus string   `json:"ca_status"`
}

// EdgeRoute is a single route group for one app/site/proxy/preview/panel.
type EdgeRoute struct {
	ID           string       `json:"id"`
	Kind         string       `json:"kind"`
	Hosts        []string     `json:"hosts"`
	TrustedProxy bool         `json:"trusted_proxy,omitempty"`
	Options      RouteOptions `json:"options"`
}

// RouteOptions controls proxy behavior for a route.
type RouteOptions struct {
	Websocket  bool `json:"websocket"`
	MaxBodyMB  int  `json:"max_body_mb"`
	UpstreamH2 bool `json:"upstream_h2"`
}

// NodeInfo identifies the shield node record on the panel.
type NodeInfo struct {
	ID      int64  `json:"id"`
	IP      string `json:"ip"`
	Deleted bool   `json:"deleted"`
}

// StatusRequest is the body for POST /api/v1/edge/status (§3.6).
type StatusRequest struct {
	EdgeID         string           `json:"edge_id"`
	EdgeVersion    string           `json:"edge_version"`
	CaddyVersion   string           `json:"caddy_version"`
	CaddyRunning   bool             `json:"caddy_running"`
	AppliedETag    string           `json:"applied_etag"`
	LastApplyAt    *time.Time       `json:"last_apply_at,omitempty"`
	LastApplyError string           `json:"last_apply_error"`
	UptimeSec      int64            `json:"uptime_sec"`
	DiskFreePct    float64          `json:"disk_free_pct"`
	MemFreeMB      int64            `json:"mem_free_mb"`
	ClockSkewSec   int64            `json:"clock_skew_sec"`
	Hosts          []HostStatus     `json:"hosts"`
	Conflicts      []ConflictStatus `json:"conflicts"`
}

// HostStatus reports the reconciliation & TLS state of a single host for its owning panel.
type HostStatus struct {
	Host         string     `json:"host"`
	State        string     `json:"state"`
	Detail       string     `json:"detail"`
	CertNotAfter *time.Time `json:"cert_not_after,omitempty"`
	Issuer       string     `json:"issuer"`
}

// ConflictStatus reports a domain conflict without leaking other panels' host lists.
type ConflictStatus struct {
	Host       string `json:"host"`
	OwnerPanel string `json:"owner_panel"`
}

// UnregisterRequest is the body for POST /api/v1/edge/unregister.
type UnregisterRequest struct {
	EdgeID string `json:"edge_id"`
	Reason string `json:"reason"`
}

// APIErrorResponse represents structured JSON errors from /api/v1/edge/*.
type APIErrorResponse struct {
	Error  string `json:"error,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// CheckAPIVersion verifies that the panel's api_version falls within the
// supported min..max range.
func CheckAPIVersion(v int) error {
	if v < MinSupportedAPIVersion || v > MaxSupportedAPIVersion {
		return fmt.Errorf("unsupported panel api_version %d (supported %d..%d)", v, MinSupportedAPIVersion, MaxSupportedAPIVersion)
	}
	return nil
}

// IsValidHostState reports whether s is one of the canonical host states in §3.6.
func IsValidHostState(s string) bool {
	switch s {
	case StateActive,
		StateIssuing,
		StateCertError,
		StatePendingDNS,
		StatePendingCAA,
		StateDNSDrift,
		StateConflict,
		StateQueuedRateLimit,
		StateUnsupportedWildcard,
		StateExcludedInternal,
		StateExcludedLoop,
		StateExcludedQuota,
		StateHeldMassDelete:
		return true
	default:
		return false
	}
}
