#!/bin/sh
set -eu

mkdir -p /etc/pebble /etc/caddy/nd/user /etc/caddy/nd/panels.gen-000000 /etc/caddy/nd/origins.gen-000000

echo "Waiting for Pebble root CA..."
i=0
until curl -skf https://pebble:15000/roots/0 -o /etc/pebble/pebble-root.pem; do
  i=$((i + 1))
  if [ "$i" -ge 60 ]; then
    echo "Timed out waiting for Pebble root CA" >&2
    exit 1
  fi
  sleep 1
done

cp /etc/pebble/pebble-root.pem /usr/local/share/ca-certificates/pebble-root.crt
update-ca-certificates

if [ ! -f /etc/caddy/Caddyfile ]; then
  ln -sfn /etc/caddy/nd/panels.gen-000000 /etc/caddy/nd/panels
  ln -sfn /etc/caddy/nd/origins.gen-000000 /etc/caddy/nd/origins
  printf '# placeholder\n' > /etc/caddy/nd/panels.gen-000000/00-placeholder.caddy
  printf '# placeholder\n' > /etc/caddy/nd/user/00-placeholder.caddy
  cat <<'EOF' > /etc/caddy/nd/global.opts
servers {
	trusted_proxies static 172.30.0.0/16 127.0.0.1/32
	client_ip_headers CF-Connecting-IP X-Forwarded-For
}
EOF
  cat <<'EOF' > /etc/caddy/nd/common.caddy
(nd_common) {
	encode zstd gzip
	handle_errors 502 503 504 {
		header Cache-Control "no-store"
		respond "Origin is temporarily unavailable. Please retry shortly." 503
	}
}
EOF
  cat <<'EOF' > /etc/caddy/Caddyfile
# >>> nd-edge managed (do not edit inside markers) >>>
{
	admin localhost:2019
	import /etc/caddy/nd/global.opts
}
import /etc/caddy/nd/common.caddy
import /etc/caddy/nd/user/*.caddy
import /etc/caddy/nd/panels/*.caddy
# <<< nd-edge managed <<<
EOF
fi

exec /usr/bin/caddy run --config /etc/caddy/Caddyfile --adapter caddyfile
