# 08 — Phased tasks (hand to agents)

Each task is sized for one PR. Do them in order inside a phase. Phases 1 and 2 can run in parallel once Phase 0 merges. Every PR: `go test ./...`, the golden tests listed, no change to Caddy output unless the task says so.

## Phase 0 — Abstraction with zero behavior change (blocking)

| # | Task | Files | Acceptance |
|---|---|---|---|
| 0.1 | Create `internal/proxy` with `Driver`, `Capabilities`, IR types (01 §2.2–2.4), registry `proxy.Get(name)` | `internal/proxy/{driver,ir}.go` | compiles. godoc on every exported type |
| 0.2 | `state.Build` from DB → IR (pure function, inputs passed in) | `internal/proxy/state/build.go` + fixtures | golden IR JSON for ≥ 12 fixtures (see 07 §1) |
| 0.3 | Caddy driver wrapping `internal/caddy` (labels via existing functions) | `internal/proxy/caddy/` | **parity test**: IR→labels equals current label output byte-for-byte for all fixtures |
| 0.4 | Move validation helpers to `internal/proxy/validate.go`; keep aliases in `internal/caddy` | | all existing tests pass |
| 0.5 | `proxy.Syncer` (debounce 300 ms, generation hash, apply log table `proxy_apply`) | `internal/proxy/sync.go`, `internal/db` migration | unit tests for coalescing + skip-unchanged |
| 0.6 | Re-point handlers / agent / mcp / phphosting callers to `p.Proxy` (`Sync`, `ContainerName`, `Status`, `Logs`, `Restart`) | `internal/handlers/*`, `internal/{agent,mcp,phphosting,workspace,htaccess}` | no direct `internal/caddy` imports outside `internal/proxy/caddy` except validation aliases. Manual: deploy app, PHP site, route, slot switch on Caddy behave as before |
| 0.7 | Settings `proxy_engine` (seed `caddy`), `/api/v1/proxy/*` canonical endpoints + aliases | handlers/settings, router | API tests |

## Phase 1 — Angie driver for the panel

| # | Task | Acceptance |
|---|---|---|
| 1.1 | `internal/proxy/pki`: ND private CA + leaf issue/renew + `ReadOriginRootCA` generalization | unit tests. Edge `/edge/routes` returns the ND CA when engine≠caddy |
| 1.2 | Angie `StackService` + main/global templates (03 §1–2) | `angie -t` passes on rendered output in Docker |
| 1.3 | Angie `AppLabels` (upstream labels) + compose generator hook (`GenerateMergedCompose`/slot variant accept a labeler) | dnsrestart test passes for Angie |
| 1.4 | App proxy site template incl. path routes, headers, untrusted header stripping, websocket, TLS modes | golden + e2e fixtures |
| 1.5 | PHP/static template incl. SiteSecurity + RoutingRule translation + fallback modes. **Verify bcrypt in Alpine image** (else switch the image) | golden + e2e PHP fixture (WordPress-like and Laravel-like) |
| 1.6 | Redirect + external proxy routes (remove `nd-route-*` containers for non-Caddy) | e2e |
| 1.7 | Gateway ports + shared mounts in StackService | e2e |
| 1.8 | Apply path: stage → `angie -t` → swap → `angie -s reload` → health → rollback | integration: broken config never goes live. reloadmem test passes |
| 1.9 | Status/overview/logs via Angie API + container logs. Prometheus | UI shows engine, version, connections |
| 1.10 | ACME: render `acme_client` + per-site `acme` (confirm syntax against Angie docs 1.12) | real cert issued on a staging domain (LE staging) |

## Phase 2 — nd-edge engines

| # | Task | Acceptance |
|---|---|---|
| 2.1 | `Engine`/`Runner` interfaces. Move the Caddy renderer behind `CaddyEngine` | existing edge golden tests unchanged |
| 2.2 | `AngieEngine` render (global, common, panel, user include dir) | golden from `testdata/edge_contract.json`. `angie -t` |
| 2.3 | `nd edge install --proxy` (package install for Angie, port ownership, marker files) | install on Debian 12 + Ubuntu 22.04/24.04 VMs |
| 2.4 | `nd edge switch-proxy` with cert import from Caddy + rollback | switch/rollback on a VM with ≥ 10 hosts. No ACME re-issue |
| 2.5 | Status API additive fields (`proxy_engine`, `proxy_version`, `proxy_running`) + panel shield UI badge | old panels still parse status (compat test) |

## Phase 3 — Install & switch UX

| # | Task | Acceptance |
|---|---|---|
| 3.1 | `install.sh --proxy` + TTY prompt + `ND_PROXY_ENGINE` seed | fresh installs × 3 engines |
| 3.2 | Upgrade path keeps Caddy installs untouched | upgrade an existing install: zero diff in compose except comments |
| 3.3 | Preflight (capability diff + dry-run validate) API + UI | blocks on hard gaps with per-site list |
| 3.4 | Switch flow (06 §3.2) + automatic rollback + `proxy_last_switch` | caddy→angie→caddy on a 20-app VM, certs imported |
| 3.5 | UI rename Caddy → Proxy, engine badge, per-engine option forms (disable unsupported fields) | screenshots in PR |
| 3.6 | `nd proxy engine get|set|preflight` CLI | CLI tests |

## Phase 4 — Ferron (Experimental)

| # | Task | Acceptance |
|---|---|---|
| 4.1 | Ferron StackService + templates (04), `logical_dns` enforced, `match/if` path routing | golden + `ferron validate` |
| 4.2 | Reload via admin `POST /reload` + `GET /reload` generation check (`watch=1` is not used: verified it misses new included files) | integration |
| 4.3 | Capability gaps wired into preflight + UI warnings + `proxy_experimental_ack` | Ferron cannot be selected without ack |
| 4.4 | nd-edge `FerronEngine` behind `--experimental` | edge golden |
| 4.5 | Track upstream: HTTP/2 idle timeout, write timeout, #996. Remove gates when fixed | — |

## Phase 5 — Docs & hardening

- User docs: engine comparison (README evidence table), when to choose which, the switch guide, and the shield guide (Angie recommended for 1 GB shields).
- Move `ferron-test/` harness to `tests/proxy-suite/` (07 §2) and add a CI job (nightly): done, see `.github/workflows/proxy-suite.yml`.
- Default recommendation in the shield setup script: Angie.

## Open questions: resolved

1. **Angie ACME directive and variable names (1.12):** resolved. `acme_client <name> <uri> [email=…] [account_key=…];` plus `acme <name>;` with `$acme_cert_<name>` / `$acme_cert_key_<name>`. One client per site (a client issues one certificate for all referencing `server_name`s), names `[A-Za-z0-9_]` only. Live issuance against Let's Encrypt staging is still `[UNVERIFIED]`.
2. **Bcrypt in `auth_basic_user_file` on Alpine:** resolved, supported (`$2y$`, `$2b$`, `$2a$`). No Debian image needed.
3. **Ferron `watch=1` for included files:** resolved, not reliable. New files matched by `include` are not detected. Use `POST /reload` and verify `GET /reload`.
4. **Container name:** resolved. Caddy keeps `caddy`; Angie and Ferron use `nd-proxy` (`Driver.ContainerName()`).
5. **Gateway ports:** resolved. Ports carry HTTP only for Angie/Ferron. `Direct` and UDP bindings stay published by the app container. Ferron `tcp *:port` blocks pass `ferron validate` but never listen, so they are not used; Angie raw TCP would need `stream {}` and is not implemented.
