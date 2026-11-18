package cmd

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"nd/internal/edge"
	"nd/internal/ui"
)

type switchProxyOpts struct {
	targetEngine string
	experimental bool
	yes          bool
}

func parseSwitchProxyArgs(args []string) (switchProxyOpts, error) {
	var o switchProxyOpts
	for _, a := range args {
		switch {
		case a == "--experimental":
			o.experimental = true
		case a == "--yes" || a == "-y":
			o.yes = true
		case strings.HasPrefix(a, "-"):
			return o, fmt.Errorf("unknown flag: %s", a)
		default:
			if o.targetEngine != "" {
				return o, fmt.Errorf("unexpected extra argument %q", a)
			}
			o.targetEngine = strings.TrimSpace(a)
		}
	}
	if o.targetEngine == "" {
		return o, errors.New("target engine required: nd edge switch-proxy <caddy|angie|ferron>")
	}
	validated, err := validateProxyInstallChoice(o.targetEngine, o.experimental, false)
	if err != nil {
		return o, err
	}
	o.targetEngine = validated
	return o, nil
}

func runEdgeSwitchProxy(ops *edgeOps, args []string) error {
	if hasHelpFlag(args) {
		fmt.Fprintln(ops.stdout, "Usage: nd edge switch-proxy <caddy|angie|ferron> [--experimental] [--yes]")
		return nil
	}
	opts, err := parseSwitchProxyArgs(args)
	if err != nil {
		return err
	}
	if ops.getEUID() != 0 {
		return errors.New("nd edge switch-proxy must be run as root")
	}
	if ops.goos != "linux" {
		return fmt.Errorf("unsupported OS %q: nd edge switch-proxy only supports Linux", ops.goos)
	}
	if !ops.hasSystemd() {
		return errors.New("systemd is required on the Shield VPS")
	}

	ctx := context.Background()
	cfgLock, err := edge.AcquireLockBlocking(ctx, edgeConfigLockPath(ops.paths))
	if err != nil {
		return err
	}
	defer func() { _ = cfgLock.Release() }()

	cfg, err := edge.LoadConfig(ops.paths.ConfigFile)
	if err != nil {
		return fmt.Errorf("load config (run 'nd edge install' first): %w", err)
	}

	sourceEngine := cfg.EngineName()
	targetEngine := opts.targetEngine
	if sourceEngine == targetEngine {
		ui.Success("Edge node is already using proxy engine %q", targetEngine)
		return nil
	}

	sourcePaths := ops.paths.EnginePathsFor(sourceEngine)
	targetPaths := ops.paths.EnginePathsFor(targetEngine)

	// Pause nd-edge daemon during cutover if active so no concurrent apply races.
	daemonWasActive := false
	if ops.systemd != nil {
		if active, _, _ := ops.systemd.IsActive(ctx, "nd-edge.service"); active {
			daemonWasActive = true
			_, _ = ops.runCmd(ctx, "systemctl", "stop", "nd-edge")
		}
	}

	var daemonLock *edge.FileLock
	if l, lErr := edge.AcquireLock(ops.paths.LockFile); lErr == nil {
		daemonLock = l
		defer func() { _ = daemonLock.Release() }()
	} else if errors.Is(lErr, edge.ErrLockHeld) {
		if daemonWasActive {
			_, _ = ops.runCmd(ctx, "systemctl", "start", "nd-edge")
		}
		return errors.New("nd-edge daemon lock is held; stop nd-edge before switching engines")
	}

	// 1. Install target engine if missing (do not start it yet).
	if _, err := ops.lookPath(targetPaths.Binary); err != nil {
		if err := installEnginePackageForSwitch(ctx, ops, targetEngine); err != nil {
			if daemonWasActive {
				_, _ = ops.runCmd(ctx, "systemctl", "start", "nd-edge")
			}
			return err
		}
	}

	if targetEngine == edge.EngineAngie {
		warnAngieOpenSSL(ctx, ops)
	}

	// 2. Import Caddy (or prior engine manual) certs so ACME rate limits aren't hit.
	var importedCount int
	if sourceEngine == edge.EngineCaddy && targetEngine != edge.EngineCaddy {
		imported, impErr := edge.ImportCaddyCertsToDir(ops.paths.CaddyCertsDir, filepath.Dir(ops.paths.Caddyfile), targetPaths.ManualCertsDir, ops.now())
		if impErr != nil {
			if daemonWasActive {
				_, _ = ops.runCmd(ctx, "systemctl", "start", "nd-edge")
			}
			return fmt.Errorf("import caddy certificates: %w", impErr)
		}
		importedCount = len(imported)
		fmt.Fprintf(ops.stdout, "  Imported %d certificate(s) from Caddy into %s\n", importedCount, targetPaths.ManualCertsDir)
	} else if sourceEngine != edge.EngineCaddy && targetEngine != edge.EngineCaddy && sourcePaths.ManualCertsDir != "" {
		if imported, impErr := edge.ImportCaddyCertsToDir(sourcePaths.ManualCertsDir, sourcePaths.CertsDir, targetPaths.ManualCertsDir, ops.now()); impErr == nil && len(imported) > 0 {
			importedCount = len(imported)
			fmt.Fprintf(ops.stdout, "  Carried over %d certificate(s) from %s into %s\n", importedCount, sourceEngine, targetPaths.ManualCertsDir)
		}
	}

	// 3. Render and validate target configuration from cached ApplyMeta + PanelState files.
	meta, _ := edge.LoadApplyMeta(ops.paths.ApplyMeta)
	if meta == nil {
		meta = &edge.ApplyMeta{}
	}
	targetCfgCopy := *cfg
	targetCfgCopy.ProxyEngine = targetEngine
	targetEng, err := buildEdgeEngineWithOps(ops, targetEngine, &targetCfgCopy)
	if err != nil {
		if daemonWasActive {
			_, _ = ops.runCmd(ctx, "systemctl", "start", "nd-edge")
		}
		return err
	}

	nextGen, activeHosts, err := stageAndValidateSwitchConfig(ctx, ops, targetEng, &targetCfgCopy, meta)
	if err != nil {
		if daemonWasActive {
			_, _ = ops.runCmd(ctx, "systemctl", "start", "nd-edge")
		}
		return fmt.Errorf("switch-proxy preflight validation failed (source %s left running): %w", sourceEngine, err)
	}

	// 4. Cutover: stop source service, update nd-edge unit, start target service.
	rollback := func(cause error) error {
		_, _ = ops.runCmd(ctx, "systemctl", "stop", targetPaths.ServiceUnit)
		_ = edge.WriteFileAtomic(ops.unitPath, []byte(renderSystemdUnitForEngine(ops.binaryPath, sourcePaths.ServiceUnit)), 0o644)
		_, _ = ops.runCmd(ctx, "systemctl", "daemon-reload")
		if _, startOldErr := ops.runCmd(ctx, "systemctl", "start", sourcePaths.ServiceUnit); startOldErr != nil {
			cause = fmt.Errorf("%w (rollback start %s failed: %v)", cause, sourcePaths.ServiceUnit, startOldErr)
		}
		meta.LastApplyError = fmt.Sprintf("switch-proxy %s->%s rolled back: %v", sourceEngine, targetEngine, cause)
		_ = edge.SaveApplyMeta(ops.paths.ApplyMeta, meta)
		if daemonWasActive {
			_, _ = ops.runCmd(ctx, "systemctl", "start", "nd-edge")
		}
		return fmt.Errorf("switch-proxy failed and rolled back to %s: %w", sourceEngine, cause)
	}

	if out, stopErr := ops.runCmd(ctx, "systemctl", "stop", sourcePaths.ServiceUnit); stopErr != nil {
		_ = out
		if daemonWasActive {
			_, _ = ops.runCmd(ctx, "systemctl", "start", "nd-edge")
		}
		return fmt.Errorf("stop %s: %w: %s", sourcePaths.ServiceUnit, stopErr, strings.TrimSpace(out))
	}

	if err := edge.WriteFileAtomic(ops.unitPath, []byte(renderSystemdUnitForEngine(ops.binaryPath, targetPaths.ServiceUnit)), 0o644); err != nil {
		return rollback(err)
	}
	if targetEngine == edge.EngineAngie {
		if err := writeAngieSystemdDropIn(ops); err != nil {
			return rollback(fmt.Errorf("angie systemd drop-in: %w", err))
		}
	}
	if out, err := ops.runCmd(ctx, "systemctl", "daemon-reload"); err != nil {
		return rollback(fmt.Errorf("systemctl daemon-reload: %w: %s", err, strings.TrimSpace(out)))
	}
	if out, err := ops.runCmd(ctx, "systemctl", "enable", "--now", targetPaths.ServiceUnit); err != nil {
		return rollback(fmt.Errorf("start %s: %w: %s", targetPaths.ServiceUnit, err, strings.TrimSpace(out)))
	}

	// 5. Probe target engine liveness + active HTTPS hosts.
	if err := targetEng.Runner().CheckAlive(ctx); err != nil {
		return rollback(fmt.Errorf("%s CheckAlive failed: %w", targetEngine, err))
	}
	if ops.systemd != nil {
		if active, detail, _ := ops.systemd.IsActive(ctx, targetPaths.ServiceUnit); !active {
			return rollback(fmt.Errorf("%s not active after start: %s", targetPaths.ServiceUnit, detail))
		}
	}
	for _, host := range activeHosts {
		if err := probeSwitchHostReady(ctx, ops, host); err != nil {
			return rollback(fmt.Errorf("HTTPS probe failed for %s: %w", host, err))
		}
	}

	// 6. Commit engine change in config.json + disable old service + resume nd-edge.
	_, _ = ops.runCmd(ctx, "systemctl", "disable", sourcePaths.ServiceUnit)
	cfg.ProxyEngine = targetEngine
	if err := edge.SaveConfig(ops.paths.ConfigFile, cfg); err != nil {
		return rollback(fmt.Errorf("persist proxy_engine=%s: %w", targetEngine, err))
	}
	nowUTC := ops.now().UTC()
	meta.CurrentGen = nextGen
	meta.LastApplyAt = &nowUTC
	meta.LastApplyError = ""
	_ = edge.SaveApplyMeta(ops.paths.ApplyMeta, meta)

	if panelsDir, rErr := edge.ReadDirLink(targetPaths.PanelsLink); rErr == nil && panelsDir != "" {
		if rebuilt, rbErr := edge.RebuildClaimsFromDir(panelsDir, targetPaths.PanelExt, nowUTC); rbErr == nil {
			_ = edge.SaveClaims(ops.paths.ClaimsFile, rebuilt)
		}
	}

	if daemonLock != nil {
		_ = daemonLock.Release()
		daemonLock = nil
	}
	if daemonWasActive {
		_, _ = ops.runCmd(ctx, "systemctl", "start", "nd-edge")
	}

	ui.Success("Switched edge proxy %s -> %s (imported_certs=%d, probed_hosts=%d)", sourceEngine, targetEngine, importedCount, len(activeHosts))
	return nil
}

func installEnginePackageForSwitch(ctx context.Context, ops *edgeOps, targetEngine string) error {
	info, err := parseOSReleaseInfo(ops.osReleasePath)
	if err != nil {
		return err
	}
	switch targetEngine {
	case edge.EngineAngie:
		if err := installAngiePackage(ctx, ops, info); err != nil {
			return err
		}
		_ = edge.WriteFileAtomic(edgeAngieInstalledMarker(ops.paths), []byte("1\n"), 0o600)
		return nil
	case edge.EngineFerron:
		if err := installFerronPackage(ctx, ops, info); err != nil {
			return err
		}
		_ = edge.WriteFileAtomic(edgeFerronInstalledMarker(ops.paths), []byte("1\n"), 0o600)
		return nil
	case edge.EngineCaddy:
		if info.Family != "debian" && info.Family != "rhel" {
			return fmt.Errorf("automatic Caddy package install is not supported on %s", info.ID)
		}
		if err := installCaddyPackage(ctx, ops, info.Family); err != nil {
			return err
		}
		_ = edge.WriteFileAtomic(edgeCaddyInstalledMarker(ops.paths), []byte("1\n"), 0o600)
		return nil
	default:
		return fmt.Errorf("cannot install unsupported engine %q", targetEngine)
	}
}

func stageAndValidateSwitchConfig(ctx context.Context, ops *edgeOps, targetEng edge.Engine, cfg *edge.Config, meta *edge.ApplyMeta) (int, []string, error) {
	ep := targetEng.Paths()
	for _, d := range []string{
		filepath.Dir(ep.MainConfig),
		ep.ManagedDir,
		ep.UserDir,
		ep.CertsDir,
		ep.ManualCertsDir,
	} {
		if strings.TrimSpace(d) == "" {
			continue
		}
		if err := os.MkdirAll(d, 0o750); err != nil {
			return 0, nil, fmt.Errorf("mkdir %s: %w", d, err)
		}
	}

	placeholderName, placeholderBody, healthName, healthBody := engineInstallTemplates(targetEng)

	userPlaceholder := filepath.Join(ep.UserDir, placeholderName)
	if _, err := os.Stat(userPlaceholder); errors.Is(err, os.ErrNotExist) {
		if err := edge.WriteFileAtomic(userPlaceholder, []byte(placeholderBody), 0o644); err != nil {
			return 0, nil, err
		}
	}

	now := ops.now()
	panels, activeHosts, anyTrustedProxy, err := renderCachedPanelsForEngine(ops.paths, targetEng, cfg, now)
	if err != nil {
		return 0, nil, err
	}

	cfStore := edge.LoadOrInitCFIPStore(ops.paths.CFIPsFile)
	globalFiles, err := targetEng.RenderGlobal(cfg, cfStore.Ranges(), anyTrustedProxy)
	if err != nil {
		return 0, nil, err
	}
	commonFiles, err := targetEng.RenderCommon()
	if err != nil {
		return 0, nil, err
	}

	nextGen := meta.CurrentGen + 1
	if nextGen <= 0 {
		nextGen = 1
	}
	panelDir := ep.PanelGenDir(nextGen)
	originDir := ep.OriginGenDir(nextGen)
	if err := os.MkdirAll(panelDir, 0o750); err != nil {
		return 0, nil, err
	}
	if err := os.MkdirAll(originDir, 0o750); err != nil {
		return 0, nil, err
	}

	if err := edge.WriteFileAtomic(filepath.Join(panelDir, placeholderName), []byte(placeholderBody), 0o640); err != nil {
		return 0, nil, err
	}
	hasActiveRoutes := false
	for _, p := range panels {
		if edge.HasNonCommentCaddyContent(string(p.CaddyBytes)) {
			hasActiveRoutes = true
		}
		if err := edge.WriteFileAtomic(filepath.Join(panelDir, p.PanelID+ep.PanelExt), p.CaddyBytes, 0o640); err != nil {
			return 0, nil, err
		}
		if len(p.PEMBytes) > 0 {
			if err := edge.WriteFileAtomic(filepath.Join(originDir, p.PanelID+".pem"), p.PEMBytes, 0o640); err != nil {
				return 0, nil, err
			}
		}
	}
	if !hasActiveRoutes {
		if err := edge.WriteFileAtomic(filepath.Join(panelDir, healthName), []byte(healthBody), 0o640); err != nil {
			return 0, nil, err
		}
	}

	for path, content := range globalFiles {
		if err := edge.WriteFileAtomic(path, []byte(content), 0o644); err != nil {
			return 0, nil, err
		}
	}
	for path, content := range commonFiles {
		if err := edge.WriteFileAtomic(path, []byte(content), 0o644); err != nil {
			return 0, nil, err
		}
	}
	if ce, ok := targetEng.(*edge.CaddyEngine); ok {
		region := ce.RenderManagedRegion(cfg)
		if existing, rErr := os.ReadFile(ep.MainConfig); rErr == nil {
			outside, _ := stripManagedRegion(string(existing))
			if strings.TrimSpace(outside) != "" {
				region = region + "\n" + strings.TrimLeft(outside, "\r\n")
			}
		}
		if err := edge.WriteFileAtomic(ep.MainConfig, []byte(region), 0o644); err != nil {
			return 0, nil, err
		}
	}

	if err := edge.SwapDirLinkAtomic(ep.OriginsLink, originDir); err != nil {
		return 0, nil, err
	}
	if err := edge.SwapDirLinkAtomic(ep.PanelsLink, panelDir); err != nil {
		return 0, nil, err
	}

	applyEnginePermsAndSELinux(ops, ep)

	if targetEng.Name() == edge.EngineFerron {
		if err := ensureFerronSystemIntegration(ops, ep); err != nil {
			return 0, nil, err
		}
	}

	if valOut, valErr := targetEng.Runner().Validate(ctx, ep.MainConfig); valErr != nil {
		return 0, nil, fmt.Errorf("%w: %s", valErr, valOut)
	}
	return nextGen, activeHosts, nil
}

func renderCachedPanelsForEngine(paths edge.Paths, eng edge.Engine, cfg *edge.Config, now time.Time) ([]edge.PanelRenderArtifact, []string, bool, error) {
	ep := eng.Paths()
	var artifacts []edge.PanelRenderArtifact
	hostSet := make(map[string]struct{})
	anyTrustedProxy := false

	for _, p := range cfg.Panels {
		st, err := edge.LoadPanelState(paths.StateFile(p.PanelID), p.PanelID, p.PanelName, now)
		if err != nil || st == nil || st.LastGood == nil {
			continue
		}
		snap := st.LastGood
		var originIPs []netip.Addr
		for _, rawIP := range snap.Origin.IPs {
			if addr, ipErr := edge.ValidateOriginIP(rawIP, edge.ParseIPStrings(cfg.ShieldIPs)); ipErr == nil {
				originIPs = append(originIPs, addr)
			}
		}
		if len(originIPs) == 0 {
			continue
		}

		var routes []edge.EdgeRoute
		for _, rt := range snap.Routes {
			var keptHosts []string
			for _, rawH := range rt.Hosts {
				norm, verr := edge.NormalizeAndValidateHost(rawH, cfg.ShieldHostname)
				if verr != nil {
					continue
				}
				if len(st.HostStates) > 0 {
					if hs, ok := st.HostStates[norm]; ok {
						switch hs.State {
						case edge.StateActive, edge.StateIssuing, edge.StateDNSDrift, edge.StateHeldMassDelete, edge.StateCertError:
						default:
							continue
						}
					}
				}
				keptHosts = append(keptHosts, norm)
				hostSet[norm] = struct{}{}
			}
			if len(keptHosts) > 0 {
				r2 := rt
				r2.Hosts = keptHosts
				if r2.TrustedProxy {
					anyTrustedProxy = true
				}
				routes = append(routes, r2)
			}
		}

		rendered, err := eng.RenderPanel(edge.RenderPanelInput{
			PanelID:     p.PanelID,
			PanelName:   p.PanelName,
			ETag:        st.LastGoodETag,
			OriginIPs:   originIPs,
			OriginPort:  snap.Origin.Port,
			CAStatus:    snap.Origin.CAStatus,
			OriginsLink: ep.OriginsLink,
			Routes:      routes,
		})
		if err != nil {
			return nil, nil, false, err
		}
		var pemBytes []byte
		if snap.Origin.CAPEM != "" {
			pemBytes = []byte(snap.Origin.CAPEM)
		}
		artifacts = append(artifacts, edge.PanelRenderArtifact{
			PanelID:    p.PanelID,
			CaddyBytes: []byte(rendered),
			PEMBytes:   pemBytes,
		})
	}

	activeHosts := make([]string, 0, len(hostSet))
	for h := range hostSet {
		activeHosts = append(activeHosts, h)
	}
	sort.Strings(activeHosts)
	return artifacts, activeHosts, anyTrustedProxy, nil
}

func probeSwitchHost(ctx context.Context, ops *edgeOps, host string) error {
	if ops.probeSwitchHost != nil {
		return ops.probeSwitchHost(ctx, host)
	}
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	dialer := &net.Dialer{Timeout: 4 * time.Second}
	tr := &http.Transport{
		DialContext: func(dctx context.Context, network, _ string) (net.Conn, error) {
			return dialer.DialContext(dctx, network, "127.0.0.1:443")
		},
		TLSClientConfig: &tls.Config{
			ServerName: host,
			MinVersion: tls.VersionTLS12,
		},
	}
	defer tr.CloseIdleConnections()

	client := &http.Client{Timeout: 5 * time.Second, Transport: tr}
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, "https://"+host+"/", nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}
