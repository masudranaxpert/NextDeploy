#!/usr/bin/env bash
export MSYS_NO_PATHCONV=1
set -euo pipefail

SUITE_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./lib.sh
source "$SUITE_DIR/lib.sh"

ENGINE="$(resolve_engine "${1:-}")"
RELOADS="${RELOADS:-20}"
STREAMS="${STREAMS:-300}"
CLIENT="rm-${ENGINE}"
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
  for i in \$(seq 1 ${STREAMS}); do
    (curl -sk --http2 -m 200 -o /dev/null -w '%{http_code} %{size_download}\n' --resolve localhost:8443:\$ip 'https://localhost:8443/drip?duration=120&numbytes=120') &
  done
  wait
" >/dev/null

sleep 10
echo "phase,mem,procs"
echo "streams-running,$(mem "$SRV"),$(nproc_srv "$SRV")"
for _ in $(seq 1 "$RELOADS"); do
  reload_srv "$ENGINE"
  sleep 3
done
echo "after-${RELOADS}-reloads,$(mem "$SRV"),$(nproc_srv "$SRV")"
docker wait "$CLIENT" >/dev/null
echo "streams-finished,$(mem "$SRV"),$(nproc_srv "$SRV")"
sleep 15
echo "15s-later,$(mem "$SRV"),$(nproc_srv "$SRV")"
echo "--- stream results (code bytes):"
docker logs "$CLIENT" 2>&1 | sort | uniq -c
