package caddy

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"panel/internal/db"
)

var allowedRedirectCodes = map[int]bool{301: true, 302: true, 307: true, 308: true}

// ValidateRoute checks and cleans a route. Defaults to admin permissions.
func ValidateRoute(r *db.ProxyRoute, isValidHost func(string) bool) error {
	return ValidateRouteForUser(r, isValidHost, true)
}

// ValidateRouteForUser checks and cleans a route with SSRF protection when isAdmin is false.
func ValidateRouteForUser(r *db.ProxyRoute, isValidHost func(string) bool, isAdmin bool) error {
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
			return fmt.Errorf("the target must look like http://10.0.0.5:8080 or https://backend.example.com")
		}
		if u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
			return fmt.Errorf("the target is only an address and port, without a path")
		}
		if strings.EqualFold(u.Hostname(), r.Domain) {
			return fmt.Errorf("the target points back at the same domain")
		}

		// Non-admin SSRF Protection
		if !isAdmin {
			if err := checkTargetSSRF(u.Hostname()); err != nil {
				return err
			}
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

func isPrivateOrSpecialIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
		return true
	}
	// Check IPv4 link-local (169.254.0.0/16 - cloud metadata AWS/GCP/Azure)
	if ip4 := ip.To4(); ip4 != nil {
		if ip4[0] == 169 && ip4[1] == 254 {
			return true
		}
		// 127.0.0.0/8
		if ip4[0] == 127 {
			return true
		}
		// 0.0.0.0/8
		if ip4[0] == 0 {
			return true
		}
		// 100.64.0.0/10 (Shared / Carrier-grade NAT)
		if ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127 {
			return true
		}
	} else {
		// IPv6 unique local (fc00::/7) or link local (fe80::/10)
		if len(ip) == net.IPv6len && (ip[0]&0xfe == 0xfc || (ip[0] == 0xfe && ip[1]&0xc0 == 0x80)) {
			return true
		}
	}
	return false
}

func checkTargetSSRF(host string) error {
	host = strings.TrimSpace(strings.ToLower(host))
	if host == "" {
		return fmt.Errorf("target host cannot be empty")
	}

	// Loopback and internal names
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || host == "host.docker.internal" {
		return fmt.Errorf("security: target cannot be localhost or an internal host")
	}

	// Internal container/service names without dots (e.g. caddy, nextdeploy, panel, db, redis)
	if !strings.Contains(host, ".") && !strings.Contains(host, ":") {
		return fmt.Errorf("security: target must be a public domain or public IP, not an internal service (%s)", host)
	}

	// Literal IP address check
	if ip := net.ParseIP(host); ip != nil {
		if isPrivateOrSpecialIP(ip) {
			return fmt.Errorf("security: target points to a private, loopback, or cloud metadata address (%s)", ip.String())
		}
		return nil
	}

	// Domain name DNS resolution check (prevents DNS rebinding to internal addresses)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
	if err != nil {
		return fmt.Errorf("target domain %s could not be resolved (%w)", host, err)
	}
	for _, ip := range ips {
		if isPrivateOrSpecialIP(ip) {
			return fmt.Errorf("security: target domain %s resolves to internal or private IP (%s)", host, ip.String())
		}
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
