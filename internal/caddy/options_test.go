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
	if l["caddy"] != "app.example.com" || l["caddy.reverse_proxy"] != "http://10.0.0.5:8080" {
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
