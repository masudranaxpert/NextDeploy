package edge

import (
	"context"
	"errors"
	"log"
	"net/http"
	"net/netip"
	"os"
	"sync"
	"time"
)

const (
	defaultDebounceDelay    = 3 * time.Second
	defaultMinApplyInterval = 5 * time.Second
	defaultStatusInterval   = 60 * time.Second
	defaultPendingProbeInt  = 2 * time.Minute
	defaultActiveProbeInt   = 6 * time.Hour
	defaultCFRefreshInt     = 24 * time.Hour
)

// DaemonOptions configures a running nd-edge daemon instance.
type DaemonOptions struct {
	Paths            Paths
	EdgeVersion      string
	CaddyVersion     string
	Caddy            CaddyRunner
	Systemd          SystemdRunner
	DNSResolver      DNSResolver
	CertProbe        *CertProbe
	HTTPClient       *http.Client
	Logger           *log.Logger
	DebounceDelay    time.Duration
	MinApplyInterval time.Duration
	StatusInterval   time.Duration
}

// Daemon coordinates independent per-panel pollers, debounced reconciliation,
// periodic cert probes, Cloudflare IP refreshes, and per-panel status reporting (§3.2).
type Daemon struct {
	opts       DaemonOptions
	cfg        *Config
	cfIPs      *CFIPStore
	dnsGate    *DNSGate
	applier    *Applier
	probe      *CertProbe
	reconciler *Reconciler

	mu          sync.Mutex
	panelStates map[string]*PanelState
	claims      *ClaimsStore
	meta        *ApplyMeta
	clients     map[string]*PanelClient
	startedAt   time.Time
	lastCaddyOK bool
	lastCaddyEr string

	reconcileCh chan bool // value = force
	statusCh    chan struct{}
}

// NewDaemon initializes daemon state from disk, holding the flock until Close
// or returning ErrLockHeld / *CorruptFileError on invalid startup state.
func NewDaemon(opts DaemonOptions) (*Daemon, *FileLock, error) {
	if opts.Logger == nil {
		opts.Logger = log.New(os.Stderr, "[nd-edge] ", log.LstdFlags)
	}
	if opts.DebounceDelay <= 0 {
		opts.DebounceDelay = defaultDebounceDelay
	}
	if opts.MinApplyInterval <= 0 {
		opts.MinApplyInterval = defaultMinApplyInterval
	}
	if opts.StatusInterval <= 0 {
		opts.StatusInterval = defaultStatusInterval
	}

	lock, err := AcquireLock(opts.Paths.LockFile)
	if err != nil {
		return nil, nil, err
	}
	if pidErr := WritePIDFile(opts.Paths.PIDFile); pidErr != nil {
		opts.Logger.Printf("warning: write pid file: %v", pidErr)
	}

	// E-STATE-3: corrupt config.json refuses to apply and never modifies Caddy files.
	cfg, err := LoadConfig(opts.Paths.ConfigFile)
	if err != nil {
		_ = lock.Release()
		return nil, nil, err
	}

	now := time.Now().UTC()
	panelStates := make(map[string]*PanelState, len(cfg.Panels))
	for _, p := range cfg.Panels {
		st, stErr := LoadPanelState(opts.Paths.StateFile(p.PanelID), p.PanelID, p.PanelName, now)
		if stErr != nil {
			// E-STATE-1: corrupt panel state quarantined; keep Caddy files, refetch on poll.
			opts.Logger.Printf("warning: %v", stErr)
		}
		panelStates[p.PanelID] = st
	}

	panelsDir, _ := ReadDirLink(opts.Paths.PanelsLink)
	claims, clErr := LoadClaims(opts.Paths.ClaimsFile, panelsDir, now)
	if clErr != nil {
		// E-STATE-2: corrupt claims rebuilt from rendered Caddy files.
		opts.Logger.Printf("warning: %v", clErr)
	}

	meta, metaErr := LoadApplyMeta(opts.Paths.ApplyMeta)
	if metaErr != nil {
		opts.Logger.Printf("warning: %v", metaErr)
	}

	cfIPs := LoadOrInitCFIPStore(opts.Paths.CFIPsFile)
	dnsGate := NewDNSGate(DNSGateOptions{
		Resolver:  opts.DNSResolver,
		CFIPs:     cfIPs,
		CachePath: opts.Paths.DNSCacheFile,
		ACMEEmail: cfg.ACMEEmail,
		Off:       cfg.DNSGateOff,
	})
	applier := NewApplier(opts.Paths, opts.Caddy)
	probe := opts.CertProbe
	if probe == nil {
		probe = NewCertProbe(opts.Paths.CaddyCertsDir, opts.Systemd)
	}
	reconciler := NewReconciler(opts.Paths, dnsGate, applier, probe, opts.Systemd, cfIPs)

	d := &Daemon{
		opts:        opts,
		cfg:         cfg,
		cfIPs:       cfIPs,
		dnsGate:     dnsGate,
		applier:     applier,
		probe:       probe,
		reconciler:  reconciler,
		panelStates: panelStates,
		claims:      claims,
		meta:        meta,
		clients:     make(map[string]*PanelClient, len(cfg.Panels)),
		startedAt:   now,
		lastCaddyOK: true,
		reconcileCh: make(chan bool, 16),
		statusCh:    make(chan struct{}, 8),
	}
	d.rebuildClientsLocked()
	return d, lock, nil
}

func (d *Daemon) rebuildClientsLocked() {
	shieldIPs := collectShieldIPs(d.cfg, d.panelStates)
	for _, p := range d.cfg.Panels {
		var originIPs []netip.Addr
		if st := d.panelStates[p.PanelID]; st != nil && st.LastGood != nil {
			originIPs = ParseIPStrings(st.LastGood.Origin.IPs)
		}
		d.clients[p.PanelID] = NewPanelClient(p, shieldIPs, originIPs)
	}
}

// TriggerSync requests an immediate reconciliation pass (used by SIGHUP / `nd edge sync`).
func (d *Daemon) TriggerSync(force bool) {
	select {
	case d.reconcileCh <- force:
	default:
	}
}

// ReloadConfig re-reads config.json (e.g. on SIGHUP after CLI mutation) and triggers sync.
func (d *Daemon) ReloadConfig() error {
	cfg, err := LoadConfig(d.opts.Paths.ConfigFile)
	if err != nil {
		return err
	}
	d.mu.Lock()
	now := time.Now().UTC()
	d.cfg = cfg
	d.dnsGate.SetOff(cfg.DNSGateOff)
	newStates := make(map[string]*PanelState, len(cfg.Panels))
	for _, p := range cfg.Panels {
		if existing, ok := d.panelStates[p.PanelID]; ok {
			newStates[p.PanelID] = existing
		} else {
			st, _ := LoadPanelState(d.opts.Paths.StateFile(p.PanelID), p.PanelID, p.PanelName, now)
			newStates[p.PanelID] = st
		}
	}
	d.panelStates = newStates
	d.rebuildClientsLocked()
	d.mu.Unlock()
	d.TriggerSync(false)
	return nil
}

// Run starts all daemon loops and blocks until ctx is canceled.
func (d *Daemon) Run(ctx context.Context) error {
	var wg sync.WaitGroup

	// Initial Caddy status check on boot.
	if d.opts.Systemd != nil {
		active, detail, _ := d.opts.Systemd.IsActive(ctx, "caddy.service")
		d.mu.Lock()
		d.lastCaddyOK = active
		d.lastCaddyEr = detail
		d.mu.Unlock()
	}

	// Independent goroutine per panel (§3.2, E-API-15).
	d.mu.Lock()
	panels := append([]PanelConfig(nil), d.cfg.Panels...)
	d.mu.Unlock()

	for _, p := range panels {
		panel := p
		wg.Add(1)
		go func() {
			defer wg.Done()
			d.runPanelPoller(ctx, panel)
		}()
	}

	// Single debounced reconciler goroutine.
	wg.Add(1)
	go func() {
		defer wg.Done()
		d.runReconcilerLoop(ctx)
	}()

	// Status loop (60s + after apply).
	wg.Add(1)
	go func() {
		defer wg.Done()
		d.runStatusLoop(ctx)
	}()

	// Cert probe loop (2m for pending, 6h for active).
	wg.Add(1)
	go func() {
		defer wg.Done()
		d.runProbeLoop(ctx)
	}()

	// Daily Cloudflare IP refresh loop.
	wg.Add(1)
	go func() {
		defer wg.Done()
		d.runCFIPsLoop(ctx)
	}()

	<-ctx.Done()
	wg.Wait()
	return nil
}

func (d *Daemon) runPanelPoller(ctx context.Context, panel PanelConfig) {
	consecutiveErrs := 0
	for {
		d.mu.Lock()
		st := d.panelStates[panel.PanelID]
		client := d.clients[panel.PanelID]
		shieldHost := d.cfg.ShieldHostname
		d.mu.Unlock()

		if st != nil && client != nil {
			out := PollPanelOnce(ctx, client, st, shieldHost, false, time.Now().UTC())
			d.mu.Lock()
			_ = SavePanelState(d.opts.Paths.StateFile(panel.PanelID), st)
			if out.SnapshotUpdated {
				d.rebuildClientsLocked()
			}
			syncState := st.SyncState
			d.mu.Unlock()

			if out.Err != nil {
				consecutiveErrs++
				d.opts.Logger.Printf("panel %s poll: %v", panel.PanelName, out.Err)
			} else {
				consecutiveErrs = 0
			}
			if out.SnapshotUpdated || out.PanelReleased {
				if out.PanelReleased {
					d.handlePanelReleased(panel.PanelID)
				}
				d.TriggerSync(false)
			}

			delay := NextPollDelay(panel.PollIntervalSec, consecutiveErrs, syncState)
			select {
			case <-ctx.Done():
				return
			case <-time.After(delay):
			}
		} else {
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Duration(panel.PollIntervalSec) * time.Second):
			}
		}
	}
}

func (d *Daemon) handlePanelReleased(panelID string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.claims.ReleasePanelClaims(panelID)
	d.cfg.RemovePanel(panelID)
	delete(d.panelStates, panelID)
	delete(d.clients, panelID)
	_ = SaveConfig(d.opts.Paths.ConfigFile, d.cfg)
	_ = SaveClaims(d.opts.Paths.ClaimsFile, d.claims)
	_ = os.Remove(d.opts.Paths.StateFile(panelID))
}

func (d *Daemon) runReconcilerLoop(ctx context.Context) {
	var lastApply time.Time
	var pendingForce bool
	var debounceTimer *time.Timer
	var debounceCh <-chan time.Time

	execute := func() {
		if !lastApply.IsZero() {
			since := time.Since(lastApply)
			if since < d.opts.MinApplyInterval {
				wait := d.opts.MinApplyInterval - since
				select {
				case <-ctx.Done():
					return
				case <-time.After(wait):
				}
			}
		}

		d.mu.Lock()
		cfg := d.cfg
		states := d.panelStates
		claims := d.claims
		meta := d.meta
		force := pendingForce
		pendingForce = false
		d.mu.Unlock()

		out, err := d.reconciler.ReconcileOnce(ctx, cfg, states, claims, meta, force, false, time.Now().UTC())
		lastApply = time.Now()

		d.mu.Lock()
		d.lastCaddyOK = out.CaddyRunning
		d.lastCaddyEr = out.CaddyStatusError
		d.mu.Unlock()

		if err != nil {
			d.opts.Logger.Printf("reconcile error: %v", err)
		} else if out.Applied {
			d.opts.Logger.Printf("applied generation %d (reload=%s)", out.Generation, out.ReloadDuration)
			if out.SlowReloadWarn {
				d.opts.Logger.Printf("warning: caddy reload took %s (>10s)", out.ReloadDuration)
			}
			select {
			case d.statusCh <- struct{}{}:
			default:
			}
		}
	}

	for {
		select {
		case <-ctx.Done():
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			return
		case force := <-d.reconcileCh:
			if force {
				pendingForce = true
			}
			if debounceTimer == nil {
				debounceTimer = time.NewTimer(d.opts.DebounceDelay)
				debounceCh = debounceTimer.C
			} else {
				if !debounceTimer.Stop() {
					select {
					case <-debounceTimer.C:
					default:
					}
				}
				debounceTimer.Reset(d.opts.DebounceDelay)
			}
		case <-debounceCh:
			debounceTimer = nil
			debounceCh = nil
			execute()
		}
	}
}

func (d *Daemon) runStatusLoop(ctx context.Context) {
	ticker := time.NewTicker(d.opts.StatusInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.pushAllStatuses(ctx)
		case <-d.statusCh:
			d.pushAllStatuses(ctx)
		}
	}
}

func (d *Daemon) pushAllStatuses(ctx context.Context) {
	d.mu.Lock()
	cfg := d.cfg
	meta := d.meta
	caddyOK := d.lastCaddyOK
	caddyErr := d.lastCaddyEr
	panels := append([]PanelConfig(nil), cfg.Panels...)
	d.mu.Unlock()

	uptime := int64(time.Since(d.startedAt).Seconds())
	for _, p := range panels {
		d.mu.Lock()
		st := d.panelStates[p.PanelID]
		client := d.clients[p.PanelID]
		doc := BuildStatusForPanel(cfg, st, meta, d.opts.EdgeVersion, d.opts.CaddyVersion, caddyOK, caddyErr, uptime, 0, d.opts.Paths)
		d.mu.Unlock()

		if client == nil {
			continue
		}
		if err := client.PostStatus(ctx, doc); err != nil && !errors.Is(err, context.Canceled) {
			d.opts.Logger.Printf("status post to %s failed: %v", p.PanelName, err)
		}
	}
}

func (d *Daemon) runProbeLoop(ctx context.Context) {
	pendingTicker := time.NewTicker(defaultPendingProbeInt)
	activeTicker := time.NewTicker(defaultActiveProbeInt)
	defer pendingTicker.Stop()
	defer activeTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-pendingTicker.C:
			d.probeHosts(ctx, false)
		case <-activeTicker.C:
			d.probeHosts(ctx, true)
		}
	}
}

func (d *Daemon) probeHosts(ctx context.Context, includeActive bool) {
	if d.probe == nil {
		return
	}
	now := time.Now().UTC()
	d.mu.Lock()
	panels := append([]PanelConfig(nil), d.cfg.Panels...)
	d.mu.Unlock()

	for _, p := range panels {
		d.mu.Lock()
		st := d.panelStates[p.PanelID]
		if st == nil {
			d.mu.Unlock()
			continue
		}
		var targets []string
		caMissing := st.LastGood != nil && st.LastGood.Origin.CAStatus == CAStatusMissing
		for h, hs := range st.HostStates {
			switch hs.State {
			case StateIssuing, StateCertError:
				targets = append(targets, h)
			case StateActive, StateDNSDrift:
				if includeActive {
					targets = append(targets, h)
				}
			}
		}
		d.mu.Unlock()

		for _, h := range targets {
			d.mu.Lock()
			prev := st.HostStates[h]
			hasDrift := prev.State == StateDNSDrift
			driftDetail := ""
			if hasDrift {
				driftDetail = prev.Detail
			}
			d.probe.UpdateHostStatusFromProbe(ctx, st, h, hasDrift, driftDetail, caMissing, now)
			d.mu.Unlock()
		}
		if len(targets) > 0 {
			d.mu.Lock()
			_ = SavePanelState(d.opts.Paths.StateFile(p.PanelID), st)
			d.mu.Unlock()
		}
	}
}

func (d *Daemon) runCFIPsLoop(ctx context.Context) {
	ticker := time.NewTicker(defaultCFRefreshInt)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := d.cfIPs.Refresh(ctx, d.opts.HTTPClient, d.opts.Paths.CFIPsFile, "", ""); err != nil {
				d.opts.Logger.Printf("cf-ips refresh failed: %v", err)
			} else {
				d.TriggerSync(false)
			}
		}
	}
}

// ReconcileNow exposes an explicit synchronous reconciliation step for CLI / tests.
func (d *Daemon) ReconcileNow(ctx context.Context, force bool, probeCerts bool) (ReconcileOutcome, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	out, err := d.reconciler.ReconcileOnce(ctx, d.cfg, d.panelStates, d.claims, d.meta, force, probeCerts, time.Now().UTC())
	d.lastCaddyOK = out.CaddyRunning
	d.lastCaddyEr = out.CaddyStatusError
	return out, err
}

// SnapshotPanelStates returns a copy of current panel states for CLI status rendering.
func (d *Daemon) SnapshotPanelStates() (*Config, map[string]*PanelState, *ApplyMeta) {
	d.mu.Lock()
	defer d.mu.Unlock()
	cfgCopy := *d.cfg
	cfgCopy.Panels = append([]PanelConfig(nil), d.cfg.Panels...)
	statesCopy := make(map[string]*PanelState, len(d.panelStates))
	for k, v := range d.panelStates {
		cp := *v
		statesCopy[k] = &cp
	}
	metaCopy := *d.meta
	return &cfgCopy, statesCopy, &metaCopy
}
