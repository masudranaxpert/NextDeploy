# nd edge — Multi-Panel Shield: Full Implementation Plan

> Audience: an implementation agent working in this repo (`D:\Project\NextDeploy`).
> Read the whole file before writing code. Sections marked **VERIFY** must be
> checked against the real tool behaviour (run `caddy adapt` / `caddy validate`)
> before relying on them.

---

## 0. Goal, scope, non-goals

### Goal
One Shield VPS (Caddy) fronts **N NextDeploy panels** (2–3 today, design for 50+),
where each panel has some domains **Direct** (`shield_node_id IS NULL`) and some
**Shielded** (`shield_node_id = <this shield>`). The user only sets a DNS A record
to the Shield IP; everything else (routing, SSL issue/renew, reboot recovery,
add/remove/move domains, add/remove panels) is automatic.

### Root cause being fixed
Today `GetEdgeShieldSetupScript` (`internal/handlers/edge_shield.go` ~L194–540)
writes a catch-all `https:// { tls { on_demand } reverse_proxy https://<ONE origin> }`
plus a global `on_demand_tls { ask <ONE panel> }`. Consequences:
- Only one panel/origin per Shield (single `ask`, single `origin_ip`).
- Certs are obtained **during the TLS handshake**; behind Cloudflare the handshake
  times out → 525, and users had to switch to Flexible to bootstrap a cert.
- Shield depends on the panel being up for every new cert (`ask`).
  (Caddy community has documented total outages when `ask` is down at restart.)

### Decision (locked — do not re-litigate)
- **No `on_demand`, no `ask`, no catch-all.** Every host is written **by name**
  into a Caddy file, exactly like a hand-written Caddyfile. Caddy then obtains
  certs in the background via normal Automatic HTTPS (HTTP-01 / TLS-ALPN-01).
- **One file per panel**: `/etc/caddy/nd/panels/<panel_id>.caddy`.
- **Pull model**: the Shield (`nd edge run`) polls each panel's HTTPS API.
  The Shield opens **no new inbound port** (only 80/443/22).
- **Caddy files are the source of truth** on the Shield. Caddy restart/reboot,
  `apt upgrade caddy`, manual `caddy reload` — all safe, nothing lives only in memory.
- `nd edge` is a **subcommand of the existing `nd` CLI** (`cmd/nd`, Go module `nd`),
  run as a systemd service on the Shield. No new product/binary.
- No fallback certs, no CNAME delegation, no Flexible-mode hacks
  (researched and rejected: not industry practice for a self-hosted panel).

### Non-goals (v1)
- Wildcard certs (`*.example.com`) — need DNS-01; mark `unsupported`.
- Same domain active on multiple Shields at once (GeoDNS active-active).
- Cloudflare **Flexible** mode support (causes redirect loop; detect + warn only).
- mTLS Shield→Origin (Phase 4, optional, designed but not required for v1).
- WireGuard backhaul (future).

---

## 1. Architecture

```
Visitor ──► (optional Cloudflare) ──► Shield Caddy :80/:443 ──HTTPS──► Origin Caddy (panel N)
                                          ▲
                                          │ writes files + `caddy reload`
                                     nd edge run (systemd)
                                          │ HTTPS poll (Bearer shield token)
                                          ▼
                        Panel 1 API   Panel 2 API   Panel 3 API
```

Legs and certs:
| Leg | Cert used |
|---|---|
| Visitor → Cloudflare (orange) | Cloudflare edge cert (automatic) |
| Visitor/Cloudflare → Shield | Let's Encrypt cert obtained by Shield Caddy per host |
| Shield → Origin | Origin Caddy `tls internal` cert; Shield trusts that panel's origin root CA (pinned per panel) |

### Shield filesystem layout
```
/etc/caddy/Caddyfile                      # nd-managed region between markers (see §5.1)
/etc/caddy/nd/global.opts                 # tokens included inside the global options block (VERIFY import-in-global)
/etc/caddy/nd/common.caddy                # shared snippets (nd_common, error pages)
/etc/caddy/nd/panels -> panels.gen-000123 # symlink, swapped atomically
/etc/caddy/nd/panels.gen-000123/
    00-placeholder.caddy                  # always present so the glob never matches zero files
    <panel_id>.caddy                      # one per registered panel
/etc/caddy/nd/origins/<panel_id>.pem      # that panel's origin root CA (0640 root:caddy)
/etc/caddy/nd/user/*.caddy                # user's own non-ND sites (migrated, never touched)
/etc/nd-edge/config.json                  # 0600 root: edge_id, panels[], settings
/var/lib/nd-edge/state/<panel_id>.json    # last-good snapshot + etag + counters
/var/lib/nd-edge/claims.json              # host -> {panel_id, claimed_at}
/var/lib/nd-edge/dns-cache.json           # DNS gate results
/var/lib/nd-edge/cf-ips.json              # Cloudflare ranges (refreshed daily)
/run/nd-edge.lock, /run/nd-edge.pid
```
Caddy cert storage stays default (`/var/lib/caddy/.local/share/caddy`) and is
**never deleted** by nd edge except on explicit `--purge-certs`.

---

## 2. Panel-side work

### 2.1 Phase 0 — existing bugs to fix first (independent of nd edge)
1. `syncShieldProxiesToCaddy` (`edge_shield.go` ~L900): set `opts.RealIP = "custom"`
   (otherwise `Normalize()` drops the Shield IPs — verify in `options.go`), prune IPs of
   deleted/changed Shield nodes, then trigger the root stack apply in background
   (find the existing `applyRootStackComposeBackground`/`syncRootStackCompose` path).
   Note current code returns early when `len(nodes)==0` — that prevents pruning the
   last removed IP; fix.
2. `DeleteShieldNode` and `ReassignShieldNodeDomains`: after DB changes, re-sync every
   affected app/PHP site/proxy route so origin labels switch between `tls internal`
   (shielded) and normal ACME (direct). Use the existing per-app
   `SyncAndApplyBackground`, the PHP route sync and the proxy-route reconcile
   functions (find exact names with grep; do not invent new ones).
3. `ReassignShieldNodeDomains` sslip/nip `REPLACE`: currently runs after the
   `shield_node_id` update and filters by the *target* id, so it also rewrites domains
   that were already on the target. Capture affected row IDs **before** the update and
   only rewrite those.
4. `ResolveUserShield` (~L798): admin vs non-admin inconsistency (non-admin silently
   gets the first node). Make it explicit: only `default_shield_id`; no implicit pick.
5. Origin Caddy labels: add `stream_close_delay 10m` to every generated
   `reverse_proxy` (app domains, PHP, proxy routes) — today every deploy/reload drops
   all WebSockets. Find label generators in `internal/caddy/caddy.go`
   (`appendSiteLabels`), `php_labels.go`, `routes.go`.
6. UI + CLI: Direct/Shield selector everywhere a domain is created/edited
   (`CaddyRoutesView.tsx`, `nd domain add`, `nd proxy add`); fix
   `cmd/nd/cmd/shield.go` `setup-script` token handling and `reassign` help text.
7. `EdgeVerifyDomain` (legacy `ask`): add `AND shield_node_id = node.ID` filters and
   include active preview hosts — only needed while old Shields still run the legacy
   config; mark deprecated.
8. New panels: generate persistent `panel_id` (UUIDv4) in `settings` on first boot
   (`GetSetting/SetSetting`); never regenerate.
9. Shielded auto-domains (sslip.io/nip.io) for **new** apps: include a short panel id
   so two panels with the same app name on the same Shield IP don't collide:
   `<app>-<pid6>-<dashed-shield-ip>.sslip.io` (`FormatWildcardDomain` in
   `internal/caddy/caddy.go`; keep the digit-suffix rule). Existing domains unchanged.

### 2.2 DB changes (`internal/db`, idempotent migrations like `migrateShieldNodes`)
`shield_nodes` add columns:
`edge_id TEXT`, `edge_version TEXT`, `caddy_version TEXT`, `registered_at DATETIME`,
`last_sync_at DATETIME`, `last_applied_etag TEXT`, `last_error TEXT`,
`edge_status_json TEXT` (latest raw status blob, capped 256 KB),
`token_prev TEXT`, `token_prev_expires_at DATETIME` (rotation grace).

New table:
```sql
CREATE TABLE IF NOT EXISTS shield_domain_status (
  node_id INTEGER NOT NULL,
  host TEXT NOT NULL,
  state TEXT NOT NULL,          -- see §3.6 state list
  detail TEXT,
  cert_not_after DATETIME,
  updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (node_id, host)
);
```

### 2.3 New panel API (all under `/api/v1/edge/`, versioned)
Auth: `Authorization: Bearer shld_...` (header, **not** query string — query tokens
leak into access logs). Accept `token_prev` until `token_prev_expires_at`.
Every response carries `X-ND-Edge-API: 1`. Rate-limit per token (e.g. 60 req/min).

**POST /api/v1/edge/register**
Request:
```json
{ "edge_id":"uuid", "edge_version":"x.y.z", "caddy_version":"v2.x",
  "hostname":"shield-sg", "public_ipv4":["107.189.6.222"], "public_ipv6":[],
  "force": false }
```
Response 200:
```json
{ "panel_id":"uuid", "panel_name":"vps-1", "shield_node_id": 3,
  "poll_interval_sec": 30, "api_version": 1 }
```
Rules: same `edge_id` → idempotent. Different `edge_id` while the node is bound and
heartbeat < 10 min → `409 {"error":"token_in_use"}` unless `force:true`.
Warn (don't fail) if request source IP ∉ node.IP and ∉ public_ipv4.

**GET /api/v1/edge/routes** (`If-None-Match: <etag>` → 304)
```json
{ "api_version": 1, "panel_id":"uuid", "etag":"sha256…",
  "origin": { "ips":["204.136.10.51"], "port":443,
              "ca_pem":"-----BEGIN…", "ca_sha256":"…", "ca_status":"ok|missing" },
  "routes": [
    { "id":"dom_12", "kind":"app_domain|php_site|proxy_route|preview|panel",
      "hosts":["app.example.com","www.app.example.com"],
      "options": { "websocket": true, "max_body_mb": 0, "upstream_h2": false } }
  ],
  "node": { "id":3, "ip":"107.189.6.222", "deleted": false } }
```
- `etag` = sha256 of canonical JSON (sorted keys, sorted routes by id, sorted hosts).
- Source rows: `app_domains WHERE shield_node_id = node.ID` (both Docker apps and
  PHP sites live here), `proxy_routes WHERE enabled=1 AND shield_node_id = node.ID`,
  active preview hosts (`previewSite` for each app/slot whose resolved shield is this
  node), panel domain only if the admin explicitly shields it.
- Expand `enable_www` into explicit `www.` hosts. Lowercase, strip trailing dot,
  convert IDN to punycode (`golang.org/x/net/idna`), dedupe across rows
  (duplicate → keep first, log).
- Node deleted → `410 Gone {"reason":"node_deleted"}`.
- Origin CA from the existing `GetOriginCaddyRootCA` logic, refactored into a
  function. If missing → `ca_status:"missing"` (see edge case E-ORIGIN-3).

**POST /api/v1/edge/status** — body is the status document of §3.6. Panel upserts
`shield_domain_status`, updates `last_sync_at`, `edge_version`, `caddy_version`,
heartbeat, `last_error`. Body limit 2 MB.

**POST /api/v1/edge/unregister** — `{edge_id, reason}` → node status `unregistered`.

Keep legacy endpoints (`verify-domain`, `setup.sh`, `uninstall.sh`,
`shield-heartbeat`, `origin-ca.crt`) working for old Shields; mark deprecated.

### 2.4 Panel UI
- Shield node card: edge version, Caddy version, last sync, sync state
  (ok / stale / auth_failed / outdated), conflicts count, "Rotate token",
  setup + upgrade + uninstall commands.
- Per-domain badge in domain tabs (`ProjectDomainsTab.tsx`, `PhpDomainsTab.tsx`,
  routes view): `Active (expires …)`, `Waiting for DNS → set A record to X`,
  `Issuing certificate…`, `Certificate error: <detail>`, `Conflict: claimed by
  another panel`, `Unsupported (wildcard)`.
- Hints: DNS shows Cloudflare IPs and cert pending > 15 min →
  "If Cloudflare 'Always Use HTTPS' + Full (strict) is on, turn Always Use HTTPS off;
  we redirect to HTTPS ourselves." Detect Flexible loop (see E-CF-4) → "Set SSL mode
  to Full or Full (strict)".

---

## 3. `nd edge` (Shield side)

### 3.1 CLI (`cmd/nd/cmd/edge.go`, logic in `cmd/nd/internal/edge/`)
```
nd edge install       --panel-url URL --token TOKEN [--acme-email E] [--adopt-existing]
nd edge add-panel     --panel-url URL --token TOKEN [--force]
nd edge remove-panel  <panel_id|name> [--purge-certs]
nd edge list                                  # panels, sync state, host counts
nd edge status [--json] [host]                # per-host state, cert expiry, last errors
nd edge sync  [--panel ID] [--force]          # immediate reconcile (signals daemon)
nd edge run                                   # daemon (systemd ExecStart)
nd edge doctor                                # all health checks, human readable
nd edge set-token <panel_id> --token TOKEN
nd edge upgrade                               # self-update with sha256 verification
nd edge uninstall [--all] [--purge-certs]
```
- Token input: accept `--token-stdin` and env `ND_EDGE_TOKEN`; never echo it.
- Mutating commands take `/run/nd-edge.lock` (flock), edit config atomically, then
  `systemctl reload nd-edge` (SIGHUP → immediate reconcile).
- The `nd` module is separate from `panel`; duplicate the API structs in
  `cmd/nd/internal/edge/api.go` and keep a shared golden JSON fixture tested on both
  sides (contract test) so they cannot drift.

### 3.2 Daemon loop (`nd edge run`)
```
startup:
  acquire flock (exit if another instance)
  load config, state, claims (corrupt file → see E-STATE-*)
  NEVER rewrite Caddy files at startup from empty state
  ensure Caddy is running (report only; see E-CADDY-1)
loop per panel (goroutine, independent):
  every poll_interval (default 30s, ±20% jitter; error backoff 30s→5m):
    GET /routes with If-None-Match
    200 → validate & store as candidate snapshot
    304 → nothing
    401/403 → state auth_failed, keep last-good, keep polling at 5m
    404 on /routes → panel_outdated, keep last-good
    410 → after 3 consecutive 410s spanning ≥10 min: release panel (remove file)
    5xx/timeout/TLS error → keep last-good
  signal reconciler
reconciler (single goroutine, debounced 3s, max 1 apply per 5s):
  desired = for each panel: last-good snapshot
  apply mass-deletion guard (§3.4)
  host validation (§3.3) → excluded hosts get state
  DNS gate (§3.5)
  claims / conflict resolution (§3.4)
  admission throttle for brand-new hosts (§3.4)
  render all panel files (§5) → if bytes identical to current gen: stop
  write new generation dir, `caddy validate` it, swap symlink, `caddy reload`
  on reload failure: swap symlink back, reload again, report error
status loop: every 60s (and after each apply) build status (§3.6), POST to each panel
cert probe loop: every 2 min for pending hosts, every 6 h for active hosts
cf-ips loop: every 24 h refresh Cloudflare ranges (fallback: embedded list)
```

### 3.3 Host validation (security critical — prevents Caddyfile injection)
- After IDN→punycode and lowercase: must match
  `^(?=.{1,253}$)([a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z0-9-]{2,63}$`
  (implement without lookahead in Go: check length separately).
- Reject: wildcards (`unsupported_wildcard`), IP literals, `localhost`,
  `.local/.localhost/.internal/.test/.example/.invalid/.lan/.home.arpa`
  (`excluded_internal`), any byte outside `[a-z0-9.-]`, labels starting/ending `-`,
  hosts that are the Shield's own management name.
- Origin IPs: `net.ParseIP`, reject loopback/link-local/unspecified/multicast and the
  Shield's own IPs (loop prevention → `excluded_loop`).
- PEM: parse with `x509`, must be a CA cert; cap 64 KB.
- Never interpolate any panel-provided string other than validated hosts and IPs into
  Caddy files. Route ids are used only after `[a-z0-9_-]{1,64}` validation.

### 3.4 Multi-panel safety
**Claims / conflicts**
- `claims.json`: `host → {panel_id, claimed_at}`. A host belongs to the first panel
  that successfully had it rendered. Another panel listing it gets
  `conflict` (detail: owner panel name) and the host is not rendered for it.
- A claim is released when the owner's last-good snapshot no longer contains the host
  (after the mass-deletion guard) or the owner panel is removed. Next reconcile gives
  it to the next claimant (domain move between panels completes within ~1 poll).
  Certificates are stored per host, so no re-issue on move.
- Ties (two new claimants in the same reconcile): earlier `registered_at`, then
  lexical `panel_id`.

**Mass-deletion guard**
- If a new snapshot would remove > 50 % of a panel's rendered hosts **and** > 5 hosts
  (or all hosts), hold it: apply only after the same etag is seen in 3 consecutive
  polls spanning ≥ 3 min. `nd edge sync --force` bypasses. Additions are never held.

**Admission throttle (Let's Encrypt limits)**
- Hosts that already have a valid cert in storage: no limit.
- Brand-new hosts: at most 50 per panel per 10 min and 250 per Shield per 3 h
  (LE: 300 new orders / 3 h per account; 50 certs / registered domain / week;
  5 failed authorizations / identifier / hour). Deferred hosts get
  `queued_rate_limit`.
- Per-panel cap on total hosts (default 2000, configurable) → `excluded_quota`.

### 3.5 DNS gate
- Resolve A/AAAA via public resolvers (1.1.1.1 and 8.8.8.8, DoH or plain UDP with
  TCP fallback), not the system cache. Use `github.com/miekg/dns` (also for CAA).
- Outcomes per host:
  - A/AAAA ⊆ Shield IPs → `dns_ok`
  - any A in Cloudflare ranges → `dns_ok_proxied` (origin hidden; trust it)
  - sslip.io/nip.io → compute embedded IP; must equal a Shield IP (no lookup needed)
  - NXDOMAIN / no A / other IPs → `pending_dns` (detail: what it points to)
  - AAAA present pointing elsewhere while A is correct → `pending_dns`
    ("remove or fix AAAA; Let's Encrypt prefers IPv6")
  - CAA present that excludes `letsencrypt.org` (and zerossl if enabled) →
    `pending_caa`
- `pending_*` hosts are not rendered (prevents useless ACME failures/rate limits).
  Re-check every 60 s for 1 h, then every 5 min, then every 30 min after 24 h.
- **Sticky**: once a host is rendered (`active` or issuing), a DNS change never
  removes it automatically — only the panel can remove it. (Avoids outages from
  resolver blips.) Report `dns_drift` as a warning instead.
- `--dns-gate=off` config switch for exotic setups (split-horizon DNS).

### 3.6 Status document (POST /status) and states
```json
{ "edge_id":"…", "edge_version":"…", "caddy_version":"…", "caddy_running":true,
  "applied_etag":"…", "last_apply_at":"…", "last_apply_error":"",
  "uptime_sec": 0, "disk_free_pct": 0, "mem_free_mb": 0, "clock_skew_sec": 0,
  "hosts":[ { "host":"app.example.com", "state":"active",
              "detail":"", "cert_not_after":"2027-01-01T00:00:00Z",
              "issuer":"Let's Encrypt" } ],
  "conflicts":[ { "host":"x.com", "owner_panel":"vps-2" } ] }
```
States: `active`, `issuing`, `cert_error`, `pending_dns`, `pending_caa`,
`dns_drift`, `conflict`, `queued_rate_limit`, `unsupported_wildcard`,
`excluded_internal`, `excluded_loop`, `excluded_quota`, `held_mass_delete`.
Only hosts of the receiving panel are sent to that panel (never leak other panels'
domains; conflicts show only the owner panel's display name).

### 3.7 Cert status detection
- Local TLS probe: dial `127.0.0.1:443` with SNI=host, `InsecureSkipVerify`, then
  verify the leaf manually against system roots + hostname. Valid → `active`
  (+ NotAfter, issuer). Handshake error or self-signed → `issuing`.
- `issuing` > 15 min → `cert_error`, detail from
  `journalctl -u caddy -o json --since -60min` entries whose `logger` starts with
  `tls.` and whose `identifier` == host; classify: `unauthorized/403` (wrong IP /
  proxy in path), `rateLimited`, `caa`, `dns`/NXDOMAIN, `timeout`, `connection`.
  Best-effort; never fail if journald is unavailable.
- Expiry < 14 days on an `active` host → warning `cert_renew_overdue`.

---

## 4. Install / upgrade / uninstall on the Shield

### 4.1 `nd edge install` (new `setup.sh` from the panel becomes a thin wrapper:
download `nd` via `install-nd.sh` logic, then `nd edge install --panel-url … --token-stdin`)
Steps, each idempotent:
1. Preconditions: root; systemd present; supported OS (Debian/Ubuntu, RHEL family);
   ports 80/443 not bound by another process (`ss -ltnp`) → abort with name of
   the process; clock skew < 5 min (warn, abort if > 1 h); disk free > 1 GB.
2. Install Caddy from the official repo if missing; record version. Don't hold the
   package (no experimental features used).
3. Ensure `admin localhost:2019` is effective (reload depends on it). If the user's
   Caddyfile has `admin off` or a different address, record it and use it, or abort
   with a clear message for `off`.
4. Caddyfile handling:
   - Missing/empty → write nd template (§5.1).
   - Contains the **legacy** NextDeploy catch-all markers
     (`# === NextDeploy Edge Shield Catch-all` … `# === End …`) and/or an
     `on_demand_tls { ask …/api/v1/edge/verify-domain… }` → remove those exactly,
     keep everything else; existing certs in storage get reused by explicit sites.
   - Contains other user sites → require `--adopt-existing`: back up to
     `/etc/caddy/Caddyfile.nd-backup.<ts>`, insert nd marker region with the
     `import` lines; if a global options block exists, insert nd lines inside it
     (between markers); if it already defines `servers { trusted_proxies … }` skip
     ours and warn (see E-RIP-3).
   - Always run `caddy validate` before replacing; on failure restore backup.
5. Create dirs/permissions: `/etc/caddy/nd/**` dirs 0750 root:caddy, files 0640
   root:caddy (Caddy runs as user `caddy` and must read pem files). SELinux: run
   `restorecon -R /etc/caddy` if `getenforce` = Enforcing.
6. Firewall: if ufw/firewalld active, open 80/tcp, 443/tcp, 443/udp. Print a
   reminder about cloud-provider firewalls.
7. Write `/etc/nd-edge/config.json` with a new `edge_id`.
8. `add-panel` flow (§4.2).
9. Install `nd-edge.service`:
   ```ini
   [Unit]
   Description=NextDeploy edge sync
   After=network-online.target caddy.service
   Wants=network-online.target caddy.service
   [Service]
   ExecStart=/usr/local/bin/nd edge run
   ExecReload=/bin/kill -HUP $MAINPID
   Restart=always
   RestartSec=5
   LimitNOFILE=65536
   [Install]
   WantedBy=multi-user.target
   ```
   Not `Requires=caddy` — nd edge must run (and report) even if Caddy is down.
10. `systemctl enable --now caddy nd-edge`; run first reconcile synchronously and
    print a summary (hosts rendered / pending DNS / conflicts).

### 4.2 `add-panel`
POST `/register` → store `{panel_id, name, url, token, registered_at}`; first
`/routes` fetch must succeed before the panel is added (otherwise abort, nothing
written). If `panel_id` already registered with a different URL → update URL (panel
moved) after confirming via a successful `/routes` call.

### 4.3 `remove-panel`
Remove `<panel_id>.caddy` + origin pem in a new generation, validate, swap, reload,
release claims, POST `/unregister` (best effort), drop state. Certs kept unless
`--purge-certs` (only for hosts not used by any remaining panel).

### 4.4 `uninstall`
Refuse while other panels remain unless `--all`. Remove nd region from Caddyfile
(restore backup if it was adopted), validate, reload (or stop Caddy if nothing is
left and Caddy was installed by us), disable `nd-edge.service`, delete
`/etc/nd-edge` and `/var/lib/nd-edge`. Keep cert storage unless `--purge-certs`.

### 4.5 `upgrade`
Download new `nd` from the panel (or release URL) to a temp file, verify sha256 /
signature, `nd version` smoke test, atomic rename over `/usr/local/bin/nd`,
`systemctl restart nd-edge`. On failure keep old binary. Never auto-upgrade by default.

---

## 5. Caddy config templates

### 5.1 Caddyfile (nd region)
```caddy
# >>> nd-edge managed (do not edit inside markers) >>>
{
	admin localhost:2019
	# email set only if --acme-email given (enables ZeroSSL fallback issuer)
	import /etc/caddy/nd/global.opts
}
import /etc/caddy/nd/common.caddy
import /etc/caddy/nd/user/*.caddy
import /etc/caddy/nd/panels/*.caddy
# <<< nd-edge managed <<<
```
**VERIFY**: `import` inside the global options block works with the installed
Caddy (`caddy adapt`). If not, render the global options inline between markers.
**VERIFY**: behaviour of an `import` glob matching zero files; we always keep
`00-placeholder.caddy` and an empty `user/00-placeholder.caddy` regardless.

### 5.2 `global.opts`
```caddy
servers {
	trusted_proxies static <Cloudflare IPv4 and IPv6 ranges, one line>
	client_ip_headers CF-Connecting-IP X-Forwarded-For
}
```
So `{client_ip}` is the real visitor IP when Cloudflare is in front, and the
direct visitor IP otherwise. Ranges from `cf-ips.json` (fallback: the list already
embedded as `CloudflareRanges` in `internal/caddy` — copy it into the nd module).

### 5.3 `common.caddy`
```caddy
(nd_common) {
	encode zstd gzip
	handle_errors 502 503 504 {
		header Cache-Control "no-store"
		respond "Origin is temporarily unavailable. Please retry shortly." 503
	}
}
```
(Keep minimal; no security headers here — origin already sets them.)
**VERIFY**: `handle_errors` with status-code arguments needs a recent Caddy
(2.8+); otherwise use `handle_errors { @down expression {err.status_code} in [502,503,504] … }`.

### 5.4 `<panel_id>.caddy` (rendered, deterministic ordering)
```caddy
# Managed by nd edge for panel "vps-1" (<panel_id>). DO NOT EDIT. etag=<etag>
(nd_up_<pid8>) {
	reverse_proxy https://204.136.10.51:443 {
		header_up Host {host}
		header_up X-Real-IP {client_ip}
		header_up X-Forwarded-For {client_ip}
		header_up X-Forwarded-Proto https
		stream_close_delay 10m
		flush_interval -1
		lb_try_duration 5s
		lb_try_interval 250ms
		transport http {
			tls_trust_pool file /etc/caddy/nd/origins/<panel_id>.pem
			tls_server_name {host}
			versions 1.1
			dial_timeout 10s
			response_header_timeout 600s
			keepalive 90s
			keepalive_idle_conns 256
			keepalive_idle_conns_per_host 64
		}
	}
}

app.example.com, www.app.example.com {
	import nd_common
	import nd_up_<pid8>
}
```
- Multiple origin IPs → multiple upstreams in one `reverse_proxy` (+ `lb_policy first`).
- `upstream_h2:true` option → `versions h2 1.1` (gRPC) for that route only
  (needs a separate snippet variant).
- **VERIFY** placeholders `{host}` in `tls_server_name` and `header_up` (current
  production config already uses them).
- Origin CA missing (`ca_status:"missing"`) → render `tls_insecure_skip_verify`
  for that panel **temporarily**, state warning `origin_ca_missing`, auto-switch when
  the CA appears.
- Sorting: hosts sorted within a route, routes sorted by id → byte-stable output
  so unchanged inputs never trigger a reload.

### 5.5 Apply procedure (atomic, all-or-nothing)
1. Build `panels.gen-<n+1>/` with all files (+ placeholder) and write pem files
   (pem changes are part of the generation: `origins.gen-<n+1>/`, also symlinked).
2. Generate a temp Caddyfile identical to the real one but importing the new gen
   paths; `caddy validate --config <tmp> --adapter caddyfile` (timeout 60 s).
3. `fsync` files + dir; atomically swap symlinks (`symlink tmp` + `rename`).
4. `caddy reload --config /etc/caddy/Caddyfile --adapter caddyfile --address <admin>`
   (timeout 90 s). Success → record gen, keep the last 3 gens, delete older.
5. Failure → swap symlinks back, reload again, mark `last_apply_error`, exponential
   retry (1 m → 30 m). Two consecutive failures with the same desired state → stop
   retrying until the desired state changes.

---

## 6. Security checklist
- Shield token: 192-bit random (existing `GenerateShieldToken`); stored 0600;
  sent only in `Authorization`; rotation with 10 min grace (`token_prev`).
- Panel URL must be `https://` unless `--insecure-http` (warn loudly). Support
  certificate pinning (`--panel-ca-sha256`) for self-signed panel URLs.
- A compromised panel can only: add **unclaimed** hosts (squat) pointing at its own
  origin, and burn ACME quota → mitigated by claims, per-panel caps and admission
  throttle. It can never read other panels' hosts or change their routes.
- Strict host/IP/PEM validation (§3.3) — no raw strings into Caddy files.
- Don't log tokens; redact `Authorization` in debug logs.
- Panel endpoints: rate limit, body size limits, constant-time token compare.
- Origin IP is secret-ish: only returned to authenticated Shield tokens.

---

## 7. Edge cases (each must be handled and, where marked [T], covered by a test)

### Shield reboot / process lifecycle
- E-BOOT-1 [T] Shield reboots: Caddy starts from files, sites serve before nd edge
  or network is up.
- E-BOOT-2 [T] nd edge starts with the panel unreachable: no file changes; status
  retried with backoff.
- E-BOOT-3 nd edge starts before Caddy: reload attempts fail → retried; no revert loop.
- E-BOOT-4 [T] Caddy fails to start after reboot (e.g. port taken): nd edge reports
  `caddy_running:false` + reason from `systemctl status`.
- E-BOOT-5 Network-online but DNS not yet working: DNS gate keeps previous results.
- E-BOOT-6 [T] Two nd edge instances started (manual + systemd): second exits on flock.
- E-BOOT-7 SIGTERM during apply: apply is atomic; on restart re-run reconcile.
- E-BOOT-8 [T] Power loss mid-write: tmp files ignored, symlink still on old gen.
- E-BOOT-9 Clock wrong after reboot (no RTC/NTP): skew reported; ACME may fail; warn.
- E-BOOT-10 `/run` is tmpfs: lock/pid recreated each boot.

### Caddy behaviour
- E-CADDY-1 Caddy stopped/crashed: nd edge reports; tries `systemctl start caddy` at
  most once per 10 min.
- E-CADDY-2 [T] `caddy validate` fails: never swap; error reported with first 2 KB
  of output.
- E-CADDY-3 [T] Validate ok but reload fails (bind error, permission on pem):
  rollback gen, reload old, report.
- E-CADDY-4 Reload hangs > 90 s: kill command, check admin `/config/` reachability,
  treat as failure.
- E-CADDY-5 Admin endpoint disabled/moved by user: detected at install and every apply.
- E-CADDY-6 User runs `caddy reload` / `systemctl restart caddy` manually: harmless.
- E-CADDY-7 [T] User edits a panel file by hand: overwritten on next change; drift
  logged (hash mismatch) — header says DO NOT EDIT.
- E-CADDY-8 `apt upgrade caddy` restarts Caddy: harmless (files are truth).
- E-CADDY-9 Caddy major version removes a directive we use: validate fails → old gen
  keeps serving; `nd edge doctor` shows it.
- E-CADDY-10 Reload drops WebSockets: mitigated by `stream_close_delay 10m`; reloads
  only on real change.
- E-CADDY-11 Glob import with zero matches: placeholder files always present.
- E-CADDY-12 Cert storage lock left by a crash: Caddy handles stale locks; report if
  issuance stuck > 1 h.
- E-CADDY-13 Many hosts (2000+): reload time measured and reported; warn if > 10 s.
- E-CADDY-14 Caddy running as root vs `caddy` user: permissions computed from the
  actual `User=` in the unit.
- E-CADDY-15 HTTP/3 (UDP 443) blocked: TCP still works; doctor warns.

### Panel connectivity / API
- E-API-1 [T] Panel down for hours/days: routes and renewals continue (no `ask`).
- E-API-2 [T] 401/403 (token rotated/revoked): keep last-good; state `auth_failed`.
- E-API-3 [T] 404 (old panel version): `panel_outdated`; keep last-good.
- E-API-4 [T] 410 node deleted: release after 3×410 over ≥10 min.
- E-API-5 [T] 5xx / timeout / TLS error: keep last-good, backoff.
- E-API-6 [T] Malformed JSON / wrong `api_version` major: reject snapshot, keep last-good.
- E-API-7 [T] Snapshot with invalid hosts: only those hosts excluded; rest applied.
- E-API-8 Panel URL changed (new domain/IP): `nd edge set-url` or re-register by
  panel_id.
- E-API-9 Panel URL is itself routed through this Shield (panel domain shielded):
  if the panel host resolves to a Shield IP, dial the origin IP directly with
  SNI/Host = panel host (custom dialer) to avoid the loop.
- E-API-10 Panel behind Cloudflare with bot protection/challenge: detect HTML
  challenge response → error `panel_blocked_by_waf`, suggest allowlisting Shield IP.
- E-API-11 Panel uses self-signed HTTPS: require `--panel-ca-sha256` pin.
- E-API-12 [T] Panel returns 304 forever after DB restore with same etag: fine.
- E-API-13 Panel DB restored from old backup: new snapshot treated normally; mass
  delete guard protects against sudden large removals.
- E-API-14 Panel temporarily returns empty routes (bug/migration): mass-deletion guard.
- E-API-15 Slow panel (>10 s): per-request timeout 20 s; other panels unaffected
  (independent goroutines).
- E-API-16 Token used by two Shields: register 409 `token_in_use`.
- E-API-17 Token leaked: rotate in UI; old token valid 10 min only.
- E-API-18 Status POST fails: routes still applied; status retried next cycle.

### Multi-panel
- E-MP-1 [T] Two panels claim the same host: first claimant wins; second `conflict`.
- E-MP-2 [T] Domain moved panel A → B: B gets it after A drops it; cert reused.
- E-MP-3 [T] Panel removed: only its file/pem/claims removed; others untouched; no
  restart.
- E-MP-4 [T] Same app name on two panels → sslip collision: prevented for new apps
  by panel id in the name; old ones surface as `conflict`.
- E-MP-5 [T] One panel's invalid data never blocks others' apply (per-panel
  validation; only a Caddy-level validate failure blocks the whole gen — then retry
  excluding the newest changed panel to isolate the offender).
- E-MP-6 Panels with different api versions: edge supports the min..max range.
- E-MP-7 Same origin IP used by two panels (two panels on one VPS? not supported):
  allowed; routes are per host anyway.
- E-MP-8 Panel re-installed with a new `panel_id` but same token: register returns new
  panel_id → old panel entry replaced, claims transferred (same node id).
- E-MP-9 Per-panel quota exceeded: `excluded_quota`.
- E-MP-10 One panel floods with new domains: admission throttle protects others.

### DNS / domains
- E-DNS-1 [T] A record missing: `pending_dns`; auto-added when fixed.
- E-DNS-2 [T] A record points elsewhere: `pending_dns` with the actual IP.
- E-DNS-3 [T] Cloudflare proxied: `dns_ok_proxied`.
- E-DNS-4 [T] `www` missing DNS: base host rendered, `www` pending, added later.
- E-DNS-5 [T] Wrong AAAA: `pending_dns` (LE prefers IPv6).
- E-DNS-6 [T] CAA forbids Let's Encrypt: `pending_caa`.
- E-DNS-7 DNS propagation delay / resolver disagreement: require 2 resolvers agree;
  otherwise keep previous state.
- E-DNS-8 [T] DNS changed away after active: sticky; `dns_drift` warning only.
- E-DNS-9 Domain expired / NXDOMAIN later: sticky + warning; renewal fails in Caddy
  logs; panel shows cert_error near expiry.
- E-DNS-10 Wildcard host: `unsupported_wildcard`.
- E-DNS-11 IDN (unicode) host: punycode in Caddy, unicode in panel UI.
- E-DNS-12 Uppercase/trailing dot/duplicate hosts: normalized, deduped.
- E-DNS-13 Host equals Shield's own hostname or IP: excluded.
- E-DNS-14 sslip.io embedded IP ≠ Shield IP: `pending_dns`.
- E-DNS-15 Split-horizon DNS on Shield: use public resolvers; `--dns-gate=off`.
- E-DNS-16 Public resolvers blocked by firewall: fall back to system resolver, warn.
- E-DNS-17 CNAME chains / CNAME to Cloudflare-proxied target: follow chain (max 8).
- E-DNS-18 Very long host (>253) or label (>63): rejected.

### Certificates / ACME
- E-ACME-1 [T] New host, grey cloud: cert in < 1 min.
- E-ACME-2 Cloudflare Full / Full (strict) with Always Use HTTPS **off**: HTTP-01 on
  port 80 succeeds.
- E-ACME-3 Full (strict) + Always Use HTTPS **on**, brand-new host: may fail; Caddy
  retries with backoff (up to 30 days); after 15 min panel shows the hint.
- E-ACME-4 Let's Encrypt rate limit hit: classified `rateLimited`; admission throttle
  prevents in normal operation; renewals via ARI are exempt.
- E-ACME-5 5 failed validations/hour/host: DNS gate prevents most; Caddy backoff.
- E-ACME-6 ACME CA outage: existing certs keep serving; Caddy retries; optional
  ZeroSSL fallback when `--acme-email` set.
- E-ACME-7 Certs from the legacy on-demand setup: reused (same storage, same host).
- E-ACME-8 Cert storage deleted: everything re-issued under throttle; warn.
- E-ACME-9 Port 80 blocked by provider: TLS-ALPN-01 on 443 still possible (not
  behind Cloudflare); doctor checks port 80 reachability from outside via the panel's
  TCP probe.
- E-ACME-10 Renewal while panel offline: works (no panel dependency).
- E-ACME-11 Clock skew > 5 min: warn (ACME/TLS issues).
- E-ACME-12 sslip.io/nip.io limits: **VERIFY** whether sslip.io is on the Public
  Suffix List; if not, many sslip hosts share the 50/week registered-domain limit →
  surface `rateLimited` clearly and recommend custom domains.
- E-ACME-13 Expiring cert (< 14 days) not renewed: `cert_renew_overdue` warning.

### Cloudflare specifics
- E-CF-1 Real visitor IP: `trusted_proxies` = CF ranges + `client_ip_headers
  CF-Connecting-IP` → `{client_ip}` forwarded to origin.
- E-CF-2 [T] Spoofed `CF-Connecting-IP` from a non-CF IP: ignored (not trusted).
- E-CF-3 CF ranges change: refreshed daily; embedded fallback.
- E-CF-4 Flexible mode: Shield redirects HTTP→HTTPS → loop. Panel detects by
  fetching `http://host/` and following ≤ 10 redirects; loop → hint "use Full or
  Full (strict)". Not supported by design.
- E-CF-5 Cloudflare 100 s proxy timeout for long requests: document; not fixable here.
- E-CF-6 Cloudflare caching HTML of the panel's error page: our 503 page sends
  `Cache-Control: no-store`.

### Origin side
- E-ORIGIN-1 [T] Origin down: Shield serves the 503 page; other panels unaffected.
- E-ORIGIN-2 [T] Origin CA rotated (origin caddy_data wiped): new `ca_sha256` in
  snapshot → new pem → reload.
- E-ORIGIN-3 Origin CA not generated yet: temporary `tls_insecure_skip_verify` +
  `origin_ca_missing` warning; auto-fix later.
- E-ORIGIN-4 Origin IP changed (VPS migration): next snapshot updates upstream.
- E-ORIGIN-5 Domain switched Shield → Direct on the panel: removed from Shield file
  (guarded) and origin re-labels to ACME (Phase 0 #2).
- E-ORIGIN-6 Domain switched Direct → Shield: the origin must re-label the host to
  `tls internal` before the Shield routes to it, because the Shield trusts only the
  origin's internal CA (an origin still serving an ACME cert for that host → 502).
  The panel applies origin labels first and only then exposes the host in `/routes`
  (track a `pending_origin_apply` flag and omit the host until the apply finished).
- E-ORIGIN-7 Origin trusted proxies must include the Shield IP (Phase 0 #1) so
  the origin uses the forwarded client IP; direct sites cannot spoof it.
- E-ORIGIN-8 Large uploads / long responses: no body limit, 600 s header timeout,
  `flush_interval -1` (SSE).
- E-ORIGIN-9 WebSockets: supported; survive reload via `stream_close_delay`.
- E-ORIGIN-10 gRPC: needs `upstream_h2` route option.
- E-ORIGIN-11 Origin serving shielded host also on :80 directly: origin should
  redirect only (no proxying) for shielded hosts (Phase 4 hardening).

### State / files / resources
- E-STATE-1 [T] Corrupt `state/<panel>.json`: move aside, keep current Caddy files,
  refetch.
- E-STATE-2 [T] Corrupt `claims.json`: rebuild from current rendered files.
- E-STATE-3 [T] Corrupt `config.json`: refuse to apply (never delete files), loud
  error; `nd edge doctor` explains.
- E-STATE-4 Disk full: writes fail atomically; report `disk_full`; no partial gen.
- E-STATE-5 Old generations accumulate: keep last 3.
- E-STATE-6 Low memory VPS (512 MB): nd edge < 30 MB RSS target; no big caches.
- E-STATE-7 Inode exhaustion: same as disk full.
- E-STATE-8 Read-only filesystem (fs error): report, keep serving.

### Install / uninstall / upgrade
- E-INST-1 Ports 80/443 used by nginx/apache: abort with process name.
- E-INST-2 Existing user Caddyfile with sites: require `--adopt-existing`, back up,
  preserve.
- E-INST-3 Legacy NextDeploy catch-all present: removed precisely; certs reused.
- E-INST-4 Re-running install: idempotent.
- E-INST-5 Unsupported OS / no systemd: abort with message.
- E-INST-6 Firewall closed: open with ufw/firewalld; remind about cloud firewall.
- E-INST-7 Token in shell history: use `--token-stdin` in generated commands.
- E-INST-8 Install interrupted halfway: next run resumes (each step idempotent).
- E-INST-9 Uninstall with other panels present: refuse unless `--all`.
- E-INST-10 Upgrade binary corrupt: sha256 check; keep old binary.
- E-INST-11 Downgrade: allowed if api version compatible.
- E-INST-12 SELinux enforcing: `restorecon`.

---

## 8. Testing

### 8.1 Unit (cmd/nd/internal/edge, go test)
- Render golden files: 0/1/many routes, multiple origin IPs, missing CA, h2 option,
  deterministic ordering, byte stability.
- Host validation table + fuzz test (`FuzzValidateHost`): no output may contain
  `{ } \n \r " ' ;` or whitespace.
- Claims: first-wins, release, ties, move between panels, panel removal.
- Mass-deletion guard; admission throttle; DNS gate with a fake resolver
  (A, AAAA, CNAME chain, CAA, NXDOMAIN, CF ranges, sslip).
- Poll state machine: 200/304/401/404/410/5xx/timeout/bad JSON.
- Atomic write helpers (crash simulation by killing between steps).
- Status document only contains hosts of the target panel.

### 8.2 Panel (internal/handlers, go test)
- `/routes`: only `shield_node_id = node` rows; www expansion; previews; dedupe;
  punycode; etag stable; 304; 410 on deleted node; auth header; token_prev grace.
- `/register`: idempotent; 409 token_in_use; force.
- `/status`: upsert, size limit, cross-node isolation.
- Phase 0 fixes each with a regression test (trusted proxies prune, reassign
  REPLACE scope, resync calls).
- Contract test: shared golden JSON parsed by both modules.

### 8.3 Integration (docker compose, CI-able)
Services: `caddy` (real), `nd-edge`, two fake panels (Go httptest servers with
fixtures), **Pebble** ACME server + `pebble-challtestsrv` (fake DNS), an origin echo
server with Caddy `tls internal`.
Scenarios: initial issue for both panels; conflict; domain move; panel removal;
panel offline; Caddy restart; nd edge restart; container "reboot" (restart all);
corrupt state files; mass-deletion; origin CA rotation; WebSocket survives reload.

### 8.4 Live checklist (2 panels + 1 Shield, real Let's Encrypt staging first)
- Each panel: 1 direct + 2 shielded domains (one grey cloud, one orange cloud).
- Cloudflare matrix: grey; Full; Full (strict); Always HTTPS on/off; Flexible
  (expect hint, not support).
- Real IP visible at origin app; spoofed headers ignored.
- `reboot` Shield → sites back without panels reachable.
- Remove panel 2 → panel 1 sites never drop (run a 1 req/s probe during removal).
- Duplicate domain across panels → conflict shown in panel 2 UI.

---

## 9. Rollout / migration
1. Ship Phase 0 fixes (panel only).
2. Ship panel API (`/register`, `/routes`, `/status`, `/unregister`) + DB + UI
   (feature-flag `edge_v2`).
3. Ship `nd edge` in the `nd` module + new `setup.sh` wrapper.
4. Existing Shields: panel shows "Upgrade Shield" with the one-line command; install
   detects the legacy catch-all and converts in place (E-INST-3). Certificates reused.
5. After all known Shields report `edge_version`, mark legacy endpoints deprecated
   (keep at least 2 releases).
6. Phase 4 (optional): Shield client cert → origin per-site `client_auth`
   (`require_and_verify`) for shielded hosts; must include a domain-fronting test
   (SNI = direct host, Host = shielded host → must be rejected; Caddy's
   `strict_sni_host` behaviour with client auth — **VERIFY**).

---

## 10. Acceptance criteria
- User action required per domain: **only** the DNS A record (and AAAA if they
  use IPv6). No Flexible bootstrap, no CNAME, no Cloudflare rules.
- Shield reboot → all sites serve within Caddy start time, without any panel up.
- Removing one panel causes zero failed requests for other panels' sites.
- Same host in two panels never serves the second panel's origin.
- No reload when nothing changed (verify with Caddy logs over 1 h idle).
- Every unexpected input (bad JSON, bad host, corrupt state) leaves the currently
  served config untouched.
- All [T] edge cases covered by automated tests; `go test ./...` passes in both
  modules; integration compose suite passes.

---

## 11. Work order for the implementing agent
1. Phase 0 fixes (§2.1) with tests. Commit.
2. DB migrations + `panel_id` setting (§2.2, §2.1 #8). Commit.
3. Panel `/routes` (+ shared route collector function), `/register`, `/status`,
   `/unregister`, auth middleware, rate limits (§2.3). Tests. Commit.
4. `cmd/nd/internal/edge`: api types, config/state/claims with atomic IO,
   host validation, renderer + golden tests. Commit.
5. Poller, DNS gate, reconciler, apply procedure, cert probe, status push. Tests. Commit.
6. CLI commands + systemd unit + install/uninstall/upgrade. Commit.
7. New `setup.sh`/`uninstall.sh` wrappers in `edge_shield.go`; legacy conversion. Commit.
8. Panel UI (§2.4). Commit.
9. Integration suite (Pebble). Commit.
10. Live checklist (§8.4) — report results; do not mark done without it.

Do not change unrelated code. Keep commits small and each one building/testing green.
