package edge

import (
	"path/filepath"
	"strings"
)

// DefaultCaddyEnginePaths returns the canonical Linux paths and service
// metadata for Caddy on an nd-edge Shield node.
func DefaultCaddyEnginePaths() EnginePaths {
	return CaddyEnginePaths(DefaultPaths())
}

// CaddyEnginePaths converts a legacy Paths struct into EnginePaths for Caddy.
func CaddyEnginePaths(p Paths) EnginePaths {
	manualCertsDir := ""
	if p.CaddyNDDir != "" {
		manualCertsDir = filepath.Join(p.CaddyNDDir, "certs")
	}
	return EnginePaths{
		MainConfig:     p.Caddyfile,
		ManagedDir:     p.CaddyNDDir,
		GlobalFile:     p.GlobalOpts,
		CommonFile:     p.CommonCaddy,
		PanelsLink:     p.PanelsLink,
		OriginsLink:    p.OriginsLink,
		UserDir:        p.UserCaddyDir,
		CertsDir:       p.CaddyCertsDir,
		ManualCertsDir: manualCertsDir,
		ServiceUnit:    "caddy.service",
		ServiceUser:    "caddy",
		Binary:         "caddy",
		PanelExt:       ".caddy",
	}
}

// CaddyEngine implements Engine by wrapping the existing Caddyfile renderers
// and CaddyRunner with identical output and behavior.
type CaddyEngine struct {
	paths       EnginePaths
	legacyPaths Paths
	adminAddr   string
	acmeEmail   string
	runner      Runner
}

// NewCaddyEngine constructs a CaddyEngine from paths and an optional CaddyRunner.
func NewCaddyEngine(paths Paths, cr CaddyRunner) *CaddyEngine {
	return NewCaddyEngineWithConfig(paths, nil, cr)
}

// NewCaddyEngineWithConfig constructs a CaddyEngine using cfg for admin
// address, ACME email, and service user metadata.
func NewCaddyEngineWithConfig(paths Paths, cfg *Config, cr CaddyRunner) *CaddyEngine {
	if strings.TrimSpace(paths.Caddyfile) == "" {
		paths = DefaultPaths()
	}
	ep := CaddyEnginePaths(paths)
	adminAddr := DefaultAdminAddr
	acmeEmail := ""
	if cfg != nil {
		if strings.TrimSpace(cfg.AdminAddr) != "" {
			adminAddr = strings.TrimSpace(cfg.AdminAddr)
		}
		acmeEmail = strings.TrimSpace(cfg.ACMEEmail)
		if strings.TrimSpace(cfg.CaddyUser) != "" {
			ep.ServiceUser = strings.TrimSpace(cfg.CaddyUser)
		}
	}
	return &CaddyEngine{
		paths:       ep,
		legacyPaths: paths,
		adminAddr:   adminAddr,
		acmeEmail:   acmeEmail,
		runner:      RunnerFromCaddyRunner(cr, ep.MainConfig, adminAddr),
	}
}

// Name returns EngineCaddy ("caddy").
func (e *CaddyEngine) Name() string {
	return EngineCaddy
}

// Paths returns the Caddy filesystem layout and service metadata.
func (e *CaddyEngine) Paths() EnginePaths {
	return e.paths
}

// RenderGlobal renders global.opts using RenderGlobalOptsForConfig.
func (e *CaddyEngine) RenderGlobal(cfg *Config, cfRanges []string, anyTrustedProxy bool) (map[string]string, error) {
	return map[string]string{
		e.paths.GlobalFile: RenderGlobalOptsForConfig(cfg, cfRanges, anyTrustedProxy),
	}, nil
}

// RenderPanel renders one panel's Caddy configuration using RenderPanelCaddy.
func (e *CaddyEngine) RenderPanel(in RenderPanelInput) (string, error) {
	if in.OriginsLink == "" {
		in.OriginsLink = e.paths.OriginsLink
	}
	return RenderPanelCaddy(in)
}

// RenderCommon renders common.caddy using RenderCommonCaddy.
func (e *CaddyEngine) RenderCommon() (map[string]string, error) {
	return map[string]string{
		e.paths.CommonFile: RenderCommonCaddy(),
	}, nil
}

// RenderManagedRegion renders the nd-edge managed Caddyfile block for MainConfig.
func (e *CaddyEngine) RenderManagedRegion(cfg *Config) string {
	adminAddr := e.adminAddr
	acmeEmail := e.acmeEmail
	if cfg != nil {
		if strings.TrimSpace(cfg.AdminAddr) != "" {
			adminAddr = strings.TrimSpace(cfg.AdminAddr)
		}
		if strings.TrimSpace(cfg.ACMEEmail) != "" {
			acmeEmail = strings.TrimSpace(cfg.ACMEEmail)
		}
	}
	return RenderCaddyfileManagedRegion(e.legacyPaths, adminAddr, acmeEmail, e.paths.PanelsLink, e.paths.UserDir)
}

// Runner returns the generalized Runner backed by CaddyRunner.
func (e *CaddyEngine) Runner() Runner {
	return e.runner
}
