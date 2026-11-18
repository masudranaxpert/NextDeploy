package state

import (
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"

	"panel/internal/db"
	"panel/internal/proxy"
)

func buildAppSites(in BuildInput, phpAppIDs map[string]bool, appMap map[string]db.App, shieldMap map[int64]db.ShieldNode) []proxy.Site {
	var sites []proxy.Site

	for _, d := range in.AppDomains {
		if phpAppIDs[d.AppID] {
			continue
		}
		service := proxy.CleanQuotedValue(d.Service)
		domain := proxy.CleanQuotedValue(d.Domain)
		if domain == "" || service == "" {
			continue
		}
		// Exclude dummy AppDomain placeholders created for PHP/static sites.
		if strings.EqualFold(service, "php") && d.Port == 80 {
			continue
		}

		app, ok := appMap[d.AppID]
		if !ok {
			app = db.App{ID: d.AppID, Name: d.AppID, ActiveSlot: "blue"}
		}
		target := in.AppTargets[d.AppID]
		slot, project := resolveAppProjectAndSlot(app, target.ActiveProject)

		port := d.Port
		if port <= 0 {
			port = 80
		}
		behindShield := d.ShieldNodeID != nil && *d.ShieldNodeID > 0
		up := &proxy.UpstreamRef{
			Name:        upstreamName(app.ID, service, port, slot),
			Service:     service,
			Container:   resolveContainerName(project, service, target.ServiceContainers),
			Port:        port,
			Scheme:      "http",
			LongStreams: true,
		}
		if slot != "" {
			up.LBTryDuration = "5s"
			up.FailDuration = "5s"
		}

		paths := buildDomainPathRoutes(d, target, app.ID, slot, project, up)

		sites = append(sites, proxy.Site{
			ID:           fmt.Sprintf("app:%s:dom:%d", app.ID, d.ID),
			AppID:        app.ID,
			DomainID:     d.ID,
			Slot:         slot,
			Hosts:        buildHosts(domain, d.EnableWWW),
			TLS:          resolveTLSMode(domain, d.EnableHTTPS, behindShield),
			BehindShield: behindShield,
			TrustedProxy: d.TrustedProxy,
			Headers:      appSecurityHeaders(),
			Kind:         proxy.SiteKindAppProxy,
			Paths:        paths,
			Upstream:     up,
		})
	}

	for _, app := range in.Apps {
		if phpAppIDs[app.ID] {
			continue
		}
		target := in.AppTargets[app.ID]
		if pv := buildPreviewSite(in, app, target, shieldMap); pv != nil {
			sites = append(sites, *pv)
		}
	}

	return sites
}

func buildDomainPathRoutes(d db.AppDomain, target AppTarget, appID, slot, project string, defaultUp *proxy.UpstreamRef) []proxy.PathRoute {
	var routes []proxy.PathRoute

	addAppDomainRoute := func(r db.AppDomainRoute) {
		prefix := strings.TrimSpace(r.Path)
		if prefix == "" {
			return
		}
		prio := r.Priority
		if prio <= 0 {
			prio = 1
		}
		if r.EffectiveDirect() {
			root := resolveStaticRoot(r.Root, target.WorkspaceDir)
			if root == "" {
				return
			}
			routes = append(routes, proxy.PathRoute{
				Prefix:      prefix,
				StripPrefix: true,
				Priority:    prio,
				Handler: proxy.Handler{
					Kind: proxy.HandlerStaticDir,
					Static: &proxy.StaticDirSpec{
						Root: root,
						Hide: defaultHidePatterns(),
					},
				},
			})
			return
		}
		upCopy := *defaultUp
		routes = append(routes, proxy.PathRoute{
			Prefix:      prefix,
			StripPrefix: false,
			Priority:    prio,
			Handler: proxy.Handler{
				Kind:     proxy.HandlerProxy,
				Upstream: &upCopy,
			},
		})
	}

	for _, r := range d.EffectiveRouteRules() {
		addAppDomainRoute(r)
	}
	if raw := strings.TrimSpace(d.RouteRulesJSON); raw != "" && raw != "[]" {
		var extra []db.AppDomainRoute
		if json.Unmarshal([]byte(raw), &extra) == nil {
			for _, r := range extra {
				addAppDomainRoute(r)
			}
		}
	}

	appendCustom := func(list []proxy.PathRoute) {
		for _, pr := range list {
			routes = append(routes, normalizeCustomPathRoute(pr, target, appID, slot, project, defaultUp))
		}
	}
	appendCustom(target.PathRoutes)
	if target.DomainPathRoutes != nil {
		appendCustom(target.DomainPathRoutes[d.ID])
	}

	if len(routes) == 0 {
		return nil
	}
	sortPathRoutes(routes)
	return routes
}

func resolveStaticRoot(raw, workspaceDir string) string {
	root := strings.TrimSpace(raw)
	if root == "" {
		return ""
	}
	ws := strings.TrimRight(strings.TrimSpace(workspaceDir), "/")
	if ws != "" && !strings.HasPrefix(root, "/") {
		cleaned := strings.TrimPrefix(path.Clean("/"+strings.ReplaceAll(root, "\\", "/")), "/")
		if cleaned == "" || cleaned == "." {
			return ws
		}
		return ws + "/" + cleaned
	}
	return root
}

func normalizeCustomPathRoute(pr proxy.PathRoute, target AppTarget, appID, slot, project string, defaultUp *proxy.UpstreamRef) proxy.PathRoute {
	if pr.Priority <= 0 {
		pr.Priority = 1
	}
	pr.Prefix = strings.TrimSpace(pr.Prefix)
	if pr.Handler.Static != nil {
		st := *pr.Handler.Static
		st.Root = resolveStaticRoot(st.Root, target.WorkspaceDir)
		if len(st.Hide) == 0 {
			st.Hide = defaultHidePatterns()
		} else {
			st.Hide = append([]string(nil), st.Hide...)
		}
		if len(st.Precompressed) > 0 {
			st.Precompressed = append([]string(nil), st.Precompressed...)
		}
		pr.Handler.Static = &st
	}
	if pr.Handler.Kind == proxy.HandlerProxy {
		var up proxy.UpstreamRef
		if pr.Handler.Upstream != nil {
			up = *pr.Handler.Upstream
		} else if defaultUp != nil {
			up = *defaultUp
		}
		if up.Service == "" && defaultUp != nil {
			up.Service = defaultUp.Service
		}
		if up.Port <= 0 && defaultUp != nil {
			up.Port = defaultUp.Port
		}
		if up.Scheme == "" {
			up.Scheme = "http"
		}
		if up.Container == "" {
			up.Container = resolveContainerName(project, up.Service, target.ServiceContainers)
		}
		if up.Name == "" {
			up.Name = upstreamName(appID, up.Service, up.Port, slot)
		}
		if slot != "" && up.LBTryDuration == "" {
			up.LBTryDuration = "5s"
		}
		if slot != "" && up.FailDuration == "" {
			up.FailDuration = "5s"
		}
		if len(up.RequestHeaders) > 0 {
			up.RequestHeaders = append([]proxy.HeaderOp(nil), up.RequestHeaders...)
		}
		up.LongStreams = true
		pr.Handler.Upstream = &up
	}
	return pr
}

func sortPathRoutes(routes []proxy.PathRoute) {
	sort.SliceStable(routes, func(i, j int) bool {
		a, b := routes[i], routes[j]
		if a.Priority != b.Priority {
			return a.Priority < b.Priority
		}
		if len(a.Prefix) != len(b.Prefix) {
			return len(a.Prefix) > len(b.Prefix)
		}
		if a.Prefix != b.Prefix {
			return a.Prefix < b.Prefix
		}
		return a.Handler.Kind < b.Handler.Kind
	})
}
