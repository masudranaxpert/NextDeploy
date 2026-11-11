package caddy

import (
	"strings"
	"testing"

	"panel/internal/db"
)

func TestGlobalOptionsNormalizeAndLabels(t *testing.T) {
	o, err := GlobalOptions{ReadHeader: "15S", Idle: "2m", MaxHeaderSize: "16KB", RealIP: "cloudflare", TrustedStrict: true}.Normalize()
	if err != nil {
		t.Fatal(err)
	}
	l := o.Labels()
	if l["caddy.servers.timeouts.read_header"] != "15s" || l["caddy.servers.max_header_size"] != "16KB" {
		t.Errorf("labels: %v", l)
	}
	if !strings.HasPrefix(l["caddy.servers.trusted_proxies"], "static 173.245.48.0/20 ") || l["caddy.servers.trusted_proxies_strict"] == "" {
		t.Errorf("cloudflare ranges missing: %v", l)
	}
	if _, ok := l["caddy.grace_period"]; ok {
		t.Error("an empty value must not create a label")
	}
	for k := range l {
		if !IsGlobalOptionLabel(k) {
			t.Errorf("%s is not recognised as a managed label, so a stale copy could never be removed", k)
		}
	}
	none, _ := GlobalOptions{}.Normalize()
	if len(none.Labels()) != 0 {
		t.Error("the default options add no labels")
	}
}

func TestGlobalOptionsRejectBadInput(t *testing.T) {
	for name, o := range map[string]GlobalOptions{
		"duration text":   {ReadHeader: "soon"},
		"too long":        {Idle: "48h"},
		"too short":       {ReadHeader: "10ms"},
		"label injection": {ReadHeader: "15s\ncaddy.admin=evil"},
		"size":            {MaxHeaderSize: "16 KB; x"},
		"whole internet":  {RealIP: "custom", TrustedProxies: []string{"0.0.0.0/0"}},
		"empty custom":    {RealIP: "custom"},
		"bad mode":        {RealIP: "magic"},
		"bad proxy":       {RealIP: "custom", TrustedProxies: []string{"nope"}},
	} {
		if _, err := o.Normalize(); err == nil {
			t.Errorf("%s must be rejected", name)
		}
	}
}

func TestRouteValidationAndLabels(t *testing.T) {
	ok := func(h string) bool { return strings.Contains(h, ".") && !strings.ContainsAny(h, " /\n") }
	r := db.ProxyRoute{Kind: "proxy", Domain: " App.Example.com ", Target: "http://10.0.0.5:8080/", EnableHTTPS: true, SkipTLSVerify: true}
	if err := ValidateRoute(&r, ok); err != nil {
		t.Fatal(err)
	}
	if r.Domain != "app.example.com" || r.Target != "http://10.0.0.5:8080" || r.SkipTLSVerify {
		t.Errorf("not cleaned: %+v", r)
	}
	l := RouteLabels(r)
	if l["caddy"] != "app.example.com" || l["caddy.reverse_proxy"] != "http://10.0.0.5:8080" || l["caddy.reverse_proxy.stream_close_delay"] != "10m" {
		t.Errorf("labels: %v", l)
	}
	tls := db.ProxyRoute{Kind: "proxy", Domain: "b.example.com", Target: "https://10.0.0.6", SkipTLSVerify: true, EnableHTTPS: true}
	if err := ValidateRoute(&tls, ok); err != nil {
		t.Fatal(err)
	}
	if RouteLabels(tls)["caddy.reverse_proxy.transport"] != "http" {
		t.Error("self-signed target needs the transport block")
	}
	red := db.ProxyRoute{Kind: "redirect", Domain: "old.example.com", Target: "https://new.example.com", KeepPath: true, RedirectCode: 999, EnableHTTPS: true}
	if err := ValidateRoute(&red, ok); err != nil {
		t.Fatal(err)
	}
	if got := RouteLabels(red)["caddy.redir"]; got != "https://new.example.com{uri} 301" {
		t.Errorf("redirect label = %q", got)
	}
	for name, bad := range map[string]db.ProxyRoute{
		"bad domain":  {Kind: "proxy", Domain: "no spaces.com", Target: "http://1.2.3.4"},
		"path":        {Kind: "proxy", Domain: "a.example.com", Target: "http://1.2.3.4/admin"},
		"credentials": {Kind: "proxy", Domain: "a.example.com", Target: "http://u:p@1.2.3.4"},
		"scheme":      {Kind: "proxy", Domain: "a.example.com", Target: "ftp://1.2.3.4"},
		"loop":        {Kind: "proxy", Domain: "a.example.com", Target: "https://a.example.com"},
		"no kind":     {Domain: "a.example.com", Target: "http://1.2.3.4"},
	} {
		b := bad
		if err := ValidateRoute(&b, ok); err == nil {
			t.Errorf("%s must be rejected", name)
		}
	}
}

const rootBase = `services:
  caddy:
    image: lucaslorentz/caddy-docker-proxy:ci-alpine
    labels:
      caddy.servers.timeouts.idle: 9m
      caddy.grace_period: 1h
      keep.me: "yes"
  panel:
    image: x
`

func TestRootComposeCarriesAndRemovesOptionLabels(t *testing.T) {
	o, _ := GlobalOptions{Idle: "2m", RealIP: "custom", TrustedProxies: []string{"10.0.0.0/8"}}.Normalize()
	out, err := GenerateRootStackCompose([]byte(rootBase), "", true, false, "", "", nil, o)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, want := range []string{"caddy.servers.timeouts.idle: 2m", "caddy.servers.trusted_proxies: static 10.0.0.0/8", "keep.me"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in:\n%s", want, s)
		}
	}
	if strings.Contains(s, "9m") || strings.Contains(s, "caddy.grace_period") {
		t.Errorf("stale option labels were not removed:\n%s", s)
	}
	back, _ := GenerateRootStackCompose(out, "", true, false, "", "", nil, GlobalOptions{})
	if strings.Contains(string(back), "caddy.servers") {
		t.Errorf("switching back to defaults must remove the labels:\n%s", back)
	}
}

func TestGlobalOptionsCombinedShieldAndDirectCloudflare(t *testing.T) {
	o, err := GlobalOptions{
		RealIP:           "none",
		ShieldProxies:    []string{"107.189.6.222"},
		DirectCloudflare: true,
	}.Normalize()
	if err != nil {
		t.Fatal(err)
	}
	l := o.Labels()
	tp := l["caddy.servers.trusted_proxies"]
	if !strings.Contains(tp, "173.245.48.0/20") || !strings.Contains(tp, "107.189.6.222") {
		t.Fatalf("expected combined Cloudflare + Shield ranges, got %q", tp)
	}
	if l["caddy.servers.trusted_proxies_strict"] != `{{""}}` {
		t.Fatalf("expected trusted_proxies_strict when DirectCloudflare=true, got %v", l)
	}
	if l["caddy.servers.client_ip_headers"] != "CF-Connecting-IP X-Forwarded-For" {
		t.Fatalf("expected client_ip_headers when DirectCloudflare=true, got %v", l)
	}

	// Shield-only without Cloudflare does not set client_ip_headers
	shieldOnly, err := GlobalOptions{RealIP: "none", ShieldProxies: []string{"107.189.6.222"}}.Normalize()
	if err != nil {
		t.Fatal(err)
	}
	sl := shieldOnly.Labels()
	if sl["caddy.servers.trusted_proxies"] != "static 107.189.6.222" {
		t.Fatalf("unexpected shield-only trusted_proxies: %v", sl)
	}
	if _, ok := sl["caddy.servers.client_ip_headers"]; ok {
		t.Fatalf("did not expect client_ip_headers for shield-only without CF: %v", sl)
	}
}

func TestUntrustedDirectProxyHeaderEnforcement(t *testing.T) {
	// Direct domain with TrustedProxy=false (default) must strip CF-Connecting-IP and forward {remote_host}
	directUntrusted := GenerateLabels(db.AppDomain{Domain: "a.example.com", Service: "web", Port: 3000, EnableHTTPS: true})
	if directUntrusted["caddy.reverse_proxy.header_up_0"] != "X-Real-IP {remote_host}" ||
		directUntrusted["caddy.reverse_proxy.header_up_1"] != "X-Forwarded-For {remote_host}" ||
		directUntrusted["caddy.reverse_proxy.header_up_2"] != "-CF-Connecting-IP" {
		t.Fatalf("expected untrusted direct proxy headers, got %v", directUntrusted)
	}

	// Direct domain with TrustedProxy=true must NOT overwrite client IP with {remote_host}
	directTrusted := GenerateLabels(db.AppDomain{Domain: "b.example.com", Service: "web", Port: 3000, EnableHTTPS: true, TrustedProxy: true})
	if _, ok := directTrusted["caddy.reverse_proxy.header_up_0"]; ok {
		t.Fatalf("did not expect untrusted header_up overrides when TrustedProxy=true: %v", directTrusted)
	}

	// Shielded domain must NOT overwrite forwarded headers with {remote_host} (Shield's IP)
	sid := int64(1)
	shielded := GenerateLabels(db.AppDomain{Domain: "c.example.com", Service: "web", Port: 3000, ShieldNodeID: &sid})
	if _, ok := shielded["caddy.reverse_proxy.header_up_0"]; ok {
		t.Fatalf("did not expect untrusted header_up overrides on shielded domain: %v", shielded)
	}

	// ProxyRoute: direct untrusted vs direct trusted vs shielded
	rUntrusted := RouteLabels(db.ProxyRoute{Kind: "proxy", Domain: "r1.example.com", Target: "http://10.0.0.5:8080", EnableHTTPS: true})
	if rUntrusted["caddy.reverse_proxy.header_up_2"] != "-CF-Connecting-IP" {
		t.Fatalf("expected untrusted proxy route headers, got %v", rUntrusted)
	}
	rTrusted := RouteLabels(db.ProxyRoute{Kind: "proxy", Domain: "r2.example.com", Target: "http://10.0.0.5:8080", EnableHTTPS: true, TrustedProxy: true})
	if _, ok := rTrusted["caddy.reverse_proxy.header_up_0"]; ok {
		t.Fatalf("did not expect untrusted header_up on trusted proxy route: %v", rTrusted)
	}
}
