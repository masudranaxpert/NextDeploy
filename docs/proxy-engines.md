# Reverse proxy engines: Caddy, Angie, Ferron

NextDeploy can run its panel and app proxy on one of three engines. Caddy is the default and is unchanged for existing installs. Angie and Ferron are opt-in.

| Engine | Image / package | Status |
|---|---|---|
| **Caddy 2** | `lucaslorentz/caddy-docker-proxy:ci-alpine`, container `caddy` | Default, stable |
| **Angie 1.12.4** | `docker.angie.software/angie:1.12.4`, container `nd-proxy` | Supported |
| **Ferron 3.0.0-rc.11** | `ferronserver/ferron:3.0.0-rc.11-alpine`, container `nd-proxy` | **Experimental** |

## Evidence

All numbers come from the same TLS + HTTP/2 + reverse-proxy setup run in Docker, default settings unless stated. Scripts live in [`tests/proxy-suite/`](../tests/proxy-suite/README.md); raw results and the verification notes are in [`docs/spikes/2026-11-18-multi-proxy-engines.md`](spikes/2026-11-18-multi-proxy-engines.md).

| Measurement | Caddy 2 | Angie 1.12.4 | Ferron 3.0.0-rc.11 |
|---|---|---|---|
| RAM per idle HTTP/2+TLS connection (20k connections) | ~61 KB | ~40 KB | ~51 KB |
| RAM after all 20k connections closed | 1.1 GB (released after 3 to 5 min) | 127 MB | 51 MB |
| Idle HTTP/2 connection closed by the server after | 300 s (configurable) | 75 s (the NextDeploy driver renders `keepalive_timeout 75s`, or `20s` when real-IP mode is Cloudflare) | never, not configurable |
| Client stops reading a response | closed | closed | never closed |
| Backend container recreated with a new IP, no reload | recovers in under 1 s | recovers in under 1 s via Docker labels (2 transient 502s in the first 5 s in the `dnsrestart` run, 0 after) | stays broken unless `logical_dns true` (the driver always sets it) |
| Throughput, proxied, 100 connections x 10 streams | 3,695 req/s | 12,258 req/s (with upstream `keepalive 64`) | 15,441 req/s |
| 300 long streams across 20 reloads | 300/300 ok | 300/300 ok | 300/300 ok |
| Client abort mid-stream | upstream released, 0 CLOSE-WAIT | same | same |

Production context: a 1 GB shield running Caddy reached 16k sockets and was OOM-killed. Cloudflare keeps thousands of HTTP/2 connections open, and Caddy's default idle timeout is 5 minutes.

### Not verified

- Live Let's Encrypt issuance (Angie `http_acme` behind a `return 308` on port 80, Ferron `on_demand_ask`) was not run against a public staging domain. Directive syntax is verified with `angie -t` and `ferron validate`.
- Package installs for `nd edge install --proxy angie` on Debian 12, Ubuntu 22.04/24.04 and Alpine were verified through repository `Release`/`APKINDEX` requests and the official docs, not a full VM install.

## When to choose which

| Choose | When |
|---|---|
| **Caddy** | You want the simplest setup, you already run NextDeploy, or you use every feature (on-demand TLS, `require_target_file` routing rules, strict right-to-left trusted-proxy parsing). Nothing changes on upgrade. |
| **Angie** | Small VPS or Edge Shield nodes, many idle HTTP/2 connections (Cloudflare in front), high throughput. It closes idle HTTP/2 itself, uses less memory per connection, picks up container IP changes without a reload, and supports every feature the panel uses except on-demand TLS. |
| **Ferron** | You want to try the fastest engine and accept the gaps below. Not recommended behind Cloudflare or on untrusted networks. |

### Ferron gaps (why it is experimental)

Verified in the Ferron 3.0.0-rc.11 source and by tests:

- HTTP/2 idle connections are never closed (no `idle_timeout` is set on the HTTP/2 options).
- No write timeout for slow readers and no request-body idle timeout. HTTP/1 header read and keep-alive are fixed at 30 s.
- Upstream issue [ferronweb/ferron#996](https://github.com/ferronweb/ferron/issues/996) (reload busy-loop) was still open at the time of the spike.
- `trusted_strict` is not supported: Ferron reads the leftmost `X-Forwarded-For` address and rejects `CF-Connecting-IP` as a header source.
- `require_target_file` routing rules cannot be expressed. The driver omits them with a `# IGNORED` comment, and the switch preflight blocks on them.
- The WordPress disk-cache fast path (`@wp_cache`) is not available. PHP still serves the page.
- `tcp *:port` blocks pass `ferron validate` but never listen. Only HTTP gateway ports work.
- Ferron cannot be selected without the experimental acknowledgement.

## Install

```bash
# Interactive prompt on a TTY (default 1 = Caddy), or pick explicitly:
sudo bash install.sh --proxy caddy
sudo bash install.sh --proxy angie
sudo bash install.sh --proxy ferron     # prints an experimental warning
```

Without a TTY and without `--proxy`, the installer uses Caddy. Re-running the installer or `nextdeploy-update` keeps the engine already in `docker-compose.yml`.

Edge Shield nodes choose their engine independently of the panel:

```bash
nd edge install --panel-url https://panel.example.com --token-stdin --proxy angie
nd edge install ... --proxy ferron --experimental    # --experimental is required
```

## Switch engines

1. **Panel:** Settings, Proxy engine. Pick the target, review the preflight, confirm.
2. **CLI:**
   ```bash
   nd proxy engine get
   nd proxy engine preflight angie
   nd proxy engine set angie --yes
   nd proxy engine set ferron --yes --experimental
   ```
3. **Shield node:** `nd edge switch-proxy <caddy|angie|ferron> [--experimental] [--yes]`.

The preflight builds the desired state, compares it with the target's capabilities, and lists affected sites. **Hard blocks** (for example `require_target_file` rules or on-demand TLS on Angie) stop the switch. **Soft warnings** (for example Ferron timeout gaps) are shown and must be accepted. A dry-run validates the rendered target config in a throwaway container before anything changes.

The switch pulls the image, renders the target config, imports Caddy-issued certificates as manual certificates (so Let's Encrypt is not hit again), regenerates app compose labels for the target, removes the `nd-route-*` label containers when leaving Caddy, then cuts over. Expect 2 to 5 s of downtime.

### Rollback and data

- A failed cutover or health probe rolls back automatically: the previous root compose service and app labels are restored from `*.prev`, and `proxy_last_switch` records `rolled_back` with the error.
- The `caddy_data` volume is never deleted automatically. Keep it for at least 30 days after leaving Caddy.
- Rendered file configs for Angie and Ferron are staged as generations (`sites.gen-<hash>`) and activated by swapping one relative symlink. A config that fails validation never goes live, and a failed reload restores the previous generation.

## Shield guide: use Angie on 1 GB nodes

For Cloudflare-fronted shields with 1 GB of RAM, Angie is the recommended engine:

- Set the real-IP mode to Cloudflare in the proxy options. The driver then renders `keepalive_timeout 20s` (otherwise `75s`), so idle HTTP/2 connections do not pile up. An explicit idle value overrides both.
- Upstreams always use `keepalive 64` with `proxy_http_version 1.1` and an empty `Connection` header. Without it, throughput collapsed to 650 req/s with errors in the benchmark.
- `worker_shutdown_timeout` bounds how long old workers drain streams after a reload.
- Hand-written rules (bot filters, rate limits) go in `/etc/angie/nd-edge/user/*.conf`, which is outside the managed region.

Roll out on a test shield first, point one low-traffic domain at it, and watch RAM, Cloudflare 520 rate and `ss -s` for 48 hours before switching a production shield.

## More detail

- Design and decisions: [`docs/adr/001-multi-proxy-drivers.md`](adr/001-multi-proxy-drivers.md)
- Per-feature mapping: [`docs/multi-proxy/02-capability-matrix.md`](multi-proxy/02-capability-matrix.md)
- Spike reports: [`docs/spikes/`](spikes/)
