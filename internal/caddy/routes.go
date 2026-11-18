package caddy

import (
	"fmt"
	"strings"

	"panel/internal/db"
	"panel/internal/proxy"
)

// ValidateRoute checks and cleans a route. Defaults to admin permissions.
func ValidateRoute(r *db.ProxyRoute, isValidHost func(string) bool) error {
	return proxy.ValidateRoute(r, isValidHost)
}

// ValidateRouteForUser checks and cleans a route with SSRF protection when isAdmin is false.
func ValidateRouteForUser(r *db.ProxyRoute, isValidHost func(string) bool, isAdmin bool) error {
	return proxy.ValidateRouteForUser(r, isValidHost, isAdmin)
}

// RouteLabels builds the caddy-docker-proxy labels for one route.
func RouteLabels(r db.ProxyRoute) map[string]string {
	labels := map[string]string{}
	var sites []string
	if r.EnableHTTPS || r.ShieldNodeID != nil {
		sites = append(sites, r.Domain)
		if r.EnableWWW {
			sites = append(sites, "www."+r.Domain)
		}
	} else {
		sites = append(sites, "http://"+r.Domain)
		if r.EnableWWW {
			sites = append(sites, "http://www."+r.Domain)
		}
	}
	labels["caddy"] = strings.Join(sites, ", ")
	if (r.EnableHTTPS || r.ShieldNodeID != nil) && (r.ShieldNodeID != nil || ShouldUseInternalTLS(r.Domain)) {
		labels["caddy.tls"] = "internal"
	}
	switch r.Kind {
	case "proxy":
		labels["caddy.reverse_proxy"] = r.Target
		labels["caddy.reverse_proxy.stream_close_delay"] = "10m"
		if !r.TrustedProxy && r.ShieldNodeID == nil {
			applyUntrustedDirectProxyHeaders(labels, "caddy.reverse_proxy")
		}
		if r.SkipTLSVerify && strings.HasPrefix(r.Target, "https://") {
			labels["caddy.reverse_proxy.transport"] = "http"
			labels["caddy.reverse_proxy.transport.tls_insecure_skip_verify"] = `{{""}}`
		}
	case "redirect":
		to := r.Target
		if r.KeepPath {
			to += "{uri}"
		}
		labels["caddy.redir"] = fmt.Sprintf("%s %d", to, r.RedirectCode)
	}
	return labels
}
