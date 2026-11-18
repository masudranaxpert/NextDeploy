// Package ferron implements the experimental proxy.Driver adapter for Ferron 3,
// rendering ferronconf global, virtual host, reverse-proxy, PHP-FPM, static, and
// redirect configurations with logical_dns upstreams, conditional match/if path
// routing, atomic generation staging, and Admin API lifecycle management.
package ferron

import (
	"context"
	"strings"

	"panel/internal/proxy"
)

const (
	// DriverName is the canonical engine identifier for the Ferron driver.
	DriverName = "ferron"
	// FerronImage is the pinned Ferron 3 Docker image used by StackService and validation.
	FerronImage = "ferronserver/ferron:3.0.0-rc.11-alpine"
	// DefaultContainerName is the Docker container name for non-Caddy proxy stacks.
	DefaultContainerName = "nd-proxy"
	// DefaultNetworkName is the case-sensitive Docker network name attached to proxy containers.
	DefaultNetworkName = "NextDeploy"
	// DefaultNetworkKey is the Compose network key attached to the proxy service.
	DefaultNetworkKey = "nextdeploy"
	// DefaultDataDir is the host directory storing staged and active Ferron configurations.
	DefaultDataDir = "/data/proxy/ferron"
	// DefaultPKISitesDir is the host directory containing per-site internal TLS leaf certs.
	DefaultPKISitesDir = "/data/proxy/pki/sites"
	// DefaultPKIImportedDir is the host directory containing imported certificates from Caddy migration.
	DefaultPKIImportedDir = "/data/proxy/pki/imported"
	// DefaultWorkspacesDir is the host root directory holding user site workspaces and PHP sockets.
	DefaultWorkspacesDir = "/data/workspaces"
	// DefaultACMEDirectory is the default ACME v2 directory URL for automatic certificates.
	DefaultACMEDirectory = "https://acme-v02.api.letsencrypt.org/directory"
	// DefaultACMECacheDir is the container directory where Ferron caches ACME accounts and certs.
	DefaultACMECacheDir = "/var/cache/ferron-acme"
	// DefaultAdminURL is the container-loopback Admin API base URL used by Ferron.
	DefaultAdminURL = "http://127.0.0.1:8081"
	// DefaultAdminTokenEnv is the environment variable name carrying the Admin API bearer token.
	DefaultAdminTokenEnv = "ND_FERRON_ADMIN_TOKEN"

	containerConfigRoot     = "/etc/ferron/nd"
	containerLiveConfig     = "/etc/ferron/nd/sites/ferron.conf"
	containerPKISitesDir    = "/etc/nd-pki/sites"
	containerPKIImportedDir = "/etc/nd-pki/imported"
)

var _ proxy.Driver = (*Driver)(nil)

// Driver implements proxy.Driver for Ferron 3.
type Driver struct {
	containerName     string
	networkName       string
	image             string
	adminURL          string
	adminToken        string
	adminTokenEnv     string
	acmeDirectory     string
	acmeCacheDir      string
	onDemandAskURL    string
	dataDir           string
	pkiSitesDir       string
	pkiImportedDir    string
	stage             *proxy.StageStore
	importedCerts     map[string]ManualCert
	strictUnsupported bool
	hostPath          func(string) string
	hooks             RuntimeHooks
}

// New constructs a Ferron Driver configured with the supplied options.
func New(opts ...Option) *Driver {
	d := &Driver{
		containerName:  DefaultContainerName,
		networkName:    DefaultNetworkName,
		image:          FerronImage,
		adminURL:       DefaultAdminURL,
		adminTokenEnv:  DefaultAdminTokenEnv,
		acmeDirectory:  DefaultACMEDirectory,
		acmeCacheDir:   DefaultACMECacheDir,
		dataDir:        DefaultDataDir,
		pkiSitesDir:    DefaultPKISitesDir,
		pkiImportedDir: DefaultPKIImportedDir,
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
		d.image = FerronImage
	}
	if d.adminURL == "" {
		d.adminURL = DefaultAdminURL
	}
	if d.adminTokenEnv == "" {
		d.adminTokenEnv = DefaultAdminTokenEnv
	}
	if d.acmeDirectory == "" {
		d.acmeDirectory = DefaultACMEDirectory
	}
	if d.acmeCacheDir == "" {
		d.acmeCacheDir = DefaultACMECacheDir
	}
	if d.dataDir == "" {
		d.dataDir = DefaultDataDir
	}
	if d.pkiSitesDir == "" {
		d.pkiSitesDir = DefaultPKISitesDir
	}
	if d.pkiImportedDir == "" {
		d.pkiImportedDir = DefaultPKIImportedDir
	}
	return d
}

// RegisterInto registers a default Ferron Driver into reg, or into the global
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

// Name returns the canonical lowercase engine identifier ("ferron").
func (d *Driver) Name() string {
	return DriverName
}

// ContainerName returns the Docker container name managed by the Ferron driver ("nd-proxy").
func (d *Driver) ContainerName() string {
	if d != nil && strings.TrimSpace(d.containerName) != "" {
		return d.containerName
	}
	return DefaultContainerName
}

// Capabilities reports the verified feature and operational matrix of Ferron 3.0.0-rc.11.
func (d *Driver) Capabilities() proxy.Capabilities {
	return proxy.Capabilities{
		AutoTLS:                 true,
		InternalTLS:             true,
		OnDemandTLS:             true,
		DockerUpstreamDiscovery: false,
		PHPFastCGI:              true,
		BasicAuthBcrypt:         true,
		RegexRouting:            true,
		RequireTargetFile:       false,
		H2IdleTimeout:           false,
		WriteTimeout:            false,
		ReadHeaderTimeout:       false,
		ReadBodyTimeout:         false,
		GracePeriod:             false,
		MaxHeaderSizeHTTP1:      false,
		TrustedStrict:           false,
		LongStreamsOnReload:     true,
		WordPressPageCache:      false, // no way to test that the cached file exists before serving it
		AdminAPI:                true,
		Experimental:            true,
	}
}

// AppLabels returns nil because Ferron routes to containers by service/container
// DNS with logical_dns true rather than reading Docker labels.
func (d *Driver) AppLabels(_ []proxy.Site, _ proxy.UpstreamRef) map[string]string {
	return nil
}

func (d *Driver) manualCertFor(siteID string, hosts []string) (ManualCert, bool) {
	if d == nil || len(d.importedCerts) == 0 {
		return ManualCert{}, false
	}
	if key := strings.ToLower(strings.TrimSpace(siteID)); key != "" {
		if mc, ok := d.importedCerts[key]; ok {
			return mc, true
		}
	}
	for _, h := range hosts {
		key := strings.ToLower(strings.TrimSpace(h))
		if mc, ok := d.importedCerts[key]; ok {
			return mc, true
		}
	}
	return ManualCert{}, false
}

func contextOrBackground(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}
