package caddy

import (
	"net"
	"net/url"
	"strconv"
	"strings"

	icaddy "panel/internal/caddy"
	"panel/internal/db"
	"panel/internal/proxy"
)

type siteDomainMeta struct {
	site       proxy.Site
	extraHosts []string
}

func buildAppProxyLabels(sites []proxy.Site, up proxy.UpstreamRef) map[string]string {
	isSlot := isSlotBatch(sites, up)
	if !isSlot && len(sites) == 1 && !needsCustomAppPaths(sites[0]) && !isGatewaySite(sites[0]) {
		domains, metas := sitesToAppDomains(sites, up)
		if len(domains) == 0 {
			return map[string]string{}
		}
		labels := icaddy.GenerateLabels(domains[0])
		applySitePostOverrides(labels, metas, up, false)
		return labels
	}
	if !isSlot && !anyCustomAppPaths(sites) {
		domains, metas := sitesToAppDomains(sites, up)
		if len(domains) == 0 {
			return map[string]string{}
		}
		sortDomainMetas(domains, metas)
		labels := icaddy.GenerateServiceLabels(domains)
		applySitePostOverrides(labels, metas, up, false)
		return labels
	}

	var regularSites, previewSites []proxy.Site
	var gateways []icaddy.HostPortBinding
	for _, s := range sites {
		if hp, ok := gatewayBindingFromSite(s, up); ok {
			gateways = append(gateways, hp)
			continue
		}
		if s.IsPreview {
			previewSites = append(previewSites, s)
			continue
		}
		regularSites = append(regularSites, s)
	}

	if isSlot && len(regularSites) == 0 && len(previewSites) == 1 && len(gateways) == 0 && !needsCustomAppPaths(previewSites[0]) {
		domains, metas := sitesToAppDomains(previewSites, up)
		if len(domains) == 0 {
			return map[string]string{}
		}
		labels := icaddy.GenerateSlotLabels(nil, nil, &domains[0], true)
		applySitePostOverrides(labels, metas, up, true)
		return labels
	}

	if isSlot && len(previewSites) == 0 && !anyCustomAppPaths(regularSites) {
		domains, metas := sitesToAppDomains(regularSites, up)
		if len(domains) == 0 && len(gateways) == 0 {
			return map[string]string{}
		}
		sortDomainMetas(domains, metas)
		labels := icaddy.GenerateSlotLabels(domains, gateways, nil, false)
		applySitePostOverrides(labels, metas, up, true)
		return labels
	}

	out := map[string]string{}
	idx := 0
	sortSitesByDomain(regularSites)
	sortSitesByDomain(previewSites)
	allDomains := append(append([]proxy.Site(nil), regularSites...), previewSites...)
	for _, s := range allDomains {
		sub := renderCustomAppSite(s, up, isSlot)
		if len(sub) == 0 {
			continue
		}
		to := "caddy"
		if idx > 0 {
			to = "caddy_" + strconv.Itoa(idx)
		}
		remapAndMerge(out, sub, "caddy", to)
		idx++
	}
	if len(gateways) > 0 {
		gwLabels := icaddy.GenerateSlotLabels(nil, gateways, nil, false)
		for i := range gateways {
			from := "caddy"
			if i > 0 {
				from = "caddy_" + strconv.Itoa(i)
			}
			to := "caddy"
			if idx > 0 {
				to = "caddy_" + strconv.Itoa(idx)
			}
			remapAndMerge(out, extractPrefixLabels(gwLabels, from), from, to)
			idx++
		}
	}
	return out
}

func cleanedHosts(hosts []string) []string {
	out := make([]string, 0, len(hosts))
	for _, raw := range hosts {
		if h := icaddy.CleanQuotedValue(raw); h != "" {
			out = append(out, h)
		}
	}
	return out
}

func splitHosts(hosts []string) (domain string, enableWWW bool, extraHosts []string) {
	clean := cleanedHosts(hosts)
	if len(clean) == 0 {
		return "", false, nil
	}
	domain = clean[0]
	if len(clean) == 2 && strings.EqualFold(clean[1], "www."+domain) {
		return domain, true, nil
	}
	if len(clean) > 1 {
		return domain, false, clean
	}
	return domain, false, nil
}

func isSlotBatch(sites []proxy.Site, up proxy.UpstreamRef) bool {
	if up.LBTryDuration != "" || up.FailDuration != "" {
		return true
	}
	for _, s := range sites {
		if s.Slot != "" || s.IsPreview || isGatewaySite(s) {
			return true
		}
		if s.Upstream != nil && (s.Upstream.LBTryDuration != "" || s.Upstream.FailDuration != "") {
			return true
		}
	}
	return false
}

func isGatewaySite(s proxy.Site) bool {
	_, ok := gatewayBindingFromSite(s, proxy.UpstreamRef{})
	return ok
}

func gatewayBindingFromSite(s proxy.Site, up proxy.UpstreamRef) (icaddy.HostPortBinding, bool) {
	if s.Kind != "" && s.Kind != proxy.SiteKindAppProxy {
		return icaddy.HostPortBinding{}, false
	}
	hosts := cleanedHosts(s.Hosts)
	if len(hosts) != 1 {
		return icaddy.HostPortBinding{}, false
	}
	h := hosts[0]
	var bindIP string
	var port int

	switch {
	case strings.HasPrefix(h, ":"):
		p, err := strconv.Atoi(strings.TrimPrefix(h, ":"))
		if err != nil || p <= 0 || p > 65535 {
			return icaddy.HostPortBinding{}, false
		}
		bindIP, port = "0.0.0.0", p
	case strings.HasPrefix(strings.ToLower(h), "http://"):
		u, err := url.Parse(h)
		if err != nil || u.Port() == "" || net.ParseIP(u.Hostname()) == nil {
			return icaddy.HostPortBinding{}, false
		}
		p, err := strconv.Atoi(u.Port())
		if err != nil || p <= 0 || p > 65535 {
			return icaddy.HostPortBinding{}, false
		}
		bindIP, port = u.Hostname(), p
	case strings.HasPrefix(s.ID, "gateway:"):
		host, portStr, err := net.SplitHostPort(h)
		if err != nil {
			return icaddy.HostPortBinding{}, false
		}
		p, err := strconv.Atoi(portStr)
		if err != nil || p <= 0 || p > 65535 {
			return icaddy.HostPortBinding{}, false
		}
		if host == "" {
			host = "0.0.0.0"
		}
		bindIP, port = host, p
	default:
		return icaddy.HostPortBinding{}, false
	}

	cPort := resolveUpstreamPort(s, up)
	if cPort <= 0 {
		cPort = port
	}
	return icaddy.HostPortBinding{
		HostPort:      port,
		ContainerPort: cPort,
		BindIP:        bindIP,
		Protocol:      "tcp",
		Service:       resolveUpstreamService(s, up),
		Direct:        false,
	}, true
}

func sitesToAppDomains(sites []proxy.Site, up proxy.UpstreamRef) ([]db.AppDomain, []siteDomainMeta) {
	domains := make([]db.AppDomain, 0, len(sites))
	metas := make([]siteDomainMeta, 0, len(sites))
	for _, s := range sites {
		domain, enableWWW, extraHosts := splitHosts(s.Hosts)
		if domain == "" {
			continue
		}
		var shieldPtr *int64
		if s.BehindShield {
			id := int64(1)
			shieldPtr = &id
		}
		d := db.AppDomain{
			ID:           s.DomainID,
			AppID:        s.AppID,
			Domain:       domain,
			Service:      resolveUpstreamService(s, up),
			Port:         resolveUpstreamPort(s, up),
			EnableHTTPS:  s.TLS != proxy.TLSNone || s.BehindShield || (s.PHP != nil && s.PHP.HTTPS),
			EnableWWW:    enableWWW,
			ShieldNodeID: shieldPtr,
			TrustedProxy: s.TrustedProxy,
		}
		if s.TLS == proxy.TLSNone && !s.BehindShield {
			d.EnableHTTPS = false
		}
		applySimpleStaticPaths(&d, s.Paths)
		domains = append(domains, d)
		metas = append(metas, siteDomainMeta{site: s, extraHosts: extraHosts})
	}
	return domains, metas
}

func sortDomainMetas(domains []db.AppDomain, metas []siteDomainMeta) {
	for i := 1; i < len(domains); i++ {
		for j := i; j > 0; j-- {
			li, ri := domains[j-1], domains[j]
			swap := false
			if li.ID > 0 && ri.ID > 0 {
				swap = ri.ID < li.ID
			} else {
				swap = strings.ToLower(ri.Domain) < strings.ToLower(li.Domain)
			}
			if !swap {
				break
			}
			domains[j], domains[j-1] = domains[j-1], domains[j]
			metas[j], metas[j-1] = metas[j-1], metas[j]
		}
	}
}

func sortSitesByDomain(sites []proxy.Site) {
	for i := 1; i < len(sites); i++ {
		for j := i; j > 0; j-- {
			li, ri := sites[j-1], sites[j]
			swap := false
			if li.DomainID > 0 && ri.DomainID > 0 {
				swap = ri.DomainID < li.DomainID
			} else {
				ld, _, _ := splitHosts(li.Hosts)
				rd, _, _ := splitHosts(ri.Hosts)
				swap = strings.ToLower(rd) < strings.ToLower(ld)
			}
			if !swap {
				break
			}
			sites[j], sites[j-1] = sites[j-1], sites[j]
		}
	}
}
