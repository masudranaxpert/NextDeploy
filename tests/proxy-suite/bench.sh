#!/usr/bin/env bash
export MSYS_NO_PATHCONV=1
set -euo pipefail

SUITE_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./lib.sh
source "$SUITE_DIR/lib.sh"

ENGINE="$(resolve_engine "${1:-}")"
CONNS="${2:-${BENCH_CONNS:-20000}}"
HOLD="${3:-${BENCH_HOLD:-90}}"
SRV="bench-${ENGINE}-srv"
CLI="bench-${ENGINE}-cli"

cleanup() {
  docker rm -f "$CLI" "$SRV" >/dev/null 2>&1 || true
}
trap cleanup EXIT

ensure_network
ensure_bench_images "$ENGINE"

docker rm -f "$SRV" "$CLI" >/dev/null 2>&1 || true
docker run -d --name "$SRV" --network "$NET" --network-alias localhost-srv \
  --ulimit nofile=1048576:1048576 \
  "proxy-suite-bench-${ENGINE}:latest" >/dev/null
wait_tcp_8443 "$SRV"

print_mem() {
  local phase="$1" ms conns
  ms="$(mem "$SRV")"
  conns="$(docker run --rm --network="container:$SRV" alpine:3.20 sh -c \
    "cat /proc/net/tcp /proc/net/tcp6 2>/dev/null | awk '\$4==\"01\" && \$2 ~ /:20FB\$/' | wc -l; awk '/^TCP:/ {found=1; print \$NF*4/1024} END {if (!found) print 0}' /proc/net/sockstat 2>/dev/null" \
    2>/dev/null | tr '\n' ',')"
  echo "${phase},${ENGINE},${conns}${ms}"
}

echo "phase,engine,established,kernel_tcp_mem_mb(all),cgroup_mem"
print_mem idle

docker run -d --name "$CLI" --network "$NET" \
  --ulimit nofile=1048576:1048576 \
  --sysctl net.ipv4.ip_local_port_range="1024 65000" \
  proxy-suite-loadclient:latest \
  -target "${SRV}:8443" -host localhost -n "$CONNS" -rate 1000 -every 20s -proto h2 >/dev/null

t=0
while [ "$t" -lt "$HOLD" ]; do
  sleep 10
  t=$((t + 10))
  print_mem "t+${t}s"
done

docker logs --tail 14 "$CLI"
docker rm -f "$CLI" >/dev/null
sleep 10
print_mem after-close
