package proxy

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"panel/internal/db"
)

const maxSecurityEntries = 500

var allowedRedirectCodes = map[int]bool{301: true, 302: true, 307: true, 308: true}

// CleanQuotedValue strips matching surrounding single, double, or backtick quotes
// and trims surrounding whitespace.
func CleanQuotedValue(v string) string {
	v = strings.TrimSpace(v)
	for len(v) >= 2 {
		first := v[0]
		last := v[len(v)-1]
		if (first == '"' && last == '"') || (first == '\'' && last == '\'') || (first == '`' && last == '`') {
			if unquoted, err := strconv.Unquote(v); err == nil {
				v = strings.TrimSpace(unquoted)
				continue
			}
			v = strings.TrimSpace(v[1 : len(v)-1])
			continue
		}
		break
	}
	return v
}

// ValidSharedTarget reports whether target is a safe absolute Unix container mount
// path without parent-directory traversal segments.
func ValidSharedTarget(target string) bool {
	t := strings.TrimSpace(target)
	if t == "" || t[0] != '/' {
		return false
	}
	for _, seg := range strings.Split(t, "/") {
		if seg == ".." {
			return false
		}
	}
	return true
}

// HasUnsafeLabelChars reports whether s contains whitespace, directive delimiters,
// quotes, or ASCII control characters unsafe inside proxy configuration directives.
func HasUnsafeLabelChars(s string) bool {
	if strings.ContainsAny(s, "\r\n\t\"'`{};# ") {
		return true
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

// NormalizeIPList trims, validates (IPv4/IPv6 address or CIDR), and deduplicates
// an IP/CIDR entry list up to the per-site security limit.
func NormalizeIPList(in []string) ([]string, error) {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, raw := range in {
		v := strings.TrimSpace(raw)
		if v == "" {
			continue
		}
		var canon string
		if strings.Contains(v, "/") {
			p, err := netip.ParsePrefix(v)
			if err != nil {
				return nil, fmt.Errorf("%q is not a valid IP range", v)
			}
			canon = p.Masked().String()
		} else {
			a, err := netip.ParseAddr(v)
			if err != nil {
				return nil, fmt.Errorf("%q is not a valid IP address", v)
			}
			canon = a.Unmap().String()
		}
		if !seen[canon] {
			seen[canon] = true
			out = append(out, canon)
		}
	}
	if len(out) > maxSecurityEntries {
		return nil, fmt.Errorf("at most %d entries", maxSecurityEntries)
	}
	return out, nil
}

// ValidateRoute checks and normalizes a ProxyRoute using admin permissions (skipping SSRF checks).
func ValidateRoute(r *db.ProxyRoute, isValidHost func(string) bool) error {
	return ValidateRouteForUser(r, isValidHost, true)
}

// ValidateRouteForUser checks and normalizes a ProxyRoute, enforcing SSRF protection
// on proxy targets when isAdmin is false.
func ValidateRouteForUser(r *db.ProxyRoute, isValidHost func(string) bool, isAdmin bool) error {
	if r == nil {
		return fmt.Errorf("route is required")
	}
	r.Domain = strings.ToLower(strings.TrimSpace(r.Domain))
	r.Target = strings.TrimSpace(r.Target)
	r.Note = strings.TrimSpace(r.Note)
	if len(r.Note) > 200 {
		return fmt.Errorf("the note is too long")
	}
	if isValidHost == nil || !isValidHost(r.Domain) {
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
	if ip4 := ip.To4(); ip4 != nil {
		if ip4[0] == 169 && ip4[1] == 254 {
			return true
		}
		if ip4[0] == 127 || ip4[0] == 0 {
			return true
		}
		if ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127 {
			return true
		}
	} else {
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
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || host == "host.docker.internal" {
		return fmt.Errorf("security: target cannot be localhost or an internal host")
	}
	if !strings.Contains(host, ".") && !strings.Contains(host, ":") {
		return fmt.Errorf("security: target must be a public domain or public IP, not an internal service (%s)", host)
	}
	if ip := net.ParseIP(host); ip != nil {
		if isPrivateOrSpecialIP(ip) {
			return fmt.Errorf("security: target points to a private, loopback, or cloud metadata address (%s)", ip.String())
		}
		return nil
	}
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
