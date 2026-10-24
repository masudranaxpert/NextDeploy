# NextDeploy Plugin System — রিসার্চ ও ডিজাইন প্রস্তাব

> স্ট্যাটাস: প্রস্তাব (draft) · তারিখ: 2026-10-24 · ব্রাঞ্চ বেস: `feat/react-ui-v2`

> **Status note:** this is the original, generic plugin-system research. The PHP hosting feature that motivated it was
> built as a **built-in module** instead (see [php-hosting.md](php-hosting.md)). The container-plugin framework described
> below is still a valid design for third-party extensions, but nothing in it is implemented yet.

## 0. সংক্ষেপে সিদ্ধান্ত (TL;DR)

| প্রশ্ন | সিদ্ধান্ত |
|---|---|
| ফোল্ডার, ব্রাঞ্চ, নাকি আলাদা রিপো? | **Plugin host (core)** থাকবে NextDeploy রিপোতেই (`internal/plugins/` + `src/plugins/`)। **প্রতিটা plugin** থাকবে আলাদা রিপোতে (বা শুরুতে একটা `nextdeploy-plugins` মনোরিপোতে)। **আলাদা long-lived ব্রাঞ্চ নয়।** |
| Plugin কিভাবে চলবে? | প্রতিটা plugin = একটা **Docker container** (compose), NextDeploy-এর internal network-এ, কোনো public port ছাড়া। |
| Auth কিভাবে? | Plugin-এর নিজস্ব কোনো login নেই। Browser → Panel (`/p/<plugin-id>/*`) → Panel-এর `AuthMiddleware` → Panel-এর reverse proxy → plugin। Panel একটা **short-lived signed identity token** (JWT) পাঠায়। |
| Plugin কি core-এর কাজ করতে পারবে (app বানানো, deploy)? | হ্যাঁ, কিন্তু **শুধু Panel API দিয়ে**, user-এর token দিয়ে। Docker socket কখনো পাবে না। |
| Limit (max_apps, memory, CPU, storage)? | সব limit **core-এ একটাই Quota service**-এ enforce হবে। Plugin API কল করলে একই কোড পাথ চলবে, তাই plugin দিয়ে limit বাইপাস অসম্ভব। Plugin container নিজেও limit-এর আওতায়। |

---

## 1. বর্তমান কোডবেস — যা আছে (রিসার্চের ভিত্তি)

- **Backend:** Go + Fiber v2 (`main.go`), SQLite (`internal/db`), routes সব `main.go`-তে হাতে লেখা।
- **Auth দুই রকম:**
  - Session cookie → `AuthMiddleware` ([internal/handlers/auth.go:62](../internal/handlers/auth.go)) — panel UI routes।
  - Bearer / `X-API-Key` token (fallback session) → `APIAuthMiddleware` ([internal/handlers/api_archive.go:20](../internal/handlers/api_archive.go)) — `/api/v1/*`।
  - Admin gate: `RequireAdminMiddleware`; app-level RBAC: `AppAccessMiddleware` + `CanAccessApp` (owner / collaborator viewer/developer) ([internal/handlers/rbac.go](../internal/handlers/rbac.go))।
- **Limits (users table):** `max_apps`, `max_memory_mb`, `max_cpus`, `max_storage_mb`।
  - `max_apps` চেক **দুই জায়গায় কপি করা** — `internal/handlers/apps.go:57` এবং `internal/mcp/handler.go:246`। `internal/migrate/import.go:149` এ কোনো চেক নেই।
  - চেকটা **count-then-insert** (transaction ছাড়া) → দুইটা parallel request একসাথে এলে limit পার হয়ে যেতে পারে (TOCTOU race)।
  - Memory/CPU: per-service clamp (`sandbox.ApplyResourceLimits`) + per-user cgroup slice (`user_limits.go`)। Storage: `CheckStorageQuota`।
- **Proxy:** `lucaslorentz/caddy-docker-proxy` — container label দেখে route বানায়; network `NextDeploy`। Panel নিজে `:8080`-এ।
- **Frontend:** React 19 + Vite, `web/dist` Go binary-তে `go:embed` করা। Sidebar nav `src/App.tsx` (~লাইন 738-797) এ hardcoded।
- **ইতিমধ্যে extension-এর মতো জিনিস:** MCP server + API token (scoped flags: reveal env, container exec, server exec, app delete) — এটাই plugin permission মডেলের ভালো ভিত্তি।

**উপসংহার:** নতুন plugin system-এর জন্য auth/RBAC/limit আবার বানাতে হবে না — শুধু (ক) limit-কে এক জায়গায় আনতে হবে, (খ) একটা proxy + identity layer বসাতে হবে, (গ) UI-তে dynamic nav।

---

## 2. বাইরের রিসার্চ — অন্যরা কিভাবে করে

| প্রজেক্ট | মডেল | শেখার বিষয় |
|---|---|---|
| **Grafana** | Backend = আলাদা process (HashiCorp go-plugin, gRPC); frontend = JS bundle; `plugin.json` manifest-এ `roles` ও page-level `action` দিয়ে RBAC | Manifest-এ permission declare করা, host enforce করে। Plugin crash করলে host crash করে না। |
| **Mattermost** | Manifest + server binary (go-plugin subprocess) + webapp bundle (registry-তে component register) | UI extension point (registry) প্যাটার্ন। |
| **Home Assistant add-ons (Ingress)** | প্রতিটা add-on = container; HA নিজে auth করে, `/api/hassio_ingress/<token>/` path দিয়ে proxy করে, user info header-এ পাঠায়; add-on-এর নিজের login বন্ধ | **NextDeploy-এর সাথে সবচেয়ে মেলে।** আমাদের Docker-native স্ট্যাকে এটাই সবচেয়ে স্বাভাবিক। |
| **Backstage** | Open-source plugins মনোরিপোতে, private plugins আলাদা রিপোতে; dynamic frontend = Module Federation | মনোরিপো dependency management সহজ করে; dynamic loading মানে repo layout-এ বাধ্যবাধকতা নেই। |
| **Coolify / Dokploy / CapRover** | Plugin system নেই — শুধু one-click templates (compose) + API/webhook | মার্কেটে plugin system একটা আলাদা হওয়ার সুযোগ। |

### Go-তে plugin চালানোর বিকল্প

| পদ্ধতি | সুবিধা | অসুবিধা | রায় |
|---|---|---|---|
| Go `plugin` package (`.so`) | in-process, দ্রুত | একই Go version/flags লাগে, Linux-only, unload হয় না, crash = panel crash | ❌ |
| HashiCorp go-plugin (subprocess + gRPC) | isolation, versioning, mTLS | panel container-এর ভেতরে binary চালাতে হবে — Docker resource limit/cgroup দেওয়া কঠিন, ভাষা প্রায় Go-only practically | ⚠️ পরে "lightweight hook plugin"-এর জন্য |
| WASM (wazero/extism) | শক্ত sandbox | ecosystem অপরিপক্ব, UI/long-running service কঠিন | ⚠️ ভবিষ্যতে hooks-এর জন্য |
| **Container + HTTP (HA Ingress style)** | যেকোনো ভাষা, Docker দিয়ে CPU/RAM limit, network isolation, আমাদের deploy pipeline reuse | একটু বেশি RAM | ✅ **প্রস্তাবিত** |

### নিরাপত্তা সতর্কতা (Caddy `forward_auth`)

- **CVE-2026-30851**: Caddy v2.10.0–v2.11.1-এ `forward_auth copy_headers` client-এর পাঠানো একই নামের header (যেমন `X-User-Id`) মুছে না → user identity জাল করা যায়। Fix: **2.11.2**।
- **CVE-2026-52845**: `Remote_User` (underscore) variant দিয়ে FastCGI-তে bypass। Fix: **2.11.4**।
- আমরা `lucaslorentz/caddy-docker-proxy:ci-alpine` (floating tag) ব্যবহার করি — version pin করা নেই।

👉 তাই identity header-এর উপর ভরসা না করে **signed token (JWT)** পাঠাবো, এবং proxy-টা **Panel-এর Go কোডেই** রাখবো (session যেহেতু panel-এর কাছেই আছে)। Caddy শুধু বাইরের TLS/edge হিসেবে থাকবে, যেমন এখন আছে।

---

## 3. Repo কৌশল — ফোল্ডার vs ব্রাঞ্চ vs আলাদা রিপো

**ব্রাঞ্চ — না।** Plugin একটা আলাদা product lifecycle; long-lived ব্রাঞ্চ মানে নিয়মিত merge conflict, আর release-এ ব্রাঞ্চ থেকে আলাদা artifact বের করা ঝামেলা। ব্রাঞ্চ শুধু ফিচার ডেভেলপমেন্টের জন্য (`feat/plugin-host`)।

**প্রস্তাবিত লেআউট:**

```
NextDeploy/                         ← এই রিপো (core + plugin host)
  internal/plugins/                 ← registry, manifest parser, proxy, lifecycle, signer
  internal/quota/                   ← একক Quota service (নতুন)
  pkg/pluginsdk/                    ← Go SDK (JWT verify, panel API client) — public, import-যোগ্য
  src/plugins/                      ← frontend host: dynamic nav, iframe/slot loader
  docs/plugin-system-design.md
  examples/plugins/hello/           ← একটা ছোট reference plugin (CI-তে e2e টেস্টের জন্য)

nextdeploy-plugins/                 ← নতুন রিপো (official plugins মনোরিপো)
  plugins/uptime-monitor/
  plugins/db-manager/
  registry.json                     ← plugin catalog (id, version, image, sha256 digest)

<third-party>/nd-plugin-xyz         ← কমিউনিটি plugin, নিজের রিপো, শুধু manifest + image publish
```

কেন এভাবে:
- Host কোড core auth/limit-এর সাথে tight-coupled → একই রিপোতে থাকলে একসাথে বদলায়, একসাথে টেস্ট হয়।
- Plugin আলাদা রিপো → আলাদা release cycle, আলাদা CI, image হিসেবে publish; core রিলিজ না করেই plugin আপডেট।
- Official plugins একটা মনোরিপোতে → Backstage/Roadie-র অভিজ্ঞতা: dependency version একসাথে আপগ্রেড সহজ।
- `pkg/pluginsdk` core রিপোতে → contract বদলালে SDK একসাথে বদলায়; `go.mod` module path `panel` থেকে public path-এ নিতে হবে (যেমন `github.com/masudranaxpert/NextDeploy`) — না হলে বাইরের রিপো import করতে পারবে না। বিকল্প: SDK আলাদা ছোট রিপো `nextdeploy-plugin-sdk`।

---

## 4. আর্কিটেকচার

```
Browser
  │  https://panel.example.com/p/uptime/...
  ▼
Caddy (edge TLS)  ──►  Panel (Go/Fiber :8080)
                          │ 1. AuthMiddleware (session cookie)  ← বিদ্যমান
                          │ 2. PluginAccessMiddleware: plugin enabled? user role allowed?
                          │ 3. সব X-ND-* / Authorization / Cookie header strip
                          │ 4. X-ND-Identity: <JWT, 60s, plugin-specific audience>
                          ▼
                     plugin container  (network: nd-plugins-<id>, no published port)
                          │
                          │ Panel API কল (identity JWT বা plugin service token সহ)
                          ▼
                     Panel /api/plugin/v1/*  → একই handler/Quota/RBAC কোড পাথ
```

### 4.1 Manifest (`nextdeploy-plugin.yaml`)

```yaml
apiVersion: nextdeploy.dev/v1
id: uptime-monitor            # [a-z0-9-], unique
name: Uptime Monitor
version: 1.2.0
minPanelVersion: 2.5.0
image: ghcr.io/nextdeploy/uptime-monitor@sha256:...   # digest pin বাধ্যতামূলক
port: 8080                    # container-এর ভেতরের port
healthcheck: /healthz

scope: user                   # "system" (শুধু admin) | "user" (প্রতি user-এর জন্য দেখা যাবে)

ui:
  nav:
    - { label: "Uptime", path: "/", icon: "activity", section: "monitoring", minRole: user }
  appTabs:                    # app workspace-এ একটা নতুন ট্যাব
    - { label: "Uptime", path: "/apps/{appId}", requires: "apps:read" }

permissions:                  # plugin যা চাইতে পারে; admin install-এর সময় দেখে approve করে
  - apps:read
  - apps:deploy
  - domains:read
  # - apps:create   ← থাকলে user-এর max_apps quota-র মধ্যে গুনা হবে

resources:                    # plugin container-এর নিজের limit
  memoryMB: 256
  cpus: 0.25
  storageMB: 512              # plugin data volume

hooks:                        # (ঐচ্ছিক, phase 3) core event → plugin webhook
  - event: app.deployed
    path: /hooks/deployed
```

### 4.2 Database (নতুন migration)

```sql
CREATE TABLE plugins (
  id TEXT PRIMARY KEY, version TEXT, image_digest TEXT,
  manifest_json TEXT NOT NULL, status TEXT NOT NULL,      -- installed|enabled|disabled|error
  signing_secret BLOB NOT NULL,                            -- প্রতি plugin আলাদা HMAC key
  granted_permissions TEXT NOT NULL,                       -- admin যেগুলো approve করেছে
  installed_by INTEGER, installed_at DATETIME
);
CREATE TABLE plugin_user_access (                          -- scope=user হলে কে ব্যবহার করতে পারবে
  plugin_id TEXT, user_id INTEGER, enabled INTEGER, PRIMARY KEY(plugin_id, user_id)
);
CREATE TABLE plugin_kv (                                   -- ছোট plugin সেটিংস, per-user
  plugin_id TEXT, user_id INTEGER, key TEXT, value BLOB, PRIMARY KEY(plugin_id,user_id,key)
);
```
Audit log-এ সব install/enable/permission-grant এবং plugin-originated API কল লেখা হবে (`actor = user, via = plugin:<id>`)।

### 4.3 Lifecycle (`internal/plugins/lifecycle.go`)

বিদ্যমান compose deploy pipeline reuse করবো:
1. Install: manifest fetch → schema validate → image digest pull → admin-কে permission list দেখানো → approve।
2. Compose generate: `sandbox.ValidateAndClampCompose` দিয়ে —
   - `docker.sock`, host bind mount, `privileged`, `cap_add`, `network_mode: host`, `pid: host` **নিষিদ্ধ** (validator এখনই অনেকটা করে)।
   - `read_only: true`, `security_opt: no-new-privileges`, `cap_drop: [ALL]`, non-root user।
   - শুধু named volume `nd-plugin-<id>-data`।
   - network: `nd-plugins-<id>` (internal: true) — panel এই network-এ join করবে; plugin অন্য user-এর app container দেখতে পাবে না।
   - Caddy label **দেওয়া হবে না** — plugin সরাসরি public না।
3. Health check → `enabled`। Disable = container stop; Uninstall = container + (ঐচ্ছিক) volume মুছা।

### 4.4 Auth ফ্লো — বিস্তারিত

**(ক) Browser → plugin (UI এবং plugin-এর নিজের API)**

`main.go`-তে `app.Use(p.AuthMiddleware)`-এর **পরে**:
```go
app.All("/p/:plugin/*", p.PluginAccessMiddleware, p.PluginProxy)
```
`PluginProxy` (Fiber `middleware/proxy` বা `fasthttp.HostClient`):
- Request থেকে **মুছে দেবে**: `Cookie`, `Authorization`, `X-API-Key`, সব `X-ND-*`, `X-Forwarded-User` ইত্যাদি (underscore variant সহ — case/`-`/`_` normalize করে)।
- যোগ করবে: `X-ND-Identity: <JWT>`, `X-Forwarded-Prefix: /p/<id>`।
- JWT (HS256, plugin-এর নিজস্ব `signing_secret`, TTL ৬০ সেকেন্ড):
  ```json
  { "iss": "nextdeploy", "aud": "plugin:uptime-monitor", "sub": "42",
    "username": "alice", "role": "user", "perms": ["apps:read","apps:deploy"],
    "exp": 1793000060, "jti": "..." }
  ```
- WebSocket ও SSE proxy সাপোর্ট লাগবে (`/p/:plugin/ws` — বিদ্যমান `WSUpgrade` প্যাটার্ন)।
- CSRF: বিদ্যমান panel CSRF/SameSite নিয়ম `/p/*`-এ প্রযোজ্য; non-GET-এ `Origin` চেক।

**(খ) Plugin → Panel API**

দুই ধরনের credential:
1. **On-behalf-of user** (সাধারণ কেস): plugin পাওয়া `X-ND-Identity` JWT-টাই `Authorization: Bearer` দিয়ে `/api/plugin/v1/*`-এ ফেরত পাঠাবে। Panel যাচাই করবে: signature, `aud`, `exp`, এবং plugin এখনো enabled কিনা। তারপর `c.Locals(contextUserKey, user)` সেট করে **বিদ্যমান handler-ই চালাবে**।
   - Effective permission = `user-এর RBAC ∩ plugin-এর granted_permissions`। অর্থাৎ plugin কখনো user-এর চেয়ে বেশি পারবে না, আর user plugin-কে যা দেয়নি তা-ও পারবে না।
   - Long-running কাজের জন্য (৬০s-এর বেশি): `POST /api/plugin/v1/token/exchange` → ১৫ মিনিটের scoped token।
2. **Plugin service token** (background job, user ছাড়া): শুধু `scope: system` plugin-এর জন্য; owner = installing admin; শুধু manifest-এর permission।

Plugin সরাসরি SQLite বা Docker socket কখনো ছোঁবে না।

**(গ) Plugin UI কিভাবে দেখাবে — দুই ধাপ**

- **Phase 1 — iframe (সবচেয়ে নিরাপদ, ভাষা-নিরপেক্ষ):** `src/plugins/PluginFrame.tsx` → `<iframe src="/p/<id>/..." sandbox="allow-scripts allow-forms allow-same-origin">`। একই origin হলেও plugin-এর response-এ panel কঠোর CSP বসাবে। Theme/route sync-এর জন্য ছোট `postMessage` bridge (`nd:theme`, `nd:navigate`, `nd:toast`)।
  - ⚠️ একই origin-এ iframe চালালে plugin JS panel-এর cookie/DOM ছুঁতে পারে (session cookie `HttpOnly` হলেও panel API কল করতে পারবে)। তাই **third-party plugin-এর জন্য আলাদা origin** ভালো: `https://plugins.<panel-domain>/p/<id>/` (একই panel backend, আলাদা Host) — Caddy label দিয়ে সহজ। তখন panel cookie plugin origin-এ যাবে না; panel একটা one-time launch token দিয়ে plugin-origin-এ ছোট session বানাবে।
- **Phase 3 — native slot (official/trusted plugins):** Vite Module Federation (`@module-federation/vite`) দিয়ে React component লোড — দেখতে native, কিন্তু কোনো sandbox নেই; তাই শুধু signed/official plugin-এর জন্য।

### 4.5 Limit / Quota — সবচেয়ে গুরুত্বপূর্ণ অংশ

**নীতি:** limit plugin-এ না, **core-এ একটাই জায়গায়**, এবং **atomic**।

1. নতুন `internal/quota` package:
   ```go
   type Service struct{ DB *db.Store; ... }
   // ReserveApp একটা transaction-এর ভেতরে count + insert করে; limit পার হলে ErrQuotaExceeded।
   func (s *Service) CreateAppWithQuota(ctx, owner db.User, id, name string) error
   func (s *Service) CheckStorage(ctx, ownerID int64, incoming int64) error   // বিদ্যমান CheckStorageQuota এখানে আসবে
   func (s *Service) ClampCompose(owner db.User, compose []byte) ([]byte, error) // ApplyResourceLimits + cgroup parent
   func (s *Service) Usage(ctx, userID int64) (Usage, error)                     // UI/plugin-কে দেখানোর জন্য
   ```
   SQLite-এ: `BEGIN IMMEDIATE; SELECT COUNT(*) ...; INSERT ...; COMMIT;` — `BEGIN IMMEDIATE` write lock নেয়, তাই দুইটা parallel create একসাথে limit পার হতে পারবে না।
2. `handlers/apps.go`, `mcp/handler.go`, `migrate/import.go` — তিনটাই এই service কল করবে (এখন দুইটায় কপি করা, একটায় নেই)। এটা plugin ছাড়াও একটা বিদ্যমান বাগ ফিক্স।
3. Plugin API-ও একই service দিয়ে যায় → plugin দিয়ে limit পার হওয়া অসম্ভব।
4. **Plugin container-এর নিজের resource:**
   - `scope: system` plugin: admin-এর/সিস্টেমের খরচ; global cap (`settings.plugin_max_total_memory_mb`)।
   - `scope: user` plugin যেটা **প্রতি user-এর জন্য আলাদা container** চালায় (যেমন user-এর নিজের DB manager): সেই container user-এর cgroup slice-এ (`UserCgroupParent`) চলবে এবং `max_memory_mb/max_cpus/max_storage_mb`-এ গুনা হবে। চাইলে `max_plugins` নামে নতুন কলাম।
5. Quota error-এর জন্য স্ট্যান্ডার্ড response: `409 {"error":"quota_exceeded","limit":"max_apps","used":5,"max":5}` — plugin UI এটা দেখে সুন্দর মেসেজ দেখাতে পারবে।
6. `GET /api/plugin/v1/me/quota` → plugin আগেই জানতে পারবে কতটুকু বাকি।

### 4.6 Plugin API scopes (প্রস্তাবিত শুরু)

| scope | কী পারে |
|---|---|
| `user:read` | নিজের প্রোফাইল, role, quota usage |
| `apps:read` | user যে app দেখতে পারে সেগুলোর list/status/logs |
| `apps:create` | app বানানো (quota-র মধ্যে) |
| `apps:deploy` | deploy/restart/stop (developer role লাগবে) |
| `apps:env:read` | env দেখা (বিদ্যমান "reveal env" ফ্ল্যাগের মতো, সংবেদনশীল) |
| `domains:read` / `domains:write` | domain |
| `events:subscribe` | `app.deployed`, `app.deleted`, `user.quota.changed` webhook |
| `exec:container` | container exec — **উচ্চ-ঝুঁকি**, install-এ লাল warning |

---

## 5. ফেজ অনুযায়ী ইমপ্লিমেন্টেশন প্ল্যান

**Phase 0 — ভিত্তি (plugin ছাড়াই উপকারী)**
- [ ] `internal/quota` বানানো, `max_apps` চেক atomic করে তিন জায়গা থেকে কল করা (+ race টেস্ট)।
- [ ] Caddy image version pin (≥ 2.11.4 base)।
- [ ] `go.mod` module path public করা (SDK import-এর জন্য) — বা SDK আলাদা রিপো।

**Phase 1 — MVP (admin-installed, iframe UI)**
- [ ] `internal/plugins`: manifest parse/validate, DB migration, install/enable/disable/uninstall, compose generate + hardening।
- [ ] `PluginProxy` + header strip + JWT signer; HTTP + WebSocket।
- [ ] `/api/plugin/v1/*` — JWT verify middleware, permission intersection, প্রথমে `user:read`, `apps:read`।
- [ ] UI: Settings → Plugins পেজ (install from image/manifest URL, permission approval dialog); sidebar-এ dynamic nav (`/api/panel/bootstrap`-এ `plugins` ফিল্ড যোগ); `PluginFrame`।
- [ ] `examples/plugins/hello` (Go, ~১০০ লাইন) + e2e টেস্ট: forged `X-ND-*` header strip হয় কিনা, অন্য user-এর app দেখা যায় না, quota পার হয় না।

**Phase 2 — User-scoped plugins ও write API**
- [ ] `plugin_user_access`, per-user enable, user-scoped container + cgroup/quota গণনা।
- [ ] `apps:create`, `apps:deploy`, `domains:*`, token exchange।
- [ ] App workspace-এ plugin tab।
- [ ] আলাদা plugin origin (`plugins.<domain>`) third-party-র জন্য।

**Phase 3 — Ecosystem**
- [ ] `nextdeploy-plugins` রিপো + `registry.json` catalog + cosign signature verify।
- [ ] Event hooks (outbound webhook, HMAC signed, retry)।
- [ ] Module Federation native slot (শুধু official signed plugin)।
- [ ] Plugin update/rollback (বিদ্যমান rollback কোড reuse)।

---

## 6. ঝুঁকি ও প্রতিরোধ

| ঝুঁকি | প্রতিরোধ |
|---|---|
| Header spoof করে অন্য user সাজা | সব identity header strip + শুধু signed JWT; plugin SDK শুধু JWT মানবে |
| Plugin panel session চুরি (same-origin iframe) | third-party plugin আলাদা origin-এ; panel cookie `HttpOnly`+`SameSite=Strict`; plugin response-এ CSP |
| Plugin দিয়ে docker host দখল | docker.sock/privileged/host mount নিষিদ্ধ (validator), `cap_drop: ALL`, non-root |
| Plugin অন্য app-এর network-এ ঢোকা | প্রতি plugin আলাদা internal network, শুধু panel joined |
| Limit বাইপাস / race | একক atomic Quota service; plugin API একই কোড পাথ |
| Malicious image / supply chain | digest pin, official registry-তে cosign signature, install-এ permission review |
| Plugin ধীর হলে panel ধীর | proxy timeout, per-plugin concurrency limit, health check-এ auto-disable |
| API বদলালে plugin ভাঙা | `/api/plugin/v1` ভার্সনড; manifest-এ `minPanelVersion`; deprecation নীতি |

---

## সূত্র (Sources)

- Grafana backend plugins (go-plugin, gRPC): https://grafana.com/developers/plugin-tools/key-concepts/backend-plugins/
- Grafana RBAC for app plugins: https://grafana.com/developers/plugin-tools/how-to-guides/app-plugins/implement-rbac-in-app-plugins
- Grafana plugin frontend sandbox: https://grafana.com/docs/grafana/latest/administration/plugin-management/plugin-frontend-sandbox/
- Mattermost plugins: https://developers.mattermost.com/contribute/more-info/server/plugins
- Home Assistant Ingress: https://www.home-assistant.io/blog/2019/04/15/hassio-ingress/ · https://deepwiki.com/home-assistant/supervisor/6.3-proxy-and-ingress
- Backstage module federation: https://backstage.io/docs/frontend-system/building-apps/module-federation · Backstage FAQ: https://backstage.io/docs/next/faq/technical · Roadie monorepo migration: https://roadie.io/blog/monorepo-plugins-migration/
- HashiCorp plugin model (Nomad): https://developer.hashicorp.com/nomad/docs/v1.8.x/concepts/plugins
- Caddy forward_auth: https://caddyserver.com/docs/caddyfile/directives/forward_auth
- CVE-2026-30851: https://corgea.com/advisories/vulnerabilities/CVE-2026-30851 · CVE-2026-52845: https://osv.dev/vulnerability/CVE-2026-52845
- Vite Module Federation (Nx): https://nx.dev/docs/technologies/module-federation/vite-module-federation
- Coolify architecture: https://coolify.io/docs/core/what-is-coolify · Dokploy templates: https://docs.dokploy.com/docs/templates
