# ADR 001: Multi-proxy drivers (Caddy, Angie, Ferron)

- Status: Accepted, implemented on branch `feat/multi-proxy-drivers`
- Date: 2026-11-18
- Plan: [`docs/multi-proxy/`](../multi-proxy/README.md). Evidence: [`docs/spikes/2026-11-18-multi-proxy-engines.md`](../spikes/2026-11-18-multi-proxy-engines.md), [`docs/spikes/2026-11-18-php-and-htaccess-deep-dive.md`](../spikes/2026-11-18-php-and-htaccess-deep-dive.md).

## Context

The panel was coupled to caddy-docker-proxy: sites were Docker labels on app containers. A production Edge Shield with 1 GB of RAM was OOM-killed under thousands of idle Cloudflare HTTP/2 connections. Angie and Ferron performed better in the benchmarks, but they are file-driven, so the label-based design could not carry them.

## Decisions

### 1. Desired-state IR, drivers render from it

`internal/proxy/state.Build` builds an engine-neutral `proxy.State` from the database (pure, no I/O; `state.Load*` does the reads). `State.ComputeGeneration()` is a SHA-256 of the canonical JSON and is used to skip unchanged applies. Drivers implement `proxy.Driver` (`Render`, `Validate`, `Apply`, `Status`, `Logs`, `Restart`, `StackService`, `AppLabels`).

Consequence: one source of truth for three engines, and golden tests per driver.

### 2. Caddy stays label-driven and unchanged

`internal/proxy/caddy` wraps `internal/caddy` and must produce byte-identical labels (parity tests). The container keeps the name `caddy`. Angie and Ferron use `nd-proxy`.

### 3. Capabilities and preflight gate every switch

Each driver reports `proxy.Capabilities`. `proxy.EvaluateCapabilities` diffs the IR against the target and returns hard blocks (switch refused) and soft warnings. Ferron additionally requires an explicit experimental acknowledgement (`proxy_experimental_ack`, and `--experimental` on the CLI and `nd edge install`).

### 4. Angie

- One `acme_client` per site, with names restricted to `[A-Za-z0-9_]`, all sharing `account_key=/var/lib/angie/acme/nd_account.key`. A single `acme_client` issues one SAN certificate for every `server_name` that references it, which would bundle tenants together.
- Docker upstream discovery through `docker_endpoint` and `angie.http.upstreams.<name>.port` / `angie.network` labels (the network name is case-sensitive). `docker_max_object_size` is raised to `1m` because the default `64k` breaks discovery above roughly 25 containers.
- `user root;` so Angie can read and write the Docker socket and open the 0660 root-owned PHP-FPM sockets.
- The `headers_more` dynamic module is always loaded; the `zstd` modules are loaded only when a site uses zstd. Child `add_header` directives drop parent ones, so security headers are re-included where a location adds its own.
- Upstreams always use `keepalive 64`. `keepalive_timeout` defaults to `75s`, or `20s` when real-IP mode is Cloudflare.
- Alpine is kept: `auth_basic_user_file` accepts `$2y$`, `$2b$` and `$2a$` bcrypt hashes (verified live).

### 5. Ferron is experimental

- Every hostname upstream sets `logical_dns true`. Without it, Ferron stays broken after a container IP change.
- Path routes use `match`/`if`/`if_not` with one combined regex, never `location` plus a host-level `proxy` (Ferron merges matching locations and the `proxy` leaks).
- `timeout "3660s"` is set in the global `http` block (it must be global to apply to HTTP/1.1).
- Reload is explicit: `POST /reload`, then `GET /reload` must show `last_reload_error == null` and a higher `active_generation`. The container does not use `watch=1`, because it does not notice new files matched by `include "…/*.conf"` and it can reload on a half-written config.
- The admin token comes from `ND_FERRON_ADMIN_TOKEN`; compose must always set it, because an unset variable leaves the literal `{{env.…}}` string as the token.
- Container runs as `0:0` for ports 80/443 and the 0660 PHP-FPM sockets. `disable_symlinks false` is required for Laravel `storage:link`.
- `require_target_file` rules cannot be translated. They are omitted with a comment (or rejected with `WithStrictUnsupported`), and the preflight blocks the switch.

### 6. Atomic generation staging

`proxy.StageStore` writes each render to `<name>.gen-<hash>/` and activates it by swapping one relative symlink (`sites -> sites.gen-…`). The directory, not individual files, is bind-mounted, so the container always follows the swap. A failed validate never activates a generation, and a failed reload or health check rolls the symlink back and reloads the previous generation. `Syncer` owns staging only when `SyncerConfig.Stage` is set; otherwise the driver's `Apply` does.

### 7. Internal TLS uses a NextDeploy CA

`internal/proxy/pki` issues ECDSA P-256 leaf certificates per site. The CA key lives outside the directory mounted into the proxy. Caddy-issued certificates are imported as manual certificates during a switch to avoid ACME rate limits.

### 8. Sync model: `SyncNow` vs `SyncAsync`

`Syncer` debounces triggers (300 ms), coalesces reasons and serializes apply. `SyncAsync(reason)` enqueues and returns; use it after state changes where the caller does not need the result. `SyncNow(ctx, reason)` runs a batch and returns the `ApplyResult`; use it where the caller must report failure (settings save, engine switch). Rollback and `proxy_apply` recording are detached from the caller's context so shutdown does not abort them.

## Consequences

- Existing Caddy installs behave as before.
- Angie and Ferron add drivers, installer flags, edge engines and UI, all gated by capabilities.
- Unsupported Ferron features are visible in the preflight rather than failing at runtime.

## Open items

- Live ACME issuance against Let's Encrypt staging for Angie and Ferron (`[UNVERIFIED]` in the spike).
- Full package-install verification for `nd edge install --proxy angie` on Debian/Ubuntu/Alpine VMs.
- Remove the Ferron gate when upstream exposes HTTP/2 idle and write timeouts and fixes #996.
