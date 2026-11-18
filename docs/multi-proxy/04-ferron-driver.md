# 04 — Ferron driver (Experimental)

Engine: Ferron 3 (MIT). Pin `ferronserver/ferron:3.0.0-rc.11-alpine` for now (switch to `3.x` stable when released). Use the `-alpine` tag so the panel can `docker exec` (distroless has no shell).

## 0. Gate: why it is Experimental

Verified in source (`ferronweb/ferron@3.0.0-rc.11` and `develop-3.x`, `zincio-http 0.4.15`) and by tests:

| Gap | Evidence | Impact |
|---|---|---|
| HTTP/2 idle connections never closed | `modules/http-server/src/server/native_sockets.rs::build_http2_options()` never calls `.idle_timeout(..)`; `zincio-http` `Http2Options::default()` has `idle_timeout: None` | Behind Cloudflare, idle origin connections stay until Cloudflare drops them (900 s). The pile-up seen on the shield is possible again |
| No write/slow-reader timeout | probe: client stops reading a 50 MB response → never closed | slowloris-style memory pinning |
| No request-body idle timeout | probe: 1 byte / 10 s upload → never closed | same |
| HTTP/1 keep-alive fixed at 30 s, header read fixed at 30 s | `Http1Options` defaults, not exposed | not tunable |
| Reload busy-loop bug | ferronweb/ferron#996 (open): "Primary runtime thread busy-loops on read() returning EAGAIN after a configuration reload" | deploy-time reloads could stall all hosts |
| Default strict DNS breaks on container recreate | test: 503 forever after the backend IP changes | must use `logical_dns true` |

UI: show "Ferron · Experimental" with these warnings. The switch preflight requires an explicit confirmation checkbox. Do not offer Ferron for nd-edge unless `--experimental` is passed.

Re-evaluate when an upstream release exposes HTTP/2 idle / write timeouts and fixes #996. Then drop the gate. The tracking issue text is drafted in the test notes.

## 1. Root stack service

```yaml
  proxy:
    container_name: nd-proxy
    image: ferronserver/ferron:3.0.0-rc.11-alpine
    user: "0:0"                               # image default is nobody; needs ports 80/443 + 0660 root-owned php sockets
    command: ["/usr/local/bin/ferron","run","-c","/etc/ferron/nd/sites/ferron.conf","--config-adapter","ferronconf"]   # no watch=1 (see §4)
    environment:
      ND_FERRON_ADMIN_TOKEN: "<non-empty secret>"   # if unset, Ferron keeps the literal "{{env.ND_FERRON_ADMIN_TOKEN}}" as the token
    ports: ["80:80","443:443/tcp","443:443/udp"]
    volumes:
      - /data/proxy/ferron:/etc/ferron/nd:ro     # parent dir (not single files) so the relative `sites` symlink swap resolves
      - /data/proxy/pki/sites:/etc/nd-pki/sites:ro
      - /data/proxy/pki/imported:/etc/nd-pki/imported:ro
      - ferron_acme:/var/cache/ferron-acme
      - /data/workspaces:/data/workspaces:ro
      - /data/workspaces/.nd-php:/data/workspaces/.nd-php
```

## 2. Layout and main config

```
/data/proxy/ferron/sites -> sites.gen-<hash>      # relative symlink swapped atomically
/data/proxy/ferron/sites.gen-<hash>/ferron.conf   # main file (live entry: /etc/ferron/nd/sites/ferron.conf)
/data/proxy/ferron/sites.gen-<hash>/sites/*.conf  # one file per site
```

```ferron
{
    admin {
        listen "127.0.0.1:8081"
        auth_token "{{env.ND_FERRON_ADMIN_TOKEN}}"
    }
    http {
        protocols h1 h2
        timeout "3660s"                    # must be in the global http block to apply to HTTP/1.1
    }
    client_ip_from_header x-forwarded-for {
        {{range trusted}}trusted_proxy "{{.}}"
        {{end}}
    }
    strict_hosts                            # one site per file; a duplicate host is a validation error
}

include "sites/*.conf"
```

## 3. Site templates

### 3.1 App proxy

```ferron
{{hosts joined}} {
    {{tls: auto → tls { provider acme; challenge http-01; contact {{email}}; cache /var/cache/ferron-acme }
         internal → tls { provider manual; cert /etc/nd-pki/sites/{{stem}}.crt; key /etc/nd-pki/sites/{{stem}}.key }
         none → (use "http {{host}}:80" selectors instead)}}
    header X-Frame-Options "SAMEORIGIN"
    header X-Content-Type-Options "nosniff"
    header Referrer-Policy "strict-origin-when-cross-origin"

    {{range paths}}                           # match blocks are AND-only: one combined regex per route
    match nd_p{{n}} { request.uri.path ~ r"^{{prefix}}(?:/|$)" }
    {{end}}
    {{range static paths}}
    if nd_p{{n}} {
        root {{root}}
        rewrite r"^{{prefix}}/?" "/" { last }       # strips the prefix
    }
    {{end}}

    if nd_default {                           # nd_default: one combined negative regex over all path prefixes
        proxy {
            upstream "http://{{container}}:{{port}}" {
                logical_dns true            # MANDATORY (container recreate)
            }
            keepalive true
        }
    }
}
```

**Do not** model path routes as `location /x { root }` + `location / { proxy }`. Ferron merges matching `location` blocks, and the `proxy` from `/` leaks into `/x` (tested: static paths returned the upstream's 404). Use `match`/`if`/`if_not`.

`location` strips its prefix before proxying. For proxied path routes that must keep the prefix, use `match`/`if` with `proxy` and no `location`, or append the prefix to the upstream URL.

### 3.2 PHP / static

```ferron
{{hosts}} {
    {{tls}}
    root {{root}}
    disable_symlinks false                       # Ferron blocks symlinks by default; Laravel storage:link etc.
    fcgi {
        backend "unix:///data/workspaces/.nd-php/u<owner>-v<ver>/run/<app>.sock"
        extension ".php"
        pass false
        keepalive true
        environment "HTTPS" "on"
    }
    compressed
    dynamic_compressed
    {{security: block/allow, basic_auth { users { user "<bcrypt>" } }, status 403 for xmlrpc/uploads/UA matchers, header hsts/frame}}
    # routing rules and the fallback share ONE guard so PATH_INFO URLs (/index.php/x) still reach PHP:
    if_not ND_PHP_PATHINFO {                     # match ND_PHP_PATHINFO { request.uri.path ~ r"\.php/" }
        {{routing rules → rewrite with regex, last}}
        rewrite r"^[^?]+" "{{fallback entry}}" { file false; directory false; last }   # default/custom fallback; strict_404 → none
    }
}
```

`redir` rules with `preserve_query`: `status` matches the path only and interpolates `{{...}}` before the regex replace. Use `${1}` (not `$1`) and two matchers: one with `request.uri.query != ""` appending `?{{request.uri.query}}`, one without.

`require_target_file` rules have no equivalent (verified: `rewrite` only supports `file false` / `directory false` on the current path). The preflight hard-blocks them; the driver omits them with a `# IGNORED` comment.

## 4. Reload

- `watch=1` is **not used**. Verified: it watches only files that existed when the config loaded, so a **new** file matched by `include "…/*.conf"` is not detected, and it can reload on a half-written config.
- The driver validates, swaps the relative `sites` symlink, then calls `POST http://127.0.0.1:8081/reload` (bearer token from `ND_FERRON_ADMIN_TOKEN`, via `docker exec … nc`).
- After a reload, check `GET /reload` → `last_reload_error == null` and `active_generation` incremented. On an error or no increment, restore the previous generation and reload again.
- Ferron restarts the admin listener on every reload. In-memory rate-limit buckets reset on reload (documented). Not used by NextDeploy today.

## 5. TLS

- ACME: built-in. Persist the cache. On-demand issuance with `on_demand_ask https://panel/api/v1/proxy/tls-ask` is possible for sslip.io wildcard previews. Implement the ask endpoint so that it approves only known hosts.
- Internal: ND private CA (same as Angie).
