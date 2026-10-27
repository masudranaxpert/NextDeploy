package caddy

import (
	"fmt"
	"net/url"
	"strings"

	"panel/internal/db"
)

var allowedRedirectCodes = map[int]bool{301: true, 302: true, 307: true, 308: true}

// ValidateRoute checks and cleans a route. isValidHost is injected so this package does not depend on the PHP code.
func ValidateRoute(r *db.ProxyRoute, isValidHost func(string) bool) error {
	r.Domain = strings.ToLower(strings.TrimSpace(r.Domain))
	r.Target = strings.TrimSpace(r.Target)
	r.Note = strings.TrimSpace(r.Note)
	if len(r.Note) > 200 {
		return fmt.Errorf("the note is too long")
	}
	if !isValidHost(r.Domain) {
		return fmt.Errorf("enter a domain such as app.example.com")
	}
	switch r.Kind {
	case "proxy":
		u, err := url.Parse(r.Target)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("the target must look like http://10.0.0.5:8080 or https://backend.internal")
		}
		if u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
			return fmt.Errorf("the target is only an address and port, without a path")
		}
		if strings.EqualFold(u.Hostname(), r.Domain) {
			return fmt.Errorf("the target points back at the same domain")
		}
		r.Target = u.Scheme + "://" + u.Host
		r.RedirectCode, r.KeepPath = 0, false
		if u.Scheme == "http" {
			r.SkipTLSVerify = false
		}
	case "redirect":
		u, err := url.Parse(r.Target)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
			return fmt.Errorf("the redirect target must be a full address such as https://example.com")
		}
		if strings.EqualFold(u.Hostname(), r.Domain) && !r.KeepPath {
			return fmt.Errorf("the redirect points back at the same domain")
		}
		if !allowedRedirectCodes[r.RedirectCode] {
			r.RedirectCode = 301
		}
		r.Target = strings.TrimRight(u.String(), "/")
		r.SkipTLSVerify = false
	default:
		return fmt.Errorf("choose reverse proxy or redirect")
	}
	return nil
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
