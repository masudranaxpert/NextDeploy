// Package caddy implements the proxy.Driver adapter for Caddy 2
// (caddy-docker-proxy), delegating label and root-compose generation to
// internal/caddy with zero behavior change.
package caddy

import (
	"context"
	"strings"
	"time"

	icaddy "panel/internal/caddy"
	"panel/internal/proxy"
)

// DriverName is the canonical engine identifier for the Caddy driver.
const DriverName = "caddy"

var _ proxy.Driver = (*Driver)(nil)

// Option configures a Caddy Driver created by New.
type Option func(*Driver)

// WithAdminURL overrides the Caddy Admin API endpoint (e.g. "http://127.0.0.1:2019" or "off").
func WithAdminURL(adminURL string) Option {
	return func(d *Driver) {
		d.adminURL = strings.TrimSpace(adminURL)
	}
}

// WithContainerName overrides the Docker container name managed by this driver.
func WithContainerName(name string) Option {
	return func(d *Driver) {
		if trimmed := strings.TrimSpace(name); trimmed != "" {
			d.containerName = trimmed
		}
	}
}

// WithRuntimeHooks injects custom runtime callbacks for testing or custom execution environments.
func WithRuntimeHooks(hooks RuntimeHooks) Option {
	return func(d *Driver) {
		d.hooks = hooks
	}
}

// Driver implements proxy.Driver for Caddy 2 using Docker container labels.
type Driver struct {
	adminURL      string
	containerName string
	hooks         RuntimeHooks
}

// New constructs a Caddy Driver configured with the supplied options.
// By default it targets container "caddy" and admin API "http://caddy:2019".
func New(opts ...Option) *Driver {
	d := &Driver{
		adminURL:      icaddy.DefaultAdminAPI,
		containerName: icaddy.CaddyContainerName,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(d)
		}
	}
	if d.adminURL == "" {
		d.adminURL = icaddy.DefaultAdminAPI
	}
	if d.containerName == "" {
		d.containerName = icaddy.CaddyContainerName
	}
	return d
}

// RegisterInto registers a default Caddy Driver in reg, or in the global proxy
// registry when reg is nil.
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

// Name returns the canonical engine identifier ("caddy").
func (d *Driver) Name() string {
	return DriverName
}

// ContainerName returns the Docker container name used by Caddy ("caddy" by default).
func (d *Driver) ContainerName() string {
	if d != nil && strings.TrimSpace(d.containerName) != "" {
		return d.containerName
	}
	return icaddy.CaddyContainerName
}

// Capabilities reports the full feature matrix supported by Caddy 2.
func (d *Driver) Capabilities() proxy.Capabilities {
	return proxy.Capabilities{
		AutoTLS:                 true,
		InternalTLS:             true,
		OnDemandTLS:             true,
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
		AdminAPI:                true,
		Experimental:            false,
	}
}

// Render returns an empty file map because Caddy is configured dynamically via Docker labels.
func (d *Driver) Render(_ *proxy.State) (proxy.Files, error) {
	return proxy.Files{}, nil
}

// Validate checks staged configuration when a custom validator hook is installed.
// Because Caddy is label-driven and produces no staged files, validation succeeds immediately by default.
func (d *Driver) Validate(ctx context.Context, stagedDir string) error {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	if d != nil && d.hooks.Validate != nil {
		return d.hooks.Validate(ctx, stagedDir)
	}
	return nil
}

// Apply invokes the configured label apply hook or returns a no-op ApplyResult
// because container compose orchestration manages Caddy labels externally.
func (d *Driver) Apply(ctx context.Context, files proxy.Files) (proxy.ApplyResult, error) {
	start := time.Now()
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return proxy.ApplyResult{
				Driver:    DriverName,
				AppliedAt: time.Now().UTC(),
			}, err
		}
	}
	if d != nil && d.hooks.Apply != nil {
		res, err := d.hooks.Apply(ctx, files)
		if res.Driver == "" {
			res.Driver = DriverName
		}
		if res.AppliedAt.IsZero() {
			res.AppliedAt = time.Now().UTC()
		}
		return res, err
	}
	return proxy.ApplyResult{
		Driver:    DriverName,
		Changed:   false,
		Reloaded:  false,
		Duration:  time.Since(start),
		Message:   "caddy is label-driven; compose orchestration manages labels",
		AppliedAt: time.Now().UTC(),
	}, nil
}
