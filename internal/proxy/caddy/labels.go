package caddy

import (
	"fmt"
	"net"
	"reflect"
	"strconv"
	"strings"

	icaddy "panel/internal/caddy"
	"panel/internal/db"
	"panel/internal/proxy"
)

const defaultStaticHide = ".git .env* *.sql *.db *.sqlite *.key *.sock"

// AppLabelsWithGateways translates sites and HTTP gateway ports into caddy-docker-proxy
// Docker container labels, matching internal/caddy.GenerateSlotLabels when gateways are present.
func (d *Driver) AppLabelsWithGateways(sites []proxy.Site, gateways []proxy.GatewayPort, up proxy.UpstreamRef) map[string]string {
	gwSites := GatewaySites(gateways)
	if len(gwSites) == 0 {
		return d.AppLabels(sites, up)
	}
	combined := make([]proxy.Site, 0, len(sites)+len(gwSites))
	combined = append(combined, sites...)
	combined = append(combined, gwSites...)
	return d.AppLabels(combined, up)
}

// GatewaySites converts non-direct HTTP proxy.GatewayPort entries into synthetic
// gateway proxy.Site values consumable by Driver.AppLabels.
func GatewaySites(gateways []proxy.GatewayPort) []proxy.Site {
	if len(gateways) == 0 {
		return nil
	}
	out := make([]proxy.Site, 0, len(gateways))
	for _, g := range gateways {
		if s, ok := GatewaySite(g); ok {
			out = append(out, s)
		}
	}
	return out
}

// GatewaySite converts a single non-direct HTTP proxy.GatewayPort into a gateway
// proxy.Site for Driver.AppLabels. Direct L4 or UDP ports return ok=false.
func GatewaySite(g proxy.GatewayPort) (proxy.Site, bool) {
	if g.Direct || g.Port <= 0 || strings.EqualFold(strings.TrimSpace(g.Protocol), "udp") {
		return proxy.Site{}, false
	}
	bindIP := strings.TrimSpace(g.BindIP)
	addr := fmt.Sprintf(":%d", g.Port)
	if bindIP != "" && bindIP != "0.0.0.0" {
		addr = "http://" + net.JoinHostPort(bindIP, strconv.Itoa(g.Port))
	}
	cPort := g.ContainerPort
	if cPort <= 0 {
		cPort = g.Port
	}
	var up proxy.UpstreamRef
	if g.Upstream != nil {
		up = *g.Upstream
	}
	if up.Port <= 0 {
		up.Port = cPort
	}
	if up.LBTryDuration == "" {
		up.LBTryDuration = "5s"
	}
	if up.FailDuration == "" {
		up.FailDuration = "5s"
	}
	up.LongStreams = true
	return proxy.Site{
		ID:       fmt.Sprintf("gateway:%s:%d", g.AppID, g.Port),
		AppID:    g.AppID,
		Hosts:    []string{addr},
		TLS:      proxy.TLSNone,
		Kind:     proxy.SiteKindAppProxy,
		Upstream: &up,
	}, true
}

// AppLabels translates engine-neutral proxy.Site IR entries and an UpstreamRef
// into caddy-docker-proxy Docker container labels by delegating to internal/caddy.
func (d *Driver) AppLabels(sites []proxy.Site, up proxy.UpstreamRef) map[string]string {
	if len(sites) == 0 {
		return map[string]string{}
	}

	if allKind(sites, proxy.SiteKindPHP, proxy.SiteKindStatic) {
		return buildPHPSiteLabels(sites)
	}
	if allKind(sites, proxy.SiteKindRedirect, proxy.SiteKindExternalProxy) {
		return buildRouteSiteLabels(sites, up)
	}

	out := map[string]string{}
	var appSites, routeSites, phpSites []proxy.Site
	for _, s := range sites {
		switch s.Kind {
		case proxy.SiteKindPHP, proxy.SiteKindStatic:
			phpSites = append(phpSites, s)
		case proxy.SiteKindRedirect, proxy.SiteKindExternalProxy:
			routeSites = append(routeSites, s)
		default:
			appSites = append(appSites, s)
		}
	}
	if len(appSites) > 0 && len(routeSites) == 0 && len(phpSites) == 0 {
		return buildAppProxyLabels(appSites, up)
	}

	idx := 0
	mergeRemapped := func(sub map[string]string) {
		if len(sub) == 0 {
			return
		}
		to := "caddy"
		if idx > 0 {
			to = "caddy_" + strconv.Itoa(idx)
		}
		remapAndMerge(out, sub, "caddy", to)
		idx++
	}
	for _, s := range appSites {
		mergeRemapped(buildAppProxyLabels([]proxy.Site{s}, up))
	}
	for _, s := range routeSites {
		mergeRemapped(buildRouteSiteLabels([]proxy.Site{s}, up))
	}
	for _, s := range phpSites {
		mergeRemapped(buildPHPSiteLabels([]proxy.Site{s}))
	}
	return out
}

func allKind(sites []proxy.Site, kinds ...proxy.SiteKind) bool {
	for _, s := range sites {
		matched := false
		for _, k := range kinds {
			if s.Kind == k {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

func buildPHPSiteLabels(sites []proxy.Site) map[string]string {
	active := make([]proxy.Site, 0, len(sites))
	for _, s := range sites {
		if s.PHP != nil && s.PHP.Suspended {
			continue
		}
		if len(cleanedHosts(s.Hosts)) == 0 {
			continue
		}
		active = append(active, s)
	}
	if len(active) == 0 {
		return map[string]string{}
	}

	if samePHPSpec(active) {
		domains, metas := sitesToAppDomains(active, proxy.UpstreamRef{})
		if len(domains) == 0 {
			return map[string]string{}
		}
		sortDomainMetas(domains, metas)
		spec := active[0].PHP
		socket := ""
		if active[0].Kind == proxy.SiteKindPHP && spec != nil {
			socket = spec.Socket
		}
		root := ""
		var routing icaddy.SiteRouting
		if spec != nil {
			root = spec.DocRoot
			routing = icaddy.SiteRouting{
				FallbackMode:  spec.FallbackMode,
				FallbackEntry: spec.FallbackEntry,
				Rules:         append([]proxy.RoutingRule(nil), spec.Routing...),
			}
		}
		var sec icaddy.SiteSecurity
		if active[0].Security != nil {
			sec = *active[0].Security
		}
		labels := icaddy.PHPSiteLabelsWithRouting(domains, root, socket, sec, routing)
		applySitePostOverrides(labels, metas, proxy.UpstreamRef{}, false)
		return labels
	}

	out := map[string]string{}
	for i, s := range active {
		sub := buildPHPSiteLabels([]proxy.Site{s})
		to := "caddy"
		if len(active) > 1 {
			to = "caddy_" + strconv.Itoa(i)
		}
		remapAndMerge(out, sub, "caddy", to)
	}
	return out
}

func samePHPSpec(sites []proxy.Site) bool {
	if len(sites) <= 1 {
		return true
	}
	base := sites[0]
	for _, s := range sites[1:] {
		if s.Kind != base.Kind || !reflect.DeepEqual(s.Security, base.Security) {
			return false
		}
		if (s.PHP == nil) != (base.PHP == nil) {
			return false
		}
		if s.PHP != nil {
			a, b := *s.PHP, *base.PHP
			a.HTTPS, b.HTTPS = false, false
			if !reflect.DeepEqual(a, b) {
				return false
			}
		}
	}
	return true
}

func buildRouteSiteLabels(sites []proxy.Site, up proxy.UpstreamRef) map[string]string {
	out := map[string]string{}
	for i, s := range sites {
		domain, enableWWW, extraHosts := splitHosts(s.Hosts)
		if domain == "" {
			continue
		}
		var shieldPtr *int64
		if s.BehindShield {
			id := int64(1)
			shieldPtr = &id
		}
		route := db.ProxyRoute{
			ID:           s.RouteID,
			Domain:       domain,
			EnableHTTPS:  s.TLS != proxy.TLSNone || s.BehindShield,
			EnableWWW:    enableWWW,
			ShieldNodeID: shieldPtr,
			TrustedProxy: s.TrustedProxy,
		}
		switch s.Kind {
		case proxy.SiteKindExternalProxy:
			route.Kind = "proxy"
			route.Target = resolveExternalTarget(s, up)
			route.SkipTLSVerify = (s.Upstream != nil && s.Upstream.SkipVerify) || up.SkipVerify
		case proxy.SiteKindRedirect:
			route.Kind = "redirect"
			if redir := resolveRedirectSpec(s); redir != nil {
				route.Target = redir.Target
				route.RedirectCode = redir.Code
				route.KeepPath = redir.KeepPath
			}
			if route.RedirectCode == 0 {
				route.RedirectCode = 301
			}
		}
		sub := icaddy.RouteLabels(route)
		applySingleSitePostOverrides(sub, "caddy", s, up, extraHosts, false)
		to := "caddy"
		if len(sites) > 1 {
			to = "caddy_" + strconv.Itoa(i)
		}
		remapAndMerge(out, sub, "caddy", to)
	}
	return out
}
