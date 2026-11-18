package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"nd/internal/edge"
)

func detectProxyVersionForEngine(ctx context.Context, ops *edgeOps, engineName string) string {
	switch strings.ToLower(strings.TrimSpace(engineName)) {
	case edge.EngineAngie:
		ep := ops.paths.EnginePathsFor(edge.EngineAngie)
		if ver, err := runnerForEngine(ops, edge.EngineAngie, ep).Version(ctx); err == nil {
			return ver
		}
		return ""
	case edge.EngineFerron:
		ep := ops.paths.EnginePathsFor(edge.EngineFerron)
		if ver, err := runnerForEngine(ops, edge.EngineFerron, ep).Version(ctx); err == nil {
			return ver
		}
		return ""
	default:
		return detectCaddyVersionWithOps(ctx, ops)
	}
}

func runDoctorForEngine(ctx context.Context, ops *edgeOps, cfg *edge.Config, add func(name, status, detail string)) {
	engineName := edge.EngineCaddy
	if cfg != nil {
		engineName = cfg.EngineName()
	} else if _, err := os.Stat(edgeFerronInstalledMarker(ops.paths)); err == nil {
		engineName = edge.EngineFerron
	} else if _, err := os.Stat(edgeAngieInstalledMarker(ops.paths)); err == nil {
		engineName = edge.EngineAngie
	}

	if engineName == edge.EngineAngie || engineName == edge.EngineFerron {
		runDoctorForManagedEngine(ctx, ops, engineName, add)
		return
	}
	runDoctorForCaddy(ctx, ops, cfg, add)
}

func runDoctorForCaddy(ctx context.Context, ops *edgeOps, cfg *edge.Config, add func(name, status, detail string)) {
	if caddyVer := detectCaddyVersionWithOps(ctx, ops); caddyVer != "" {
		add("caddy-binary", "PASS", caddyVer)
	} else {
		add("caddy-binary", "FAIL", "caddy binary not found or failed to run")
	}

	if ops.systemd != nil {
		if active, detail, _ := ops.systemd.IsActive(ctx, "caddy.service"); active {
			add("caddy.service", "PASS", "active")
		} else {
			add("caddy.service", "FAIL", detail)
		}
		if active, detail, _ := ops.systemd.IsActive(ctx, "nd-edge.service"); active {
			add("nd-edge.service", "PASS", "active")
		} else {
			add("nd-edge.service", "WARN", detail)
		}
	}

	adminAddr := edge.DefaultAdminAddr
	if cfg != nil && cfg.AdminAddr != "" {
		adminAddr = cfg.AdminAddr
	}
	if ops.caddy != nil {
		if err := ops.caddy.CheckAdmin(ctx, adminAddr); err != nil {
			add("caddy-admin", "FAIL", fmt.Sprintf("%s unreachable: %v", adminAddr, err))
		} else {
			add("caddy-admin", "PASS", fmt.Sprintf("%s reachable", adminAddr))
		}
	}

	if rawCF, err := os.ReadFile(ops.paths.Caddyfile); err != nil {
		add("Caddyfile", "FAIL", err.Error())
	} else {
		cfText := string(rawCF)
		if strings.Contains(cfText, legacyCatchAllStartMarker) || strings.Contains(cfText, "/api/v1/edge/verify-domain") {
			add("caddy-directives", "FAIL", "legacy catch-all or NextDeploy on_demand_tls directives still present")
		} else if !strings.Contains(cfText, edge.ManagedStartMarker) || !strings.Contains(cfText, edge.ManagedEndMarker) {
			add("caddy-directives", "FAIL", "nd-edge managed markers missing from Caddyfile")
		} else if ops.caddy != nil {
			if valOut, valErr := ops.caddy.Validate(ctx, ops.paths.Caddyfile); valErr != nil {
				add("caddy-directives", "FAIL", fmt.Sprintf("caddy validate failed: %v %s", valErr, valOut))
			} else {
				add("caddy-directives", "PASS", "managed markers present and caddy validate passed")
			}
		} else {
			add("caddy-directives", "PASS", "managed markers present")
		}
	}
}

func runDoctorForManagedEngine(ctx context.Context, ops *edgeOps, engineName string, add func(name, status, detail string)) {
	ep := ops.paths.EnginePathsFor(engineName)
	r := runnerForEngine(ops, engineName, ep)

	if ver, err := r.Version(ctx); err == nil && ver != "" {
		add(engineName+"-binary", "PASS", ver)
	} else {
		add(engineName+"-binary", "FAIL", fmt.Sprintf("%s binary not found or failed to run", engineName))
	}

	if ops.systemd != nil {
		if active, detail, _ := ops.systemd.IsActive(ctx, ep.ServiceUnit); active {
			add(ep.ServiceUnit, "PASS", "active")
		} else {
			add(ep.ServiceUnit, "FAIL", detail)
		}
		if active, detail, _ := ops.systemd.IsActive(ctx, "nd-edge.service"); active {
			add("nd-edge.service", "PASS", "active")
		} else {
			add("nd-edge.service", "WARN", detail)
		}
	}

	if err := r.CheckAlive(ctx); err != nil {
		add(engineName+"-alive", "FAIL", err.Error())
	} else {
		add(engineName+"-alive", "PASS", engineName+" alive")
	}

	mainName := filepath.Base(ep.MainConfig)
	if rawMain, err := os.ReadFile(ep.MainConfig); err != nil {
		add(mainName, "FAIL", err.Error())
	} else if !strings.Contains(string(rawMain), "user/*.conf") || !strings.Contains(string(rawMain), "panels/*.conf") {
		add(engineName+"-config", "FAIL", "nd-edge include directives missing from "+mainName)
	} else if valOut, valErr := r.Validate(ctx, ep.MainConfig); valErr != nil {
		add(engineName+"-config", "FAIL", fmt.Sprintf("%s validate failed: %v %s", engineName, valErr, valOut))
	} else {
		add(engineName+"-config", "PASS", engineName+" validate passed")
	}
}

func uninstallAngieEdge(ctx context.Context, ops *edgeOps, all bool) {
	uninstallNonCaddyEdge(ctx, ops, edge.EngineAngie, all)
}

func uninstallNonCaddyEdge(ctx context.Context, ops *edgeOps, engineName string, all bool) {
	ep := ops.paths.EnginePathsFor(engineName)
	marker := edgeInstalledMarkerForEngine(ops.paths, engineName)
	installedByND := false
	if _, err := os.Stat(marker); err == nil {
		installedByND = true
	}
	managedBanner := fmt.Sprintf("Managed by nd edge (%s)", engineName)

	if rawMain, err := os.ReadFile(ep.MainConfig); err == nil {
		if strings.Contains(string(rawMain), managedBanner) {
			_ = os.Remove(ep.MainConfig)
			if installedByND {
				_, _ = ops.runCmd(ctx, "systemctl", "disable", "--now", engineName)
			}
		}
	} else if installedByND {
		_, _ = ops.runCmd(ctx, "systemctl", "disable", "--now", engineName)
	}

	if engineName == edge.EngineFerron {
		rootConf := ferronRootConfigPath(ep)
		if rawRoot, rErr := os.ReadFile(rootConf); rErr == nil && strings.Contains(string(rawRoot), managedBanner) {
			_ = os.Remove(rootConf)
		}
		unitDir := filepath.Dir(ops.unitPath)
		_ = os.Remove(filepath.Join(unitDir, "ferron.service.d", "nd-edge.conf"))
		_ = os.Remove(filepath.Join(unitDir, "ferron.service.d"))
		if installedByND {
			_ = os.Remove(filepath.Join(unitDir, "ferron.service"))
		}
		_, _ = ops.runCmd(ctx, "systemctl", "daemon-reload")
	}

	if all && ep.CertsDir != "" {
		_ = os.RemoveAll(ep.CertsDir)
	}
	if ep.ManagedDir != "" {
		_ = os.RemoveAll(ep.ManagedDir)
	}
}
