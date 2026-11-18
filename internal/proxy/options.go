package proxy

import (
	"fmt"
	"net/netip"
	"regexp"
	"strings"
	"time"
)

// GlobalOptions holds server-wide reverse-proxy timeout, header, and real-IP settings.
// An empty string indicates the active engine's default behavior.
type GlobalOptions struct {
	Profile          string   `json:"profile"`                     // default | balanced | high | strict | custom
	ReadHeader       string   `json:"read_header"`                 // max time allowed to read request headers
	ReadBodyIdle     string   `json:"read_body_idle"`              // max pause while a request body is uploaded
	Idle             string   `json:"idle"`                        // keep-alive wait for the next request
	GracePeriod      string   `json:"grace_period"`                // max drain time during graceful reload
	MaxHeaderSize    string   `json:"max_header_size"`             // max request header block size (e.g. 16KB)
	RealIP           string   `json:"real_ip"`                     // none | cloudflare | custom
	TrustedProxies   []string `json:"trusted_proxies"`             // trusted proxy IPs/CIDRs when real_ip is custom
	TrustedStrict    bool     `json:"trusted_strict"`              // parse forwarded headers strictly right-to-left
	ShieldProxies    []string `json:"shield_proxies,omitempty"`    // auto-managed active Edge Shield node IPs
	DirectCloudflare bool     `json:"direct_cloudflare,omitempty"` // true when any direct site uses trusted_proxy
}

// CloudflareRanges lists Cloudflare's published edge proxy IPv4 and IPv6 CIDR ranges.
var CloudflareRanges = []string{
	"173.245.48.0/20", "103.21.244.0/22", "103.22.200.0/22", "103.31.4.0/22", "141.101.64.0/18",
	"108.162.192.0/18", "190.93.240.0/20", "188.114.96.0/20", "197.234.240.0/22", "198.41.128.0/17",
	"162.158.0.0/15", "104.16.0.0/13", "104.24.0.0/14", "172.64.0.0/13", "131.0.72.0/22",
	"2400:cb00::/32", "2606:4700::/32", "2803:f800::/32", "2405:b500::/32", "2405:8100::/32",
	"2a06:98c0::/29", "2c0f:f248::/32",
}

// OptionProfiles defines built-in starting presets for GlobalOptions.
var OptionProfiles = map[string]GlobalOptions{
	"default":  {Profile: "default", RealIP: "none"},
	"balanced": {Profile: "balanced", ReadHeader: "15s", Idle: "2m", GracePeriod: "30s", RealIP: "none"},
	"high":     {Profile: "high", ReadHeader: "10s", Idle: "5m", GracePeriod: "30s", RealIP: "none"},
	"strict":   {Profile: "strict", ReadHeader: "5s", ReadBodyIdle: "30s", Idle: "1m", GracePeriod: "15s", MaxHeaderSize: "8KB", RealIP: "none"},
}

var sizeRe = regexp.MustCompile(`^[1-9][0-9]{0,5}(KB|KiB|MB|MiB)$`)

func checkDuration(name, v string, min, max time.Duration) error {
	if v == "" {
		return nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return fmt.Errorf("%s: use a duration such as 15s or 2m", name)
	}
	if d < min || d > max {
		return fmt.Errorf("%s must be between %s and %s", name, min, max)
	}
	return nil
}

// Normalize validates and canonicalizes GlobalOptions fields, rejecting unsafe
// values, invalid durations, malformed sizes, or wildcard 0.0.0.0/0 trust ranges.
func (o GlobalOptions) Normalize() (GlobalOptions, error) {
	o.Profile = strings.ToLower(strings.TrimSpace(o.Profile))
	if o.Profile == "" {
		o.Profile = "custom"
	}
	for _, f := range []struct {
		name string
		v    *string
		min  time.Duration
		max  time.Duration
	}{
		{"Header timeout", &o.ReadHeader, time.Second, 5 * time.Minute},
		{"Upload idle timeout", &o.ReadBodyIdle, time.Second, 30 * time.Minute},
		{"Keep-alive timeout", &o.Idle, time.Second, time.Hour},
		{"Reload grace period", &o.GracePeriod, time.Second, 10 * time.Minute},
	} {
		*f.v = strings.ToLower(strings.TrimSpace(*f.v))
		if err := checkDuration(f.name, *f.v, f.min, f.max); err != nil {
			return o, err
		}
	}
	o.MaxHeaderSize = strings.TrimSpace(o.MaxHeaderSize)
	if o.MaxHeaderSize != "" && !sizeRe.MatchString(o.MaxHeaderSize) {
		return o, fmt.Errorf("Header size limit: use a size such as 16KB")
	}
	switch o.RealIP {
	case "", "none":
		o.RealIP = "none"
		o.TrustedProxies, o.TrustedStrict = nil, false
	case "cloudflare":
		o.TrustedProxies = nil
	case "custom":
		list, err := NormalizeIPList(o.TrustedProxies)
		if err != nil {
			return o, fmt.Errorf("Trusted proxies: %w", err)
		}
		if len(list) == 0 {
			return o, fmt.Errorf("Trusted proxies: add at least one address or range")
		}
		for _, r := range list {
			if p, err := netip.ParsePrefix(r); err == nil && p.Bits() == 0 {
				return o, fmt.Errorf("Trusted proxies: %q would trust the whole internet", r)
			}
		}
		o.TrustedProxies = list
	default:
		return o, fmt.Errorf("Real visitor IP: unknown mode")
	}
	if len(o.ShieldProxies) > 0 {
		sp, err := NormalizeIPList(o.ShieldProxies)
		if err != nil {
			return o, fmt.Errorf("Shield proxies: %w", err)
		}
		if len(sp) == 0 {
			o.ShieldProxies = nil
		} else {
			o.ShieldProxies = sp
		}
	} else {
		o.ShieldProxies = nil
	}
	return o, nil
}
