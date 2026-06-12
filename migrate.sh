#!/usr/bin/env bash
set -euo pipefail

DATA_DIR="${DATA_DIR:-/data}"
PANEL_CONTAINER="${PANEL_CONTAINER:-panel}"
BUNDLE_URL=""
BUNDLE_FILE=""
NO_DEPLOY=""

die() { echo "ERROR: $*" >&2; exit 1; }
info() { echo "[migrate] $*"; }

usage() {
  cat <<'EOF'
NextDeploy panel migration import

Usage:
  migrate.sh --url URL              Download bundle from export link
  migrate.sh --file PATH            Use local .nd-migrate file

Options:
  --data-dir PATH     Data directory (default: /data)
  --container NAME    Panel container name (default: panel)
  --no-deploy         Skip compose up after import
  -h, --help          Show this help

Examples:
  sudo bash migrate.sh --url "https://panel.example.com/migrate/download/TOKEN"
  sudo bash migrate.sh --file /tmp/panel-migrate.nd-migrate
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --url) BUNDLE_URL="$2"; shift 2 ;;
    --file) BUNDLE_FILE="$2"; shift 2 ;;
    --data-dir) DATA_DIR="$2"; shift 2 ;;
    --container) PANEL_CONTAINER="$2"; shift 2 ;;
    --no-deploy) NO_DEPLOY="1"; shift ;;
    -h|--help) usage; exit 0 ;;
    *) die "Unknown option: $1 (try --help)" ;;
  esac
done

if [[ -n "$BUNDLE_URL" && -n "$BUNDLE_FILE" ]]; then
  die "Use either --url or --file, not both"
fi
if [[ -z "$BUNDLE_URL" && -z "$BUNDLE_FILE" ]]; then
  usage
  exit 2
fi

if [[ $EUID -ne 0 ]]; then
  die "Run as root (sudo) so files can be placed under ${DATA_DIR}"
fi

if ! docker ps --format '{{.Names}}' | grep -qx "$PANEL_CONTAINER"; then
  die "Panel container '${PANEL_CONTAINER}' is not running. Start NextDeploy first."
fi

INCOMING="${DATA_DIR}/migrate-incoming"
mkdir -p "$INCOMING"
chmod 700 "$INCOMING"

TS="$(date +%s)"
DEST="${INCOMING}/import-${TS}.nd-migrate"

if [[ -n "$BUNDLE_URL" ]]; then
  info "Downloading bundle…"
  if command -v curl >/dev/null 2>&1; then
    curl -fL --retry 3 --retry-delay 5 -o "$DEST" "$BUNDLE_URL"
  elif command -v wget >/dev/null 2>&1; then
    wget -O "$DEST" "$BUNDLE_URL"
  else
    die "curl or wget required to download bundle"
  fi
else
  if [[ ! -f "$BUNDLE_FILE" ]]; then
    die "Bundle file not found: $BUNDLE_FILE"
  fi
  info "Copying local bundle…"
  cp -f "$BUNDLE_FILE" "$DEST"
fi

if [[ ! -s "$DEST" ]]; then
  rm -f "$DEST"
  die "Downloaded bundle is empty"
fi

chmod 600 "$DEST"
CONTAINER_PATH="/data/migrate-incoming/$(basename "$DEST")"

info "Importing into panel (this may take a while)…"
IMPORT_ARGS=(panel migrate import "$CONTAINER_PATH" --delete-after)
if [[ -n "$NO_DEPLOY" ]]; then
  IMPORT_ARGS+=(--no-deploy)
fi

if ! docker exec "$PANEL_CONTAINER" "${IMPORT_ARGS[@]}"; then
  die "Import failed. Bundle left at ${DEST} for inspection."
fi

info "Migration import completed successfully."
