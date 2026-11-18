package caddy

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	icaddy "panel/internal/caddy"
	"panel/internal/db"
	"panel/internal/proxy"
)

func anyCustomAppPaths(sites []proxy.Site) bool {
	for _, s := range sites {
		if needsCustomAppPaths(s) {
			return true
		}
	}
	return false
}

func needsCustomAppPaths(s proxy.Site) bool {
	if len(s.Paths) > 2 {
		return true
	}
	for _, pr := range s.Paths {
		if pr.Handler.Kind != proxy.HandlerStaticDir || pr.Handler.Static == nil || !pr.StripPrefix {
			return true
		}
		if len(pr.Handler.Static.Precompressed) > 0 {
			return true
		}
		trimmed := trimPathMatcherWildcard(pr.Prefix)
		if _, ok := db.NormalizeCaddyPathMatcherFromURLPrefix(trimmed); !ok {
			return true
		}
		if len(pr.Handler.Static.Hide) > 0 && strings.Join(pr.Handler.Static.Hide, " ") != defaultStaticHide {
			return true
		}
	}
	return false
}

func trimPathMatcherWildcard(prefix string) string {
	s := strings.TrimSpace(prefix)
	s = strings.TrimSuffix(s, "/*")
	s = strings.TrimSuffix(s, "*")
	return strings.TrimRight(s, "/")
}

func applySimpleStaticPaths(d *db.AppDomain, paths []proxy.PathRoute) {
	if len(paths) == 0 {
		return
	}
	ordered := sortedPathRoutes(paths)
	for i, pr := range ordered {
		if pr.Handler.Static == nil {
			continue
		}
		prefix := trimPathMatcherWildcard(pr.Prefix)
		root := strings.TrimSpace(pr.Handler.Static.Root)
		if i == 0 {
			d.ServeStatic = true
			d.StaticURLPrefix = prefix
			d.StaticPath = root
		} else if i == 1 {
			d.ServeMedia = true
			d.MediaURLPrefix = prefix
			d.MediaPath = root
		}
	}
}

func sortedPathRoutes(paths []proxy.PathRoute) []proxy.PathRoute {
	out := append([]proxy.PathRoute(nil), paths...)
	for i := range out {
		if out[i].Priority <= 0 {
			out[i].Priority = 1
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Priority != b.Priority {
			return a.Priority < b.Priority
		}
		if len(a.Prefix) != len(b.Prefix) {
			return len(a.Prefix) > len(b.Prefix)
		}
		if a.Prefix != b.Prefix {
			return a.Prefix < b.Prefix
		}
		if a.Handler.Kind != b.Handler.Kind {
			return a.Handler.Kind < b.Handler.Kind
		}
		return pathRouteRoot(a) < pathRouteRoot(b)
	})
	return out
}

func pathRouteRoot(pr proxy.PathRoute) string {
	if pr.Handler.Static != nil {
		return strings.TrimSpace(pr.Handler.Static.Root)
	}
	return ""
}

func renderCustomAppSite(s proxy.Site, up proxy.UpstreamRef, isSlot bool) map[string]string {
	noPaths := s
	noPaths.Paths = nil
	domains, metas := sitesToAppDomains([]proxy.Site{noPaths}, up)
	if len(domains) == 0 {
		return map[string]string{}
	}
	var labels map[string]string
	if isSlot {
		labels = icaddy.GenerateSlotLabels(domains, nil, nil, false)
	} else {
		labels = icaddy.GenerateLabels(domains[0])
	}
	for k := range labels {
		if k == "caddy.reverse_proxy" || strings.HasPrefix(k, "caddy.reverse_proxy.") {
			delete(labels, k)
		}
	}

	port := resolveUpstreamPort(s, up)
	if port <= 0 {
		port = 80
	}
	untrusted := !s.TrustedProxy && !s.BehindShield

	order := 1
	for _, pr := range sortedPathRoutes(s.Paths) {
		p := strings.TrimSpace(pr.Prefix)
		if p == "" {
			continue
		}
		if pr.Handler.Kind == proxy.HandlerStaticDir && pr.Handler.Static != nil {
			if emitStaticPathRoute(labels, order, p, pr) {
				order++
			}
			continue
		}
		if pr.Handler.Kind == proxy.HandlerProxy {
			emitProxyPathRoute(labels, order, p, pr, port, untrusted, isSlot)
			order++
		}
	}

	rpBase := "caddy.reverse_proxy"
	if order > 1 {
		rpBase = fmt.Sprintf("caddy.%d_reverse_proxy", order)
	}
	labels[rpBase] = "{{upstreams " + strconv.Itoa(port) + "}}"
	labels[rpBase+".stream_close_delay"] = "10m"
	if isSlot {
		tryDur := resolveLBTryDuration(s, up)
		if tryDur == "" {
			tryDur = "5s"
		}
		failDur := resolveFailDuration(s, up)
		if failDur == "" {
			failDur = "5s"
		}
		labels[rpBase+".lb_try_duration"] = tryDur
		labels[rpBase+".fail_duration"] = failDur
	}
	if untrusted {
		applyUntrustedHeaders(labels, rpBase)
	}

	applySingleSitePostOverrides(labels, "caddy", s, up, metas[0].extraHosts, isSlot)
	return labels
}

func emitStaticPathRoute(labels map[string]string, order int, prefix string, pr proxy.PathRoute) bool {
	root := strings.TrimSpace(pr.Handler.Static.Root)
	if root == "" {
		return false
	}
	directive := "handle"
	if pr.StripPrefix {
		directive = "handle_path"
	}
	base := fmt.Sprintf("caddy.%d_%s", order, directive)
	labels[base] = prefix
	labels[base+".0_root"] = "* " + root
	if len(pr.Handler.Static.Precompressed) > 0 {
		labels[base+".1_file_server.precompressed"] = strings.Join(pr.Handler.Static.Precompressed, " ")
	}
	labels[base+".1_file_server"] = `{{""}}`
	hide := defaultStaticHide
	if len(pr.Handler.Static.Hide) > 0 {
		hide = strings.Join(pr.Handler.Static.Hide, " ")
	}
	labels[base+".1_file_server.hide"] = hide
	return true
}

func emitProxyPathRoute(labels map[string]string, order int, prefix string, pr proxy.PathRoute, fallbackPort int, untrusted, isSlot bool) {
	u := pr.Handler.Upstream
	target, isHTTPS, skipVerify := pathProxyTarget(u, fallbackPort)

	var rpBase string
	if pr.StripPrefix {
		hpBase := fmt.Sprintf("caddy.%d_handle_path", order)
		labels[hpBase] = prefix
		rpBase = hpBase + ".reverse_proxy"
		labels[rpBase] = target
	} else {
		rpBase = fmt.Sprintf("caddy.%d_reverse_proxy", order)
		labels[rpBase] = prefix + " " + target
	}

	labels[rpBase+".stream_close_delay"] = "10m"
	if tryDur := pathProxyLBTryDuration(u, isSlot); tryDur != "" {
		labels[rpBase+".lb_try_duration"] = tryDur
	}
	if u != nil && u.FailDuration != "" {
		labels[rpBase+".fail_duration"] = u.FailDuration
	}
	if isHTTPS {
		if u != nil && u.External != "" {
			if skipVerify {
				applyProxySkipVerifyOnly(labels, rpBase)
			}
		} else {
			applyProxyTLSTransport(labels, rpBase, skipVerify)
		}
	}
	if untrusted {
		applyUntrustedHeaders(labels, rpBase)
	} else if u != nil && len(u.RequestHeaders) > 0 {
		applyCustomRequestHeaders(labels, rpBase, u.RequestHeaders)
	}
}

func pathProxyTarget(u *proxy.UpstreamRef, fallbackPort int) (target string, isHTTPS, skipVerify bool) {
	if u != nil && strings.TrimSpace(u.External) != "" {
		ext := strings.TrimSpace(u.External)
		return ext, strings.HasPrefix(strings.ToLower(ext), "https://"), u.SkipVerify
	}
	routePort := fallbackPort
	if u != nil && u.Port > 0 {
		routePort = u.Port
	}
	if routePort <= 0 {
		routePort = 80
	}
	target = "{{upstreams " + strconv.Itoa(routePort) + "}}"
	if u != nil {
		isHTTPS = strings.EqualFold(u.Scheme, "https")
		skipVerify = u.SkipVerify
	}
	return target, isHTTPS, skipVerify
}

func pathProxyLBTryDuration(u *proxy.UpstreamRef, isSlot bool) string {
	if u != nil && u.LBTryDuration != "" {
		return u.LBTryDuration
	}
	if isSlot {
		return "30s"
	}
	return ""
}

func remapAndMerge(dst, src map[string]string, fromPrefix, toPrefix string) {
	for k, v := range src {
		if k == fromPrefix {
			dst[toPrefix] = v
		} else if strings.HasPrefix(k, fromPrefix+".") {
			dst[toPrefix+strings.TrimPrefix(k, fromPrefix)] = v
		} else {
			dst[k] = v
		}
	}
}

func extractPrefixLabels(src map[string]string, prefix string) map[string]string {
	out := map[string]string{}
	for k, v := range src {
		if k == prefix || strings.HasPrefix(k, prefix+".") {
			out[k] = v
		}
	}
	return out
}
