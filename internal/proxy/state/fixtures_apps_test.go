package state

import (
	"panel/internal/db"
	"panel/internal/proxy"
)

type goldenCase struct {
	name  string
	file  string
	input BuildInput
}

func int64Ptr(v int64) *int64 { return &v }

func allGoldenCases() []goldenCase {
	out := append([]goldenCase(nil), appGoldenCases()...)
	out = append(out, siteGoldenCases()...)
	return out
}

func appGoldenCases() []goldenCase {
	return []goldenCase{
		{
			name: "01_simple_app_www_auto_tls",
			file: "01_app_simple_www_auto_tls.golden.json",
			input: BuildInput{
				ACMEEmail: "ops@example.com",
				Apps: []db.App{
					{ID: "web-a1b2", Name: "web", ActiveSlot: "blue"},
				},
				AppDomains: []db.AppDomain{
					{
						ID:          1,
						AppID:       "web-a1b2",
						Domain:      "example.com",
						Service:     "web",
						Port:        3000,
						IsPrimary:   true,
						EnableHTTPS: true,
						EnableWWW:   true,
					},
				},
			},
		},
		{
			name: "02_app_path_routes_static_and_nested_proxy",
			file: "02_app_path_routes.golden.json",
			input: BuildInput{
				Apps: []db.App{
					{ID: "store-1", Name: "store", ActiveSlot: "blue"},
				},
				AppDomains: []db.AppDomain{
					{
						ID:              2,
						AppID:           "store-1",
						Domain:          "shop.example.com",
						Service:         "web",
						Port:            3000,
						EnableHTTPS:     true,
						ServeStatic:     true,
						StaticPath:      "dist/assets",
						StaticURLPrefix: "/assets",
						ServeMedia:      true,
						MediaPath:       "/data/workspaces/store-1/uploads",
						MediaURLPrefix:  "/media",
					},
				},
				AppTargets: map[string]AppTarget{
					"store-1": {
						WorkspaceDir: "/data/workspaces/store-1",
						ServiceContainers: map[string]string{
							"web": "store-1-blue-web-1",
							"api": "store-1-blue-api-1",
						},
						PathRoutes: []proxy.PathRoute{
							{
								Prefix:   "/api/*",
								Priority: 3,
								Handler: proxy.Handler{
									Kind: proxy.HandlerProxy,
									Upstream: &proxy.UpstreamRef{
										Service: "api",
										Port:    4000,
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "03_internal_tls_localhost_sslip_and_shield",
			file: "03_internal_tls_variants.golden.json",
			input: BuildInput{
				ShieldNodes: []db.ShieldNode{
					{ID: 10, Name: "edge-eu", IP: "198.51.100.10", Status: "connected"},
				},
				Apps: []db.App{
					{ID: "tls-app", Name: "tls-app", ActiveSlot: "blue"},
				},
				AppDomains: []db.AppDomain{
					{ID: 11, AppID: "tls-app", Domain: "localhost", Service: "web", Port: 8080, EnableHTTPS: true},
					{ID: 12, AppID: "tls-app", Domain: "dev.localhost", Service: "web", Port: 8080, EnableHTTPS: true},
					{ID: 13, AppID: "tls-app", Domain: "app-192-168-1-10.sslip.io", Service: "web", Port: 8080, EnableHTTPS: true},
					{ID: 14, AppID: "tls-app", Domain: "pub-203-0-113-42.sslip.io", Service: "web", Port: 8080, EnableHTTPS: true},
					{ID: 15, AppID: "tls-app", Domain: "shielded.example.com", Service: "web", Port: 8080, EnableHTTPS: false, ShieldNodeID: int64Ptr(10)},
					{ID: 16, AppID: "tls-app", Domain: "plain.example.com", Service: "web", Port: 8080, EnableHTTPS: false},
				},
			},
		},
		{
			name: "04_trusted_proxy_cloudflare_vs_untrusted",
			file: "04_trusted_proxy_cloudflare.golden.json",
			input: BuildInput{
				Global: proxy.GlobalOptions{
					Profile:       "custom",
					ReadHeader:    "15s",
					Idle:          "2m",
					RealIP:        "cloudflare",
					TrustedStrict: true,
				},
				ShieldNodes: []db.ShieldNode{
					{ID: 1, Name: "edge-1", IP: "107.189.6.222", Status: "connected"},
				},
				Apps: []db.App{
					{ID: "tp-app", Name: "tp-app", ActiveSlot: "blue"},
				},
				AppDomains: []db.AppDomain{
					{ID: 21, AppID: "tp-app", Domain: "cf.example.com", Service: "web", Port: 3000, EnableHTTPS: true, TrustedProxy: true},
					{ID: 22, AppID: "tp-app", Domain: "direct.example.com", Service: "web", Port: 3000, EnableHTTPS: true, TrustedProxy: false},
					{ID: 23, AppID: "tp-app", Domain: "shield.example.com", Service: "web", Port: 3000, EnableHTTPS: true, ShieldNodeID: int64Ptr(1), TrustedProxy: true},
				},
			},
		},
		{
			name: "05_blue_green_active_slot_and_preview",
			file: "05_blue_green_slot_preview.golden.json",
			input: BuildInput{
				PanelID:         "550e8a00-e29b-41d4-a716-446655440000",
				DefaultPublicIP: "203.0.113.50",
				ShieldNodes: []db.ShieldNode{
					{ID: 2, Name: "edge-us", IP: "198.51.100.77", Status: "connected"},
				},
				Apps: []db.App{
					{ID: "shop-a", Name: "shop", ActiveSlot: "blue", PinnedSlot: "blue", StandbySlot: "green"},
					{ID: "api-2", Name: "api-2", ActiveSlot: "green", PinnedSlot: "green", StandbySlot: "blue"},
				},
				AppDomains: []db.AppDomain{
					{ID: 31, AppID: "shop-a", Domain: "shop.example.com", Service: "web", Port: 3000, EnableHTTPS: true},
					{ID: 32, AppID: "api-2", Domain: "api.example.com", Service: "api", Port: 4000, EnableHTTPS: true, ShieldNodeID: int64Ptr(2)},
				},
				AppTargets: map[string]AppTarget{
					"shop-a": {
						Preview: &AppPreviewConfig{Provider: "sslip.io", Service: "web", Port: 3000},
					},
					"api-2": {
						ShieldNodeID: int64Ptr(2),
						Preview:      &AppPreviewConfig{Provider: "sslip.io", Service: "api", Port: 4000},
					},
				},
			},
		},
	}
}
