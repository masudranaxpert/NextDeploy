// Package angie implements the proxy.Driver adapter for Angie OSS 1.12+,
// rendering nginx-compatible virtual host, upstream, global, and PHP-FPM
// configurations with Docker container upstream discovery, per-site ACME,
// atomic relative-symlink staging, and read-only OSS status metrics.
package angie

import (
	"context"
	"strings"

	"panel/internal/proxy"
)

const (
	// DriverName is the canonical engine identifier for the Angie driver.
	DriverName = "angie"
	// AngieImage is the pinned Angie OSS Docker image used by StackService and validation.
	AngieImage = "docker.angie.software/angie:1.12.4"
	// DefaultContainerName is the Docker container name for non-Caddy proxy stacks.
	DefaultContainerName = "nd-proxy"
	// DefaultNetworkName is the case-sensitive Docker network name matched by angie.network labels.
	DefaultNetworkName = "NextDeploy"
	// DefaultNetworkKey is the Compose network key attached to the proxy service.
	DefaultNetworkKey = "nextdeploy"
	// DefaultDataDir is the host directory storing staged and active Angie configurations.
	DefaultDataDir = "/data/proxy/angie"
	// DefaultPKISitesDir is the host directory containing per-site internal TLS leaf certs.
	DefaultPKISitesDir = "/data/proxy/pki/sites"
	// DefaultPKIImportedDir is the host directory containing imported Caddy certificates.
	DefaultPKIImportedDir = "/data/proxy/pki/imported"
	// DefaultWorkspacesDir is the host root directory holding user site workspaces.
	DefaultWorkspacesDir = "/data/workspaces"
	// DefaultACMEDirectory is the default ACME v2 directory URL for automatic certificates.
	DefaultACMEDirectory = "https://acme-v02.api.letsencrypt.org/directory"
	// DefaultMaxBodySize is the default client_max_body_size applied to PHP/static sites.
	DefaultMaxBodySize = "5g"
	// containerConfigRoot is where DataDir is bind-mounted inside nd-proxy.
	containerConfigRoot = "/etc/angie/nd"
	// containerLiveConfig is the active config path resolved through the relative sites symlink.
	containerLiveConfig = "/etc/angie/nd/sites/angie.conf"
	// containerStatusURL is the internal loopback OSS status endpoint inside nd-proxy.
	containerStatusURL = "http://127.0.0.1:2019/status/"
)

var _ proxy.Driver = (*Driver)(nil)

// ManualCert references a pre-existing certificate and private key inside the
// Angie container (such as certificates imported during a Caddy migration).
type ManualCert struct {
	CertPath string
	KeyPath  string
}

// Driver implements proxy.Driver for Angie OSS 1.12+.
type Driver struct {
	containerName   string
	networkName     string
	image           string
	statusURL       string
	acmeDirectory   string
	workerProcesses string
	maxBodySize     string
	enableQUIC      bool
	forceZstd       bool
	dataDir         string
	pkiSitesDir     string
	pkiImportedDir  string
	stage           *proxy.StageStore
	importedCerts   map[string]ManualCert
	hostPath        func(string) string
	hooks           RuntimeHooks
}

// New constructs an Angie Driver configured with the supplied options.
func New(opts ...Option) *Driver {
	d := &Driver{
		containerName:   DefaultContainerName,
		networkName:     DefaultNetworkName,
		image:           AngieImage,
		acmeDirectory:   DefaultACMEDirectory,
		workerProcesses: "auto",
		maxBodySize:     DefaultMaxBodySize,
		enableQUIC:      true,
		dataDir:         DefaultDataDir,
		pkiSitesDir:     DefaultPKISitesDir,
		pkiImportedDir:  DefaultPKIImportedDir,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(d)
		}
	}
	if d.containerName == "" {
		d.containerName = DefaultContainerName
	}
	if d.networkName == "" {
		d.networkName = DefaultNetworkName
	}
	if d.image == "" {
		d.image = AngieImage
	}
	if d.acmeDirectory == "" {
		d.acmeDirectory = DefaultACMEDirectory
	}
	if d.workerProcesses == "" {
		d.workerProcesses = "auto"
	}
	if d.maxBodySize == "" {
		d.maxBodySize = DefaultMaxBodySize
	}
	if d.dataDir == "" {
		d.dataDir = DefaultDataDir
	}
	return d
}

// RegisterInto registers a default Angie Driver into reg, or into the global
// proxy registry when reg is nil.
func RegisterInto(reg *proxy.Registry) error {
	d := New()
	if reg == nil {
		return proxy.Register(d)
	}
	return reg.Register(d)
}

func init() {
	_ = RegisterInto(nil)
}

// Name returns the canonical lowercase engine identifier ("angie").
func (d *Driver) Name() string {
	return DriverName
}

// ContainerName returns the Docker container name managed by the Angie driver ("nd-proxy").
func (d *Driver) ContainerName() string {
	if d != nil && strings.TrimSpace(d.containerName) != "" {
		return d.containerName
	}
	return DefaultContainerName
}

// Capabilities reports the verified feature and operational matrix of Angie OSS 1.12.4.
func (d *Driver) Capabilities() proxy.Capabilities {
	return proxy.Capabilities{
		AutoTLS:                 true,
		InternalTLS:             true,
		OnDemandTLS:             false,
		DockerUpstreamDiscovery: true,
		PHPFastCGI:              true,
		BasicAuthBcrypt:         true,
		RegexRouting:            true,
		RequireTargetFile:       true,
		H2IdleTimeout:           true,
		WriteTimeout:            true,
		ReadHeaderTimeout:       true,
		ReadBodyTimeout:         true,
		GracePeriod:             true,
		MaxHeaderSizeHTTP1:      true,
		TrustedStrict:           true,
		LongStreamsOnReload:     true,
		WordPressPageCache:      true,
		AdminAPI:                false,
		Experimental:            false,
	}
}

func contextOrBackground(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}
