#!/usr/bin/env bash
export MSYS_NO_PATHCONV=1
set -euo pipefail

SUITE_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./lib.sh
source "$SUITE_DIR/lib.sh"

ENGINE="$(resolve_engine "${1:-}")"
H2LOAD_N="${H2LOAD_N:-60000}"
H2LOAD_C="${H2LOAD_C:-100}"
H2LOAD_M="${H2LOAD_M:-10}"
SRV="s-${ENGINE}"

cleanup() {
  docker rm -f app "$SRV" >/dev/null 2>&1 || true
}
trap cleanup EXIT

start_app
start_srv "$ENGINE"

docker run --rm --network "container:${SRV}" alpine:3.20 sh -c "
  apk add -q nghttp2 >/dev/null 2>&1
  h2load -n ${H2LOAD_N} -c ${H2LOAD_C} -m ${H2LOAD_M} https://localhost:8443/get 2>&1 | grep -E 'finished in|requests:|time for request:'
"
echo "mem after: $(mem "$SRV")"
