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
	if l["caddy.php_fastcgi.read_timeout"] != "120s" || l["caddy.file_server.precompressed"] != "gzip zstd" {
		t.Fatalf("expected fastcgi timeouts and precompressed labels, got fastcgi timeout %q, precompressed %q", l["caddy.php_fastcgi.read_timeout"], l["caddy.file_server.precompressed"])
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

func TestSiteRoutingValidationAndLabels(t *testing.T) {
	r := SiteRouting{
		FallbackMode: FallbackStrict404,
		Rules: []RoutingRule{
			{ID: "r1", Kind: "block", MatchType: "regex", Pattern: `^/(.+)\.php$`, Status: 404, Enabled: true},
			{ID: "r2", Kind: "rewrite", MatchType: "regex", Pattern: `^/profile/([-\w]+)/([-\w]+)$`, Target: "/profile.php?id=$1&action=$2", Enabled: true},
			{ID: "r3", Kind: "redir", MatchType: "prefix", Pattern: "/old-docs", Target: "https://docs.example.com", Status: 301, Enabled: true},
			{ID: "r4", Kind: "rewrite", MatchType: "exact", Pattern: "/disabled", Target: "/x.php", Enabled: false},
		},
	}
	if err := ValidateRouting(&r, false); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
	l := PHPSiteLabelsWithRouting([]db.AppDomain{{Domain: "app.test", EnableHTTPS: true}}, "/srv/htdocs", "/run/app.sock", SiteSecurity{}, r)

	want := map[string]string{
		"caddy.@nd_rule_0.path_regexp": `ndr0 ^/(.+)\.php$`,
		"caddy.route.000_respond":      "@nd_rule_0 404",
		"caddy.@nd_rule_1.path_regexp": `ndr1 ^/profile/([-\w]+)/([-\w]+)$`,
		"caddy.route.001_rewrite":      "@nd_rule_1 /profile.php?id={re.ndr1.1}&action={re.ndr1.2}",
		"caddy.@nd_rule_2.path":        "/old-docs /old-docs/*",
		"caddy.route.002_redir":        "@nd_rule_2 https://docs.example.com 301",
		"caddy.route.003_rewrite":      "@wp_cache {http.matchers.file.relative}",
		"caddy.php_fastcgi.try_files":  "{path} {path}/index.php =404",
	}
	for k, v := range want {
		if l[k] != v {
			t.Errorf("%s = %q, want %q", k, l[k], v)
		}
	}
	if _, ok := l["caddy.rewrite"]; ok {
		t.Error("top-level caddy.rewrite must be moved inside route when custom rules are enabled")
	}

	// Static SPA fallback
	spa := SiteRouting{FallbackMode: FallbackCustom, FallbackEntry: "/index.html"}
	if err := ValidateRouting(&spa, true); err != nil {
		t.Fatalf("static spa validate: %v", err)
	}
	sl := PHPSiteLabelsWithRouting([]db.AppDomain{{Domain: "spa.test", EnableHTTPS: true}}, "/srv/htdocs", "", SiteSecurity{}, spa)
	if sl["caddy.rewrite_fallback"] != "@nd_spa /index.html" {
		t.Errorf("spa fallback = %q", sl["caddy.rewrite_fallback"])
	}

	// Security rejections
	badCases := []SiteRouting{
		{FallbackMode: FallbackCustom, FallbackEntry: "../etc/passwd"},
		{Rules: []RoutingRule{{Kind: "rewrite", MatchType: "exact", Pattern: "/a", Target: "http://127.0.0.1:8080", Enabled: true}}},
		{Rules: []RoutingRule{{Kind: "rewrite", MatchType: "exact", Pattern: "/a", Target: "/.env", Enabled: true}}},
		{Rules: []RoutingRule{{Kind: "rewrite", MatchType: "regex", Pattern: `^/(unclosed`, Target: "/a.php", Enabled: true}}},
		{Rules: []RoutingRule{{Kind: "redir", MatchType: "exact", Pattern: "/a", Target: "javascript:alert(1)", Enabled: true}}},
		{Rules: []RoutingRule{{Kind: "rewrite", MatchType: "exact", Pattern: "/a {bad}", Target: "/a.php", Enabled: true}}},
	}
	for idx, bad := range badCases {
		if err := ValidateRouting(&bad, false); err == nil {
			t.Errorf("badCase[%d] should have failed validation", idx)
		}
	}

	// Verify UnlessFile (!-f) and PreserveQuery ([QSA]) + $0 expansion + normal PHP defaults
	ext := SiteRouting{
		Rules: []RoutingRule{
			{ID: "e1", Kind: "rewrite", MatchType: "regex", Pattern: `^/([^.]+)$`, Target: "/$1.php?src=$0", UnlessFile: true, PreserveQuery: true, Enabled: true},
		},
	}
	if err := ValidateRouting(&ext, false); err != nil {
		t.Fatalf("ext validate: %v", err)
	}
	el := PHPSiteLabelsWithRouting([]db.AppDomain{{Domain: "php.test", EnableHTTPS: true}}, "/srv/htdocs", "/run/php.sock", SiteSecurity{}, ext)
	if el["caddy.@nd_rule_0.not.file"] != "{path} {path}/" {
		t.Errorf("expected not.file guard, got %q", el["caddy.@nd_rule_0.not.file"])
	}
	if el["caddy.route.000_rewrite"] != "@nd_rule_0 /{re.ndr0.1}.php?src={re.ndr0.0}&{query}" {
		t.Errorf("unexpected rewrite label: %q", el["caddy.route.000_rewrite"])
	}
}
