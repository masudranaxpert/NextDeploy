package angie

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"panel/internal/proxy"
)

func renderMainConfig(d *Driver, st *proxy.State, useZstd bool) string {
	workers := "auto"
	if d != nil && strings.TrimSpace(d.workerProcesses) != "" && !proxy.HasUnsafeLabelChars(d.workerProcesses) {
		workers = strings.TrimSpace(d.workerProcesses)
	}
	grace := "10m"
	if st != nil && strings.TrimSpace(st.Global.GracePeriod) != "" {
		grace = formatAngieDuration(st.Global.GracePeriod, "10m")
	}

	var b strings.Builder
	b.WriteString("user root;\n")
	fmt.Fprintf(&b, "worker_processes %s;\n", workers)
	b.WriteString("worker_rlimit_nofile 1048576;\n")
	fmt.Fprintf(&b, "worker_shutdown_timeout %s;\n", grace)
	b.WriteString("pid /run/angie/angie.pid;\n")
	b.WriteString("error_log stderr warn;\n")
	b.WriteString("load_module /usr/lib/angie/modules/ngx_http_headers_more_filter_module.so;\n")
	if useZstd {
		b.WriteString("load_module /usr/lib/angie/modules/ngx_http_zstd_filter_module.so;\n")
		b.WriteString("load_module /usr/lib/angie/modules/ngx_http_zstd_static_module.so;\n")
	}
	b.WriteString("\nevents {\n")
	b.WriteString("    worker_connections 65535;\n")
	b.WriteString("}\n\n")
	b.WriteString("http {\n")
	b.WriteString("    include /etc/angie/mime.types;\n")
	b.WriteString("    default_type application/octet-stream;\n")
	b.WriteString("    access_log off;\n")
	b.WriteString("    sendfile on;\n")
	b.WriteString("    tcp_nopush on;\n")
	b.WriteString("    tcp_nodelay on;\n")
	b.WriteString("    include /etc/angie/prometheus_all.conf;\n")
	b.WriteString("    include 00-global.conf;\n\n")
	b.WriteString("    server {\n")
	b.WriteString("        listen 127.0.0.1:2019;\n")
	b.WriteString("        server_name _;\n")
	b.WriteString("        access_log off;\n\n")
	b.WriteString("        location /status/ {\n")
	b.WriteString("            api /status/;\n")
	b.WriteString("            allow 127.0.0.1;\n")
	b.WriteString("            deny all;\n")
	b.WriteString("        }\n\n")
	b.WriteString("        location = /p8s {\n")
	b.WriteString("            prometheus all;\n")
	b.WriteString("            allow 127.0.0.1;\n")
	b.WriteString("            deny all;\n")
	b.WriteString("        }\n")
	b.WriteString("    }\n\n")
	// Requests for names no site serves (scanners hitting the bare IP, stale DNS) must not land
	// on whichever site happens to be first: drop plain HTTP and refuse the TLS handshake.
	b.WriteString("    server {\n")
	b.WriteString("        listen 80 default_server;\n")
	b.WriteString("        listen 443 ssl default_server;\n")
	b.WriteString("        server_name _;\n")
	b.WriteString("        ssl_reject_handshake on;\n")
	b.WriteString("        access_log off;\n")
	b.WriteString("        return 444;\n")
	b.WriteString("    }\n\n")
	b.WriteString("    include upstreams/*.conf;\n")
	b.WriteString("    include sites/*.conf;\n")
	b.WriteString("}\n")
	return b.String()
}

func renderGlobalConfig(d *Driver, st *proxy.State, plan *renderPlan) (string, error) {
	g := st.Global
	readHeader := formatAngieDuration(g.ReadHeader, "10s")
	readBody := formatAngieDuration(g.ReadBodyIdle, "15s")
	defaultIdle := "75s"
	if plan.cfGlobal {
		defaultIdle = "20s"
	}
	idle := formatAngieDuration(g.Idle, defaultIdle)
	maxHeader := formatAngieSize(g.MaxHeaderSize, "16k")

	var b strings.Builder
	b.WriteString("docker_endpoint unix:/var/run/docker.sock;\n")
	b.WriteString("docker_max_object_size 1m;\n")
	b.WriteString("resolver 127.0.0.11 valid=10s ipv6=off;\n\n")

	fmt.Fprintf(&b, "client_header_timeout %s;\n", readHeader)
	fmt.Fprintf(&b, "client_body_timeout %s;\n", readBody)
	fmt.Fprintf(&b, "keepalive_timeout %s;\n", idle)
	b.WriteString("send_timeout 60s;\n")
	fmt.Fprintf(&b, "large_client_header_buffers 4 %s;\n", maxHeader)
	b.WriteString("client_max_body_size 0;\n")
	// WordPress pages are often 100-300 KB; with the 64 KB default every such response was
	// spooled to a temp file ("buffered to a temporary file" warnings). Buffers are only
	// allocated while a request uses them.
	b.WriteString("client_body_buffer_size 128k;\n")
	b.WriteString("fastcgi_buffer_size 32k;\n")
	b.WriteString("fastcgi_buffers 16 32k;\n")
	b.WriteString("fastcgi_busy_buffers_size 64k;\n\n")

	b.WriteString("variables_hash_max_size 8192;\n")
	b.WriteString("variables_hash_bucket_size 128;\n")
	b.WriteString("server_names_hash_max_size 8192;\n")
	b.WriteString("server_names_hash_bucket_size 128;\n\n")

	b.WriteString("map $http_upgrade $nd_connection_upgrade {\n")
	b.WriteString("    default upgrade;\n")
	b.WriteString("    '' '';\n")
	b.WriteString("}\n\n")

	b.WriteString("map $args $nd_amp_args {\n")
	b.WriteString("    default \"&$args\";\n")
	b.WriteString("    \"\" \"\";\n")
	b.WriteString("}\n\n")

	if plan.useWPCache {
		writeWPCacheMaps(&b)
	}

	ranges := collectTrustedRealIPRanges(g)
	if len(ranges) > 0 {
		for _, r := range ranges {
			if proxy.HasUnsafeLabelChars(r) {
				return "", fmt.Errorf("angie global: unsafe trusted proxy CIDR %q", r)
			}
			fmt.Fprintf(&b, "set_real_ip_from %s;\n", r)
		}
		header := "X-Forwarded-For"
		if plan.cfGlobal {
			header = "CF-Connecting-IP"
		}
		fmt.Fprintf(&b, "real_ip_header %s;\n", header)
		if g.TrustedStrict || plan.cfGlobal {
			b.WriteString("real_ip_recursive on;\n")
		} else {
			b.WriteString("real_ip_recursive off;\n")
		}
		b.WriteString("\n")
	}

	if len(plan.acmeClients) > 0 {
		acmeURL := DefaultACMEDirectory
		if d != nil && strings.TrimSpace(d.acmeDirectory) != "" {
			acmeURL = strings.TrimSpace(d.acmeDirectory)
		}
		if proxy.HasUnsafeLabelChars(acmeURL) {
			return "", fmt.Errorf("angie global: unsafe ACME directory URL %q", acmeURL)
		}
		email := strings.TrimSpace(st.ACMEEmail)
		if email != "" && proxy.HasUnsafeLabelChars(email) {
			return "", fmt.Errorf("angie global: unsafe ACME email %q", email)
		}

		names := make([]string, 0, len(plan.acmeClients))
		for name := range plan.acmeClients {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			if email != "" {
				fmt.Fprintf(&b, "acme_client %s %s email=%s account_key=/var/lib/angie/acme/nd_account.key;\n", name, acmeURL, email)
			} else {
				fmt.Fprintf(&b, "acme_client %s %s account_key=/var/lib/angie/acme/nd_account.key;\n", name, acmeURL)
			}
		}
	}

	return b.String(), nil
}

func writeWPCacheMaps(b *strings.Builder) {
	b.WriteString("map $request_method $wp_skip_method {\n    default 0;\n    POST 1;\n}\n")
	b.WriteString("map $args $wp_skip_args {\n    default 1;\n    \"\" 0;\n}\n")
	b.WriteString("map $http_cookie $wp_skip_cookie {\n    default 0;\n    \"~*(comment_author|wordpress_[a-f0-9]+|wp-postpass|wordpress_logged_in|woocommerce_items_in_cart|wp_woocommerce_session)\" 1;\n}\n")
	b.WriteString("map $uri $wp_skip_uri {\n    default 0;\n    \"~*(/wp-admin/|/xmlrpc\\.php|/wp-(app|cron|login|register|mail)\\.php|/wp-json/|/feed/|sitemap.*\\.xml)\" 1;\n}\n")
	cacheExprs := []string{
		`/wp-content/cache/all${uri}index.html`,
		`/wp-content/cache/all${uri}/index.html`,
		`/wp-content/cache/supercache/${host}${request_uri}/index-https.html`,
		`/wp-content/cache/supercache/${host}${request_uri}/index.html`,
		`/wp-content/cache/cache-enabler/${host}${request_uri}/https-index.html`,
		`/wp-content/cache/cache-enabler/${host}${request_uri}/index.html`,
		`/wp-content/cache/wp-rocket/${host}${request_uri}/index-https.html`,
		`/wp-content/cache/wp-rocket/${host}${request_uri}/index.html`,
	}
	for i, expr := range cacheExprs {
		fmt.Fprintf(b, "map \"$wp_skip_method$wp_skip_args$wp_skip_cookie$wp_skip_uri\" $wp_c%d {\n    \"0000\" \"%s\";\n    default \"/__nd_miss__\";\n}\n", i+1, expr)
	}
	b.WriteString("\n")
}

func collectTrustedRealIPRanges(g proxy.GlobalOptions) []string {
	cfActive := strings.EqualFold(strings.TrimSpace(g.RealIP), "cloudflare") || g.DirectCloudflare
	seen := make(map[string]bool)
	var out []string
	add := func(list []string) {
		for _, raw := range list {
			ip := strings.TrimSpace(raw)
			if ip == "" || seen[ip] {
				continue
			}
			seen[ip] = true
			out = append(out, ip)
		}
	}
	if cfActive {
		add(proxy.CloudflareRanges)
	}
	if strings.EqualFold(strings.TrimSpace(g.RealIP), "custom") {
		add(g.TrustedProxies)
	}
	add(g.ShieldProxies)
	return out
}

func renderProxySnippet() string {
	return `proxy_http_version 1.1;
proxy_set_header Host $host;
proxy_set_header Upgrade $http_upgrade;
proxy_set_header Connection $nd_connection_upgrade;
proxy_set_header X-Forwarded-Proto $scheme;
proxy_set_header X-Forwarded-Host $host;
proxy_read_timeout 1h;
proxy_send_timeout 60s;
proxy_buffering off;
`
}

func renderHideSnippet() string {
	return `location ~ (/\.git|/\.env|\.(?:sql|db|sqlite|key|sock)$) {
    deny all;
}
`
}

// PanelUpstreamName is the upstream of the panel itself. The panel container lives in the root
// stack and never carries Angie discovery labels, so it gets a static, DNS-resolved server.
const PanelUpstreamName = "nd_panel"

func renderUpstream(name string, up proxy.UpstreamRef) string {
	var b strings.Builder
	fmt.Fprintf(&b, "upstream %s {\n", name)
	fmt.Fprintf(&b, "    zone %s 64k;\n", name)
	if name == PanelUpstreamName && up.Port > 0 {
		host := strings.TrimSpace(up.Container)
		if host == "" {
			host = strings.TrimSpace(up.Service)
		}
		if host != "" && !proxy.HasUnsafeLabelChars(host) && !strings.ContainsAny(host, " ;{}") {
			fmt.Fprintf(&b, "    server %s:%d resolve;\n", host, up.Port)
		}
	}
	b.WriteString("    keepalive 64;\n")
	b.WriteString("}\n")
	return b.String()
}

func formatAngieDuration(raw, fallback string) string {
	s := strings.TrimSpace(strings.ToLower(raw))
	if s == "" {
		return fallback
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return fallback
	}
	if d%time.Hour == 0 {
		return strconv.FormatInt(int64(d/time.Hour), 10) + "h"
	}
	if d%time.Minute == 0 {
		return strconv.FormatInt(int64(d/time.Minute), 10) + "m"
	}
	if d%time.Second == 0 {
		return strconv.FormatInt(int64(d/time.Second), 10) + "s"
	}
	secs := int64((d + time.Second - 1) / time.Second)
	if secs < 1 {
		secs = 1
	}
	return strconv.FormatInt(secs, 10) + "s"
}

func formatAngieSize(raw, fallback string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return fallback
	}
	low := strings.ToLower(s)
	switch {
	case strings.HasSuffix(low, "kib"):
		return strings.TrimSuffix(low, "kib") + "k"
	case strings.HasSuffix(low, "kb"):
		return strings.TrimSuffix(low, "kb") + "k"
	case strings.HasSuffix(low, "mib"):
		return strings.TrimSuffix(low, "mib") + "m"
	case strings.HasSuffix(low, "mb"):
		return strings.TrimSuffix(low, "mb") + "m"
	case strings.HasSuffix(low, "gib"):
		return strings.TrimSuffix(low, "gib") + "g"
	case strings.HasSuffix(low, "gb"):
		return strings.TrimSuffix(low, "gb") + "g"
	default:
		if proxy.HasUnsafeLabelChars(low) {
			return fallback
		}
		return low
	}
}
