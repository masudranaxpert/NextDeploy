package caddy

import (
	"strings"

	"panel/internal/proxy"
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
var CloudflareRanges = proxy.CloudflareRanges

// OptionProfiles are starting points; picking one fills the fields, which stay editable.
var OptionProfiles = map[string]GlobalOptions{
	"default":  {Profile: "default", RealIP: "none"},
	"balanced": {Profile: "balanced", ReadHeader: "15s", Idle: "2m", GracePeriod: "30s", RealIP: "none"},
	"high":     {Profile: "high", ReadHeader: "10s", Idle: "5m", GracePeriod: "30s", RealIP: "none"},
	"strict":   {Profile: "strict", ReadHeader: "5s", ReadBodyIdle: "30s", Idle: "1m", GracePeriod: "15s", MaxHeaderSize: "8KB", RealIP: "none"},
}

// Normalize validates the options and fills in the derived fields. It never trusts the input:
// every value ends up inside a label that becomes part of a generated Caddyfile.
func (o GlobalOptions) Normalize() (GlobalOptions, error) {
	po, err := proxy.GlobalOptions{
		Profile:          o.Profile,
		ReadHeader:       o.ReadHeader,
		ReadBodyIdle:     o.ReadBodyIdle,
		Idle:             o.Idle,
		GracePeriod:      o.GracePeriod,
		MaxHeaderSize:    o.MaxHeaderSize,
		RealIP:           o.RealIP,
		TrustedProxies:   o.TrustedProxies,
		TrustedStrict:    o.TrustedStrict,
		ShieldProxies:    o.ShieldProxies,
		DirectCloudflare: o.DirectCloudflare,
	}.Normalize()
	if err != nil {
		return o, err
	}
	return GlobalOptions{
		Profile:          po.Profile,
		ReadHeader:       po.ReadHeader,
		ReadBodyIdle:     po.ReadBodyIdle,
		Idle:             po.Idle,
		GracePeriod:      po.GracePeriod,
		MaxHeaderSize:    po.MaxHeaderSize,
		RealIP:           po.RealIP,
		TrustedProxies:   po.TrustedProxies,
		TrustedStrict:    po.TrustedStrict,
		ShieldProxies:    po.ShieldProxies,
		DirectCloudflare: po.DirectCloudflare,
	}, nil
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
