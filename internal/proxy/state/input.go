// Package state constructs the engine-agnostic proxy.State intermediate
// representation (IR) from pre-loaded NextDeploy panel, app, PHP, route,
// shield, and port inputs without performing database, Docker, or network I/O.
package state

import (
	"panel/internal/db"
	"panel/internal/proxy"
)

// AppPreviewConfig describes the per-app preview URL configuration stored under
// settings["preview:<appID>"].
type AppPreviewConfig struct {
	Provider string `json:"provider"` // "sslip.io", "nip.io", or "custom"
	Base     string `json:"base"`     // custom base domain when Provider == "custom"
	Service  string `json:"service"`  // target compose service for preview traffic
	Port     int    `json:"port"`     // container port (defaults to 3000 when <= 0)
}

// AppTarget holds pre-resolved compose project, container, workspace, preview,
// and custom path-route metadata for a single Docker app.
type AppTarget struct {
	WorkspaceDir      string                      // container-visible app workspace path (e.g. /data/workspaces/<appID>)
	ActiveProject     string                      // pre-resolved active compose project name (optional override)
	StandbyProject    string                      // pre-resolved standby compose project name (optional override)
	ServiceContainers map[string]string           // service -> resolved container name for active slot
	StandbyContainers map[string]string           // service -> resolved container name for standby slot
	Preview           *AppPreviewConfig           // nil or empty Provider disables slot preview site
	PublicIP          string                      // pre-resolved public or shield IP for preview hostnames
	ShieldNodeID      *int64                      // non-nil when preview traffic routes through a Shield node
	PathRoutes        []proxy.PathRoute           // optional app-level path routes (e.g. nested service proxy)
	DomainPathRoutes  map[int64][]proxy.PathRoute // optional per-domain path routes keyed by AppDomain.ID
	GatewayBindings   []proxy.HostPortBinding     // optional compose-extracted port bindings for ContainerPort lookup
}

// PHPSiteInput bundles a db.PHPSite record with its pre-loaded security,
// routing/settings, owner suspension status, and container-visible paths.
type PHPSiteInput struct {
	Site           db.PHPSite
	Security       proxy.SecuritySpec
	SecurityJSON   string             // optional raw JSON parsed when Security is zero
	Routing        *proxy.SiteRouting // optional override; when nil, parsed from Site.SettingsJSON
	Stopped        bool               // combined with SettingsJSON.Stopped and OwnerSuspended
	OwnerSuspended bool               // true when the owning user account is suspended
	DocRoot        string             // optional override; defaults to /data/workspaces/<appID>/htdocs[/<root_dir>]
	SocketPath     string             // optional override; defaults to /data/workspaces/.nd-php/u<owner>-v<ver>/run/<appID>.sock
}

// BuildInput holds all pre-loaded NextDeploy configuration and database state
// required by Build to produce a deterministic *proxy.State IR.
type BuildInput struct {
	// Panel settings
	ProxyEngine          string // loaded from settings["proxy_engine"] (metadata for callers; ignored by IR generation)
	AdminAPI             string // loaded from caddy_config["admin_api"] (metadata for callers; ignored by IR generation)
	PanelDomain          string
	PanelEnableHTTPS     bool
	PanelEnableWWW       bool
	PanelID              string
	PanelShieldNodeID    *int64
	ACMEEmail            string
	SharedMountPrefix    string
	SharedVolumeNamesRaw string
	SharedVolumeNames    []string

	// Global proxy options & Edge Shield inventory
	Global          proxy.GlobalOptions
	ShieldNodes     []db.ShieldNode
	DefaultPublicIP string // origin server public IP fallback for preview hosts

	// Docker apps, domains, and pre-resolved targets
	Apps       []db.App
	AppDomains []db.AppDomain
	AppTargets map[string]AppTarget

	// PHP / static sites
	PHPSites []PHPSiteInput

	// Custom reverse-proxy / redirect routes
	ProxyRoutes []db.ProxyRoute

	// Host port allocations (Direct == false ports become HTTP gateways)
	AllocatedPorts []db.AllocatedPort
}
