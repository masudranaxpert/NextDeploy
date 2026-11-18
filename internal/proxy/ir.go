/*
*
Package proxy defines the engine-agnostic reverse-proxy abstraction, desired-state
intermediate representation (IR), driver contract, capability preflight evaluator,
and shared routing/validation/port-extraction helpers.

Concrete proxy backends (Caddy, Angie, Ferron) implement [Driver] and register
themselves via [Register]. Callers construct a [State] IR, compute a deterministic
configuration digest with [State.ComputeGeneration], and evaluate target engine
compatibility via [EvaluateCapabilities].
*/
package proxy

import (
	"strings"
	"time"
)

// Engine identifies a supported reverse proxy engine.
type Engine string

// Supported reverse proxy engine identifiers.
const (
	EngineCaddy  Engine = "caddy"
	EngineAngie  Engine = "angie"
	EngineFerron Engine = "ferron"
)

// ValidEngine reports whether name is a recognized reverse proxy engine identifier.
func ValidEngine(name string) bool {
	_, ok := ParseEngine(name)
	return ok
}

// ParseEngine normalizes and validates a proxy engine identifier.
func ParseEngine(name string) (Engine, bool) {
	switch Engine(strings.ToLower(strings.TrimSpace(name))) {
	case EngineCaddy:
		return EngineCaddy, true
	case EngineAngie:
		return EngineAngie, true
	case EngineFerron:
		return EngineFerron, true
	default:
		return "", false
	}
}

// TLSMode defines how TLS certificates are provisioned and served for a site.
type TLSMode string

// Supported TLS modes for virtual host blocks.
const (
	TLSAuto     TLSMode = "auto"
	TLSInternal TLSMode = "internal"
	TLSNone     TLSMode = "none"
)

// SiteKind identifies the serving behavior of a site in the desired-state IR.
type SiteKind string

// Supported site kinds.
const (
	SiteKindAppProxy      SiteKind = "app_proxy"
	SiteKindPHP           SiteKind = "php"
	SiteKindStatic        SiteKind = "static"
	SiteKindRedirect      SiteKind = "redirect"
	SiteKindExternalProxy SiteKind = "external_proxy"
)

// HeaderAction describes how an HTTP header operation modifies headers.
type HeaderAction string

// Supported HTTP header modification actions.
const (
	HeaderSet    HeaderAction = "set"
	HeaderAdd    HeaderAction = "add"
	HeaderDelete HeaderAction = "del"
)

// HeaderOp represents a single response or upstream request header modification.
type HeaderOp struct {
	Action HeaderAction `json:"action"`
	Name   string       `json:"name"`
	Value  string       `json:"value,omitempty"`
}

// HandlerKind identifies the type of request handler in a site or path route.
type HandlerKind string

// Supported handler kinds.
const (
	HandlerProxy     HandlerKind = "proxy"
	HandlerStaticDir HandlerKind = "static_dir"
	HandlerPHP       HandlerKind = "php"
	HandlerRedirect  HandlerKind = "redirect"
)

// StaticDirSpec configures direct static file serving for a directory root.
type StaticDirSpec struct {
	Root          string   `json:"root"`
	Hide          []string `json:"hide,omitempty"`
	Precompressed []string `json:"precompressed,omitempty"`
}

// Handler describes how requests matching a path route or site default are served.
type Handler struct {
	Kind     HandlerKind    `json:"kind"`
	Static   *StaticDirSpec `json:"static,omitempty"`
	Upstream *UpstreamRef   `json:"upstream,omitempty"`
	PHP      *PHPSpec       `json:"php,omitempty"`
	Redirect *RedirectSpec  `json:"redirect,omitempty"`
}

// PathRoute maps a URL path prefix to a handler, ordered by priority.
type PathRoute struct {
	Prefix      string  `json:"prefix"`
	StripPrefix bool    `json:"strip_prefix,omitempty"`
	Priority    int     `json:"priority,omitempty"`
	Handler     Handler `json:"handler"`
}

// UpstreamRef identifies the backend target for a proxied site, path route, or gateway port.
type UpstreamRef struct {
	Name           string     `json:"name,omitempty"`
	Service        string     `json:"service,omitempty"`
	Container      string     `json:"container,omitempty"`
	Port           int        `json:"port,omitempty"`
	Scheme         string     `json:"scheme,omitempty"`
	SkipVerify     bool       `json:"skip_verify,omitempty"`
	External       string     `json:"external,omitempty"`
	LongStreams    bool       `json:"long_streams,omitempty"`
	LBTryDuration  string     `json:"lb_try_duration,omitempty"`
	FailDuration   string     `json:"fail_duration,omitempty"`
	RequestHeaders []HeaderOp `json:"request_headers,omitempty"`
}

// PHPCacheSpec configures static asset and WordPress page-cache behavior for PHP/static sites.
type PHPCacheSpec struct {
	WordPressPageCache bool   `json:"wordpress_page_cache,omitempty"`
	MediaCacheControl  string `json:"media_cache_control,omitempty"`
	CodeCacheControl   string `json:"code_cache_control,omitempty"`
}

// PHPSpec configures PHP-FPM or static document-root serving for a site.
type PHPSpec struct {
	DocRoot             string        `json:"doc_root"`
	Socket              string        `json:"socket,omitempty"`
	PHPVersion          string        `json:"php_version,omitempty"`
	SiteType            string        `json:"site_type,omitempty"`
	Suspended           bool          `json:"suspended,omitempty"`
	FallbackMode        string        `json:"fallback_mode,omitempty"`
	FallbackEntry       string        `json:"fallback_entry,omitempty"`
	Routing             []RoutingRule `json:"routing,omitempty"`
	Compression         []string      `json:"compression,omitempty"`
	Precompressed       []string      `json:"precompressed,omitempty"`
	HidePatterns        []string      `json:"hide_patterns,omitempty"`
	Cache               *PHPCacheSpec `json:"cache,omitempty"`
	FastCGIDialTimeout  string        `json:"fastcgi_dial_timeout,omitempty"`
	FastCGIReadTimeout  string        `json:"fastcgi_read_timeout,omitempty"`
	FastCGIWriteTimeout string        `json:"fastcgi_write_timeout,omitempty"`
	HTTPS               bool          `json:"https,omitempty"`
}

// SecuritySpec defines per-site access control and HTTP security enforcement
// applied before a request reaches PHP or static files.
type SecuritySpec struct {
	BlockedIPs      []string `json:"blocked_ips"`
	AllowOnly       []string `json:"allow_only"`
	BasicAuthUser   string   `json:"basic_auth_user"`
	BasicAuthHash   string   `json:"basic_auth_hash"`
	BlockXMLRPC     bool     `json:"block_xmlrpc"`
	BlockUploadPHP  bool     `json:"block_upload_php"`
	BlockScanners   bool     `json:"block_scanners"`
	HSTS            bool     `json:"hsts"`
	FrameProtection bool     `json:"frame_protection"`
}

// SiteSecurity is an alias for SecuritySpec preserved for site-security API compatibility.
type SiteSecurity = SecuritySpec

// RedirectSpec configures an HTTP redirect response for a site.
type RedirectSpec struct {
	Code     int    `json:"code"`
	Target   string `json:"target"`
	KeepPath bool   `json:"keep_path,omitempty"`
}

// Site represents one virtual host or host group served by the reverse proxy.
type Site struct {
	ID           string        `json:"id"`
	AppID        string        `json:"app_id,omitempty"`
	DomainID     int64         `json:"domain_id,omitempty"`
	RouteID      int64         `json:"route_id,omitempty"`
	Slot         string        `json:"slot,omitempty"`
	IsPreview    bool          `json:"is_preview,omitempty"`
	Hosts        []string      `json:"hosts"`
	TLS          TLSMode       `json:"tls"`
	OnDemandTLS  bool          `json:"on_demand_tls,omitempty"`
	BehindShield bool          `json:"behind_shield,omitempty"`
	TrustedProxy bool          `json:"trusted_proxy,omitempty"`
	Headers      []HeaderOp    `json:"headers,omitempty"`
	Kind         SiteKind      `json:"kind"`
	Paths        []PathRoute   `json:"paths,omitempty"`
	Default      *Handler      `json:"default,omitempty"`
	PHP          *PHPSpec      `json:"php,omitempty"`
	Security     *SecuritySpec `json:"security,omitempty"`
	Redirect     *RedirectSpec `json:"redirect,omitempty"`
	Upstream     *UpstreamRef  `json:"upstream,omitempty"`
}

// GatewayPort maps a host port published by the proxy container to an app upstream.
type GatewayPort struct {
	Port          int          `json:"port"`
	ContainerPort int          `json:"container_port"`
	BindIP        string       `json:"bind_ip"`
	Protocol      string       `json:"protocol"`
	Direct        bool         `json:"direct,omitempty"`
	AppID         string       `json:"app_id,omitempty"`
	Upstream      *UpstreamRef `json:"upstream,omitempty"`
}

// SharedMount binds an external Docker volume into the proxy container for static file serving.
type SharedMount struct {
	VolumeName string `json:"volume_name"`
	Target     string `json:"target"`
	ReadOnly   bool   `json:"read_only,omitempty"`
}

// State is the complete engine-agnostic desired-state intermediate representation
// of all reverse-proxy configuration derived from NextDeploy state.
type State struct {
	Global       GlobalOptions `json:"global"`
	ACMEEmail    string        `json:"acme_email,omitempty"`
	Panel        *Site         `json:"panel,omitempty"`
	Sites        []Site        `json:"sites,omitempty"`
	Gateways     []GatewayPort `json:"gateways,omitempty"`
	SharedMounts []SharedMount `json:"shared_mounts,omitempty"`
	Generation   string        `json:"generation,omitempty"`
}

// Reason describes what triggered a proxy synchronization request.
type Reason struct {
	Kind   string `json:"kind"`
	ID     string `json:"id,omitempty"`
	Detail string `json:"detail,omitempty"`
}

// StackOptions configures root-stack Docker Compose service generation for a proxy driver.
type StackOptions struct {
	Engine           Engine        `json:"engine"`
	Image            string        `json:"image,omitempty"`
	ContainerName    string        `json:"container_name,omitempty"`
	NetworkName      string        `json:"network_name,omitempty"`
	NetworkKey       string        `json:"network_key,omitempty"`
	AdminAPI         string        `json:"admin_api,omitempty"`
	ACMEEmail        string        `json:"acme_email,omitempty"`
	PanelDomain      string        `json:"panel_domain,omitempty"`
	PanelEnableHTTPS bool          `json:"panel_enable_https,omitempty"`
	PanelEnableWWW   bool          `json:"panel_enable_www,omitempty"`
	Global           GlobalOptions `json:"global"`
	SharedMounts     []SharedMount `json:"shared_mounts,omitempty"`
	Gateways         []GatewayPort `json:"gateways,omitempty"`
	DataDir          string        `json:"data_dir,omitempty"`
	PKIDir           string        `json:"pki_dir,omitempty"`
	WorkspacesDir    string        `json:"workspaces_dir,omitempty"`
	PHPSocketsDir    string        `json:"php_sockets_dir,omitempty"`
	AdminTokenEnv    string        `json:"admin_token_env,omitempty"`
}

// Files maps relative paths inside an engine configuration directory to rendered file contents.
type Files map[string]string

// ApplyResult captures the outcome of rendering, validating, and applying proxy configuration.
type ApplyResult struct {
	Generation string        `json:"generation"`
	Driver     string        `json:"driver"`
	Changed    bool          `json:"changed"`
	Reloaded   bool          `json:"reloaded"`
	RolledBack bool          `json:"rolled_back,omitempty"`
	Duration   time.Duration `json:"duration"`
	Message    string        `json:"message,omitempty"`
	AppliedAt  time.Time     `json:"applied_at"`
}

// Status reports live health and runtime metrics of the active reverse proxy engine.
type Status struct {
	Driver         string     `json:"driver"`
	ContainerName  string     `json:"container_name"`
	Running        bool       `json:"running"`
	Version        string     `json:"version,omitempty"`
	Connections    int64      `json:"connections,omitempty"`
	ActiveRequests int64      `json:"active_requests,omitempty"`
	Generation     string     `json:"generation,omitempty"`
	LastReloadAt   *time.Time `json:"last_reload_at,omitempty"`
	LastError      string     `json:"last_error,omitempty"`
	Detail         string     `json:"detail,omitempty"`
	RawConfig      string     `json:"raw_config,omitempty"`
	Experimental   bool       `json:"experimental,omitempty"`
}
