# Backend, CLI & Installer Audit and Architecture Ground-Truth Report

- **Target:** `D:\Project\NextDeploy` (branch `feat/multi-proxy-drivers`, HEAD `f000868`)
- **Date:** 2026-11-18
- **Scope:** Modes 1 & 2 (`auditing-codebases` skill) across Go backend (`main.go`, `internal/**`), CLI/Edge (`cmd/nd/**`), Docker/Compose/Installer (`install.sh`, `migrate.sh`, `uninstall.sh`, `docker-compose*.yml`, `Dockerfile`), and `testdata/`, mapped against `docs/multi-proxy/` (`README.md`, `01-architecture.md`, `02-capability-matrix.md`, `05-edge.md`, `06-install-and-switch.md`, `07-testing.md`, `08-tasks.md`).
- **Exclusions:** Zero files under `src/` were inspected, reviewed, or edited. Zero production source files were modified.

---

## Verdict

| Check | Status | Notes |
|---|---|---|
| Baseline Go Test Suite (`-count=1`) | **PASS** | Root (`20 ok`, `6 [no test files]`) and `cmd/nd` (`4 ok`, `3 [no test files]`) pass with `-count=1`. |
| Plan / Pre-Ship Readiness (`feat/multi-proxy-drivers`) | **NEEDS FIXES** | `docs/multi-proxy/` is plan-only (`README.md:3`). `pre_ship_scan.py --strict` exits `1` (0 blockers, 80 warnings). Significant missing caller coverage, god files, and critical implementation traps (POSIX directory rename, relative container symlink targets, live port label filter, sync vs debounce contract, `caddy_config` table split) must be resolved in Phase 0–3 design before coding. |

---

## 1. Baseline Hygiene, Static Scanner & Test Results

### 1.1 Git State & Root Hygiene
- `git status -sb`: `## feat/multi-proxy-drivers`, untracked `?? docs/` and `?? ferron-test/`.
- Untracked binaries and runtime files present in repo root (`Get-Item` verified): `panel` (`49,672,990` B), `panel-linux` (`32,508,010` B), `panel.exe` (`32,301,568` B), `panel.db` (`0` B), `.env.local` (`41` B). None are tracked (`git ls-files` shows only `.env.example` tracked among those checked). `.gitignore` covers `/panel`, `/panel-*`, `*.exe`, `*.db`, `.env.*`, `*.local`, though untracked `ferron-test/` (~hundreds of Rust/bench files) pollutes static scans.

### 1.2 Static Scanner (`pre_ship_scan.py --strict`)
Executed `python C:\Users\asus\.gemini\config\skills\auditing-codebases\scripts\pre_ship_scan.py --target "D:\Project\NextDeploy" --strict` -> exited `1`:
- **Blockers:** `0`
- **Warnings:** `80`
  - `local_trap` (25): `install.sh:563`, `internal/sysinfo/sysinfo.go:224` (`http://caddy:2019/metrics`, `127.0.0.1:2019`), `internal/handlers/edge_shield.go:243` (`http://caddy:2019/pki/ca/local`, `127.0.0.1:2019`), `internal/handlers/php_sites.go:315`, `internal/rclone/rclone.go:257`, 18 hits in `ferron-test/`, 2 hits reported in `src/`.
  - `swallowed_error` (25): all 25 reported entries are under `src/` (not inspected).
  - `todo_marker` (4): all 4 inside `ferron-test/`.
  - `god_file` (25, truncated at scanner limit): `main.go` (`726` lines) + 24 entries reported under `src/`.
  - `naming_smell` (1): reported under `src/`.

### 1.3 Backend / CLI / Script Files Exceeding 500 Lines (God-File Risk)
Measured directly across `*.go`, `*.sh`, `*.yml`, `*.yaml`, `Dockerfile` excluding `src/`, `node_modules/`, `ferron-test/`, `.git/`, `dist/`:

| File | Lines | Impact on Multi-Proxy Plan |
|---|---:|---|
| `internal/agent/omprpc/wire.go` | 8591 | Generated/protocol wire layer |
| `cmd/nd/cmd/commands.go` | 3287 | Registers/documents `proxy`, `php`, `shield`, `edge` help text |
| `internal/mcp/handler.go` | 2867 | 9 `internal/caddy`/override/reload call sites (`311..2338`) |
| `cmd/nd/cmd/edge.go` | 2479 | Edge install/doctor/sync/uninstall; must NOT inline Angie/Ferron here |
| `internal/handlers/edge_shield.go` | 1620 | Origin CA read, compose sync, shield status |
| `internal/handlers/settings.go` | 1499 | Root compose sync/apply (`433..509, 732..795, 1131..1182`) |
| `internal/dockerapi/dockerapi.go` | 1364 | Live port state (`316`), container lookup (`946`), network (`1050`), prune (`1087`) |
| `internal/caddy/caddy.go` | 1174 | **>1000 lines**: mixes admin HTTP client, compose merge, label gen, DNS helpers |
| `internal/handlers/panel.go` | 1164 | `Panel` struct, slot project resolution, compose actions |
| `internal/handlers/caddy.go` | 1081 | Slot/app compose sync, container lifecycle, logs/overview |
| `internal/caddy/slot_compose.go` | 757 | Mixes `.env` parsing, port extraction/strip, slot labels & preview compose |
| `main.go` | 726 | Dependency wiring (`Panel`, `PHP`, `Copilot`), route & Fiber proxy setup |
| `internal/db/migrate.go` | 708 | Needs `proxy_apply` migration; extract to helper/split file |
| `install.sh` | 666 | Embeds `/usr/local/bin/nextdeploy-update` heredoc; Caddy-coupled |
| `cmd/nd/cmd/proxy.go` | 663 | `nd proxy` subcommands; needs `engine get|set|preflight` |
| `cmd/nd/internal/edge/{apply,dns,reconciler,render,daemon,poller}.go` | 819 / 678 / 665 / 631 / 557 / 502 | Edge applier/reconciler/renderer must split engines into dedicated files |
| `cmd/nd/cmd/php.go` | 907 | `nd php` CLI help/subcommands |
| `internal/handlers/{edge_api,spa,php_sites,workspace_files,migrate,apps}.go` | 859 / 758 / 742 / 831 / 688 / 510 | Missing/under-tracked proxy callers & settings exposure |
| `internal/phphosting/{runtime,manager}.go` | 612 / 545 | Holder containers + direct `docker exec caddy caddy reload` |

### 1.4 Baseline Uncached Go Tests (`go test -count=1 ./...`)
- **Root module (`D:\Project\NextDeploy`)**: exit `0`
  - Passed (20): `panel` (`0.578s`), `panel/internal/agent` (`22.641s`), `panel/internal/agent/omprpc` (`1.457s`), `panel/internal/backup` (`0.181s`), `panel/internal/caddy` (`0.100s`), `panel/internal/db` (`1.167s`), `panel/internal/dockerapi` (`0.107s`), `panel/internal/dockerx` (`0.145s`), `panel/internal/handlers` (`20.275s`), `panel/internal/htaccess` (`0.185s`), `panel/internal/logview` (`0.116s`), `panel/internal/mcp` (`7.267s`), `panel/internal/phphosting` (`0.186s`), `panel/internal/qos` (`0.135s`), `panel/internal/quota` (`0.616s`), `panel/internal/rclone` (`0.116s`), `panel/internal/resmatch` (`0.066s`), `panel/internal/sandbox` (`0.072s`), `panel/internal/volumex` (`0.073s`), `panel/internal/workspace` (`0.064s`).
  - No test files (6): `gitx`, `migrate`, `perflog`, `runutil`, `sysinfo`, `wsmanifest`.
- **CLI module (`D:\Project\NextDeploy\cmd\nd`, separate `go.mod`)**: exit `0`
  - Passed (4): `nd/cmd` (`3.451s`), `nd/internal/config` (`0.065s`), `nd/internal/edge` (`2.029s`), `nd/internal/sync` (`0.075s`).
  - No test files (3): `nd`, `nd/internal/client`, `nd/internal/ui`.

---

## 2. `internal/caddy` API & Behavior Inventory

| File (Lines) | Exported Symbols | Behavior / Notes |
|---|---|---|
| `caddy.go` (1174) | Consts `DefaultAdminAPI` (`"http://caddy:2019"`), `CaddyContainerName` (`"caddy"`), `CaddyImage` (`"lucaslorentz/caddy-docker-proxy:ci-alpine"`), `CaddyNetwork` / `PanelNetworkName` (`"NextDeploy"`), `PanelNetworkKey` (`"nextdeploy"`), `GeneratedCompose` (`".nextdeploy.generated.compose.yml"`); Type `NDSharedMount{VolumeName, Target}`; Funcs `ValidNDSharedTarget`, `CleanQuotedValue`, `AdminStatus`, `AdminConfigGet`, `AdminConfigPost`, `GenerateLabels`, `GenerateServiceLabels`, `ParseComposeServiceNames`, `GenerateMergedCompose`, `MergeCaddyGatewayPorts`, `GenerateRootStackCompose`, `IsBehindCloudflare`, `ShouldUseInternalTLS`, `ExtractWildcardDomainIP`, `ShortPanelID`, `FormatWildcardDomain`, `LabelsToYAML`. | `appendSiteLabels` injects `X-Frame-Options: SAMEORIGIN`, `X-Content-Type-Options: nosniff`, `Referrer-Policy`, `X-XSS-Protection`; sets `tls internal` when `(EnableHTTPS \|\| ShieldNodeID!=nil) && (ShieldNodeID!=nil \|\| ShouldUseInternalTLS(domain))`; sets `handle_path` + `file_server` hiding `.git .env* *.sql *.db *.sqlite *.key *.sock`; sets `stream_close_delay: "10m"`; strips/overwrites `X-Forwarded-*` + `CF-Connecting-IP` via `applyUntrustedDirectProxyHeaders` unless `TrustedProxy` or `ShieldNodeID!=nil`. `GenerateRootStackCompose` requires both `services["caddy"]` (`636`) and `services["panel"]` (`677`). |
| `options.go` (184) | Var `CloudflareRanges`, `OptionProfiles` (`default`, `balanced`, `high`, `strict`); Type `GlobalOptions{Profile, ReadHeader, ReadBodyIdle, Idle, GracePeriod, MaxHeaderSize, RealIP, TrustedProxies, TrustedStrict, ShieldProxies, DirectCloudflare}`; Methods `(GlobalOptions).Normalize()`, `(GlobalOptions).Labels()`; Func `IsGlobalOptionLabel`. | Emits `caddy.servers.*` + `caddy.email` global labels; `trusted_proxies` includes `CloudflareRanges` + `ShieldProxies` (`DirectCloudflare` adds CF ranges). |
| `routes.go` (186) | Funcs `ValidateRoute`, `ValidateRouteForUser`, `RouteLabels`. | `checkTargetSSRF` (`routes.go:109`) blocks dotless hosts and loopback/private/link-local/CGNAT `100.64.0.0/10` IPs via `net.DefaultResolver.LookupIP`, **but only when `!isAdmin`** (`ValidateRouteForUser`). |
| `php_labels.go` (200) | Type `SiteSecurity{BlockedIPs, AllowOnly, BlockXMLRPC, BlockUploadPHP, BlockScanners, HSTS, FrameProtection, BasicAuthUser, BasicAuthHash}`; Funcs `PHPSiteLabels`, `PHPSiteLabelsWithRouting`, `NormalizeIPList`. | Emits `@wp_cache`, `php_fastcgi` (`dial_timeout 5s`, `read_timeout 120s`, `write_timeout 120s`, `env HTTPS on` when TLS), `file_server.precompressed = "gzip zstd"`, security rules, and `<prefix>.basic_auth.<BasicAuthUser>`. |
| `php_routing.go` (278) | Consts `FallbackDefault`, `FallbackStrict404`, `FallbackCustom`; Types `RoutingRule{ID, Kind, MatchType, Pattern, Target, Status, Enabled, UnlessFile (json:"unless_exists,omitempty"), RequireTargetFile (json:"require_target_file,omitempty"), PreserveQuery (json:"preserve_query,omitempty"), Note}`, `SiteRouting{FallbackMode, FallbackEntry, Rules}`; Funcs `ValidateRouting`, `MergeUniqueRoutingRules`. | Enforces `maxRoutingRules = 100`. Plan 01/04/08 must translate all 3 `Kind` (`rewrite`/`redir`/`block`), 3 `MatchType` (`exact`/`prefix`/`regex`), `UnlessFile`, `RequireTargetFile`, `PreserveQuery`, and 3 fallback modes. |
| `slot_compose.go` (757) | Type `HostPortBinding{HostPort, ContainerPort, BindIP, Protocol, Service, Direct}`; Funcs `ParseComposeEnv`, `ExtractAndStripHostPorts`, `GenerateSlotLabels`, `SlotPreviewDomain`, `GenerateSlotMergedCompose`, `GenerateSlotMergedComposeWithEnv`. | Separates `Direct` L4 ports from HTTP gateway ports (`nonHTTPContainerPorts`, UDP, or DB image tokens), strips non-direct host bindings into Caddy gateway labels (`:HostPort` -> `{{upstreams ContainerPort}}`), keeps direct host ports when `!isPreviewOnly`. |

---

## 3. Caller Inventory vs. Plan Coverage (`docs/multi-proxy/`)

Plan `01-architecture.md` §1.1 (line 24) and `08-tasks.md` Task 0.6 list only `handlers/{caddy,caddy_options,panel,php_*,preview,settings,spa,workspace_files}.go` and `internal/{agent,mcp,htaccess,phphosting,workspace}`. Direct codebase search found **many unmentioned callers**:

| Area / File | Exact Call Sites / Imports Observed | Gap in Plan / Required Phase 0.6 Work |
|---|---|---|
| `internal/handlers/deploy.go` | `:256` `bindings, syncErr := p.SyncAppSlotComposeCtx(...)`; `:371` `SyncAppSlotComposeCtx` | **Design blocker**: `deploy.go:256` uses returned `[]caddy.HostPortBinding` (`b.Direct`, `b.ContainerPort`) to stop old stacks holding direct L4 ports and choose readiness probe port. Pure `p.Proxy.Sync()` returning `error` cannot replace `ExtractAndStripHostPorts` + `SyncAppPorts`. |
| `internal/handlers/rollback.go` | `:129, 226` `SyncAppSlotComposeCtx`; `:164-165, 260-261` `syncRootStackCompose` + `applyRootStackComposeBackground(..., "caddy")` | Missing from plan caller list; triggers compose recreate of `"caddy"`. |
| `internal/handlers/compose_deploy.go` | `:316` `SyncAppCaddyOverride` | Missing from plan list. |
| `internal/handlers/compose_helpers.go` | `:22` `SyncAppCaddyOverride` inside `ComposeFileView` | Missing; previewing effective compose relies on generated file sync. |
| `internal/handlers/apps.go` | `:180, 202` `SyncAppCaddyOverride`; `:476` `SyncAppCaddyOverrideCtx`; `:483` `SyncAppSlotComposeCtx` | Missing; app creation/update/ownership transfer (`reconcileTransferredComposeLimits`). |
| `internal/handlers/app_limits.go` | `:254` `SyncAppCaddyOverrideCtx` | Missing. |
| `internal/handlers/api_archive.go` | `:130` `SyncAppCaddyOverrideCtx` | Missing. |
| `internal/handlers/api_files.go` | `:391, 570` `SyncAppCaddyOverrideCtx` | Missing. |
| `internal/handlers/api_cli_ext.go` | `:550-551` `APIProxyReload` calls `ReconcileProxyRoutes` + logs `"caddy"` | Missing; powers `nd proxy reload`. |
| `internal/handlers/github.go` | `:878` `p.PHP.ReloadCaddy(bg)`; `:891` `SyncAppSlotComposeCtx` | Missing; GitHub webhook auto-deploy bypasses `Panel` proxy abstraction. |
| `internal/handlers/git_sync.go` | `:146` `SyncAppCaddyOverrideCtx` | Missing. |
| `internal/handlers/migrate.go` | `:369` `SyncAppCaddyOverrideCtx` (`AfterAppImported` hook) | Missing; panel migration restore must sync new proxy driver too. |
| `internal/handlers/users.go` | `:623` `SyncAppCaddyOverrideCtx`; `:628` `SyncAppSlotComposeCtx` | Missing; user suspend/unsuspend re-syncs apps. |
| `internal/handlers/workspace_json.go` | `:265` `SyncAppCaddyOverrideCtx` | Missing. |
| `internal/handlers/edge_api.go` & `edge_shield.go` | `edge_api.go:350`, `edge_shield.go:215, 283` `ReadOriginCaddyRootCA`; `edge_shield.go:1460, 1558-1559` `syncRootStackCompose` + `"caddy"` | `ReadOriginCaddyRootCA` returns origin Caddy CA only; must generalize to `p.Proxy.OriginRootCA` / `internal/proxy/pki`. |
| `internal/phphosting/{runtime,security,settings}.go` | `runtime.go:278` `PublishHolder`, `:325` `PublishHolder` (`busybox:stable`), `:369` `RemoveHolder` + `ReloadCaddy`, `:375-385` `ReloadCaddy` (`docker exec caddy caddy reload --config /config/caddy/autosave.json`); `security.go` & `settings.go` import `caddy` | `phphosting.Manager` (`manager.go:20`, wired at `main.go:81`) holds no `Proxy` callback/interface; `SyncSite`/`SyncRoutes`/`StopSite`/`StartSite`/`ChangeVersion`/`SaveSiteSecurity` directly create `busybox:stable` holder containers and exec `caddy reload`. Must decouple holder/reload behind a proxy-sync callback when engine != `caddy`. |
| `internal/agent/host_tools.go` & `host_tools_php_routing.go` | `host_tools.go:230, 288, 452` and `host_tools_php_routing.go:239` call `m.PHP.ReloadCaddy(ctx)` | Copilot PHP domain/routing mutations directly reload Caddy via `phphosting`. |
| `internal/mcp/handler.go` & `mcp/php_routing.go` | `handler.go:311, 941, 1314, 1381, 1401, 2183, 2338` `SyncAppCaddyOverrideCtx`; `handler.go:1529` & `php_routing.go:206` `h.p.PHP.ReloadCaddy` | Heavily coupled; 2,867-line `mcp/handler.go` omitted from `08-tasks.md` caller details beyond package name. |
| `internal/sysinfo/sysinfo.go` | `:224` `http://caddy:2019/metrics` and `http://127.0.0.1:2019/metrics` | Not listed in plan; Prometheus/telemetry scraper hardcodes Caddy Admin metrics endpoint (matches Task 1.9 need). |
| `internal/dockerapi/dockerapi.go` | `:288, 340-362, 946, 1050, 1087` | Not listed in plan; live port parser, container finder, shared network attach, image prune protection all hardcode Caddy. |
| `internal/sandbox/validator.go` | `:222-223, 228-229` allows only external networks `nextdeploy` or `caddy` | Low risk (`NextDeploy` stays valid), noted as hardcoded `"caddy"` check. |
| `internal/volumex/volumex.go` | `:317` `isInfraBackupVolumeName` checks `caddy`, `letsencrypt`, `acme` | Should also protect future proxy PKI/ACME volumes (`angie`, `ferron`, `nd-proxy` if named volumes are used). |
| `main.go` | `:162` `SyncRootStackComposeOnStart`, `:176` `ReconcileProxyRoutes`, `:302-312` `/api/v1/proxy/*`, `:384-390` + `:609-625` `/caddy/*` | Must wire `p.Proxy` before `SyncRootStackComposeOnStart()` and `p.PHP`. |

---

## 4. `internal/db` Schema, `state.Build` Inputs & Migration Target

- **Connection & Migration Mechanism (`internal/db/store.go`, `migrate.go`)**:
  - `db.Open` configures SQLite WAL mode with `SetMaxOpenConns(1)` and `SetMaxIdleConns(1)` (`store.go:27-28`), then calls `s.migrate()` (`migrate.go:8-708`). Migrations use `CREATE TABLE IF NOT EXISTS` and `pragma_table_info` checks (`migrate.go`, `store_php.go:61`, `store_proxy_routes.go:33`, `store_shield_nodes.go:63`).
  - Phase 0.5 `proxy_apply` table migration belongs in `internal/db/migrate.go` (preferably delegated to a small `migrateProxyTables()` helper/new `store_proxy_apply.go` so `migrate.go` does not grow beyond 708 lines).
- **Critical Plan Gap — `caddy_config` vs `settings` Tables**:
  - Plan `01-architecture.md:26` and `06-install-and-switch.md:5-14` assume settings live only in `settings`. In reality, **`caddy_config` is a separate SQLite table** (`migrate.go:40-46`, `store_settings.go:36-64`) storing `"global_options"`, `"admin_api"`, `"email"`, `"caddy_image"`, and `"shield_trusted_proxies"`, while `settings` holds `"panel_domain"`, `"panel_enable_https"`, `"panel_enable_www"`, `"panel_id"`, `"panel_shield_node_id"`, `"caddy_shared_mount_prefix"`, `"caddy_shared_volume_names"`, `"root_apply_status"`, and `"preview:<appID>"` (`handlers/preview.go:22`).
- **Pure Inputs Required by Phase 0.2 `state.Build(in BuildInput)`**:
  To remain pure and unit-testable (no DB/Docker/network calls inside `state.Build`), the caller must load and pass:
  1. **Panel & Global Settings**: `panel_domain`, `panel_enable_https`, `panel_enable_www`, `panel_id`, `panel_shield_node_id`, `caddy_config["global_options"]` (`GlobalOptions`), `caddy_config["email"]`, `caddy_shared_mount_prefix`, `caddy_shared_volume_names` (note JSON array vs CSV trap below), `proxy_engine`.
  2. **Shield Nodes**: `db.ListShieldNodes(ctx)` (for `ShieldProxies` IPs, `DirectCloudflare`, and `BehindShield` checks).
  3. **Docker Apps + Domains + Resolved Slot/Upstream Info**: `db.ListApps(ctx)`, all `AppDomain` rows (**NOTE: `store_domains.go` currently has `ListAppDomains(ctx, appID)` only — a `ListAllAppDomains` query must be added to avoid N+1**), per-app `PreviewConfig` (`settings["preview:<appID>"]`) + pre-resolved public/shield IP (`ResolveAppPublicIP` currently calls `sysinfo.Collect`, which MUST stay outside `state.Build`), and pre-resolved active/standby compose service target / slot project names (`<base>-<slot>` or legacy compose project name, `panel.go:711-716`, `caddy.go:369`).
  4. **PHP/Static Sites**: `db.ListPHPSites`, per-site `SiteSecurity` (`GetPHPSecurityJSON`), `phphosting.SettingsFor(site)`, owner status (`UserStatusSuspended` disables routes: `phphosting/runtime.go:269-271`), `phphosting.DocRoot`, `phphosting.SocketPath` (`pool.go:28-38`). **Important:** `php_sites.go:253,268` stores dummy `Service: "php", Port: 80` in `app_domains`; `state.Build` must check `php_sites` so PHP/static sites are not emitted as Docker container upstreams!
  5. **Proxy Routes**: `db.ListProxyRoutes(ctx, nil)` filtered to `Enabled == true` (note: exclude `PendingOriginApply` only where Shield requires it? Actually origin still serves routes; edge filters `PendingOriginApply`).
  6. **Allocated Gateway Ports**: `db.ListAllocatedPorts(ctx)` filtered to `!Direct && Port > 0` (**without** `filterCaddyGatewayPorts`, see Open Question #5 below).

---

## 5. Answers to Plan Open Questions #4 and #5

### 5.1 Open Question #4 — Container Name `"caddy"` vs `"nd-proxy"`
Every hardcoded reference to `"caddy"` container/service/unit found across the repo:
- **Constants & Compose**: `internal/caddy/caddy.go:28-29, 618, 636, 675, 687` (`DefaultAdminAPI = "http://caddy:2019"`, `CaddyContainerName = "caddy"`, `services["caddy"]`); `docker-compose.yml:5-6, 28-29` (`container_name: caddy`, `panel.depends_on: - caddy`); `docker-compose.local.yml:5-6, 27-28`.
- **Docker API (`internal/dockerapi/dockerapi.go`)**:
  - `:340-341` `InspectLivePortState`: identifies proxy via `name == "caddy"` or compose service `"caddy"`.
  - `:946` `containerIDByName`: **bug** `trimmed == name || (name == "caddy" && (trimmed == "nextdeploy_caddy" || strings.Contains(trimmed, "caddy")))` — matches *any* container whose name contains `"caddy"` when looking up `"caddy"`, and misses `"nd-proxy"`.
  - `:1050` `EnsureCaddySharedNetwork`: hardcodes `"caddy"`.
  - `:1087` `protectedImageMarkers`: protects `"caddy-docker-proxy"` only; must add Angie/Ferron image markers so "Delete Unused" never prunes them.
- **Panel Handlers & Subsystems**:
  - `internal/handlers/caddy.go:477, 479, 508, 510, 513, 519, 524, 949` (`Start/Restart/StopContainerByName`, `DockerLogs`, `applyRootStackComposeBackground(..., "caddy")`). Note `:487-488` `force_start` nsenter script also stops host `caddy`/`nginx`/etc. (wait: leave `force_start` careful — `nginx` on host could be stopped, fine on container host).
  - `internal/handlers/caddy_options.go:77` (`services := []string{"caddy"}`).
  - `internal/handlers/settings.go:774, 1176` (`"caddy"` compose target).
  - `internal/handlers/edge_shield.go:226` (`docker exec caddy cat /data/caddy/pki/authorities/local/root.crt`), `:243` (`http://caddy:2019/pki/ca/local`).
  - `internal/handlers/workspace_files.go:76` (SSRF hostname blocklist checks `localhost || caddy || panel` — **must add `"nd-proxy"`**!).
  - `internal/phphosting/runtime.go:383` (`docker exec caddy caddy reload ...`).
  - `internal/sysinfo/sysinfo.go:224` (`http://caddy:2019/metrics`).
- **Installer / Scripts**: `install.sh:442, 484` (`merge_caddy_shared_volumes_from_previous_compose` inside `nextdeploy-update` matches `caddy:` and `caddy_data: {}`); `uninstall.sh:236, 243, 276-277, 292, 310-312` (`docker stop/rm panel caddy`, `caddy-docker-proxy` image, `nextdeploy_caddy_data` / `caddy_data` volume).
- **Edge CLI (`cmd/nd`)**: `cmd/nd/cmd/edge.go:478, 531, 612, 685, 719, 1132-1133, 1894, 2391, 2396, 2407, 2412` (`caddy.service`, `caddy` user, `caddy` binary) and `cmd/nd/internal/edge/{config,apply}.go` (`DefaultPaths()`, `ExecCaddyRunner`, `CaddyUser`).
- **Recommendation**: Keep container/service name `caddy` when `proxy_engine == "caddy"` (matches `06-install-and-switch.md:32`) and use `nd-proxy` (service `proxy` or `nd-proxy`) when `proxy_engine != "caddy"`, removing `depends_on: - caddy` from `panel` in root Compose (or using Compose profiles / a single service key `proxy` while keeping `container_name: caddy` on Caddy upgrades).

### 5.2 Open Question #5 — `MergeCaddyGatewayPorts` & Gateway Ports (HTTP vs Raw TCP)
**Verified Answer: Gateway ports proxied through Caddy/proxy are HTTP only; raw TCP/UDP L4 ports bypass the proxy (`Direct = true`).**
- `internal/caddy/slot_compose.go:31-61, 91-112` defines `nonHTTPContainerPorts` (FTP/SSH/SMTP/DNS/IMAP/POP3/MSSQL/Oracle/MQTT/MySQL/RDP/NATS/Postgres/RabbitMQ/Redis/Cassandra/Kafka/Memcached/Cockroach/Mongo) and `isDirectHostPort` (returns `true` for UDP, `nonHTTPContainerPorts`, or DB image tokens like `postgres`/`mariadb`/`mysql`/`redis`/`mongo`).
- `ExtractAndStripHostPorts` (`slot_compose.go:127`) strips ONLY non-direct (`Direct == false`) host ports from app services (`Direct == true` ports remain published directly on the app container when `!isPreviewOnly`, verified in `port_reconcile_test.go:119-137, 155-157`).
- `GenerateSlotLabels` (`slot_compose.go:494-518`) skips `if hp.Direct { continue }` and emits HTTP reverse-proxy labels (`siteAddr = ":<HostPort>"` or `"http://<BindIP>:<HostPort>"` -> `reverse_proxy {{upstreams <ContainerPort>}}`).
- `MergeCaddyGatewayPorts` (`caddy.go:567-600`) preserves `80`/`443`, skips `if ap.Port <= 0 || ap.Direct { continue }`, and adds `<BindIP>:<Port>:<Port>` to the proxy service.
- **Implication**: Angie does **not** need `stream {}` and Ferron does **not** need `tcp *:port` for gateway ports unless product requirements change.
- **CRITICAL TRAP IN `settings.go:433-450` + `dockerapi.go:361`**: `syncRootStackComposeState` calls `filterCaddyGatewayPorts(allocated, live)` (`settings.go:481`), which rejects any `AllocatedPort` unless `live.CaddyGatewayPorts[ap.Port]` is `true`. `InspectLivePortState` (`dockerapi.go:361`, `288`) populates `CaddyGatewayPorts` by scanning **`caddy` / `caddy_*` Docker labels on running containers**. On Angie/Ferron, app containers will not carry `caddy` labels, causing `filterCaddyGatewayPorts` to drop **100% of HTTP gateway ports**! For non-Caddy engines (or post-migration slot apps), gateway eligibility must come from `allocated_ports` (`!Direct && !live.DirectHostPorts[ap.Port]` for running app projects) rather than `caddy` container labels.

---

## 6. `cmd/nd` (CLI & `nd-edge`) Architecture Mapping

### 6.1 `cmd/nd/internal/edge/*` Interfaces & Files
- **`config.go`**: `Paths` (`Caddyfile`, `CaddyNDDir`, `GlobalOpts`, `CommonCaddy`, `PanelsLink`, `OriginsLink`, `UserCaddyDir`, `CaddyCertsDir`, plus `/etc/nd-edge/config.json`, `/var/lib/nd-edge/*`, `/run/nd-edge.{lock,pid}`). `PanelGenDir(gen)` / `OriginGenDir(gen)`.
- **`apply.go` (819 lines)**:
  - `CaddyRunner` interface (`29-33`): `Validate(ctx, configPath) (string, error)`, `Reload(ctx, configPath, adminAddr) (time.Duration, error)`, `CheckAdmin(ctx, adminAddr) error`.
  - `SystemdRunner` interface (`37-42`): `IsActive`, `Start`, `CaddyUser`, `JournalCaddyErrors`.
  - `Applier.Apply`: computes `DesiredHash`, detects drift on active generation files, stages `panels.gen-N` + `origins.gen-N` + temporary validation copy `valPanelsDir`, runs `Validate`, then atomically swaps symlinks (`SwapDirLinkAtomic`), runs `Reload`, rolls back on reload failure, prunes keeping 3 generations.
- **`fs.go`, `lock_unix.go`, `lock_windows.go`, `chown_{posix,windows}.go`**:
  - `WriteFileAtomic`, `WriteJSONAtomic`, `SwapSymlinkAtomic`, `SwapDirLinkAtomic` (Linux symlink rename; Windows fallback to pointer file when unprivileged), `AcquireLock` (`flock` / `LockFileEx`), `ChownGroup`.
- **`render.go` (631 lines)**: `RenderPanelInput`, `RenderCaddyfileManagedRegion`, `RenderGlobalOpts*`, `RenderGlobalOptsForConfig`, `RenderCommonCaddy`, `RenderPlaceholderCaddy`, `RenderHealthProbeCaddy`, `RenderPanelCaddy`.
- **Golden & Contract Tests**:
  - `cmd/nd/internal/edge/render_test.go`: `TestRenderCoreTemplates`, `TestRenderPanelGoldenCases` (golden files under `testdata/`).
  - `cmd/nd/internal/edge/apply_test.go`, `reconciler_test.go`, `cmd/nd/cmd/edge_test.go` (tests unit content at `190-209`: explicitly forbids `Requires=caddy`, expects `After=/Wants= caddy.service`).
  - **Contract Fixture Divergence Found**: Root `testdata/edge_contract.json` (`99` lines, tested by `internal/handlers/edge_api_test.go:434-468`) has **3** routes (`dom_12`, `php_7`, `proxy_4`), `unregister_request.reason = "uninstall"`, and asserts exact `ComputeEdgeRoutesETag`. Meanwhile `cmd/nd/internal/edge/testdata/edge_contract.json` (`79` lines, tested by `cmd/nd/internal/edge/api_test.go:19-69` with `dec.DisallowUnknownFields()`) has **1** route (`dom_12`), `unregister_request.reason = "panel_removed"`. **Note**: BOTH contract files require `"caddy_version"` and `"caddy_running"` in `RegisterRequest` / `StatusRequest`; `api_test.go` uses `DisallowUnknownFields()`, so Phase 2.5 additive `proxy_engine` / `proxy_version` / `proxy_running` fields require updating the Go structs and both contract JSON fixtures in lockstep.

### 6.2 `cmd/nd/cmd/*` Commands (`edge.go`, `proxy.go`, `php*.go`, `shield.go`, `commands.go`)
- **`edge.go` (2479 lines)**: `edgeOps` dependency-injection struct (`46-70`) makes `runEdgeInstall`, `runEdgeDoctor`, `runEdgeUninstall` unit-testable, but `installCaddyPackage` (`685`), `prepareInstallCaddyfile` (`760`), `ensureInstallLayout` (`972`), `renderSystemdUnit` (`1126`), `runEdgeDoctor` (`1828`), and `runEdgeUninstall` (`2337`) hardcode `caddy` binary/Debian/RHEL repos/`caddy.service`/`Caddyfile`. Note `ensureInstallLayout` + `applyInstallPermsAndSELinux` runs `chown` to `CaddyUser` and `restorecon -R` when SELinux is enforcing — any `angie`/`ferron` edge installer must preserve SELinux + unprivileged service-user group read permissions.
- **`proxy.go` (663 lines)**: `RunProxy` dispatches `list/ls`, `add/create`, `rm/delete`, `enable`, `disable`, `toggle`, `reload`, `logs`, `status`, `options [show|set]`, calling `/api/v1/proxy/*`. Help text and `ui.Success` messages say `"Caddy"`; `proxy.go` currently has no `engine` subcommand (`Phase 3.6`).
- **`php.go`, `php_routing.go`, `commands.go`**: User-facing strings say `"Caddy"` (`php_routing.go:59-61,69`, `php.go:75`, `commands.go:3197-3231`).

---

## 7. Root Stack & Installer Ground Truth (`docker-compose*.yml`, `Dockerfile`, `install.sh`, `migrate.sh`, `uninstall.sh`)

- **`docker-compose.yml` & `docker-compose.local.yml`**:
  - `caddy`: `container_name: caddy`, ports `80:80`, `443:443/tcp`, `443:443/udp`, mounts `/var/run/docker.sock:/var/run/docker.sock`, `caddy_data:/data`, `/data/workspaces:/data/workspaces:ro` (`./data/workspaces:/data/workspaces:ro` in local).
  - `panel`: `container_name: panel`, `depends_on: - caddy` (**breaks if `caddy` service is removed/replaced unless updated!**), mounts `/data:/data`, `/opt/nextdeploy/docker-compose.yml:/stack/docker-compose.yml:rw` (`./docker-compose.local.yml:/stack/docker-compose.yml:rw` in local).
- **`Dockerfile` (`1-42`)**: `golang:1.24-alpine` build -> `docker:27-cli` runtime (`rclone`, `util-linux`, `bash`, `libstdc++`, `openssh-client`, `python3`, `curl`, `jq`, `git`). Proxy engines (`caddy`, `angie`, `ferron`) are NOT installed in `panel` container; `angie -t` / `ferron validate` / `docker exec` operate on the sibling proxy container (or `docker run --rm` during preflight dry-run).
- **`install.sh` (`666` lines)**:
  - Accepts `--domain`, `--email` (`CADDY_EMAIL` parsed at `:57`, unused elsewhere!), `--dir`, `--data-dir`.
  - `patch_data_dir_bind_mounts_in_compose` (`225-234`, duplicated in `nextdeploy-update` `:525-528`) ONLY rewrites `/data/workspaces:/data/workspaces:ro` and `/data:/data`. If non-Caddy compose mounts `/data/proxy/...:/etc/angie/nd:ro`, custom `--data-dir` will break unless this `sed` is generalized!
  - `create_update_script` (`359-546`) writes `/usr/local/bin/nextdeploy-update`, whose `merge_caddy_shared_volumes_from_previous_compose` (`427-496`) hardcodes `caddy:` (`442`), `caddy_data: {}` (`484`), and `- /data/workspaces:/data/workspaces:ro` (`471`), and downloads upstream `COMPOSE_URL` unconditionally (`docker-compose.yml` with `caddy`!), which would clobber an Angie/Ferron root compose on `nextdeploy-update` unless engine-aware preservation is added (Phase 3.1/3.2).
- **`uninstall.sh` (`408` lines)** & **`migrate.sh` (`166` lines)**:
  - `migrate.sh` delegates via `docker exec "$PANEL_CONTAINER" "$PANEL_BIN" migrate import ...`, calling `AfterAppImported` -> `SyncAppCaddyOverrideCtx` (`handlers/migrate.go:369`).
  - `uninstall.sh` stops `panel caddy` (`276-277`), removes `lucaslorentz/caddy-docker-proxy:ci-alpine` (`292`), and removes `nextdeploy_caddy_data` / `caddy_data` (`311-312`); must be updated to also stop `nd-proxy`, remove Angie/Ferron images, and remove proxy data (unless `--keep-data`).

---

## 8. Plan Audit (`docs/multi-proxy/`): Security, Concurrency & Implementation Traps

1. **POSIX Directory Rename Bug (`01-architecture.md:174`, `06-install-and-switch.md:49`)**:
   - **Plan states:** render into `conf/.staging-<gen>/` -> "atomic rename to `conf/sites/`".
   - **Fact:** In Linux/POSIX, `os.Rename(stagingDir, sitesDir)` **fails with `ENOTEMPTY`/`EEXIST`** if `sitesDir` already exists as a non-empty directory! Removing `sitesDir` before `Rename` creates a non-atomic race window.
   - **Fix:** Copy `cmd/nd/internal/edge`'s generation symlink swap (`sites` -> `sites.gen-<gen>`) with one critical container constraint: the symlink target **must be relative** (e.g., `sites.gen-000123`, not `/data/proxy/angie/sites.gen-000123`) so it resolves identically inside the `panel` container (`/data/proxy/angie/...`) AND inside the proxy container (`/etc/angie/nd/...`)! Also test single-file vs directory mounts on Windows dev hosts (`os.Symlink` fallback in `lock_windows.go:102-107` creates a plain text pointer file on unprivileged Windows, which Angie/Ferron would not follow as an include dir).
2. **Synchronous HTTP Contract & `PendingOriginApply` vs. 300ms Debounce (`01-architecture.md:170-175`)**:
   - Current endpoints return validation/apply errors synchronously and gate DB state on success: `PHPSecuritySave` (`php_security.go:127-129`), `CaddyRouteSave` (`caddy_options.go:296-302` — clears `PendingOriginApply` **only after** `syncProxyRoute` succeeds!), `PHPRoutingSave`, `PHPCreateSite`.
   - If `p.Proxy.Sync()` always returns after queuing a 300ms background debounce, broken config errors (`angie -t` / `ferron validate`) won't surface to API/CLI callers, AND `PendingOriginApply` would clear before the origin CA/TLS vhost is live on origin, causing Shield 502s.
   - **Fix:** Expose both `SyncNow(ctx, reason)` (coalesces into the single active batch and blocks callers until that batch's `Validate`+`Apply` result returns) and `SyncAsync(reason)` for background hooks.
3. **PHP-FPM Socket & DocRoot Container Path Mismatch (`02-capability-matrix.md:45`)**:
   - Plan matrix shows `unix:/run/php/<app>.sock`. Actual `phphosting.SocketPath` (`internal/phphosting/pool.go:37-39`) is `/data/workspaces/.nd-php/u<ownerID>-v<ver>/run/<appID>.sock`, which reaches Caddy via `/data/workspaces:/data/workspaces:ro` (`docker-compose.yml:25`). Verify in Phase 1.2/1.5 that the non-root UID inside the Angie/Ferron container can traverse/connect to FPM `.sock` files and read `htdocs` inside the `:ro` bind mount!
4. **Canonical `/api/v1/proxy/*` Dual Auth Conflict (`main.go:301-312` vs `01-architecture.md:183`, `08-tasks.md` 0.7)**:
   - `/api/v1/proxy/{routes,reload,logs,status,options}` **already exists** (`main.go:302-312`) using `p.APIAuthMiddleware` (CLI token auth), while web UI routes live at `/caddy/*` (`main.go:615-625`) behind session auth (`RequireAdminMiddleware` on `:384-388`, note `/caddy/routes` is not in `:384-390` because non-admins can manage their own routes per `caddy_options.go:180-183`). Task 0.7 must make `/api/v1/proxy/*` accept both session cookies and Bearer tokens without breaking `nd proxy`.
5. **Pre-existing `caddy_shared_volume_names` Serialization Bug (`spa.go:646-658` vs `settings.go:1146`)**:
   - `settings.go:1146` writes `caddy_shared_volume_names` via `json.Marshal(sharedNames)` (`["vol1","vol2"]`), whereas `spa.go:650` splits it with `strings.Split(raw, ",")`! `state.Build` must parse JSON array cleanly (with CSV fallback) and `spa.go` should be fixed in Phase 0.6/0.7.
6. **Security Traps**:
   - **SSRF (`routes.go:46`, `workspace_files.go:76`)**: `ValidateRouteForUser` skips `checkTargetSSRF` for admins and suffers DNS-rebinding TOCTOU between save time and Angie `resolver`/Ferron `logical_dns` resolution; `workspace_files.go:76` must add `"nd-proxy"` alongside `"caddy"`/`"panel"`.
   - **Real-IP Spoofing (`main.go:714-725`)**: Panel Fiber trusts `X-Forwarded-For` from all RFC1918 CIDRs (`10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`, `fc00::/7`). Angie/Ferron site templates **must** reproduce `applyUntrustedDirectProxyHeaders` (drop/overwrite incoming `X-Forwarded-For`, `X-Forwarded-Host`, `CF-Connecting-IP` when `!TrustedProxy && !BehindShield`).
   - **Directive/Config Injection & `BasicAuthHash` (`php_security.go:119`, `php_routing.go`, `edge/validate.go:67`)**: `RoutingRule.Pattern`/`Target` in Angie/Ferron require strict character/regex validation & escaping (`;`, `{`, `}`, newlines, `$` in nginx `proxy_pass`/`return`/variables, and escaping `$` or writing raw `.htpasswd` file without shell/template interpolation). Keep `phphosting.ValidAuthUser` enforced engine-wide.
   - **PKI Key Exposure**: When implementing `internal/proxy/pki` (Phase 1.1), store private CA key outside `/data/proxy/<engine>/` (`0700` dir, `0600` key) so proxy containers mounting config `:ro` never see the CA signing key — only issued leaf certs and public CA cert (`origin-ca.crt`).
   - **Concurrent `docker compose up` Races**: `settings.go:745` (`rootApplyMu`) and `panel.go:46` (`ComposeMu`) exist; ensure `proxy.Syncer` respects `rootApplyMu` / `ComposeMu` (or serializes Compose writes cleanly) and preserves `atomicWriteFile`'s `EBUSY`/`EXDEV` in-place write fallback (`panel.go:738-755`) for `/stack/docker-compose.yml` single-file bind mounts.

---

## 9. Concrete Recommendations Before/During Phase 0

1. Split god files proactively rather than adding engine code to `internal/caddy/caddy.go` (`1174` L), `handlers/settings.go` (`1499` L), or `cmd/nd/cmd/edge.go` (`2479` L): place new proxy code in `internal/proxy/{driver,ir,validate,sync}.go`, `internal/proxy/state/build.go`, `internal/proxy/{caddy,angie,ferron,pki}/`, and `cmd/nd/internal/edge/{engine,caddy,angie,ferron}/`.
2. Decouple `ExtractAndStripHostPorts` + `SyncAppPorts` from Caddy label generation before rewriting `deploy.go`, `rollback.go`, and `panel.go` callers to `p.Proxy.Sync*`.
3. Add `ListAllAppDomains(ctx)` in `internal/db/store_domains.go` and unify reading both `settings` and `caddy_config` in `state.Build`'s input loader.
4. Fix `dockerapi.containerIDByName("caddy")` substring match (`dockerapi.go:946`) and replace `filterCaddyGatewayPorts`'s Caddy-label check (`settings.go:433-450`) before testing Phase 1.7 gateway ports.
5. Reconcile the two `edge_contract.json` files (keep backward-compatible `caddy_version`/`caddy_running` populated alongside new `proxy_*` fields).
