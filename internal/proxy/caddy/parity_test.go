package caddy

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"testing"

	icaddy "panel/internal/caddy"
	"panel/internal/db"
	"panel/internal/proxy"
	"panel/internal/proxy/state"

	"gopkg.in/yaml.v3"
)

func assertMapsByteEqual(t *testing.T, label string, got, want map[string]string) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("%s: key count mismatch: got %d, want %d\ngot keys:  %v\nwant keys: %v",
			label, len(got), len(want), sortedKeys(got), sortedKeys(want))
	}
	for k, wv := range want {
		gv, ok := got[k]
		if !ok {
			t.Errorf("%s: missing key %q (want %q)", label, k, wv)
			continue
		}
		if gv != wv {
			t.Errorf("%s: key %q = %q, want %q", label, k, gv, wv)
		}
	}
	for k, gv := range got {
		if _, ok := want[k]; !ok {
			t.Errorf("%s: unexpected extra key %q = %q", label, k, gv)
		}
	}
	if gotYAML, wantYAML := icaddy.LabelsToYAML(got), icaddy.LabelsToYAML(want); gotYAML != wantYAML {
		t.Errorf("%s: LabelsToYAML mismatch:\n--- got ---\n%s--- want ---\n%s", label, gotYAML, wantYAML)
	}
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func TestParity_AppProxySingleDomainVariants(t *testing.T) {
	d := New()
	shieldID := int64(7)

	cases := []struct {
		name string
		site proxy.Site
		up   proxy.UpstreamRef
		dom  db.AppDomain
	}{
		{
			name: "https_www_untrusted",
			site: proxy.Site{
				ID:    "app:a1:dom:1",
				AppID: "a1",
				Hosts: []string{"app.example.com", "www.app.example.com"},
				TLS:   proxy.TLSAuto,
				Kind:  proxy.SiteKindAppProxy,
			},
			up: proxy.UpstreamRef{Port: 3000, Service: "web"},
			dom: db.AppDomain{
				Domain: "app.example.com", Service: "web", Port: 3000,
				EnableHTTPS: true, EnableWWW: true,
			},
		},
		{
			name: "https_trusted_cloudflare",
			site: proxy.Site{
				ID:           "app:a1:dom:2",
				Hosts:        []string{"cf.example.com"},
				TLS:          proxy.TLSAuto,
				TrustedProxy: true,
				Kind:         proxy.SiteKindAppProxy,
				Upstream:     &proxy.UpstreamRef{Port: 8080, Service: "api"},
			},
			up: proxy.UpstreamRef{Port: 3000},
			dom: db.AppDomain{
				Domain: "cf.example.com", Service: "api", Port: 8080,
				EnableHTTPS: true, TrustedProxy: true,
			},
		},
		{
			name: "behind_shield_internal_tls",
			site: proxy.Site{
				ID:           "app:a1:dom:3",
				Hosts:        []string{"shielded.example.com", "www.shielded.example.com"},
				TLS:          proxy.TLSInternal,
				BehindShield: true,
				Kind:         proxy.SiteKindAppProxy,
				Upstream:     &proxy.UpstreamRef{Port: 4000, Service: "web"},
			},
			dom: db.AppDomain{
				Domain: "shielded.example.com", Service: "web", Port: 4000,
				EnableHTTPS: true, EnableWWW: true, ShieldNodeID: &shieldID,
			},
		},
		{
			name: "local_dev_domain_internal_tls_untrusted",
			site: proxy.Site{
				ID:       "app:a1:dom:4",
				Hosts:    []string{"myapp.local"},
				TLS:      proxy.TLSInternal,
				Kind:     proxy.SiteKindAppProxy,
				Upstream: &proxy.UpstreamRef{Port: 3000},
			},
			dom: db.AppDomain{
				Domain: "myapp.local", Port: 3000, EnableHTTPS: true,
			},
		},
		{
			name: "http_only_with_www",
			site: proxy.Site{
				ID:       "app:a1:dom:5",
				Hosts:    []string{"plain.example.com", "www.plain.example.com"},
				TLS:      proxy.TLSNone,
				Kind:     proxy.SiteKindAppProxy,
				Upstream: &proxy.UpstreamRef{Port: 0},
			},
			dom: db.AppDomain{
				Domain: "plain.example.com", Port: 0, EnableHTTPS: false, EnableWWW: true,
			},
		},
		{
			name: "static_and_media_paths_with_security_headers",
			site: proxy.Site{
				ID:    "app:a1:dom:6",
				Hosts: []string{"assets.example.com"},
				TLS:   proxy.TLSAuto,
				Kind:  proxy.SiteKindAppProxy,
				Headers: []proxy.HeaderOp{
					{Action: proxy.HeaderSet, Name: "X-Frame-Options", Value: "SAMEORIGIN"},
					{Action: proxy.HeaderSet, Name: "X-Content-Type-Options", Value: "nosniff"},
					{Action: proxy.HeaderSet, Name: "Referrer-Policy", Value: "strict-origin-when-cross-origin"},
					{Action: proxy.HeaderSet, Name: "X-XSS-Protection", Value: "1; mode=block"},
				},
				Paths: []proxy.PathRoute{
					{
						Prefix: "/media/*", StripPrefix: true, Priority: 2,
						Handler: proxy.Handler{Kind: proxy.HandlerStaticDir, Static: &proxy.StaticDirSpec{Root: "/data/workspaces/a1/media"}},
					},
					{
						Prefix: "/assets/*", StripPrefix: true, Priority: 1,
						Handler: proxy.Handler{Kind: proxy.HandlerStaticDir, Static: &proxy.StaticDirSpec{Root: "/data/workspaces/a1/dist"}},
					},
				},
				Upstream: &proxy.UpstreamRef{Port: 3000},
			},
			dom: db.AppDomain{
				Domain: "assets.example.com", Port: 3000, EnableHTTPS: true,
				ServeStatic: true, StaticURLPrefix: "/assets", StaticPath: "/data/workspaces/a1/dist",
				ServeMedia: true, MediaURLPrefix: "/media", MediaPath: "/data/workspaces/a1/media",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := d.AppLabels([]proxy.Site{tc.site}, tc.up)
			want := icaddy.GenerateLabels(tc.dom)
			assertMapsByteEqual(t, tc.name, got, want)
		})
	}
}

func TestParity_AppProxyMultiDomainService(t *testing.T) {
	d := New()
	shieldID := int64(3)

	// Pass out of ID order to verify identical sortedDomains behavior.
	sites := []proxy.Site{
		{
			ID: "app:a1:dom:20", DomainID: 20, Hosts: []string{"two.example.com"},
			TLS: proxy.TLSNone, Kind: proxy.SiteKindAppProxy,
			Upstream: &proxy.UpstreamRef{Port: 5000},
		},
		{
			ID: "app:a1:dom:10", DomainID: 10, Hosts: []string{"one.example.com", "www.one.example.com"},
			TLS: proxy.TLSInternal, BehindShield: true, Kind: proxy.SiteKindAppProxy,
			Paths: []proxy.PathRoute{{
				Prefix: "/static/*", StripPrefix: true, Priority: 1,
				Handler: proxy.Handler{Kind: proxy.HandlerStaticDir, Static: &proxy.StaticDirSpec{Root: "/data/workspaces/a1/public"}},
			}},
			Upstream: &proxy.UpstreamRef{Port: 3000},
		},
	}
	domains := []db.AppDomain{
		{ID: 20, Domain: "two.example.com", Port: 5000, EnableHTTPS: false},
		{
			ID: 10, Domain: "one.example.com", Port: 3000, EnableHTTPS: true, EnableWWW: true,
			ShieldNodeID: &shieldID, ServeStatic: true, StaticURLPrefix: "/static", StaticPath: "/data/workspaces/a1/public",
		},
	}

	got := d.AppLabels(sites, proxy.UpstreamRef{Port: 3000})
	want := icaddy.GenerateServiceLabels(domains)
	assertMapsByteEqual(t, "multi_domain_service", got, want)
}

func TestParity_AppProxyMixedStaticAndProxyPaths(t *testing.T) {
	d := New()
	site := proxy.Site{
		ID:       "app:a1:dom:proxy_path",
		Hosts:    []string{"mix.example.com"},
		TLS:      proxy.TLSAuto,
		Kind:     proxy.SiteKindAppProxy,
		Upstream: &proxy.UpstreamRef{Port: 3000},
		Paths: []proxy.PathRoute{
			{
				Prefix: "/static/*", StripPrefix: true, Priority: 1,
				Handler: proxy.Handler{Kind: proxy.HandlerStaticDir, Static: &proxy.StaticDirSpec{Root: "/data/workspaces/a1/static"}},
			},
			{
				Prefix: "/api/*", Priority: 2,
				Handler: proxy.Handler{Kind: proxy.HandlerProxy, Upstream: &proxy.UpstreamRef{Port: 4000}},
			},
		},
	}
	got := d.AppLabels([]proxy.Site{site}, proxy.UpstreamRef{Port: 3000})
	want := map[string]string{
		"caddy":                                    "mix.example.com",
		"caddy.header.X-Frame-Options":             "SAMEORIGIN",
		"caddy.header.X-Content-Type-Options":      "nosniff",
		"caddy.header.Referrer-Policy":             "strict-origin-when-cross-origin",
		"caddy.header.X-XSS-Protection":            "1; mode=block",
		"caddy.1_handle_path":                      "/static/*",
		"caddy.1_handle_path.0_root":               "* /data/workspaces/a1/static",
		"caddy.1_handle_path.1_file_server":        `{{""}}`,
		"caddy.1_handle_path.1_file_server.hide":   defaultStaticHide,
		"caddy.2_reverse_proxy":                    "/api/* {{upstreams 4000}}",
		"caddy.2_reverse_proxy.stream_close_delay": "10m",
		"caddy.2_reverse_proxy.header_up_0":        "X-Real-IP {remote_host}",
		"caddy.2_reverse_proxy.header_up_1":        "X-Forwarded-For {remote_host}",
		"caddy.2_reverse_proxy.header_up_2":        "-CF-Connecting-IP",
		"caddy.3_reverse_proxy":                    "{{upstreams 3000}}",
		"caddy.3_reverse_proxy.stream_close_delay": "10m",
		"caddy.3_reverse_proxy.header_up_0":        "X-Real-IP {remote_host}",
		"caddy.3_reverse_proxy.header_up_1":        "X-Forwarded-For {remote_host}",
		"caddy.3_reverse_proxy.header_up_2":        "-CF-Connecting-IP",
	}
	assertMapsByteEqual(t, "mixed_static_and_proxy_paths", got, want)
}

func TestParity_SlotPreviewAndGatewayPorts(t *testing.T) {
	d := New()

	// 1. Active slot with 2 domains (1 trusted + static path, 1 untrusted) and 2 HTTP gateway ports.
	slotSites := []proxy.Site{
		{
			ID: "app:a1:dom:1", DomainID: 1, Slot: "blue",
			Hosts: []string{"prod.example.com", "www.prod.example.com"},
			TLS:   proxy.TLSAuto, TrustedProxy: true, Kind: proxy.SiteKindAppProxy,
			Paths: []proxy.PathRoute{{
				Prefix: "/assets/*", StripPrefix: true, Priority: 1,
				Handler: proxy.Handler{Kind: proxy.HandlerStaticDir, Static: &proxy.StaticDirSpec{Root: "/data/workspaces/a1/assets"}},
			}},
			Upstream: &proxy.UpstreamRef{Port: 3000, LBTryDuration: "5s", FailDuration: "5s"},
		},
		{
			ID: "app:a1:dom:2", DomainID: 2, Slot: "blue",
			Hosts:    []string{"alt.example.com"},
			TLS:      proxy.TLSNone,
			Kind:     proxy.SiteKindAppProxy,
			Upstream: &proxy.UpstreamRef{Port: 3000, LBTryDuration: "5s", FailDuration: "5s"},
		},
		{
			ID: "gateway:a1:4013", Slot: "blue", Hosts: []string{":4013"}, TLS: proxy.TLSNone,
			Kind:     proxy.SiteKindAppProxy,
			Upstream: &proxy.UpstreamRef{Port: 3000, LBTryDuration: "5s", FailDuration: "5s"},
		},
		{
			ID: "gateway:a1:8080", Slot: "blue", Hosts: []string{"http://127.0.0.1:8080"}, TLS: proxy.TLSNone,
			Kind:     proxy.SiteKindAppProxy,
			Upstream: &proxy.UpstreamRef{Port: 80, LBTryDuration: "5s", FailDuration: "5s"},
		},
	}
	wantActive := icaddy.GenerateSlotLabels(
		[]db.AppDomain{
			{
				ID: 1, Domain: "prod.example.com", Port: 3000, EnableHTTPS: true, EnableWWW: true, TrustedProxy: true,
				ServeStatic: true, StaticURLPrefix: "/assets", StaticPath: "/data/workspaces/a1/assets",
			},
			{ID: 2, Domain: "alt.example.com", Port: 3000, EnableHTTPS: false},
		},
		[]icaddy.HostPortBinding{
			{HostPort: 4013, ContainerPort: 3000, BindIP: "0.0.0.0", Protocol: "tcp", Direct: false},
			{HostPort: 8080, ContainerPort: 80, BindIP: "127.0.0.1", Protocol: "tcp", Direct: false},
		},
		nil,
		false,
	)
	assertMapsByteEqual(t, "slot_active_domains_and_gateways", d.AppLabels(slotSites, proxy.UpstreamRef{Port: 3000}), wantActive)

	// 2. Gateway-only slot (no domains).
	gwOnly := []proxy.Site{
		{
			ID: "gateway:a1:18080", Slot: "blue", Hosts: []string{":18080"}, TLS: proxy.TLSNone,
			Kind: proxy.SiteKindAppProxy, Upstream: &proxy.UpstreamRef{Port: 80},
		},
	}
	wantGWOnly := icaddy.GenerateSlotLabels(nil, []icaddy.HostPortBinding{{HostPort: 18080, ContainerPort: 80, BindIP: "0.0.0.0", Protocol: "tcp"}}, nil, false)
	assertMapsByteEqual(t, "slot_gateway_only", d.AppLabels(gwOnly, proxy.UpstreamRef{}), wantGWOnly)

	// 3. Standby preview-only slot.
	pvHost := icaddy.SlotPreviewDomain("prod.example.com", "green")
	pvSite := []proxy.Site{
		{
			ID: "slot:a1:green", Slot: "green", IsPreview: true,
			Hosts: []string{pvHost}, TLS: proxy.TLSAuto, Kind: proxy.SiteKindAppProxy,
			Upstream: &proxy.UpstreamRef{Port: 3000, LBTryDuration: "5s", FailDuration: "5s"},
		},
	}
	wantPreview := icaddy.GenerateSlotLabels(nil, nil, &db.AppDomain{Domain: pvHost, Port: 3000, EnableHTTPS: true}, true)
	assertMapsByteEqual(t, "slot_preview_only", d.AppLabels(pvSite, proxy.UpstreamRef{Port: 3000}), wantPreview)
}

func TestParity_PHPSiteSecurityRoutingAndFallbacks(t *testing.T) {
	d := New()
	shieldID := int64(9)

	fullSec := proxy.SecuritySpec{
		BlockedIPs:      []string{"1.2.3.4", "10.0.0.0/8"},
		AllowOnly:       []string{"203.0.113.0/24"},
		BasicAuthUser:   "bob",
		BasicAuthHash:   "$2a$10$abcdefghijklmnopqrstuu",
		BlockXMLRPC:     true,
		BlockUploadPHP:  true,
		BlockScanners:   true,
		HSTS:            true,
		FrameProtection: true,
	}
	fullRouting := proxy.SiteRouting{
		FallbackMode: proxy.FallbackStrict404,
		Rules: []proxy.RoutingRule{
			{ID: "r1", Kind: "block", MatchType: "regex", Pattern: `^/(.+)\.sql$`, Status: 403, Enabled: true},
			{ID: "r2", Kind: "rewrite", MatchType: "regex", Pattern: `^/([^.]+)$`, Target: "/$1.php?src=$0", UnlessFile: true, RequireTargetFile: true, PreserveQuery: true, Enabled: true},
			{ID: "r3", Kind: "rewrite", MatchType: "prefix", Pattern: "/blog/*", Target: "/index.php?p=blog", PreserveQuery: true, Enabled: true},
			{ID: "r4", Kind: "redir", MatchType: "exact", Pattern: "/old", Target: "https://new.example.com/landing", Status: 302, Enabled: true},
			{ID: "r5", Kind: "block", MatchType: "exact", Pattern: "/disabled", Status: 404, Enabled: false},
		},
	}

	// 1. WordPress/Laravel PHP site with full security + full routing + strict_404 fallback + multi-domain.
	phpSites := []proxy.Site{
		{
			ID: "php:app1:dom:2", DomainID: 2, AppID: "app1",
			Hosts: []string{"shop.example.com"}, TLS: proxy.TLSInternal, BehindShield: true,
			Kind: proxy.SiteKindPHP, Security: &fullSec,
			PHP: &proxy.PHPSpec{
				DocRoot:       "/data/workspaces/app1/htdocs",
				Socket:        "/data/workspaces/.nd-php/u1-v83/run/app1.sock",
				FallbackMode:  fullRouting.FallbackMode,
				FallbackEntry: fullRouting.FallbackEntry,
				Routing:       fullRouting.Rules,
				Cache:         &proxy.PHPCacheSpec{WordPressPageCache: true},
				HTTPS:         true,
			},
		},
		{
			ID: "php:app1:dom:1", DomainID: 1, AppID: "app1",
			Hosts: []string{"main.example.com", "www.main.example.com"}, TLS: proxy.TLSAuto,
			Kind: proxy.SiteKindPHP, Security: &fullSec,
			PHP: &proxy.PHPSpec{
				DocRoot:       "/data/workspaces/app1/htdocs",
				Socket:        "/data/workspaces/.nd-php/u1-v83/run/app1.sock",
				FallbackMode:  fullRouting.FallbackMode,
				FallbackEntry: fullRouting.FallbackEntry,
				Routing:       fullRouting.Rules,
				Cache:         &proxy.PHPCacheSpec{WordPressPageCache: true},
				HTTPS:         true,
			},
		},
	}
	wantPHP := icaddy.PHPSiteLabelsWithRouting(
		[]db.AppDomain{
			{ID: 2, Domain: "shop.example.com", EnableHTTPS: true, ShieldNodeID: &shieldID},
			{ID: 1, Domain: "main.example.com", EnableHTTPS: true, EnableWWW: true},
		},
		"/data/workspaces/app1/htdocs",
		"/data/workspaces/.nd-php/u1-v83/run/app1.sock",
		fullSec,
		fullRouting,
	)
	assertMapsByteEqual(t, "php_full_security_routing_strict404_multi_domain", d.AppLabels(phpSites, proxy.UpstreamRef{}), wantPHP)

	// 2. PHP default fallback without enabled custom rules (top-level @wp_cache rewrite) + trusted proxy.
	phpDefault := proxy.Site{
		ID: "php:wp", Hosts: []string{"wp.example.com"}, TLS: proxy.TLSAuto, TrustedProxy: true,
		Kind: proxy.SiteKindPHP,
		PHP: &proxy.PHPSpec{
			DocRoot: "/srv/wp", Socket: "/data/workspaces/.nd-php/u1-v83/run/wp.sock",
			FallbackMode: proxy.FallbackDefault,
		},
	}
	wantDefault := icaddy.PHPSiteLabelsWithRouting(
		[]db.AppDomain{{Domain: "wp.example.com", EnableHTTPS: true, TrustedProxy: true}},
		"/srv/wp", "/data/workspaces/.nd-php/u1-v83/run/wp.sock", icaddy.SiteSecurity{}, icaddy.SiteRouting{},
	)
	assertMapsByteEqual(t, "php_default_fallback_trusted", d.AppLabels([]proxy.Site{phpDefault}, proxy.UpstreamRef{}), wantDefault)

	// 3. PHP custom fallback entry (app.php) + HTTP-only.
	phpCustom := proxy.Site{
		ID: "php:custom", Hosts: []string{"api.local"}, TLS: proxy.TLSNone,
		Kind: proxy.SiteKindPHP,
		PHP: &proxy.PHPSpec{
			DocRoot: "/srv/api/public", Socket: "/run/api.sock",
			FallbackMode: proxy.FallbackCustom, FallbackEntry: "app.php",
		},
	}
	wantCustom := icaddy.PHPSiteLabelsWithRouting(
		[]db.AppDomain{{Domain: "api.local", EnableHTTPS: false}},
		"/srv/api/public", "/run/api.sock", icaddy.SiteSecurity{},
		icaddy.SiteRouting{FallbackMode: icaddy.FallbackCustom, FallbackEntry: "app.php"},
	)
	assertMapsByteEqual(t, "php_custom_fallback_http_only", d.AppLabels([]proxy.Site{phpCustom}, proxy.UpstreamRef{}), wantCustom)

	// 4. Static SPA site with FallbackCustom (no php_fastcgi, no @wp_cache).
	staticSPA := proxy.Site{
		ID: "php:spa", Hosts: []string{"spa.example.com", "www.spa.example.com"}, TLS: proxy.TLSAuto,
		Kind: proxy.SiteKindStatic,
		PHP: &proxy.PHPSpec{
			DocRoot: "/srv/spa/dist", FallbackMode: proxy.FallbackCustom, FallbackEntry: "index.html",
		},
	}
	wantStatic := icaddy.PHPSiteLabelsWithRouting(
		[]db.AppDomain{{Domain: "spa.example.com", EnableHTTPS: true, EnableWWW: true}},
		"/srv/spa/dist", "", icaddy.SiteSecurity{},
		icaddy.SiteRouting{FallbackMode: icaddy.FallbackCustom, FallbackEntry: "index.html"},
	)
	assertMapsByteEqual(t, "static_spa_custom_fallback", d.AppLabels([]proxy.Site{staticSPA}, proxy.UpstreamRef{}), wantStatic)

	// 5. Suspended PHP site emits zero labels.
	suspended := phpDefault
	suspended.PHP.Suspended = true
	if got := d.AppLabels([]proxy.Site{suspended}, proxy.UpstreamRef{}); len(got) != 0 {
		t.Fatalf("suspended PHP site must emit no labels, got %v", got)
	}
}

func TestParity_RoutesExternalProxyAndRedirect(t *testing.T) {
	d := New()
	shieldID := int64(5)

	cases := []struct {
		name  string
		site  proxy.Site
		up    proxy.UpstreamRef
		route db.ProxyRoute
	}{
		{
			name: "external_proxy_http_untrusted",
			site: proxy.Site{
				ID: "route:1", Hosts: []string{"ext1.example.com"}, TLS: proxy.TLSAuto,
				Kind:     proxy.SiteKindExternalProxy,
				Upstream: &proxy.UpstreamRef{External: "http://10.0.0.5:8080"},
			},
			route: db.ProxyRoute{Kind: "proxy", Domain: "ext1.example.com", Target: "http://10.0.0.5:8080", EnableHTTPS: true},
		},
		{
			name: "external_proxy_https_skip_verify_trusted",
			site: proxy.Site{
				ID: "route:2", Hosts: []string{"ext2.example.com", "www.ext2.example.com"},
				TLS: proxy.TLSAuto, TrustedProxy: true, Kind: proxy.SiteKindExternalProxy,
				Upstream: &proxy.UpstreamRef{External: "https://10.0.0.6:8443", SkipVerify: true},
			},
			route: db.ProxyRoute{
				Kind: "proxy", Domain: "ext2.example.com", Target: "https://10.0.0.6:8443",
				EnableHTTPS: true, EnableWWW: true, SkipTLSVerify: true, TrustedProxy: true,
			},
		},
		{
			name: "external_proxy_shielded_http_upstream_fallback",
			site: proxy.Site{
				ID: "route:3", Hosts: []string{"ext3.example.com"},
				TLS: proxy.TLSInternal, BehindShield: true, Kind: proxy.SiteKindExternalProxy,
			},
			up: proxy.UpstreamRef{External: "http://192.168.1.50:9000"},
			route: db.ProxyRoute{
				Kind: "proxy", Domain: "ext3.example.com", Target: "http://192.168.1.50:9000",
				EnableHTTPS: true, ShieldNodeID: &shieldID,
			},
		},
		{
			name: "redirect_keep_path_https",
			site: proxy.Site{
				ID: "route:4", Hosts: []string{"old.example.com", "www.old.example.com"},
				TLS: proxy.TLSAuto, Kind: proxy.SiteKindRedirect,
				Redirect: &proxy.RedirectSpec{Code: 302, Target: "https://new.example.com", KeepPath: true},
			},
			route: db.ProxyRoute{
				Kind: "redirect", Domain: "old.example.com", Target: "https://new.example.com",
				RedirectCode: 302, KeepPath: true, EnableHTTPS: true, EnableWWW: true,
			},
		},
		{
			name: "redirect_http_only_no_keep_path",
			site: proxy.Site{
				ID: "route:5", Hosts: []string{"legacy.example.com"},
				TLS: proxy.TLSNone, Kind: proxy.SiteKindRedirect,
				Redirect: &proxy.RedirectSpec{Code: 301, Target: "https://canonical.example.com/home", KeepPath: false},
			},
			route: db.ProxyRoute{
				Kind: "redirect", Domain: "legacy.example.com", Target: "https://canonical.example.com/home",
				RedirectCode: 301, KeepPath: false, EnableHTTPS: false,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := d.AppLabels([]proxy.Site{tc.site}, tc.up)
			want := icaddy.RouteLabels(tc.route)
			assertMapsByteEqual(t, tc.name, got, want)
		})
	}
}

func TestParity_StackServiceAndRootCompose(t *testing.T) {
	d := New()

	baseRootCompose := `networks:
    nextdeploy:
        name: NextDeploy
services:
    caddy:
        container_name: caddy
        environment:
            - CADDY_ADMIN=0.0.0.0:2019
            - CADDY_DOCKER_EVENT_THROTTLE_INTERVAL=50ms
            - CADDY_DOCKER_POLLING_INTERVAL=5s
            - CADDY_INGRESS_NETWORKS=NextDeploy
        image: lucaslorentz/caddy-docker-proxy:ci-alpine
        labels:
            caddy.auto_https: disable_redirects
            caddy.servers.timeouts.idle: 99m
            caddy.grace_period: 1h
        networks:
            - nextdeploy
        ports:
            - 80:80
            - 443:443/tcp
            - 443:443/udp
        restart: unless-stopped
        volumes:
            - /var/run/docker.sock:/var/run/docker.sock
            - caddy_data:/data
            - /data/workspaces:/data/workspaces:ro
    panel:
        image: masudranaxpert/nextdeploy:latest
volumes:
    caddy_data: {}
`

	opts := proxy.StackOptions{
		Engine:           proxy.EngineCaddy,
		Image:            icaddy.CaddyImage,
		ContainerName:    "caddy",
		ACMEEmail:        "ops@example.com",
		PanelDomain:      "panel.example.com",
		PanelEnableHTTPS: true,
		PanelEnableWWW:   true,
		Global: proxy.GlobalOptions{
			Profile:          "custom",
			ReadHeader:       "15s",
			ReadBodyIdle:     "30s",
			Idle:             "2m",
			GracePeriod:      "20s",
			MaxHeaderSize:    "16KB",
			RealIP:           "cloudflare",
			TrustedStrict:    true,
			ShieldProxies:    []string{"107.189.6.222"},
			DirectCloudflare: true,
		},
		SharedMounts: []proxy.SharedMount{
			{VolumeName: "shared_assets", Target: "/mnt/shared/assets", ReadOnly: true},
			{VolumeName: "bad_rel", Target: "relative/path", ReadOnly: true},
		},
		Gateways: []proxy.GatewayPort{
			{Port: 4013, ContainerPort: 3000, BindIP: "0.0.0.0", Protocol: "tcp", AppID: "a1"},
			{Port: 8090, ContainerPort: 8080, BindIP: "127.0.0.1", Protocol: "tcp", AppID: "a2"},
			{Port: 5000, ContainerPort: 5000, BindIP: "0.0.0.0", Protocol: "udp", AppID: "a3"},
			{Port: 5432, ContainerPort: 5432, BindIP: "0.0.0.0", Protocol: "tcp", Direct: true, AppID: "db"},
		},
	}

	// 1. MergeRootCompose must match icaddy.GenerateRootStackCompose byte-for-byte.
	gotYAML, err := MergeRootCompose([]byte(baseRootCompose), opts)
	if err != nil {
		t.Fatalf("MergeRootCompose error: %v", err)
	}
	wantYAML, err := icaddy.GenerateRootStackCompose(
		[]byte(baseRootCompose),
		opts.PanelDomain,
		opts.PanelEnableHTTPS,
		opts.PanelEnableWWW,
		opts.ACMEEmail,
		opts.Image,
		[]icaddy.NDSharedMount{
			{VolumeName: "shared_assets", Target: "/mnt/shared/assets"},
			{VolumeName: "bad_rel", Target: "relative/path"},
		},
		toCaddyGlobalOptions(opts.Global),
		toDBAllocatedPorts(opts.Gateways)...,
	)
	if err != nil {
		t.Fatalf("icaddy.GenerateRootStackCompose error: %v", err)
	}
	if string(gotYAML) != string(wantYAML) {
		t.Fatalf("MergeRootCompose YAML mismatch:\n--- got ---\n%s--- want ---\n%s", gotYAML, wantYAML)
	}

	// 2. StackService(opts) must match services["caddy"] and volumes from icaddy.GenerateRootStackCompose.
	svc, vols, err := d.StackService(opts)
	if err != nil {
		t.Fatalf("StackService error: %v", err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(wantYAML, &doc); err != nil {
		t.Fatalf("unmarshal wantYAML: %v", err)
	}
	wantSvc := doc["services"].(map[string]any)["caddy"].(map[string]any)
	wantVols := doc["volumes"].(map[string]any)

	if !reflect.DeepEqual(svc, wantSvc) {
		t.Fatalf("StackService service map mismatch:\ngot:  %#v\nwant: %#v", svc, wantSvc)
	}
	if !reflect.DeepEqual(vols, wantVols) {
		t.Fatalf("StackService volumes map mismatch:\ngot:  %#v\nwant: %#v", vols, wantVols)
	}

	// 3. Panel Site IR passed to AppLabels must match the panel labels emitted by GenerateRootStackCompose.
	wantPanelLabelsRaw := doc["services"].(map[string]any)["panel"].(map[string]any)["labels"].(map[string]any)
	wantPanelLabels := make(map[string]string, len(wantPanelLabelsRaw))
	for k, v := range wantPanelLabelsRaw {
		wantPanelLabels[k] = v.(string)
	}
	panelSite := proxy.Site{
		ID:           "panel",
		Hosts:        []string{"panel.example.com", "www.panel.example.com"},
		TLS:          proxy.TLSAuto,
		TrustedProxy: true,
		Kind:         proxy.SiteKindAppProxy,
		Upstream: &proxy.UpstreamRef{
			Port: 8080,
			RequestHeaders: []proxy.HeaderOp{
				{Action: proxy.HeaderSet, Name: "X-Real-IP", Value: "{client_ip}"},
				{Action: proxy.HeaderSet, Name: "X-Forwarded-For", Value: "{client_ip}"},
			},
		},
	}
	assertMapsByteEqual(t, "panel_site_labels", d.AppLabels([]proxy.Site{panelSite}, *panelSite.Upstream), wantPanelLabels)

	// 4. Switching back to default GlobalOptions with empty ACMEEmail strips stale labels.
	resetOpts := proxy.StackOptions{
		Engine:           proxy.EngineCaddy,
		PanelDomain:      "panel.example.com",
		PanelEnableHTTPS: true,
		Global:           proxy.GlobalOptions{RealIP: "none", DirectCloudflare: true},
	}
	resetYAML, err := MergeRootCompose(gotYAML, resetOpts)
	if err != nil {
		t.Fatalf("MergeRootCompose reset error: %v", err)
	}
	wantResetYAML, err := icaddy.GenerateRootStackCompose(
		wantYAML,
		resetOpts.PanelDomain,
		resetOpts.PanelEnableHTTPS,
		resetOpts.PanelEnableWWW,
		resetOpts.ACMEEmail,
		icaddy.CaddyImage,
		nil,
		toCaddyGlobalOptions(resetOpts.Global),
	)
	if err != nil {
		t.Fatalf("icaddy reset error: %v", err)
	}
	if string(resetYAML) != string(wantResetYAML) {
		t.Fatalf("MergeRootCompose reset YAML mismatch:\n--- got ---\n%s--- want ---\n%s", resetYAML, wantResetYAML)
	}
}

func TestParity_EdgeCasesMultiRoutePublicInternalAndSlotProxyPath(t *testing.T) {
	d := New()

	// 1. Public domain forced to TLSInternal (BehindShield=false).
	phpInternal := proxy.Site{
		ID: "php:pub_int", Hosts: []string{"blog.example.com"}, TLS: proxy.TLSInternal,
		Kind: proxy.SiteKindPHP,
		PHP:  &proxy.PHPSpec{DocRoot: "/srv/blog", Socket: "/run/blog.sock"},
	}
	gotPHP := d.AppLabels([]proxy.Site{phpInternal}, proxy.UpstreamRef{})
	if gotPHP["caddy.tls"] != "internal" {
		t.Fatalf("expected caddy.tls=internal on explicit TLSInternal PHP site, got %v", gotPHP)
	}

	// 2. Multi-route batch (ExternalProxy + Redirect).
	routeBatch := []proxy.Site{
		{
			ID: "route:1", RouteID: 1, Hosts: []string{"ext.example.com"}, TLS: proxy.TLSAuto,
			Kind: proxy.SiteKindExternalProxy, Upstream: &proxy.UpstreamRef{External: "http://10.0.0.5:8080"},
		},
		{
			ID: "route:2", RouteID: 2, Hosts: []string{"go.example.com"}, TLS: proxy.TLSAuto,
			Kind: proxy.SiteKindRedirect, Redirect: &proxy.RedirectSpec{Code: 302, Target: "https://dest.example.com", KeepPath: true},
		},
	}
	gotBatch := d.AppLabels(routeBatch, proxy.UpstreamRef{})
	want0 := icaddy.RouteLabels(db.ProxyRoute{Kind: "proxy", Domain: "ext.example.com", Target: "http://10.0.0.5:8080", EnableHTTPS: true})
	want1 := icaddy.RouteLabels(db.ProxyRoute{Kind: "redirect", Domain: "go.example.com", Target: "https://dest.example.com", RedirectCode: 302, KeepPath: true, EnableHTTPS: true})
	wantBatch := map[string]string{}
	remapAndMerge(wantBatch, want0, "caddy", "caddy_0")
	remapAndMerge(wantBatch, want1, "caddy", "caddy_1")
	assertMapsByteEqual(t, "multi_route_batch", gotBatch, wantBatch)

	// 3. Slot site with Proxy path route emits 30s on path and 5s on default catch-all.
	slotProxyPath := proxy.Site{
		ID: "app:a1:dom:1", Slot: "green", Hosts: []string{"api.example.com"}, TLS: proxy.TLSAuto,
		Kind: proxy.SiteKindAppProxy,
		Paths: []proxy.PathRoute{{
			Prefix: "/ws/*", Priority: 1,
			Handler: proxy.Handler{Kind: proxy.HandlerProxy, Upstream: &proxy.UpstreamRef{Port: 4000}},
		}},
		Upstream: &proxy.UpstreamRef{Port: 3000, LBTryDuration: "5s", FailDuration: "5s"},
	}
	gotSlotPath := d.AppLabels([]proxy.Site{slotProxyPath}, proxy.UpstreamRef{Port: 3000})
	if gotSlotPath["caddy.1_reverse_proxy.lb_try_duration"] != "30s" ||
		gotSlotPath["caddy.2_reverse_proxy.lb_try_duration"] != "5s" ||
		gotSlotPath["caddy.2_reverse_proxy.fail_duration"] != "5s" {
		t.Fatalf("unexpected slot proxy path lb durations: %v", gotSlotPath)
	}

	// 4. Empty / blank hosts produce empty map cleanly.
	if gotEmpty := d.AppLabels([]proxy.Site{{ID: "blank", Kind: proxy.SiteKindAppProxy}}, proxy.UpstreamRef{}); len(gotEmpty) != 0 {
		t.Fatalf("expected empty labels for blank hosts, got %v", gotEmpty)
	}

	// 5. Multi-domain PHP with mixed HTTPS still groups cleanly and matches PHPSiteLabelsWithRouting.
	mixedPHP := []proxy.Site{
		{
			ID: "php:m:1", DomainID: 1, Hosts: []string{"secure.example.com"}, TLS: proxy.TLSAuto,
			Kind: proxy.SiteKindPHP, PHP: &proxy.PHPSpec{DocRoot: "/srv/m", Socket: "/run/m.sock", HTTPS: true},
		},
		{
			ID: "php:m:2", DomainID: 2, Hosts: []string{"plain.example.com"}, TLS: proxy.TLSNone,
			Kind: proxy.SiteKindPHP, PHP: &proxy.PHPSpec{DocRoot: "/srv/m", Socket: "/run/m.sock", HTTPS: false},
		},
	}
	wantMixedPHP := icaddy.PHPSiteLabelsWithRouting(
		[]db.AppDomain{
			{ID: 1, Domain: "secure.example.com", EnableHTTPS: true},
			{ID: 2, Domain: "plain.example.com", EnableHTTPS: false},
		},
		"/srv/m", "/run/m.sock", icaddy.SiteSecurity{}, icaddy.SiteRouting{},
	)
	assertMapsByteEqual(t, "php_multi_domain_mixed_https", d.AppLabels(mixedPHP, proxy.UpstreamRef{}), wantMixedPHP)
}

func TestParity_CustomPathRouteIRFieldsAndLongestPrefixOrdering(t *testing.T) {
	d := New()

	// Intentionally supply /api/* before /api/v2/* at equal priority to verify longest-prefix-first sorting.
	site := proxy.Site{
		ID:           "app:a1:dom:1",
		Slot:         "blue",
		Hosts:        []string{"edge.example.com"},
		TLS:          proxy.TLSAuto,
		TrustedProxy: true,
		Kind:         proxy.SiteKindAppProxy,
		Paths: []proxy.PathRoute{
			{
				Prefix: "/api/*", StripPrefix: true, Priority: 1,
				Handler: proxy.Handler{
					Kind: proxy.HandlerProxy,
					Upstream: &proxy.UpstreamRef{
						Port:          4000,
						Scheme:        "https",
						SkipVerify:    true,
						LBTryDuration: "18s",
						FailDuration:  "12s",
						RequestHeaders: []proxy.HeaderOp{
							{Action: proxy.HeaderSet, Name: "X-Forwarded-Proto", Value: "https"},
						},
					},
				},
			},
			{
				Prefix: "/api/v2/*", StripPrefix: false, Priority: 1,
				Handler: proxy.Handler{
					Kind:   proxy.HandlerStaticDir,
					Static: &proxy.StaticDirSpec{Root: "/data/workspaces/a1/v2", Precompressed: []string{"gzip", "br"}},
				},
			},
		},
		Upstream: &proxy.UpstreamRef{
			Port:          3000,
			LBTryDuration: "15s",
			FailDuration:  "9s",
		},
	}

	got := d.AppLabels([]proxy.Site{site}, *site.Upstream)
	want := map[string]string{
		"caddy":                               "edge.example.com",
		"caddy.header.X-Frame-Options":        "SAMEORIGIN",
		"caddy.header.X-Content-Type-Options": "nosniff",
		"caddy.header.Referrer-Policy":        "strict-origin-when-cross-origin",
		"caddy.header.X-XSS-Protection":       "1; mode=block",
		// Longest prefix (/api/v2/*) ordered first with handle (StripPrefix=false) + precompressed:
		"caddy.1_handle":                             "/api/v2/*",
		"caddy.1_handle.0_root":                      "* /data/workspaces/a1/v2",
		"caddy.1_handle.1_file_server":               `{{""}}`,
		"caddy.1_handle.1_file_server.precompressed": "gzip br",
		"caddy.1_handle.1_file_server.hide":          defaultStaticHide,
		// Shorter prefix (/api/*) ordered second with handle_path + reverse_proxy (StripPrefix=true):
		"caddy.2_handle_path":                                                  "/api/*",
		"caddy.2_handle_path.reverse_proxy":                                    "{{upstreams 4000}}",
		"caddy.2_handle_path.reverse_proxy.stream_close_delay":                 "10m",
		"caddy.2_handle_path.reverse_proxy.lb_try_duration":                    "18s",
		"caddy.2_handle_path.reverse_proxy.fail_duration":                      "12s",
		"caddy.2_handle_path.reverse_proxy.transport":                          "http",
		"caddy.2_handle_path.reverse_proxy.transport.tls":                      `{{""}}`,
		"caddy.2_handle_path.reverse_proxy.transport.tls_insecure_skip_verify": `{{""}}`,
		"caddy.2_handle_path.reverse_proxy.header_up_0":                        "X-Forwarded-Proto https",
		// Default catch-all honors site Upstream LBTryDuration / FailDuration:
		"caddy.3_reverse_proxy":                    "{{upstreams 3000}}",
		"caddy.3_reverse_proxy.stream_close_delay": "10m",
		"caddy.3_reverse_proxy.lb_try_duration":    "15s",
		"caddy.3_reverse_proxy.fail_duration":      "9s",
	}
	assertMapsByteEqual(t, "custom_path_ir_fields_and_longest_prefix", got, want)
}

func TestParity_EndToEndStateBuildToCaddyDriver(t *testing.T) {
	d := New()
	shieldID := int64(3)

	in := state.BuildInput{
		PanelDomain:          "panel.example.com",
		PanelEnableHTTPS:     true,
		PanelEnableWWW:       false,
		ACMEEmail:            "ops@example.com",
		SharedMountPrefix:    "/mnt/shared",
		SharedVolumeNamesRaw: `["media_vol"]`,
		Global: proxy.GlobalOptions{
			Profile:       "balanced",
			ReadHeader:    "15s",
			Idle:          "2m",
			GracePeriod:   "30s",
			RealIP:        "cloudflare",
			TrustedStrict: true,
		},
		ShieldNodes: []db.ShieldNode{{ID: shieldID, Name: "edge-1", IP: "198.51.100.10", Status: "connected"}},
		Apps: []db.App{
			{ID: "app1", Name: "My App", ActiveSlot: "blue", PinnedSlot: "blue", StandbySlot: "green"},
			{ID: "php1", Name: "WP Blog", OwnerID: 1},
		},
		AppDomains: []db.AppDomain{
			{
				ID: 1, AppID: "app1", Domain: "app.example.com", Service: "web", Port: 3000,
				EnableHTTPS: true, EnableWWW: true, ServeStatic: true, StaticURLPrefix: "/assets", StaticPath: "/data/workspaces/app1/dist",
			},
			{ID: 2, AppID: "php1", Domain: "blog.example.com", Service: "php", Port: 80, EnableHTTPS: true},
		},
		AppTargets: map[string]state.AppTarget{
			"app1": {
				WorkspaceDir: "/data/workspaces/app1",
				Preview:      &state.AppPreviewConfig{Provider: "custom", Base: "app.example.com", Service: "web", Port: 3000},
				GatewayBindings: []proxy.HostPortBinding{
					{HostPort: 4013, ContainerPort: 3000, BindIP: "0.0.0.0", Protocol: "tcp", Service: "web", Direct: false},
				},
			},
		},
		PHPSites: []state.PHPSiteInput{{
			Site: db.PHPSite{AppID: "php1", OwnerID: 1, PHPVersion: "8.3", SiteType: "wordpress"},
			Security: proxy.SecuritySpec{
				BlockedIPs: []string{"1.2.3.4"}, BlockXMLRPC: true, HSTS: true,
			},
			Routing: &proxy.SiteRouting{
				FallbackMode: proxy.FallbackStrict404,
				Rules: []proxy.RoutingRule{
					{ID: "r1", Kind: "block", MatchType: "exact", Pattern: "/secret", Status: 403, Enabled: true},
				},
			},
		}},
		ProxyRoutes: []db.ProxyRoute{
			{ID: 5, Kind: "proxy", Domain: "api.example.com", Target: "https://10.0.0.9:8443", EnableHTTPS: true, SkipTLSVerify: true, Enabled: true},
			{ID: 6, Kind: "redirect", Domain: "old.example.com", Target: "https://app.example.com", RedirectCode: 301, KeepPath: true, EnableHTTPS: true, Enabled: true},
		},
		AllocatedPorts: []db.AllocatedPort{
			{Port: 4013, BindIP: "0.0.0.0", AppID: "app1", Service: "web", Protocol: "tcp", Direct: false},
			{Port: 5432, BindIP: "0.0.0.0", AppID: "app1", Service: "db", Protocol: "tcp", Direct: true},
		},
	}

	st, err := state.Build(in)
	if err != nil {
		t.Fatalf("state.Build failed: %v", err)
	}

	var appSites, previewSites, phpSites []proxy.Site
	var routeExt, routeRedir proxy.Site
	for _, s := range st.Sites {
		switch {
		case s.AppID == "app1" && s.IsPreview:
			previewSites = append(previewSites, s)
		case s.AppID == "app1":
			appSites = append(appSites, s)
		case s.AppID == "php1":
			phpSites = append(phpSites, s)
		case s.ID == "route:5":
			routeExt = s
		case s.ID == "route:6":
			routeRedir = s
		}
	}

	// App + GatewayPort via AppLabelsWithGateways must match GenerateSlotLabels byte-for-byte.
	gotAppGW := d.AppLabelsWithGateways(appSites, st.Gateways, *appSites[0].Upstream)
	wantAppGW := icaddy.GenerateSlotLabels(
		[]db.AppDomain{in.AppDomains[0]},
		[]icaddy.HostPortBinding{{HostPort: 4013, ContainerPort: 3000, BindIP: "0.0.0.0", Protocol: "tcp", Service: "web", Direct: false}},
		nil, false,
	)
	assertMapsByteEqual(t, "e2e_app_slot_and_gateway", gotAppGW, wantAppGW)

	// Preview slot must match GenerateSlotLabels(previewOnly=true) byte-for-byte.
	pvHost := icaddy.SlotPreviewDomain("app.example.com", "green")
	assertMapsByteEqual(t, "e2e_preview_slot",
		d.AppLabels(previewSites, *previewSites[0].Upstream),
		icaddy.GenerateSlotLabels(nil, nil, &db.AppDomain{Domain: pvHost, Service: "web", Port: 3000, EnableHTTPS: true}, true),
	)

	// PHP site must match PHPSiteLabelsWithRouting byte-for-byte.
	assertMapsByteEqual(t, "e2e_php_site",
		d.AppLabels(phpSites, proxy.UpstreamRef{}),
		icaddy.PHPSiteLabelsWithRouting(
			[]db.AppDomain{{ID: 2, Domain: "blog.example.com", EnableHTTPS: true}},
			"/data/workspaces/php1/htdocs",
			"/data/workspaces/.nd-php/u1-v83/run/php1.sock",
			in.PHPSites[0].Security,
			*in.PHPSites[0].Routing,
		),
	)

	// Routes must match RouteLabels byte-for-byte.
	assertMapsByteEqual(t, "e2e_route_proxy", d.AppLabels([]proxy.Site{routeExt}, proxy.UpstreamRef{}), icaddy.RouteLabels(in.ProxyRoutes[0]))
	assertMapsByteEqual(t, "e2e_route_redir", d.AppLabels([]proxy.Site{routeRedir}, proxy.UpstreamRef{}), icaddy.RouteLabels(in.ProxyRoutes[1]))

	// Panel + StackService must match GenerateRootStackCompose byte-for-byte.
	stackOpts := proxy.StackOptions{
		Engine:           proxy.EngineCaddy,
		PanelDomain:      in.PanelDomain,
		PanelEnableHTTPS: in.PanelEnableHTTPS,
		PanelEnableWWW:   in.PanelEnableWWW,
		ACMEEmail:        st.ACMEEmail,
		Global:           st.Global,
		SharedMounts:     st.SharedMounts,
		Gateways:         st.Gateways,
	}
	svc, vols, err := d.StackService(stackOpts)
	if err != nil {
		t.Fatalf("StackService failed: %v", err)
	}
	if svc["container_name"] != "caddy" || vols["caddy_data"] == nil {
		t.Fatalf("unexpected StackService output: svc=%v vols=%v", svc, vols)
	}
	panelLabels := d.AppLabels([]proxy.Site{*st.Panel}, *st.Panel.Upstream)
	wantPanel := icaddy.GenerateLabels(db.AppDomain{Domain: "panel.example.com", Port: 8080, EnableHTTPS: true, TrustedProxy: true})
	wantPanel["caddy.reverse_proxy.header_up_0"] = "X-Real-IP {client_ip}"
	wantPanel["caddy.reverse_proxy.header_up_1"] = "X-Forwarded-For {client_ip}"
	assertMapsByteEqual(t, "e2e_panel_labels", panelLabels, wantPanel)
}

func TestDriver_ContractRegistryRenderValidateApplyAndRuntime(t *testing.T) {
	// Registry auto-registration via init() and custom RegisterInto.
	globalDrv, err := proxy.Get("caddy")
	if err != nil {
		t.Fatalf("expected caddy auto-registered in global registry: %v", err)
	}
	if globalDrv.Name() != "caddy" || globalDrv.ContainerName() != icaddy.CaddyContainerName {
		t.Fatalf("unexpected identity: name=%q container=%q", globalDrv.Name(), globalDrv.ContainerName())
	}

	customReg := proxy.NewRegistry()
	if err := RegisterInto(customReg); err != nil {
		t.Fatalf("RegisterInto(customReg): %v", err)
	}
	if got, err := customReg.Get("CADDY"); err != nil || got.Name() != "caddy" {
		t.Fatalf("custom registry lookup failed: got=%v err=%v", got, err)
	}

	// Capabilities check (all 18 flags).
	caps := globalDrv.Capabilities()
	wantCaps := proxy.Capabilities{
		AutoTLS: true, InternalTLS: true, OnDemandTLS: true,
		DockerUpstreamDiscovery: true, PHPFastCGI: true, BasicAuthBcrypt: true,
		RegexRouting: true, RequireTargetFile: true, H2IdleTimeout: true,
		WriteTimeout: true, ReadHeaderTimeout: true, ReadBodyTimeout: true,
		GracePeriod: true, MaxHeaderSizeHTTP1: true, TrustedStrict: true,
		LongStreamsOnReload: true, AdminAPI: true, Experimental: false,
	}
	if caps != wantCaps {
		t.Fatalf("Capabilities mismatch:\ngot:  %+v\nwant: %+v", caps, wantCaps)
	}

	// Render, Validate, Apply.
	files, err := globalDrv.Render(&proxy.State{})
	if err != nil || files == nil || len(files) != 0 {
		t.Fatalf("Render expected empty non-nil Files, got files=%v err=%v", files, err)
	}
	if err := globalDrv.Validate(context.Background(), ""); err != nil {
		t.Fatalf("Validate expected nil, got %v", err)
	}
	res, err := globalDrv.Apply(context.Background(), files)
	if err != nil || res.Driver != "caddy" || res.Changed || res.Reloaded {
		t.Fatalf("Apply unexpected: res=%+v err=%v", res, err)
	}

	// Canceled context must fail cleanly on Validate / Apply / Status / Logs / Restart.
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := globalDrv.Validate(canceledCtx, ""); !errors.Is(err, context.Canceled) {
		t.Fatalf("Validate canceled ctx error = %v", err)
	}
	if _, err := globalDrv.Apply(canceledCtx, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("Apply canceled ctx error = %v", err)
	}
	if _, err := globalDrv.Status(canceledCtx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Status canceled ctx error = %v", err)
	}
	if _, err := globalDrv.Logs(canceledCtx, 10); !errors.Is(err, context.Canceled) {
		t.Fatalf("Logs canceled ctx error = %v", err)
	}
	if err := globalDrv.Restart(canceledCtx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Restart canceled ctx error = %v", err)
	}

	// Status with real httptest Caddy Admin API (no Docker daemon required).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/config/" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"apps":{"http":{"servers":{"srv0":{"listen":[":443"]}}}}}`))
	}))
	defer srv.Close()

	drvUp := New(WithAdminURL(srv.URL), WithRuntimeHooks(RuntimeHooks{
		Inspect: func(context.Context, string) (ContainerInspectResult, error) {
			return ContainerInspectResult{}, errors.New("docker mocked absent")
		},
		ExecVersion: func(context.Context, string) (string, error) {
			return "v2.8.4 h1:abc\n", nil
		},
	}))
	st, err := drvUp.Status(context.Background())
	if err != nil {
		t.Fatalf("Status error: %v", err)
	}
	if !st.Running || st.Driver != "caddy" || st.Version != "v2.8.4 h1:abc" || !strings.Contains(st.RawConfig, `"srv0"`) {
		t.Fatalf("unexpected Status: %+v", st)
	}

	// Admin API returning 500 -> Running false, LastError set, nil error.
	errSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer errSrv.Close()
	drvDown := New(WithAdminURL(errSrv.URL), WithRuntimeHooks(RuntimeHooks{
		Inspect: func(context.Context, string) (ContainerInspectResult, error) {
			return ContainerInspectResult{Exists: true, Running: false, Status: "exited"}, nil
		},
	}))
	stDown, err := drvDown.Status(context.Background())
	if err != nil || stDown.Running || !strings.Contains(stDown.LastError, "500") {
		t.Fatalf("unexpected down Status: %+v err=%v", stDown, err)
	}

	// Admin API "off" relies on container inspect.
	drvOff := New(WithAdminURL("off"), WithRuntimeHooks(RuntimeHooks{
		Inspect: func(context.Context, string) (ContainerInspectResult, error) {
			return ContainerInspectResult{Exists: true, Running: true, Status: "running"}, nil
		},
	}))
	stOff, err := drvOff.Status(context.Background())
	if err != nil || !stOff.Running || !strings.Contains(stOff.Detail, "admin API disabled") {
		t.Fatalf("unexpected admin=off Status: %+v err=%v", stOff, err)
	}

	// Logs clamping + CRLF line splitting.
	var seenTail int
	drvLogs := New(WithRuntimeHooks(RuntimeHooks{
		Logs: func(_ context.Context, _ string, tail int) (string, error) {
			seenTail = tail
			return "line1\r\n\r\nline2\n", nil
		},
	}))
	lines, err := drvLogs.Logs(context.Background(), 0)
	if err != nil || seenTail != 300 || !reflect.DeepEqual(lines, []string{"line1", "line2"}) {
		t.Fatalf("Logs default clamp mismatch: tail=%d lines=%v err=%v", seenTail, lines, err)
	}
	_, _ = drvLogs.Logs(context.Background(), 9999)
	if seenTail != 2000 {
		t.Fatalf("Logs max clamp mismatch: tail=%d, want 2000", seenTail)
	}

	// Restart calls Restart then EnsureNetwork; skips EnsureNetwork on failure.
	var restarted, netEnsured bool
	drvRestart := New(WithRuntimeHooks(RuntimeHooks{
		Restart: func(context.Context, string) error {
			restarted = true
			return nil
		},
		EnsureNetwork: func(context.Context) error {
			netEnsured = true
			return nil
		},
	}))
	if err := drvRestart.Restart(context.Background()); err != nil || !restarted || !netEnsured {
		t.Fatalf("Restart failed: restarted=%v netEnsured=%v err=%v", restarted, netEnsured, err)
	}
	netEnsured = false
	drvRestartErr := New(WithRuntimeHooks(RuntimeHooks{
		Restart:       func(context.Context, string) error { return errors.New("boom") },
		EnsureNetwork: func(context.Context) error { netEnsured = true; return nil },
	}))
	if err := drvRestartErr.Restart(context.Background()); err == nil || netEnsured {
		t.Fatalf("Restart failure should skip EnsureNetwork: err=%v netEnsured=%v", err, netEnsured)
	}
}
