#!/usr/bin/env bash
export MSYS_NO_PATHCONV=1
set -euo pipefail

SUITE_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

CADDY_IMAGE="${CADDY_IMAGE:-lucaslorentz/caddy-docker-proxy:ci-alpine}"
ANGIE_IMAGE="${ANGIE_IMAGE:-docker.angie.software/angie:1.12.4}"
FERRON_IMAGE="${FERRON_IMAGE:-ferronserver/ferron:3.0.0-rc.11-alpine}"
GO_IMAGE="${GO_IMAGE:-golang:1.26-alpine}"
NET="${PROXY_SUITE_NET:-proxy-suite}"

resolve_engine() {
  local eng="${1:-${ENGINE:-}}"
  if [ -z "$eng" ]; then
    echo "error: ENGINE is required (usage: <script> caddy|angie|ferron or ENGINE=<name>)" >&2
    exit 1
  fi
  case "$eng" in
    caddy|angie|ferron) echo "$eng" ;;
    *)
      echo "error: unsupported ENGINE '$eng' (expected caddy|angie|ferron)" >&2
      exit 1
      ;;
  esac
}

engine_base_image() {
  case "$1" in
    caddy)  echo "$CADDY_IMAGE" ;;
    angie)  echo "$ANGIE_IMAGE" ;;
    ferron) echo "$FERRON_IMAGE" ;;
  esac
}

ensure_network() {
  docker network inspect "$NET" >/dev/null 2>&1 || docker network create "$NET" >/dev/null
}

ensure_image() {
  local tag="$1"
  local dockerfile="$2"
  local context="$3"
  shift 3
  if [ "${REBUILD:-0}" = "1" ] || ! docker image inspect "$tag" >/dev/null 2>&1; then
    (cd "$SUITE_DIR" && docker build -q -t "$tag" -f "$dockerfile" "$@" "$context" >/dev/null)
  fi
}

ensure_engine_images() {
  local eng="$1"
  local base
  base="$(engine_base_image "$eng")"
  ensure_image "proxy-suite-${eng}:latest" "engines/${eng}/Dockerfile" "engines" --build-arg "BASE_IMAGE=${base}"
}

ensure_bench_images() {
  local eng="$1"
  local base
  base="$(engine_base_image "$eng")"
  ensure_image "proxy-suite-bench-${eng}:latest" "engines/${eng}/bench.Dockerfile" "engines" --build-arg "BASE_IMAGE=${base}"
  ensure_image "proxy-suite-loadclient:latest" "loadclient/Dockerfile" "loadclient" --build-arg "GO_IMAGE=${GO_IMAGE}"
}

ensure_probe_image() {
  ensure_image "proxy-suite-probe:latest" "probe/Dockerfile" "probe" --build-arg "GO_IMAGE=${GO_IMAGE}"
}

wait_tcp_8443() {
  local cname="$1"
  local i=0
  while [ "$i" -lt 40 ]; do
    if docker run --rm --network "container:$cname" alpine:3.20 sh -c \
      "grep -qi ':20FB ' /proc/net/tcp /proc/net/tcp6 2>/dev/null" >/dev/null 2>&1; then
      return 0
    fi
    i=$((i + 1))
    sleep 0.25
  done
  echo "error: $cname did not listen on :8443 in time" >&2
  docker logs "$cname" 2>&1 | tail -n 20 >&2 || true
  return 1
}

start_app() {
  ensure_network
  docker rm -f app filler >/dev/null 2>&1 || true
  docker run -d --name app --network "$NET" \
    --label "angie.http.upstreams.app.port=8080" \
    --label "angie.network=${NET}" \
    ghcr.io/mccutchen/go-httpbin -max-duration 600s >/dev/null
  sleep 1
}

start_srv() {
  local eng="$1"
  ensure_network
  ensure_engine_images "$eng"
  local extra=()
  if [ "$eng" = "angie" ]; then
    extra=(-v "/var/run/docker.sock:/var/run/docker.sock")
  fi
  docker rm -f "s-$eng" >/dev/null 2>&1 || true
  docker run -d --name "s-$eng" --network "$NET" \
    "${extra[@]}" \
    --ulimit nofile=1048576:1048576 \
    "proxy-suite-${eng}:latest" >/dev/null
  wait_tcp_8443 "s-$eng"
}

mem() {
  local cname="$1"
  docker stats --no-stream --format '{{.MemUsage}}' "$cname" | cut -d/ -f1 | tr -d ' '
}

nproc_srv() {
  local cname="$1"
  docker top "$cname" 2>/dev/null | tail -n +2 | wc -l | tr -d ' '
}

# sock <container> <state_hex> <local(2)/remote(3)> <port_hex>
sock() {
  local cname="$1" state="$2" field="$3" port_hex="$4"
  docker run --rm --network "container:$cname" alpine:3.20 sh -c \
    "cat /proc/net/tcp /proc/net/tcp6 2>/dev/null | awk '\$4==\"${state}\" && \$${field} ~ /:${port_hex}\$/' | wc -l" | tr -d ' '
}

ferron_reload_gen() {
  local cname="$1"
  local json
  json="$(docker exec "$cname" wget -q -O- http://127.0.0.1:8081/reload)"
  printf '%s' "$json" | sed -n 's/.*"active_generation":\([0-9]*\).*/\1/p'
}

reload_srv() {
  local eng="$1"
  local cname="s-$eng"
  case "$eng" in
    caddy)
      docker exec "$cname" caddy reload --config /etc/caddy/Caddyfile --force >/dev/null
      ;;
    angie)
      docker exec "$cname" angie -t >/dev/null 2>&1
      docker exec "$cname" angie -s reload >/dev/null
      ;;
    ferron)
      local prev post status gen err i=0
      prev="$(ferron_reload_gen "$cname")"
      prev="${prev:-0}"
      post="$(docker exec "$cname" wget -q -O- --post-data= http://127.0.0.1:8081/reload)"
      case "$post" in
        *'"status":"reload_initiated"'*) ;;
        *)
          echo "error: ferron POST /reload failed: $post" >&2
          return 1
          ;;
      esac
      while [ "$i" -lt 20 ]; do
        status="$(docker exec "$cname" wget -q -O- http://127.0.0.1:8081/reload)"
        gen="$(printf '%s' "$status" | sed -n 's/.*"active_generation":\([0-9]*\).*/\1/p')"
        if [ -n "$gen" ] && [ "$gen" -gt "$prev" ]; then
          case "$status" in
            *'"last_reload_error":null'*) return 0 ;;
            *)
              echo "error: ferron reload reported error: $status" >&2
              return 1
              ;;
          esac
        fi
        i=$((i + 1))
        sleep 0.25
      done
      echo "error: ferron active_generation did not advance from $prev ($status)" >&2
      return 1
      ;;
  esac
}
