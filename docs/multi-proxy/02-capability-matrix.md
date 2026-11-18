# 02 — Capability matrix

Legend: ✅ supported natively · 🟡 supported with a different mechanism or a caveat · ❌ not supported (gate it in UI/API) · 🔍 verify during implementation

Caddy column = current behavior (source of truth). Angie/Ferron columns = how the driver must reproduce it.

## Sites and routing

| NextDeploy feature | Caddy (today) | Angie | Ferron |
|---|---|---|---|
| Domain + optional `www.` | site address list | `server_name d www.d;` ✅ | host block `d, www.d` ✅ (verified: `d, www.d` and `d www.d` are identical) |
| HTTP-only domain (`EnableHTTPS=false`) | `http://d` | `listen 80;` server only ✅ | `http d:80` host / `tls false` ✅ |
| HTTPS automatic (public) | ACME (default) | one `acme_client` **per site** (names `[A-Za-z0-9_]` only, shared `account_key`) + `acme <name>` in the server ✅ | `tls { provider acme; challenge http-01; cache … }` ✅ |
| HTTPS internal (dev domains, `ShouldUseInternalTLS`, behind shield) | `tls internal` | ND private CA leaf cert (`ssl_certificate`) 🟡 | ND private CA leaf cert (`tls cert key`) 🟡 |
| HTTP→HTTPS redirect | automatic | explicit `listen 80` server `return 308 https://$host$request_uri;` ✅ | `https_redirect` (default on with TLS) ✅ |
| wildcard sslip.io/nip.io domains | per-host ACME | per-host ACME ✅ | per-host ACME, or `on_demand` + `on_demand_ask` (panel endpoint) ✅ |
| Path route: direct static/media (`handle_path` + `file_server`, hide list) | yes | `location ^~ /prefix/ { alias <root>/; }` + deny hide patterns ✅ | `match` + `if` + `root` + `rewrite` to strip prefix 🟡 (do NOT use nested `location` + host `proxy`: Ferron merges matching locations and inherits `proxy`, verified) |
| Path route: reverse proxy to service | `reverse_proxy /p {{upstreams}}` | `location /p { proxy_pass http://<up>; }` ✅ | `location /p { proxy { upstream … } }` 🟡 (Ferron strips the location prefix before proxying; append the prefix to the upstream URL to keep it) |
| Default reverse proxy | `reverse_proxy {{upstreams PORT}}` | `location / { proxy_pass http://<up>; }` ✅ | `proxy { upstream http://<container>:<port> { logical_dns true } }` ✅ |
| Long streams survive reload (`stream_close_delay 10m`) | yes | old workers keep streams; bound with `worker_shutdown_timeout 10m` ✅ | in-flight requests survived 20 reloads (tested); HTTP/2 conns get GOAWAY 🟡 |
| WebSocket | automatic | `proxy_http_version 1.1; Upgrade/Connection map` ✅ | automatic ✅ |
| Security headers (X-Frame-Options, nosniff, Referrer-Policy, X-XSS) | `header` | `add_header … always;` ✅ | `header Name "v"` ✅ |
| Strip spoofable forwarded headers when untrusted (`applyUntrustedDirectProxyHeaders`) | header_up | `proxy_set_header X-Forwarded-For $remote_addr;` etc. ✅ | `request_header` replace ✅ |
| Container IP change without reload | ✅ | ✅ Docker module | ✅ only with `logical_dns true` |
| Gateway ports (allocated host ports) | Caddy publishes ports | `listen <port>` servers + compose port publish ✅ | `*:<port>` HTTP host blocks + publish ✅. Raw TCP is **not** supported: `tcp *:port` passes `ferron validate` but never listens, so `Direct`/UDP gateways are skipped |

## Global options (`caddy.GlobalOptions`)

| Option | Caddy | Angie | Ferron |
|---|---|---|---|
| `read_header` | `timeouts.read_header` | `client_header_timeout` ✅ | ❌ fixed 30 s (zincio-http `header_read_timeout`, not exposed) |
| `read_body_idle` | `timeouts.read_body_idle` | `client_body_timeout` ✅ (per-read, not total) | ❌ |
| `idle` | `timeouts.idle` | `keepalive_timeout` (HTTP/1.1 and HTTP/2) ✅ | ❌ HTTP/2 never closes idle connections (verified in source). HTTP/1.1 is fixed at 30 s |
| `grace_period` | `grace_period` | `worker_shutdown_timeout` ✅ | ❌ (reload drains via GOAWAY; no knob) |
| `max_header_size` | `max_header_size` | `large_client_header_buffers 4 <size>` ✅ | `h2_max_header_list_size` 🟡 (HTTP/1 limit fixed at 16 KiB) |
| `real_ip` cloudflare/custom + `trusted_proxies` | `trusted_proxies` + `client_ip_headers` | `set_real_ip_from <cidr>; real_ip_header CF-Connecting-IP;` ✅ | `client_ip_from_header x-forwarded-for { trusted_proxy … }` 🟡 (no CF-Connecting-IP; X-Forwarded-For works behind Cloudflare) |
| `trusted_strict` (right-to-left) | yes | `real_ip_recursive on` ✅ | ❌ Ferron always reads the leftmost `X-Forwarded-For` address (verified in source). Preflight hard-blocks when `trusted_strict` is set |
| shield proxies, direct cloudflare (auto) | yes | same mechanism ✅ | same mechanism 🟡 |
| Profiles (default/balanced/high/strict) | label sets | map to directives ✅ | partial; show unsupported fields as disabled |

## PHP / static hosting (`PHPSiteLabels`, `SiteSecurity`, `SiteRouting`)

| Feature | Caddy | Angie | Ferron |
|---|---|---|---|
| `php_fastcgi` over unix socket | yes | `fastcgi_pass unix:/data/workspaces/.nd-php/u<owner>-v<ver>/run/<app>.sock; include fastcgi_params; fastcgi_param SCRIPT_FILENAME …` ✅ (`user root;` so the 0660 socket is readable) | `fcgi { backend "unix://<sock>"; extension ".php"; pass false; keepalive true; environment "HTTPS" "on" }` ✅ (container runs as `0:0`) |
| Front-controller fallback (default / strict_404 / custom entry) | try_files | `try_files $uri $uri/ /index.php?$query_string;` ✅ | `rewrite r"^[^?]+" "/index.php" { file false; directory false; last }` ✅, with every routing rule and the fallback inside one `if_not ND_PHP_PATHINFO` block so `PATH_INFO` URLs (`/index.php/x`) keep working |
| `file_server.hide` list | yes | `location ~ (/\.git|/\.env|\.sql$|…) { deny all; }` ✅ | `match` + `status 404` ✅ |
| `encode zstd gzip` | yes | `gzip on;` plus the bundled zstd dynamic modules (`load_module`; separate `angie-module-zstd` package on host installs) 🟡 | `compressed` + `dynamic_compressed` ✅ |
| Media/code cache headers | header matchers | `location ~* \.(png|…)$ { expires 30d; add_header Cache-Control … }` ✅ | `match` + `header` ✅ |
| Blocked IPs / allow-only | remote_ip matchers | `deny`/`allow` ✅ | `block` / `allow` ✅ |
| Basic auth (bcrypt hash) | `basic_auth` | `auth_basic` + htpasswd ✅ (verified live on the Alpine image: `$2y$`, `$2b$`, `$2a$` all accepted, so no Debian image is needed) | `basic_auth` ✅ (verified: `$2y$`, `$2b$`; brute-force protection is on by default; `-fips` images reject bcrypt) |
| Block xmlrpc / PHP in uploads / scanner UAs | matchers | `location = /xmlrpc.php { return 403; }`, `location ~* /uploads/.*\.php$ { return 403; }`, `if ($http_user_agent ~* (sqlmap|…)) { return 403; }` ✅ | `match` + `status 403` ✅ |
| HSTS, frame protection | header | `add_header` ✅ | `header` ✅ |
| Routing rules: rewrite exact/prefix/regex | `rewrite` | `rewrite ^…$ … last;` / `location =` ✅ | `rewrite r"…" "…"` ✅ |
| Routing rules: redir with code + preserve query | `redir` | `return <code> <target>$is_args$args;` ✅ | 🟡 `status <code> { regex …; location … }` matches the path only. Interpolation runs before the regex replace, so write `${1}` (not `$1`) and add a `match` on `request.uri.query != ""` to append `?{{request.uri.query}}` |
| Routing rules: block 403/404/410 | `respond` | `return <code>;` ✅ | `status <code> { regex … }` ✅ |
| `unless_exists` (!-f/!-d) | file matcher | `try_files` or `if (!-f $request_filename)` ✅ | `rewrite … { file false; directory false }` ✅ |
| `require_target_file` (`.php -f`) | file matcher | `if (-f $document_root$uri.php)` 🟡 | ❌ no file-exists check on a transformed path (`rewrite` only supports `file false` / `directory false` on the current path). Preflight hard-blocks enabled rules; the driver omits them with a `# IGNORED` comment |
| WordPress disk-cache fast path (`@wp_cache`, 7 plugin paths) | `php_labels.go` matcher | `map` + `try_files` ✅ | ❌ no `-f` disk probe. PHP still serves the page |
| `.htaccess` import | → RoutingRule | same IR ✅ | same IR, minus `require_target_file` 🟡 |
| Symlinks in webroot | allowed | allowed (`disable_symlinks off` default) ✅ | **blocked by default**, set `disable_symlinks false` or `if_not_owner` (Laravel `storage:link` needs it) 🟡 |

## Custom proxy routes (`ProxyRoute`)

| Feature | Caddy | Angie | Ferron |
|---|---|---|---|
| proxy to external `http(s)://host:port` | label container `nd-route-<id>` | `server` + `proxy_pass` + `resolver` for hostnames ✅ | `proxy { upstream <target> { logical_dns true } }` ✅ |
| `skip_tls_verify` | `tls_insecure_skip_verify` | `proxy_ssl_verify off` (default off in nginx; set `on` when false) ✅ | `no_verification` ✅ |
| redirect route (code, keep_path) | `redir` | `return <code> <url>$request_uri;` ✅ | 🟡 `status <code> { location "<url>{{request.uri}}" }` (the query survives only through `{{request.uri}}`) |
| SSRF validation | `ValidateRouteForUser` | engine-neutral, reuse ✅ | ✅ |
| Implementation | one label container per route | rendered into conf; remove the `nd-route-*` containers on switch | rendered |

## Slots (blue/green) and previews

| Feature | Caddy | Angie | Ferron |
|---|---|---|---|
| Per-slot labels, preview host `<slot>-…` | labels per slot | upstream per slot `nd_<app>_<slot>`; active site → active slot upstream; preview site → slot upstream. Switch = re-render + reload ✅ | upstream URL = slot container name; switch = re-render + reload ✅ |
| Zero-downtime cutover | label swap | graceful reload ✅ | reload (watch out for #996) 🟡 |

## Operations

| Feature | Caddy | Angie | Ferron |
|---|---|---|---|
| Health/overview | admin API `:2019` | `api` module (`/status/`, read-only in OSS) + `angie -t` ✅ | admin API `/health`, `/status`, `/reload`, `/config` ✅ |
| Metrics | `caddy.metrics` | Prometheus endpoint (OSS) ✅ | Prometheus/OTLP ✅ |
| Logs | container logs | container logs (`error_log stderr`) ✅ | container logs ✅ |
| Config validation before apply | caddy adapt | `angie -t -c <staged>` ✅ | `ferron validate -c <staged>` ✅ |
| Shared volumes for file roots (`nd_shared_*`) | mounted into caddy | mount into angie ✅ | mount into ferron ✅ |

## Resilience defaults the drivers MUST set

| Concern | Angie default to render | Ferron |
|---|---|---|
| Idle HTTP/2 (Cloudflare) | `keepalive_timeout 20s;` when real_ip=cloudflare, else 75s | ❌ cannot; warn in UI (Cloudflare closes after 900 s) |
| Slow readers | `send_timeout 60s;` | ❌ cannot; warn |
| Connection cap | `worker_connections 65535; worker_rlimit_nofile 1048576;` | n/a |
| Upstream keepalive | `keepalive 64; proxy_http_version 1.1; proxy_set_header Connection "";` (mandatory) | default pooled ✅ |
| Reload drain | `worker_shutdown_timeout 10m;` (matches `stream_close_delay 10m`) | n/a |
| Docker DNS for external hostnames | `resolver 127.0.0.11 valid=10s ipv6=off;` | `logical_dns true` |
