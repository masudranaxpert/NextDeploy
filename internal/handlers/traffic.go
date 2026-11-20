package handlers

import (
	"context"
	"log"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"panel/internal/db"
	"panel/internal/proxy"
	"panel/internal/proxy/angie"
	pstate "panel/internal/proxy/state"
	"panel/internal/sysinfo"
	"panel/internal/traffic"

	"github.com/gofiber/fiber/v2"
)

// Per-site traffic: once a minute the panel reads the reverse proxy's own running request
// counters (Angie status zones, Caddy per-host metrics) and keeps the per-minute difference
// in memory (internal/traffic). Requests themselves are never touched.

const (
	trafficInterval   = time.Minute
	trafficMapRefresh = 5 * time.Minute
	keyPanel          = "panel"
	keyAll            = "all"
)

type trafficMap struct {
	engine string
	zones  map[string]string // Angie zone -> key
	hosts  map[string]string // host name -> key
	at     time.Time
}

type trafficCollector struct {
	store *traffic.Store
	mu    sync.Mutex
	m     trafficMap
	once  sync.Once
	// caddyPerHostAt is the last time Caddy reported per-host numbers.
	caddyPerHostAt time.Time
}

var trafficCol = &trafficCollector{store: traffic.NewStore()}

// trafficKey groups proxy sites by what the user sees: a project or PHP site (both are apps),
// the panel itself, or a personal proxy route.
func trafficKey(s proxy.Site) string {
	switch {
	case s.AppID != "":
		return "app:" + s.AppID
	case s.ID == "panel":
		return keyPanel
	case s.RouteID > 0:
		return "route:" + strconv.FormatInt(s.RouteID, 10)
	default:
		return "site:" + s.ID
	}
}

func buildTrafficMap(engine string, st *proxy.State) trafficMap {
	m := trafficMap{engine: engine, zones: map[string]string{}, hosts: map[string]string{}, at: time.Now()}
	if st == nil {
		return m
	}
	add := func(s proxy.Site) {
		k := trafficKey(s)
		m.zones[angie.ZoneName(s.ID)] = k
		for _, h := range s.Hosts {
			if h = strings.ToLower(strings.TrimSpace(h)); h != "" {
				m.hosts[h] = k
			}
		}
	}
	if st.Panel != nil {
		p := *st.Panel
		p.ID = "panel"
		add(p)
	}
	for _, s := range st.Sites {
		add(s)
	}
	for _, gw := range st.Gateways {
		k := "gw:" + strconv.Itoa(gw.Port)
		if gw.AppID != "" {
			k = "app:" + gw.AppID
		}
		m.zones[angie.GatewayZoneName(gw.Port)] = k
	}
	return m
}

// StartTrafficCollector starts the once-a-minute reader. Safe to call more than once.
func (p *Panel) StartTrafficCollector() {
	trafficCol.once.Do(func() {
		go func() {
			// Start on a minute boundary so each bucket covers one clock minute.
			time.Sleep(time.Until(time.Now().Truncate(time.Minute).Add(time.Minute + 2*time.Second)))
			p.collectTraffic()
			t := time.NewTicker(trafficInterval)
			defer t.Stop()
			for range t.C {
				p.collectTraffic()
			}
		}()
	})
}

func (p *Panel) trafficMapping(ctx context.Context, engine string, force bool) trafficMap {
	trafficCol.mu.Lock()
	cur := trafficCol.m
	trafficCol.mu.Unlock()
	if !force && cur.engine == engine && time.Since(cur.at) < trafficMapRefresh {
		return cur
	}
	// Only site IDs, hosts and owners are needed, so no app target resolution (docker calls).
	st, err := pstate.LoadAndBuild(ctx, p.DB, pstate.LoadOptions{})
	if err != nil {
		log.Printf("traffic: build site map: %v", err)
		return cur
	}
	m := buildTrafficMap(engine, st)
	trafficCol.mu.Lock()
	trafficCol.m = m
	trafficCol.mu.Unlock()
	return m
}

func (p *Panel) collectTraffic() {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("traffic: collector panic: %v", r)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	engine := p.ProxyEngine()
	now := time.Now()

	drv, err := p.ProxyDriver()
	if err == nil {
		if zc, ok := drv.(proxy.ZoneCounter); ok {
			zones, zerr := zc.ZoneCounters(ctx)
			if zerr == nil {
				m := p.trafficMapping(ctx, engine, false)
				totals, unknown := sumByKey(zones, m.zones)
				if unknown {
					// A site was added since the map was built; map it now (at most once a minute).
					m = p.trafficMapping(ctx, engine, true)
					totals, _ = sumByKey(zones, m.zones)
				}
				trafficCol.store.Observe(now, engine, totals, true, "")
				return
			}
			// The proxy is down or restarting: skip this minute instead of recording a gap
			// as "no per-site data"; the next reading catches up the difference.
			return
		}
	}
	if engine == "caddy" {
		if hosts, ok := sysinfo.CaddyHostCounters(); ok {
			m := p.trafficMapping(ctx, engine, false)
			totals := map[string]traffic.Counters{}
			for h, c := range hosts {
				k, ok := m.hosts[h]
				if !ok {
					continue // hosts Caddy saw that no site owns (IP access, scanners)
				}
				t := totals[k]
				t.Requests += c.Requests
				t.S2xx += c.S2xx
				t.S3xx += c.S3xx
				t.S4xx += c.S4xx
				t.S5xx += c.S5xx
				totals[k] = t
			}
			trafficCol.store.Observe(now, engine, totals, true, "")
			trafficCol.mu.Lock()
			trafficCol.caddyPerHostAt = now
			trafficCol.mu.Unlock()
			return
		}
		// caddy-docker-proxy rewrites Caddy's config when containers change, which turns
		// per-host metrics off until they are switched on again (CaddyHostCounters does that).
		// Skip such short gaps instead of flipping the page to "total only".
		trafficCol.mu.Lock()
		recent := now.Sub(trafficCol.caddyPerHostAt) < 15*time.Minute
		trafficCol.mu.Unlock()
		if recent {
			return
		}
	}
	// Engines without per-site counters: keep the overall number so the page still says
	// something useful.
	note := "This proxy does not count requests per site, so only the total is shown."
	if engine == "caddy" {
		note = "Caddy is not reporting per-site numbers (its metrics or per-host metrics are off), so only the total is shown."
	}
	if total, ok := p.ProxyRequestsTotal(ctx); ok {
		trafficCol.store.Observe(now, engine, map[string]traffic.Counters{keyAll: {Requests: total}}, false, note)
	} else {
		trafficCol.store.Observe(now, engine, map[string]traffic.Counters{}, false, note)
	}
}

// sumByKey adds up zone totals per site key; unknown reports a zone no site maps to.
func sumByKey(zones map[string]proxy.ZoneStats, zoneToKey map[string]string) (map[string]traffic.Counters, bool) {
	out := map[string]traffic.Counters{}
	unknown := false
	for zone, z := range zones {
		k, ok := zoneToKey[zone]
		if !ok {
			if strings.HasPrefix(zone, "nd_") {
				unknown = true
			}
			continue
		}
		t := out[k]
		t.Requests += z.Requests
		t.S2xx += z.S2xx
		t.S3xx += z.S3xx
		t.S4xx += z.S4xx
		t.S5xx += z.S5xx
		out[k] = t
	}
	return out, unknown
}

type trafficSite struct {
	traffic.SiteSummary
	Name    string   `json:"name"`
	Kind    string   `json:"kind"` // project | php | wordpress | static | panel | route | other
	AppID   string   `json:"app_id,omitempty"`
	Domains []string `json:"domains,omitempty"`
	Owner   string   `json:"owner,omitempty"`
}

// GET /api/v1/traffic?window=30
func (p *Panel) TrafficSummary(c *fiber.Ctx) error {
	u, ok := currentUser(c)
	if !ok {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"ok": false, "error": "unauthorized"})
	}
	p.StartTrafficCollector()
	ctx := c.UserContext()
	window, _ := strconv.Atoi(c.Query("window", "30"))
	switch window {
	case 15, 30, 60:
	default:
		window = 30
	}
	isAdmin := u.Role == db.RoleAdmin

	var apps []db.App
	var err error
	if isAdmin {
		apps, err = p.DB.ListApps(ctx)
	} else {
		apps, err = p.DB.ListAppsForUser(ctx, u.ID)
	}
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"ok": false, "error": err.Error()})
	}
	byID := make(map[string]db.App, len(apps))
	for _, a := range apps {
		byID[a.ID] = a
	}
	phpType := map[string]string{}
	if sites, err := p.DB.ListPHPSites(ctx); err == nil {
		for _, s := range sites {
			phpType[s.AppID] = s.SiteType
		}
	}
	owners := map[int64]string{}
	if isAdmin {
		if users, err := p.DB.ListUsers(ctx); err == nil {
			for _, us := range users {
				owners[us.ID] = us.Username
			}
		}
	}
	domains := map[string][]string{}
	if all, err := p.DB.ListAllAppDomains(ctx); err == nil {
		for _, d := range all {
			if _, mine := byID[d.AppID]; mine && d.Domain != "" {
				domains[d.AppID] = append(domains[d.AppID], d.Domain)
			}
		}
	}

	include := func(key string) bool {
		if strings.HasPrefix(key, "app:") {
			_, ok := byID[strings.TrimPrefix(key, "app:")]
			return ok
		}
		return isAdmin // panel, routes, gateways and the overall total are admin-only
	}
	snap := trafficCol.store.Summary(time.Now(), window, include)

	sites := make([]trafficSite, 0, len(snap.Sites))
	for _, s := range snap.Sites {
		ts := trafficSite{SiteSummary: s}
		switch {
		case strings.HasPrefix(s.Key, "app:"):
			a := byID[strings.TrimPrefix(s.Key, "app:")]
			ts.AppID, ts.Name, ts.Domains = a.ID, a.Name, domains[a.ID]
			if ts.Name == "" {
				ts.Name = a.ID
			}
			ts.Kind = "project"
			if t, ok := phpType[a.ID]; ok {
				ts.Kind = "php"
				if t == "wordpress" || t == "static" {
					ts.Kind = t
				}
			}
			if isAdmin {
				ts.Owner = owners[a.OwnerID]
			}
		case s.Key == keyPanel:
			ts.Name, ts.Kind = "Panel", "panel"
			if d := strings.TrimSpace(p.DB.GetSetting(ctx, "panel_domain")); d != "" {
				ts.Domains = []string{d}
			}
		case s.Key == keyAll:
			ts.Name, ts.Kind = "All sites", "other"
		case strings.HasPrefix(s.Key, "route:"):
			ts.Name, ts.Kind = "Proxy route #"+strings.TrimPrefix(s.Key, "route:"), "route"
		case strings.HasPrefix(s.Key, "gw:"):
			ts.Name, ts.Kind = "Port "+strings.TrimPrefix(s.Key, "gw:"), "other"
		default:
			ts.Name, ts.Kind = strings.TrimPrefix(s.Key, "site:"), "other"
		}
		sort.Strings(ts.Domains)
		sites = append(sites, ts)
	}

	return c.JSON(fiber.Map{
		"ok":               true,
		"engine":           snap.Source,
		"per_site":         snap.PerSite,
		"note":             snap.Note,
		"window":           window,
		"since":            snap.Since,
		"updated":          snap.Updated,
		"minutes":          snap.Minutes,
		"per_minute":       snap.PerMin,
		"total":            snap.Total,
		"coverage_minutes": snap.Coverage,
		"sites":            sites,
	})
}
