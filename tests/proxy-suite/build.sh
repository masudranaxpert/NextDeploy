#!/usr/bin/env bash
export MSYS_NO_PATHCONV=1
set -euo pipefail

SUITE_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./lib.sh
source "$SUITE_DIR/lib.sh"

TARGET="${1:-${ENGINE:-all}}"
REBUILD=1

build_one() {
  local eng="$1"
  echo "Building suite + bench images for $eng ($(engine_base_image "$eng"))..."
  ensure_engine_images "$eng"
  ensure_bench_images "$eng"
}

echo "Building probe and loadclient images..."
ensure_probe_image
ensure_image "proxy-suite-loadclient:latest" "loadclient/Dockerfile" "loadclient" --build-arg "GO_IMAGE=${GO_IMAGE}"

case "$TARGET" in
  all)
    for eng in caddy angie ferron; do
      build_one "$eng"
    done
    ;;
  caddy|angie|ferron)
    build_one "$TARGET"
    ;;
  *)
    echo "usage: $0 [caddy|angie|ferron|all]" >&2
    exit 1
    ;;
esac

echo "Build complete."
