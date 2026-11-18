package ferron

import (
	"strings"

	"panel/internal/proxy"
)

// ManualCert references a pre-existing PEM certificate chain and private key inside
// the Ferron container (such as certificates imported during engine switching).
type ManualCert struct {
	CertPath string
	KeyPath  string
}

// Option configures a Ferron Driver constructed by New.
type Option func(*Driver)

// WithContainerName overrides the Docker container name managed by this driver.
func WithContainerName(name string) Option {
	return func(d *Driver) {
		if trimmed := strings.TrimSpace(name); trimmed != "" {
			d.containerName = trimmed
		}
	}
}

// WithNetworkName overrides the Docker network name referenced by StackService.
func WithNetworkName(name string) Option {
	return func(d *Driver) {
		if trimmed := strings.TrimSpace(name); trimmed != "" {
			d.networkName = trimmed
		}
	}
}

// WithImage overrides the pinned Ferron Docker image used by StackService and validation.
func WithImage(image string) Option {
	return func(d *Driver) {
		if trimmed := strings.TrimSpace(image); trimmed != "" {
			d.image = trimmed
		}
	}
}

// WithAdminURL overrides the Ferron Admin API base URL (defaults to http://127.0.0.1:8081).
func WithAdminURL(adminURL string) Option {
	return func(d *Driver) {
		if trimmed := strings.TrimSpace(adminURL); trimmed != "" {
			d.adminURL = strings.TrimRight(trimmed, "/")
		}
	}
}

// WithAdminToken configures a literal Admin API bearer token for direct HTTP requests.
func WithAdminToken(token string) Option {
	return func(d *Driver) {
		d.adminToken = strings.TrimSpace(token)
	}
}

// WithAdminTokenEnv overrides the environment variable name interpolated into ferron.conf.
func WithAdminTokenEnv(envName string) Option {
	return func(d *Driver) {
		if trimmed := strings.TrimSpace(envName); trimmed != "" {
			d.adminTokenEnv = trimmed
		}
	}
}

// WithACMEDirectory overrides the ACME directory URL used in rendered tls acme blocks.
func WithACMEDirectory(dirURL string) Option {
	return func(d *Driver) {
		if trimmed := strings.TrimSpace(dirURL); trimmed != "" {
			d.acmeDirectory = trimmed
		}
	}
}

// WithACMECacheDir overrides the ACME cache directory path rendered into tls acme blocks.
func WithACMECacheDir(dir string) Option {
	return func(d *Driver) {
		if trimmed := strings.TrimSpace(dir); trimmed != "" {
			d.acmeCacheDir = trimmed
		}
	}
}

// WithDataDir overrides the host configuration directory mounted at /etc/ferron/nd inside nd-proxy.
func WithDataDir(dir string) Option {
	return func(d *Driver) {
		if trimmed := strings.TrimSpace(dir); trimmed != "" {
			d.dataDir = trimmed
		}
	}
}

// WithPKIDir overrides the host internal PKI sites directory mounted at /etc/nd-pki/sites inside nd-proxy.
func WithPKIDir(sitesDir string) Option {
	return func(d *Driver) {
		if trimmed := strings.TrimSpace(sitesDir); trimmed != "" {
			d.pkiSitesDir = trimmed
		}
	}
}

// WithPKIDirs overrides the host internal PKI sites and imported-cert directories.
func WithPKIDirs(sitesDir, importedDir string) Option {
	return func(d *Driver) {
		if trimmed := strings.TrimSpace(sitesDir); trimmed != "" {
			d.pkiSitesDir = trimmed
		}
		if trimmed := strings.TrimSpace(importedDir); trimmed != "" {
			d.pkiImportedDir = trimmed
		}
	}
}

// WithOnDemandAskURL sets the approval URL emitted as on_demand_ask for OnDemandTLS sites.
func WithOnDemandAskURL(askURL string) Option {
	return func(d *Driver) {
		d.onDemandAskURL = strings.TrimSpace(askURL)
	}
}

// WithStageStore attaches an atomic relative-symlink StageStore used by Apply.
func WithStageStore(stage *proxy.StageStore) Option {
	return func(d *Driver) {
		d.stage = stage
	}
}

// WithImportedCerts configures manual TLS certificate overrides keyed by lowercase host or site ID.
func WithImportedCerts(certs map[string]ManualCert) Option {
	return func(d *Driver) {
		if len(certs) == 0 {
			d.importedCerts = nil
			return
		}
		cp := make(map[string]ManualCert, len(certs))
		for k, v := range certs {
			key := strings.ToLower(strings.TrimSpace(k))
			if key != "" && strings.TrimSpace(v.CertPath) != "" && strings.TrimSpace(v.KeyPath) != "" {
				cp[key] = ManualCert{
					CertPath: strings.TrimSpace(v.CertPath),
					KeyPath:  strings.TrimSpace(v.KeyPath),
				}
			}
		}
		d.importedCerts = cp
	}
}

// WithStrictUnsupported toggles whether Render errors on unsupported features like RequireTargetFile.
// When false (default), unsupported rules are omitted with an explicit comment block.
func WithStrictUnsupported(strict bool) Option {
	return func(d *Driver) {
		d.strictUnsupported = strict
	}
}

// WithRuntimeHooks injects command/API hooks so unit tests run without Docker.
func WithRuntimeHooks(hooks RuntimeHooks) Option {
	return func(d *Driver) {
		d.hooks = hooks
	}
}

// WithHostPath maps a panel-side path to the path the Docker daemon sees. It is needed when
// the panel runs in a container whose data lives elsewhere on the host (e.g. ./data:/data),
// because `docker run -v` sources are resolved on the host.
func WithHostPath(fn func(string) string) Option {
	return func(d *Driver) {
		d.hostPath = fn
	}
}

// toHost translates a panel path for use as a `docker run -v` source.
func (d *Driver) toHost(p string) string {
	if d == nil || d.hostPath == nil || p == "" {
		return p
	}
	if out := d.hostPath(p); out != "" {
		return out
	}
	return p
}
