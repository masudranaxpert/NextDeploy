package edge

import (
	"bytes"
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
	ManualHostStates []HostStatus
}

// Reconciler executes the §3.2–§3.5 reconciliation pipeline across all panels.
type Reconciler struct {
	mu                    sync.Mutex
	paths                 Paths
	engine                Engine
	dnsGate               *DNSGate
	applier               *Applier
	probe                 *CertProbe
	systemd               SystemdRunner
	cfIPs                 *CFIPStore
	extraShieldIPs        []netip.Addr
	lastCaddyStartAttempt time.Time
}

// NewReconciler constructs a Reconciler.
func NewReconciler(paths Paths, dnsGate *DNSGate, applier *Applier, probe *CertProbe, systemd SystemdRunner, cfIPs *CFIPStore) *Reconciler {
	return NewReconcilerWithEngine(paths, nil, dnsGate, applier, probe, systemd, cfIPs)
}

// NewReconcilerWithEngine constructs a Reconciler bound to a specific proxy Engine.
func NewReconcilerWithEngine(paths Paths, eng Engine, dnsGate *DNSGate, applier *Applier, probe *CertProbe, systemd SystemdRunner, cfIPs *CFIPStore) *Reconciler {
	if cfIPs == nil {
		cfIPs = NewCFIPStore()
	}
	if applier == nil {
		if eng != nil {
			applier = NewApplierForEngine(paths, eng)
		} else {
			applier = NewApplier(paths, nil)
		}
	}
	if probe == nil {
		certsDir := paths.CaddyCertsDir
		manualCertsDir := ""
		if eng != nil {
			certsDir = eng.Paths().CertsDir
			manualCertsDir = eng.Paths().ManualCertsDir
		}
		probe = NewCertProbe(certsDir, systemd)
		probe.ManualCertsDir = manualCertsDir
	}
	if dnsGate == nil {
		dnsGate = NewDNSGate(DNSGateOptions{CFIPs: cfIPs, CachePath: paths.DNSCacheFile})
	}
	return &Reconciler{
		paths:   paths,
		engine:  eng,
		dnsGate: dnsGate,
		applier: applier,
		probe:   probe,
		systemd: systemd,
		cfIPs:   cfIPs,
	}
}

// SetEngine updates the proxy Engine used by this Reconciler.
func (r *Reconciler) SetEngine(eng Engine) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.engine = eng
}

// SetExtraShieldIPs configures fallback public IPs for standalone Shield nodes
// when cfg.ShieldIPs and panel node IPs are empty.
func (r *Reconciler) SetExtraShieldIPs(ips []netip.Addr) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.extraShieldIPs = append([]netip.Addr(nil), ips...)
}

func (r *Reconciler) activeEnginePaths() EnginePaths {
	if r.engine != nil {
		return r.engine.Paths()
	}
	return CaddyEnginePaths(r.paths)
}

// ReconcileOnce runs a single full reconciliation across cfg.Panels and manual
// domains, updating panelStates, claims, meta, and proxy generation files atomically.
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
	return r.ReconcileOnceWithManual(ctx, cfg, panelStates, claims, meta, nil, force, probeCerts, now)
}

// ReconcileOnceWithManual runs reconciliation using manualOverride when non-nil
// (used by CLI mutators to validate and apply candidate changes before writing domains.json).
func (r *Reconciler) ReconcileOnceWithManual(
	ctx context.Context,
	cfg *Config,
	panelStates map[string]*PanelState,
	claims *ClaimsStore,
	meta *ApplyMeta,
	manualOverride *ManualDomainsFile,
	force bool,
	probeCerts bool,
	now time.Time,
) (ReconcileOutcome, error) {
	return r.reconcileInternal(ctx, cfg, panelStates, claims, meta, manualOverride, force, probeCerts, false, now)
}

// ValidateManualCandidate renders the full Shield config with manualCandidate and
// validates it with the active proxy engine without swapping live generations or writing state.
func (r *Reconciler) ValidateManualCandidate(
	ctx context.Context,
	cfg *Config,
	panelStates map[string]*PanelState,
	claims *ClaimsStore,
	meta *ApplyMeta,
	manualCandidate *ManualDomainsFile,
	now time.Time,
) error {
	_, err := r.reconcileInternal(ctx, cfg, panelStates, claims, meta, manualCandidate, true, false, true, now)
	return err
}

func (r *Reconciler) reconcileInternal(
	ctx context.Context,
	cfg *Config,
	panelStates map[string]*PanelState,
	claims *ClaimsStore,
	meta *ApplyMeta,
	manualOverride *ManualDomainsFile,
	force bool,
	probeCerts bool,
	validateOnly bool,
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

	ep := r.activeEnginePaths()
	serviceUnit := ep.ServiceUnit
	if serviceUnit == "" {
		serviceUnit = "caddy.service"
	}

	// 1. Check active proxy service status (E-BOOT-4, E-CADDY-1).
	out.CaddyRunning = true
	if !validateOnly && r.systemd != nil {
		active, detail, _ := r.systemd.IsActive(ctx, serviceUnit)
		out.CaddyRunning = active
		out.CaddyStatusError = detail
		if !active {
			if r.lastCaddyStartAttempt.IsZero() || now.Sub(r.lastCaddyStartAttempt) >= caddyStartCooldown {
				r.lastCaddyStartAttempt = now
				if err := r.systemd.Start(ctx, serviceUnit); err == nil {
					if active2, detail2, _ := r.systemd.IsActive(ctx, serviceUnit); active2 {
						out.CaddyRunning = true
						out.CaddyStatusError = ""
					} else {
						out.CaddyStatusError = detail2
					}
				}
			}
		}
	}

	// Load manual domains store (or staged candidate override).
	var manualFile *ManualDomainsFile
	if manualOverride != nil {
		manualFile = CloneManualDomainsFile(manualOverride)
	} else if r.paths.DomainsFile != "" {
		loaded, loadErr := LoadManualDomains(r.paths.DomainsFile)
		if loadErr != nil {
			return out, loadErr
		}
		manualFile = loaded
	} else {
		manualFile = &ManualDomainsFile{Schema: ManualDomainsSchema}
	}
	domainsFileExists := false
	if r.paths.DomainsFile != "" {
		if _, statErr := os.Stat(r.paths.DomainsFile); statErr == nil {
			domainsFileExists = true
		}
	}
	prevManualBytes, _, hasExistingManual := r.readExistingManualFile()

	// 2. Startup safety (§3.2): NEVER rewrite Caddy files at startup from empty state.
	anyLastGood := false
	for _, p := range cfg.Panels {
		if st := panelStates[p.PanelID]; st != nil && st.LastGood != nil {
			anyLastGood = true
			break
		}
	}
	if !validateOnly && !anyLastGood && len(manualFile.Domains) == 0 && !domainsFileExists && !hasExistingManual && r.hasExistingRenderedFiles() {
		out.NoOp = true
		out.SkippedEmptyBoot = true
		return out, nil
	}

	// 3. Collect Shield IPs (config override + panel node.ip fields + extra fallback).
	shieldIPs := collectShieldIPsWithExtra(cfg, panelStates, r.extraShieldIPs)

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
				claimants = append(claimants, Claimant{
					PanelID:      p.PanelID,
					PanelName:    p.PanelName,
					RegisteredAt: p.RegisteredAt,
					DesiredHosts: desiredHostsFromRendered(renderedByPanel[p.PanelID]),
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
				// Preserve existing disk files AND claims so another panel cannot steal
				// the host and cause duplicate Caddy site blocks.
				st.SyncState = SyncStateError
				st.LastError = fmt.Sprintf("invalid origin CA PEM: %v", err)
				if cb, pb, ok := r.readExistingPanelFiles(p.PanelID); ok {
					pp.preserveDisk = true
					pp.diskCaddy = cb
					pp.diskPEM = pb
					claimants = append(claimants, Claimant{
						PanelID:      p.PanelID,
						PanelName:    p.PanelName,
						RegisteredAt: p.RegisteredAt,
						DesiredHosts: desiredHostsFromRendered(renderedByPanel[p.PanelID]),
					})
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

	// 4. Claims / conflict resolution (§3.4), including manual domain ownership.
	if mc := ManualClaimant(manualFile); mc != nil {
		claimants = append(claimants, *mc)
	}
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
	anyTrustedProxy := false
	for _, p := range cfg.Panels {
		pp := prepared[p.PanelID]
		if pp == nil {
			continue
		}
		if pp.preserveDisk {
			if renderedCaddyUsesTrustedProxy(pp.diskCaddy) {
				anyTrustedProxy = true
			}
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
				if r2.TrustedProxy {
					anyTrustedProxy = true
				}
				finalRoutes = append(finalRoutes, r2)
			}
		}

		renderInput := RenderPanelInput{
			PanelID:     p.PanelID,
			PanelName:   p.PanelName,
			ETag:        pp.st.LastGoodETag,
			OriginIPs:   pp.originIPs,
			OriginPort:  pp.originPort,
			CAStatus:    pp.caStatus,
			OriginsLink: ep.OriginsLink,
			Routes:      finalRoutes,
		}
		var rendered string
		var err error
		if r.engine != nil {
			rendered, err = r.engine.RenderPanel(renderInput)
		} else {
			rendered, err = RenderPanelCaddy(renderInput)
		}
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

	manualArtifact, manualTrustedProxy, manualHostStates, mErr := r.prepareManualArtifact(
		ctx, cfg, manualFile, prevManualBytes, hasExistingManual, shieldIPs,
		ownedByPanel[ManualClaimPanelID], conflictsByPanel[ManualClaimPanelID],
		anyTrustedProxy, force, validateOnly, now,
	)
	if mErr != nil {
		return out, mErr
	}
	if manualTrustedProxy {
		anyTrustedProxy = true
	}

	// 7. Atomic proxy generation apply (§5.5).
	var applyIn ApplyInput
	if r.engine != nil && r.engine.Name() != EngineCaddy {
		globalFiles, gErr := r.engine.RenderGlobal(cfg, r.cfIPs.Ranges(), anyTrustedProxy)
		if gErr != nil {
			return out, gErr
		}
		commonFiles, cErr := r.engine.RenderCommon()
		if cErr != nil {
			return out, cErr
		}
		applyIn = ApplyInput{
			AdminAddr:   cfg.AdminAddr,
			ACMEEmail:   cfg.ACMEEmail,
			CaddyGroup:  ep.ServiceUser,
			GlobalFiles: globalFiles,
			CommonFiles: commonFiles,
			Panels:      artifacts,
			Manual:      manualArtifact,
			Force:       force,
		}
	} else {
		caddyGroup := strings.TrimSpace(cfg.CaddyUser)
		if caddyGroup == "" && r.systemd != nil {
			caddyGroup = r.systemd.CaddyUser(ctx)
		}
		applyIn = ApplyInput{
			AdminAddr:   cfg.AdminAddr,
			ACMEEmail:   cfg.ACMEEmail,
			CaddyGroup:  caddyGroup,
			GlobalOpts:  []byte(RenderGlobalOptsForConfig(cfg, r.cfIPs.Ranges(), anyTrustedProxy)),
			CommonCaddy: []byte(RenderCommonCaddy()),
			Panels:      artifacts,
			Manual:      manualArtifact,
			Force:       force,
		}
	}
	if validateOnly {
		return out, r.applier.ValidateOnly(ctx, applyIn)
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
			// Surface held_mass_delete for hosts in LastGood that are slated for
			// removal in PendingSnapshot while still keeping them rendered.
			if pp.st.PendingSnapshot != nil {
				pendingSet := collectSnapshotValidHostSet(pp.st.PendingSnapshot, cfg.ShieldHostname)
				for host, hs := range pp.hostStatuses {
					if _, stillPresent := pendingSet[host]; !stillPresent {
						if _, wasRendered := admitted[host]; wasRendered {
							hs.State = StateHeldMassDelete
							hs.Detail = appendWarning(hs.Detail, fmt.Sprintf("held by mass-deletion guard (%d/3 polls)", pp.st.PendingCount))
							pp.hostStatuses[host] = hs
						}
					}
				}
			}
			pp.st.HostStates = pp.hostStatuses
		}
		_ = SavePanelState(r.paths.StateFile(p.PanelID), pp.st)
	}

	out.ManualHostStates = r.finalizeManualHostStates(ctx, manualHostStates, probeCerts, now)

	_ = SaveClaims(r.paths.ClaimsFile, claims)
	_ = r.dnsGate.SaveCache()
	_ = SaveApplyMeta(r.paths.ApplyMeta, meta)

	return out, applyErr
}

func (r *Reconciler) hasExistingRenderedFiles() bool {
	ep := r.activeEnginePaths()
	panelsDir, err := ReadDirLink(ep.PanelsLink)
	if err != nil || panelsDir == "" {
		return false
	}
	entries, err := os.ReadDir(panelsDir)
	if err != nil {
		return false
	}
	ext := ep.PanelExt
	if ext == "" {
		ext = ".caddy"
	}
	for _, e := range entries {
		if !e.IsDir() &&
			e.Name() != PlaceholderFileName && e.Name() != HealthFileName &&
			e.Name() != PlaceholderAngieFileName && e.Name() != HealthAngieFileName &&
			e.Name() != PlaceholderFerronFileName && e.Name() != HealthFerronFileName &&
			strings.HasSuffix(e.Name(), ext) {
			return true
		}
	}
	return false
}

func (r *Reconciler) loadCurrentRenderedHosts() map[string]map[string]struct{} {
	ep := r.activeEnginePaths()
	out := make(map[string]map[string]struct{})
	panelsDir, err := ReadDirLink(ep.PanelsLink)
	if err != nil || panelsDir == "" {
		return out
	}
	cs, err := RebuildClaimsFromDir(panelsDir, ep.PanelExt, time.Now().UTC())
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
	ep := r.activeEnginePaths()
	ext := ep.PanelExt
	if ext == "" {
		ext = ".caddy"
	}
	panelsDir, err := ReadDirLink(ep.PanelsLink)
	if err != nil || panelsDir == "" {
		return nil, nil, false
	}
	cb, err := os.ReadFile(filepath.Join(panelsDir, panelID+ext))
	if err != nil {
		return nil, nil, false
	}
	var pb []byte
	if originsDir, oErr := ReadDirLink(ep.OriginsLink); oErr == nil && originsDir != "" {
		pb, _ = os.ReadFile(filepath.Join(originsDir, panelID+".pem"))
	}
	return cb, pb, true
}

func (r *Reconciler) readExistingManualFile() ([]byte, string, bool) {
	ep := r.activeEnginePaths()
	ext := ep.PanelExt
	if ext == "" {
		ext = ".caddy"
	}
	panelsDir, err := ReadDirLink(ep.PanelsLink)
	if err != nil || panelsDir == "" {
		return nil, ext, false
	}
	b, err := os.ReadFile(filepath.Join(panelsDir, ManualPanelID+ext))
	if err != nil {
		return nil, ext, false
	}
	return b, ext, true
}

func (r *Reconciler) activeEngineName() string {
	if r.engine != nil {
		return r.engine.Name()
	}
	return EngineCaddy
}

func (r *Reconciler) loadCurrentRenderedManualHosts() map[string]struct{} {
	out := make(map[string]struct{})
	raw, _, ok := r.readExistingManualFile()
	if !ok {
		return out
	}
	for _, h := range parseUserConfigHostsText(string(raw), r.activeEngineName()) {
		out[h] = struct{}{}
	}
	return out
}

func (r *Reconciler) prepareManualArtifact(
	ctx context.Context,
	cfg *Config,
	manualFile *ManualDomainsFile,
	prevManualBytes []byte,
	hasExistingManual bool,
	shieldIPs []netip.Addr,
	ownedManual map[string]struct{},
	manualConflicts []ConflictStatus,
	anyPanelTrustedProxy bool,
	force bool,
	validateOnly bool,
	now time.Time,
) (*PanelRenderArtifact, bool, []HostStatus, error) {
	if manualFile == nil || (len(manualFile.Domains) == 0 && !hasExistingManual) {
		return nil, false, nil, nil
	}

	conflictOwners := make(map[string]string, len(manualConflicts))
	for _, c := range manualConflicts {
		conflictOwners[c.Host] = c.OwnerPanel
	}

	renderedManual := r.loadCurrentRenderedManualHosts()
	dnsBlocked := make(map[string]bool)
	cfProxied := make(map[string]bool)
	statuses := make([]HostStatus, 0, len(manualFile.Domains))
	renderable := make([]ManualDomainEntry, 0, len(manualFile.Domains))

	for _, entry := range manualFile.Domains {
		allHosts := entry.AllHosts()
		if !entry.Enabled {
			for _, h := range allHosts {
				statuses = append(statuses, HostStatus{Host: h, State: "disabled", Detail: "disabled by operator"})
			}
			continue
		}

		var driftHosts = make(map[string]string)
		var keptHosts []string
		for _, h := range allHosts {
			if owner, isConflict := conflictOwners[h]; isConflict {
				dnsBlocked[h] = true
				statuses = append(statuses, HostStatus{Host: h, State: StateConflict, Detail: "claimed by " + owner})
				continue
			}
			if len(ownedManual) > 0 {
				if _, ok := ownedManual[h]; !ok {
					dnsBlocked[h] = true
					statuses = append(statuses, HostStatus{Host: h, State: StateConflict, Detail: "host ownership conflict"})
					continue
				}
			}

			if entry.TLS == ManualTLSAuto && !validateOnly && r.dnsGate != nil {
				_, alreadyRendered := renderedManual[h]
				res := r.dnsGate.CheckHost(ctx, h, shieldIPs, alreadyRendered, force, now)
				if !res.Allowed {
					dnsBlocked[h] = true
					statuses = append(statuses, HostStatus{Host: h, State: string(res.Outcome), Detail: res.Detail})
					continue
				}
				if res.Outcome == DNSOutcomeOKProxied {
					cfProxied[h] = true
				}
				if res.Drift {
					driftHosts[h] = res.Detail
				}
			}
			keptHosts = append(keptHosts, h)
		}
		if len(keptHosts) > 0 {
			renderable = append(renderable, entry)
			for _, h := range keptHosts {
				if detail, hasDrift := driftHosts[h]; hasDrift {
					statuses = append(statuses, HostStatus{Host: h, State: StateDNSDrift, Detail: detail})
				} else if entry.TLS == ManualTLSNone {
					statuses = append(statuses, HostStatus{Host: h, State: StateActive, Detail: "HTTP only"})
				} else {
					statuses = append(statuses, HostStatus{Host: h, State: StateIssuing})
				}
			}
		}
	}

	ep := r.activeEnginePaths()
	opts := ManualRenderOptions{
		Now:             now,
		ACMEEmail:       cfg.ACMEEmail,
		ManualCertsDir:  ep.ManualCertsDir,
		CertsDir:        ep.CertsDir,
		DNSBlockedHosts: dnsBlocked,
		CFProxiedHosts:  cfProxied,
		ShieldTrustsCF:  anyPanelTrustedProxy || renderedManualBytesUsesCF(prevManualBytes),
	}
	res, err := RenderManualForEngine(r.engine, renderable, opts)
	if err != nil {
		return nil, false, nil, fmt.Errorf("render manual domains: %w", err)
	}
	contentBytes := []byte(res.Content)
	changed := !hasExistingManual || !bytes.Equal(prevManualBytes, contentBytes)
	art := &PanelRenderArtifact{
		PanelID:    ManualPanelID,
		CaddyBytes: contentBytes,
		Changed:    changed,
	}
	return art, res.AnyTrustedProxy, statuses, nil
}

func renderedManualBytesUsesCF(b []byte) bool {
	s := string(b)
	return strings.Contains(s, "header_up X-Real-IP {client_ip}") || strings.Contains(s, "X-Real-IP $remote_addr;")
}

func (r *Reconciler) finalizeManualHostStates(ctx context.Context, in []HostStatus, probeCerts bool, now time.Time) []HostStatus {
	if len(in) == 0 {
		return nil
	}
	out := make([]HostStatus, len(in))
	copy(out, in)
	if probeCerts && r.probe != nil {
		for i := range out {
			if out[i].State != StateIssuing && out[i].State != StateDNSDrift {
				continue
			}
			pr := r.probe.ProbeHost(ctx, out[i].Host, now)
			if pr.Valid {
				if out[i].State != StateDNSDrift {
					out[i].State = StateActive
				}
				if pr.RenewWarn {
					out[i].Detail = appendWarning(out[i].Detail, WarningCertRenewOverdue)
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Host < out[j].Host })
	return out
}

func collectShieldIPsWithExtra(cfg *Config, panelStates map[string]*PanelState, extra []netip.Addr) []netip.Addr {
	base := collectShieldIPs(cfg, panelStates)
	if len(extra) == 0 {
		return base
	}
	seen := make(map[netip.Addr]struct{}, len(base)+len(extra))
	for _, a := range base {
		seen[a] = struct{}{}
	}
	for _, a := range extra {
		u := a.Unmap()
		if _, ok := seen[u]; !ok {
			seen[u] = struct{}{}
			base = append(base, u)
		}
	}
	return base
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

func desiredHostsFromRendered(rendered map[string]struct{}) map[string]struct{} {
	desired := make(map[string]struct{}, len(rendered))
	for h := range rendered {
		desired[h] = struct{}{}
	}
	return desired
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

func renderedCaddyUsesTrustedProxy(caddyBytes []byte) bool {
	s := string(caddyBytes)
	return strings.Contains(s, "_tp)") || strings.Contains(s, "X-Real-IP $remote_addr;")
}
