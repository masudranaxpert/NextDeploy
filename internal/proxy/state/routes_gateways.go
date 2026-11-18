package state

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"panel/internal/db"
	"panel/internal/proxy"
)

func buildPanelSite(in BuildInput, g proxy.GlobalOptions) *proxy.Site {
	domain := proxy.CleanQuotedValue(in.PanelDomain)
	if domain == "" {
		return nil
	}
	behindShield := in.PanelShieldNodeID != nil && *in.PanelShieldNodeID > 0
	panelTrusted := g.RealIP == "cloudflare" || g.RealIP == "custom"

	var reqHeaders []proxy.HeaderOp
	if panelTrusted {
		reqHeaders = []proxy.HeaderOp{
			{Action: proxy.HeaderSet, Name: "X-Real-IP", Value: "{client_ip}"},
			{Action: proxy.HeaderSet, Name: "X-Forwarded-For", Value: "{client_ip}"},
		}
	}

	return &proxy.Site{
		ID:           "panel",
		Hosts:        buildHosts(domain, in.PanelEnableWWW),
		TLS:          resolveTLSMode(domain, in.PanelEnableHTTPS, behindShield),
		BehindShield: behindShield,
		TrustedProxy: panelTrusted,
		Headers:      appSecurityHeaders(),
		Kind:         proxy.SiteKindAppProxy,
		Upstream: &proxy.UpstreamRef{
			Name:           "nd_panel",
			Service:        "panel",
			Container:      "panel",
			Port:           8080,
			Scheme:         "http",
			LongStreams:    true,
			RequestHeaders: reqHeaders,
		},
	}
}

func buildPreviewSite(in BuildInput, app db.App, target AppTarget, shieldMap map[int64]db.ShieldNode) *proxy.Site {
	pinned := strings.TrimSpace(app.PinnedSlot)
	standby := strings.TrimSpace(app.StandbySlot)
	if pinned == "" || standby == "" || target.Preview == nil {
		return nil
	}
	cfg := target.Preview
	svc := proxy.CleanQuotedValue(cfg.Service)
	if svc == "" || strings.TrimSpace(cfg.Provider) == "" {
		return nil
	}

	shieldID := target.ShieldNodeID
	if shieldID != nil && *shieldID <= 0 {
		shieldID = nil
	}
	targetIP := strings.TrimSpace(target.PublicIP)
	if targetIP == "" && shieldID != nil {
		if n, ok := shieldMap[*shieldID]; ok {
			targetIP = strings.TrimSpace(n.IP)
		}
	}
	if targetIP == "" && shieldID == nil {
		targetIP = strings.TrimSpace(in.DefaultPublicIP)
	}

	var base string
	switch strings.ToLower(strings.TrimSpace(cfg.Provider)) {
	case "sslip.io", "nip.io":
		if shieldID != nil {
			base = FormatWildcardDomain(app.Name, targetIP, cfg.Provider, in.PanelID)
		} else {
			base = FormatWildcardDomain(app.Name, targetIP, cfg.Provider)
		}
	case "custom":
		base = strings.ToLower(strings.TrimSpace(cfg.Base))
	}
	if base == "" {
		return nil
	}
	previewDomain := SlotPreviewDomain(base, standby)
	if previewDomain == "" {
		return nil
	}

	port := cfg.Port
	if port <= 0 {
		port = 3000
	}
	behindShield := shieldID != nil
	standbyProj := resolveSlotProject(app, standby, target.StandbyProject)

	return &proxy.Site{
		ID:           fmt.Sprintf("slot:%s:%s", app.ID, standby),
		AppID:        app.ID,
		Slot:         standby,
		IsPreview:    true,
		Hosts:        []string{previewDomain},
		TLS:          resolveTLSMode(previewDomain, true, behindShield),
		BehindShield: behindShield,
		TrustedProxy: false,
		Headers:      appSecurityHeaders(),
		Kind:         proxy.SiteKindAppProxy,
		Upstream: &proxy.UpstreamRef{
			Name:          upstreamName(app.ID, svc, port, standby),
			Service:       svc,
			Container:     resolveContainerName(standbyProj, svc, target.StandbyContainers),
			Port:          port,
			Scheme:        "http",
			LongStreams:   true,
			LBTryDuration: "5s",
			FailDuration:  "5s",
		},
	}
}

func buildProxyRouteSite(r db.ProxyRoute) (proxy.Site, bool, error) {
	if !r.Enabled {
		return proxy.Site{}, false, nil
	}
	domain := proxy.CleanQuotedValue(r.Domain)
	if domain == "" {
		return proxy.Site{}, false, nil
	}
	behindShield := r.ShieldNodeID != nil && *r.ShieldNodeID > 0
	site := proxy.Site{
		ID:           fmt.Sprintf("route:%d", r.ID),
		RouteID:      r.ID,
		Hosts:        buildHosts(domain, r.EnableWWW),
		TLS:          resolveTLSMode(domain, r.EnableHTTPS, behindShield),
		BehindShield: behindShield,
		TrustedProxy: r.TrustedProxy,
	}

	target := strings.TrimSpace(r.Target)
	switch strings.ToLower(strings.TrimSpace(r.Kind)) {
	case "proxy":
		site.Kind = proxy.SiteKindExternalProxy
		u, err := url.Parse(target)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return proxy.Site{}, false, fmt.Errorf("proxy route %d (%s): invalid target %q", r.ID, domain, r.Target)
		}
		scheme := strings.ToLower(u.Scheme)
		port := 80
		if scheme == "https" {
			port = 443
		}
		if ps := u.Port(); ps != "" {
			if p, perr := strconv.Atoi(ps); perr == nil && p > 0 {
				port = p
			}
		}
		site.Upstream = &proxy.UpstreamRef{
			Name:        fmt.Sprintf("nd_route_%d", r.ID),
			Port:        port,
			Scheme:      scheme,
			SkipVerify:  r.SkipTLSVerify && scheme == "https",
			External:    target,
			LongStreams: true,
		}
	case "redirect":
		site.Kind = proxy.SiteKindRedirect
		code := r.RedirectCode
		if code != 301 && code != 302 && code != 307 && code != 308 {
			code = 301
		}
		site.Redirect = &proxy.RedirectSpec{
			Code:     code,
			Target:   target,
			KeepPath: r.KeepPath,
		}
	default:
		return proxy.Site{}, false, fmt.Errorf("proxy route %d (%s): unsupported kind %q", r.ID, domain, r.Kind)
	}
	return site, true, nil
}

func buildGateways(in BuildInput, appMap map[string]db.App) []proxy.GatewayPort {
	var out []proxy.GatewayPort
	for _, ap := range in.AllocatedPorts {
		if ap.Direct || ap.Port <= 0 {
			continue
		}
		bindIP := strings.TrimSpace(ap.BindIP)
		if bindIP == "" {
			bindIP = "0.0.0.0"
		}
		proto := strings.ToLower(strings.TrimSpace(ap.Protocol))
		if proto == "" {
			proto = "tcp"
		}

		service := proxy.CleanQuotedValue(ap.Service)
		containerPort := ap.Port
		target := in.AppTargets[ap.AppID]
		for _, hb := range target.GatewayBindings {
			if hb.HostPort == ap.Port && (service == "" || hb.Service == service) && hb.ContainerPort > 0 {
				containerPort = hb.ContainerPort
				if service == "" {
					service = hb.Service
				}
				break
			}
		}

		app, ok := appMap[ap.AppID]
		if !ok {
			app = db.App{ID: ap.AppID, Name: ap.AppID, ActiveSlot: "blue"}
		}
		slot, project := resolveAppProjectAndSlot(app, target.ActiveProject)

		var up *proxy.UpstreamRef
		if ap.AppID != "" {
			svcToken := service
			if svcToken == "" {
				svcToken = "app"
			}
			up = &proxy.UpstreamRef{
				Name:          upstreamName(app.ID, svcToken, containerPort, slot),
				Service:       service,
				Container:     resolveContainerName(project, svcToken, target.ServiceContainers),
				Port:          containerPort,
				Scheme:        "http",
				LongStreams:   true,
				LBTryDuration: "5s",
				FailDuration:  "5s",
			}
		}

		out = append(out, proxy.GatewayPort{
			Port:          ap.Port,
			ContainerPort: containerPort,
			BindIP:        bindIP,
			Protocol:      proto,
			Direct:        false,
			AppID:         ap.AppID,
			Upstream:      up,
		})
	}
	return out
}
