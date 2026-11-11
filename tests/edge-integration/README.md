# `nd edge` Multi-Panel Shield Integration Tests

This directory contains two complementary test suites for NextDeploy's multi-panel Shield architecture (`docs/plans/nd-edge-plan.md` §8.3):

1. **In-Process End-to-End Simulation (`sim_test.go`)** — no Docker required; runs anywhere (Windows, macOS, Linux) via standard `go test ./...`.
   - Wires two real `internal/handlers.Panel` instances backed by independent SQLite databases (`panelbridge/`) directly into real `nd/internal/edge` components (`PanelClient`, `PollPanelOnce`, `Reconciler`, `ClaimsStore`, `DNSGate` with a fake `DNSResolver`, `Applier` with a stateful validating `CaddyRunner`).
   - Covers: 2 panels -> 1 Shield (hybrid Direct + Shielded domains), `409 token_in_use` and cross-panel domain conflict reporting, domain move A -> B, panel removal leaving Panel A's Caddyfile untouched, mass-deletion guard, origin CA rotation, `E-ORIGIN-6` `pending_origin_apply`, and offline disk reboot recovery.

2. **Docker Compose Pebble Suite (`docker-compose.yml` + `integration_test.go`)** — exercises real Caddy 2.8+, `nd edge run`, Pebble ACME, `pebble-challtestsrv`, two `fakepanel` HTTP servers, and two `origin` echo/WebSocket servers behind `tls internal`.

## Running the Simulation Suite (No Docker)

```bash
cd tests/edge-integration
go test -v -count=1 ./...
```

## Compile-Checking the Integration Tag (No Docker)

```bash
cd tests/edge-integration
go test -tags=integration -c .
```

## Running the Full Docker Compose + Pebble Suite (Linux / CI / Docker Host)

```bash
cd tests/edge-integration

# 1. Start the full Pebble + Caddy + nd-edge + fakepanel + origin stack
docker compose up -d --build

# 2. Run all §8.3 integration scenarios against the live compose stack
ND_EDGE_COMPOSE_TEST=1 go test -v -tags=integration -count=1 -timeout 10m ./...

# 3. Inspect logs on failure
docker compose logs --tail=200 caddy nd-edge pebble panel-a panel-b

# 4. Tear down stack and volumes
docker compose down -v
```

### Exposed Host Ports (`docker-compose.yml`)

| Service | Host Port | Purpose |
|---|---|---|
| `caddy` (Shield) | `10080` / `10443` / `12019` | HTTP / HTTPS / Caddy Admin API |
| `pebble` | `14000` / `15000` | ACME directory / Pebble management API (`/roots/0`) |
| `pebble-challtestsrv` | `18055` | Fake DNS management API (`/add-a`, `/clear-a`) |
| `panel-a` / `panel-b` | `18081` / `18082` | Fake panel Edge API + `/_control/*` fixtures |
| `origin-a-caddy` / `origin-b-caddy` | `12021` / `12022` | Origin Caddy Admin API (for CA rotation tests) |
