# Proxy Integration & Benchmark Suite (`tests/proxy-suite`)

Parameterized Docker test harness for comparing and validating NextDeploy proxy engines (**Caddy**, **Angie**, **Ferron**) per [`docs/multi-proxy/07-testing.md`](../../docs/multi-proxy/07-testing.md).

## Default Engine Images

| Engine | Default `BASE_IMAGE` | Override Env Var | Notes |
|---|---|---|---|
| `caddy` | `lucaslorentz/caddy-docker-proxy:ci-alpine` | `CADDY_IMAGE` | Runs `caddy run --config /etc/caddy/Caddyfile --adapter caddyfile` |
| `angie` | `docker.angie.software/angie:1.12.4` | `ANGIE_IMAGE` | Configured with `keepalive 64`, `docker_max_object_size 512k`, `worker_shutdown_timeout 10m`, `status_zone` |
| `ferron` | `ferronserver/ferron:3.0.0-rc.11-alpine` | `FERRON_IMAGE` | Configured with `logical_dns true`; explicit `POST /reload` + `GET /reload` verification (`watch=1` omitted) |

All scripts export `MSYS_NO_PATHCONV=1` so container paths (`/proc/...`, `/etc/...`, `/var/run/docker.sock`) work under Windows Git Bash as well as Linux/macOS.

## Quick Start

From `tests/proxy-suite/`:

```bash
# Build probe, loadclient, and engine images (caddy, angie, ferron, or all)
./build.sh all
./build.sh angie
```

### Run Individual Suites per Engine

Replace `<engine>` with `caddy`, `angie`, or `ferron` (or pass via `ENGINE=<engine>`):

```bash
# 1. Backend container recreate with new IP (no proxy reload)
./dnsrestart.sh angie

# 2. 300 concurrent long-lived HTTP/2 streams across 20 reloads
./reloadmem.sh angie

# 3. 200 mid-stream client aborts (checks CLOSE_WAIT and upstream release)
./abort.sh angie

# 4. Connection timeout probe (idle-h1, idle-h2, silent, slowheader, slowbody, slowread)
./timeouts.sh angie

# 5. Proxied HTTP/2 throughput (h2load -n 60000 -c 100 -m 10)
./throughput.sh angie

# 6. 20k concurrent HTTP/2+TLS idle connection memory benchmark
./bench.sh angie 20000 90
```

### Run the Full Suite for an Engine

```bash
./run-all.sh caddy
./run-all.sh angie
./run-all.sh ferron
```

### Environment Overrides

- `CADDY_IMAGE`, `ANGIE_IMAGE`, `FERRON_IMAGE`, `GO_IMAGE`: override container base images.
- `PROXY_SUITE_NET`: Docker bridge network name (default: `proxy-suite`).
- `REBUILD=1`: force Docker image rebuild before running a test.
- `RELOADS=20`, `STREAMS=300`: tune `reloadmem.sh`.
- `ABORT_CONNS=200`: tune `abort.sh`.
- `PROBE_N=2`, `PROBE_MAX=420s`, `PROBE_MODES=...`: tune `timeouts.sh`.
- `H2LOAD_N=60000`, `H2LOAD_C=100`, `H2LOAD_M=10`: tune `throughput.sh`.
- `BENCH_CONNS=20000`, `BENCH_HOLD=90`: tune `bench.sh`.

## Pass Criteria (`docs/multi-proxy/07-testing.md`)

- **`dnsrestart`:** `>= 95%` 200s within `5s`, `100%` after `5s`.
- **`reloadmem`:** `300/300` streams return `200`, memory returns within `20%` of baseline `15s` after streams finish.
- **`abort`:** `0` `CLOSE_WAIT` client sockets, `0` upstream connections within `5s`.
- **`timeouts`:** Angie closes `idle-h2` within configured idle + `5s` and closes `slowread`; Ferron documents upstream gaps (`idle-h2` / `slowread` > `7m0s`).
- **`throughput`:** `0` failed requests; Angie `>= 8k req/s` on reference hardware.
- **`20k` memory:** Angie `<= 50 KB/conn`, all `20k` connections stay established.
