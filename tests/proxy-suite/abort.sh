#!/usr/bin/env bash
export MSYS_NO_PATHCONV=1
set -euo pipefail

SUITE_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./lib.sh
source "$SUITE_DIR/lib.sh"

ENGINE="$(resolve_engine "${1:-}")"
CONNS="${ABORT_CONNS:-200}"
CLIENT="ab-${ENGINE}"
SRV="s-${ENGINE}"

cleanup() {
  docker rm -f "$CLIENT" app "$SRV" >/dev/null 2>&1 || true
}
trap cleanup EXIT

start_app
start_srv "$ENGINE"

docker rm -f "$CLIENT" >/dev/null 2>&1 || true
docker run -d --name "$CLIENT" --network "$NET" --entrypoint sh curlimages/curl -c "
  ip=\$(getent hosts ${SRV} | cut -d' ' -f1)
  for i in \$(seq 1 ${CONNS}); do
    curl -sk --http2 -m 3 -o /dev/null --resolve localhost:8443:\$ip 'https://localhost:8443/drip?duration=60&numbytes=60' &
  done
  wait
" >/dev/null

echo "t,client_close_wait(:8443),upstream_estab(->app:8080),mem"
prev=0
for t in 2 5 10 20 30 45 60 75; do
  sleep $((t - prev))
  prev="$t"
  echo "${t}s,$(sock "$SRV" 08 2 20FB),$(sock "$SRV" 01 3 1F90),$(mem "$SRV")"
done
