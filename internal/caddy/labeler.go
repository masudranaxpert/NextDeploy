package caddy

import (
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"

	"panel/internal/db"
	"panel/internal/proxy"
)

// AppLabeler computes engine-specific Docker container labels for a service.
type AppLabeler = proxy.AppLabeler

func buildServiceLabelsWithLabeler(
	appID, projectName, slot, svcName string,
	sDomains []db.AppDomain,
	sPorts []HostPortBinding,
	svcPreview *db.AppDomain,
	isPreviewOnly bool,
	labeler AppLabeler,
) map[string]string {
	effectiveAppID := strings.TrimSpace(appID)
	if effectiveAppID == "" {
		for _, d := range sDomains {
			if strings.TrimSpace(d.AppID) != "" {
				effectiveAppID = strings.TrimSpace(d.AppID)
				break
			}
		}
	}
	if effectiveAppID == "" && svcPreview != nil && strings.TrimSpace(svcPreview.AppID) != "" {
		effectiveAppID = strings.TrimSpace(svcPreview.AppID)
	}
	if effectiveAppID == "" {
		effectiveAppID = strings.TrimSuffix(strings.TrimSuffix(strings.TrimSpace(projectName), "-blue"), "-green")
	}

	sitesByPort := make(map[int][]proxy.Site)
	if isPreviewOnly {
		if svcPreview != nil {
			site, port := domainToProxySite(*svcPreview, effectiveAppID, svcName, slot, true)
			sitesByPort[port] = append(sitesByPort[port], site)
		}
	} else {
		for _, d := range sortedDomains(sDomains) {
			site, port := domainToProxySite(d, effectiveAppID, svcName, slot, false)
			sitesByPort[port] = append(sitesByPort[port], site)
		}
		for _, hp := range sPorts {
			if hp.Direct || hp.HostPort <= 0 {
				continue
			}
			cPort := hp.ContainerPort
			if cPort <= 0 {
				cPort = hp.HostPort
			}
			addr := fmt.Sprintf(":%d", hp.HostPort)
			if hp.BindIP != "" && hp.BindIP != "0.0.0.0" {
				addr = "http://" + net.JoinHostPort(hp.BindIP, strconv.Itoa(hp.HostPort))
			}
			up := proxy.UpstreamRef{
				Name:          proxy.UpstreamName(effectiveAppID, svcName, cPort, slot),
				Service:       svcName,
				Port:          cPort,
				Scheme:        "http",
				LongStreams:   true,
				LBTryDuration: "5s",
				FailDuration:  "5s",
			}
			sitesByPort[cPort] = append(sitesByPort[cPort], proxy.Site{
				ID:       fmt.Sprintf("gateway:%s:%d", effectiveAppID, hp.HostPort),
				AppID:    effectiveAppID,
				Hosts:    []string{addr},
				TLS:      proxy.TLSNone,
				Kind:     proxy.SiteKindAppProxy,
				Upstream: &up,
			})
		}
	}

	ports := make([]int, 0, len(sitesByPort))
	for port := range sitesByPort {
		ports = append(ports, port)
	}
	sort.Ints(ports)

	out := map[string]string{}
	caddySiteIdx := 0
	for _, port := range ports {
		up := proxy.UpstreamRef{
			Name:        proxy.UpstreamName(effectiveAppID, svcName, port, slot),
			Service:     svcName,
			Port:        port,
			Scheme:      "http",
			LongStreams: true,
		}
		if slot != "" {
			up.LBTryDuration = "5s"
			up.FailDuration = "5s"
		}
		lbl := labeler(sitesByPort[port], up)
		if len(lbl) == 0 {
			continue
		}
		caddySiteIdx = mergeEngineLabels(out, lbl, caddySiteIdx)
	}
	return out
}

func domainToProxySite(d db.AppDomain, appID, svcName, slot string, isPreview bool) (proxy.Site, int) {
	domain := cleanQuotedValue(d.Domain)
	hosts := make([]string, 0, 2)
	if domain != "" {
		hosts = append(hosts, domain)
		if d.EnableWWW {
			hosts = append(hosts, "www."+domain)
		}
	}
	behindShield := d.ShieldNodeID != nil && *d.ShieldNodeID > 0
	tlsMode := proxy.TLSNone
	if d.EnableHTTPS || behindShield {
		if behindShield || proxy.ShouldUseInternalTLS(domain) {
			tlsMode = proxy.TLSInternal
		} else {
			tlsMode = proxy.TLSAuto
		}
	}
	port := d.Port
	if port <= 0 {
		port = 80
	}
	up := proxy.UpstreamRef{
		Name:        proxy.UpstreamName(appID, svcName, port, slot),
		Service:     svcName,
		Port:        port,
		Scheme:      "http",
		LongStreams: true,
	}
	if slot != "" {
		up.LBTryDuration = "5s"
		up.FailDuration = "5s"
	}

	var pathRoutes []proxy.PathRoute
	for _, r := range normalizedRoutes(d) {
		if r.Direct {
			pathRoutes = append(pathRoutes, proxy.PathRoute{
				Prefix:      r.Path,
				StripPrefix: true,
				Priority:    r.Priority,
				Handler: proxy.Handler{
					Kind: proxy.HandlerStaticDir,
					Static: &proxy.StaticDirSpec{
						Root: r.Root,
						Hide: []string{".git", ".env*", "*.sql", "*.db", "*.sqlite", "*.key", "*.sock"},
					},
				},
			})
			continue
		}
		upCopy := up
		pathRoutes = append(pathRoutes, proxy.PathRoute{
			Prefix:   r.Path,
			Priority: r.Priority,
			Handler: proxy.Handler{
				Kind:     proxy.HandlerProxy,
				Upstream: &upCopy,
			},
		})
	}

	return proxy.Site{
		ID:           fmt.Sprintf("app:%s:dom:%d", appID, d.ID),
		AppID:        appID,
		DomainID:     d.ID,
		Slot:         slot,
		IsPreview:    isPreview,
		Hosts:        hosts,
		TLS:          tlsMode,
		BehindShield: behindShield,
		TrustedProxy: d.TrustedProxy,
		Kind:         proxy.SiteKindAppProxy,
		Paths:        pathRoutes,
		Upstream:     &up,
	}, port
}

func mergeEngineLabels(dst, src map[string]string, caddyIdx int) int {
	hasCaddy := false
	maxGroup := -1
	for k := range src {
		if k == "caddy" || strings.HasPrefix(k, "caddy.") {
			hasCaddy = true
			if maxGroup < 0 {
				maxGroup = 0
			}
		} else if strings.HasPrefix(k, "caddy_") {
			hasCaddy = true
			rest := strings.TrimPrefix(k, "caddy_")
			if dot := strings.IndexByte(rest, '.'); dot >= 0 {
				rest = rest[:dot]
			}
			if n, err := strconv.Atoi(rest); err == nil && n > maxGroup {
				maxGroup = n
			}
		}
	}
	if !hasCaddy {
		for k, v := range src {
			dst[k] = v
		}
		return caddyIdx
	}

	keys := make([]string, 0, len(src))
	for k := range src {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		newKey := remapCaddyPrefix(k, caddyIdx)
		dst[newKey] = src[k]
	}
	return caddyIdx + maxGroup + 1
}

func remapCaddyPrefix(key string, offset int) string {
	if offset == 0 {
		return key
	}
	if key == "caddy" {
		return "caddy_" + strconv.Itoa(offset)
	}
	if strings.HasPrefix(key, "caddy.") {
		return "caddy_" + strconv.Itoa(offset) + strings.TrimPrefix(key, "caddy")
	}
	if strings.HasPrefix(key, "caddy_") {
		rest := strings.TrimPrefix(key, "caddy_")
		if dot := strings.IndexByte(rest, '.'); dot >= 0 {
			if n, err := strconv.Atoi(rest[:dot]); err == nil {
				return "caddy_" + strconv.Itoa(offset+n) + rest[dot:]
			}
		} else if n, err := strconv.Atoi(rest); err == nil {
			return "caddy_" + strconv.Itoa(offset+n)
		}
	}
	return key
}
