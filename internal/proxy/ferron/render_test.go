package ferron

import (
	"regexp"
	"strings"
	"testing"

	"panel/internal/db"
	"panel/internal/proxy"
	"panel/internal/proxy/pki"
	"panel/internal/proxy/state"
)

func buildRichTestState(t *testing.T) *proxy.State {
	t.Helper()
	return &proxy.State{
		ACMEEmail: "ops@example.com",
		Global: proxy.GlobalOptions{
			Profile:          "custom",
			MaxHeaderSize:    "32k",
			RealIP:           "cloudflare",
			DirectCloudflare: true,
			TrustedProxies:   []string{"10.99.0.5"},
			ShieldProxies:    []string{"198.51.100.10"},
		},
		Panel: &proxy.Site{
			ID:           "panel",
			Hosts:        []string{"panel.example.com"},
			TLS:          proxy.TLSAuto,
			TrustedProxy: false,
			Headers:      []proxy.HeaderOp{{Action: proxy.HeaderSet, Name: "X-Frame-Options", Value: "DENY"}},
			Kind:         proxy.SiteKindAppProxy,
			Upstream:     &proxy.UpstreamRef{Name: "nd_panel_3000", Container: "nextdeploy-panel", Port: 3000},
		},
		Sites: []proxy.Site{
			{
				ID:           "app:app1:dom:1",
				AppID:        "app1",
				Hosts:        []string{"app.example.com", "www.app.example.com"},
				TLS:          proxy.TLSAuto,
				TrustedProxy: false,
				Kind:         proxy.SiteKindAppProxy,
				Paths: []proxy.PathRoute{
					{
						Prefix:      "/api",
						StripPrefix: true,
						Priority:    10,
						Handler: proxy.Handler{
							Kind:     proxy.HandlerProxy,
							Upstream: &proxy.UpstreamRef{Name: "nd_app1_api_4000", Container: "app1-api", Port: 4000},
						},
					},
					{
						Prefix:      "/api/v2",
						StripPrefix: false,
						Priority:    10,
						Handler: proxy.Handler{
							Kind:     proxy.HandlerProxy,
							Upstream: &proxy.UpstreamRef{Name: "nd_app1_v2_4001", Container: "app1-v2", Port: 4001},
						},
					},
					{
						Prefix:      "/assets",
						StripPrefix: true,
						Priority:    1,
						Handler: proxy.Handler{
							Kind: proxy.HandlerStaticDir,
							Static: &proxy.StaticDirSpec{
								Root:          "/srv/shared/assets",
								Hide:          []string{".env", "*.map"},
								Precompressed: []string{"br", "gzip"},
							},
						},
					},
				},
				Upstream: &proxy.UpstreamRef{Name: "nd_app1_web_3000", Container: "app1-web", Port: 3000},
			},
			{
				ID:           "app:app2:dom:2",
				AppID:        "app2",
				Hosts:        []string{"shield.local.test"},
				TLS:          proxy.TLSInternal,
				BehindShield: true,
				TrustedProxy: true,
				Kind:         proxy.SiteKindAppProxy,
				Slot:         "green",
				IsPreview:    true,
				Upstream: &proxy.UpstreamRef{
					Name:          "nd_app2_web_3000_green",
					Container:     "app2-green",
					Port:          3000,
					LBTryDuration: "5s",
					RequestHeaders: []proxy.HeaderOp{
						{Action: proxy.HeaderAdd, Name: "X-Slot", Value: "green"},
						{Action: proxy.HeaderDelete, Name: "X-Debug"},
					},
				},
			},
			{
				ID:       "app:app3:dom:3",
				Hosts:    []string{"plain.example.com", "www.plain.example.com"},
				TLS:      proxy.TLSNone,
				Kind:     proxy.SiteKindAppProxy,
				Upstream: &proxy.UpstreamRef{Name: "nd_app3_3000", Container: "app3-web", Port: 3000},
			},
			{
				ID:          "app:tenant:dom:4",
				Hosts:       []string{"tenant.example.com"},
				TLS:         proxy.TLSAuto,
				OnDemandTLS: true,
				Kind:        proxy.SiteKindAppProxy,
				Upstream:    &proxy.UpstreamRef{Name: "nd_tenant_3000", Container: "tenant-web", Port: 3000},
			},
			{
				ID:    "php:wp1",
				AppID: "wp1",
				Hosts: []string{"wp.example.com", "www.wp.example.com"},
				TLS:   proxy.TLSAuto,
				Kind:  proxy.SiteKindPHP,
				PHP: &proxy.PHPSpec{
					DocRoot:      "/data/workspaces/wp1/public",
					Socket:       "/data/workspaces/.nd-php/u1-v83/run/wp1.sock",
					FallbackMode: "default",
					Compression:  []string{"zstd", "gzip"},
					Cache: &proxy.PHPCacheSpec{
						MediaCacheControl: "public, max-age=2592000",
						CodeCacheControl:  "public, max-age=86400",
					},
					HidePatterns: []string{".git", "*.secret"},
					Routing: []proxy.RoutingRule{
						{ID: "blk1", Kind: "block", MatchType: "prefix", Pattern: "/private/", Status: 403, Enabled: true},
						{ID: "red1", Kind: "redir", MatchType: "regex", Pattern: `^/old/(.*)$`, Target: "/new/$1", Status: 301, Enabled: true, PreserveQuery: true},
						{ID: "rew1", Kind: "rewrite", MatchType: "exact", Pattern: "/healthz", Target: "/ping.php", Enabled: true, UnlessFile: false, PreserveQuery: true},
						{ID: "rtf1", Kind: "rewrite", MatchType: "regex", Pattern: `^/([^.]+)$`, Target: "/$1.php", Enabled: true, RequireTargetFile: true},
					},
				},
				Security: &proxy.SecuritySpec{
					BlockedIPs:      []string{"203.0.113.5", "198.51.100.0/24"},
					AllowOnly:       []string{"0.0.0.0/0"},
					BasicAuthUser:   "admin",
					BasicAuthHash:   "$2y$10$abcdefghijklmnopqrstuvABCDEFGHIJKLMNOPQRSTUVWXYZ012",
					BlockXMLRPC:     true,
					BlockUploadPHP:  true,
					BlockScanners:   true,
					HSTS:            true,
					FrameProtection: true,
				},
			},
			{
				ID:    "php:laravel1",
				AppID: "laravel1",
				Hosts: []string{"api.laravel.example.com"},
				TLS:   proxy.TLSAuto,
				Kind:  proxy.SiteKindPHP,
				PHP: &proxy.PHPSpec{
					DocRoot:      "/data/workspaces/laravel1/public",
					Socket:       "/data/workspaces/.nd-php/u1-v84/run/laravel1.sock",
					FallbackMode: "strict_404",
				},
			},
			{
				ID:    "php:suspended",
				AppID: "suspended",
				Hosts: []string{"paused.example.com"},
				TLS:   proxy.TLSAuto,
				Kind:  proxy.SiteKindPHP,
				PHP: &proxy.PHPSpec{
					DocRoot:   "/data/workspaces/suspended/public",
					Socket:    "/data/workspaces/.nd-php/u1-v83/run/suspended.sock",
					Suspended: true,
				},
			},
			{
				ID:    "static:spa1",
				AppID: "spa1",
				Hosts: []string{"spa.example.com"},
				TLS:   proxy.TLSAuto,
				Kind:  proxy.SiteKindStatic,
				PHP: &proxy.PHPSpec{
					DocRoot:       "/data/workspaces/spa1/dist",
					FallbackMode:  "custom",
					FallbackEntry: "/index.html",
				},
			},
			{
				ID:       "route:10",
				Hosts:    []string{"go.example.com"},
				TLS:      proxy.TLSAuto,
				Kind:     proxy.SiteKindRedirect,
				Redirect: &proxy.RedirectSpec{Code: 308, Target: "https://dest.example.com", KeepPath: true},
			},
			{
				ID:       "route:11",
				Hosts:    []string{"ext.example.com"},
				TLS:      proxy.TLSAuto,
				Kind:     proxy.SiteKindExternalProxy,
				Upstream: &proxy.UpstreamRef{Name: "nd_route_11", External: "https://origin.partner.internal:8443", SkipVerify: true},
			},
		},
		Gateways: []proxy.GatewayPort{
			{Port: 8080, ContainerPort: 8080, Protocol: "tcp", Upstream: &proxy.UpstreamRef{Container: "app1-gw", Port: 8080}},
			{Port: 5353, ContainerPort: 5353, Protocol: "udp", Upstream: &proxy.UpstreamRef{Container: "dns", Port: 5353}},
			{Port: 5432, ContainerPort: 5432, Protocol: "tcp", Direct: true, Upstream: &proxy.UpstreamRef{Container: "pg", Port: 5432}},
		},
	}
}

func assertUniversalFerronInvariants(t *testing.T, files proxy.Files) {
	t.Helper()
	main, ok := files["ferron.conf"]
	if !ok {
		t.Fatalf("missing ferron.conf in rendered files")
	}
	for _, want := range []string{
		"strict_hosts",
		`listen "127.0.0.1:8081"`,
		`auth_token "{{env.ND_FERRON_ADMIN_TOKEN}}"`,
		"protocols h1 h2",
		`timeout "3660s"`,
		`include "sites/*.conf"`,
		"match ND_PHP_PATHINFO",
		"match ND_HIDDEN_FILES",
	} {
		if !strings.Contains(main, want) {
			t.Errorf("ferron.conf missing required fragment %q:\n%s", want, main)
		}
	}

	upstreamRe := regexp.MustCompile(`(?m)^\s*upstream\s+"([^"]+)"`)
	for name, content := range files {
		if strings.Contains(content, "watch=1") {
			t.Errorf("%s contains forbidden watch=1 directive", name)
		}
		if strings.Contains(content, "location /") {
			t.Errorf("%s uses dangerous location directive instead of match/if", name)
		}
		// Every hostname upstream must immediately open a block with logical_dns true.
		lines := strings.Split(content, "\n")
		for i, line := range lines {
			m := upstreamRe.FindStringSubmatch(line)
			if len(m) != 2 {
				continue
			}
			if isHostnameUpstream(m[1]) {
				if !strings.Contains(line, "{") || i+1 >= len(lines) || !strings.Contains(lines[i+1], "logical_dns true") {
					t.Errorf("%s line %d: hostname upstream %q missing immediate logical_dns true block:\n%s", name, i+1, m[1], line)
				}
			}
		}
	}
}

func TestRender_AllScenariosAndInvariants(t *testing.T) {
	d := New(
		WithOnDemandAskURL("http://127.0.0.1:3000/api/internal/on-demand-ask"),
		WithImportedCerts(map[string]ManualCert{
			"imported.example.com": {CertPath: "/etc/nd-pki/imported.crt", KeyPath: "/etc/nd-pki/imported.key"},
		}),
	)
	st := buildRichTestState(t)
	files, err := d.Render(st)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	assertUniversalFerronInvariants(t, files)

	main := files["ferron.conf"]
	for _, wantTrusted := range []string{`trusted_proxy "173.245.48.0/20"`, `trusted_proxy "10.99.0.5"`, `trusted_proxy "198.51.100.10"`, "h2_max_header_list_size 32768"} {
		if !strings.Contains(main, wantTrusted) {
			t.Errorf("ferron.conf missing %q", wantTrusted)
		}
	}

	// App with paths: must isolate static/proxy paths via match/if + no host-level proxy, longest prefix before /api.
	app1 := findFileContaining(t, files, "app.example.com:443, www.app.example.com:443 {")
	idxAssets := strings.Index(app1, "/assets")
	idxV2 := strings.Index(app1, "/api/v2")
	idxAPI := strings.Index(app1, `"^/api(?:/|$)"`)
	if idxAssets < 0 || idxV2 < 0 || idxAPI < 0 || !(idxAssets < idxV2 && idxV2 < idxAPI) {
		t.Fatalf("unexpected PathRoute matcher ordering in app1:\n%s", app1)
	}
	if !strings.Contains(app1, `request_header "X-Real-IP" "{{remote.ip}}"`) ||
		!strings.Contains(app1, `request_header "-CF-Connecting-IP"`) {
		t.Fatalf("expected untrusted header stripping in app1:\n%s", app1)
	}
	if !strings.Contains(app1, "precompressed true") || !strings.Contains(app1, `disable_symlinks false`) {
		t.Fatalf("expected precompressed + disable_symlinks false on static path route:\n%s", app1)
	}

	// Internal TLS behind shield -> PKI stem cert + custom request_header ops + retry_connection.
	shield := findFileContaining(t, files, "shield.local.test:443 {")
	stem, _ := pki.SafeSiteStem("app:app2:dom:2")
	if !strings.Contains(shield, containerPKISitesDir+"/"+stem+".crt") || !strings.Contains(shield, `request_header "+X-Slot" "green"`) ||
		!strings.Contains(shield, `request_header "-X-Debug"`) || !strings.Contains(shield, "retry_connection true") {
		t.Fatalf("unexpected shield/slot config:\n%s", shield)
	}

	// HTTP-only TLSNone -> explicit http host:80, no tls block.
	plain := findFileContaining(t, files, "http plain.example.com:80, www.plain.example.com:80 {")
	if strings.Contains(plain, "    tls ") {
		t.Fatalf("TLSNone site must not contain tls block:\n%s", plain)
	}

	// On-demand TLS.
	tenant := findFileContaining(t, files, "tenant.example.com:443 {")
	if !strings.Contains(tenant, "on_demand true") || !strings.Contains(tenant, `on_demand_ask "http://127.0.0.1:3000/api/internal/on-demand-ask"`) {
		t.Fatalf("expected on_demand directives in tenant site:\n%s", tenant)
	}

	// WordPress PHP + security + routing + ignored RequireTargetFile + if_not ND_PHP_PATHINFO.
	wp := findFileContaining(t, files, "wp.example.com:443, www.wp.example.com:443 {")
	for _, want := range []string{
		`disable_symlinks false`,
		`backend "unix:///data/workspaces/.nd-php/u1-v83/run/wp1.sock"`,
		`extension ".php"`,
		`pass false`,
		`keepalive true`,
		`environment "HTTPS" "on"`,
		`block "203.0.113.5" "198.51.100.0/24"`,
		`allow "0.0.0.0/0"`,
		`users {`,
		`if ND_XMLRPC`,
		`if ND_UPLOAD_PHP`,
		`if ND_SCANNER_UA`,
		`Strict-Transport-Security`,
		`X-Frame-Options`,
		`public, max-age=2592000`,
		`IGNORED rule "rtf1" (rewrite): RequireTargetFile=true is unsupported by Ferron`,
		`if_not ND_PHP_PATHINFO`,
		`rewrite r"^[^?]+" "/index.php"`,
		`location "/new/${1}?{{request.uri.query}}"`,
	} {
		if !strings.Contains(wp, want) {
			t.Errorf("WordPress config missing %q:\n%s", want, wp)
		}
	}

	// Laravel strict_404 must not emit /index.php fallback rewrite.
	laravel := findFileContaining(t, files, "api.laravel.example.com:443 {")
	if strings.Contains(laravel, `rewrite r"^[^?]+" "/index.php"`) || !strings.Contains(laravel, "strict_404") {
		t.Fatalf("unexpected Laravel strict_404 fallback:\n%s", laravel)
	}

	// Suspended PHP site must not be rendered.
	for name, content := range files {
		if strings.Contains(content, "paused.example.com") {
			t.Fatalf("suspended site leaked into %s:\n%s", name, content)
		}
	}

	// Static SPA fallback.
	spa := findFileContaining(t, files, "spa.example.com:443 {")
	if !strings.Contains(spa, `rewrite r"^[^?]+" "/index.html"`) || strings.Contains(spa, "fcgi") {
		t.Fatalf("unexpected static SPA config:\n%s", spa)
	}

	// Redirect query preservation & ExternalProxy SkipVerify.
	redir := findFileContaining(t, files, "go.example.com:443 {")
	if !strings.Contains(redir, "status 308") || !strings.Contains(redir, `location "https://dest.example.com{{request.uri}}"`) {
		t.Fatalf("unexpected redirect config:\n%s", redir)
	}
	ext := findFileContaining(t, files, "ext.example.com:443 {")
	if !strings.Contains(ext, "no_verification true") || !strings.Contains(ext, "logical_dns true") {
		t.Fatalf("unexpected external proxy config:\n%s", ext)
	}

	// Gateways exclude UDP & Direct.
	gw, ok := files["sites/999-gateways.conf"]
	if !ok || !strings.Contains(gw, "*:8080 {") || strings.Contains(gw, "5353") || strings.Contains(gw, "5432") {
		t.Fatalf("unexpected gateways file:\n%s", gw)
	}
}

func TestRender_StrictUnsupportedRejectsRequireTargetFile(t *testing.T) {
	strictDrv := New(WithStrictUnsupported(true))
	st := &proxy.State{
		Sites: []proxy.Site{{
			ID:    "php:1",
			Hosts: []string{"php.example.com"},
			TLS:   proxy.TLSNone,
			Kind:  proxy.SiteKindPHP,
			PHP: &proxy.PHPSpec{
				DocRoot: "/data/workspaces/php1",
				Socket:  "/data/workspaces/.nd-php/u1-v83/run/php1.sock",
				Routing: []proxy.RoutingRule{
					{ID: "r1", Kind: "rewrite", MatchType: "regex", Pattern: `^/(.+)$`, Target: "/$1.php", RequireTargetFile: true, Enabled: true},
				},
			},
		}},
	}
	if _, err := strictDrv.Render(st); err == nil {
		t.Fatalf("expected error in strict unsupported mode for RequireTargetFile rule")
	}
}

func TestRender_StateBuildEndToEnd(t *testing.T) {
	st, err := state.Build(state.BuildInput{
		PanelDomain:      "panel.nd.test",
		PanelEnableHTTPS: true,
		ACMEEmail:        "admin@nd.test",
		Apps:             []db.App{{ID: "a1", Name: "web", ActiveSlot: "blue"}},
		AppDomains:       []db.AppDomain{{ID: 1, AppID: "a1", Domain: "nd.test", Service: "web", Port: 3000, EnableHTTPS: true, EnableWWW: true}},
		AppTargets:       map[string]state.AppTarget{"a1": {ServiceContainers: map[string]string{"web": "nd-a1-blue"}}},
	})
	if err != nil {
		t.Fatalf("state.Build: %v", err)
	}
	files, err := New().Render(st)
	if err != nil {
		t.Fatalf("Render(state.Build): %v", err)
	}
	assertUniversalFerronInvariants(t, files)
	panel := files["sites/000-panel.conf"]
	if !strings.Contains(panel, "panel.nd.test:443 {") || !strings.Contains(panel, "http panel.nd.test:80 {") || !strings.Contains(panel, "logical_dns true") {
		t.Fatalf("unexpected rendered panel site:\n%s", panel)
	}
}

func findFileContaining(t *testing.T, files proxy.Files, substr string) string {
	t.Helper()
	for name, content := range files {
		if strings.HasPrefix(name, "sites/") && strings.Contains(content, substr) {
			return content
		}
	}
	t.Fatalf("no sites/*.conf file contains %q", substr)
	return ""
}

func TestStaticPathWithoutHidePatternsStillHidesSecrets(t *testing.T) {
	st := &proxy.State{Sites: []proxy.Site{{
		ID: "assets", Hosts: []string{"a.test"}, TLS: proxy.TLSNone, Kind: proxy.SiteKindAppProxy,
		Upstream: &proxy.UpstreamRef{Name: "web", Service: "web", Container: "web", Port: 80, Scheme: "http"},
		Paths: []proxy.PathRoute{{Prefix: "/assets", StripPrefix: true, Priority: 10,
			Handler: proxy.Handler{Kind: proxy.HandlerStaticDir, Static: &proxy.StaticDirSpec{Root: "/mnt/shared/vol"}}}},
		Default: &proxy.Handler{Kind: proxy.HandlerProxy},
	}}}
	files, err := New().Render(st)
	if err != nil {
		t.Fatal(err)
	}
	var site string
	for k, v := range files {
		if strings.HasPrefix(k, "sites/") {
			site = v
		}
	}
	if !strings.Contains(site, "_hide {") || !strings.Contains(site, `\.env`) || !strings.Contains(site, "status 404") {
		t.Fatalf("static path without Hide must still block secrets:\n%s", site)
	}
}
