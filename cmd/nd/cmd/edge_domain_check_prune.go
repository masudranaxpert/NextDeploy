package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"nd/internal/edge"
	"nd/internal/ui"
)

const (
	pruneServiceFileName = "nd-edge-domain-prune.service"
	pruneTimerFileName   = "nd-edge-domain-prune.timer"
)

type panelHostHint struct {
	Host       string `json:"host"`
	PanelName  string `json:"panel"`
	Issue      string `json:"issue"`
	Suggestion string `json:"suggestion"`
}

func runEdgeDomainCheck(ops *edgeOps, args []string) error {
	if hasHelpFlag(args) {
		printEdgeCommandHelp(ops.stdout, "domain check")
		return nil
	}
	var jsonOut, includeAll bool
	var filters []string
	for _, a := range args {
		switch a {
		case "--json":
			jsonOut = true
		case "--all":
			includeAll = true
		default:
			if strings.HasPrefix(a, "-") {
				return fmt.Errorf("unknown flag: %s", a)
			}
			filters = append(filters, strings.TrimSuffix(strings.ToLower(strings.TrimSpace(a)), "."))
		}
	}

	ctx := context.Background()
	cfg, _ := edge.LoadConfig(ops.paths.ConfigFile)
	manualFile, err := edge.LoadManualDomains(ops.paths.DomainsFile)
	if err != nil {
		return err
	}
	now := ops.now()
	health, _ := edge.LoadDomainsHealth(ops.paths.DomainsHealthFile)

	reports, _, err := runDomainHealthChecks(ctx, ops, cfg, manualFile, health, filters, includeAll, now)
	if err != nil {
		return err
	}
	if ops.paths.DomainsHealthFile != "" {
		_ = edge.SaveDomainsHealth(ops.paths.DomainsHealthFile, health)
	}

	if jsonOut {
		b, _ := json.MarshalIndent(map[string]any{
			"checked_at": now.UTC().Format(time.RFC3339),
			"reports":    reports,
		}, "", "  ")
		fmt.Fprintln(ops.stdout, string(b))
		return nil
	}

	if len(reports) == 0 {
		fmt.Fprintln(ops.stdout, ui.Dim("No domains matched."))
		return nil
	}

	tableRows := make([][]string, 0, len(reports))
	for _, r := range reports {
		src := ui.Dim(r.Source)
		if r.Source == "manual" {
			src = ui.Cyan("manual")
		}
		tr := r.Traffic
		if tr == "unknown" {
			tr = ui.Dim(tr)
		}
		tableRows = append(tableRows, []string{
			ui.Bold(r.Domain),
			src,
			ui.EdgeStatusBadge(r.DNS.Classification),
			ui.EdgeStatusBadge(r.Origin.Status),
			ui.EdgeStatusBadge(r.TLS.Status),
			tr,
		})
	}
	ui.PrintTable(ops.stdout, []string{"DOMAIN", "SOURCE", "DNS", "ORIGIN", "TLS", "TRAFFIC"}, tableRows)
	return nil
}

func runEdgeDomainPrune(ops *edgeOps, args []string) error {
	if hasHelpFlag(args) {
		printEdgeCommandHelp(ops.stdout, "domain prune")
		return nil
	}

	reason := edge.PruneReasonDNS
	grace := edge.DefaultManualPruneGrace
	applyMode := false
	includeAll := false
	yes := false
	jsonOut := false
	scheduleMode := ""
	autoApply := false

	for i := 0; i < len(args); i++ {
		a := args[i]
		name, val, hasEq := strings.Cut(a, "=")
		readVal := func() (string, error) {
			if hasEq {
				return val, nil
			}
			if i+1 >= len(args) {
				return "", fmt.Errorf("%s requires a value", name)
			}
			i++
			return args[i], nil
		}
		switch name {
		case "--dry-run":
			applyMode = false
		case "--apply":
			applyMode = true
		case "--yes", "-y":
			yes = true
		case "--all":
			includeAll = true
		case "--json":
			jsonOut = true
		case "--auto-apply":
			autoApply = true
		case "--reason":
			v, err := readVal()
			if err != nil {
				return err
			}
			reason = strings.ToLower(strings.TrimSpace(v))
			switch reason {
			case edge.PruneReasonDNS, edge.PruneReasonOrigin, edge.PruneReasonAll:
			default:
				return fmt.Errorf("invalid --reason %q (must be dns, origin, or all)", v)
			}
		case "--grace":
			v, err := readVal()
			if err != nil {
				return err
			}
			d, err := parseGraceDuration(v)
			if err != nil {
				return err
			}
			grace = d
		case "--schedule":
			v, err := readVal()
			if err != nil {
				return err
			}
			scheduleMode = strings.ToLower(strings.TrimSpace(v))
			if scheduleMode != "daily" && scheduleMode != "off" {
				return fmt.Errorf("invalid --schedule %q (must be daily or off)", v)
			}
		default:
			return fmt.Errorf("unknown flag: %s", a)
		}
	}

	ctx := context.Background()
	if scheduleMode != "" {
		return configurePruneSchedule(ctx, ops, scheduleMode, autoApply, reason, grace, jsonOut)
	}

	cfg, err := edge.LoadConfig(ops.paths.ConfigFile)
	if err != nil && applyMode {
		return err
	}
	manualFile, err := edge.LoadManualDomains(ops.paths.DomainsFile)
	if err != nil {
		return err
	}
	now := ops.now()
	health, _ := edge.LoadDomainsHealth(ops.paths.DomainsHealthFile)

	reports, panelHints, err := runDomainHealthChecks(ctx, ops, cfg, manualFile, health, nil, includeAll, now)
	if err != nil {
		return err
	}
	if ops.paths.DomainsHealthFile != "" {
		_ = edge.SaveDomainsHealth(ops.paths.DomainsHealthFile, health)
	}

	reportMap := make(map[string]edge.DomainCheckReport, len(reports))
	for _, r := range reports {
		if r.Source == "manual" {
			reportMap[r.Domain] = r
		}
	}

	candidates := edge.EvaluatePruneCandidates(manualFile.Domains, reportMap, health, reason, grace, now)

	if !applyMode {
		if jsonOut {
			b, _ := json.MarshalIndent(map[string]any{
				"dry_run":          true,
				"reason":           reason,
				"grace":            grace.String(),
				"candidates":       candidates,
				"panel_domain_hints": panelHints,
			}, "", "  ")
			fmt.Fprintln(ops.stdout, string(b))
			return nil
		}
		if len(candidates) == 0 {
			fmt.Fprintf(ops.stdout, "%s\n", ui.Green(fmt.Sprintf("Dry run: no manual domains qualify for pruning (reason=%s, grace=%s, requires >=2 bad checks).", reason, grace)))
		} else {
			fmt.Fprintf(ops.stdout, "%s\n\n", ui.HiYellow(fmt.Sprintf("Dry run: %d manual domain(s) would be pruned (reason=%s, grace=%s):", len(candidates), reason, grace)))
			tableRows := make([][]string, 0, len(candidates))
			for _, c := range candidates {
				tableRows = append(tableRows, []string{
					ui.Bold(c.Entry.Domain),
					ui.Red(c.ReasonTag),
					strconv.Itoa(c.CheckCount),
					c.BadSince.Format("2006-01-02 15:04"),
					c.Detail,
				})
			}
			ui.PrintTable(ops.stdout, []string{"DOMAIN", "REASON", "CHECKS", "BAD SINCE", "DETAIL"}, tableRows)
			fmt.Fprintf(ops.stdout, "\n%s\n", ui.Dim("Re-run with 'nd edge domain prune --apply' to move these to trash (restorable for 30 days)."))
		}
		if len(panelHints) > 0 {
			fmt.Fprintf(ops.stdout, "\n%s\n", ui.Yellow("Panel domains with issues (read-only; manage on the panel):"))
			for _, ph := range panelHints {
				fmt.Fprintf(ops.stdout, "  %-28s panel=%-12s issue=%s -> %s\n", ui.Bold(ph.Host), ui.Cyan(ph.PanelName), ui.Red(ph.Issue), ui.Dim(ph.Suggestion))
			}
		}
		return nil
	}

	if len(candidates) == 0 {
		if jsonOut {
			b, _ := json.MarshalIndent(map[string]any{"dry_run": false, "pruned": []any{}}, "", "  ")
			fmt.Fprintln(ops.stdout, string(b))
			return nil
		}
		fmt.Fprintln(ops.stdout, "Nothing to prune.")
		return nil
	}

	names := make([]string, len(candidates))
	for i, c := range candidates {
		names[i] = c.Entry.Domain
	}
	ok, err := confirmDestructiveAction(ops, yes, fmt.Sprintf("Prune %d manual domain(s) (%s) to trash?", len(names), strings.Join(names, ", ")))
	if err != nil {
		return err
	}
	if !ok {
		fmt.Fprintln(ops.stdout, "Aborted.")
		return nil
	}

	lock, err := edge.AcquireLockBlocking(ctx, edgeConfigLockPath(ops.paths))
	if err != nil {
		return err
	}
	defer func() { _ = lock.Release() }()

	candidate := edge.CloneManualDomainsFile(manualFile)
	trashFile, err := edge.LoadManualTrash(ops.paths.DomainsTrashFile, now)
	if err != nil {
		return err
	}

	var pruned []edge.ManualDomainEntry
	for _, c := range candidates {
		idx, entry := edge.FindManualDomain(candidate, c.Entry.Domain)
		if idx < 0 || entry == nil {
			continue
		}
		pruned = append(pruned, *entry)
		edge.MoveToTrash(trashFile, *entry, c.ReasonTag, now)
		candidate.Domains = append(candidate.Domains[:idx], candidate.Domains[idx+1:]...)
	}

	reloadedViaDaemon, err := applyCandidateManualDomains(ctx, ops, cfg, candidate, now)
	if err != nil {
		return err
	}
	if err := edge.SaveManualDomains(ops.paths.DomainsFile, candidate); err != nil {
		return err
	}
	_ = edge.SaveManualTrash(ops.paths.DomainsTrashFile, trashFile, now)
	cleanupAfterRemoval(ops, cfg, candidate, pruned, now)
	if reloadedViaDaemon {
		signalDaemonReload(ctx, ops)
	}

	if jsonOut {
		b, _ := json.MarshalIndent(map[string]any{
			"dry_run": false,
			"pruned":  candidates,
		}, "", "  ")
		fmt.Fprintln(ops.stdout, string(b))
		return nil
	}
	ui.Success("Pruned %d manual domain(s) to trash (restore within 30d with 'nd edge domain restore <domain>')", len(pruned))
	return nil
}

func runDomainHealthChecks(
	ctx context.Context,
	ops *edgeOps,
	cfg *edge.Config,
	manualFile *edge.ManualDomainsFile,
	health *edge.DomainsHealthStore,
	filters []string,
	includePanels bool,
	now time.Time,
) ([]edge.DomainCheckReport, []panelHostHint, error) {
	filterSet := make(map[string]struct{}, len(filters))
	for _, f := range filters {
		filterSet[f] = struct{}{}
	}
	matchesFilter := func(domain string) bool {
		if len(filterSet) == 0 {
			return true
		}
		_, ok := filterSet[strings.ToLower(domain)]
		return ok
	}

	cfIPs := edge.LoadOrInitCFIPStore(ops.paths.CFIPsFile)
	panelStates := map[string]*edge.PanelState{}
	if cfg != nil {
		panelStates = loadPanelStatesForConfig(ops, cfg, now)
	}
	shieldIPs := collectAllShieldIPs(ops, cfg, panelStates)

	var ep edge.EnginePaths
	if cfg != nil {
		ep = ops.paths.EnginePathsFor(cfg.EngineName())
	} else {
		ep = ops.paths.EnginePathsFor(edge.EngineCaddy)
	}
	probe := edge.NewCertProbe(ep.CertsDir, ops.systemd)
	probe.ManualCertsDir = ep.ManualCertsDir
	dnsGate := edge.NewDNSGate(edge.DNSGateOptions{
		CFIPs:    cfIPs,
		Resolver: ops.dnsResolver,
	})

	inspectDNS := func(host string) edge.FreshDNSInspection {
		if ops.dnsInspectFn != nil {
			return ops.dnsInspectFn(ctx, cfg, shieldIPs, host)
		}
		return dnsGate.InspectHostFresh(ctx, host, shieldIPs)
	}
	checkOrigin := func(entry *edge.ManualDomainEntry, urls []string, hostHdr, sni string, noVerify bool) edge.OriginHealthResult {
		if ops.originCheckFn != nil {
			return ops.originCheckFn(ctx, entry, urls, hostHdr, sni, noVerify)
		}
		if entry != nil {
			return edge.CheckOriginHealth(ctx, *entry, 5*time.Second)
		}
		return edge.CheckOriginHealthTargets(ctx, urls, hostHdr, sni, noVerify, 5*time.Second)
	}
	checkTLS := func(entry *edge.ManualDomainEntry, host string) edge.TLSHealthResult {
		if ops.tlsCheckFn != nil {
			return ops.tlsCheckFn(ctx, probe, entry, host, now)
		}
		return edge.CheckHostTLSHealth(ctx, probe, entry, host, now)
	}

	var reports []edge.DomainCheckReport
	if manualFile != nil {
		for i := range manualFile.Domains {
			entry := manualFile.Domains[i]
			if !matchesFilter(entry.Domain) {
				// Also match if any alias was passed explicitly.
				matched := false
				for _, h := range entry.AllHosts() {
					if matchesFilter(h) {
						matched = true
						break
					}
				}
				if !matched {
					continue
				}
			}

			dnsRes := inspectDNS(entry.Domain)
			origRes := checkOrigin(&entry, nil, "", "", entry.Origin.NoVerify)
			tlsRes := checkTLS(&entry, entry.Domain)

			var stat edge.DomainHealthStat
			if health != nil {
				stat = health.Record(entry.Domain, dnsRes.Classification, origRes.Status, now)
			}

			reports = append(reports, edge.DomainCheckReport{
				Domain:         entry.Domain,
				Source:         "manual",
				Enabled:        entry.Enabled,
				TLSMode:        entry.TLS,
				DNS:            dnsRes,
				Origin:         origRes,
				TLS:            tlsRes,
				Traffic:        "unknown",
				BadDNSCount:    stat.BadDNSCount,
				BadDNSSince:    stat.FirstBadDNSAt,
				BadOriginCount: stat.BadOriginCount,
				BadOriginSince: stat.FirstBadOriginAt,
			})
		}
	}

	userSites, _ := edge.ScanUserConfigSites(ops.paths, engineNameFromConfig(cfg))
	if len(userSites) > 0 {
		seenUserPrimary := make(map[string]struct{})
		for _, s := range userSites {
			key := s.Primary + "|" + filepath.Base(s.File)
			if _, dup := seenUserPrimary[key]; dup {
				continue
			}
			matched := matchesFilter(s.Primary)
			if !matched {
				for _, h := range s.Hosts {
					if matchesFilter(h) {
						matched = true
						break
					}
				}
			}
			if !matched {
				continue
			}
			seenUserPrimary[key] = struct{}{}

			dnsRes := inspectDNS(s.Primary)
			origRes := edge.OriginHealthResult{Status: "unknown"}
			if urls := normalizeUserUpstreamURLs(s.Upstreams); len(urls) > 0 {
				origRes = checkOrigin(nil, urls, s.Primary, "", false)
			}
			tlsRes := checkTLS(nil, s.Primary)

			reports = append(reports, edge.DomainCheckReport{
				Domain:  s.Primary,
				Source:  "user:" + filepath.Base(s.File),
				Enabled: true,
				TLSMode: s.TLS,
				DNS:     dnsRes,
				Origin:  origRes,
				TLS:     tlsRes,
				Traffic: "unknown",
			})
		}
	}

	var hints []panelHostHint
	if includePanels && cfg != nil {
		for _, p := range cfg.Panels {
			st := panelStates[p.PanelID]
			if st == nil || st.LastGood == nil {
				continue
			}
			var panelURLs []string
			port := st.LastGood.Origin.Port
			if port <= 0 {
				port = 443
			}
			for _, ipStr := range st.LastGood.Origin.IPs {
				if addr, err := netip.ParseAddr(strings.TrimSpace(ipStr)); err == nil {
					panelURLs = append(panelURLs, "https://"+net.JoinHostPort(addr.Unmap().String(), strconv.Itoa(port)))
				}
			}
			seen := make(map[string]struct{})
			for _, rt := range st.LastGood.Routes {
				for _, h := range rt.Hosts {
					norm := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(h), "."))
					if norm == "" || !matchesFilter(norm) {
						continue
					}
					if _, dup := seen[norm]; dup {
						continue
					}
					seen[norm] = struct{}{}

					dnsRes := inspectDNS(norm)
					origRes := edge.OriginHealthResult{Status: "unknown"}
					if len(panelURLs) > 0 {
						origRes = checkOrigin(nil, panelURLs, norm, norm, st.LastGood.Origin.CAStatus != edge.CAStatusOK)
					}
					tlsRes := checkTLS(nil, norm)

					reports = append(reports, edge.DomainCheckReport{
						Domain:  norm,
						Source:  p.PanelName,
						Enabled: true,
						TLSMode: "auto",
						DNS:     dnsRes,
						Origin:  origRes,
						TLS:     tlsRes,
						Traffic: "unknown",
					})

					if edge.IsBadDNSClassification(dnsRes.Classification) {
						hints = append(hints, panelHostHint{
							Host:       norm,
							PanelName:  p.PanelName,
							Issue:      "dns:" + dnsRes.Classification,
							Suggestion: fmt.Sprintf("managed by panel %q (run 'nd shield reassign ...' on that panel or remove the domain there)", p.PanelName),
						})
					} else if edge.IsBadOriginStatus(origRes.Status) {
						hints = append(hints, panelHostHint{
							Host:       norm,
							PanelName:  p.PanelName,
							Issue:      "origin:" + origRes.Status,
							Suggestion: fmt.Sprintf("managed by panel %q (check origin health or reassign on panel)", p.PanelName),
						})
					}
				}
			}
		}
	}

	sort.Slice(reports, func(i, j int) bool {
		if reports[i].Domain != reports[j].Domain {
			return reports[i].Domain < reports[j].Domain
		}
		return reports[i].Source < reports[j].Source
	})
	sort.Slice(hints, func(i, j int) bool { return hints[i].Host < hints[j].Host })
	return reports, hints, nil
}

func collectAllShieldIPs(ops *edgeOps, cfg *edge.Config, panelStates map[string]*edge.PanelState) []netip.Addr {
	seen := make(map[netip.Addr]struct{})
	var out []netip.Addr
	addStrs := func(strs []string) {
		for _, a := range edge.ParseIPStrings(strs) {
			u := a.Unmap()
			if _, ok := seen[u]; !ok {
				seen[u] = struct{}{}
				out = append(out, u)
			}
		}
	}
	if cfg != nil {
		addStrs(cfg.ShieldIPs)
	}
	for _, st := range panelStates {
		if st != nil && st.LastGood != nil && st.LastGood.Node.IP != "" {
			addStrs([]string{st.LastGood.Node.IP})
		}
	}
	if ops.localPublicIPs != nil {
		v4, v6 := ops.localPublicIPs()
		addStrs(v4)
		addStrs(v6)
	}
	return out
}

func normalizeUserUpstreamURLs(raw []string) []string {
	var urls []string
	seen := make(map[string]struct{})
	for _, u := range raw {
		t := strings.TrimSpace(u)
		if t == "" || strings.HasPrefix(t, "unix/") || strings.HasPrefix(t, "unix:") {
			continue
		}
		if strings.HasPrefix(t, "h2c://") {
			t = "http://" + strings.TrimPrefix(t, "h2c://")
		} else if !strings.Contains(t, "://") {
			t = "http://" + t
		}
		if _, ok := seen[t]; !ok {
			seen[t] = struct{}{}
			urls = append(urls, t)
		}
	}
	return urls
}

func parseGraceDuration(raw string) (time.Duration, error) {
	s := strings.ToLower(strings.TrimSpace(raw))
	if s == "0" || s == "0s" || s == "0d" {
		return 0, nil
	}
	if strings.HasSuffix(s, "d") {
		num := strings.TrimSuffix(s, "d")
		days, err := strconv.Atoi(strings.TrimSpace(num))
		if err != nil || days < 0 {
			return 0, fmt.Errorf("invalid --grace %q", raw)
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || d < 0 {
		return 0, fmt.Errorf("invalid --grace %q (expected e.g. 7d or 24h)", raw)
	}
	return d, nil
}

func configurePruneSchedule(ctx context.Context, ops *edgeOps, mode string, autoApply bool, reason string, grace time.Duration, jsonOut bool) error {
	unitDir := filepath.Dir(ops.unitPath)
	if unitDir == "" || unitDir == "." {
		unitDir = "/etc/systemd/system"
	}
	servicePath := filepath.Join(unitDir, pruneServiceFileName)
	timerPath := filepath.Join(unitDir, pruneTimerFileName)

	if mode == "off" {
		if ops.hasSystemd != nil && ops.hasSystemd() && ops.runCmd != nil {
			_, _ = ops.runCmd(ctx, "systemctl", "disable", "--now", pruneTimerFileName)
		}
		_ = os.Remove(timerPath)
		_ = os.Remove(servicePath)
		if ops.hasSystemd != nil && ops.hasSystemd() && ops.runCmd != nil {
			_, _ = ops.runCmd(ctx, "systemctl", "daemon-reload")
		}
		if jsonOut {
			b, _ := json.MarshalIndent(map[string]any{"schedule": "off"}, "", "  ")
			fmt.Fprintln(ops.stdout, string(b))
			return nil
		}
		ui.Success("Disabled scheduled domain prune timer (%s)", pruneTimerFileName)
		return nil
	}

	bin := ops.binaryPath
	if strings.TrimSpace(bin) == "" {
		bin = edgeDefaultBinaryPath
	}
	modeFlag := "--dry-run"
	if autoApply {
		modeFlag = "--apply --yes"
	}
	execCmd := fmt.Sprintf("%s edge domain prune %s --reason %s --grace %s --json", bin, modeFlag, reason, grace)
	serviceUnit := fmt.Sprintf(`[Unit]
Description=NextDeploy Edge manual domain prune check
After=network-online.target

[Service]
Type=oneshot
ExecStart=%s
StandardOutput=journal
StandardError=journal
`, execCmd)

	timerUnit := `[Unit]
Description=Daily NextDeploy Edge manual domain prune check

[Timer]
OnCalendar=daily
Persistent=true
RandomizedDelaySec=15m

[Install]
WantedBy=timers.target
`
	if err := os.MkdirAll(unitDir, 0o755); err != nil {
		return err
	}
	if err := edge.WriteFileAtomic(servicePath, []byte(serviceUnit), 0o644); err != nil {
		return err
	}
	if err := edge.WriteFileAtomic(timerPath, []byte(timerUnit), 0o644); err != nil {
		return err
	}
	if ops.hasSystemd != nil && ops.hasSystemd() && ops.runCmd != nil {
		if out, err := ops.runCmd(ctx, "systemctl", "daemon-reload"); err != nil {
			return fmt.Errorf("systemctl daemon-reload: %w (%s)", err, strings.TrimSpace(out))
		}
		if out, err := ops.runCmd(ctx, "systemctl", "enable", "--now", pruneTimerFileName); err != nil {
			return fmt.Errorf("systemctl enable %s: %w (%s)", pruneTimerFileName, err, strings.TrimSpace(out))
		}
	}

	if jsonOut {
		b, _ := json.MarshalIndent(map[string]any{
			"schedule":     "daily",
			"auto_apply":   autoApply,
			"service_path": servicePath,
			"timer_path":   timerPath,
		}, "", "  ")
		fmt.Fprintln(ops.stdout, string(b))
		return nil
	}
	ui.Success("Installed daily domain prune timer (%s; auto-apply=%v)", timerPath, autoApply)
	return nil
}
