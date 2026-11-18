# 03 — Angie driver

Engine: Angie OSS (BSD-2-Clause, fork of nginx by former nginx core developers). The OSS edition includes the Docker upstream module and the ACME client. The REST API for writing upstreams and active health probes are **Angie PRO only**: do not use them.

Pinned image: `docker.angie.software/angie:1.12.4` (Alpine). Pin an exact version and bump it deliberately.

## 1. Root stack service

```yaml
services:
  proxy:                         # keep container_name "caddy"? NO -> use "nd-proxy" for all engines (see 06 for migration)
    container_name: nd-proxy
    image: docker.angie.software/angie:1.12.4
    restart: unless-stopped
    networks: [nextdeploy]
    networks: [nextdeploy]       # compose key; the Docker network is named NextDeploy and `angie.network` is case-sensitive
    ports: ["80:80", "443:443/tcp", "443:443/udp"]   # + gateway ports
    ulimits: { nofile: { soft: 1048576, hard: 1048576 } }
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock     # Docker upstream module (needs read+write per Angie docs)
      - /data/proxy/angie:/etc/angie/nd:ro              # parent dir mounted (not single files): rendered `sites -> sites.gen-<hash>` symlink swaps resolve in the container
      - /data/proxy/pki/sites:/etc/nd-pki/sites:ro      # ND private CA leaf certs (internal TLS)
      - /data/proxy/pki/imported:/etc/nd-pki/imported:ro  # certs imported from Caddy on switch
      - angie_acme:/var/lib/angie/acme                  # ACME state, persistent
      - /data/workspaces:/data/workspaces:ro            # PHP/static roots (same as caddy today)
      - /data/workspaces/.nd-php:/data/workspaces/.nd-php   # php-fpm sockets (u<owner>-v<ver>/run/<app>.sock)
      # + nd_shared_* volumes (SharedMounts)
```

The panel validates the staged generation in a throwaway container (`docker run --rm -v <staged>:/etc/angie/nd/staged:ro … angie -t -c /etc/angie/nd/staged/angie.conf`) and reloads with `docker exec nd-proxy angie -s reload -c /etc/angie/nd/sites/angie.conf`. `/etc/angie/nd/sites/angie.conf` (through the `sites` symlink) is the live entry point.

## 2. Config layout

```
/data/proxy/angie/
  sites -> sites.gen-<hash>      # relative symlink, swapped atomically
  sites.gen-<hash>/
    angie.conf                   # main config (rendered by the driver, not a static template)
    ...                          # global, upstream, site, snippet files
```

Older generations are kept (3 by default) for rollback.

`angie.conf` (rendered):

```nginx
user root;                              # docker.sock + 0660 root-owned php-fpm sockets
worker_processes auto;                  # explicit count when the container is cgroup-limited
worker_rlimit_nofile 1048576;
worker_shutdown_timeout 10m;            # = Caddy stream_close_delay 10m (GlobalOptions.GracePeriod overrides)
error_log stderr warn;
load_module /usr/lib/angie/modules/ngx_http_headers_more_filter_module.so;
# zstd modules are loaded only when a site uses zstd
events { worker_connections 65535; }
http {
    include       /etc/angie/mime.types;
    access_log    off;                  # or JSON to stdout when enabled in settings
    # global, upstreams and sites are rendered into the same generation
}
```

`00-global` (rendered from `GlobalOptions`):

```nginx
docker_endpoint unix:/var/run/docker.sock;
docker_max_object_size 1m;                         # default 64k breaks container discovery above ~25 containers
resolver 127.0.0.11 valid=10s ipv6=off;           # Docker DNS: container-hosted panel only, never on host-installed nd-edge

client_header_timeout  {{read_header|10s}};
client_body_timeout    {{read_body_idle|15s}};
keepalive_timeout      {{idle|75s}};               # 20s when real_ip=cloudflare (see README evidence)
send_timeout           60s;
large_client_header_buffers 4 {{max_header_size|16k}};
client_max_body_size   0;                          # per-site overrides

# real IP
{{range trusted}}set_real_ip_from {{.}};{{end}}
real_ip_header {{CF-Connecting-IP | X-Forwarded-For}};
real_ip_recursive {{trusted_strict ? on : off}};

map $http_upgrade $nd_connection_upgrade { default upgrade; '' ''; }

# ACME: one client per site (see §5). Client names match [A-Za-z0-9_]+ only.
acme_client nd_app_1_dom_2 https://acme-v02.api.letsencrypt.org/directory account_key=/var/lib/angie/acme/nd_account.key;
```

## 3. Upstreams (Docker labels)

`upstreams/nd_<app>_<svc>_<port>[_<slot>].conf`:

```nginx
upstream nd_<app>_<svc>_<port> {
    zone nd_<app>_<svc>_<port> 64k;     # required by the Docker module
    keepalive 64;                       # mandatory (throughput: 650 → 12k req/s)
}
```

App container labels (emitted by `Driver.AppLabels` into the generated compose):

```yaml
labels:
  angie.http.upstreams.nd_<app>_<svc>_<port>.port: "<port>"
  angie.network: "NextDeploy"
```

Behavior (tested): container start → added; stop/remove → removed; recreate with new IP → the new IP is picked up within ~1 s, no reload.

Edge case: an upstream with zero members returns 502. While an app is stopped, render a `error_page 502 = @nd_down` fallback page per site (verified: an empty `zone` + `keepalive` upstream passes `angie -t`).

Docker module notes (verified): the socket needs read+write access (`user root;`), `docker_max_object_size` must be raised from the `64k` default, and `angie.network` is case-sensitive.

## 4. Site templates

### 4.1 App proxy site (`Kind=AppProxy`)

```nginx
server {
    listen 80;
    server_name {{hosts}};
    {{if tls}}return 308 https://$host$request_uri;{{else}}include /etc/angie/nd/snippets/app-body-{{id}}.conf;{{end}}
}
{{if tls}}
server {
    listen 443 ssl;
    listen 443 quic;                           # optional, from settings. Never add `reuseport` per site (duplicate listen options error)
    http2 on;
    server_name {{hosts}};
    {{if auto}}acme nd_app_1_dom_2;  ssl_certificate $acme_cert_nd_app_1_dom_2; ssl_certificate_key $acme_cert_key_nd_app_1_dom_2;   # one acme_client per site, [A-Za-z0-9_] only
    {{else}}ssl_certificate /etc/nd-pki/sites/{{id}}.crt; ssl_certificate_key /etc/nd-pki/sites/{{id}}.key;{{end}}
    status_zone {{id}};                        # per-site stats for /status/ and Prometheus
    include /etc/angie/nd/snippets/security-headers.conf;
    {{range paths}}
    {{if static}}location ^~ {{.Prefix}}/ { alias {{.Root}}/; include /etc/angie/nd/snippets/hide.conf; }   # only StaticDir routes
    {{else}}location {{.Prefix}} { proxy_pass http://{{upstream}}; include /etc/angie/nd/snippets/proxy.conf; }{{end}}   # only Proxy routes
    {{end}}
    location / { proxy_pass http://{{upstream}}; include /etc/angie/nd/snippets/proxy.conf; }
}
{{end}}
```

`snippets/proxy.conf`:

```nginx
proxy_http_version 1.1;
proxy_set_header Host $host;
proxy_set_header Upgrade $http_upgrade;
proxy_set_header Connection $nd_connection_upgrade;   # "" for keepalive, "upgrade" for websockets
proxy_set_header X-Real-IP $remote_addr;
proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;   # untrusted sites: $remote_addr (strip spoofed chain)
proxy_set_header X-Forwarded-Proto $scheme;
proxy_read_timeout 1h;
proxy_buffering off;                  # streaming parity with Caddy (flush); revisit per site
```

### 4.2 PHP / static site

```nginx
server {
    ... listen/tls as above ...
    root {{root}};
    index index.php index.html;
    include /etc/angie/nd/snippets/hide.conf;          # .git .env* *.sql *.db *.sqlite *.key *.sock
    {{security: allow/deny, auth_basic, xmlrpc, uploads php, scanner UA, hsts, frame}}   # these regex locations MUST precede `location ~ \.php$` (first match wins) or /uploads/shell.php executes
    {{routing rules in order}}
    location / { try_files $uri $uri/ {{fallback: /index.php?$query_string | =404 | custom}}; }
    location ~ \.php$ {
        try_files $uri =404;
        fastcgi_pass unix:/data/workspaces/.nd-php/u{{owner}}-v{{ver}}/run/{{app}}.sock;
        include fastcgi_params;
        fastcgi_param SCRIPT_FILENAME $document_root$fastcgi_script_name;
    }
    # a child add_header drops every parent add_header: re-include security-headers.conf here, and omit `expires` when setting max-age manually (duplicate Cache-Control)
    location ~* \.(ico|gif|jpe?g|png|svg|woff2?|webp|avif|ttf|eot)$ { include /etc/angie/nd/snippets/security-headers.conf; add_header Cache-Control "public, max-age=2592000, immutable"; }
}
```

WordPress sites also render the `@wp_cache` disk-cache fast path (7 plugin paths) with `map` + `try_files`, matching `php_labels.go`.

Routing rule translation (`RoutingRule`):

| Kind / match | Angie |
|---|---|
| rewrite exact | `location = /p { rewrite ^ {{target}} last; }` |
| rewrite prefix | `rewrite ^/p(.*)$ {{target}}$1 last;` |
| rewrite regex | `rewrite {{pattern}} {{target}} last;` (RE2 → PCRE: validate with `angie -t`; reject lookarounds not allowed by RE2 anyway) |
| redir | `return {{status}} {{target}}{{preserve_query? $is_args$args}};` |
| block | `return {{status}};` |
| unless_exists | wrap: `if (!-e $request_filename) { rewrite … last; }` |

Basic auth: render `htpasswd/<site>` with the stored bcrypt hash. Verified live: the Alpine image accepts `$2y$`, `$2b$` and `$2a$` in `auth_basic_user_file`, so no Debian image is needed.

### 4.3 Redirect and external proxy routes

```nginx
server { listen 80; server_name {{hosts}}; return {{code}} {{target}}{{keep_path? $request_uri}}; }
server { ... tls ...; location / { set $nd_t {{target}}; proxy_pass $nd_t; proxy_ssl_verify {{!skip}}; proxy_ssl_trusted_certificate /etc/ssl/certs/ca-certificates.crt; proxy_ssl_server_name on; include …/proxy.conf; } }
```

`proxy_ssl_verify on` requires `proxy_ssl_trusted_certificate` (Angie does not load system roots on its own; the image has them at `/etc/ssl/certs/ca-certificates.crt`). A variable in `proxy_pass` has nginx variable-URI behavior if the target contains a path.

Using a variable in `proxy_pass` together with `resolver` makes hostname targets re-resolve without a reload.

## 5. TLS

- **ACME (`TLSMode=auto`)**: Angie built-in ACME client (verified syntax: `acme_client <name> <uri> [email=…] [account_key=<file>] …;` and `acme <name>;` per server, with `$acme_cert_<name>` / `$acme_cert_key_<name>`). **One `acme_client` per site**, because each client issues a single certificate covering every `server_name` that references it. Names must match `[A-Za-z0-9_]+` (a `-` breaks the variable name), and every client shares `account_key=/var/lib/angie/acme/nd_account.key`. A server with `acme` needs a valid `server_name` (an empty one fails `angie -t`); wildcards need `challenge=dns`. Persist `/var/lib/angie/acme`. Angie answers `/.well-known/acme-challenge/` before location selection, so the port-80 `return 308` does not block HTTP-01 (`[UNVERIFIED]` against live Let's Encrypt staging).
- **Internal (`TLSMode=internal`)**: `internal/proxy/pki` issues per-site leaf certs from the NextDeploy CA (ECDSA P-256, 90-day lifetime, renewed at 30 days, written to `/data/proxy/pki/sites/<id>.{crt,key}`). Then reload.
- **Shield origin**: `ReadOriginCaddyRootCA` → generalize to `ReadOriginRootCA`. Return the ND CA PEM when the driver is not Caddy. nd-edge already fetches `ca_pem` from `/edge/routes` (`origin.ca_pem`), so no edge change is needed.
- **Cert migration from Caddy** (06): import existing Caddy-issued certs from `caddy_data` (`/data/caddy/certificates/<issuer>/<host>/<host>.{crt,key}`) as manual certs until Angie's ACME takes over. This avoids Let's Encrypt rate limits on a switch.

## 6. Operations

- Status: `GET /status/` via an `api /status/;` location bound to `127.0.0.1:2019` inside the container (read-only in OSS: writes return 405). Add `status_zone` in each `server {}`, otherwise per-site stats stay empty. Show connections, upstream peers and SSL info in the overview.
- Validate: throwaway container with the staged generation mounted (see §1). Reload: `docker exec nd-proxy angie -s reload`. Reload spawns new workers. Old workers drain up to `worker_shutdown_timeout`. RAM temporarily rises (tested: 82 → 139 MB with 300 streams, back to 59 MB after).
- Metrics: `include /etc/angie/prometheus_all.conf;` in `http {}` and `location = /p8s { prometheus all; }` (verified live).
