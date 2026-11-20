package proxy

import "context"

// Driver defines the engine-specific adapter contract implemented by each reverse
// proxy backend (Caddy, Angie, Ferron). Implementations must be safe for concurrent calls.
type Driver interface {
	// Name returns the canonical lowercase engine identifier ("caddy", "angie", or "ferron").
	Name() string

	// ContainerName returns the Docker container name used by this engine ("caddy" or "nd-proxy").
	ContainerName() string

	// Capabilities reports the feature flags supported by this engine.
	Capabilities() Capabilities

	// StackService returns the root Docker Compose service definition and any top-level
	// named volumes required to run this engine in the NextDeploy stack.
	StackService(opts StackOptions) (service map[string]any, volumes map[string]any, err error)

	// AppLabels returns Docker container labels required on an app service container
	// for the given sites and upstream reference. File-rendered drivers without Docker
	// label discovery return nil or an empty map.
	AppLabels(sites []Site, up UpstreamRef) map[string]string

	// Render converts the desired-state IR into engine configuration files keyed by path
	// relative to the engine's configuration root. Label-only drivers return an empty map.
	Render(st *State) (Files, error)

	// Validate runs the engine's config syntax check against a staged configuration
	// directory before live activation.
	Validate(ctx context.Context, stagedDir string) error

	// Apply activates rendered configuration files atomically, reloads the engine,
	// and rolls back to the previous generation if reload or health verification fails.
	Apply(ctx context.Context, files Files) (ApplyResult, error)

	// Status queries live operational health, version, and metrics from the running proxy.
	Status(ctx context.Context) (Status, error)

	// Logs returns up to the most recent lines from the proxy container output.
	Logs(ctx context.Context, lines int) ([]string, error)

	// Restart restarts the proxy container.
	Restart(ctx context.Context) error
}

// Capabilities describes the feature and operational matrix of a reverse proxy driver.
// It drives switch preflight checks and UI option gating.
type Capabilities struct {
	AutoTLS                 bool `json:"auto_tls"`
	InternalTLS             bool `json:"internal_tls"`
	OnDemandTLS             bool `json:"on_demand_tls"`
	DockerUpstreamDiscovery bool `json:"docker_upstream_discovery"`
	PHPFastCGI              bool `json:"php_fastcgi"`
	BasicAuthBcrypt         bool `json:"basic_auth_bcrypt"`
	RegexRouting            bool `json:"regex_routing"`
	RequireTargetFile       bool `json:"require_target_file"`
	H2IdleTimeout           bool `json:"h2_idle_timeout"`
	WriteTimeout            bool `json:"write_timeout"`
	ReadHeaderTimeout       bool `json:"read_header_timeout"`
	ReadBodyTimeout         bool `json:"read_body_timeout"`
	GracePeriod             bool `json:"grace_period"`
	MaxHeaderSizeHTTP1      bool `json:"max_header_size_http1"`
	TrustedStrict           bool `json:"trusted_strict"`
	LongStreamsOnReload     bool `json:"long_streams_on_reload"`
	WordPressPageCache      bool `json:"wordpress_page_cache"`
	AdminAPI                bool `json:"admin_api"`
	Experimental            bool `json:"experimental"`
}

// RequestCounter is implemented by drivers that can report how many HTTP requests the proxy
// has served since it started. The dashboard turns the difference between two readings into
// the "Site Traffic" rate.
type RequestCounter interface {
	RequestsTotal(ctx context.Context) (uint64, error)
}

// ZoneStats are the running request totals of one proxy server zone (one site).
type ZoneStats struct {
	Requests uint64
	S2xx     uint64
	S3xx     uint64
	S4xx     uint64
	S5xx     uint64
}

// ZoneCounter is implemented by drivers that count requests per site. Keys are the
// engine's own zone names; the panel maps them back to sites with its desired state.
type ZoneCounter interface {
	ZoneCounters(ctx context.Context) (map[string]ZoneStats, error)
}
