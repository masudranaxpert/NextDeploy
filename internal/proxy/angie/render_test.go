package angie

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"panel/internal/proxy"
	"panel/internal/proxy/pki"
)

func loadIRFixture(t *testing.T, name string) *proxy.State {
	t.Helper()
	path := filepath.Join("..", "state", "testdata", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	var st proxy.State
	if err := json.Unmarshal(data, &st); err != nil {
		t.Fatalf("unmarshal fixture %s: %v", name, err)
	}
	return &st
}

func TestDriverIdentityRegistryAndCapabilities(t *testing.T) {
	drv, err := proxy.Get("angie")
	if err != nil {
		t.Fatalf("proxy.Get(angie): %v", err)
	}
	if drv.Name() != "angie" {
		t.Fatalf("Name() = %q, want angie", drv.Name())
	}
	if drv.ContainerName() != "nd-proxy" {
		t.Fatalf("ContainerName() = %q, want nd-proxy", drv.ContainerName())
	}

	want := proxy.Capabilities{
		AutoTLS:                 true,
		InternalTLS:             true,
		OnDemandTLS:             false,
		DockerUpstreamDiscovery: true,
		PHPFastCGI:              true,
		BasicAuthBcrypt:         true,
		RegexRouting:            true,
		RequireTargetFile:       true,
		H2IdleTimeout:           true,
		WriteTimeout:            true,
		ReadHeaderTimeout:       true,
		ReadBodyTimeout:         true,
		GracePeriod:             true,
		MaxHeaderSizeHTTP1:      true,
		TrustedStrict:           true,
		LongStreamsOnReload: true, WordPressPageCache: true,
		AdminAPI:                false,
		Experimental:            false,
	}
	if got := drv.Capabilities(); got != want {
		t.Fatalf("Capabilities() = %+v, want %+v", got, want)
	}
}

func TestAppLabels(t *testing.T) {
	d := New()
	if got := d.AppLabels(nil, proxy.UpstreamRef{Name: "", Port: 3000}); got != nil {
		t.Fatalf("expected nil for empty upstream name, got %v", got)
	}
	if got := d.AppLabels(nil, proxy.UpstreamRef{Name: "nd_app_web_3000", Port: 0}); got != nil {
		t.Fatalf("expected nil for non-positive port, got %v", got)
	}

	got := d.AppLabels(nil, proxy.UpstreamRef{Name: "nd_app_web_3000_blue", Port: 3000})
	if len(got) != 2 {
		t.Fatalf("expected 2 labels, got %v", got)
	}
	if got["angie.http.upstreams.nd_app_web_3000_blue.port"] != "3000" {
		t.Fatalf("unexpected port label: %v", got)
	}
	if got["angie.network"] != "NextDeploy" {
		t.Fatalf("unexpected network label: %q", got["angie.network"])
	}

	custom := New(WithNetworkName("CustomNet"))
	gotCustom := custom.AppLabels(nil, proxy.UpstreamRef{Name: "nd_api_4000", Port: 4000})
	if gotCustom["angie.network"] != "CustomNet" {
		t.Fatalf("expected case-sensitive CustomNet, got %q", gotCustom["angie.network"])
	}
}

func TestStackService(t *testing.T) {
	d := New()
	svc, vols, err := d.StackService(proxy.StackOptions{
		NetworkKey:    "custom_net",
		NetworkName:   "CustomNet",
		DataDir:       "/srv/proxy/angie",
		PKIDir:        "/srv/proxy/pki",
		WorkspacesDir: "/srv/workspaces",
		PHPSocketsDir: "/run/nd-php",
		Gateways: []proxy.GatewayPort{
			{Port: 4013, ContainerPort: 3000, BindIP: "0.0.0.0", Protocol: "tcp", Direct: false},
			{Port: 8090, ContainerPort: 8080, BindIP: "127.0.0.1", Protocol: "tcp", Direct: false},
			{Port: 5432, ContainerPort: 5432, BindIP: "0.0.0.0", Protocol: "tcp", Direct: true},
		},
		SharedMounts: []proxy.SharedMount{
			{VolumeName: "assets_vol", Target: "/mnt/shared/assets_vol", ReadOnly: true},
		},
	})
	if err != nil {
		t.Fatalf("StackService: %v", err)
	}

	if svc["container_name"] != "nd-proxy" {
		t.Fatalf("container_name = %v, want nd-proxy", svc["container_name"])
	}
	if svc["image"] != AngieImage {
		t.Fatalf("image = %v, want %s", svc["image"], AngieImage)
	}
	if nets, ok := svc["networks"].([]any); !ok || len(nets) != 1 || nets[0] != "custom_net" {
		t.Fatalf("networks = %v, want [custom_net]", svc["networks"])
	}
	ulimits := svc["ulimits"].(map[string]any)["nofile"].(map[string]any)
	if ulimits["soft"] != 1048576 || ulimits["hard"] != 1048576 {
		t.Fatalf("unexpected ulimits: %v", ulimits)
	}

	ports := svc["ports"].([]any)
	portSet := make(map[string]bool)
	for _, p := range ports {
		portSet[p.(string)] = true
	}
	for _, want := range []string{"80:80", "443:443/tcp", "443:443/udp", "4013:4013", "127.0.0.1:8090:8090"} {
		if !portSet[want] {
			t.Fatalf("missing port %q in %v", want, ports)
		}
	}
	if portSet["5432:5432"] {
		t.Fatalf("direct L4 port 5432 must not be published on proxy: %v", ports)
	}

	mounts := svc["volumes"].([]any)
	mountLines := make([]string, len(mounts))
	for i, m := range mounts {
		mountLines[i] = m.(string)
	}
	joined := strings.Join(mountLines, "\n")
	for _, must := range []string{
		"/var/run/docker.sock:/var/run/docker.sock",
		"/srv/proxy/angie:/etc/angie/nd:ro",
		"/srv/proxy/pki/sites:/etc/nd-pki/sites:ro",
		"/srv/proxy/pki/imported:/etc/nd-pki/imported:ro",
		"angie_acme:/var/lib/angie/acme",
		"/srv/workspaces:/data/workspaces:ro",
		"/run/nd-php:/run/nd-php",
		":/mnt/shared/assets_vol:ro",
	} {
		if !strings.Contains(joined, must) {
			t.Fatalf("missing mount %q in:\n%s", must, joined)
		}
	}
	if strings.Contains(joined, "/pki/ca") {
		t.Fatalf("private CA directory must NEVER be mounted: %s", joined)
	}
	if _, ok := vols["angie_acme"]; !ok {
		t.Fatalf("expected angie_acme top-level volume in %v", vols)
	}
	if len(vols) != 2 {
		t.Fatalf("expected angie_acme + 1 nd_shared volume, got %v", vols)
	}

	if _, _, err := d.StackService(proxy.StackOptions{PKIDir: "/data/proxy/pki/ca"}); err == nil {
		t.Fatal("expected error when PKIDir points directly to CA directory")
	}
}

func TestRenderAllGoldenIRFixtures(t *testing.T) {
	d := New()
	fixtures := []string{
		"01_app_simple_www_auto_tls.golden.json",
		"02_app_path_routes.golden.json",
		"03_internal_tls_variants.golden.json",
		"04_trusted_proxy_cloudflare.golden.json",
		"05_blue_green_slot_preview.golden.json",
		"06_php_wordpress_security_cache.golden.json",
		"07_php_laravel_routing_suspended.golden.json",
		"08_php_dummy_domain_exclusion.golden.json",
		"09_static_site.golden.json",
		"10_proxy_routes_external_redirect.golden.json",
		"11_gateway_http_vs_direct_l4.golden.json",
		"12_panel_shared_mounts_deterministic.golden.json",
	}
	for _, f := range fixtures {
		st := loadIRFixture(t, f)
		files, err := d.Render(st)
		if err != nil {
			t.Fatalf("%s: Render failed: %v", f, err)
		}
		if files["angie.conf"] == "" || files["00-global.conf"] == "" {
			t.Fatalf("%s: missing core files", f)
		}
	}
}

func TestRenderAppProxyPathsTLSAndRealIP(t *testing.T) {
	d := New()

	// Fixture 01: AppProxy with www + AutoTLS + untrusted headers + non-CF keepalive 75s
	st01 := loadIRFixture(t, "01_app_simple_www_auto_tls.golden.json")
	f01, err := d.Render(st01)
	if err != nil {
		t.Fatalf("Render 01: %v", err)
	}
	assertContainsAll(t, "01/angie.conf", f01["angie.conf"],
		"user root;",
		"worker_rlimit_nofile 1048576;",
		"worker_shutdown_timeout 10m;",
		"worker_connections 65535;",
		"include /etc/angie/prometheus_all.conf;",
		"listen 127.0.0.1:2019;",
		"api /status/;",
		"prometheus all;",
	)
	if strings.Contains(f01["angie.conf"], "ngx_http_zstd_filter_module.so") {
		t.Fatalf("zstd module should not be loaded when unused")
	}
	acmeName01 := ACMEClientName("app:web-a1b2:dom:1")
	assertContainsAll(t, "01/00-global.conf", f01["00-global.conf"],
		"docker_endpoint unix:/var/run/docker.sock;",
		"docker_max_object_size 1m;",
		"resolver 127.0.0.11 valid=10s ipv6=off;",
		"keepalive_timeout 75s;",
		"large_client_header_buffers 4 16k;",
		"acme_client "+acmeName01+" https://acme-v02.api.letsencrypt.org/directory email=ops@example.com account_key=/var/lib/angie/acme/nd_account.key;",
	)
	assertContainsAll(t, "01/upstream", f01["upstreams/nd_web_a1b2_web_3000_blue.conf"],
		"upstream nd_web_a1b2_web_3000_blue {",
		"zone nd_web_a1b2_web_3000_blue 64k;",
		"keepalive 64;",
	)
	site01 := f01["sites/"+SafeFileStem("app:web-a1b2:dom:1")+".conf"]
	assertContainsAll(t, "01/site", site01,
		"server_name example.com www.example.com;",
		"return 308 https://$host$request_uri;",
		"listen 443 ssl;",
		"listen 443 quic;",
		"http2 on;",
		"acme "+acmeName01+";",
		"ssl_certificate $acme_cert_"+acmeName01+";",
		"ssl_certificate_key $acme_cert_key_"+acmeName01+";",
		"Alt-Svc",
		"error_page 502 =503 @nd_down;",
		"proxy_set_header X-Forwarded-For $remote_addr;",
		`proxy_set_header CF-Connecting-IP "";`,
	)
	if strings.Contains(site01, "reuseport") {
		t.Fatalf("per-site server block must NOT set reuseport on quic listener:\n%s", site01)
	}

	// Fixture 02: StaticDir alias ONLY vs Proxy location ONLY
	st02 := loadIRFixture(t, "02_app_path_routes.golden.json")
	f02, err := d.Render(st02)
	if err != nil {
		t.Fatalf("Render 02: %v", err)
	}
	site02 := f02["sites/"+SafeFileStem("app:store-1:dom:2")+".conf"]
	assertContainsAll(t, "02/site", site02,
		"location ^~ /assets/ {\n        alias /data/workspaces/store-1/dist/assets/;\n",
		"location ^~ /media/ {\n        alias /data/workspaces/store-1/uploads/;\n",
		"location /api {\n        proxy_pass http://nd_store_1_api_4000_blue;",
	)

	// Fixture 03: Internal TLS PKI paths + HTTP-only TLSNone
	st03 := loadIRFixture(t, "03_internal_tls_variants.golden.json")
	f03, err := d.Render(st03)
	if err != nil {
		t.Fatalf("Render 03: %v", err)
	}
	stem15, _ := pki.SafeSiteStem("app:tls-app:dom:15")
	site15 := f03["sites/"+SafeFileStem("app:tls-app:dom:15")+".conf"]
	assertContainsAll(t, "03/internal", site15,
		"ssl_certificate /etc/nd-pki/sites/"+stem15+".crt;",
		"ssl_certificate_key /etc/nd-pki/sites/"+stem15+".key;",
	)
	site16 := f03["sites/"+SafeFileStem("app:tls-app:dom:16")+".conf"]
	if strings.Contains(site16, "listen 443") || strings.Contains(site16, "return 308") {
		t.Fatalf("TLSNone site must not emit HTTPS redirect or 443 listener:\n%s", site16)
	}

	// Fixture 04: Cloudflare RealIP + TrustedStrict + Shield override + default CF 20s idle
	st04 := loadIRFixture(t, "04_trusted_proxy_cloudflare.golden.json")
	st04.Global.Idle = ""
	f04, err := d.Render(st04)
	if err != nil {
		t.Fatalf("Render 04: %v", err)
	}
	assertContainsAll(t, "04/global", f04["00-global.conf"],
		"keepalive_timeout 20s;",
		"set_real_ip_from 173.245.48.0/20;",
		"set_real_ip_from 107.189.6.222;",
		"real_ip_header CF-Connecting-IP;",
		"real_ip_recursive on;",
	)
	site21 := f04["sites/"+SafeFileStem("app:tp-app:dom:21")+".conf"]
	assertContainsAll(t, "04/trusted", site21, "proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;")
	site22 := f04["sites/"+SafeFileStem("app:tp-app:dom:22")+".conf"]
	assertContainsAll(t, "04/untrusted", site22, "proxy_set_header X-Forwarded-For $remote_addr;", `proxy_set_header CF-Connecting-IP "";`)

	// Fixture 12: Panel request header {client_ip} translation + Strict/Balanced size conversion
	st12 := loadIRFixture(t, "12_panel_shared_mounts_deterministic.golden.json")
	st12.Global.MaxHeaderSize = "8KB"
	f12, err := d.Render(st12)
	if err != nil {
		t.Fatalf("Render 12: %v", err)
	}
	assertContainsAll(t, "12/angie.conf", f12["angie.conf"], "worker_shutdown_timeout 30s;")
	assertContainsAll(t, "12/global", f12["00-global.conf"], "keepalive_timeout 2m;", "large_client_header_buffers 4 8k;")
	panelConf := f12["sites/panel.conf"]
	assertContainsAll(t, "12/panel", panelConf,
		`proxy_set_header X-Real-IP "$remote_addr";`,
		`proxy_set_header X-Forwarded-For "$remote_addr";`,
	)
	if strings.Contains(panelConf, "{client_ip}") {
		t.Fatalf("Caddy {client_ip} placeholder leaked into Angie panel config:\n%s", panelConf)
	}
}

func TestRenderRoutesGatewaysAndImportedCerts(t *testing.T) {
	d := New(WithImportedCerts(map[string]ManualCert{
		"ext.example.com": {
			CertPath: "/etc/nd-pki/imported/ext.example.com.crt",
			KeyPath:  "/etc/nd-pki/imported/ext.example.com.key",
		},
	}))

	st10 := loadIRFixture(t, "10_proxy_routes_external_redirect.golden.json")
	f10, err := d.Render(st10)
	if err != nil {
		t.Fatalf("Render 10: %v", err)
	}
	// ext.example.com uses imported manual cert and must NOT emit acme_client for route:1
	if strings.Contains(f10["00-global.conf"], ACMEClientName("route:1")) {
		t.Fatalf("imported cert override must suppress acme_client for route:1")
	}
	extConf := f10["sites/"+SafeFileStem("route:1")+".conf"]
	assertContainsAll(t, "10/ext-imported", extConf,
		"ssl_certificate /etc/nd-pki/imported/ext.example.com.crt;",
		`set $nd_target "https://10.0.0.50:8443";`,
		"proxy_pass $nd_target;",
		"proxy_ssl_verify off;",
		"proxy_ssl_server_name on;",
	)
	// Also verify HTTPS ExternalProxy with SkipVerify == false emits trusted CA
	stHTTPS := &proxy.State{
		Sites: []proxy.Site{
			{
				ID:           "route:99",
				Hosts:        []string{"strict.example.com"},
				TLS:          proxy.TLSAuto,
				TrustedProxy: true,
				Kind:         proxy.SiteKindExternalProxy,
				Upstream: &proxy.UpstreamRef{
					Name:       "nd_route_99",
					Scheme:     "https",
					SkipVerify: false,
					External:   "https://origin.example.com:8443",
				},
			},
		},
	}
	fStrict, err := d.Render(stHTTPS)
	if err != nil {
		t.Fatalf("Render strict https route: %v", err)
	}
	assertContainsAll(t, "strict-ext", fStrict["sites/"+SafeFileStem("route:99")+".conf"],
		"proxy_ssl_verify on;",
		"proxy_ssl_trusted_certificate /etc/ssl/certs/ca-certificates.crt;",
		"proxy_ssl_server_name on;",
	)

	redirConf := f10["sites/"+SafeFileStem("route:3")+".conf"]
	assertContainsAll(t, "10/redir", redirConf, "return 302 https://target.example.com/docs$request_uri;")

	// Fixture 11: Gateways
	st11 := loadIRFixture(t, "11_gateway_http_vs_direct_l4.golden.json")
	f11, err := d.Render(st11)
	if err != nil {
		t.Fatalf("Render 11: %v", err)
	}
	assertContainsAll(t, "11/gateways", f11["sites/gateways.conf"],
		"listen 4013;",
		"listen 127.0.0.1:8090;",
		"proxy_pass http://nd_gw_app_web_3000_blue;",
		"proxy_pass http://nd_gw_app_api_8080_blue;",
		"error_page 502 =503 @nd_down;",
	)
}

func TestRenderPHPLaravelWordPressStaticAndStrict404(t *testing.T) {
	d := New()

	// Fixture 06: WordPress + @wp_cache + Security + BasicAuth + zstd/headers-more
	st06 := loadIRFixture(t, "06_php_wordpress_security_cache.golden.json")
	f06, err := d.Render(st06)
	if err != nil {
		t.Fatalf("Render 06: %v", err)
	}
	assertContainsAll(t, "06/angie.conf", f06["angie.conf"],
		"user root;",
		"ngx_http_headers_more_filter_module.so",
		"ngx_http_zstd_filter_module.so",
		"ngx_http_zstd_static_module.so",
	)
	assertContainsAll(t, "06/global", f06["00-global.conf"], "$wp_skip_cookie", "$wp_c1", "$wp_c8")
	wpStem := SafeFileStem("php:wp-blog")
	wpConf := f06["sites/"+wpStem+".conf"]
	assertContainsAll(t, "06/wp", wpConf,
		"root /data/workspaces/wp-blog/htdocs;",
		"disable_symlinks off;",
		"client_max_body_size 5g;",
		"deny 1.2.3.4;",
		"deny 10.0.0.0/8;",
		"allow 203.0.113.0/24;\n    deny all;",
		"auth_basic_user_file htpasswd/"+wpStem+".htpasswd;",
		"location = /xmlrpc.php {",
		"location ~* /(uploads|upload|files|media|wp-content/uploads)/",
		"Strict-Transport-Security: max-age=31536000; includeSubDomains",
		"X-Frame-Options: SAMEORIGIN",
		"fastcgi_split_path_info ^(.+\\.php)(/.*)$;",
		"set $nd_path_info $fastcgi_path_info;",
		"try_files $fastcgi_script_name =404;",
		"fastcgi_param PATH_INFO $nd_path_info;",
		"fastcgi_param HTTPS on;",
		"fastcgi_pass unix:/data/workspaces/.nd-php/u7-v83/run/wp-blog.sock;",
		"try_files $wp_c1 $wp_c2 $wp_c3 $wp_c4 $wp_c5 $wp_c6 $wp_c7 $wp_c8 $uri $uri/ /index.php?$args;",
	)
	if gotHT := f06["htpasswd/"+wpStem+".htpasswd"]; !strings.HasPrefix(gotHT, "admin:$2y$10$") {
		t.Fatalf("unexpected htpasswd file content: %q", gotHT)
	}
	// Security location rules MUST precede FastCGI PHP execution block
	if strings.Index(wpConf, "location = /xmlrpc.php") > strings.Index(wpConf, "fastcgi_pass") {
		t.Fatalf("xmlrpc block must precede fastcgi_pass block")
	}
	if strings.Index(wpConf, "location ~* /(uploads|") > strings.Index(wpConf, "fastcgi_pass") {
		t.Fatalf("uploads php block must precede fastcgi_pass block")
	}

	// Fixture 07: Laravel + RoutingRule ($0 capture shift, RequireTargetFile, UnlessFile, redir $nd_amp_args, block) + Suspended skip
	st07 := loadIRFixture(t, "07_php_laravel_routing_suspended.golden.json")
	f07, err := d.Render(st07)
	if err != nil {
		t.Fatalf("Render 07: %v", err)
	}
	if _, exists := f07["sites/"+SafeFileStem("php:lar-susp")+".conf"]; exists {
		t.Fatalf("suspended PHP site must not be rendered")
	}
	if strings.Contains(f07["00-global.conf"], ACMEClientName("php:lar-susp")) {
		t.Fatalf("suspended PHP site must not allocate acme_client")
	}
	larConf := f07["sites/"+SafeFileStem("php:lar-app")+".conf"]
	assertContainsAll(t, "07/laravel", larConf,
		"rewrite ^/health$ /health.php last;",
		"rewrite ^/v1(?:/.*)?$ /api.php last;",
		"if (-e $request_filename) { set $nd_rw_2 \"0\"; }",
		"if (!-f $document_root$uri.php) { set $nd_rw_2 \"0\"; }",
		"rewrite (^/([^.]+)$) /$2.php?src=$1 last;",
		"if ($uri ~ ^/legacy(?:/.*)?$) {\n        return 301 https://docs.example.com/new?ref=old$nd_amp_args;",
		`if ($uri = "/xmlrpc.php") {`+"\n"+`        return 403;`,
	)

	// Fixture 09: Static site
	st09 := loadIRFixture(t, "09_static_site.golden.json")
	f09, err := d.Render(st09)
	if err != nil {
		t.Fatalf("Render 09: %v", err)
	}
	staticConf := f09["sites/"+SafeFileStem("php:docs-1")+".conf"]
	assertContainsAll(t, "09/static", staticConf,
		"root /data/workspaces/docs-1/htdocs/dist;",
		"try_files $uri $uri/ /index.html;",
		`if ($uri = "/old") {`+"\n"+`        return 302 /new;`,
	)
	if strings.Contains(staticConf, "fastcgi_pass") {
		t.Fatalf("static site must not emit fastcgi_pass:\n%s", staticConf)
	}

	// Strict404 PHP site must NEVER contain $uri/index.php in try_files (prevents raw PHP source leak)
	stStrict := &proxy.State{
		Sites: []proxy.Site{
			{
				ID:    "php:strict",
				Hosts: []string{"strict.test"},
				TLS:   proxy.TLSNone,
				Kind:  proxy.SiteKindPHP,
				PHP: &proxy.PHPSpec{
					DocRoot:      "/data/workspaces/strict/htdocs",
					Socket:       "/data/workspaces/.nd-php/u1-v83/run/strict.sock",
					FallbackMode: proxy.FallbackStrict404,
				},
			},
		},
	}
	fStrict, err := d.Render(stStrict)
	if err != nil {
		t.Fatalf("Render strict_404: %v", err)
	}
	strictConf := fStrict["sites/"+SafeFileStem("php:strict")+".conf"]
	assertContainsAll(t, "strict404", strictConf, "index index.php index.html;", "try_files $uri $uri/ =404;")
	if strings.Contains(strictConf, "$uri/index.php") {
		t.Fatalf("strict_404 must NEVER emit $uri/index.php in try_files:\n%s", strictConf)
	}
}

func assertContainsAll(t *testing.T, label, got string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(got, w) {
			t.Errorf("%s: missing %q in:\n%s", label, w, got)
		}
	}
}

// The panel container never carries Angie discovery labels: its upstream must have a static,
// DNS-resolved server or every panel request gets "no live upstreams" (503).
func TestPanelUpstreamHasStaticServer(t *testing.T) {
	got := renderUpstream(PanelUpstreamName, proxy.UpstreamRef{Name: "nd_panel", Container: "panel", Service: "panel", Port: 8080})
	if !strings.Contains(got, "server panel:8080 resolve;") {
		t.Fatalf("panel upstream:\n%s", got)
	}
	app := renderUpstream("nd_app_x", proxy.UpstreamRef{Name: "nd_app_x", Container: "app-1", Port: 80})
	if strings.Contains(app, "server ") {
		t.Fatalf("app upstreams stay label-discovered:\n%s", app)
	}
}

func TestMainConfigRejectsUnknownHostsAndSizesFastCGIBuffers(t *testing.T) {
	files, err := New().Render(&proxy.State{})
	if err != nil {
		t.Fatal(err)
	}
	assertContainsAll(t, "angie.conf", files["angie.conf"],
		"listen 80 default_server;", "listen 443 ssl default_server;", "ssl_reject_handshake on;", "return 444;")
	assertContainsAll(t, "00-global.conf", files["00-global.conf"],
		"fastcgi_buffers 16 32k;", "fastcgi_buffer_size 32k;", "client_body_buffer_size 128k;")
}
