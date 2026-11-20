package angie

import (
	"fmt"
	"sort"
	"strings"

	"panel/internal/proxy"
	"panel/internal/proxy/pki"
)

type acmeClientSpec struct {
	Name string
}

type tlsResolution struct {
	Mode         proxy.TLSMode
	ACMEClient   string
	ManualCert   *ManualCert
	InternalStem string
}

type renderPlan struct {
	state       *proxy.State
	useZstd     bool
	useWPCache  bool
	cfGlobal    bool
	upstreams   map[string]proxy.UpstreamRef
	acmeClients map[string]acmeClientSpec
	siteTLS     map[string]tlsResolution
}

// Render translates the desired-state IR into Angie configuration files keyed by
// relative path inside the active configuration root.
func (d *Driver) Render(st *proxy.State) (proxy.Files, error) {
	if st == nil {
		st = &proxy.State{}
	}
	plan, orderedSites, err := d.buildRenderPlan(st)
	if err != nil {
		return nil, err
	}

	files := make(proxy.Files)
	files["angie.conf"] = renderMainConfig(d, st, plan.useZstd)

	globalConf, err := renderGlobalConfig(d, st, plan)
	if err != nil {
		return nil, err
	}
	files["00-global.conf"] = globalConf
	files["snippets/proxy.conf"] = renderProxySnippet()
	files["snippets/hide.conf"] = renderHideSnippet()

	upNames := make([]string, 0, len(plan.upstreams))
	for name := range plan.upstreams {
		upNames = append(upNames, name)
	}
	sort.Strings(upNames)
	for _, name := range upNames {
		files["upstreams/"+name+".conf"] = renderUpstream(name, plan.upstreams[name])
	}

	for _, site := range orderedSites {
		if site.Kind == proxy.SiteKindPHP || site.Kind == proxy.SiteKindStatic {
			if site.PHP != nil && site.PHP.Suspended {
				continue
			}
		}
		if len(cleanHosts(site.Hosts)) == 0 {
			continue
		}
		tlsInfo := plan.siteTLS[site.ID]
		siteFiles, err := d.renderSiteFiles(site, tlsInfo, plan.cfGlobal)
		if err != nil {
			return nil, err
		}
		for rel, content := range siteFiles {
			files[rel] = content
		}
	}

	if gwConf := renderGatewayServers(st.Gateways); gwConf != "" {
		files["sites/gateways.conf"] = gwConf
	}

	return files, nil
}

func (d *Driver) buildRenderPlan(st *proxy.State) (*renderPlan, []proxy.Site, error) {
	plan := &renderPlan{
		state:       st,
		useZstd:     d != nil && d.forceZstd,
		cfGlobal:    strings.EqualFold(strings.TrimSpace(st.Global.RealIP), "cloudflare") || st.Global.DirectCloudflare,
		upstreams:   make(map[string]proxy.UpstreamRef),
		acmeClients: make(map[string]acmeClientSpec),
		siteTLS:     make(map[string]tlsResolution),
	}

	var orderedSites []proxy.Site
	if st.Panel != nil {
		orderedSites = append(orderedSites, *st.Panel)
	}
	orderedSites = append(orderedSites, st.Sites...)

	for _, site := range orderedSites {
		if (site.Kind == proxy.SiteKindPHP || site.Kind == proxy.SiteKindStatic) &&
			site.PHP != nil && site.PHP.Suspended {
			continue
		}
		hosts := cleanHosts(site.Hosts)
		if len(hosts) == 0 {
			continue
		}

		if siteUsesZstd(site) {
			plan.useZstd = true
		}
		if site.Kind == proxy.SiteKindPHP && site.PHP != nil &&
			site.PHP.Cache != nil && site.PHP.Cache.WordPressPageCache {
			plan.useWPCache = true
		}

		collectSiteUpstreams(site, plan.upstreams)

		res, err := d.resolveSiteTLS(site, hosts)
		if err != nil {
			return nil, nil, err
		}
		plan.siteTLS[site.ID] = res
		if res.ACMEClient != "" {
			plan.acmeClients[res.ACMEClient] = acmeClientSpec{Name: res.ACMEClient}
		}
	}

	for _, gw := range st.Gateways {
		if gw.Direct || gw.Port <= 0 || gw.Upstream == nil {
			continue
		}
		addDockerUpstream(plan.upstreams, gw.Upstream)
	}

	return plan, orderedSites, nil
}

func (d *Driver) renderSiteFiles(site proxy.Site, tlsInfo tlsResolution, cfGlobal bool) (proxy.Files, error) {
	stem := SafeFileStem(site.ID)
	switch site.Kind {
	case proxy.SiteKindRedirect:
		conf, err := d.renderRedirectSite(site, tlsInfo, cfGlobal)
		if err != nil {
			return nil, err
		}
		return proxy.Files{"sites/" + stem + ".conf": conf}, nil
	case proxy.SiteKindExternalProxy:
		conf, err := d.renderExternalProxySite(site, tlsInfo, cfGlobal)
		if err != nil {
			return nil, err
		}
		return proxy.Files{"sites/" + stem + ".conf": conf}, nil
	case proxy.SiteKindPHP, proxy.SiteKindStatic:
		return d.renderPHPSite(site, tlsInfo, cfGlobal)
	default:
		conf, err := d.renderAppProxySite(site, tlsInfo, cfGlobal)
		if err != nil {
			return nil, err
		}
		return proxy.Files{"sites/" + stem + ".conf": conf}, nil
	}
}

func (d *Driver) resolveSiteTLS(site proxy.Site, hosts []string) (tlsResolution, error) {
	mode := effectiveTLSMode(site)
	switch mode {
	case proxy.TLSNone:
		return tlsResolution{Mode: proxy.TLSNone}, nil
	case proxy.TLSInternal:
		stem, err := pki.SafeSiteStem(site.ID)
		if err != nil {
			return tlsResolution{}, fmt.Errorf("angie site %q internal TLS stem: %w", site.ID, err)
		}
		return tlsResolution{
			Mode:         proxy.TLSInternal,
			InternalStem: stem,
		}, nil
	case proxy.TLSAuto:
		if cert := d.lookupImportedCert(site.ID, hosts); cert != nil {
			return tlsResolution{
				Mode:       proxy.TLSAuto,
				ManualCert: cert,
			}, nil
		}
		return tlsResolution{
			Mode:       proxy.TLSAuto,
			ACMEClient: ACMEClientName(site.ID),
		}, nil
	default:
		return tlsResolution{}, fmt.Errorf("angie site %q: unsupported TLS mode %q", site.ID, mode)
	}
}

func (d *Driver) lookupImportedCert(siteID string, hosts []string) *ManualCert {
	if d == nil || len(d.importedCerts) == 0 {
		return nil
	}
	if cert, ok := d.importedCerts[strings.ToLower(strings.TrimSpace(siteID))]; ok {
		cp := cert
		return &cp
	}
	for _, h := range hosts {
		if cert, ok := d.importedCerts[strings.ToLower(strings.TrimSpace(h))]; ok {
			cp := cert
			return &cp
		}
	}
	return nil
}

func effectiveTLSMode(site proxy.Site) proxy.TLSMode {
	if site.TLS != "" {
		return site.TLS
	}
	if site.BehindShield {
		return proxy.TLSInternal
	}
	return proxy.TLSNone
}

func siteUsesZstd(site proxy.Site) bool {
	if site.PHP != nil {
		for _, c := range site.PHP.Compression {
			if strings.EqualFold(strings.TrimSpace(c), "zstd") {
				return true
			}
		}
		for _, p := range site.PHP.Precompressed {
			if strings.EqualFold(strings.TrimSpace(p), "zstd") {
				return true
			}
		}
	}
	for _, pr := range site.Paths {
		if pr.Handler.Static != nil {
			for _, p := range pr.Handler.Static.Precompressed {
				if strings.EqualFold(strings.TrimSpace(p), "zstd") {
					return true
				}
			}
		}
	}
	return false
}

func collectSiteUpstreams(site proxy.Site, dst map[string]proxy.UpstreamRef) {
	if site.Kind == proxy.SiteKindExternalProxy || site.Kind == proxy.SiteKindRedirect ||
		site.Kind == proxy.SiteKindPHP || site.Kind == proxy.SiteKindStatic {
		return
	}
	addDockerUpstream(dst, site.Upstream)
	if site.Default != nil {
		addDockerUpstream(dst, site.Default.Upstream)
	}
	for _, pr := range site.Paths {
		if pr.Handler.Kind == proxy.HandlerProxy {
			addDockerUpstream(dst, pr.Handler.Upstream)
		}
	}
}

func addDockerUpstream(dst map[string]proxy.UpstreamRef, up *proxy.UpstreamRef) {
	if up == nil || strings.TrimSpace(up.External) != "" {
		return
	}
	name := SanitizeIdent(up.Name)
	if name == "" {
		return
	}
	if _, exists := dst[name]; !exists {
		cp := *up
		cp.Name = name
		dst[name] = cp
	}
	// Blue/green: the standby slot's containers start (and register through their Docker
	// labels) before the cutover re-renders the config. Declare the other slot's upstream too,
	// so Angie can attach them right away instead of logging "cannot find http upstream" and
	// leaving the new slot's upstream empty ("no live upstreams", 503) until the next reload.
	if sib := siblingSlotUpstream(name); sib != "" {
		if _, exists := dst[sib]; !exists {
			cp := *up
			cp.Name = sib
			dst[sib] = cp
		}
	}
}

// siblingSlotUpstream returns the other blue/green slot's name of a slot upstream, or "".
func siblingSlotUpstream(name string) string {
	switch {
	case strings.HasSuffix(name, "_blue"):
		return strings.TrimSuffix(name, "_blue") + "_green"
	case strings.HasSuffix(name, "_green"):
		return strings.TrimSuffix(name, "_green") + "_blue"
	}
	return ""
}

func cleanHosts(hosts []string) []string {
	seen := make(map[string]bool, len(hosts))
	out := make([]string, 0, len(hosts))
	for _, raw := range hosts {
		h := strings.ToLower(proxy.CleanQuotedValue(raw))
		if h == "" || proxy.HasUnsafeLabelChars(h) || seen[h] {
			continue
		}
		seen[h] = true
		out = append(out, h)
	}
	return out
}
