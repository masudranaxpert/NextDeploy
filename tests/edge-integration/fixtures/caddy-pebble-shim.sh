#!/bin/sh
set -eu

REAL_CADDY="/usr/bin/caddy.real"

if [ -z "${PEBBLE_ACME_CA:-}" ]; then
  exec "$REAL_CADDY" "$@"
fi

subcmd="${1:-}"
case "$subcmd" in
  run|validate|adapt|reload)
    ;;
  *)
    exec "$REAL_CADDY" "$@"
    ;;
esac

cfg_path=""
prev=""
for arg in "$@"; do
  if [ "$prev" = "--config" ]; then
    cfg_path="$arg"
    break
  fi
  prev="$arg"
done

if [ -z "$cfg_path" ] || [ ! -f "$cfg_path" ]; then
  exec "$REAL_CADDY" "$@"
fi

tmp_cfg="$(mktemp /tmp/caddy-pebble.XXXXXX.Caddyfile)"
if [ "$subcmd" != "run" ]; then
  trap 'rm -f "$tmp_cfg"' EXIT
fi

awk -v ca="$PEBBLE_ACME_CA" -v root="${PEBBLE_ACME_CA_ROOT:-}" '
BEGIN { injected = 0 }
{
  print $0
  if (!injected && $0 ~ /^[[:space:]]*\{[[:space:]]*$/) {
    print "\tacme_ca " ca
    if (root != "") {
      print "\tacme_ca_root " root
    }
    injected = 1
  }
}
' "$cfg_path" > "$tmp_cfg"

# Rebuild positional args replacing cfg_path with tmp_cfg.
new_args=""
replace_next=0
i=1
for arg in "$@"; do
  if [ "$replace_next" -eq 1 ]; then
    eval "arg_${i}=\"$tmp_cfg\""
    replace_next=0
  else
    eval "arg_${i}=\"\$arg\""
    if [ "$arg" = "--config" ]; then
      replace_next=1
    fi
  fi
  i=$((i + 1))
done

case "$#" in
  1) "$REAL_CADDY" "$arg_1" ;;
  2) "$REAL_CADDY" "$arg_1" "$arg_2" ;;
  3) "$REAL_CADDY" "$arg_1" "$arg_2" "$arg_3" ;;
  4) "$REAL_CADDY" "$arg_1" "$arg_2" "$arg_3" "$arg_4" ;;
  5) "$REAL_CADDY" "$arg_1" "$arg_2" "$arg_3" "$arg_4" "$arg_5" ;;
  6) "$REAL_CADDY" "$arg_1" "$arg_2" "$arg_3" "$arg_4" "$arg_5" "$arg_6" ;;
  7) "$REAL_CADDY" "$arg_1" "$arg_2" "$arg_3" "$arg_4" "$arg_5" "$arg_6" "$arg_7" ;;
  8) "$REAL_CADDY" "$arg_1" "$arg_2" "$arg_3" "$arg_4" "$arg_5" "$arg_6" "$arg_7" "$arg_8" ;;
  *)
    echo "caddy-pebble-shim: too many args" >&2
    exit 1
    ;;
esac
