package edge

import (
	"context"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	panelAdmissionLimit  = 50
	panelAdmissionWindow = 10 * time.Minute
	shieldAdmissionLimit = 250
	shieldAdmissionWindow = 3 * time.Hour
	caddyStartCooldown   = 10 * time.Minute
)

// ReconcileOutcome summarizes one full reconciliation pass.
type ReconcileOutcome struct {
	ApplyResult
	CaddyRunning     bool
	CaddyStatusError string
	SkippedEmptyBoot bool
}

// Reconciler executes the §3.2–§3.5 reconciliation pipeline across all panels.
type Reconciler struct {
	mu                    sync.Mutex
	paths                 Paths
	dnsGate               *DNSGate
	applier               *Applier
	probe                 *CertProbe
	systemd               SystemdRunner
	cfIPs                 *CFIPStore
	lastCaddyStartAttempt time.Time
}

// NewReconciler constructs a Reconciler.
func NewReconciler(paths Paths, dnsGate *DNSGate, applier *Applier, probe *CertProbe, systemd SystemdRunner, cfIPs *CFIPStore) *Reconciler {
	if cfIPs == nil {
		cfIPs = NewCFIPStore()
	}
	if applier == nil {
		applier = NewApplier(paths, nil)
	}
	if probe == nil {
		probe = NewCertProbe(paths.CaddyCertsDir, systemd)
	}
	if dnsGate == nil {
		dnsGate = NewDNSGate(DNSGateOptions{CFIPs: cfIPs, CachePath: paths.DNSCacheFile})
	}
	return &Reconciler{
		paths:   paths,
		dnsGate: dnsGate,
		applier: applier,
		probe:   probe,
		systemd: systemd,
		cfIPs:   cfIPs,
	}
}

// ReconcileOnce runs a single full reconciliation across cfg.Panels, updating
// panelStates, claims, meta, and Caddy generation files atomically.
func (r *Reconciler) ReconcileOnce(
	ctx context.Context,
	cfg *Config,
	panelStates map[string]*PanelState,
	claims *ClaimsStore,
	meta *ApplyMeta,
	force bool,
	probeCerts bool,
	now time.Time,
) (ReconcileOutcome, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var out ReconcileOutcome
	if cfg == nil {
		return out, fmt.Errorf("nil config")
	}
	if claims == nil {
		claims = NewClaimsStore()
	}
	if meta == nil {
		meta = &ApplyMeta{}
	}

	// 1. Check Caddy service status (E-BOOT-4, E-CADDY-1).
	out.CaddyRunning = true
	if r.systemd != nil {
		active, detail, _ := r.systemd.IsActive(ctx, "caddy.service")
		out.CaddyRunning = active
		out.CaddyStatusError = detail
		if !active {
			if r.lastCaddyStartAttempt.IsZero() || now.Sub(r.lastCaddyStartAttempt) >= caddyStartCooldown {
				r.lastCaddyStartAttempt = now
				if err := r.systemd.Start(ctx, "caddy.service"); err == nil {
					if active2, detail2, _ := r.systemd.IsActive(ctx, "caddy.service"); active2 {
						out.CaddyRunning = true
						out.CaddyStatusError = ""
					} else {
						out.CaddyStatusError = detail2
					}
				}
			}
		}
	}

	// 2. Startup safety (§3.2): NEVER rewrite Caddy files at startup from empty state.
	anyLastGood := false
	for _, p := range cfg.Panels {
		if st := panelStates[p.PanelID]; st != nil && st.LastGood != nil {
			anyLastGood = true
			break
		}
	}
	if !anyLastGood && r.hasExistingRenderedFiles() {
		out.NoOp = true
		out.SkippedEmptyBoot = true
		return out, nil
	}

	// 3. Collect Shield IPs (config override + panel node.ip fields).
	shieldIPs := collectShieldIPs(cfg, panelStates)

	// Load currently rendered hosts from active panels dir for sticky DNS & throttle exemption.
	renderedByPanel := r.loadCurrentRenderedHosts()

	type preparedPanel struct {
		cfg            PanelConfig
		st             *PanelState
		originIPs      []netip.Addr
		originPort     int
		caStatus       string
		caPEM          string
		caMissing      bool
		validRoutes    []EdgeRoute
		candidateHosts map[string]struct{}
		dnsDrift       map[string]string
		hostStatuses   map[string]HostStatus
		preserveDisk   bool
		diskCaddy      []byte
		diskPEM        []byte
	}

	prepared := make(map[string]*preparedPanel, len(cfg.Panels))
	claimants := make([]Claimant, 0, len(cfg.Panels))

	for _, p := range cfg.Panels {
		st := panelStates[p.PanelID]
		if st == nil {
			st = NewPanelState(p.PanelID, p.PanelName)
			panelStates[p.PanelID] = st
		}

		pp := &preparedPanel{
			cfg:            p,
			st:             st,
			candidateHosts: make(map[string]struct{}),
			dnsDrift:       make(map[string]string),
			hostStatuses:   make(map[string]HostStatus),
		}
		prepared[p.PanelID] = pp

		// If this panel hasn't polled yet (LastGood == nil) but already has rendered
		// files on disk (e.g., unreachable at boot E-BOOT-2), preserve those files.
		if st.LastGood == nil {
			if cb, pb, ok := r.readExistingPanelFiles(p.PanelID); ok {
				pp.preserveDisk = true
				pp.diskCaddy = cb
				pp.diskPEM = pb
				desired := make(map[string]struct{}, len(renderedByPanel[p.PanelID]))
				for h := range renderedByPanel[p.PanelID] {
					desired[h] = struct{}{}
				}
				claimants = append(claimants, Claimant{
					PanelID:      p.PanelID,
					PanelName:    p.PanelName,
					RegisteredAt: p.RegisteredAt,
					DesiredHosts: desired,
				})
			}
			continue
		}

		snap := st.LastGood
		if err := ValidatePanelID(p.PanelID); err != nil {
			st.SyncState = SyncStateError
			st.LastError = err.Error()
			continue
		}

		// Validate origin IPs (§3.3).
		var validOriginIPs []netip.Addr
		var originLoopErr string
		for _, rawIP := range snap.Origin.IPs {
			addr, err := ValidateOriginIP(rawIP, shieldIPs)
			if err != nil {
				originLoopErr = err.Error()
				continue
			}
			validOriginIPs = append(validOriginIPs, addr)
		}
		pp.originIPs = validOriginIPs
		pp.originPort = snap.Origin.Port
		if pp.originPort <= 0 || pp.originPort > 65535 {
			pp.originPort = 443
		}

		// Validate Origin CA PEM (§3.3, E-ORIGIN-3).
		pp.caStatus = snap.Origin.CAStatus
		if pp.caStatus == "" {
			if snap.Origin.CAPEM != "" {
				pp.caStatus = CAStatusOK
			} else {
				pp.caStatus = CAStatusMissing
			}
		}
		if pp.caStatus == CAStatusOK {
			if err := ValidateCAPEM(snap.Origin.CAPEM); err != nil {
				// Per E-MP-5, invalid CA on one panel must not block other panels.
				st.SyncState = SyncStateError
				st.LastError = fmt.Sprintf("invalid origin CA PEM: %v", err)
				if cb, pb, ok := r.readExistingPanelFiles(p.PanelID); ok {
					pp.preserveDisk = true
					pp.diskCaddy = cb
					pp.diskPEM = pb
				}
				continue
			}
			pp.caPEM = snap.Origin.CAPEM
		} else {
			pp.caMissing = true
		}

		quota := cfg.PerPanelQuota
		if quota <= 0 {
			quota = DefaultPerPanelQuota
		}

		seenInPanel := make(map[string]struct{})
		acceptedCount := 0
		panelRendered := renderedByPanel[p.PanelID]

		routesCopy := make([]EdgeRoute, len(snap.Routes))
		copy(routesCopy, snap.Routes)
		sort.Slice(routesCopy, func(i, j int) bool { return routesCopy[i].ID < routesCopy[j].ID })

		for _, route := range routesCopy {
			if ValidateRouteID(route.ID) != nil {
				continue
			}
			var routeHosts []string
			for _, rawHost := range route.Hosts {
				norm, verr := NormalizeAndValidateHost(rawHost, cfg.ShieldHostname)
				if verr != nil {
					key := strings.ToLower(strings.TrimSpace(rawHost))
					if key != "" {
						pp.hostStatuses[key] = HostStatus{
							Host:   key,
							State:  verr.State,
							Detail: verr.Detail,
						}
					}
					continue
				}
				if _, dup := seenInPanel[norm]; dup {
					continue
				}
				seenInPanel[norm] = struct{}{}

				if len(validOriginIPs) == 0 {
					detail := originLoopErr
					if detail == "" {
						detail = "no valid origin IP configured"
					}
					pp.hostStatuses[norm] = HostStatus{
						Host:   norm,
						State:  StateExcludedLoop,
						Detail: detail,
					}
					continue
				}

				if acceptedCount >= quota {
					pp.hostStatuses[norm] = HostStatus{
						Host:   norm,
						State:  StateExcludedQuota,
						Detail: fmt.Sprintf("exceeded per-panel host quota (%d)", quota),
					}
					continue
				}
				acceptedCount++

				_, alreadyRendered := panelRendered[norm]
				dnsRes := r.dnsGate.CheckHost(ctx, norm, shieldIPs, alreadyRendered, force, now)
				if !dnsRes.Allowed {
					pp.hostStatuses[norm] = HostStatus{
						Host:   norm,
						State:  string(dnsRes.Outcome),
						Detail: dnsRes.Detail,
					}
					continue
				}
				if dnsRes.Drift {
					pp.dnsDrift[norm] = dnsRes.Detail
				}

				routeHosts = append(routeHosts, norm)
				pp.candidateHosts[norm] = struct{}{}
			}
			if len(routeHosts) > 0 {
				r2 := route
				r2.Hosts = routeHosts
				pp.validRoutes = append(pp.validRoutes, r2)
			}
		}

		claimants = append(claimants, Claimant{
			PanelID:      p.PanelID,
			PanelName:    p.PanelName,
			RegisteredAt: p.RegisteredAt,
			DesiredHosts: pp.candidateHosts,
		})
	}

	// 4. Claims / conflict resolution (§3.4).
	ownedByPanel, conflictsByPanel := claims.ResolveClaims(claimants, now)

	// 5. Admission throttle for brand-new hosts (§3.4).
	meta.ShieldAdmissions = pruneTimestamps(meta.ShieldAdmissions, now.Add(-shieldAdmissionWindow))

	// Sort panels by priority for fair deterministic admission ordering.
	orderedPanels := make([]PanelConfig, len(cfg.Panels))
	copy(orderedPanels, cfg.Panels)
	sort.Slice(orderedPanels, func(i, j int) bool {
		if !orderedPanels[i].RegisteredAt.Equal(orderedPanels[j].RegisteredAt) {
			return orderedPanels[i].RegisteredAt.Before(orderedPanels[j].RegisteredAt)
		}
		return orderedPanels[i].PanelID < orderedPanels[j].PanelID
	})

	admittedByPanel := make(map[string]map[string]struct{}, len(orderedPanels))
	for _, p := range orderedPanels {
		pp := prepared[p.PanelID]
		if pp == nil {
			continue
		}
		pp.st.Conflicts = conflictsByPanel[p.PanelID]
		for _, c := range pp.st.Conflicts {
			pp.hostStatuses[c.Host] = HostStatus{
				Host:   c.Host,
				State:  StateConflict,
				Detail: "claimed by " + c.OwnerPanel,
			}
		}

		if pp.preserveDisk || pp.st.LastGood == nil {
			continue
		}

		pp.st.AdmissionTimestamps = pruneTimestamps(pp.st.AdmissionTimestamps, now.Add(-panelAdmissionWindow))
		owned := ownedByPanel[p.PanelID]
		admitted := make(map[string]struct{}, len(owned))
		panelRendered := renderedByPanel[p.PanelID]

		ownedList := make([]string, 0, len(owned))
		for h := range owned {
			ownedList = append(ownedList, h)
		}
		sort.Strings(ownedList)

		for _, host := range ownedList {
			_, alreadyRendered := panelRendered[host]
			alreadyStoredCert := r.probe != nil && r.probe.HasValidStoredCert(host, now)
			if alreadyRendered || alreadyStoredCert {
				admitted[host] = struct{}{}
				continue
			}

			// Brand-new host: check 50/panel/10m and 250/Shield/3h.
			if len(pp.st.AdmissionTimestamps) >= panelAdmissionLimit || len(meta.ShieldAdmissions) >= shieldAdmissionLimit {
				pp.hostStatuses[host] = HostStatus{
					Host:   host,
					State:  StateQueuedRateLimit,
					Detail: "admission rate limit reached; queued for next window",
				}
				continue
			}
			pp.st.AdmissionTimestamps = append(pp.st.AdmissionTimestamps, now)
			meta.ShieldAdmissions = append(meta.ShieldAdmissions, now)
			admitted[host] = struct{}{}
		}
		admittedByPanel[p.PanelID] = admitted
	}

	// 6. Render artifacts for each panel.
	artifacts := make([]PanelRenderArtifact, 0, len(cfg.Panels))
	for _, p := range cfg.Panels {
		pp := prepared[p.PanelID]
		if pp == nil {
			continue
		}
		if pp.preserveDisk {
			artifacts = append(artifacts, PanelRenderArtifact{
				PanelID:    p.PanelID,
				CaddyBytes: pp.diskCaddy,
				PEMBytes:   pp.diskPEM,
				Changed:    false,
			})
			continue
		}
		if pp.st.LastGood == nil {
			continue
		}

		admitted := admittedByPanel[p.PanelID]
		var finalRoutes []EdgeRoute
		for _, rt := range pp.validRoutes {
			var kept []string
			for _, h := range rt.Hosts {
				if _, ok := admitted[h]; ok {
					kept = append(kept, h)
				}
			}
			if len(kept) > 0 {
				r2 := rt
				r2.Hosts = kept
				finalRoutes = append(finalRoutes, r2)
			}
		}

		rendered, err := RenderPanelCaddy(RenderPanelInput{
			PanelID:     p.PanelID,
			PanelName:   p.PanelName,
			ETag:        pp.st.LastGoodETag,
			OriginIPs:   pp.originIPs,
			OriginPort:  pp.originPort,
			CAStatus:    pp.caStatus,
			OriginsLink: r.paths.OriginsLink,
			Routes:      finalRoutes,
		})
		if err != nil {
			pp.st.SyncState = SyncStateError
			pp.st.LastError = err.Error()
			continue
		}
		var pemBytes []byte
		if pp.caPEM != "" {
			pemBytes = []byte(pp.caPEM)
		}
		artifacts = append(artifacts, PanelRenderArtifact{
			PanelID:    p.PanelID,
			CaddyBytes: []byte(rendered),
			PEMBytes:   pemBytes,
			Changed:    pp.st.LastAppliedETag != pp.st.LastGoodETag,
		})
	}

	// 7. Atomic Caddy generation apply (§5.5).
	applyIn := ApplyInput{
		AdminAddr:   cfg.AdminAddr,
		ACMEEmail:   cfg.ACMEEmail,
		GlobalOpts:  []byte(RenderGlobalOpts(r.cfIPs.Ranges())),
		CommonCaddy: []byte(RenderCommonCaddy()),
		Panels:      artifacts,
		Force:       force,
	}
	applyRes, applyErr := r.applier.Apply(ctx, applyIn, meta, now)
	out.ApplyResult = applyRes

	// Update per-panel state, ETag tracking, and cert probe statuses.
	for _, p := range cfg.Panels {
		pp := prepared[p.PanelID]
		if pp == nil || pp.st == nil {
			continue
		}
		if errMsg, isolated := applyRes.IsolatedPanels[p.PanelID]; isolated {
			pp.st.SyncState = SyncStateError
			pp.st.LastError = "caddy validate failed for panel: " + errMsg
		} else if applyErr == nil && pp.st.LastGood != nil && !pp.preserveDisk {
			pp.st.LastAppliedETag = pp.st.LastGoodETag
		}

		// Populate rendered hosts status.
		admitted := admittedByPanel[p.PanelID]
		for host := range admitted {
			driftDetail, hasDrift := pp.dnsDrift[host]
			if probeCerts && r.probe != nil {
				pp.hostStatuses[host] = r.probe.UpdateHostStatusFromProbe(ctx, pp.st, host, hasDrift, driftDetail, pp.caMissing, now)
			} else if prev, ok := pp.st.HostStates[host]; ok && (prev.State == StateActive || prev.State == StateIssuing || prev.State == StateCertError || prev.State == StateDNSDrift) {
				if hasDrift {
					prev.State = StateDNSDrift
					prev.Detail = appendWarning(prev.Detail, driftDetail)
				}
				pp.hostStatuses[host] = prev
			} else {
				state := StateIssuing
				detail := ""
				if hasDrift {
					state = StateDNSDrift
					detail = driftDetail
				}
				if pp.caMissing {
					detail = appendWarning(detail, WarningOriginCAMissing)
				}
				if _, exists := pp.st.HostFirstIssuing[host]; !exists {
					pp.st.HostFirstIssuing[host] = now
				}
				pp.hostStatuses[host] = HostStatus{
					Host:   host,
					State:  state,
					Detail: detail,
				}
			}
		}

		if !pp.preserveDisk {
			pp.st.HostStates = pp.hostStatuses
		}
		_ = SavePanelState(r.paths.StateFile(p.PanelID), pp.st)
	}

	_ = SaveClaims(r.paths.ClaimsFile, claims)
	_ = r.dnsGate.SaveCache()
	_ = SaveApplyMeta(r.paths.ApplyMeta, meta)

	return out, applyErr
}

func (r *Reconciler) hasExistingRenderedFiles() bool {
	panelsDir, err := ReadDirLink(r.paths.PanelsLink)
	if err != nil || panelsDir == "" {
		return false
	}
	entries, err := os.ReadDir(panelsDir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && e.Name() != PlaceholderFileName && strings.HasSuffix(e.Name(), ".caddy") {
			return true
		}
	}
	return false
}

func (r *Reconciler) loadCurrentRenderedHosts() map[string]map[string]struct{} {
	out := make(map[string]map[string]struct{})
	panelsDir, err := ReadDirLink(r.paths.PanelsLink)
	if err != nil || panelsDir == "" {
		return out
	}
	cs, err := RebuildClaimsFromCaddyDir(panelsDir, time.Now().UTC())
	if err != nil || cs == nil {
		return out
	}
	for host, cl := range cs.Claims {
		if out[cl.PanelID] == nil {
			out[cl.PanelID] = make(map[string]struct{})
		}
		out[cl.PanelID][host] = struct{}{}
	}
	return out
}

func (r *Reconciler) readExistingPanelFiles(panelID string) ([]byte, []byte, bool) {
	panelsDir, err := ReadDirLink(r.paths.PanelsLink)
	if err != nil || panelsDir == "" {
		return nil, nil, false
	}
	cb, err := os.ReadFile(filepath.Join(panelsDir, panelID+".caddy"))
	if err != nil {
		return nil, nil, false
	}
	var pb []byte
	if originsDir, oErr := ReadDirLink(r.paths.OriginsLink); oErr == nil && originsDir != "" {
		pb, _ = os.ReadFile(filepath.Join(originsDir, panelID+".pem"))
	}
	return cb, pb, true
}

func collectShieldIPs(cfg *Config, panelStates map[string]*PanelState) []netip.Addr {
	seen := make(map[netip.Addr]struct{})
	var out []netip.Addr
	add := func(s string) {
		if a, err := netip.ParseAddr(strings.TrimSpace(s)); err == nil && a.Zone() == "" {
			a = a.Unmap()
			if _, ok := seen[a]; !ok {
				seen[a] = struct{}{}
				out = append(out, a)
			}
		}
	}
	for _, s := range cfg.ShieldIPs {
		add(s)
	}
	for _, st := range panelStates {
		if st != nil && st.LastGood != nil && st.LastGood.Node.IP != "" {
			add(st.LastGood.Node.IP)
		}
	}
	return out
}

func pruneTimestamps(in []time.Time, cutoff time.Time) []time.Time {
	if len(in) == 0 {
		return nil
	}
	out := in[:0]
	for _, ts := range in {
		if !ts.Before(cutoff) {
			out = append(out, ts)
		}
	}
	return out
}
