package caddy

import (
	"fmt"
	"net/netip"
	"regexp"
	"strings"
	"time"
)

// GlobalOptions are the server-wide Caddy settings the admin can change. An empty value means
// "Caddy's own default". They are written as labels on the Caddy container itself.
type GlobalOptions struct {
	Profile          string   `json:"profile"`                     // default | balanced | high | strict | custom
	ReadHeader       string   `json:"read_header"`                 // time allowed to read request headers
	ReadBodyIdle     string   `json:"read_body_idle"`              // max pause while a request body is uploaded
	Idle             string   `json:"idle"`                        // keep-alive wait for the next request
	GracePeriod      string   `json:"grace_period"`                // how long a reload waits for open connections
	MaxHeaderSize    string   `json:"max_header_size"`             // e.g. 16KB; bigger headers get 431
	RealIP           string   `json:"real_ip"`                     // none | cloudflare | custom
	TrustedProxies   []string `json:"trusted_proxies"`             // used when real_ip is custom
	TrustedStrict    bool     `json:"trusted_strict"`              // read forwarded headers right to left
	ShieldProxies    []string `json:"shield_proxies,omitempty"`    // auto-managed active Shield node IPs
	DirectCloudflare bool     `json:"direct_cloudflare,omitempty"` // auto-managed when any direct domain/route has trusted_proxy=true
}

// CloudflareRanges are Cloudflare's published proxy ranges (https://www.cloudflare.com/ips/).
// Cloudflare changes them rarely; use "custom" to keep your own list current.
var CloudflareRanges = []string{
	"173.245.48.0/20", "103.21.244.0/22", "103.22.200.0/22", "103.31.4.0/22", "141.101.64.0/18",
	"108.162.192.0/18", "190.93.240.0/20", "188.114.96.0/20", "197.234.240.0/22", "198.41.128.0/17",
	"162.158.0.0/15", "104.16.0.0/13", "104.24.0.0/14", "172.64.0.0/13", "131.0.72.0/22",
	"2400:cb00::/32", "2606:4700::/32", "2803:f800::/32", "2405:b500::/32", "2405:8100::/32",
	"2a06:98c0::/29", "2c0f:f248::/32",
}

// OptionProfiles are starting points; picking one fills the fields, which stay editable.
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

// Normalize validates the options and fills in the derived fields. It never trusts the input:
// every value ends up inside a label that becomes part of a generated Caddyfile.
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

// Labels returns the label set for the Caddy container (keys always start with "caddy.servers"
// or are "caddy.grace_period", so stale ones can be removed by prefix).
func (o GlobalOptions) Labels() map[string]string {
	out := map[string]string{}
	set := func(k, v string) {
		if v != "" {
			out[k] = v
		}
	}
	set("caddy.servers.timeouts.read_header", o.ReadHeader)
	set("caddy.servers.timeouts.read_body_idle", o.ReadBodyIdle)
	set("caddy.servers.timeouts.idle", o.Idle)
	set("caddy.servers.max_header_size", o.MaxHeaderSize)
	set("caddy.grace_period", o.GracePeriod)

	cfActive := o.RealIP == "cloudflare" || o.DirectCloudflare
	seen := make(map[string]bool)
	var ranges []string
	appendRanges := func(list []string) {
		for _, raw := range list {
			ip := strings.TrimSpace(raw)
			if ip == "" || seen[ip] {
				continue
			}
			seen[ip] = true
			ranges = append(ranges, ip)
		}
	}
	if cfActive {
		appendRanges(CloudflareRanges)
	}
	if o.RealIP == "custom" {
		appendRanges(o.TrustedProxies)
	}
	appendRanges(o.ShieldProxies)

	if len(ranges) > 0 {
		out["caddy.servers.trusted_proxies"] = "static " + strings.Join(ranges, " ")
		if cfActive || o.TrustedStrict {
			out["caddy.servers.trusted_proxies_strict"] = `{{""}}`
			out["caddy.servers.client_ip_headers"] = "CF-Connecting-IP X-Forwarded-For"
		}
	}
	return out
}

// applyUntrustedDirectProxyHeaders enforces TrustedProxy == false on direct Origin
// reverse_proxy blocks by stripping CF-Connecting-IP and forwarding the direct socket IP.
func applyUntrustedDirectProxyHeaders(labels map[string]string, rpBase string) {
	labels[rpBase+".header_up_0"] = "X-Real-IP {remote_host}"
	labels[rpBase+".header_up_1"] = "X-Forwarded-For {remote_host}"
	labels[rpBase+".header_up_2"] = "-CF-Connecting-IP"
}

// IsGlobalOptionLabel reports whether a label key is managed by GlobalOptions.
func IsGlobalOptionLabel(key string) bool {
	return key == "caddy.grace_period" || key == "caddy.servers" || strings.HasPrefix(key, "caddy.servers.")
}
