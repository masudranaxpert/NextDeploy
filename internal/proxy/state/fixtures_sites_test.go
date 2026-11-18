package state

import (
	"panel/internal/db"
	"panel/internal/proxy"
)

func siteGoldenCases() []goldenCase {
	return []goldenCase{
		{
			name: "06_php_wordpress_security_and_cache",
			file: "06_php_wordpress_security_cache.golden.json",
			input: BuildInput{
				AppDomains: []db.AppDomain{
					{ID: 40, AppID: "wp-blog", Domain: "blog.example.com", Service: "php", Port: 80, EnableHTTPS: true, EnableWWW: true},
				},
				PHPSites: []PHPSiteInput{
					{
						Site: db.PHPSite{
							AppID:      "wp-blog",
							OwnerID:    7,
							SiteType:   "wordpress",
							PHPVersion: "8.3",
						},
						Security: proxy.SecuritySpec{
							BlockedIPs:      []string{"1.2.3.4", "10.0.0.5/8"},
							AllowOnly:       []string{"203.0.113.0/24"},
							BasicAuthUser:   "admin",
							BasicAuthHash:   "$2y$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy",
							BlockXMLRPC:     true,
							BlockUploadPHP:  true,
							BlockScanners:   true,
							HSTS:            true,
							FrameProtection: true,
						},
					},
				},
			},
		},
		{
			name: "07_php_laravel_routing_and_suspended",
			file: "07_php_laravel_routing_suspended.golden.json",
			input: BuildInput{
				AppDomains: []db.AppDomain{
					{ID: 41, AppID: "lar-app", Domain: "laravel.example.com", Service: "php", Port: 80, EnableHTTPS: true},
					{ID: 42, AppID: "lar-susp", Domain: "suspended.example.com", Service: "php", Port: 80, EnableHTTPS: true},
				},
				PHPSites: []PHPSiteInput{
					{
						Site: db.PHPSite{
							AppID:      "lar-app",
							OwnerID:    3,
							SiteType:   "php",
							PHPVersion: "8.4",
							RootDir:    "public",
						},
						Routing: &proxy.SiteRouting{
							FallbackMode:  proxy.FallbackCustom,
							FallbackEntry: "/index.php",
							Rules: []proxy.RoutingRule{
								{ID: "r1", Kind: "rewrite", MatchType: "exact", Pattern: "/health", Target: "/health.php", Enabled: true},
								{ID: "r2", Kind: "rewrite", MatchType: "prefix", Pattern: "/v1/*", Target: "/api.php", Enabled: true},
								{ID: "r3", Kind: "rewrite", MatchType: "regex", Pattern: `^/([^.]+)$`, Target: "/$1.php?src=$0", Enabled: true, UnlessFile: true, RequireTargetFile: true, PreserveQuery: true},
								{ID: "r4", Kind: "redir", MatchType: "prefix", Pattern: "/legacy/*", Target: "https://docs.example.com/new?ref=old", Status: 301, Enabled: true, PreserveQuery: true},
								{ID: "r5", Kind: "block", MatchType: "exact", Pattern: "/xmlrpc.php", Status: 403, Enabled: true},
							},
						},
					},
					{
						Site: db.PHPSite{
							AppID:        "lar-susp",
							OwnerID:      9,
							SiteType:     "php",
							PHPVersion:   "8.2",
							SettingsJSON: `{"routing":{"fallback_mode":"strict_404"}}`,
						},
						OwnerSuspended: true,
					},
				},
			},
		},
		{
			name: "08_php_dummy_app_domain_exclusion",
			file: "08_php_dummy_domain_exclusion.golden.json",
			input: BuildInput{
				Apps: []db.App{
					{ID: "php-1", Name: "php-1", ActiveSlot: "blue"},
					{ID: "node-1", Name: "node-1", ActiveSlot: "blue"},
				},
				AppDomains: []db.AppDomain{
					{ID: 50, AppID: "php-1", Domain: "php1.example.com", Service: "php", Port: 80, EnableHTTPS: true},
					{ID: 51, AppID: "orphan-php", Domain: "stray-php.example.com", Service: "php", Port: 80, EnableHTTPS: true},
					{ID: 52, AppID: "node-1", Domain: "node1.example.com", Service: "web", Port: 3000, EnableHTTPS: true},
				},
				PHPSites: []PHPSiteInput{
					{
						Site: db.PHPSite{
							AppID:      "php-1",
							OwnerID:    1,
							SiteType:   "php",
							PHPVersion: "8.3",
						},
					},
				},
			},
		},
		{
			name: "09_static_site_spa_fallback",
			file: "09_static_site.golden.json",
			input: BuildInput{
				AppDomains: []db.AppDomain{
					{ID: 60, AppID: "docs-1", Domain: "docs.example.com", Service: "php", Port: 80, EnableHTTPS: true},
				},
				PHPSites: []PHPSiteInput{
					{
						Site: db.PHPSite{
							AppID:        "docs-1",
							OwnerID:      4,
							SiteType:     "static",
							RootDir:      "dist",
							SettingsJSON: `{"routing":{"fallback_mode":"custom","fallback_entry":"/index.html","rules":[{"id":"s1","kind":"redir","match_type":"exact","pattern":"/old","target":"/new","status":302,"enabled":true}]}}`,
						},
						Security: proxy.SecuritySpec{
							FrameProtection: true,
						},
					},
				},
			},
		},
		{
			name: "10_custom_external_proxy_and_redirect_routes",
			file: "10_proxy_routes_external_redirect.golden.json",
			input: BuildInput{
				ProxyRoutes: []db.ProxyRoute{
					{ID: 1, Kind: "proxy", Domain: "ext.example.com", Target: "https://10.0.0.50:8443", EnableHTTPS: true, EnableWWW: true, SkipTLSVerify: true, Enabled: true},
					{ID: 2, Kind: "proxy", Domain: "plain.example.com", Target: "http://10.0.0.51:8080", EnableHTTPS: true, SkipTLSVerify: true, TrustedProxy: true, Enabled: true},
					{ID: 3, Kind: "redirect", Domain: "go.example.com", Target: "https://target.example.com/docs", RedirectCode: 302, KeepPath: true, EnableHTTPS: true, Enabled: true},
					{ID: 4, Kind: "proxy", Domain: "disabled.example.com", Target: "https://10.0.0.99:9000", EnableHTTPS: true, Enabled: false},
				},
			},
		},
		{
			name: "11_gateway_http_vs_direct_l4_exclusion",
			file: "11_gateway_http_vs_direct_l4.golden.json",
			input: BuildInput{
				Apps: []db.App{
					{ID: "gw-app", Name: "gw-app", ActiveSlot: "blue"},
				},
				AppTargets: map[string]AppTarget{
					"gw-app": {
						GatewayBindings: []proxy.HostPortBinding{
							{HostPort: 4013, ContainerPort: 3000, BindIP: "0.0.0.0", Protocol: "tcp", Service: "web"},
							{HostPort: 8090, ContainerPort: 8080, BindIP: "127.0.0.1", Protocol: "tcp", Service: "api"},
						},
					},
				},
				AllocatedPorts: []db.AllocatedPort{
					{Port: 4013, BindIP: "0.0.0.0", AppID: "gw-app", Service: "web", Protocol: "tcp", Direct: false},
					{Port: 8090, BindIP: "127.0.0.1", AppID: "gw-app", Service: "api", Protocol: "tcp", Direct: false},
					{Port: 5432, BindIP: "0.0.0.0", AppID: "gw-app", Service: "db", Protocol: "tcp", Direct: true},
					{Port: 5000, BindIP: "0.0.0.0", AppID: "gw-app", Service: "game", Protocol: "udp", Direct: true},
				},
			},
		},
		{
			name: "12_panel_shared_mounts_deterministic",
			file: "12_panel_shared_mounts_deterministic.golden.json",
			input: BuildInput{
				PanelDomain:          "panel.example.com",
				PanelEnableHTTPS:     true,
				PanelEnableWWW:       true,
				ACMEEmail:            "admin@example.com",
				SharedMountPrefix:    "/mnt/shared/",
				SharedVolumeNamesRaw: `["media_vol","assets_vol","media_vol"]`,
				Global: proxy.GlobalOptions{
					Profile:       "balanced",
					ReadHeader:    "15s",
					Idle:          "2m",
					GracePeriod:   "30s",
					RealIP:        "cloudflare",
					TrustedStrict: true,
				},
			},
		},
	}
}
