#!/usr/bin/env bash
export MSYS_NO_PATHCONV=1
set -euo pipefail

SUITE_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./lib.sh
source "$SUITE_DIR/lib.sh"

ENGINE="$(resolve_engine "${1:-}")"
CLIENT="dn-${ENGINE}"
SRV="s-${ENGINE}"

cleanup() {
  docker rm -f "$CLIENT" filler app "$SRV" >/dev/null 2>&1 || true
}
trap cleanup EXIT

ip_of() {
  docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' "$1"
}

start_app
start_srv "$ENGINE"
old_ip="$(ip_of app)"

docker rm -f "$CLIENT" >/dev/null 2>&1 || true
docker run -d --name "$CLIENT" --network "$NET" --entrypoint sh curlimages/curl -c "
  sip=\$(getent hosts ${SRV} | cut -d' ' -f1)
  for i in \$(seq 1 150); do
    echo \"\$(date +%s) \$(curl -sk -o /dev/null -w '%{http_code}' -m 3 --resolve localhost:8443:\$sip https://localhost:8443/get)\"
    sleep 0.2
  done
" >/dev/null

sleep 6
T0="$(date +%s)"
docker rm -f app >/dev/null
docker run -d --name filler --network "$NET" alpine:3.20 sleep 600 >/dev/null
docker run -d --name app --network "$NET" \
  --label "angie.http.upstreams.app.port=8080" \
  --label "angie.network=${NET}" \
  ghcr.io/mccutchen/go-httpbin -max-duration 600s >/dev/null

echo "engine=$ENGINE app IP $old_ip -> $(ip_of app)"
docker wait "$CLIENT" >/dev/null
docker logs "$CLIENT" | awk -v t0="$T0" '
  {
    s = $1 - t0
    b = (s < 0) ? "1-before" : ((s < 5) ? "2-0..5s" : ((s < 15) ? "3-5..15s" : "4-15s+"))
    k[b " " $2]++
  }
  END { for (x in k) print x, k[x] }
' | sort
