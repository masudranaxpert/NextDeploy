# 07 — Testing

## 1. Unit / golden tests (no Docker)

| Package | Test |
|---|---|
| `internal/proxy/state` | DB fixtures → IR golden JSON (apps with paths, PHP with security + routing, proxy routes, slots, panel, gateways, shield/trusted variants) |
| `internal/proxy/caddy` | IR → labels must equal **today's** `internal/caddy` output for every fixture (parity gate: Phase 0 cannot merge without this) |
| `internal/proxy/angie` | IR → files golden. Every RoutingRule kind/match, every SiteSecurity flag, TLS modes, untrusted vs trusted headers |
| `internal/proxy/ferron` | IR → files golden. Assert `logical_dns true` on every hostname upstream. Assert no `location` + host-level `proxy` combo |
| `internal/proxy` | Capabilities diff / preflight: fixtures that must block or warn per engine |
| `internal/proxy/pki` | CA create, leaf issue/renew, SAN correctness, file perms 0600 |
| `cmd/nd/internal/edge` | Existing Caddy render golden tests unchanged. New Angie/Ferron golden tests from `testdata/edge_contract.json` |

Validation in CI: run `angie -t` and `ferron validate` against every golden output inside their Docker images (`docker run --rm -v …`). Mark these as `//go:build integration` if CI lacks Docker.

## 2. Integration suite (Docker)

The benchmarking harness now lives in `tests/proxy-suite/` (moved from `ferron-test/`: `probe/`, `loadclient/`, `*.sh`, `bench.sh`) and is parameterized by engine (`./run-all.sh caddy|angie|ferron`). A nightly job runs it, plus the Go unit/race/integration tests and the installer tests: `.github/workflows/proxy-suite.yml`.

| Test | Pass criteria (per engine) |
|---|---|
| `dnsrestart` (recreate backend, new IP, no reload) | ≥ 95 % 200s within 5 s, 100 % after 5 s |
| `reloadmem` (300 streams × 20 reloads) | 300/300 streams 200. RAM back within 20 % of baseline 15 s after streams end |
| `abort` (200 client aborts mid-stream) | 0 CLOSE-WAIT. Upstream conns 0 within 5 s |
| `timeouts` probe | Angie: idle-h2 ≤ configured idle + 5 s; slow reader closed. Ferron: documented gaps only (assert current values so regressions/fixes are noticed) |
| `throughput` (h2load 60k, c100 m10) | 0 failed. Angie ≥ 8k req/s on the reference box |
| `20k memory` | Angie ≤ 50 KB/conn. All 20k connections stay established |
| Feature e2e | One fixture app per feature row of 02 (static path, PHP + routing + security, redirect route, external proxy route, websocket, blue/green switch, preview host). curl assertions |

## 3. Manual acceptance before release

- Fresh install × 3 engines on a clean VM (1 vCPU / 1 GB). Deploy a sample app, PHP site, and proxy route.
- Switch caddy → angie → caddy on a VM with 20 apps. Zero failed requests outside the cutover window. Certs not re-issued (imported).
- nd-edge: install with `--proxy angie` against a staging panel. Switch on a shield carrying real Cloudflare traffic for 48 h. Watch RAM, `ss -s`, and Cloudflare 520 rate.
