#!/bin/sh
set -eu

mkdir -p /etc/nd-edge /var/lib/nd-edge/state /etc/caddy/nd/user /run /etc/pebble

echo "Waiting for Pebble root CA..."
i=0
until [ -s /etc/pebble/pebble-root.pem ] || curl -skf https://pebble:15000/roots/0 -o /etc/pebble/pebble-root.pem; do
  i=$((i + 1))
  if [ "$i" -ge 60 ]; then
    echo "Timed out waiting for Pebble root CA" >&2
    exit 1
  fi
  sleep 1
done

cp /etc/pebble/pebble-root.pem /usr/local/share/ca-certificates/pebble-root.crt
update-ca-certificates

echo "Waiting for Caddy Admin API on localhost:2019..."
i=0
until curl -sf http://localhost:2019/config/ >/dev/null 2>&1; do
  i=$((i + 1))
  if [ "$i" -ge 60 ]; then
    echo "Timed out waiting for Caddy admin API" >&2
    exit 1
  fi
  sleep 1
done

if [ ! -f /etc/nd-edge/config.json ]; then
  echo "Waiting for panel-a and panel-b..."
  for url in http://panel-a:8080/healthz http://panel-b:8080/healthz; do
    i=0
    until curl -sf "$url" >/dev/null 2>&1; do
      i=$((i + 1))
      if [ "$i" -ge 60 ]; then
        echo "Timed out waiting for $url" >&2
        exit 1
      fi
      sleep 1
    done
  done

  edge_id="${ND_EDGE_ID:-edge-compose-0001}"
  shield_ip="${ND_SHIELD_IP:-172.30.0.10}"
  tok_a="${PANEL_A_TOKEN:-shld_panel_a_token}"
  tok_b="${PANEL_B_TOKEN:-shld_panel_b_token}"

  # Register both panels via /api/v1/edge/register and seed config.json with
  # dns_gate_off=true (Pebble resolves domains via pebble-challtestsrv:8053).
  reg_payload="{\"edge_id\":\"$edge_id\",\"edge_version\":\"dev\",\"caddy_version\":\"v2.8.4\",\"hostname\":\"shield-compose\",\"public_ipv4\":[\"$shield_ip\"]}"
  curl -sf -X POST http://panel-a:8080/api/v1/edge/register \
    -H "Authorization: Bearer $tok_a" \
    -H "Content-Type: application/json" \
    -d "$reg_payload" >/dev/null
  curl -sf -X POST http://panel-b:8080/api/v1/edge/register \
    -H "Authorization: Bearer $tok_b" \
    -H "Content-Type: application/json" \
    -d "$reg_payload" >/dev/null

  now_rfc="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  cat <<EOF > /etc/nd-edge/config.json
{
  "edge_id": "$edge_id",
  "admin_addr": "localhost:2019",
  "dns_gate_off": true,
  "per_panel_quota": 2000,
  "caddy_user": "root",
  "shield_ips": ["$shield_ip"],
  "panels": [
    {
      "panel_id": "${PANEL_A_ID:-11111111-1111-4111-8111-111111111111}",
      "panel_name": "panel-a",
      "shield_node_id": 1,
      "url": "http://panel-a:8080",
      "token": "$tok_a",
      "poll_interval_sec": 3,
      "registered_at": "$now_rfc",
      "insecure_http": true
    },
    {
      "panel_id": "${PANEL_B_ID:-22222222-2222-4222-8222-222222222222}",
      "panel_name": "panel-b",
      "shield_node_id": 1,
      "url": "http://panel-b:8080",
      "token": "$tok_b",
      "poll_interval_sec": 3,
      "registered_at": "$now_rfc",
      "insecure_http": true
    }
  ]
}
EOF
  chmod 0600 /etc/nd-edge/config.json
fi

exec /usr/local/bin/nd edge run
