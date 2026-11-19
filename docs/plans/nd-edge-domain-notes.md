# Design Notes: `nd edge domain` (Manual Domains & Garbage Cleanup)

## 1. Research Summary & Pipeline Integration

### 1.1 Managed Generation & Apply Pipeline
- `cmd/nd/internal/edge/apply.go` (`Apply` for Caddy) and `apply_engine.go` (`applyForEngine` for Angie/Ferron) share the same atomic generation model:
  1. Compare desired config files (`global`, `common`, `panels/*`) against the currently active `panels` / `origins` symlinks (`compareCurrentGeneration`, `compareEngineGeneration`).
  2. Stage `panels.gen-NNNNNN` and `origins.gen-NNNNNN`.
  3. Build an isolated validation staging copy (`.panels.validate.<gen>`) rewriting `origins/` paths to the new `origins.gen-<gen>` and validate via `Runner.Validate` (`caddy validate`, `angie -t -c`, `ferron validate -c ... --json --config-adapter ferronconf`).
  4. Write global/common files, atomically swap relative directory symlinks (`SwapDirLinkAtomic`), and call `Runner.Reload` with automatic rollback to the previous generation on failure.
  5. Prune keeping the newest 3 generations.

- **Storage paths (`edge.Paths`)**:
  - `DomainsFile`: `/etc/nd-edge/domains.json` (`0600`, `"schema": 1`)
  - `DomainsTrashFile`: `/etc/nd-edge/domains.trash.json` (`0600`, 30-day retention pruned on write)
  - `DomainsHealthFile`: `/var/lib/nd-edge/domains.health.json` (`0600`, multi-check bad-DNS / bad-origin history)
- **Plug-in point for manual domains:**
- Add `Manual *PanelRenderArtifact` to `ApplyInput` (`apply.go`).
- Render all enabled manual entries into a single deterministic file `zz-manual.caddy` (Caddy) or `zz-manual.conf` (Angie/Ferron) inside `panels.gen-NNNNNN`.
- Include `in.Manual` in `DesiredHash`, `compareCurrentGeneration`, `compareEngineGeneration`, `stageGeneration`, `stageEngineGeneration`, `shouldEmitHealthProbe`, and `shouldEmitEngineHealthProbe`.
- Manual files never go into `user/` (`user/*.conf`, `user/*.caddy`, or unmanaged `Caddyfile` blocks), and `RebuildClaimsFromDir` (`claims.go`) skips `zz-manual.caddy` / `zz-manual.conf` so manual hosts are never mistaken for a panel.
- `stageAndValidateSwitchConfig` (`cmd/nd/cmd/edge_switch.go`) also renders `zz-manual` for the target engine (and migrates explicit `--tls manual` cert pairs into `targetPaths.ManualCertsDir`) so `nd edge switch-proxy` carries manual domains over seamlessly and validates them before cutover.

### 1.2 Reconciler, Boot Guard, and Daemon Coordination
- `Reconciler.ReconcileOnce` (`reconciler.go`) currently skips reconciliation on startup when `anyLastGood == false && r.hasExistingRenderedFiles()`. Updated so if enabled manual domains exist in `/etc/nd-edge/domains.json`, reconciliation runs even when 0 panels are registered or panels have not polled yet.
- For mutating CLI commands (`add`, `set`, `rm`, `enable`, `disable`, `prune --apply`, `restore`, `import`):
  1. Acquire the edge config lock (`edgeConfigLockPath`).
  2. Load `domains.json`, validate input/upstreams/certs, check conflicts against `claims.json` and `user/` configs.
  3. Build candidate `ManualDomainsFile` in memory without writing `domains.json` yet.
  4. If the daemon lock (`paths.LockFile`) is free, acquire it and run full `reconcileWithLoadedStates` with the candidate manual domains; if `Apply` fails, return the engine error and leave `domains.json` untouched.
  5. If the daemon is running (holds `paths.LockFile`), stage and run `Runner.Validate` synchronously against the candidate config before saving `domains.json`, then save `domains.json` atomically (`0600`) and trigger daemon reload (`signalDaemonReload` via `systemctl reload nd-edge` / `SIGHUP`). `Daemon.ReloadConfig` re-reads `config.json` and triggers immediate reconciliation (`domains.json` is read on every reconcile).

### 1.3 DNS Gate, Conflicts & Health Persistence
- **TLS `auto` DNS gating:** Matches panel route behavior cleanly:
  - `DNSGate.CheckHost` gates `--tls auto` manual hosts (honoring `cfg.DNSGateOff`).
  - If `pending_dns` or `pending_caa` and not yet rendered, the domain is saved in `domains.json` and surfaced as `pending_dns` / `pending_caa` in `nd edge status`, `domain list`, and `domain check`, without requesting ACME certificates prematurely. Once DNS points to the Shield (or Cloudflare), `nd-edge` / `nd edge sync` promotes it to HTTPS automatically.
  - Once rendered, `DNSGate` sticky `dns_drift` keeps serving existing sites rather than dropping them on transient DNS issues.
  - Standalone Shield fallback: when `cfg.ShieldIPs` and panel node IPs are empty, fall back to `ops.localPublicIPs()`.
- **Conflict ownership:**
  - `nd edge domain add` / `set` / `restore` / `import` refuse any host claimed by a panel (`claims.json`) or defined in `user/*.conf`, `user/*.caddy`, or unmanaged `Caddyfile` blocks (`StripManagedRegion`).
  - `Reconciler` excludes any panel host claimed by a manual domain and marks that panel host `conflict` (`"claimed by manual domain"`), preventing duplicate virtual hosts in Caddy/Angie/Ferron.
- **Garbage detection (`check` & `prune`):**
  - Added `DNSGate.InspectHostFresh` (bypasses backoff cache, reuses `queryOneServer` / `querySystemFallback` / `resolversAgree` / `classifyResolvedIPs`) to classify DNS as `ok`, `ok_proxied` (Cloudflare), `elsewhere`, `nxdomain`, `no_records`, `pending_caa`, or `error`.
  - Origin check performs TCP dial + HTTP(S) probe (`HEAD`/`GET /` with `Host` and TLS SNI + optional `InsecureSkipVerify`), classifying `up`, `refused`, `timeout`, `tls_error`.
  - TLS check reuses `CertProbe.ProbeHost` (`valid`, `expiring`, `missing`, `acme_failing`).
  - Persistent bad-state tracking in `/var/lib/nd-edge/domains.health.json` records `first_bad_dns_at` / `bad_dns_count` and `first_bad_origin_at` / `bad_origin_count`. `prune` requires `bad_count >= 2` and duration `>= --grace` (default `7d`), and never prunes `ok_proxied` or panel domains.

---

## 2. Per-Engine Option Mapping

| Feature / Flag | Caddy (`zz-manual.caddy`) | Angie (`zz-manual.conf`) | Ferron 3 (`zz-manual.conf`) |
|---|---|---|---|
| TLS `auto` | `<hosts> { ... }` (Caddy ACME) | Per-entry `acme_client <id>` sharing `account_key` (plus `LoadUsableManualCert` reuse when imported cert >30d valid) | `tls { provider acme; challenge http-01; cache ... }` (or `provider manual` for imported certs >30d valid) |
| TLS `none` (HTTP-only) | `http://<host>` addresses | `listen 80; listen [::]:80;` only | `http <host>:80 { ... }` |
| TLS `manual` | `tls <cert> <key>` | `ssl_certificate` / `ssl_certificate_key` (port 443 + optional port 80) | `tls { provider manual; cert ...; key ... }` |
| `--force-https=false` (with TLS) | Separate `http://` + `https://` blocks | Port 80 `server` proxies directly instead of `return 308` | `https_redirect false` inside host block |
| `--redirect-www to-apex/to-www` | Separate host block with `redir https://<dest>{uri} 308` | Separate `server` block (80 + 443 when TLS) with `return 308 https://<dest>$request_uri;` | Separate host block with `status 308 { location "https://<dest>{{request.uri}}" }` |
| Multiple upstreams + `--lb` | `lb_policy round_robin \| least_conn \| ip_hash` | `upstream` block with default RR, `least_conn;`, or `ip_hash;` | `algorithm round_robin` / `algorithm least_conn` / `affinity ip` |
| `--origin-http` / `--origin-https` / `--no-verify` / `--sni` | `http://` or `https://` upstream + `transport http { tls; tls_insecure_skip_verify; tls_server_name ... }` | `proxy_pass http://` or `https://` + `proxy_ssl_server_name on; proxy_ssl_name ...; proxy_ssl_verify off|on;` (`proxy_ssl_trusted_certificate /etc/ssl/certs/ca-certificates.crt;`) | `upstream "http://..."` or `"https://..."` + `no_verification` (note: Ferron uses upstream URL host for SNI; documented caveat when `--sni` overrides an IP/different host) |
| `--host-header` | `header_up Host {host}` / `{upstream_hostport}` / `<val>` | `proxy_set_header Host $host` / `$proxy_host` / `"<val>"` | Preserved by default (`original`); `request_header Host "<upstream>"` / `"<val>"` |
| `--websocket` | Native HTTP/1.1 upgrade in `reverse_proxy` | `Connection $nd_connection_upgrade; Upgrade $http_upgrade;` when on, `Connection "";` when off | Native WebSocket pass-through in `proxy` |
| `--max-body` | `request_body { max_size <bytes> }` | `client_max_body_size <bytes>;` (`0` = unlimited) | Comment note (Ferron 3 has no hard max body reject directive) |
| `--timeout` / `--connect-timeout` | `response_header_timeout` / `dial_timeout` | `proxy_read_timeout` / `proxy_send_timeout` / `proxy_connect_timeout` | Host `http { timeout "<dur>" }` (note: H2 honors per-host; H1 uses global `3660s`) + upstream `connection_timeout "<dur>"` |
| `--real-ip cloudflare\|none` | `{client_ip}` vs `{remote_host}` + `-CF-Connecting-IP`; `anyTrustedProxy` fed into `RenderGlobal` | `$remote_addr` vs `$realip_remote_addr` + `CF-Connecting-IP ""`; **never** emits duplicate `set_real_ip_from` in site file; sets `anyTrustedProxy` for `global.conf` | `X-Real-IP` / `X-Forwarded-For` `{{remote.ip}}` + `-CF-Connecting-IP` when `none`; sets `anyTrustedProxy` for `global.conf` |
| `--header-up` / `--header-down` | `header_up K "V"` / `header K "V"` | `proxy_set_header K "V";` / `add_header K "V" always;` (inside `location /` alongside `Alt-Svc` to avoid `add_header` override drop) | `request_header K "V"` / `header K "V"` |
| `--keepalive N` | `keepalive_idle_conns_per_host N` (or `keepalive off` when `0`) | `keepalive N;` in `upstream` (omitted when `0`) | `keepalive` (or `keepalive false` when `0`) |

---

## 3. Design Decisions & Open Questions
1. **DNSGate policy for `--tls auto` manual domains (§5.1):** Matches panel routes by excluding `pending_dns` / `pending_caa` hosts from `zz-manual.*` until DNS points to the Shield (or Cloudflare), preventing failed ACME issuance attempts while still surfacing `pending_dns` / `pending_caa` in `nd edge status` and `nd edge domain check`. Once rendered, `DNSGate` sticky `dns_drift` keeps serving existing sites rather than tearing them down on transient DNS issues. `nd edge domain show --config` and `--dry-run` render the full config regardless so operators can preview before DNS propagation.
2. **`proxy_ssl_trusted_certificate` on Angie:** Probes `/etc/ssl/certs/ca-certificates.crt` (Debian/Ubuntu/Alpine) then `/etc/pki/tls/certs/ca-bundle.crt` (RHEL/Alma/Rocky), defaulting to `/etc/ssl/certs/ca-certificates.crt`.
3. **Ferron 3 engine caveats (verified against `ferron:3-alpine validate`):** Ferron 3 does not enforce per-route `max_body` and derives origin TLS SNI from the upstream URL host rather than an arbitrary `--sni` override when connecting by IP; deterministic `# note:` comments are emitted in `zz-manual.conf` when those options are set so the operator sees why.
4. **Traffic column in `nd edge domain check`:** Reported as `unknown` (as allowed by §6) when no unified per-host log/counter aggregator is active across Caddy, Angie, and Ferron. Future work can populate counters from Angie's `status_zone` API, Ferron admin metrics, or Caddy admin metrics.
5. **Prune systemd schedule (`--schedule daily|off`):** Writes/removes `/etc/systemd/system/nd-edge-domain-prune.{service,timer}` running `nd edge domain prune --dry-run --json` (or `--apply --yes` only when `--auto-apply` is explicitly passed) via `edgeOps`.
