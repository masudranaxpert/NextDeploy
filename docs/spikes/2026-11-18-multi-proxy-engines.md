# Technical Spike: Multi-Proxy Engine Verification (Angie 1.12.4 & Ferron 3.0.0-rc.11)

**Date:** 2026-11-18  
**Branch:** `feat/multi-proxy-drivers`  
**Scope:** External-engine deep research and live container verification for `docs/multi-proxy/{README,02-capability-matrix,03-angie-driver,04-ferron-driver,05-edge,07-testing,08-tasks}.md` and `ferron-test/`.

---

## Goal

Verify every `🔍` item, Open Question (#1–#3 and engine constraints for #5), directive syntax, runtime behavior, and package repository detail for **Angie 1.12.4** (`docker.angie.software/angie:1.12.4`) and **Ferron 3.0.0-rc.11** (`ferronserver/ferron:3.0.0-rc.11-alpine`) against official documentation, local source (`ferron-test/src/ferron-rc11`), and live Docker containers, and flag every discrepancy or gap in `docs/multi-proxy/`.

---

## Verified Facts (with Sources & Live Evidence)

### 1. Angie 1.12.4 (`docker.angie.software/angie:latest` / `:1.12.4`)

**Runtime & Build Evidence (`docker run --rm docker.angie.software/angie:latest angie -V`):**
- Version: `Angie/1.12.4` (`nginx/1.31.2`, OpenSSL 3.5.9 / 3.5.8, Alpine Linux `3.24.4`).
- Registry manifests verified via `docker manifest inspect`:
  - `docker.angie.software/angie:1.12.4` and `:1.12.4-alpine` share amd64 digest `sha256:0f1f3896bf34177db069bb7931a01dc0e85970d7867b8c1ebb93460161a5bd4c`.
  - `docker.angie.software/angie:1.12.4-debian` exists (`amd64` digest `sha256:2bccafa2f0f097c9ccc0c68ad877f39188f3739d23407574eeb8b55cf09aff19`).
- Built-in static modules confirmed in `angie -V`: `--with-http_acme_module`, `--with-http_realip_module`, `--with-http_v2_module`, `--with-http_v3_module`, `--with-stream`, `--with-stream_acme_module`, `--with-stream_realip_module`, `--with-stream_ssl_module`, `--with-stream_ssl_preread_module`, `--http-acme-client-path=/var/lib/angie/acme`.
- Dynamic modules present in `/usr/lib/angie/modules/`: includes `ngx_http_zstd_filter_module.so`, `ngx_http_zstd_static_module.so`, `ngx_http_brotli_filter_module.so`, `ngx_http_headers_more_filter_module.so`, etc.
- Default `/etc/angie/angie.conf` runs `user angie;` and `worker_rlimit_nofile 65536;`, `worker_connections 65536;`, and loads no dynamic modules unless `load_module` is added.

#### 1.1 `http_acme` (`acme_client`, `acme`, `$acme_cert_*`, `$acme_cert_key_*`)
Verified via official docs (`https://en.angie.software/angie/docs/configuration/modules/http/http_acme/`) and live `angie -t`:
- **Syntax:**
  ```nginx
  acme_client <name> <uri> [enabled=on|off] [key_type=ecdsa|rsa] [key_bits=256|2048] [email=<email>] [max_cert_size=<size>] [max_key_auth_size=<size>] [renew_before_expiry=<time>] [renew_on_load] [retry_after_error=off|<time>] [challenge=dns|http|alpn] [profile=<name>] [account_key=<file>] [eab=id[:alg]:key];
  ```
- **Defaults:** `enabled=on`, `key_type=ecdsa`, `key_bits=256` (ecdsa) / `2048` (rsa), `renew_before_expiry=30d`, `retry_after_error=2h`, `challenge=http`. State lives under `--http-acme-client-path=/var/lib/angie/acme/<lowercase_client_name>` (overridable via `acme_client_path`).
- **CRITICAL — One `acme_client` = One Certificate:** Per official docs: *"Each client manages a single certificate; to obtain separate certificates, configure multiple `acme_client` blocks"* and *"A single certificate covers all valid domain names and IP addresses specified in the `server_name` directives of every server block that references the client with the given name."* Sharing one global `acme_client nd_le` across all sites bundles every tenant domain onto one SAN certificate (100-SAN cap, cross-tenant SAN exposure, single failing domain stalls renewal for all).
  - **Fix:** Emit one `acme_client` per site (or domain group) using `account_key=/var/lib/angie/acme/nd_account.key;` so all clients share one ACME account (tested live with `angie -t` on two sites sharing `account_key=/tmp/acct.key` — passed). Note per docs: keys are created in config order for `enabled=on` clients, so the first `acme_client` creates the shared `account_key` if missing.
- **CRITICAL — Identifier Naming (`[A-Za-z0-9_]` only):** Tested live in `angie -t`: `acme_client bad-name ...; ssl_certificate $acme_cert_bad-name;` FAILS with:
  `angie: [emerg] unknown "acme_cert_bad" variable`.
  Client names MUST match `[A-Za-z0-9_]+` (no hyphens, dots, or colons; e.g., sanitize `app:1:dom:2` -> `nd_app_1_dom_2`).
- **Empty/Missing `server_name` Fails `angie -t`:** Tested live: a `server` block with `acme <name>;` and no valid `server_name` fails `angie -t` with `[emerg] no valid domain name defined in server block ... for ACME client`. Regex `server_name` values are skipped; wildcards require `challenge=dns`.
- **`email=` parameter:** Both `email=;` (empty) and omitting `email=` pass `angie -t`; omit `email=` when unconfigured.
- **HTTP-01 challenge vs port-80 redirect (`return 308`):** Per official docs, Angie's `http_acme` intercepts `/.well-known/acme-challenge/<token>` after reading request headers, **before** normal location/rewrite selection, so `return 308 https://$host$request_uri;` in the port-80 `server` block does not block HTTP-01 (`[UNVERIFIED - live Let's Encrypt staging HTTP-01 challenge not run in isolated local spike]`).
- **Resolver behavior:** `acme_client` resolves the ACME directory via `resolver`, defaulting to `/etc/resolv.conf` if omitted. Note `127.0.0.11` is Docker's embedded DNS — do NOT emit `resolver 127.0.0.11` on host-installed `nd-edge`.
- **Cold-start variable semantics:** `$acme_cert_key_<name>` exists immediately on startup; `$acme_cert_<name>` exists only after at least one cert is obtained (`angie -t` passes before issuance).

#### 1.2 `api` OSS Status Endpoint & `prometheus_all.conf`
Verified via `https://en.angie.software/angie/docs/configuration/modules/http/http_api/` and live container (`curl` against Angie 1.12.4):
- **`api` syntax:** Context is `location` (`location /status/ { api /status/; allow 127.0.0.1; deny all; }`).
- **OSS Read-Only Verified Live:**
  - `GET http://127.0.0.1:18080/status/` returned `200 OK` JSON containing `"angie": {"version":"1.12.4", "build_time":..., "address":..., "generation":1, "load_time":...}`, `"connections"`, `"slabs"`, `"resolvers"`, `"http": {"server_zones", "caches", "limit_conns", "limit_reqs", "upstreams"}`.
  - `DELETE /status/http/server_zones/test_zone` returned `405 Method Not Allowed` (OSS accepts `GET`/`HEAD` only; write/config API is PRO-only).
  - `api_config_files` defaults to `off` (keep `off` to avoid leaking config contents).
- **`status_zone` Required for Per-Site Stats & Prometheus:** Without `status_zone <name>;` inside `server {}` (and optionally `location`), `server_zones` and per-site Prometheus metrics remain empty.
- **Prometheus (`prometheus_all.conf`):** Verified live in container:
  - In `http {}`: `include /etc/angie/prometheus_all.conf;`
  - In local server: `location = /p8s { prometheus all; }`
  - `GET /p8s` returned `200 OK` OpenMetrics text (`angie_connections_*`, `angie_http_server_zones_*`). Requires NO exposed `api` location to function.

#### 1.3 `http2`, `listen 443 quic`, `zstd`, `proxy_ssl_verify`, `add_header`, Docker Module
- **`http2 on;` and `listen 443 quic;`:** Both pass `angie -t`.
  - **CRITICAL QUIC `reuseport` trap:** Tested live in `angie -t`: putting `listen 443 quic reuseport;` in two `server {}` blocks FAILS with `[emerg] duplicate listen options for 0.0.0.0:443`. Per-site `server {}` blocks must use `listen 443 quic;` WITHOUT `reuseport` (put `reuseport` at most once on a default server block).
  - **QUIC `Alt-Svc`:** Unlike Ferron, Angie does NOT emit `Alt-Svc` automatically; it requires `add_header Alt-Svc 'h3=":443"; ma=86400';` (subject to `add_header` inheritance below).
- **CRITICAL `add_header` Override Quirk (Tested Live):** In `docker.angie.software/angie:1.12.4`, when a `server {}` sets `add_header X-Frame-Options "SAMEORIGIN" always;` and a child `location ~* \.png$` sets `expires 30d; add_header Cache-Control "public, max-age=2592000, immutable";`, `curl -sI /a.png` returned **NO `X-Frame-Options` header** AND returned **two `Cache-Control` headers** (`Cache-Control: max-age=2592000` from `expires 30d;` plus `Cache-Control: public, max-age=2592000, immutable`). Child `add_header` drops all parent `add_header` directives (including security headers, HSTS, and `Alt-Svc`).
- **Zstd (`02-capability-matrix.md:48` 🔍):** Verified live in Docker image: `ngx_http_zstd_filter_module.so` and `ngx_http_zstd_static_module.so` exist in `/usr/lib/angie/modules/`. Loading them with `load_module /usr/lib/angie/modules/ngx_http_zstd_filter_module.so;` + `zstd on; zstd_min_length 1;` produced `Content-Encoding: zstd` on `curl -H "Accept-Encoding: zstd"`. On distro packages (`nd-edge`), zstd requires the separate `angie-module-zstd` package.
- **Docker Upstream Module (`ferron-test/docs/angie-docker.md` + `angie -t`):**
  - `docker_endpoint unix:/var/run/docker.sock;` in `http {}` requires **read+write** permissions on the socket (`user root;` or adding `angie` user to the docker socket GID).
  - **`docker_max_object_size` default is `64k`** (~25 containers max; Angie docs warn if `/containers/json` exceeds `docker_max_object_size`, container discovery fails!). **Must raise to `512k` or `1m`.**
  - Labels: `angie.http.upstreams.<name>.port="<port>"` (or `angie.stream.upstreams.<name>.port="<port>"` for `stream {}` TCP) + `angie.network="<docker_network>"` (case-sensitive!).
  - **Empty upstream (app stopped):** Tested live: `upstream nd_empty { zone nd_empty 64k; keepalive 64; }` passes `angie -t` with zero servers, and `error_page 502 =503 @nd_down;` cleanly serves `503` when `no live upstreams` occurs.
- **`proxy_ssl_verify on`:** Per `http_proxy` docs and `angie -t`, `proxy_ssl_verify` defaults to `off`. Enabling `proxy_ssl_verify on;` **requires** `proxy_ssl_trusted_certificate` (default: none — Nginx/Angie do not auto-load system CAs). In `docker.angie.software/angie:1.12.4`, system roots exist at `/etc/ssl/certs/ca-certificates.crt` (`/etc/ssl/cert.pem` symlink).
- **Static `alias` + nested hide regex (`03-angie-driver.md:129`) & PHP regex order:**
  - Tested live in `angie:1.12.4`: `location ^~ /static/ { alias /tmp/www/sub/; location ~ (/\.git|/\.env) { deny all; } }` passed `angie -t` and returned `200` for `/static/test.txt` and `403` for `/static/.env` and `/static/.git/config`.
  - In PHP sites, regex `location` blocks match in **first-match config order**; `location = /xmlrpc.php`, `location ~* /uploads/.*\.php$`, and `location ~ (/\.git|/\.env|...)` **MUST precede** `location ~ \.php$` or `/uploads/shell.php` will execute PHP.

#### 1.4 Angie Alpine `musl` Bcrypt Test (`auth_basic_user_file`)
**Live Test Result in `docker.angie.software/angie:latest` (`Angie/1.12.4`, Alpine 3.24, musl libc):**
Tested with `htpasswd` hashes in `auth_basic_user_file` guarding a static file (note: `return 200` runs in rewrite phase *before* `auth_basic` in access phase, so static file content phase was used):
- Unauthenticated `GET /auth`: `401`
- `$2y$` (`htpasswd -nbB`): right=`200`, wrong=`401` (`password mismatch` in error log)
- `$2b$` (same hash with `$2b$` prefix): right=`200`, wrong=`401`
- `$2a$` (same hash with `$2a$` prefix): right=`200`, wrong=`401`
- `$apr1$` & `{SHA}`: right=`200`, wrong=`401`
**Verdict:** Alpine `1.12.4` musl `crypt()` **natively supports `$2y$`, `$2b$`, and `$2a$` bcrypt hashes**. Switching to `:1.12.4-debian` for bcrypt is NOT required.

#### 1.5 Official Angie Package Repos & Signing Keys (`nd edge install --proxy angie`)
Verified verbatim from `https://en.angie.software/angie/docs/installation/oss_packages/` AND live `HEAD` requests:

| OS | Repo Entry / Command | Verified URL & Status | Signing Key |
|---|---|---|---|
| **Debian 12** (`bookworm`) | `deb [signed-by=/etc/apt/keyrings/angie-signing.gpg] https://download.angie.software/angie/debian/12 bookworm main` | `.../debian/12/dists/bookworm/Release` -> **`200 OK`** (`.../debian/dists/bookworm/Release` -> **`404`**!) | `https://angie.software/keys/angie-signing.gpg` (`200 OK`) |
| **Ubuntu 22.04** (`jammy`) | `deb [signed-by=/etc/apt/keyrings/angie-signing.gpg] https://download.angie.software/angie/ubuntu/22.04 jammy main` | `.../ubuntu/22.04/dists/jammy/Release` -> **`200 OK`** (`.../ubuntu/dists/jammy/Release` -> **`404`**!) | `https://angie.software/keys/angie-signing.gpg` (`200 OK`) |
| **Ubuntu 24.04** (`noble`) | `deb [signed-by=/etc/apt/keyrings/angie-signing.gpg] https://download.angie.software/angie/ubuntu/24.04 noble main` | `.../ubuntu/24.04/dists/noble/Release` -> **`200 OK`** (`.../ubuntu/dists/noble/Release` -> **`404`**!) | `https://angie.software/keys/angie-signing.gpg` (`200 OK`) |
| **Alpine** (3.21–3.24) | `https://download.angie.software/angie/alpine/v<X.Y>/main` | `.../alpine/v3.21/main/x86_64/APKINDEX.tar.gz` -> **`200 OK`** | `https://angie.software/keys/angie-signing.rsa` (`200 OK` -> `/etc/apk/keys/angie-signing.rsa`) |

- Note: Official docs use `https://download.en.angie.software/angie/$ID/$VERSION_ID $VERSION_CODENAME main` (`download.en.angie.software` also returns `200 OK`); **both** require `$ID/$VERSION_ID` for Debian/Ubuntu (without `$VERSION_ID`, APT fails with `404`), and Alpine uses `.rsa` (`angie-signing.rsa`), NOT `.gpg`.

---

### 2. Ferron 3.0.0-rc.11 (`ferronserver/ferron:3-alpine` + `ferron-test/src/ferron-rc11`)

**Runtime & Build Evidence (`ferron version` in `ferronserver/ferron:3-alpine`):**
- Version: `Ferron 3.0.0-rc.11` (`Compiled on: 2026-11-08`, `x86_64-unknown-linux-musl`, Alpine `3.24.4`).
- `docker manifest inspect ferronserver/ferron:3.0.0-rc.11-alpine`: exists (`amd64` `sha256:aa3033c193...`).
- Image `Entrypoint: null`, `Cmd: ["/usr/local/bin/ferron","run","-c","/etc/ferron/ferron.conf"]`, `User: "nobody"`.
- Upstream check (`api.github.com/repos/ferronweb/ferron`): `3.0.0-rc.11` (published `2026-11-08`) is still the latest release; issue **`ferronweb/ferron#996`** (*"Primary runtime thread busy-loops on read() returning EAGAIN after a configuration reload"*) remains **`open`** (created `2026-11-04`, updated `2026-11-10`).
- `modules/http-server/src/server/native_sockets.rs:97-114` (`build_http2_options`): still never sets `.idle_timeout(..)` on `Http2Options`.

#### 2.1 Multi-Host Block Syntax (`02-capability-matrix.md:11` 🔍)
Tested live via `ferron validate` and `ferron adapt`:
- `example.com, www.example.com { ... }` and `example.com www.example.com { ... }` produce **identical** adapted JSON (`"port": null`, two host entries: `"example.com"`, `"www.example.com"`).
- `http example.com:80, www.example.com:80 { ... }` produces `"port": 80` for both hosts under `"http"`.

#### 2.2 `watch=1` Behavior on `include "/etc/ferron/nd/sites/*.conf"` (Open Question #3)
**Verified in Source (`modules/config-ferronconf/src/{lib.rs:44-75, translation.rs:183-209, watcher.rs:58-60}`) AND Live Container:**
- `load_top_level_statements` expands globs at load time into `loaded_files`, and `FerronConfConfigurationWatcher::new(&loaded_files)` calls `debouncer.watch(file, RecursiveMode::NonRecursive)` **only on existing `loaded_files` — NOT on the parent directory**.
- **Live test results** (`ferron run --config-params "watch=1;file=/tmp/ferron/ferron.conf" --config-adapter ferronconf` with `include "/tmp/ferron/nd/sites/*.conf"`):
  1. In-place edit of existing `site1.conf` (`sed -i`): **reloaded** (`active_generation`: `0 -> 1`).
  2. Atomic rename over existing `site1.conf` (`mv tmp site1.conf`): **reloaded** (`active_generation`: `1 -> 2`).
  3. **Creating a NEW file `site2.conf` in `/tmp/ferron/nd/sites/`:** **NOT detected** (`active_generation` stayed `2`, requests to `Host: site2.local` returned `NOT_LOADED`!).
  4. `touch /tmp/ferron/ferron.conf`: **reloaded** (`active_generation`: `2 -> 3`, `site2.local` loaded).
  5. `POST /reload` with Bearer token: **reloaded** (`active_generation`: `3 -> 4`).
- **Trap with bind-mounting `ferron.conf` as a single file (`04-ferron-driver.md:32`):** If `ferron.conf` is bind-mounted as a file (`ferron.conf:/etc/ferron/ferron.conf:ro`), atomic rename on the host replaces the host inode while the container sees the old inode forever. Also, keeping `watch=1` enabled during multi-file writes risks premature partial reloads before `ferron validate` completes.
- **Recommendation:** Omit `watch=1` (or do not rely on it) and explicitly call `POST http://127.0.0.1:8081/reload`, then verify `GET /reload` (`last_reload_error == null` and `active_generation` incremented). Bind-mount the directory rather than individual files.

#### 2.3 Admin API & `auth_token` Env Interpolation
Verified in `modules/admin-api/src/{config.rs,validator.rs,server/router.rs,handlers/status.rs}` and live container:
- Endpoints: `GET /health`, `GET /status`, `GET /config`, `POST /reload`, `GET /reload`, `GET /runtime` (all default enabled when `admin {}` block exists).
- **`auth_token "{{env.ND_FERRON_ADMIN_TOKEN}}"`:** `core/src/config/mod.rs:308-320` resolves `{{env.*}}` via `std::env::var`. Tested live: `GET /health` returned `200 OK` without auth (always exempt in `router.rs:32`); `GET /status` returned `401 Unauthorized` without token and `200 OK` JSON with `Authorization: Bearer <token>`.
- **Caveat:** If `ND_FERRON_ADMIN_TOKEN` is unset in the container env, Ferron keeps the literal string `"{{env.ND_FERRON_ADMIN_TOKEN}}"` as the active token instead of failing startup.
- **Reload behavior:** Ferron shuts down and restarts the admin API HTTP listener on every reload (`[INFO] Admin API shutting down (reload)`).

#### 2.4 `client_ip_from_header` + `trusted_proxy` + `trusted_strict` (`02-capability-matrix.md:36-37` 🔍)
**CRITICAL SECURITY FINDING — Verified in `types/http/src/client_ip.rs:106-113, 234-237`:**
- Only `x-forwarded-for` and `forwarded` are accepted (`ClientIpHeader::from_str("cf-connecting-ip")` and `"x-real-ip"` both return `None`).
- `extract_x_forwarded_for` runs:
  ```rust
  let first = value.split(',').next()?.trim();
  first.parse::<IpAddr>().ok()
  ```
  and `stages/client_ip.rs:198-209` unit-tests that `"192.0.2.1, 10.0.0.1, 172.16.0.1"` returns **`192.0.2.1` (the LEFTMOST IP)**!
- **Verdict:** **`trusted_strict` (right-to-left parsing) is NOT SUPPORTED (`❌`) in Ferron 3.0.0-rc.11.** Behind a proxy that appends to a client-supplied `X-Forwarded-For` header (instead of overwriting it), Ferron trusts the spoofable leftmost IP whenever the connecting peer matches `trusted_proxy`.

#### 2.5 `on_demand` / `on_demand_ask`, `disable_symlinks`, `fcgi_php`, `basic_auth`, `require_target_file`, `status` Redirects, `tcp *:port`, CLI
- **`on_demand` / `on_demand_ask`:** Verified in `ferron-test/docs/v3/configuration__security__acme.md` and `ferron validate`:
  ```ferron
  tls {
      provider acme
      challenge http-01
      contact admin@example.com
      cache /var/cache/ferron-acme
      on_demand
      on_demand_ask "https://panel.local/api/v1/proxy/tls-ask"
  }
  ```
  Ferron sends `GET <url>?domain=<sni>` and requires `200` to issue (`on_demand_ask_auth`, `on_demand_ask_no_verification` also exist).
- **`disable_symlinks false`:** Live-tested (`ln -s /tmp/target/real.txt /tmp/www/link.txt`): default (`true`) returned `403 Forbidden`; `disable_symlinks false` returned `200 OK` (`secret-file`).
- **`fcgi_php`:** Verified via `docs/v3/configuration__content__fastcgi.md` and `ferron validate`: `fcgi_php unix:///run/nd-php/<app>.sock` (or block form with `keepalive; limit N;`).
- **`basic_auth` bcrypt:** Live-tested in `ferronserver/ferron:3-alpine`: `$2y$` and `$2b$` both returned `200 OK` on valid password, `401 Unauthorized` on wrong password / missing header. Note: `brute_force_protection` defaults to enabled (`max_attempts 5`, `window 5m`, `lockout_duration 15m`), and `-fips` images reject bcrypt.
- **`require_target_file` (`02-capability-matrix.md:58` 🔍):** Verified in `modules/http-rewrite/src/config.rs:274-283` and `ferron directives`: **`❌` NOT SUPPORTED**. `rewrite` supports only `file false` and `directory false` (skipping when the *current* request path is a file/directory), and cannot test existence of a transformed target path like `$uri.php`.
- **CRITICAL — `status 3xx` Redirect Query Preservation (`02-capability-matrix.md:55,68`):** Verified in `modules/http-response/src/stage.rs:122-129,228-235` AND live raw TCP tests:
  - `status` runs `regex.replace(request_path, location)` against **`uri.path()` only** (drops query string!).
  - `location` interpolates `{{...}}` **before** `regex.replace`! Live test of `regex r"^/rkeep/(.*)"` + `location "https://dest.example.com/v2/$1{{request.uri.query}}"` on `/rkeep/foo?a=1&b=2` produced `location: https://dest.example.com/v2/=1&b=2` (because `$1a` was parsed as capture group `1a`!).
  - Live test of `url /keep` + `location "https://dest.example.com{{request.uri}}"` produced `location: https://dest.example.com/keep?a=1&b=2`.
  - **Fix if used:** Use `${1}` delimited group + a `match` checking `request.uri.query != ""` for conditional `?{{request.uri.query}}`, or `{{request.uri}}` when path is preserved unmodified.
- **CRITICAL — `tcp *:port` Phantom Protocol (`08-tasks.md` Q#5):** Verified live in `ferronserver/ferron:3-alpine`:
  - `ferron validate` on `tcp *:19000 { proxy tcp://127.0.0.1:18090 }` exited `0` (`{"valid": true}`), because `ferron validate` skips protocols with no registered validator (`http` is the only server module in `ferron-rc11/modules/`!).
  - When `ferron run` started with that config, **`/proc/net/tcp` showed port `19000` was NOT listening** — `tcp` blocks are silently ignored!
  - HTTP gateway ports (`*:18095 { tls false; ... }`) DO listen and work (`200 OK gw-ok`).
- **`match` AND-only semantics (`04-ferron-driver.md:88`):** `modules/http-server/src/config/resolver/matcher.rs:66-70` evaluates all expressions in a `match` block with **AND** logic. For multiple static paths, `match nd_any_static` must use a single combined regex (e.g. `r"^(?:/assets/|/media/)"`) or nested `if_not` blocks.
- **Semicolons:** Verified via `ferron validate --json`: single-line `{ key val; key2 val2 }` syntax IS accepted by `ferronconf`.
- **`ferron validate` CLI flags:** `-c, --config <CONFIG_PATH>`, `--config-params <CONFIG_PARAMS>`, `--config-adapter <CONFIG_ADAPTER>`, `-j, --json`. Exit code `0` for valid or `Unknown directive` warnings; exit `1` for `Invalid configuration` or parse errors. Non-`.conf`/`.ferron`/`.json` paths require `--config-adapter ferronconf`.

---

### 3. `ferron-test/` Artifact Audit
- `ferron-test/results-angie.txt`, `results-suite.txt`, `results-suite2.txt`, `results-timeouts.txt`: confirm Angie (`12,258 req/s` with `keepalive 64`, `~40 KB/conn` at 20k, `127 MB` after close, `75 s` idle-h2, `2` transient `502`s in `0..5s` on `dnsrestart`, `0` after) and Ferron (`15,441 req/s`, `51 MB` after close, `>7m0s` idle-h2 and slowread, stays `503` forever on `dnsrestart` without `logical_dns true`).
- `ferron-test/probe/main.go`: `slowread` waits until `SO_RCVBUF` + server write buffers fill before `send_timeout 60s` triggers (~`347s` total on Angie/nginx/Caddy; never on Ferron).
- **Bug found in `ferron-test/docker/reload-results.txt:19-66`:** `reload-test.sh` ran without `MSYS_NO_PATHCONV=1`, so Git Bash rewrote `/etc/ferron/conf.d/site.conf` to `C:/Program Files/Git/etc/ferron/conf.d/site.conf`, meaning `sed -i` failed on every iteration (`X-Gen` stayed `"0"` in all modes!). When porting `ferron-test/` to `tests/proxy-suite/` (`07-testing.md`), ensure `MSYS_NO_PATHCONV=1` (as in `suite/lib.sh:2`) and fix path quoting.

---

## Plan Corrections & Discrepancies Found (`docs/multi-proxy/`)

| Doc & Location | Issue in Plan | Verified Correction Needed |
|---|---|---|
| `03-angie-driver.md:83,125` & `05-edge.md:52` | Single shared `acme_client nd_le` used by all sites | **Critical:** One `acme_client` issues ONE shared SAN cert for all referencing `server_name`s. Render **one `acme_client` per site** (`[A-Za-z0-9_]` only, no `-`/`:`/`.`) sharing `account_key=/var/lib/angie/acme/nd_account.key;`. |
| `03-angie-driver.md:15` vs `03-angie-driver.md:102` | Compose defines `networks: [nextdeploy]` (lowercase) while label emits `angie.network: "NextDeploy"` | Align network casing to match actual NextDeploy Docker network name (`angie.network` is case-sensitive). |
| `03-angie-driver.md:66` | Missing `docker_max_object_size` | Default `64k` breaks above ~25 containers. Set `docker_max_object_size 512k;` (or `1m;`). |
| `03-angie-driver.md:20-21, 42-43, 204` | Bind-mounts `conf/` and `angie.conf` individually, while staging is at `.staging-<gen>/` outside `conf/` | `docker exec nd-proxy angie -t` cannot see sibling `.staging-<gen>/`, and single-file bind-mounts pin the old inode across host atomic renames. Mount `/data/proxy/angie:/etc/angie/nd-root` (or stage inside `conf/.staging/` + test via throwaway `docker run` or `-c` staged path). |
| `03-angie-driver.md:122` | `listen 443 quic;` notes | Never add `reuseport` to per-site `server {}` blocks (`duplicate listen options` error); add `Alt-Svc` header if H3 advertisement is wanted (remembering `add_header` override quirk). |
| `03-angie-driver.md:129-130` | `{{range paths}}` shows both `alias` and `proxy_pass` inside loop | Render `location ^~ {{.Prefix}}/ { alias ... }` ONLY for `StaticDir` routes and `location {{.Prefix}} { proxy_pass ... }` ONLY for `Proxy` routes (not both for the same prefix). |
| `03-angie-driver.md:158-168` | PHP/static regex `location` order + `add_header` drop | Ensure `location = /xmlrpc.php`, `location ~* /uploads/.*\.php$`, and `hide.conf` precede `location ~ \.php$`. Also re-include `security-headers.conf` inside `location ~* \.(ico|...)$` because `add_header Cache-Control` drops `server`-level `add_header`s (and omit `expires 30d;` if setting `max-age` manually to avoid duplicate `Cache-Control`). |
| `03-angie-driver.md:189` | `proxy_ssl_verify {{!skip}}` without CA path | When `proxy_ssl_verify on;`, Angie requires `proxy_ssl_trusted_certificate /etc/ssl/certs/ca-certificates.crt;` (`proxy_ssl_verify_depth 2;`). Also note `proxy_pass $nd_t;` with a variable has nginx variable-URI behavior if `target` contains a path component. |
| `03-angie-driver.md:114-169, 203` | Missing `status_zone` | Add `status_zone <site_id>;` inside each `server {}` so `/status/` and Prometheus `/p8s` report per-site metrics. |
| `05-edge.md:50-65` | Host-based `nd-edge` differences | Do NOT emit `resolver 127.0.0.11` (Docker-only) on host `nd-edge`. Note `angie-module-zstd` is a separate optional package if zstd is ever enabled. |
| `05-edge.md:77` & `08-tasks.md:38` | `download.angie.software` repo details | Use `https://download.angie.software/angie/$ID/$VERSION_ID $VERSION_CODENAME main` (`debian/12`, `ubuntu/22.04`, `ubuntu/24.04` — omitting `$VERSION_ID` returns `404`). For Alpine, use `.rsa` key (`https://angie.software/keys/angie-signing.rsa` -> `/etc/apk/keys/`). |
| `04-ferron-driver.md:32, 123` | `watch=1` on `include` + single-file `ferron.conf` mount | `watch=1` **does NOT detect newly added files** in `include ".../*.conf"`, and single-file bind-mounts break inode swaps. Disable `watch=1` and use explicit `POST /reload` + `GET /reload` check; mount parent dir instead of `ferron.conf` file. |
| `04-ferron-driver.md:51` | `auth_token "{{env.ND_FERRON_ADMIN_TOKEN}}"` | Works if env var is set; if unset, literal `"{{env.ND_FERRON_ADMIN_TOKEN}}"` becomes the password. Ensure compose always sets a non-empty secret. |
| `04-ferron-driver.md:88` | `if_not nd_any_static` | `match` blocks are AND-only; build one combined regex `r"^(?:/p1/|/p2/)"` for `nd_any_static` or nest `if_not`. |
| `02-capability-matrix.md:11` | `d, www.d 🔍` | Confirmed ✅ (`d, www.d` and `d www.d` are identical). |
| `02-capability-matrix.md:37` | `trusted_strict (right-to-left) 🔍` | **Mark `❌` for Ferron.** Ferron always reads the leftmost `X-Forwarded-For` IP (and rejects `CF-Connecting-IP`). |
| `02-capability-matrix.md:48` | `zstd via module 🔍` | Confirmed `🟡` (`ngx_http_zstd_filter_module.so` bundled in Docker image; separate package `angie-module-zstd` on host). |
| `02-capability-matrix.md:51` | Angie Alpine bcrypt `🔍` | Confirmed `✅` (`$2y$`, `$2b$`, `$2a$` all verified live on Alpine 1.12.4). |
| `02-capability-matrix.md:55,68` | Ferron `status` redirect `preserve_query` / `keep_path` | Mark `🟡`: `status` ignores query string unless `${1}` + conditional `match` on `request.uri.query != ""` (or `{{request.uri}}`) is used. |
| `02-capability-matrix.md:58` | Ferron `require_target_file 🔍` | Confirmed `❌` (no arbitrary target file-exists check in Ferron). |
| `08-tasks.md:75` | `Ferron needs tcp *:port blocks` | **False in Ferron 3.0.0-rc.11:** `tcp *:port` passes `ferron validate` (`valid: true`) but has **no runtime TCP module** and never listens. Only HTTP gateway ports (`*:<port>`) work on Ferron. |

---

## Community / Upstream Gotchas & Unverified Items

- **Ferron `#996`:** Still `open` as of `2026-11-10`; HTTP/2 idle timeout (`native_sockets.rs`) and write timeout remain unimplemented in `3.0.0-rc.11`. Keep Experimental gate.
- **`[UNVERIFIED - requires live staging domain/DNS]`:** Live ACME certificate issuance (`http-01` behind `return 308`, `dns-01`, Ferron `on_demand_ask`) against Let's Encrypt staging was not run in local isolated Docker (no public DNS routing to localhost).
- **`[UNVERIFIED - requires live Debian/Ubuntu/Alpine VM]`:** Full `apt-get install angie` / `apk add angie` and systemd unit file contents inside Debian 12 / Ubuntu 22.04 / 24.04 / Alpine VMs were verified via repository `Release`/`APKINDEX` `HEAD` (`200 OK`) and official docs rather than a full VM package install.
- **`[UNVERIFIED - requires live Docker socket event test]`:** Live container recreate with `angie.http.upstreams.*` + `angie.network` was verified previously in `ferron-test/results-angie.txt` (`dnsrestart`), not re-run in this spike; `angie -t` confirmed directive syntax.

---

## Options & Recommendation

1. **Angie ACME topology:** Use **per-site `acme_client`** names (`[A-Za-z0-9_]+`) with a shared `account_key=/var/lib/angie/acme/nd_account.key`, rather than one global `acme_client nd_le`.
2. **Angie Docker image:** Stay on **`docker.angie.software/angie:1.12.4`** (Alpine) — bcrypt (`$2y$`, `$2b$`, `$2a$`) and zstd modules both work out of the box; no need for `-debian`.
3. **Ferron reload mechanism:** Do **not** rely on `watch=1` for `include "/etc/ferron/nd/sites/*.conf"`. Use explicit `POST /reload` + `GET /reload` generation/error verification, mount parent config directory (not `ferron.conf` as a single file), and disable `watch=1` to avoid mid-write partial reloads.
4. **Preflight gates to enforce:** Block Ferron on `require_target_file`, `trusted_strict` (or warn on spoofable XFF), raw TCP gateway ports (if any exist — deferring port-type audit to backend auditor), and require explicit `proxy_experimental_ack`.

## Unverified / Next Steps

- [ ] Backend auditor to resolve Plan Open Questions #4 (`"caddy"` vs `"nd-proxy"` container name refs) and #5 (whether `MergeCaddyGatewayPorts` carries HTTP-only or raw TCP ports — note Ferron cannot do raw TCP).
- [ ] Phase 1.10 / Phase 2.3: run live Let's Encrypt staging test on Angie `http_acme` and live `apt`/`systemd` install verification on Debian 12 + Ubuntu 22.04/24.04 VMs.
