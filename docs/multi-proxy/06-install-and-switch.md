# 06 — Install choice, settings, and switching engines

## 1. Settings

New setting keys (DB `settings` table):

| Key | Values | Default |
|---|---|---|
| `proxy_engine` | `caddy` \| `angie` \| `ferron` | `caddy` (existing installs: always `caddy` on upgrade) |
| `proxy_image` | image ref | engine default (pinned) |
| `proxy_experimental_ack` | `true/false` | `false` (required to use Ferron) |
| `proxy_last_switch` | JSON `{from,to,at,status,error}` | — |

Keep `caddy_*` keys. They are read by the Caddy driver only. Rename later.

## 2. Installer (`install.sh`)

- New flag: `--proxy caddy|angie|ferron`.
- If the flag is absent and stdin is a TTY: prompt

  ```
  Choose reverse proxy engine:
    1) Caddy  (default, simplest; label-driven)
    2) Angie  (lowest RAM per connection, best for small VPS / high traffic)
    3) Ferron (experimental: fastest, but missing idle/write timeouts)
  [1]:
  ```

  If the flag is absent and stdin is not a TTY: `caddy`.
- Write the root compose with the chosen engine's service (`Driver.StackService`) and persist `proxy_engine` via the panel's first-boot env (`ND_PROXY_ENGINE`) so the panel seeds the setting.
- Upgrade path: the existing logic that preserves panel labels and `nd_shared` volumes must become engine-aware. For Caddy it stays exactly as it is today. For Angie/Ferron it preserves `/data/proxy/<engine>/` (the panel re-renders it anyway).
- Container name: keep `caddy` for the Caddy driver (do not break existing scripts). Use `nd-proxy` for Angie/Ferron. Expose `Driver.ContainerName()` and remove the hardcoded `CaddyContainerName` from callers.

## 3. Switching (panel)

UI: Settings → **Proxy engine**: current engine, version, status, and a "Switch engine" button. CLI: `nd proxy engine set <engine> [--yes]`.

### 3.1 Preflight (must pass before anything changes)

1. Build the IR. Compute `requiredCapabilities(IR)`, e.g. `RegexRouting`, `PHPFastCGI`, `BasicAuthBcrypt`, `RequireTargetFile`, `H2IdleTimeout` (if `GlobalOptions.idle` is set).
2. Diff with `target.Capabilities()`. List each unsupported feature with the affected sites. **Block** the switch on hard gaps. **Warn** on soft gaps (e.g. Ferron idle timeout).
3. Ferron: require `proxy_experimental_ack=true` (checkbox + typed confirm).
4. Dry-run: `Render` + `Validate` the target config in a throwaway container (`docker run --rm <image> <validate-cmd>`) with the staged files mounted.
5. Check that ports 80/443 are free besides the current proxy, and that the Docker socket is accessible (Angie).

### 3.2 Switch steps

1. Pull the target image.
2. Render the target config to `/data/proxy/<engine>/` (staged → final).
3. Certificates:
   - From Caddy: copy issued certs from the `caddy_data` volume (`/data/caddy/certificates/<issuer>/<host>/<host>.crt|.key`) into `/data/proxy/pki/imported/<host>.{crt,key}`. Render them as manual certs for hosts with `TLSMode=auto`. The target ACME takes over at renewal (render as ACME once the imported cert is <30 days from expiry, or on the next deploy after the target has obtained its own).
   - Internal TLS: ND CA already exists (pki is engine-independent).
4. Re-generate **all app compose overrides** with the target's `AppLabels` (Angie upstream labels / none). This needs `docker compose up -d` per app, which recreates containers only when labels change. Batch with a concurrency limit, but do it **before** the cutover so the upstreams are populated.
5. Recreate custom proxy routes: remove the `nd-route-*` label containers (Caddy-only). Their routes now live in rendered files.
6. Cutover: `docker compose up -d proxy` with the new service definition (stop old, start new on the same ports). Downtime is ~2–5 s.
7. Verify: health endpoint, plus an HTTPS GET to N sample hosts (panel domain first). Compare status codes with the pre-switch probe.
8. Commit: persist `proxy_engine`, `proxy_last_switch{status:ok}`.

### 3.3 Rollback (automatic on any failure in 6–7, or manual within 24 h)

1. Restore the previous root compose service and app labels (kept as `*.prev`).
2. `docker compose up -d proxy`.
3. Mark `proxy_last_switch{status:rolled_back,error}`.

Keep the old engine's data volume (`caddy_data`) for at least 30 days. Never delete it automatically.

## 4. nd-edge

See 05 §3–4. The edge engine is independent of the panel engine. Mixed setups (panel Caddy + edge Angie) are expected and supported.
