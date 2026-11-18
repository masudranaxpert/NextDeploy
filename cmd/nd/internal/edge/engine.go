package edge

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

const (
	// EngineCaddy selects the default Caddy proxy engine.
	EngineCaddy = "caddy"
	// EngineAngie selects the Angie proxy engine.
	EngineAngie = "angie"
	// EngineFerron selects the experimental Ferron proxy engine.
	EngineFerron = "ferron"
)

// EnginePaths holds the filesystem layout, systemd unit, service user, binary,
// and file extension used by an edge proxy Engine.
type EnginePaths struct {
	MainConfig     string
	ManagedDir     string
	GlobalFile     string
	CommonFile     string
	PanelsLink     string
	OriginsLink    string
	UserDir        string
	CertsDir       string
	ManualCertsDir string
	ServiceUnit    string
	ServiceUser    string
	Binary         string
	PanelExt       string
}

// PanelGenDir returns the generation directory path for rendered panel files.
func (p EnginePaths) PanelGenDir(gen int) string {
	return filepath.Join(p.ManagedDir, fmt.Sprintf("panels.gen-%06d", gen))
}

// OriginGenDir returns the generation directory path for origin CA PEM files.
func (p EnginePaths) OriginGenDir(gen int) string {
	return filepath.Join(p.ManagedDir, fmt.Sprintf("origins.gen-%06d", gen))
}

// Engine abstracts proxy-specific configuration rendering, paths, and runtime
// validation/reload control for nd-edge.
type Engine interface {
	Name() string // caddy | angie | ferron
	Paths() EnginePaths
	RenderGlobal(cfg *Config, cfRanges []string, anyTrustedProxy bool) (map[string]string, error)
	RenderPanel(in RenderPanelInput) (string, error)
	RenderCommon() (map[string]string, error)
	Runner() Runner
}

// Runner generalizes CaddyRunner across proxy engines.
type Runner interface {
	Validate(ctx context.Context, mainConfig string) (string, error)
	Reload(ctx context.Context) (time.Duration, error)
	CheckAlive(ctx context.Context) error
	Version(ctx context.Context) (string, error)
}

type versionProvider interface {
	Version(ctx context.Context) (string, error)
}

type caddyRunnerAdapter struct {
	caddy      CaddyRunner
	mainConfig string
	adminAddr  string
}

// RunnerFromCaddyRunner adapts a legacy CaddyRunner into the generic Runner
// interface. If cr already implements Runner, it is returned directly.
func RunnerFromCaddyRunner(cr CaddyRunner, mainConfig, adminAddr string) Runner {
	if cr == nil {
		cr = &ExecCaddyRunner{}
	}
	if adapter, ok := cr.(*runnerCaddyAdapter); ok {
		return adapter.runner
	}
	if strings.TrimSpace(mainConfig) == "" {
		mainConfig = DefaultPaths().Caddyfile
	}
	if strings.TrimSpace(adminAddr) == "" {
		adminAddr = DefaultAdminAddr
	}
	return &caddyRunnerAdapter{
		caddy:      cr,
		mainConfig: mainConfig,
		adminAddr:  adminAddr,
	}
}

func (a *caddyRunnerAdapter) Validate(ctx context.Context, mainConfig string) (string, error) {
	if strings.TrimSpace(mainConfig) == "" {
		mainConfig = a.mainConfig
	}
	return a.caddy.Validate(ctx, mainConfig)
}

func (a *caddyRunnerAdapter) Reload(ctx context.Context) (time.Duration, error) {
	return a.caddy.Reload(ctx, a.mainConfig, a.adminAddr)
}

func (a *caddyRunnerAdapter) CheckAlive(ctx context.Context) error {
	return a.caddy.CheckAdmin(ctx, a.adminAddr)
}

func (a *caddyRunnerAdapter) Version(ctx context.Context) (string, error) {
	if vp, ok := a.caddy.(versionProvider); ok {
		return vp.Version(ctx)
	}
	return (&ExecCaddyRunner{}).Version(ctx)
}

type runnerCaddyAdapter struct {
	runner Runner
}

// CaddyRunnerFromRunner adapts a generic Runner into the legacy CaddyRunner
// interface for backward compatibility with existing Applier/daemon callers.
func CaddyRunnerFromRunner(r Runner) CaddyRunner {
	if r == nil {
		return &ExecCaddyRunner{}
	}
	if adapter, ok := r.(*caddyRunnerAdapter); ok {
		return adapter.caddy
	}
	return &runnerCaddyAdapter{runner: r}
}

func (a *runnerCaddyAdapter) Validate(ctx context.Context, configPath string) (string, error) {
	return a.runner.Validate(ctx, configPath)
}

func (a *runnerCaddyAdapter) Reload(ctx context.Context, _ string, _ string) (time.Duration, error) {
	return a.runner.Reload(ctx)
}

func (a *runnerCaddyAdapter) CheckAdmin(ctx context.Context, _ string) error {
	return a.runner.CheckAlive(ctx)
}

// IsValidEngineName reports whether name is a recognized edge proxy engine.
func IsValidEngineName(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case EngineCaddy, EngineAngie, EngineFerron:
		return true
	default:
		return false
	}
}

// NewEngine constructs an Engine by name (defaulting empty string to caddy).
// When rootDir is non-empty, engine paths are rooted inside rootDir.
func NewEngine(name string, cfg *Config, rootDir string) (Engine, error) {
	paths := DefaultPaths()
	if rootDir != "" {
		paths = NewPaths(rootDir)
	}
	return NewEngineForPaths(name, paths, cfg, nil)
}

// NewEngineForPaths constructs an Engine using paths (honoring rooted test
// layouts) and optional CaddyRunner when selecting the Caddy engine.
func NewEngineForPaths(name string, paths Paths, cfg *Config, cr CaddyRunner) (Engine, error) {
	normalized := strings.ToLower(strings.TrimSpace(name))
	if normalized == "" {
		normalized = EngineCaddy
	}
	switch normalized {
	case EngineCaddy:
		return NewCaddyEngineWithConfig(paths, cfg, cr), nil
	case EngineAngie:
		ep := paths.EnginePathsFor(EngineAngie)
		return NewAngieEngineWithConfig(ep, cfg, nil), nil
	case EngineFerron:
		ep := paths.EnginePathsFor(EngineFerron)
		return NewFerronEngineWithConfig(ep, cfg, nil), nil
	default:
		return nil, fmt.Errorf("unknown proxy engine %q", name)
	}
}
