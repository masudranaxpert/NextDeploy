#!/usr/bin/env bash
export MSYS_NO_PATHCONV=1
set -euo pipefail

SUITE_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./lib.sh
source "$SUITE_DIR/lib.sh"

ENGINE="$(resolve_engine "${1:-}")"
PROBE_N="${PROBE_N:-2}"
PROBE_MAX="${PROBE_MAX:-420s}"
PROBE_MODES="${PROBE_MODES:-idle-h1,idle-h2,silent,slowheader,slowbody,slowread}"
SRV="s-${ENGINE}"
PROBE="p-${ENGINE}"

cleanup() {
  docker rm -f "$PROBE" app "$SRV" >/dev/null 2>&1 || true
}
trap cleanup EXIT

ensure_probe_image
start_app
start_srv "$ENGINE"

docker rm -f "$PROBE" >/dev/null 2>&1 || true
docker run --name "$PROBE" --network "$NET" proxy-suite-probe:latest \
  -target "${SRV}:8443" \
  -sni localhost \
  -modes "$PROBE_MODES" \
  -n "$PROBE_N" \
  -max "$PROBE_MAX"
