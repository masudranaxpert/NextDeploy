package caddy

import (
	"strings"
	"testing"

	"panel/internal/db"
)

func TestSiteSecurityLabels(t *testing.T) {
	sec := SiteSecurity{
		BlockedIPs: []string{"1.2.3.4", "10.0.0.0/8"}, AllowOnly: []string{"203.0.113.0/24"},
		BasicAuthUser: "bob", BasicAuthHash: "$2a$10$x", BlockXMLRPC: true, BlockUploadPHP: true,
		BlockScanners: true, HSTS: true, FrameProtection: true,
	}
	l := PHPSiteLabels([]db.AppDomain{{Domain: "a.test", EnableHTTPS: true}}, "/srv", "/s.sock", sec)
	want := map[string]string{
		"caddy.@blocked_ip.client_ip":      "1.2.3.4 10.0.0.0/8",
		"caddy.@not_allowed.not.client_ip": "203.0.113.0/24",
		"caddy.@xmlrpc.path":               "/xmlrpc.php",
		"caddy.basic_auth.bob":             "$2a$10$x",
		"caddy.header.X-Frame-Options":     "SAMEORIGIN",
		"caddy.@media.path":                "*.ico *.gif *.jpg *.jpeg *.png *.svg *.woff *.woff2 *.webp *.avif *.ttf *.eot",
		"caddy.header_0":                   `@media Cache-Control "public, max-age=2592000, immutable"`,
		"caddy.@code.path":                 "*.css *.js",
		"caddy.header_1":                   `@code Cache-Control "public, max-age=604800, stale-while-revalidate=86400"`,
	}
	for k, v := range want {
		if l[k] != v {
			t.Errorf("%s = %q, want %q", k, l[k], v)
		}
	}
	// every respond rule has its own numbered key, so none overwrites another
	n := 0
	for k := range l {
		if strings.HasPrefix(k, "caddy.respond_") {
			n++
		}
	}
	if n != 6 {
		t.Errorf("respond rules = %d, want 6", n)
	}
	if empty := PHPSiteLabels([]db.AppDomain{{Domain: "a.test", EnableHTTPS: true}}, "/srv", "", SiteSecurity{}); len(empty) == 0 || empty["caddy.@blocked_ip.client_ip"] != "" {
		t.Error("an empty security setting must add no rules")
	}
}

func TestWordPressStaticCacheLabels(t *testing.T) {
	l := PHPSiteLabels([]db.AppDomain{{Domain: "wp.test", EnableHTTPS: true}}, "/data/workspaces/app/htdocs", "/run/app.sock", SiteSecurity{})
	if l["caddy.rewrite"] != "@wp_cache {http.matchers.file.relative}" {
		t.Fatalf("expected rewrite label, got %q", l["caddy.rewrite"])
	}
	if !strings.Contains(l["caddy.@wp_cache.file.try_files"], "/wp-content/cache/all{path}/index.html") {
		t.Fatalf("missing WP Fastest Cache try_files: %q", l["caddy.@wp_cache.file.try_files"])
	}
	if !strings.Contains(l["caddy.@wp_cache.0_not.header_regexp"], "wordpress_logged_in") {
		t.Fatalf("missing logged in cookie bypass: %q", l["caddy.@wp_cache.0_not.header_regexp"])
	}
	// Static site without socket must not have cache rules
	staticLabels := PHPSiteLabels([]db.AppDomain{{Domain: "static.test", EnableHTTPS: true}}, "/data/workspaces/app/htdocs", "", SiteSecurity{})
	if _, ok := staticLabels["caddy.rewrite"]; ok {
		t.Fatalf("static site should not have wp_cache rewrite label")
	}
}

func TestNormalizeIPList(t *testing.T) {
	got, err := NormalizeIPList([]string{" 1.2.3.4 ", "1.2.3.4", "10.1.2.3/8", "::ffff:5.6.7.8", "2001:db8::1"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"1.2.3.4", "10.0.0.0/8", "5.6.7.8", "2001:db8::1"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("got %v, want %v", got, want)
	}
	for _, bad := range []string{"not-an-ip", "1.2.3.4/99", "1.2.3", "a b"} {
		if _, err := NormalizeIPList([]string{bad}); err == nil {
			t.Errorf("%q must be rejected", bad)
		}
	}
}
