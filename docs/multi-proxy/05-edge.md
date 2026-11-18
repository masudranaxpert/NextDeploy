# 05 — nd-edge (shield) multi-engine

nd-edge is already well separated:

```
poller (GET /api/v1/edge/routes) → RoutesResponse{Origin, Routes[]}
  → reconciler (DNS gate, claims, probes)
  → render.go: RenderCaddyfileManagedRegion / RenderGlobalOpts* / RenderCommonCaddy / RenderPanelCaddy
  → apply.go: Applier (generations panels.gen-N, origins.gen-N, symlink swap) + CaddyRunner{Validate, Reload, CheckAdmin}
  → status push (POST /api/v1/edge/status, caddy_version, caddy_running)
```

So the edge needs a **renderer + runner per engine**. The data model, generations, DNS gate, and probes stay as they are.

## 1. Interfaces

```go
// cmd/nd/internal/edge/engine.go
type Engine interface {
    Name() string                                   // caddy | angie | ferron
    Paths() EnginePaths                             // main config, managed dir, service unit, binary
    RenderGlobal(cfg *Config, cfRanges []string, anyTrustedProxy bool) (map[string]string, error)
    RenderPanel(in RenderPanelInput) (string, error)    // one file per panel (same input as today)
    RenderCommon() (map[string]string, error)
    Runner() Runner
}

type Runner interface {                             // generalizes CaddyRunner
    Validate(ctx context.Context, mainConfig string) (string, error)
    Reload(ctx context.Context) (time.Duration, error)
    CheckAlive(ctx context.Context) error
    Version(ctx context.Context) (string, error)
}
```

- `caddy`: move the existing `render.go` functions behind `CaddyEngine` with no output change. Golden tests must stay identical.
- `angie`: render `/etc/angie/nd-edge/{global.conf,common.conf,panels/<panelID>.conf}`. Validate with `angie -t`. Reload with `systemctl reload angie` (or `angie -s reload`).
- `ferron`: render `/etc/ferron/nd-edge/...`. Validate with `ferron validate -c`. Reload via `systemctl reload ferron` (SIGHUP) or the admin API. **Experimental only.**

## 2. Edge site template (Angie)

One `server` per route group, proxying to the origin over HTTPS with the origin CA. This mirrors `writeUpstreamSnippet`:

```nginx
upstream nd_up_<panelShort> {
    {{range origin.ips}}server {{.}}:{{origin.port}};{{end}}
    keepalive 64;                                       # mandatory
}
server {
    listen 80; listen 443 ssl; http2 on;
    server_name {{hosts}};
    acme nd_<panelShort>;  ssl_certificate $acme_cert_nd_<panelShort>; ssl_certificate_key $acme_cert_key_nd_<panelShort>;   # one acme_client per panel/domain group ([A-Za-z0-9_] names, shared account_key); or Cloudflare Origin Cert (recommended behind CF, avoids ACME failures)
    keepalive_timeout 20s;                              # Cloudflare idle pile-up fix
    client_max_body_size {{options.max_body_mb|0}}m;
    location / {
        proxy_pass https://nd_up_<panelShort>;
        proxy_http_version 1.1;
        proxy_set_header Connection $nd_connection_upgrade;   # websocket
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Host $host;
        proxy_ssl_server_name on; proxy_ssl_name $host;
        proxy_ssl_verify on; proxy_ssl_trusted_certificate /etc/angie/nd-edge/origins/<panelID>.pem;
        proxy_read_timeout 1h; proxy_buffering off;
    }
}
```

Global (`RenderGlobalOptsForConfig` equivalent): `set_real_ip_from` Cloudflare ranges + shield IPs, `real_ip_header CF-Connecting-IP`, `keepalive_timeout 20s`, `send_timeout 60s`, `client_header_timeout 10s`, `client_body_timeout 15s`, `worker_connections 65535`, `worker_rlimit_nofile 1048576`, `worker_shutdown_timeout 10m`. Host-installed edges must **not** emit `resolver 127.0.0.11` (Docker's embedded DNS). zstd on host packages needs the separate `angie-module-zstd` package.

Bot/abuse rules that operators add by hand today (e.g. the `shield_block` snippet: cross-site fetch, bad bot UA, no UA, per-IP rate limits) belong in a **user include dir** (`/etc/angie/nd-edge/user/*.conf`), exactly like `/etc/caddy/nd/user/*.caddy` today. Rate limits: `limit_req_zone $binary_remote_addr zone=… rate=…;`.

## 3. Install

`nd edge install --panel-url URL --token-stdin --proxy caddy|angie|ferron [--experimental]`

- Default: `caddy` for existing installs (the marker file says Caddy). Recommended in the docs and the setup script: `angie`.
- Angie package install: official repo `https://download.angie.software/angie/$ID/$VERSION_ID $VERSION_CODENAME main` (verified: `debian/12 bookworm`, `ubuntu/22.04 jammy`, `ubuntu/24.04 noble`; omitting `$VERSION_ID` returns 404). Signing key for apt: `https://angie.software/keys/angie-signing.gpg` (to `/etc/apt/keyrings/angie-signing.gpg`). Alpine: `https://download.angie.software/angie/alpine/v<X.Y>/main` with the **RSA** key `https://angie.software/keys/angie-signing.rsa` (to `/etc/apk/keys/`). Write the marker `angie-installed-by-nd`. Verified through repository `Release`/`APKINDEX` requests; a full VM install is `[UNVERIFIED]`.
- Ferron: official installer or binary archive + systemd unit. Requires `--experimental`.
- Port check: only one engine may own 80/443. Stop and disable others after a successful validate + start.
- `adoptExisting` (current Caddyfile adoption) applies only to `caddy`.

## 4. Switch

`nd edge switch-proxy <engine>`:

1. Install the target engine if missing (do not start it).
2. Render the full config from the **current** applied state (`ApplyMeta` + last `RoutesResponse` cache).
3. Validate.
4. Certificates: if the source is Caddy, copy issued certs from Caddy storage into `/etc/<engine>/nd-edge/certs/` and render them as manual certs. ACME takes over on renewal.
5. `systemctl stop caddy && systemctl start angie`. Downtime is ~1–2 s.
6. Probe `CheckAlive` and an HTTPS request to each active host (the existing probe loop). On failure: stop the new engine, start the old one, report the error.
7. Persist `engine` in `edge.Config` and the status.

## 5. API additions (backward compatible)

`StatusRequest` and `RegisterRequest` gain optional fields. Keep `caddy_version` / `caddy_running` populated for old panels (fill them from the active engine).

```json
{ "proxy_engine": "angie", "proxy_version": "1.12.4", "proxy_running": true }
```

Panel: show the engine per shield node in the shield UI. No change to the `routes` contract.

## 6. Production rollout for the current shield (107.189.6.222)

1. On a test shield: `nd edge install --proxy angie`. Point one low-traffic domain at it via the panel. Watch for 48 h: RAM, 520s in Cloudflare analytics, `ss -s`.
2. Move `/etc/caddy/Caddyfile` hand-written blocks (non-managed: flixbd, desi4u, pinay, gcloud) into `/etc/angie/nd-edge/user/*.conf`, translated with the templates above. These are outside nd-edge's managed region, so they need a manual one-time translation.
3. `nd edge switch-proxy angie` on 107 in a low-traffic window, with the rollback plan from step 6 of the switch flow.
