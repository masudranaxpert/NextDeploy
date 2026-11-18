# Technical Spike Brief: Deep-Dive PHP & `.htaccess` Hosting on Angie 1.12.4 and Ferron 3.0.0-rc.11

**Date:** 2026-11-18  
**Author:** `researcher-proxy` (Mode 4 Research Spike)  
**Target Branch / Plan Docs:** `feat/multi-proxy-drivers` (`docs/multi-proxy/{02-capability-matrix,03-angie-driver,04-ferron-driver,08-tasks}.md`)  
**Live Images Tested:**
- `php:8.3-fpm-alpine` (PHP `8.3.36`)
- `docker.angie.software/angie:1.12.4` (`Angie/1.12.4`, Alpine 3.24)
- `ferronserver/ferron:3-alpine` (`Ferron 3.0.0-rc.11`, Alpine)
- `curlimages/curl:latest` + `alpine:3.20` (probe client & compression fixture prep)

> All Docker test containers, volumes, and networks (`nd-live-*`) were removed after verification (`docker ps -a --filter "name=nd-live"` empty).  
> Every directive and edge case below was verified against NextDeploy's Go codebase, Ferron's Rust source (`ferron-test/src/ferron-rc11`), Ferron/Angie official documentation (`https://r.jina.ai/...` + `ferron-test/docs/v3/`), and live PHP-FPM Docker containers.

---

## 1. Executive Summary & Critical Findings

Implementing NextDeploy's PHP/`.htaccess` feature set (`internal/caddy/php_labels.go`, `internal/caddy/php_routing.go`, `internal/phphosting/*`, `internal/htaccess/*`) on Angie and Ferron exposes **8 critical plan corrections / production hazards** that would break WordPress, Laravel, security isolation, or file uploads if the existing plan drafts (`02`, `03`, `04`) were implemented literally:

| # | Area | Severity | Verified Finding | Required Plan / Code Fix |
|---|------|----------|------------------|--------------------------|
| 1 | **FPM Socket Path & `0660` Perms** | **CRITICAL** | `internal/phphosting/pool.go:37-39,81-84` puts sockets at `/data/workspaces/.nd-php/u<ownerID>-v<ver>/run/<appID>.sock` (NOT `/run/nd-php/` as `03`/`04` claim) with `user = <sp.UID>`, `group = <sp.UID>`, `listen.mode = 0660` (no `listen.owner`/`listen.group`). PHP-FPM master runs as root -> live socket is `srw-rw---- 1 root root`. Default `user angie;` (`uid=100`) returns **`502 Bad Gateway` (Permission denied)**; default Ferron `nobody` (`uid=65534`) returns **`500 Internal Server Error`**. | Mount `/data/workspaces/.nd-php` (with sockets reachable) at the real path. Set `user root;` in `angie.conf` AND `user: "0:0"` in Ferron compose (or update `pool.go` to `listen.mode = 0666` / shared group). |
| 2 | **Ferron Catch-All Rewrite Wipes `$_GET` & `PATH_INFO`** | **CRITICAL** | Both `02-capability-matrix.md:46` and Ferron's own `https://ferron.sh/docs/migration/from-htaccess` write `rewrite r"^(.*)$" "/index.php" { file false; directory false; }`. Source (`modules/http-rewrite/src/lib.rs:222-226`, `config.rs:285-289`) matches `path + "?" + query`, so `r"^(.*)$"` -> `"/index.php"` **wipes `QUERY_STRING`/`$_GET` (`[]`) and `PATH_INFO` (`""`)**, proved live in test `(b0)` & `(f1)`. | Use `rewrite r"^[^?]+" "/index.php" { file false; directory false; last; }` wrapped with all site `rewrite` rules inside `if_not IS_PHP_PATHINFO` (`request.uri.path ~ r"\.php/"`). Verified live in `(a2+b1)`, `(e*)`, `(f)`, `(f2)`. |
| 3 | **Ferron Layer Ordering Trap (`if`/`if_not`)** | **HIGH** | `LayeredConfiguration::get_entries("rewrite", true)` (`core/src/config/layer.rs:132`) iterates layers **in reverse (`if_not` before host block)**, and no-op `rewrite` (`old == rewritten`) ignores `last` (`modules/http-rewrite/src/config.rs:295`). Putting `if_not IS_PHP_PATHINFO { fallback }` at the host level causes the fallback to run **before** host-level `RoutingRule` rewrites! | Emit ALL `RoutingRule` rewrites AND the front-controller fallback in exact priority order **inside one `if_not IS_PHP_PATHINFO { ... }` block** (or omit wrapper if no `PATH_INFO` needed? Always wrap to keep `PATH_INFO` working). Verified live! |
| 4 | **Angie `Strict404` & `RequireTargetFile` PHP Source Leak** | **CRITICAL** | Writing `try_files $uri $uri/index.php =404;` (literal port of Caddy `{path} {path}/index.php =404`) in Angie **leaks raw PHP source (`<?php ...`, `application/octet-stream`)** on `/realdir/`, verified live in `(d6d)`! Likewise `try_files $uri $uri/ /$1.php?$args ...` checks literal `?$args` in the filename (`RequireTargetFile` failed live) and non-last `try_files` matches serve `.php` raw. | For `FallbackStrict404`: use `index index.php index.html;` + `try_files $uri $uri/ =404;`. For `FallbackCustom`: `try_files $uri $uri/ /<entry>?$args;`. For `RequireTargetFile`: `if (-f $document_root$uri.php) { rewrite ^(.+)$ $1.php last; }`! |
| 5 | **Angie `try_files` + `fastcgi_split_path_info` Wipes `PATH_INFO`** | **HIGH** | `03-angie-driver.md:162-167` lacks `fastcgi_split_path_info` (breaking `/index.php/foo/bar`). Even with `fastcgi_split_path_info`, `try_files $fastcgi_script_name =404;` resets `$fastcgi_path_info` to `""` (`PATH_INFO: ""` verified live before fix). | Capture `$fastcgi_path_info` first: `set $nd_path_info $fastcgi_path_info;` -> `try_files $fastcgi_script_name =404;` -> `fastcgi_param PATH_INFO $nd_path_info;`. Verified live in `(f-fixed)` & `(f2)`. |
| 6 | **Angie `add_header` Inheritance Drop** | **MEDIUM** | Static `location ~* \.(css\|js)$ { add_header Cache-Control ...; }` drops server-level HSTS, `X-Frame-Options`, `X-Content-Type-Options`, `Referrer-Policy`. | Use bundled `ngx_http_headers_more_filter_module.so` (`more_set_headers`, package `angie-module-headers-more`, verified live with `Cache-Control` + `Strict-Transport-Security` both sent) OR duplicate `add_header ... always;` inside every static `location`. |
| 7 | **Missing `@wp_cache` Disk Cache & Ferron Gap** | **MEDIUM** | `php_labels.go:76-90` auto-serves 7 WP disk cache paths without PHP (`<1ms`); completely omitted from `02`, `03`, `04`. Angie reproduces all 7 paths via `map` + `try_files` (verified live `(b2)`–`(b5)`). Ferron `match` has **no `-f` file-exists matcher** and `rewrite` `file` checks current path only, so `@wp_cache` disk cache probing is a Ferron capability gap. | Implement `map` + `try_files` `@wp_cache` snippet in Angie Phase 1.5; document `@wp_cache` disk-probe gap in `02-capability-matrix.md` for Ferron (PHP still works normally; `http-cache` is separate in-memory caching). |
| 8 | **Timeouts, Upload Limits, `HTTPS on`, Symlinks** | **HIGH** | NextDeploy `settings.go:33-34` supports `5G` uploads & `3600s` timeouts. Angie defaults `1m` / `60s`. Ferron has **no max-body-size directive**, defaults global `http.timeout` to `5m`, **ignores per-host `http { timeout }` on HTTP/1.1** (verified live: `1s` only killed `sleep(3)` -> `408` in global `{ http { ... } }`!), defaults `disable_symlinks true` (breaks Laravel `public/storage` -> `403` live!), and `fcgi_php` cannot set `environment "HTTPS" "on"` behind Shield. | Angie: `client_max_body_size` from site setting (or `5g;`), `fastcgi_read_timeout` >= `max_execution_time + 30s`, `fastcgi_param HTTPS on;` when HTTPS/Shield. Ferron: global `{ http { timeout "3660s"; } }`, `disable_symlinks false` (relying on `symlinks.go`), and `fcgi { ... }` block instead of `fcgi_php` to inject `keepalive true` & `environment "HTTPS" "on"`. |

---

## 2. Audit of NextDeploy's Existing PHP & `.htaccess` Implementation

Verified directly against:
- `internal/phphosting/{consts,pool,runtime,settings,security,symlinks,wordpress}.go` and `internal/phphosting/docker/{Dockerfile,nd-fpm.conf,nd-php.ini}`
- `internal/caddy/{php_labels,php_routing}.go`, `internal/proxy/routing.go`
- `internal/htaccess/{types,parser,scanner}.go`
- `internal/handlers/{php_sites,php_routing,php_security,php_data,php_backup,workspace_files}.go`

### 2.1 PHP-FPM Runtime, Socket Path & Permissions
- **Socket path (`pool.go:37-39`):** `SocketPath(ownerID, version, appID)` returns:
  `/data/workspaces/.nd-php/u<ownerID>-v<ver>/run/<appID>.sock` (inside PHP-FPM container: `/run/php/<appID>.sock`).
- **Document root (`pool.go:30-34`):** `/data/workspaces/<appID>/htdocs` (or `/data/workspaces/<appID>/htdocs/<RootDir>`).
- **Pool config (`pool.go:80-130`):**
  - `user = <sp.UID>`, `group = <sp.UID>`
  - `listen = /run/php/<appID>.sock`, **`listen.mode = 0660`** (no `listen.owner`, no `listen.group` -> created by master `root` as `root:root 0660`)
  - `security.limit_extensions = .php`, `php_admin_value[cgi.fix_pathinfo] = 1` (`nd-php.ini:3` also sets `cgi.fix_pathinfo = 1`)
  - `request_terminate_timeout = max(MaxExecutionTime + 30, 60)s`
  - `open_basedir = /data/workspaces/<appID>:/usr/local/lib/php:/dev/urandom`
- **PHP site settings ceilings (`settings.go:32-50`):**
  - Defaults: `memory_limit=256M`, `max_execution_time=60`, `max_input_time=120`, `max_input_vars=3000`, `post_max_size=64M`, `upload_max_filesize=64M`, `max_children=20`.
  - Allowed choices: `SizeChoices` up to **`5G`**, `TimeChoices` up to **`3600` seconds**.
- **Symlink policy (`symlinks.go:13-88`):** Intra-site symlinks (e.g., Laravel `public/storage -> ../storage/app/public`) are intentionally permitted; a background sweeper runs every `3s` + on `SyncRoutes` to delete symlinks whose target escapes `SitePath(appID)`.

### 2.2 Site Serving, `@wp_cache`, Compression, Headers, Fallbacks (`php_labels.go`, `php_routing.go`)
- **Compression (`php_labels.go:63,116`):** Dynamic `encode zstd gzip` + static `precompressed gzip zstd` (`.gz`, `.zst` sidecars).
- **Default security & cache headers (`php_labels.go:64-69,153-158`):**
  - Always: `X-Content-Type-Options: nosniff`, `Referrer-Policy: strict-origin-when-cross-origin`
  - Media (`*.ico *.gif *.jpg *.jpeg *.png *.svg *.woff *.woff2 *.webp *.avif *.ttf *.eot`): `Cache-Control: public, max-age=2592000, immutable`
  - Code (`*.css *.js`): `Cache-Control: public, max-age=604800, stale-while-revalidate=86400`
  - Optional: `HSTS` (`Strict-Transport-Security: "max-age=31536000; includeSubDomains"`), `FrameProtection` (`X-Frame-Options: SAMEORIGIN`)
- **Hidden files (`php_labels.go:72-73,118`):**
  - Regex `404`: `(^|/)\.(git|svn|hg|env|nextdeploy-db|ht[a-z]*)(/|$|\.)` (keeps `/.well-known` reachable)
  - `file_server.hide`: `.git .env* *.sql *.db *.sqlite *.key *.sock`
- **Security filters (`php_labels.go:122-158`):**
  - `BlockedIPs` -> `403`; `AllowOnly` (`!client_ip`) -> `403`
  - `BlockScanners` -> User-Agent regex `(?i)(sqlmap|nikto|nmap|masscan|acunetix|nessus|openvas|wpscan|dirbuster|gobuster|havij|zgrab)` -> `403`
  - `BlockXMLRPC` -> exact `/xmlrpc.php` -> `403`
  - `BlockUploadPHP` -> path regex `(?i)/(uploads|upload|files|media|wp-content/uploads)/.*\.(php|phtml|phar|php[0-9])$` -> `403`
  - `BasicAuthUser` + `BasicAuthHash` -> `golang.org/x/crypto/bcrypt` cost 10 (`php_security.go:115`, generates **`$2a$10$...`** hashes)
- **WordPress `@wp_cache` (`php_labels.go:76-90`):**
  - Bypassed when: Cookie matches `comment_author|wordpress_[a-f0-9]+|wp-postpass|wordpress_logged_in|woocommerce_items_in_cart|wp_woocommerce_session`; Path matches `(?i)(/wp-admin/|/xmlrpc.php|/wp-(app|cron|login|register|mail).php|/wp-json/|/feed/|sitemap.*.xml)`; Method == `POST`; or `{query} != ''`.
  - Tries 7 cache files in order:
    1. `/wp-content/cache/all{path}/index.html`
    2. `/wp-content/cache/supercache/{host}{uri}/index-https.html`
    3. `/wp-content/cache/supercache/{host}{uri}/index.html`
    4. `/wp-content/cache/cache-enabler/{host}{uri}/https-index.html`
    5. `/wp-content/cache/cache-enabler/{host}{uri}/index.html`
    6. `/wp-content/cache/wp-rocket/{host}{uri}/index-https.html`
    7. `/wp-content/cache/wp-rocket/{host}{uri}/index.html`
- **Fallback modes (`php_labels.go:99-115`):**
  - `FallbackDefault` (`""`): Caddy `php_fastcgi` built-in `{path} {path}/index.php index.php` with `PATH_INFO` splitting (`/index.php/foo/bar` works).
  - `FallbackStrict404` (`strict_404`): `{path} {path}/index.php =404`.
  - `FallbackCustom` (`custom`): `{path} {path}/index.php /<entry>` (or SPA `rewrite_fallback` on static sites).
- **HTTPS env & timeouts (`php_labels.go:93-109`):** `dial_timeout 5s`, `read_timeout 120s`, `write_timeout 120s`, `env HTTPS on` when `ShieldNodeID != nil || EnableHTTPS`, plus untrusted direct proxy header stripping when `!TrustedProxy && ShieldNodeID == nil`.
- **`.htaccess` Converter (`internal/htaccess/{parser,scanner}.go` + `internal/handlers/php_routing.go`):**
  - **NextDeploy already converts `.htaccess` to engine-agnostic `RoutingRule` IR** at scan/import time (`convert-htaccess` / `scan-htaccess`), supporting: `RewriteRule` (`[F]`, `[G]`, `[R=code]`, `[QSA]`, `[QSD]`, `[NC]`), `RewriteCond` (`!-f`/`!-d`, `%{REQUEST_FILENAME}.php -f`), `<Files>`/`<FilesMatch>` deny rules, `Redirect`/`RedirectMatch`, `DirectoryIndex` -> `FallbackCustom`, and subfolder `Deny from all` / `Require all denied`. Rejects PCRE lookaround/backreferences cleanly (already compatible with Rust `regex`!).

---

## 3. Official Docs & Engine Source Findings (`r.jina.ai` + `ferron-rc11`)

### 3.1 Angie `1.12.4`
Sources fetched via `https://r.jina.ai/`:
- `https://en.angie.software/angie/docs/configuration/modules/http/http_fastcgi/`
- `https://en.angie.software/angie/docs/configuration/modules/http/` (`http_core` directives)
- `https://en.angie.software/angie/docs/configuration/modules/http/http_rewrite/`
- `https://en.angie.software/angie/docs/configuration/modules/http/http_headers/`
- `https://en.angie.software/angie/docs/configuration/modules/http/http_gzip/` & `http_gzip_static/`
- `https://en.angie.software/angie/docs/configuration/modules/http/http_auth_basic/` & `http_access/`
- `https://en.angie.software/angie/docs/configuration/modules/external-modules/headers-more/` & `zstd/`
- `https://en.angie.software/angie/docs/installation/oss_packages/` & `external-modules/`

Key verified directives & semantics:
- **Bundled vs Packaged External Modules:** Live `ls /usr/lib/angie/modules` in `docker.angie.software/angie:1.12.4` confirms `ngx_http_headers_more_filter_module.so`, `ngx_http_zstd_filter_module.so`, and `ngx_http_zstd_static_module.so` are pre-installed in the Docker image. On Linux hosts (`oss_packages/` + `external-modules/`), they are separate official packages in `download.angie.software`: **`angie-module-headers-more`** (`v0.40`) and **`angie-module-zstd`** (`0.90.9`, provides both `ngx_http_zstd_filter_module.so` and `ngx_http_zstd_static_module.so`). `http_gzip` and `http_gzip_static` (`--with-http_gzip_static_module` in `angie -V`) are built directly into Angie OSS.
- **FastCGI defaults (`angie -V`, `/etc/angie/fastcgi.conf`):**
  - `fastcgi.conf` defines `SCRIPT_FILENAME $document_root$fastcgi_script_name`, `REQUEST_METHOD $upstream_request_method`, `HTTPS $https if_not_empty`, `REDIRECT_STATUS 200`.
  - Default `fastcgi_read_timeout 60s;` & `fastcgi_send_timeout 60s;`, default `client_max_body_size 1m;` (MUST be raised to match `MaxExecutionTime` / `5g` uploads!).
  - `fastcgi_split_path_info ^(.+\.php)(/.*)$;`: sets `$fastcgi_script_name` + `$fastcgi_path_info`, **but `try_files` wipes `$fastcgi_path_info`**, so `$nd_path_info` MUST be saved with `set $nd_path_info $fastcgi_path_info;` before `try_files $fastcgi_script_name =404;`.
- **`location` Precedence & Security Ordering:** `=` (exact) > `^~` (prefix stop) > first matching `~` / `~*` (regex in file order) > longest prefix. Security `location ~*` blocks (`.git`/`.env` `404`, `/uploads/*.php` `403`, `/secret/` `403`) **must appear before** `location ~ ^(.+\.php)(/.*)?$`.
- **No `.htaccess`:** Not supported; uses NextDeploy's `RoutingRule` IR.

### 3.2 Ferron `3.0.0-rc.11`
Sources verified via `https://r.jina.ai/https://ferron.sh/docs/...`, `ferron-test/docs/v3/`, `ferron directives`, and `ferron-test/src/ferron-rc11/`:
- `migration/from-htaccess` (`migration__from-htaccess.md`): Explicitly states *"Ferron deliberately does not read `.htaccess` files."* Uses Rust `regex` (linear time, no lookahead/lookbehind/backreferences — matches `internal/htaccess/parser.go` restrictions).
- **Pipeline Order (`request-pipeline`, `modules/http-response/src/stage.rs`, `modules/http-rewrite/src/lib.rs`):**
  1. Config resolution (`*` -> host -> `location` [prefix-stripped!] -> `if`/`if_not`) runs ONCE on original sanitized URL before stages.
  2. Stages: ACME -> `client_ip_from_header` -> `https_redirect` -> `status`/`abort`/`allow`/`block` (`HttpResponseStage`) & `rewrite` (`RewriteStage`, unordered wrt each other!) -> `basic_auth` -> `fcgi` (`FcgiPassStage`/`FcgiFileStage`) -> `static_file` -> post-response (`dynamic_compressed`, `header`).
  3. **Critical Layer Rule (`core/src/config/layer.rs:132`):** `get_entries("rewrite", true)` walks `layers.iter().rev()` -> `if`/`if_not` layers run **before** host-block directives! And `rewrite` no-op (`old == rewritten`) does **not** honor `last` (`modules/http-rewrite/src/config.rs:295`).
- **`fcgi` vs `fcgi_php` (`modules/http-fcgi/src/{config,lib,validator,stages/file}.rs`):**
  - `fcgi_php <url>` has **no nested block** (`validator.rs:53`), `pass = false`, `keepalive = false`, and only sets `HTTPS=on` when `ctx.http.encrypted == true` (`stages/file.rs:102` — false over plain HTTP behind Cloudflare Shield!).
  - Full `fcgi` block supports `backend "unix:///..."`, `extension ".php"`, `pass false`, `keepalive true`, `environment "HTTPS" "on"`. (**Validator note:** `config.rs:58` parses `limit`, but `validator.rs:43-51` omits `limit`, so using `limit` emits an unused-directive warning; omit `limit` unless patched).
- **Body Limits & Timeouts (`modules/http-server/src/{loader,server/mod,handler/pipeline}.rs`, `ferron directives`):**
  - **NO `client_max_body_size` / max upload size directive exists** (`buffer_request <size>` is disabled by default and only buffers if set).
  - `http { timeout <duration> }` defaults to `5m` (`300s`). **Must be placed in global `{ http { timeout "3660s"; } }`**, NOT only in a per-host block, because plain HTTP connections resolve `HttpConnectionOptions` at accept time (`native_sockets.rs:192`) before `Host` lookup!
- **Symlinks (`types/http/src/file_descriptor.rs:40-57`):** Defaults to **`disable_symlinks true` (`403`)**. Accepts `false` or `"if_not_owner"` (`modules/http-server/src/loader.rs:430` help string has a stale typo `"disable_not_owner"`, while parser checks `"if_not_owner"`!).
- **`PATH_INFO` (`modules/http-server/src/handler/file_pipeline.rs:517-629,736-751`, `http-fcgi/src/stages/file.rs:136-143`):** Natively splits `path_info` during file resolution (when no `rewrite` rule alters the URL first).
- **Compression, Headers, Basic Auth:**
  - `compressed true` (static, default `true`), `dynamic_compressed true` (FastCGI/PHP, default `false`), `precompressed true` (`.zst`/`.br`/`.gz`/`.deflate` sidecars, default `false`).
  - `file_cache_control` sets static `Cache-Control`; `header +Name "val"` sets response headers including `status` error responses.
  - `basic_auth` verifies `$2a$`, `$2b$`, `$2y$` bcrypt (`modules/http-basicauth/src/password_hash/mod.rs:326-330`); enables `brute_force_protection` by default.

---

## 4. Live PHP-FPM Docker Verification Matrix (`php:8.3-fpm-alpine`)

Tested live against NextDeploy's pool socket permissions (`user=1001; group=1001; listen=/run/php/wp1.sock; listen.mode=0660;` -> `srw-rw---- 1 root root`) and shared workspaces volume `/data/workspaces/{wp1,lar1}/htdocs`:

| Scenario | Test Case | Angie `1.12.4` Observed Result | Ferron `3.0.0-rc.11-alpine` Observed Result |
|---|---|---|---|
| **(a) Socket UID & perms (`0660 root:root`)** | Default container user | **`502 Bad Gateway`** (`connect() to unix:/run/php/wp1.sock failed (13: Permission denied)`, `user angie` uid 100) | **`500 Internal Server Error`** (`nobody` uid 65534) |
| **(a) Fixed container/worker UID** | `user root;` (Angie) / `--user 0:0` (Ferron) | **`200 OK`** JSON from PHP-FPM | **`200 OK`** JSON from PHP-FPM |
| **(b) WP pretty permalinks** | `GET /post/hello-world?a=1&b=2` | **`200 OK`**: `SCRIPT_FILENAME=.../index.php`, `REQUEST_URI=/post/hello-world?a=1&b=2`, `QUERY_STRING=a=1&b=2`, `GET={"a":"1","b":"2"}`, `HTTPS=on` | **Doc rule `r"^(.*)$"` -> `200 OK` BUT `QUERY_STRING=""`, `GET=[]` (FAIL!)**<br>**Fixed `r"^[^?]+"` -> `200 OK`: `QUERY_STRING=a=1&b=2`, `GET={"a":"1","b":"2"}`, `HTTPS=on` (PASS!)** |
| **(b) `@wp_cache` hit & bypass** | `/cached-post` (`wp-content/cache/all/cached-post/index.html`) | **HIT:** `200 OK` `WP_FASTEST_CACHE_HIT` (no PHP)<br>**Bypass `?preview=1`:** PHP `200 OK`<br>**Bypass `wordpress_logged_in` cookie:** PHP `200 OK`<br>**Bypass `POST`:** PHP `200 OK` | **Gap:** No dynamic `-f` disk-cache probe across 7 plugin paths (serves via PHP `index.php` unless Ferron `http-cache` is configured) |
| **(c) Laravel `public/` + symlink** | `/storage/test.txt`, `/api/users?page=2`, `/escape.txt` | Default `disable_symlinks off`: `/storage/test.txt` **`200 OK`** (`LARAVEL_SYMLINK_OK`), `/api/users?page=2` **`200 OK`**, `/escape.txt` `200` (relies on `symlinks.go`). With `if_not_owner`: `/storage/test.txt` **`200`**, `/escape.txt` **`404`** | Default (`true`): `/storage/test.txt` **`403 Forbidden`**!<br>With `disable_symlinks false`: `/storage/test.txt` **`200 OK`** (`LARAVEL_SYMLINK_OK`), `/api/users?page=2` **`200 OK`**.<br>With `"if_not_owner"`: `/storage/test.txt` **`200`**, `/escape.txt` **`403`** |
| **(d) Security rules** | `/xmlrpc.php`, `/wp-content/uploads/evil.php`, `/.env`, `/.git/config`, UA `sqlmap/1.7` | `/xmlrpc.php` -> **`403`**; `/wp-content/uploads/evil.php` -> **`403`** (not executed); `/.env` -> **`404`**; `/.git/config` -> **`404`**; `sqlmap` UA -> **`403`** | `/xmlrpc.php` -> **`403`**; `/wp-content/uploads/evil.php` -> **`403`** (not executed); `/.env` -> **`404`**; `/.git/config` -> **`404`**; `sqlmap` UA -> **`403`** |
| **(d) BasicAuth bcrypt** | Go/PHP `$2a$`, `$2b$`, `$2y$` hashes | Unauth -> **`401`**; `$2a$` -> **`200`**; `$2b$` -> **`200`**; `$2y$` -> **`200`** | Unauth -> **`401`**; `$2a$` -> **`200`**; `$2b$` -> **`200`**; `$2y$` -> **`200`** |
| **(d/e) `strict_404` & `custom` fallback** | Missing route, `/realdir/`, `/realdir`, `/emptydir/` | *Naive `try_files $uri $uri/index.php =404`:* **LEAKED raw PHP source (`200`, `application/octet-stream`) on `/realdir/`!**<br>*Fixed `index index.php; try_files $uri $uri/ =404;`:* `/realdir/` -> **`200` PHP**, `/realdir` -> **`301` `/realdir/`**, `/emptydir/` -> **`404`**, `/missing-route` -> **`404`** | No catch-all rewrite: `/missing-route` -> **`404`**, `/realdir/` -> **`200` PHP** (`/realdir/index.php`). Custom fallback `/sub/info.php` -> **`200`** (`GET={"k":"1"}`) |
| **(e) `.htaccess` `RoutingRule`s + POST** | Exact `/legacy-exact?z=9`, prefix `unless_exists` `/docs/page?q=1`, real file `/realfile.txt`, regex `/article/42?src=x`, 301 redir `/go-out?foo=bar`, block `/secret/locked.php`, `POST` JSON, `RequireTargetFile` | All passed: `/legacy-exact` -> `sub/info.php?from=exact`; `/docs/page?q=1` -> `sub/info.php?q=1`; `/realfile.txt` -> `REAL_STATIC_FILE`; `/article/42` -> `sub/info.php?id=42`; `/go-out?foo=bar` -> `301 https://wp1.test/dest?foo=bar`; `/secret/locked.php` -> `403`; `POST` -> JSON intact; `RequireTargetFile`: `try_files /$1.php?$args` **FAILED** (checked literal `?k=1` in filename) -> `if (-f $document_root$uri.php)` required | Host-layer vs `if_not` layer split failed until unified inside `if_not IS_PHP_PATHINFO`; unified passed ALL: `/legacy-exact?z=9` -> `from=exact&z=9`; `/docs/page?q=1` -> `q=1`; `/article/42?src=x` -> `id=42&src=x`; `/go-out?foo=bar` -> `301 ...?foo=bar` & `/go-out` -> `301 .../dest`; `/secret/locked.php` -> `403`; `POST` -> JSON intact; `RequireTargetFile` **GAP** |
| **(e) Compression & header preservation** | `big.css` (`.gz`/`.zst`) + dynamic PHP + HSTS | `big.css` (`Accept-Encoding: zstd`): `200`, **`Content-Encoding: zstd`**, **`Strict-Transport-Security`** + **`Cache-Control`** BOTH present (`more_set_headers`); PHP `zstd`: **`Content-Encoding: zstd`** | `big.css` (`Accept-Encoding: zstd`): `200`, **`Content-Encoding: zstd`**, `etag: ...-precompress-zstd`, **`strict-transport-security`** + **`cache-control`** BOTH present; PHP `zstd`: **`Content-Encoding: zstd`** |
| **(e) Timeout (`1s` vs `sleep(3)`)** | Slow PHP execution | `fastcgi_read_timeout 1s`: **`504 Gateway Timeout`** | Per-host `http { timeout "1s" }` on HTTP/1.1: **ignored (`200 SLOW_OK` after 3s)**! Global `{ http { timeout "1s" } }`: **`408 Request Timeout`** |
| **(f) `PATH_INFO`** | `/index.php/foo/bar?x=1` & `/sub/info.php/a/b?y=2` | *Without `set $nd_path_info`:* `PATH_INFO=""` (FAIL).<br>*With `set $nd_path_info` before `try_files`:* **`PATH_INFO=/foo/bar`**, **`QUERY_STRING=x=1`**, **`GET={"x":"1"}`** | *Doc rule `r"^(.*)$"`:* `PATH_INFO=""`, `QUERY_STRING=""` (FAIL).<br>*Unified `if_not IS_PHP_PATHINFO`:* **`PATH_INFO=/foo/bar`** & **`/a/b`**, **`QUERY_STRING=x=1`**, **`GET={"x":"1"}`** |

---

## 5. Production-Ready Templates for Driver Implementation

### 5.1 Phase 1.5 — Angie (`1.12.4`) Verified PHP/Static Template

Ensure `angie.conf` (`main` + `http` context) loads:
```nginx
user root; # required while pool.go uses listen.mode = 0660 with root master socket
load_module modules/ngx_http_headers_more_filter_module.so; # angie-module-headers-more
load_module modules/ngx_http_zstd_filter_module.so;         # angie-module-zstd
load_module modules/ngx_http_zstd_static_module.so;         # angie-module-zstd

http {
    client_max_body_size 5g; # or site max(PostMaxSize, UploadMaxFilesize)
    gzip on;
    gzip_static on;
    gzip_types text/plain text/css application/javascript application/json image/svg+xml application/xml;
    zstd on;
    zstd_static on;
    zstd_types text/plain text/css application/javascript application/json image/svg+xml application/xml;

    # @wp_cache bypass maps
    map $request_method $wp_skip_method { default 0; POST 1; }
    map $args $wp_skip_args { default 1; "" 0; }
    map $http_cookie $wp_skip_cookie {
        default 0;
        "~*(comment_author|wordpress_[a-f0-9]+|wp-postpass|wordpress_logged_in|woocommerce_items_in_cart|wp_woocommerce_session)" 1;
    }
    map $uri $wp_skip_uri {
        default 0;
        "~*(/wp-admin/|/xmlrpc\.php|/wp-(app|cron|login|register|mail)\.php|/wp-json/|/feed/|sitemap.*\.xml)" 1;
    }
    map "$wp_skip_method$wp_skip_args$wp_skip_cookie$wp_skip_uri" $wp_c1 { "0000" "/wp-content/cache/all${uri}index.html"; default "/__nd_miss__"; }
    map "$wp_skip_method$wp_skip_args$wp_skip_cookie$wp_skip_uri" $wp_c2 { "0000" "/wp-content/cache/all${uri}/index.html"; default "/__nd_miss__"; }
    map "$wp_skip_method$wp_skip_args$wp_skip_cookie$wp_skip_uri" $wp_c3 { "0000" "/wp-content/cache/supercache/${host}${request_uri}/index-https.html"; default "/__nd_miss__"; }
    map "$wp_skip_method$wp_skip_args$wp_skip_cookie$wp_skip_uri" $wp_c4 { "0000" "/wp-content/cache/supercache/${host}${request_uri}/index.html"; default "/__nd_miss__"; }
    map "$wp_skip_method$wp_skip_args$wp_skip_cookie$wp_skip_uri" $wp_c5 { "0000" "/wp-content/cache/cache-enabler/${host}${request_uri}/https-index.html"; default "/__nd_miss__"; }
    map "$wp_skip_method$wp_skip_args$wp_skip_cookie$wp_skip_uri" $wp_c6 { "0000" "/wp-content/cache/cache-enabler/${host}${request_uri}/index.html"; default "/__nd_miss__"; }
    map "$wp_skip_method$wp_skip_args$wp_skip_cookie$wp_skip_uri" $wp_c7 { "0000" "/wp-content/cache/wp-rocket/${host}${request_uri}/index-https.html"; default "/__nd_miss__"; }
    map "$wp_skip_method$wp_skip_args$wp_skip_cookie$wp_skip_uri" $wp_c8 { "0000" "/wp-content/cache/wp-rocket/${host}${request_uri}/index.html"; default "/__nd_miss__"; }
}
```

Per-site `server { ... }` block:
```nginx
server {
    listen 80; # + 443 ssl / quic per TLS mode
    server_name {{domains}};
    root /data/workspaces/{{appID}}/htdocs{{rootSuffix}};
    index index.php index.html;
    disable_symlinks off; # matches Caddy; symlinks.go sweeps escaping links

    # Security headers via headers-more (NOT dropped by static location add_header)
    more_set_headers "X-Content-Type-Options: nosniff";
    more_set_headers "Referrer-Policy: strict-origin-when-cross-origin";
    # Optional SiteSecurity:
    more_set_headers "Strict-Transport-Security: max-age=31536000; includeSubDomains";
    more_set_headers "X-Frame-Options: SAMEORIGIN";

    # IP ACL / Basic Auth / Scanner UA
    # allow <cidr>; deny all;   OR   deny <ip>;
    # auth_basic "Restricted"; auth_basic_user_file /data/proxy/angie/htpasswd/{{siteID}};
    if ($http_user_agent ~* "(sqlmap|nikto|nmap|masscan|acunetix|nessus|openvas|wpscan|dirbuster|gobuster|havij|zgrab)") {
        return 403;
    }

    # Sensitive & hidden files (MUST precede .php handler!)
    location ~* (^|/)\.(git|svn|hg|env|nextdeploy-db|ht[a-z]*)(/|$|\.) { return 404; }
    location ~* \.(sql|db|sqlite|key|sock)$ { return 404; }
    location = /xmlrpc.php { return 403; }
    location ~* /(uploads|upload|files|media|wp-content/uploads)/.*\.(php|phtml|phar|php[0-9])$ { return 403; }

    # RoutingRule rules BEFORE @wp_cache & PHP fallback (use `return`, `rewrite ... last`, `try_files`)
    # RequireTargetFile example: if (-f $document_root$uri.php) { rewrite ^(.+)$ $1.php last; }

    # Static cache headers (security headers survive via more_set_headers)
    location ~* \.(ico|gif|jpg|jpeg|png|svg|woff|woff2|webp|avif|ttf|eot)$ {
        add_header Cache-Control "public, max-age=2592000, immutable";
        try_files $uri =404;
    }
    location ~* \.(css|js)$ {
        add_header Cache-Control "public, max-age=604800, stale-while-revalidate=86400";
        try_files $uri =404;
    }

    # Fallback mode (ONLY put directory index via `index` directive, NEVER `$uri/index.php` in non-last try_files!)
    # - FallbackDefault:    try_files $wp_c1 $wp_c2 $wp_c3 $wp_c4 $wp_c5 $wp_c6 $wp_c7 $wp_c8 $uri $uri/ /index.php?$args;
    # - FallbackCustom:     try_files $wp_c1 $wp_c2 ... $uri $uri/ /<entry>?$args;
    # - FallbackStrict404:  location / { try_files $wp_c1 ... $uri $uri/ =404; }
    location / {
        try_files $wp_c1 $wp_c2 $wp_c3 $wp_c4 $wp_c5 $wp_c6 $wp_c7 $wp_c8 $uri $uri/ /index.php?$args;
    }

    # PHP-FPM with safe PATH_INFO capture BEFORE try_files resets $fastcgi_path_info
    location ~ ^(.+\.php)(/.*)?$ {
        fastcgi_split_path_info ^(.+\.php)(/.*)$;
        set $nd_path_info $fastcgi_path_info;
        try_files $fastcgi_script_name =404;
        include /etc/angie/fastcgi.conf;
        fastcgi_param SCRIPT_FILENAME $document_root$fastcgi_script_name;
        fastcgi_param PATH_INFO $nd_path_info;
        fastcgi_param HTTPS on; # when EnableHTTPS || ShieldNodeID != nil
        fastcgi_connect_timeout 5s;
        fastcgi_read_timeout {{max(MaxExecutionTime+30, 120)}}s;
        fastcgi_send_timeout 120s;
        fastcgi_pass unix:/data/workspaces/.nd-php/u{{ownerID}}-v{{ver}}/run/{{appID}}.sock;
    }
}
```

### 5.2 Phase 4 — Ferron (`3.0.0-rc.11`) Verified PHP/Static Template

Docker compose / host service must run with `user: "0:0"` (or `pool.go` must set `listen.mode = 0666`) and raise global `http.timeout` to `>= maxExecutionTime + 60s` (`"3660s"`):

```kdl
{
    http {
        timeout "3660s"
    }
}

match ND_HIDDEN { request.uri.path ~ r"(?i)(^|/)\.(git|svn|hg|env|nextdeploy-db|ht[a-z]*)(/|$|\.)" }
match ND_SENSITIVE_EXT { request.uri.path ~ r"(?i)\.(sql|db|sqlite|key|sock)$" }
match ND_MEDIA { request.uri.path ~ r"(?i)\.(ico|gif|jpg|jpeg|png|svg|woff|woff2|webp|avif|ttf|eot)$" }
match ND_CODE { request.uri.path ~ r"(?i)\.(css|js)$" }
match ND_PHP_PATHINFO { request.uri.path ~ r"\.php/" }
match ND_HAS_QUERY { request.uri.query != "" }
// Site-specific matchers: ND_SCANNER_<id>, ND_XMLRPC, ND_UPLOAD_PHP, ND_RULE_<id>_<n>

example.com {
    root "/data/workspaces/{{appID}}/htdocs{{rootSuffix}}"
    disable_symlinks false
    compressed true
    dynamic_compressed true
    precompressed true

    header +X-Content-Type-Options "nosniff"
    header +Referrer-Policy "strict-origin-when-cross-origin"
    // Optional: header +Strict-Transport-Security "max-age=31536000; includeSubDomains"
    // Optional: header +X-Frame-Options "SAMEORIGIN"

    if ND_MEDIA { file_cache_control "public, max-age=2592000, immutable" }
    if ND_CODE { file_cache_control "public, max-age=604800, stale-while-revalidate=86400" }

    // ACL / Security blocks (evaluated in HttpResponseStage before basic_auth)
    if ND_HIDDEN { status 404 { body "Not Found" } }
    if ND_SENSITIVE_EXT { status 404 { body "Not Found" } }
    // if ND_XMLRPC / ND_UPLOAD_PHP / ND_SCANNER / RoutingRule block -> status 403/410
    // if ND_REDIR_n -> nested if ND_HAS_QUERY { status 301 { location "{{target}}?{{request.uri.query}}" } } / if_not ND_HAS_QUERY { status 301 { location "{{target}}" } }

    // basic_auth { realm "Restricted Area"; users { <user> "<bcrypt-$2a$-hash>"; } }

    fcgi {
        backend "unix:///data/workspaces/.nd-php/u{{ownerID}}-v{{ver}}/run/{{appID}}.sock"
        extension ".php"
        pass false
        keepalive true
        environment "HTTPS" "on" // omit if !EnableHTTPS && ShieldNodeID == nil
    }

    // ALL rewrites (RoutingRule + fallback) unified in one layer so order & PATH_INFO stay intact!
    if_not ND_PHP_PATHINFO {
        // ... converted RoutingRule rewrites with `{ last }` (paired with `\?(.*)$` when adding fixed params) ...

        // FallbackDefault:
        rewrite r"^[^?]+" "/index.php" {
            file false
            directory false
            last
        }
        // FallbackCustom: replace "/index.php" with "/<entry>"
        // FallbackStrict404: omit catch-all fallback rewrite! (Native static + index.php -> 404 for missing)
    }
}
```

---

## 6. Actionable Plan Doc Corrections Required

1. **`docs/multi-proxy/03-angie-driver.md`:**
   - Replace `/run/nd-php/{{app}}.sock` with NextDeploy's actual `pool.go` path `/data/workspaces/.nd-php/u<ownerID>-v<ver>/run/<appID>.sock` (note `:ro` on `/data/workspaces` vs RW `.nd-php` mount).
   - Add `user root;` caveat in `angie.conf` (or update `pool.go` socket mode/group) — verified live `502` without it!
   - Replace naive `location ~ \.php$ { try_files $uri =404; }` with `fastcgi_split_path_info` + `set $nd_path_info $fastcgi_path_info;` + `try_files $fastcgi_script_name =404;` + `fastcgi_param PATH_INFO $nd_path_info;` + `fastcgi_param HTTPS on;` + per-site `client_max_body_size` & `fastcgi_read_timeout`.
   - Explicitly warn against `$uri/index.php` in non-last `try_files` (raw PHP source disclosure verified live!).
   - Document `more_set_headers` (`angie-module-headers-more`) + `zstd` (`angie-module-zstd`) + `@wp_cache` `map`/`try_files`.
2. **`docs/multi-proxy/04-ferron-driver.md`:**
   - Replace `/run/nd-php/{{app}}.sock` with `/data/workspaces/.nd-php/.../run/<appID>.sock`.
   - Replace `fcgi_php` with `fcgi { ...; pass false; keepalive true; environment "HTTPS" "on"; }` when Shield/HTTPS needs forced `HTTPS=on`.
   - Fix front-controller rewrite from `r"^(.*)$"` -> `r"^[^?]+"` inside unified `if_not IS_PHP_PATHINFO` alongside all `RoutingRule` rewrites.
   - Add `compressed true`, `dynamic_compressed true`, `precompressed true`, `file_cache_control`, hidden dotfile/extension `404` blocks, and global `{ http { timeout "3660s"; } }`.
3. **`docs/multi-proxy/02-capability-matrix.md` & `05-edge.md`:**
   - Fix Ferron rewrite row from `r"^(.*)$"` to `r"^[^?]+"` + `if_not IS_PHP_PATHINFO`.
   - Add `@wp_cache` disk-cache row: Caddy ✅ / Angie ✅ / Ferron ❌ (no `-f` dynamic path probe across 7 plugin paths).
   - Add Request Max Body Size row: Caddy (unlimited/configurable) ✅ / Angie (`client_max_body_size` up to `5g;`) ✅ / Ferron ⚠️ (no `client_max_body_size` directive).
   - Mark Ferron `RequireTargetFile` (`{path}.php`) explicitly ❌ / unsupported (not `🔍`).
   - Add `angie-module-headers-more` and `angie-module-zstd` to `05-edge.md` / `nd edge install --proxy angie` package install lists (`oss_packages`).
