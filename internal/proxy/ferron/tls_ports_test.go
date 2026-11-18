package ferron

import (
	"regexp"
	"strings"
	"testing"

	"panel/internal/proxy"
)

// A port-less host with an explicit `tls {}` block makes Ferron 3.0.0-rc.11 speak TLS on :80,
// breaking every plain-HTTP site. TLS sites must be bound to :443 explicitly, with a separate
// plain :80 block that redirects (ACME HTTP-01 is answered before the redirect).
func TestTLSSiteUsesExplicit443AndHTTPRedirect(t *testing.T) {
	s := proxy.Site{ID: "app-x", Hosts: []string{"a.example.com", "www.example.com"}, TLS: proxy.TLSAuto}
	h, err := formatHostsHeader(s)
	if err != nil {
		t.Fatal(err)
	}
	if h != "a.example.com:443, www.example.com:443" {
		t.Fatalf("hosts header = %q", h)
	}
	r := httpsRedirectBlock(s)
	for _, want := range []string{"http a.example.com:80, www.example.com:80 {", "status 308", `location "https://{{request.host}}{{request.uri}}"`} {
		if !strings.Contains(r, want) {
			t.Fatalf("redirect block missing %q:\n%s", want, r)
		}
	}
	plain := proxy.Site{ID: "p", Hosts: []string{"b.example.com"}, TLS: proxy.TLSNone}
	if h, _ := formatHostsHeader(plain); h != "http b.example.com:80" {
		t.Fatalf("plain hosts header = %q", h)
	}
	if httpsRedirectBlock(plain) != "" {
		t.Fatal("plain site must not get a redirect block")
	}
	if got := hostWithPort("[2001:db8::1]", "443"); got != "[2001:db8::1]:443" {
		t.Fatalf("ipv6 = %q", got)
	}
	if got := hostWithPort("a.example.com:8443", "443"); got != "a.example.com:8443" {
		t.Fatalf("explicit port kept = %q", got)
	}
}

func TestCaddyStylePrefixGlobIsNotLiteral(t *testing.T) {
	for _, in := range []string{"/static/*", "/static/", "/static", "static*"} {
		if got := normalizePrefix(in); got != "/static" {
			t.Errorf("normalizePrefix(%q) = %q", in, got)
		}
	}
	re := regexp.MustCompile(prefixMatchRegex("/static/*"))
	if !re.MatchString("/static/hello.txt") || re.MatchString("/staticx") {
		t.Fatalf("prefix regex %q wrong", re)
	}
}

func TestHideGlobPatterns(t *testing.T) {
	re := regexp.MustCompile(buildHideRegex([]string{".git", ".env*", "*.sql"}, false))
	for _, p := range []string{"/.env", "/.env.local", "/x/.env.production", "/.git/config", "/dump.sql"} {
		if !re.MatchString(p) {
			t.Errorf("%s should be hidden (re=%s)", p, re)
		}
	}
	for _, p := range []string{"/hello.txt", "/environment.txt", "/static/app.js"} {
		if re.MatchString(p) {
			t.Errorf("%s must not be hidden (re=%s)", p, re)
		}
	}
}

// The same domain added twice (two domain rows of one PHP site) must not produce two host
// blocks: Ferron's strict_hosts would reject the whole configuration.
func TestDuplicateHostsAcrossSitesRenderOnce(t *testing.T) {
	mk := func(id string) proxy.Site {
		return proxy.Site{ID: id, Kind: proxy.SiteKindAppProxy, Hosts: []string{"dup.example.com"}, TLS: proxy.TLSNone,
			Upstream: &proxy.UpstreamRef{Container: "app", Port: 80}}
	}
	files, err := New().Render(&proxy.State{Sites: []proxy.Site{mk("a"), mk("b")}})
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for name, body := range files {
		n += strings.Count(body, "http dup.example.com:80 {")
		_ = name
	}
	if n != 1 {
		t.Fatalf("host block rendered %d times, want 1", n)
	}
}

// Ferron reads the protocol prefix once per header; repeating it ("http a:80, http b:80")
// creates an empty host named "http " that clashes between files under strict_hosts.
func TestHTTPPrefixOncePerHeader(t *testing.T) {
	plain := proxy.Site{ID: "p", Hosts: []string{"a.example.com", "www.a.example.com", "http c.example.com"}, TLS: proxy.TLSNone}
	h, err := formatHostsHeader(plain)
	if err != nil {
		t.Fatal(err)
	}
	if h != "http a.example.com:80, www.a.example.com:80, c.example.com" {
		t.Fatalf("plain header = %q", h)
	}
	tlsSite := proxy.Site{ID: "t", Hosts: []string{"x.example.com", "www.x.example.com"}, TLS: proxy.TLSAuto}
	if r := httpsRedirectBlock(tlsSite); strings.Count(r, "http ") != 1 {
		t.Fatalf("redirect header repeats the prefix:\n%s", r)
	}
}
