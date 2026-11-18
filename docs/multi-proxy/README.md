# Multi-proxy support: Caddy, Angie, Ferron

Status: **implemented** across Phases 0 to 5 on branch `feat/multi-proxy-drivers` (Ferron stays experimental). This folder is the original plan, corrected after live verification. Where it differs from the code, the code and the records below win:

- Decisions: [`docs/adr/001-multi-proxy-drivers.md`](../adr/001-multi-proxy-drivers.md)
- Verification reports: [`docs/spikes/2026-11-18-multi-proxy-engines.md`](../spikes/2026-11-18-multi-proxy-engines.md), [`docs/spikes/2026-11-18-php-and-htaccess-deep-dive.md`](../spikes/2026-11-18-php-and-htaccess-deep-dive.md)
- User guide: [`docs/proxy-engines.md`](../proxy-engines.md)

## সংক্ষেপে (বাংলা)

NextDeploy এখন শুধু Caddy (caddy-docker-proxy) দিয়ে চলে। এই plan অনুযায়ী তিনটা proxy engine সাপোর্ট করা হবে:

| Engine | ভূমিকা | কেন |
|---|---|---|
| **Caddy** | default, এখন যা আছে তাই থাকবে | সবচেয়ে সহজ, Docker label দিয়ে সব নিজে বানায়, কোনো regression হবে না |
| **Angie** | "low-RAM / high-traffic" engine, shield (nd-edge)-এর জন্য recommended | nginx-এর মতো হালকা (~৪০ KB/connection), idle HTTP/2 নিজে বন্ধ করে, Docker label দিয়ে reload ছাড়া container IP বদল ধরে, ACME built-in, BSD license, ফ্রি |
| **Ferron** | experimental | সবচেয়ে দ্রুত (throughput), memory সবচেয়ে ভালো ফেরত দেয়, কিন্তু এখন HTTP/2 idle timeout আর write timeout নেই (upstream issue), তাই feature-gate করে রাখা হবে |

- **Install-এর সময় জিজ্ঞেস করবে** কোনটা চাই (`--proxy caddy|angie|ferron`, না দিলে prompt, default caddy)।
- **পরে বদলানো যাবে**: panel → Settings → Proxy engine, বা `nd proxy engine set <name>`। বদলানোর আগে preflight চেক করবে, ব্যবহৃত কোনো feature নতুন engine-এ না থাকলে আটকে দেবে, rollback থাকবে।
- **nd-edge (shield)-এও একই তিনটা**: `nd edge install --proxy angie` ইত্যাদি, পরে `nd edge switch-proxy`।
- পুরো কাজটা ছোট ছোট phase/PR-এ ভাগ করা (দেখো [08-tasks.md](08-tasks.md))। প্রতিটা phase-এর acceptance criteria আছে, যাতে আলাদা agent আলাদা phase করতে পারে।

## Why (evidence)

Benchmarks were run on Docker (Kiri/WSL2) with identical TLS + HTTP/2 + reverse-proxy setups, default settings unless stated. Raw results and scripts: `tests/proxy-suite/` (moved from the original `ferron-test/` harness).

| Measurement | Caddy 2 | Angie 1.12.4 | nginx 1.30 | Ferron 3.0.0-rc.11 |
|---|---|---|---|---|
| RAM per idle HTTP/2+TLS connection (20k conns) | ~61 KB | **~40 KB** | ~40 KB | ~51 KB |
| RAM after all 20k connections closed | 1.1 GB (released only after 3–5 min) | 127 MB | 125 MB | **51 MB** |
| Idle HTTP/2 connection closed by server after | 300 s (configurable) | **75 s** | 75 s | **never** (not configurable) |
| Slow reader (client stops reading) | closed | closed | closed | **never closed** |
| Backend container recreated (new IP), no reload | **recovers in <1 s** | **recovers in <1 s** (Docker labels) | stays broken (502) | stays broken unless `logical_dns true` |
| Throughput, proxied, 100 conns × 10 streams | 3,695 req/s | 12,258 req/s (upstream keepalive) | 10,133 req/s (upstream keepalive) | **15,441 req/s** |
| 300 long streams during 20 reloads | 300/300 ok | 300/300 ok | 300/300 ok | 300/300 ok |
| Client abort mid-stream: upstream released, CLOSE-WAIT | ok, 0 | ok, 0 | ok, 0 | ok, 0 |

Production context (shield `107.189.6.222`, 1 GB RAM): Caddy reached 16k sockets and got OOM-killed. Cause: Cloudflare edges keep thousands of HTTP/2 connections, Caddy's default `idle 5m`, and Go's per-connection memory, plus GC and swap thrashing. Angie closes idle HTTP/2 at 75 s and uses ~1/3 less RAM per connection.

## Goals

1. One abstraction, three engines, for **both** the panel's app proxy and **nd-edge**.
2. **Zero behavior change for existing Caddy installs** (Caddy driver = today's code, wrapped).
3. Engine chosen at install, switchable later with preflight + rollback.
4. Every NextDeploy proxy feature mapped per engine; unsupported ones are clearly gated in UI/API.

## Non-goals (for this plan)

- No HAProxy/Traefik/nginx-vanilla drivers. Angie covers the nginx family. The driver interface allows adding them later.
- No change to the panel ↔ nd-edge API semantics. Only additive optional fields.
- No automatic engine choice. The admin chooses.

## Document map

| File | Content |
|---|---|
| [01-architecture.md](01-architecture.md) | Current Caddy coupling (file-by-file), target `internal/proxy` design, Driver interface, desired-state IR, sync/reload model |
| [02-capability-matrix.md](02-capability-matrix.md) | Every NextDeploy proxy feature → Caddy / Angie / Ferron mapping and status |
| [03-angie-driver.md](03-angie-driver.md) | Angie driver: compose, config layout, templates per feature, reload, TLS, Docker labels |
| [04-ferron-driver.md](04-ferron-driver.md) | Ferron driver: layout, templates, mandatory settings, known gaps, gating |
| [05-edge.md](05-edge.md) | nd-edge multi-engine: renderer/runner interfaces, install, switch, API additions |
| [06-install-and-switch.md](06-install-and-switch.md) | install.sh flag/prompt, settings, switch flow, cert migration, rollback |
| [07-testing.md](07-testing.md) | Golden tests, docker integration suite, acceptance tests per engine |
| [08-tasks.md](08-tasks.md) | Phased PR-sized task list with acceptance criteria (hand to agents) |

## Key decisions

| # | Decision | Reason |
|---|---|---|
| D1 | Introduce a **desired-state IR** built from DB, and drivers render from it | Caddy is label-driven, Angie/Ferron are file-driven. An IR keeps one source of truth |
| D2 | Caddy driver keeps **caddy-docker-proxy labels** (wraps existing `internal/caddy`) | No regression, no migration for current users |
| D3 | Angie uses **rendered files + Docker-label upstreams** | Labels give reload-free container IP changes. Files give sites/TLS/routing |
| D4 | Ferron uses **rendered files + `logical_dns true` + explicit `POST /reload`** | Ferron has no Docker discovery. `logical_dns` is required to survive container recreation (tested). `watch=1` is not used: it does not notice new files matched by `include "…/*.conf"` |
| D5 | Internal TLS for Angie/Ferron via a **NextDeploy-managed private CA** | Caddy's `tls internal` has no equivalent. nd-edge already trusts an origin CA PEM, so it is reused |
| D6 | Ferron is **Experimental**, behind a capability gate + warning | Missing HTTP/2 idle timeout and write timeout (verified in source), open reload bug ferronweb/ferron#996 |
| D7 | Reloads are **debounced and batched** (one reload per burst) | nginx-family reload spawns workers; frequent deploys must not stack reloads |
| D8 | Angie/nginx-family upstreams **always** use keepalive (`keepalive 64; proxy_http_version 1.1; proxy_set_header Connection "";`) | Without it throughput collapses (650 req/s with 10k errors vs 12k req/s, 0 errors) |
