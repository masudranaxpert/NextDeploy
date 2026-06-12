#!/usr/bin/env bash
set -euo pipefail

DATA_DIR="${DATA_DIR:-/data}"
PANEL_CONTAINER="${PANEL_CONTAINER:-panel}"
BUNDLE_URL=""
BUNDLE_FILE=""
NO_DEPLOY=""

die() { echo "ERROR: $*" >&2; exit 1; }
info() { echo "[migrate] $*"; }
warn() { echo "[migrate] WARNING: $*" >&2; }

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
    curl -fL -H "Accept-Encoding: identity" --retry 3 --retry-delay 5 -o "$DEST" "$BUNDLE_URL"
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

if head -c 256 "$DEST" 2>/dev/null | grep -aqiE '<!doctype|<html'; then
  rm -f "$DEST"
  die "URL returned HTML, not a .nd-migrate bundle. Use /migrate/download/TOKEN or --file."
fi

bundle_magic=""
if command -v xxd >/dev/null 2>&1; then
  bundle_magic="$(xxd -l 2 -p "$DEST" 2>/dev/null | tr -d '\n' || true)"
elif command -v hexdump >/dev/null 2>&1; then
  bundle_magic="$(hexdump -n 2 -e '2/1 "%02x"' "$DEST" 2>/dev/null || true)"
else
  bundle_magic="$(od -An -tx1 -N2 "$DEST" 2>/dev/null | tr -d ' \n' || true)"
fi

bundle_ok=false
if [[ "$bundle_magic" == "1f8b" ]]; then
  bundle_ok=true
elif command -v file >/dev/null 2>&1 && file -b "$DEST" 2>/dev/null | grep -qiE 'gzip|POSIX tar'; then
  bundle_ok=true
elif command -v python3 >/dev/null 2>&1 && python3 -c "import sys; sys.exit(0 if open(sys.argv[1],'rb').read(2)==b'\\x1f\\x8b' else 1)" "$DEST" 2>/dev/null; then
  bundle_ok=true
fi

if [[ "$bundle_ok" != true ]]; then
  bundle_bytes="$(wc -c <"$DEST" | tr -d ' ')"
  if [[ "${bundle_bytes:-0}" -gt 52428800 ]]; then
    warn "gzip magic not detected (got ${bundle_magic:-unknown}) but file is large (${bundle_bytes} bytes) — continuing"
  else
    rm -f "$DEST"
    die "File is not a valid .nd-migrate gzip bundle (magic ${bundle_magic:-unknown}, size ${bundle_bytes} bytes)."
  fi
fi

chmod 600 "$DEST"
CONTAINER_PATH="/data/migrate-incoming/$(basename "$DEST")"

PANEL_BIN="/usr/local/bin/panel"
if ! docker exec "$PANEL_CONTAINER" test -x "$PANEL_BIN" 2>/dev/null; then
  PANEL_BIN="panel"
fi

IMPORT_FLAGS=(--delete-after)
if [[ -n "$NO_DEPLOY" ]]; then
  IMPORT_FLAGS+=(--no-deploy)
fi

if [[ -f "${DATA_DIR}/panel.db" ]] && command -v sqlite3 >/dev/null 2>&1; then
  if ! sqlite3 "${DATA_DIR}/panel.db" "SELECT 1 FROM users WHERE role='admin' LIMIT 1;" 2>/dev/null | grep -q 1; then
    die "Complete panel setup first (open the panel in a browser and create the admin account), then run migrate again."
  fi
fi

info "Importing into panel (this may take a while)…"
info "docker exec ${PANEL_CONTAINER} ${PANEL_BIN} migrate import ${CONTAINER_PATH}"
if ! docker exec "$PANEL_CONTAINER" "$PANEL_BIN" migrate import "$CONTAINER_PATH" "${IMPORT_FLAGS[@]}"; then
  die "Import failed. Bundle left at ${DEST} for inspection. If you see 'no admin user found', complete panel setup in the browser first."
fi

info "Migration import completed successfully."
