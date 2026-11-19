# Task: `nd edge domain` — manual domains and garbage-domain cleanup on a Shield VPS

> Hand this whole file to an implementing agent as its prompt. It is self-contained.

## 0. Role and ground rules

You are implementing a new feature in the NextDeploy `nd` CLI, the **edge** part
(`cmd/nd`, its own Go module `nd`). Work in the repository at `D:\Project\NextDeploy`
on a new branch `feat/nd-edge-domain` from `main`.

- **Research first, then design, then code.** Section 2 lists what to read; do not
  skip it. Write down what you learned (a short `docs/plans/nd-edge-domain-notes.md`)
  before changing code, so a reviewer can follow your decisions.
- Production Shield VPSes run this binary. A mistake takes real sites offline. Every
  change to proxy config must be **validated before reload** and **rolled back** on
  failure, exactly like the existing panel-route pipeline does.
- Never delete or rewrite the operator's hand-written config
  (`/etc/angie/nd-edge/user/*.conf`, `/etc/ferron/nd-edge/user/*.conf`, the Caddyfile
  outside nd's managed region).
- Never remove a domain that comes from a panel; those are owned by the panel.
- Destructive commands default to **dry-run** or ask for confirmation in a terminal
  (see `stdinIsTerminal` in `cmd/nd/cmd/edge_switch.go`); `--yes` skips the question.
- Keep `go vet ./...` and `go test ./...` green in `cmd/nd` (and in the repo root if you
  touch it). Add tests for everything listed in section 7.
- Do not commit, tag or push; leave the branch ready for review.

## 1. Why this exists

A Shield VPS (`nd edge`) sits in front of NextDeploy panels: the `nd-edge` daemon
pulls routes from each registered panel and renders them for the active reverse proxy
(Caddy, Angie or Ferron). Operators also proxy **sites that are not on any panel**
(other servers, legacy hosts). Today they must hand-write engine-specific config
(Caddyfile blocks, Angie `server {}` blocks with ACME clients, Ferron blocks). A real
migration on one Shield needed ~200 lines of Angie config and manual certificate copies
for 8 such sites, and the config is lost or must be re-ported on `switch-proxy`.

Second problem: over time a Shield accumulates **garbage domains** — names whose DNS no
longer points at the Shield (moved away, expired, NXDOMAIN), or whose origin is dead.
They waste ACME attempts (rate limits), log noise, and memory, and nobody notices.

Goal: one engine-neutral command that adds a proxied domain in one line, survives
engine switches and panel syncs, and a safe way to find and remove garbage.

## 2. Research before designing (mandatory)

Read and understand, in this order:

1. **Command entry and conventions**
   - `cmd/nd/cmd/edge.go` — `RunShield`/`runEdge…` dispatch, `edgeOps` (the injectable
     OS/systemd/HTTP layer used by every test), flag parsing style, `hasHelpFlag`,
     `signalDaemonReload`, `edgeConfigLockPath` and the config lock.
   - `cmd/nd/cmd/edge_help.go` — **all help text lives in `edgeCommandDocs` /
     `edgeCommandOrder`**; `edge_help_test.go` enforces that documented flags appear in
     the usage line. New commands must be added there.
   - `cmd/nd/cmd/edge_switch.go` — confirmation prompt pattern, rollback pattern,
     `probeSwitchHostReady`.
2. **Edge core**
   - `cmd/nd/internal/edge/config.go` — `Config`, `Paths`, `EnginePathsFor`, atomic
     `SaveConfig`. Note config lives in `/etc/nd-edge/config.json`.
   - `cmd/nd/internal/edge/engine.go` — `Engine` interface (`RenderGlobal`,
     `RenderPanel`, `RenderCommon`, `Runner`) and `EnginePaths`.
   - `cmd/nd/internal/edge/{caddy,angie,ferron}_engine.go` — how each engine renders a
     panel file, TLS (ACME, imported manual certs via `LoadUsableManualCert`,
     `ManualCertsDir`), real-IP handling, websocket upgrade maps.
   - `cmd/nd/internal/edge/apply.go`, `apply_engine.go` — the **generation** pipeline
     (`panels.gen-NNNNNN` dirs + symlink swap, validate, reload, rollback, failure
     back-off). Your manual domains must go through this, not around it.
   - `cmd/nd/internal/edge/reconciler.go`, `daemon.go`, `poller.go` — when renders
     happen; the SIGHUP path used by `nd edge sync`.
   - `cmd/nd/internal/edge/claims.go` — host ownership/conflict detection between
     panels (`ClaimsStore`, `RebuildClaimsFromDir`). Manual domains must take part.
   - `cmd/nd/internal/edge/dns.go` — **`DNSGate`**: outcomes `dns_ok`,
     `dns_ok_proxied` (Cloudflare), `pending_dns`, `pending_caa`; caching in a JSON
     file; `classifyResolvedIPs` with Shield IPs and Cloudflare ranges (`cfips.go`).
     Reuse it for both TLS gating and garbage detection — do not write a new resolver.
   - `cmd/nd/internal/edge/status.go`, `probe.go` — host status rows and TLS probing.
   - `cmd/nd/internal/edge/cert_import.go` — cert handling and imported cert layout.
3. **Panel side (for consistency of UX, not to change it)**
   - `cmd/nd/cmd/proxy.go` (`nd proxy add <domain> <upstream> --shield <id>`): reuse its
     upstream syntax and flag names where they overlap.
4. **Engine syntax references** (verify every directive you emit)
   - Angie: https://en.angie.software/angie/docs/ (ACME: `acme_client`, `acme`,
     `$acme_cert_<name>`; `ssl_reject_handshake`; `proxy_ssl_*`; `resolver`).
   - Ferron 3: `ferron-test/docs/v3/*.md` in the repo (offline copy).
   - Caddy: https://caddyserver.com/docs/caddyfile (reverse_proxy, transport http tls,
     header_up, redir).
5. **Real-world input**: the manual sites migrated on Shield 107 — the Angie translation
   was kept at `/etc/angie/nd-edge/user/manual-sites.conf` on that server (ask the user
   for a copy if you want it). Use it to check which options people actually need
   (TLS to origin with SNI, skip verify, CF real IP, websockets, big uploads,
   keepalive).

Deliverable of this step: the notes file with (a) where manual domains plug into the
render/apply pipeline, (b) how each engine expresses every option in section 4, (c) open
questions.

## 3. Command surface

```
nd edge domain add    <domain> <upstream>[,<upstream>...] [flags]
nd edge domain set    <domain> [flags]            # change options of an existing manual domain
nd edge domain rm     <domain>... [--yes]         # remove manual domains (moved to trash)
nd edge domain list   [--json] [--all]            # manual domains; --all also lists panel domains (read-only)
nd edge domain show   <domain> [--json] [--config] # details; --config prints the rendered engine config
nd edge domain enable|disable <domain>            # keep it, stop serving it
nd edge domain check  [<domain>...] [--json]      # health report (DNS, origin, TLS) — never changes anything
nd edge domain prune  [--dry-run|--apply] [flags] # remove garbage manual domains (default: dry-run)
nd edge domain restore <domain> | --list          # bring back something removed/pruned (trash)
nd edge domain export [--file F]                  # JSON of all manual domains
nd edge domain import  --file F [--replace] [--dry-run]
```

Aliases: `ls` for `list`, `remove|delete` for `rm`. `nd edge domain --help` lists all
subcommands; each has full help in `edgeCommandDocs` (key e.g. `domain add`).

### 3.1 Upstream syntax (`<upstream>`)

| Input | Meaning |
|---|---|
| `1.2.3.4` | `http://1.2.3.4:80` |
| `1.2.3.4:8080` | `http://1.2.3.4:8080` |
| `1.2.3.4:443` | `https://1.2.3.4:443` (port 443 implies https unless `--origin-http`) |
| `https://origin.example.com` / `http://…:port` | explicit scheme |
| `[2001:db8::1]:8080` | IPv6 |
| `a:80,b:80` | several upstreams → load-balanced (round robin; `--lb least_conn|ip_hash`) |

Validate strictly; reject paths/queries in upstreams and anything with characters that
could inject config (reuse the existing "unsafe chars" checks used by the renderers).

### 3.2 Flags for `add` / `set`

| Flag | Default | Effect |
|---|---|---|
| `--www` | off | also serve `www.<domain>` |
| `--alias host` (repeatable) | — | extra names on the same site |
| `--redirect-www to-apex\|to-www` | — | 308 one form to the other instead of serving both |
| `--tls auto\|none\|manual` | `auto` | auto = ACME (DNS-gated); none = HTTP only; manual = `--cert`/`--key` files |
| `--cert F --key F` | — | with `--tls manual`; copied into nd's cert dir, never referenced in place |
| `--force-https` | on with TLS | redirect :80 → :443 (ACME HTTP-01 must still work) |
| `--origin-http` / `--origin-https` | from upstream | override scheme to origin |
| `--no-verify` | off | do not verify the origin certificate |
| `--sni name` | origin host / `$host` | SNI sent to the origin |
| `--host-header original\|upstream\|<value>` | `original` | Host header sent to the origin |
| `--websocket` | on | upgrade support |
| `--max-body SIZE` | unlimited (`0`) | client body limit, e.g. `100m` |
| `--timeout DUR` | 300s | read/send timeout to origin |
| `--connect-timeout DUR` | 10s | |
| `--real-ip cloudflare\|none` | `cloudflare` when the Shield trusts CF | pass visitor IP (X-Real-IP / X-Forwarded-For from CF-Connecting-IP) |
| `--header-up K=V` (repeatable) | — | add request header to origin |
| `--header-down K=V` (repeatable) | — | add response header |
| `--keepalive N` | 32 | upstream keepalive connections (engines that support it) |
| `--note "text"` | — | free text shown in `list` |
| `--disabled` | — | add but do not serve yet |
| `--dry-run` | — | print what would be rendered for the active engine; change nothing |
| `--json` | — | machine output |

Out of scope (say so in help): per-path routing, rate limits, bot blocking, custom log
formats, `/media/` prefix stripping. Those stay in the user/ files. Design the data model
so path routes could be added later without a migration.

## 4. Data model and storage

- File: `/etc/nd-edge/domains.json` (next to `config.json`; put the path in `Paths`).
  Atomic writes (`edge.WriteFileAtomic`), mode 0600, guarded by the same config lock as
  other edge mutations. Include `"schema": 1`.
- Entry: `domain`, `aliases[]`, `www`, `redirect_www`, `upstreams[]` (normalized
  scheme/host/port), `lb`, `tls` (`auto|none|manual`), `cert_file`, `key_file`,
  `origin` options (verify, sni, host header), `websocket`, `max_body_bytes`,
  timeouts, `real_ip`, headers, `keepalive`, `enabled`, `note`, `created_at`,
  `updated_at`, `id` (stable, e.g. `m_<hash>` used for file names / ACME client names).
- Trash: `/etc/nd-edge/domains.trash.json` — removed/pruned entries with
  `removed_at`, `reason` (`manual`, `prune:dns`, `prune:origin`…). Keep 30 days; prune on
  write. `restore` moves an entry back (fails on conflict).
- Domains are lower-cased, IDNA-normalized (punycode), no trailing dot; wildcard
  `*.example.com` allowed only with `--tls manual` or `--tls none` (no DNS-01 support).

## 5. How it must work

### 5.1 Rendering and apply
- Add a renderer per engine for manual domains (`RenderManual(entries, cfg)` on the
  `Engine` interface, or a sibling interface) producing a file inside the **managed
  generation**, e.g. `panels.gen-NNNNNN/zz-manual.conf` (`.caddy` for Caddy), so it is
  swapped, validated and rolled back with everything else. Do **not** write into `user/`.
- Rendering is deterministic (sorted) so unchanged input yields identical output and no
  reload. Golden-test each engine.
- Every mutating command: load → validate input → check conflicts → render → run the
  normal apply (validate with the engine, reload, probe) → only then save
  `domains.json`. If apply fails, nothing is saved and the error from `angie -t` /
  `caddy validate` / Ferron validation is shown. When the daemon is running, coordinate
  through the lock/SIGHUP path the same way `nd edge sync` does; never race the daemon.
- `switch-proxy` must carry manual domains over automatically (they are re-rendered for
  the target engine during the switch's own apply). Add a test.
- TLS `auto`: reuse `DNSGate`; a domain whose DNS does not reach the Shield yet is kept
  but rendered HTTP-only (or not at all — decide, document, and match how panel routes
  behave), and becomes HTTPS automatically once DNS is OK. Respect `DNSGateOff`.
- Angie: ACME via `acme_client` + `$acme_cert_<id>`; Caddy: automatic; Ferron: its ACME.
  Reuse already-imported Caddy certs for the same names when present (avoid rate limits).
- Real IP: honour the Shield's existing trusted-proxy settings; do not emit a second
  `real_ip_header`/`set_real_ip_from` that collides with global config (this broke Angie
  on Shield 107 — check `RenderGlobal` output).

### 5.2 Conflicts
- A host may belong to exactly one owner: one panel **or** manual. Extend claims so
  manual hosts are visible; `add` refuses a host claimed by a panel (and a panel route
  for a manually claimed host must show up as a conflict in `nd edge status` instead of
  silently overriding). Also refuse hosts that appear in `user/*.conf` (parse
  `server_name` / site addresses, like `parseHostsFromPanelAngie`), with a hint.

### 5.3 Visibility
- `nd edge status` shows manual hosts with a `SOURCE` column (`manual` / panel name) and
  the same TLS/DNS state as panel hosts. `--json` gets `source`.
- `nd edge doctor` adds a `manual-domains` check (file valid, count, any failing).

## 6. Garbage detection: `check` and `prune`

`check` (read-only) evaluates each domain (manual by default; `--all` includes panel
domains, read-only) and reports one row per host:

| Signal | How | Classification |
|---|---|---|
| DNS | `DNSGate` (fresh query, bypass cache) | `ok`, `ok_proxied` (Cloudflare), `elsewhere` (resolves to other IPs), `nxdomain`, `no_records`, `error` |
| Origin | TCP connect + one HTTP(S) request to the upstream with the right Host/SNI, short timeout | `up` (any HTTP status), `refused`, `timeout`, `tls_error` |
| TLS | cert served by the Shield for the name, expiry | `valid (N days)`, `expiring`, `missing`, `acme_failing` |
| Traffic (optional) | requests in the last N days if logs/status zones allow it | number or `unknown` |

`prune` turns that into removals, **manual domains only**:
- Default is a dry-run table: what would be removed and why.
- `--apply` (with confirmation in a terminal, or `--yes`) actually removes; removed
  entries go to the trash (restorable), then one apply/reload for all of them.
- A domain is garbage only if a condition **persists**: record first-seen-bad time in
  the DNS cache or a small `domains.health.json`; require it for `--grace 7d` (default)
  across at least 2 checks. Never prune on a single failed lookup.
- Reasons selectable: `--reason dns` (nxdomain / no_records / elsewhere — default),
  `--reason origin` (origin down for the grace period), `--reason all`.
- `elsewhere` with Cloudflare proxied records counts as **ok** (proxied DNS hides the
  Shield IP); never prune `ok_proxied`.
- Also clean the derived leftovers of removed domains: their ACME state dirs / imported
  certs that nothing references anymore, and their DNS cache entries.
- For panel domains `prune --all` only **reports** them with a hint
  (`nd shield reassign …` on the panel or remove the domain there); it never edits them.
- Optional: `--schedule daily|off` installs/removes a systemd timer that runs
  `nd edge domain prune --dry-run --json` and logs the result (not `--apply`, unless the
  operator passes `--auto-apply` explicitly). Report-only by default.

## 7. Tests (required)

- Parser tests: every upstream form in 3.1, invalid inputs, IDNA, wildcard rules.
- Golden render tests per engine (Caddy, Angie, Ferron) for: plain HTTP, ACME,
  manual cert, origin HTTPS with SNI + no-verify, multiple upstreams + lb, www redirect,
  headers, max body, websockets, disabled entry (not rendered).
- Engine validation tests where binaries are available (follow
  `angie_integration_test.go` / `ferron_integration_test.go`: skip when the binary or
  Docker is missing).
- Command tests through `edgeOps` fakes: add → files + apply called; apply failure →
  `domains.json` unchanged; conflict with panel claim; conflict with user/ file;
  rm → trash; restore; export/import round trip; `--dry-run` changes nothing.
- `switch-proxy` test: manual domains rendered for the target engine.
- Prune tests with a fake resolver/clock: single failure does nothing; persistent
  NXDOMAIN past grace → pruned; `ok_proxied` never pruned; panel domains never touched;
  dry-run default.
- Help test: new docs present, documented flags appear in usage lines.

## 8. Documentation

- Full help for every new subcommand in `edgeCommandDocs`, with examples, e.g.:
  ```
  nd edge domain add shop.example.com 10.0.0.5:8080 --www
  nd edge domain add api.example.com https://10.0.0.6 --no-verify --max-body 200m
  nd edge domain check
  nd edge domain prune            # shows what would be removed
  nd edge domain prune --apply    # removes it (restorable for 30 days)
  ```
- Mention the feature in `nd edge --help` and in the panel's Shield docs modal
  (`src/components/shield/ShieldDocsModal.tsx`, English + Bangla text) only if that does
  not require panel API changes.

## 9. Acceptance criteria

1. One command adds a working HTTPS proxy for a domain on Caddy, Angie and Ferron, with
   validation and automatic rollback on bad config.
2. Manual domains survive daemon restarts, panel syncs and `switch-proxy`, and never
   touch `user/` files or panel routes.
3. `check` gives a clear table; `prune` is dry-run by default, needs a persistent
   condition, never removes Cloudflare-proxied or panel domains, and every removal is
   restorable.
4. `go vet` and all tests pass in `cmd/nd`; new golden files reviewed.
5. Notes file explains design decisions and any open question you could not settle.
