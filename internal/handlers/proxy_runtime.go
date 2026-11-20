package handlers

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"

	"panel/internal/caddy"
	"panel/internal/db"
	"panel/internal/proxy"
	"panel/internal/proxy/angie"
	pcaddy "panel/internal/proxy/caddy"
	"panel/internal/proxy/ferron"
	"panel/internal/proxy/pki"
	pstate "panel/internal/proxy/state"
)

// proxyRuntime holds the active reverse-proxy engine, its driver, and (for file-rendered
// engines) the debounced Syncer. Caddy keeps today's label-driven flow and has no Syncer.
type proxyRuntime struct {
	mu      sync.Mutex
	engine  string
	drivers map[string]proxy.Driver
	syncer  *proxy.Syncer
	// newDriverFn and buildStateFn are test hooks.
	newDriverFn  func(name string) (proxy.Driver, error)
	buildStateFn proxy.BuildStateFunc
}

func normalizeProxyEngine(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "angie":
		return "angie"
	case "ferron":
		return "ferron"
	default:
		return "caddy"
	}
}

// ProxyEngine returns the active proxy engine ("caddy", "angie", "ferron").
func (p *Panel) ProxyEngine() string {
	p.proxyRT.mu.Lock()
	defer p.proxyRT.mu.Unlock()
	if p.proxyRT.engine == "" {
		val := ""
		if p.DB != nil {
			val = p.DB.GetSetting(context.Background(), db.SettingProxyEngine)
		}
		p.proxyRT.engine = normalizeProxyEngine(val)
	}
	return p.proxyRT.engine
}

func (p *Panel) isCaddyEngine() bool { return p.ProxyEngine() == "caddy" }

// proxyServiceKey is the root compose service key of the active proxy engine.
func (p *Panel) proxyServiceKey() string {
	if p.isCaddyEngine() {
		return caddy.CaddyContainerName
	}
	return "nd-proxy"
}

// proxyEngineDataDir is the host directory holding rendered config generations for an engine
// (bind-mounted into nd-proxy; the panel sees the same path through its /data mount).
func proxyEngineDataDir(name string) string {
	switch normalizeProxyEngine(name) {
	case "angie":
		return angie.DefaultDataDir
	case "ferron":
		return ferron.DefaultDataDir
	default:
		return ""
	}
}

// proxyStageStore returns the generation store ("sites" symlink → sites.gen-*) for an engine.
func proxyStageStore(name string) (*proxy.StageStore, error) {
	dir := proxyEngineDataDir(name)
	if dir == "" {
		return nil, nil
	}
	return proxy.NewStageStore(proxy.StageConfig{BaseDir: dir, KeepGenerations: 3})
}

func newProxyDriver(name string) (proxy.Driver, error) { return newProxyDriverWithHost(name, nil) }

// newProxyDriverWithHost builds a driver whose `docker run -v` sources go through hostPath.
func newProxyDriverWithHost(name string, hostPath func(string) string) (proxy.Driver, error) {
	switch normalizeProxyEngine(name) {
	case "angie":
		st, err := proxyStageStore("angie")
		if err != nil {
			return nil, err
		}
		// Without a StageStore the driver would only reload and never write files.
		return angie.New(angie.WithStageStore(st), angie.WithHostPath(hostPath)), nil
	case "ferron":
		st, err := proxyStageStore("ferron")
		if err != nil {
			return nil, err
		}
		return ferron.New(ferron.WithStageStore(st), ferron.WithHostPath(hostPath)), nil
	default:
		return pcaddy.New(), nil
	}
}

// proxyDriverFor returns a cached driver for the given engine.
func (p *Panel) proxyDriverFor(name string) (proxy.Driver, error) {
	name = normalizeProxyEngine(name)
	p.proxyRT.mu.Lock()
	defer p.proxyRT.mu.Unlock()
	if d, ok := p.proxyRT.drivers[name]; ok {
		return d, nil
	}
	mk := p.proxyRT.newDriverFn
	if mk == nil {
		mk = func(n string) (proxy.Driver, error) { return newProxyDriverWithHost(n, p.hostPathOf) }
	}
	d, err := mk(name)
	if err != nil {
		return nil, err
	}
	if p.proxyRT.drivers == nil {
		p.proxyRT.drivers = map[string]proxy.Driver{}
	}
	p.proxyRT.drivers[name] = d
	return d, nil
}

// ProxyDriver returns the driver of the active engine.
func (p *Panel) ProxyDriver() (proxy.Driver, error) { return p.proxyDriverFor(p.ProxyEngine()) }

// proxyAppLabeler returns the label generator for app compose overrides: nil keeps the
// historic Caddy labels byte-for-byte; Angie emits upstream discovery labels; Ferron none.
func (p *Panel) proxyAppLabeler() caddy.AppLabeler {
	if p.isCaddyEngine() {
		return nil
	}
	d, err := p.ProxyDriver()
	if err != nil {
		return nil
	}
	return d.AppLabels
}

// proxyLoadOptions resolves per-app compose projects and public IPs for the IR builder.
func (p *Panel) proxyLoadOptions() pstate.LoadOptions {
	return pstate.LoadOptions{
		ResolveAppTarget: func(ctx context.Context, app db.App) (pstate.AppTarget, error) {
			t := pstate.AppTarget{ActiveProject: p.ActiveComposeProjectName(ctx, app, app.ID)}
			if p.Store != nil {
				t.WorkspaceDir = p.Store.Path(app.ID)
			}
			if ip, shield := p.ResolveAppPublicIP(ctx, app); ip != "" {
				t.PublicIP = ip
				t.ShieldNodeID = shield
			}
			return t, nil
		},
		ResolvePublicIP: func(ctx context.Context) string { return p.originPublicIP(ctx) },
	}
}

// buildProxyState builds the engine-agnostic desired state from the database.
func (p *Panel) buildProxyState(ctx context.Context) (*proxy.State, error) {
	if fn := p.proxyRT.buildStateFn; fn != nil {
		return fn(ctx)
	}
	st, err := pstate.LoadAndBuild(ctx, p.DB, p.proxyLoadOptions())
	if err == nil {
		p.ensureInternalCerts(st)
	}
	return st, err
}

// proxyPKI is the panel's private CA for internal-TLS sites (sites behind a Shield edge).
// Caddy issues these itself (`tls internal`); Angie and Ferron load the PEM files.
var (
	proxyPKIOnce sync.Once
	proxyPKIMgr  *pki.Manager
	proxyPKIErr  error
)

// ensureInternalCerts issues or renews the leaf certificate of every internal-TLS site so the
// rendered Angie/Ferron configs never point at a missing file. Failures are logged; validation
// then reports the missing certificate for that site.
func (p *Panel) ensureInternalCerts(st *proxy.State) (renewed int) {
	if st == nil {
		return 0
	}
	proxyPKIOnce.Do(func() {
		alignPKIWithCaddyRoot(context.Background(), pki.DefaultCADir, pki.DefaultSitesDir)
		proxyPKIMgr, proxyPKIErr = pki.NewManager(pki.DefaultCADir, pki.DefaultSitesDir)
	})
	if proxyPKIErr != nil {
		log.Printf("proxy: internal PKI: %v", proxyPKIErr)
		return 0
	}
	for _, site := range st.Sites {
		if !siteNeedsInternalCert(site) || len(site.Hosts) == 0 {
			continue
		}
		cp, err := proxyPKIMgr.EnsureSiteCert(site.ID, site.Hosts, time.Time{})
		if err != nil {
			log.Printf("proxy: internal cert for site %s: %v", site.ID, err)
			continue
		}
		if cp.Renewed {
			renewed++
		}
	}
	return renewed
}

// renewInternalCertsLoop re-checks internal-TLS leaf certificates twice a day. A renewed file
// does not change the rendered config, so the proxy is force-reloaded to pick it up.
func (p *Panel) renewInternalCertsLoop() {
	t := time.NewTicker(12 * time.Hour)
	defer t.Stop()
	for range t.C {
		if p.isCaddyEngine() || p.proxyRT.buildStateFn != nil {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		st, err := pstate.LoadAndBuild(ctx, p.DB, p.proxyLoadOptions())
		if err == nil && p.ensureInternalCerts(st) > 0 {
			if _, serr := p.ProxySyncNow(ctx, proxy.Reason{Kind: "internal_cert_renewal"}, true); serr != nil {
				log.Printf("proxy: reload after internal cert renewal: %v", serr)
			}
		}
		cancel()
	}
}

func siteNeedsInternalCert(site proxy.Site) bool {
	return site.TLS == proxy.TLSInternal || (site.TLS == "" && site.BehindShield)
}

// proxySyncer returns the Syncer for the active file-rendered engine (nil for Caddy).
func (p *Panel) proxySyncer() *proxy.Syncer {
	if p.isCaddyEngine() {
		return nil
	}
	drv, err := p.ProxyDriver()
	if err != nil {
		log.Printf("proxy: driver for %s: %v", p.ProxyEngine(), err)
		return nil
	}
	p.proxyRT.mu.Lock()
	defer p.proxyRT.mu.Unlock()
	if p.proxyRT.syncer != nil {
		return p.proxyRT.syncer
	}
	// Stage into the engine data dir (host-visible through the /data mount): validation runs in
	// a sibling container via `docker run -v <host path>`, which cannot see the panel's /tmp.
	var stage *proxy.StageStore
	if p.proxyRT.newDriverFn == nil {
		st, serr := proxyStageStore(p.proxyRT.engine)
		if serr != nil {
			log.Printf("proxy: stage store: %v", serr)
			return nil
		}
		stage = st
	}
	s, err := proxy.NewSyncer(proxy.SyncerConfig{
		Driver:     drv,
		Stage:      stage,
		BuildState: p.buildProxyState,
		RecordApply: func(ctx context.Context, generation, driver, status, errMsg string) error {
			if p.DB == nil {
				return nil
			}
			_, err := p.DB.RecordProxyApply(ctx, generation, driver, status, errMsg)
			return err
		},
	})
	if err != nil {
		log.Printf("proxy: syncer: %v", err)
		return nil
	}
	p.proxyRT.syncer = s
	return s
}

// ProxySync schedules a debounced config sync for file-rendered engines. It is a no-op for
// Caddy, whose config follows container labels.
func (p *Panel) ProxySync(_ context.Context, reason proxy.Reason) error {
	if s := p.proxySyncer(); s != nil {
		s.SyncAsync(reason)
	}
	return nil
}

// ProxySyncNow renders, validates and applies immediately (file-rendered engines only).
func (p *Panel) ProxySyncNow(ctx context.Context, reason proxy.Reason, force bool) (proxy.ApplyResult, error) {
	s := p.proxySyncer()
	if s == nil {
		return proxy.ApplyResult{Driver: p.ProxyEngine(), Message: "label-driven engine"}, nil
	}
	if force {
		return s.ForceSyncNow(ctx, reason)
	}
	return s.SyncNow(ctx, reason)
}

// setProxyEngine switches the in-memory runtime to engine (the setting is persisted by the caller).
func (p *Panel) setProxyEngine(engine string) {
	engine = normalizeProxyEngine(engine)
	p.proxyRT.mu.Lock()
	old := p.proxyRT.syncer
	p.proxyRT.engine = engine
	p.proxyRT.syncer = nil
	p.proxyRT.mu.Unlock()
	if old != nil {
		_ = old.Close()
	}
}

// StartProxyRuntime wires the PHP hosting hooks and, for Angie/Ferron, renders and applies the
// full config once at start-up so the proxy never runs on a stale or bootstrap-only config.
func (p *Panel) StartProxyRuntime() {
	if p.PHP != nil {
		p.PHP.SetProxySync(p.ProxyEngine, p.ProxySync)
	}
	go p.renewInternalCertsLoop()
	if p.isCaddyEngine() {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		// The proxy container may still be starting; retry a few times.
		for attempt := 1; attempt <= 5; attempt++ {
			res, err := p.ProxySyncNow(ctx, proxy.Reason{Kind: "startup"}, true)
			if err == nil {
				log.Printf("proxy: %s config applied at start-up (generation %s)", p.ProxyEngine(), res.Generation)
				return
			}
			log.Printf("proxy: start-up apply attempt %d failed: %v", attempt, err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Duration(attempt*5) * time.Second):
			}
		}
	}()
}

// ProxyRequestsTotal reads the active proxy's request counter for the dashboard's Site Traffic
// chart. Caddy has none here (its Prometheus endpoint is scraped instead), so it reports false.
func (p *Panel) ProxyRequestsTotal(ctx context.Context) (uint64, bool) {
	drv, err := p.ProxyDriver()
	if err != nil {
		return 0, false
	}
	rc, ok := drv.(proxy.RequestCounter)
	if !ok {
		return 0, false
	}
	total, err := rc.RequestsTotal(ctx)
	return total, err == nil
}
