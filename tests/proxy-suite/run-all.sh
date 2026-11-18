#!/usr/bin/env bash
export MSYS_NO_PATHCONV=1
set -euo pipefail

SUITE_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./lib.sh
source "$SUITE_DIR/lib.sh"

ENGINE="$(resolve_engine "${1:-}")"

"$SUITE_DIR/build.sh" "$ENGINE"

for test_name in timeouts abort reloadmem dnsrestart throughput; do
  echo "########## ${test_name} / ${ENGINE}"
  "$SUITE_DIR/${test_name}.sh" "$ENGINE"
  echo
done

echo "########## 20k-memory / ${ENGINE}"
"$SUITE_DIR/bench.sh" "$ENGINE" "${BENCH_CONNS:-20000}" "${BENCH_HOLD:-90}"
