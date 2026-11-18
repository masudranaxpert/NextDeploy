# 01 — Architecture

## 1. Current state (as-is)

NextDeploy's app proxy is **caddy-docker-proxy** (`lucaslorentz/caddy-docker-proxy:ci-alpine`, container `caddy`, network `NextDeploy`). The panel never writes a Caddyfile for apps. It writes **Docker labels** onto app containers (via generated compose overrides), and caddy-docker-proxy turns the labels into Caddy config at runtime.

### 1.1 Caddy coupling inventory

| Area | File(s) | What it does today |
|---|---|---|
| Constants | `internal/caddy/caddy.go` (`DefaultAdminAPI`, `CaddyContainerName`, `CaddyImage`, `CaddyNetwork`, `GeneratedCompose`) | Hardcodes engine identity |
| App site labels | `internal/caddy/caddy.go`: `GenerateLabels`, `GenerateServiceLabels`, `appendSiteLabels`, `normalizedRoutes` | Domain, www, HTTPS/`tls internal`, security headers, path routes (`handle_path` + `file_server` for direct static/media; `reverse_proxy {{upstreams PORT}}`), `stream_close_delay 10m`, untrusted-proxy header stripping |
| App compose merge | `GenerateMergedCompose`, `normalizeServiceContainerNames`, `attachNetwork`, `mergeLabels`, `injectEnvFile` | Writes `.nextdeploy.generated.compose.yml` with labels + `NextDeploy` network |
| Blue/green slots | `internal/caddy/slot_compose.go`: `GenerateSlotLabels`, `SlotPreviewDomain`, `GenerateSlotMergedCompose(WithEnv)`, `ExtractAndStripHostPorts`, `HostPortBinding` | Per-slot labels, preview hostnames (`<slot>-…`), host-port gateways moved to Caddy |
| Gateway ports | `MergeCaddyGatewayPorts` | Publishes allocated app ports on the Caddy container |
| Root stack | `GenerateRootStackCompose` (+ `NDSharedMount`, `mergeNDSharedMountsIntoDoc`) | Caddy service in the root compose: image, admin, panel domain labels, global options labels, shared volume mounts (`nd_shared_*`) for file_server roots |
| Global options | `internal/caddy/options.go`: `GlobalOptions` (profile, read_header, read_body_idle, idle, grace_period, max_header_size, real_ip none/cloudflare/custom, trusted_proxies, trusted_strict, shield_proxies, direct_cloudflare), `OptionProfiles`, `Labels()` | Rendered as `caddy.servers.*` labels on the Caddy container |
| Custom proxy routes | `internal/caddy/routes.go`: `ValidateRoute(ForUser)` (SSRF checks), `RouteLabels`; handlers `caddy_options.go`: `syncProxyRoute` (container `nd-route-<id>`), `ReconcileProxyRoutes` | Proxy/redirect routes to external targets, implemented as tiny label-only containers |
| PHP / static sites | `internal/caddy/php_labels.go`: `PHPSiteLabels(WithRouting)`, `SiteSecurity`; `internal/phphosting/*` | `root`, `php_fastcgi unix//…/<app>.sock`, `encode`, caching headers, `file_server.hide`, IP block/allow, basic auth (bcrypt), block xmlrpc / upload-php / scanner UAs, HSTS, frame protection |
| Routing rules | `internal/caddy/php_routing.go`: `RoutingRule` (rewrite/redir/block; exact/prefix/regex; unless_exists; require_target_file; preserve_query), `SiteRouting` (fallback default/strict_404/custom) | `.htaccess` import (`internal/htaccess`) feeds these |
| Admin / status | `AdminStatus`, `AdminConfigGet`, `AdminConfigPost`; handlers `CaddyOverview`, `CaddyLogs`, `CaddyContainerAction`, `CaddySaveConfig` | Health + overview UI, logs, restart |
| TLS helpers | `ShouldUseInternalTLS`, `IsBehindCloudflare`, `FormatWildcardDomain` (sslip.io/nip.io), `ShortPanelID` | Dev and wildcard domains, Cloudflare detection |
| Shield origin CA | `handlers/edge_shield.go`: `GetOriginCaddyRootCA`, `ReadOriginCaddyRootCA` | nd-edge trusts the origin Caddy local CA for `tls internal` sites |
| Handlers using `internal/caddy` | `handlers/{caddy,caddy_options,panel,php_*,preview,settings,spa,workspace_files}.go`, `internal/{agent,mcp,htaccess,phphosting,workspace}` | Callers to re-point at `internal/proxy` |
| Installer | `install.sh` (`--email`, preserves panel Caddy labels and `nd_shared` volumes across upgrades) | Writes root compose with Caddy |
| Settings | keys `caddy_image`, `caddy_version`, `caddy_shared_volumes`, `caddy_shared_volume_names`, `caddy_shared_mount_prefix` | Engine-specific |
| UI | `src/features/...` Caddy views (`CaddyOptionsView`, `CaddyProxyView`, `CaddyRoutesView`), shield components | "Caddy" naming everywhere |
| nd-edge | `cmd/nd/internal/edge/{render,apply,reconciler,daemon}.go`, `cmd/nd/cmd/edge.go` | Renders Caddyfile, `CaddyRunner` validate/reload, installs Caddy package |

## 2. Target design

```
                   DB (apps, domains, slots, php sites, routes, options, shield)
                                        │
                              internal/proxy/state
                         BuildDesiredState(ctx) → *State (IR)
                                        │
                     ┌──────────────────┼──────────────────┐
                     ▼                  ▼                  ▼
            proxy/caddy driver   proxy/angie driver   proxy/ferron driver
            (labels via existing (files + docker      (files + logical_dns
             internal/caddy)      label upstreams)     + watch reload)
                     │                  │                  │
                     └──────── proxy.Syncer (debounced apply) ┘
```

### 2.1 New packages

```
internal/proxy/              // engine-agnostic API used by handlers
  driver.go                  // Driver interface, Capabilities, registry
  ir.go                      // desired-state IR types
  state/build.go             // DB → IR (pure, unit-testable)
  sync.go                    // Syncer: debounce, render, validate, apply, rollback
  pki/                       // NextDeploy internal CA for non-Caddy "internal" TLS
  caddy/                     // wraps internal/caddy (labels); no behavior change
  angie/                     // templates + runner
  ferron/                    // templates + runner
```

`internal/caddy` stays as the Caddy implementation detail. Handlers stop importing it directly, except validation helpers (`ValidateRoute*`, `ValidateRouting`, `NormalizeIPList`, `CleanQuotedValue`). Move those to `internal/proxy/validate.go` and keep thin aliases in `internal/caddy` for one release.

### 2.2 IR (desired state)

```go
package proxy

type TLSMode string // "auto" (ACME) | "internal" (private CA) | "none" (http only)

type State struct {
    Global      GlobalOptions      // from caddy.GlobalOptions, renamed, engine-neutral
    Panel       *Site              // panel domain (root stack)
    Sites       []Site             // app domains, PHP/static sites, proxy routes, slot previews
    Gateways    []GatewayPort      // allocated host ports → app upstreams
    SharedMounts []SharedMount     // nd_shared_* volumes needed for file roots
    Generation  string             // hash of the IR (for idempotent apply)
}

type Site struct {
    ID          string             // stable: "app:<appID>:dom:<domainID>", "php:<appID>", "route:<id>", "panel"
    Hosts       []string           // includes www.<domain> when EnableWWW
    TLS         TLSMode
    BehindShield bool              // ShieldNodeID != nil
    TrustedProxy bool              // trust forwarded headers (Cloudflare/shield)
    Headers     []HeaderOp         // security headers (set/add/del)
    Kind        SiteKind           // AppProxy | PHP | Static | Redirect | ExternalProxy
    Paths       []PathRoute        // ordered, longest-prefix wins
    Default     *Handler           // catch-all handler
    PHP         *PHPSpec           // root, fpm socket, fallback, routing rules
    Security    *SecuritySpec      // ip allow/block, basic auth, block xmlrpc/upload-php/scanners, hsts, frame
    Redirect    *RedirectSpec      // code, target, keep path
    Upstream    *UpstreamRef       // app upstream (see 2.3)
}

type PathRoute struct {
    Prefix   string
    Handler  Handler               // StaticDir{Root, Hide[]} | Proxy{Upstream}
    StripPrefix bool               // Caddy handle_path semantics
}

type UpstreamRef struct {
    Name       string              // stable: nd_<appID>_<service>_<port>[_<slot>]
    Service    string              // compose service
    Container  string              // resolved container name (for ferron logical_dns)
    Port       int
    Scheme     string              // http | https
    SkipVerify bool                // proxy routes with self-signed targets
    External   string              // for ExternalProxy routes: full URL
    LongStreams bool               // keep streams on reload (Caddy stream_close_delay 10m)
}
```

Rules:
- `state/build.go` must be **pure** (inputs passed in, no Docker calls) so golden tests can cover it.
- `Generation` = SHA-256 of a canonical JSON of the IR. The Syncer skips the apply when it is unchanged.

### 2.3 Upstream addressing per engine

| Engine | How a site reaches an app container | Survives container recreate without reload? |
|---|---|---|
| Caddy | `{{upstreams PORT}}` in labels on the app container (caddy-docker-proxy resolves IPs) | Yes (today) |
| Angie | `upstream <Name> { zone <Name> 64k; keepalive 64; }` in config; app container gets labels `angie.http.upstreams.<Name>.port=<Port>` and `angie.network=NextDeploy` | Yes (Docker module, tested) |
| Ferron | `proxy { upstream http://<Container>:<Port> { logical_dns true } }` | Yes **only** with `logical_dns true` (tested; default strict DNS stays broken) |

So the **app compose generator must emit engine-specific labels**: Caddy site labels, Angie upstream labels, or no labels for Ferron. This is the main seam in `GenerateMergedCompose` / `GenerateSlotMergedCompose`. Add an `AppLabeler` hook from the driver.

### 2.4 Driver interface

```go
type Driver interface {
    Name() string                          // "caddy" | "angie" | "ferron"
    Capabilities() Capabilities

    // Root stack integration (compose service for the proxy container).
    StackService(opts StackOptions) (service map[string]any, volumes map[string]any, err error)

    // Labels to put on app containers (Caddy: full site labels; Angie: upstream labels; Ferron: nil).
    AppLabels(site []Site, up UpstreamRef) map[string]string

    // File-rendered engines (Angie/Ferron). Caddy returns empty.
    Render(st *State) (Files, error)       // path → content, relative to engine conf dir
    Validate(ctx context.Context, staged string) error // e.g. `angie -t -c`, `ferron validate -c`
    Apply(ctx context.Context, files Files) (ApplyResult, error) // atomic swap + reload, keeps previous generation

    Status(ctx context.Context) (Status, error)        // up, version, connections, last reload
    Logs(ctx context.Context, lines int) ([]string, error)
    Restart(ctx context.Context) error
}

type Capabilities struct {
    AutoTLS, InternalTLS, OnDemandTLS bool
    DockerUpstreamDiscovery bool
    PHPFastCGI, BasicAuthBcrypt, RegexRouting bool
    H2IdleTimeout, WriteTimeout, ReadBodyTimeout bool
    LongStreamsOnReload bool
    Experimental bool
}
```

`Capabilities` drives UI gating and the switch preflight (06).

### 2.5 Sync model

Every place that today calls `SyncAppCaddyOverrideCtx`, `SyncAppSlotComposeCtx`, `syncProxyRoute`, `ReconcileProxyRoutes`, PHP site save/security/routing save, settings save, or panel domain change must call:

```go
p.Proxy.Sync(ctx, proxy.Reason{Kind: "app_domain", ID: appID})
```

Syncer behavior:
1. Debounce 300 ms. Coalesce reasons.
2. `state.Build` → IR → `Generation`. Return if unchanged.
3. Caddy: regenerate compose overrides as today (labels), `docker compose up -d` the affected app. No reload is needed because caddy-docker-proxy watches events.
4. Angie/Ferron: `Render` into `conf/.staging-<gen>/` → `Validate` inside the proxy container → atomic rename to `conf/sites/` (keep `conf/.prev`) → reload (Angie `angie -s reload`; Ferron: the file watcher picks it up, or `POST /reload` on the admin API) → health check (`/health` or TCP 443) → on failure restore `.prev` and reload again.
5. Record the result in a `proxy_apply` table (`generation`, `driver`, `status`, `error`, `at`) for the UI.

App labels (Angie upstream labels) are applied through the existing compose flow. The Docker module adds the container to the upstream **without** a reload. Reloads are only needed for site/TLS/routing changes.

### 2.6 Panel naming

- Introduce `p.Proxy` (driver + syncer) in `handlers.Panel`.
- Rename UI "Caddy" → "Proxy" with an engine badge ("Caddy" / "Angie" / "Ferron · experimental").
- Keep API paths stable (`/api/.../caddy/...` aliases). Add `/api/v1/proxy/...` as canonical.
