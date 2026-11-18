package angie

import (
	"strings"

	"panel/internal/proxy"
)

// Option configures an Angie Driver constructed by New.
type Option func(*Driver)

// WithContainerName overrides the Docker container name managed by this driver.
func WithContainerName(name string) Option {
	return func(d *Driver) {
		if trimmed := strings.TrimSpace(name); trimmed != "" {
			d.containerName = trimmed
		}
	}
}

// WithNetworkName overrides the case-sensitive Docker network name emitted in angie.network labels.
func WithNetworkName(name string) Option {
	return func(d *Driver) {
		if trimmed := strings.TrimSpace(name); trimmed != "" {
			d.networkName = trimmed
		}
	}
}

// WithImage overrides the pinned Angie Docker image used by StackService and dry-run validation.
func WithImage(image string) Option {
	return func(d *Driver) {
		if trimmed := strings.TrimSpace(image); trimmed != "" {
			d.image = trimmed
		}
	}
}

// WithStatusURL overrides the Angie OSS API status endpoint queried directly over HTTP.
func WithStatusURL(statusURL string) Option {
	return func(d *Driver) {
		d.statusURL = strings.TrimSpace(statusURL)
	}
}

// WithACMEDirectory overrides the ACME directory URL used in rendered acme_client directives.
func WithACMEDirectory(dirURL string) Option {
	return func(d *Driver) {
		if trimmed := strings.TrimSpace(dirURL); trimmed != "" {
			d.acmeDirectory = trimmed
		}
	}
}

// WithWorkerProcesses overrides worker_processes in angie.conf (defaults to "auto").
func WithWorkerProcesses(count string) Option {
	return func(d *Driver) {
		if trimmed := strings.TrimSpace(count); trimmed != "" {
			d.workerProcesses = trimmed
		}
	}
}

// WithMaxBodySize overrides client_max_body_size for PHP/static sites (defaults to "5g").
func WithMaxBodySize(size string) Option {
	return func(d *Driver) {
		if trimmed := strings.TrimSpace(size); trimmed != "" {
			d.maxBodySize = trimmed
		}
	}
}

// WithQUIC toggles HTTP/3 QUIC listeners (listen 443 quic;) and Alt-Svc headers on TLS sites.
func WithQUIC(enabled bool) Option {
	return func(d *Driver) {
		d.enableQUIC = enabled
	}
}

// WithZstd forces loading the Angie zstd modules even when no site explicitly requests zstd.
func WithZstd(enabled bool) Option {
	return func(d *Driver) {
		d.forceZstd = enabled
	}
}

// WithDataDir sets the host configuration directory mounted at /etc/angie/nd inside nd-proxy.
func WithDataDir(dir string) Option {
	return func(d *Driver) {
		if trimmed := strings.TrimSpace(dir); trimmed != "" {
			d.dataDir = trimmed
		}
	}
}

// WithPKIDirs overrides the host internal PKI sites and imported-cert directories used for dry-run mounts.
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

// WithStageStore attaches an atomic relative-symlink StageStore used by Apply.
func WithStageStore(stage *proxy.StageStore) Option {
	return func(d *Driver) {
		d.stage = stage
	}
}

// WithImportedCerts configures manual TLS certificate overrides (keyed by lowercase host or site ID)
// used during Caddy-to-Angie migration to avoid hitting ACME rate limits.
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

// WithRuntimeHooks injects runtime command/API hooks so unit tests run without Docker.
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
