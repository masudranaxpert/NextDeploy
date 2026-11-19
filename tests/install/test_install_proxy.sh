#!/usr/bin/env bash
set -euo pipefail

export MSYS_NO_PATHCONV=1

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

TMP_ROOT="$(mktemp -d)"
trap 'rm -rf "$TMP_ROOT"' EXIT

assert_contains() {
  local file="$1" needle="$2" msg="$3"
  if ! grep -Fq -- "$needle" "$file"; then
    echo "FAIL: $msg (missing '$needle' in $file)" >&2
    cat "$file" >&2
    exit 1
  fi
}

assert_not_contains() {
  local file="$1" needle="$2" msg="$3"
  if grep -Fq -- "$needle" "$file"; then
    echo "FAIL: $msg (unexpected '$needle' in $file)" >&2
    cat "$file" >&2
    exit 1
  fi
}

assert_eq() {
  local got="$1" want="$2" msg="$3"
  if [[ "$got" != "$want" ]]; then
    echo "FAIL: $msg (got='$got', want='$want')" >&2
    exit 1
  fi
}

validate_compose_yaml() {
  local cf="$1"
  if command -v docker &>/dev/null && docker compose version &>/dev/null 2>&1; then
    local target="$cf"
    if command -v cygpath &>/dev/null; then
      target="$(cygpath -w "$cf")"
    fi
    docker compose -f "$target" config -q
  fi
}

echo "==> [1/7] Checking bash syntax and root compose files..."
bash -n "$REPO_ROOT/install.sh"
bash -n "$REPO_ROOT/uninstall.sh"
validate_compose_yaml "$REPO_ROOT/docker-compose.yml"
validate_compose_yaml "$REPO_ROOT/docker-compose.local.yml"
assert_not_contains "$REPO_ROOT/docker-compose.yml" "depends_on:" "root docker-compose.yml must not hardcode depends_on: caddy"
assert_not_contains "$REPO_ROOT/docker-compose.local.yml" "depends_on:" "docker-compose.local.yml must not hardcode depends_on: caddy"

echo "==> [2/7] Testing --proxy parsing, validation, and non-TTY default..."
(
  export ND_SOURCE_ONLY=1
  # shellcheck source=../../install.sh
  source "$REPO_ROOT/install.sh"

  parse_args --proxy=ANGIE --dir "$TMP_ROOT/opt1" --data-dir "$TMP_ROOT/data1"
  choose_proxy_engine </dev/null
  assert_eq "$PROXY_ENGINE" "angie" "--proxy=ANGIE should normalize to angie"

  PROXY_ENGINE=""
  PROXY_ENGINE_EXPLICIT=false
  INSTALL_DIR="$TMP_ROOT/opt_empty"
  mkdir -p "$INSTALL_DIR"
  choose_proxy_engine </dev/null
  assert_eq "$PROXY_ENGINE" "caddy" "non-TTY without --proxy must default to caddy"
)

if (
  export ND_SOURCE_ONLY=1
  source "$REPO_ROOT/install.sh"
  parse_args --proxy nginx
) >/dev/null 2>&1; then
  echo "FAIL: invalid --proxy nginx should fail" >&2
  exit 1
fi

echo "==> [3/7] Testing fresh compose generation + bootstrap for caddy, angie, ferron..."
for eng in caddy angie ferron; do
  (
    export ND_SOURCE_ONLY=1
    source "$REPO_ROOT/install.sh"
    INSTALL_DIR="$TMP_ROOT/inst_$eng"
    DATA_DIR="$TMP_ROOT/data_$eng"
    COMPOSE_URL="file://$REPO_ROOT/docker-compose.yml"
    parse_args --proxy "$eng" --dir "$INSTALL_DIR" --data-dir "$DATA_DIR"
    choose_proxy_engine </dev/null
    create_directories
    seed_proxy_bootstrap "$PROXY_ENGINE" "$DATA_DIR"
    download_compose
    validate_compose_yaml "$INSTALL_DIR/docker-compose.yml"

    assert_contains "$INSTALL_DIR/docker-compose.yml" "ND_PROXY_ENGINE: $eng" "panel env should set ND_PROXY_ENGINE=$eng"
    assert_not_contains "$INSTALL_DIR/docker-compose.yml" "depends_on:" "$eng compose must not have depends_on"

    case "$eng" in
      caddy)
        assert_contains "$INSTALL_DIR/docker-compose.yml" "container_name: caddy" "caddy service kept intact"
        assert_contains "$INSTALL_DIR/docker-compose.yml" "caddy_data: {}" "caddy_data volume kept intact"
        ;;
      angie)
        assert_contains "$INSTALL_DIR/docker-compose.yml" "container_name: nd-proxy" "angie uses nd-proxy"
        assert_contains "$INSTALL_DIR/docker-compose.yml" "docker.angie.software/angie:1.12.4" "angie image"
        assert_contains "$INSTALL_DIR/docker-compose.yml" "angie_acme: {}" "angie_acme volume"
        assert_contains "$INSTALL_DIR/docker-compose.yml" "/etc/nd-pki/sites:ro" "mounts pki/sites only"
        assert_not_contains "$INSTALL_DIR/docker-compose.yml" "pki/ca" "must never mount ca dir"
        test -f "$DATA_DIR/proxy/angie/sites/angie.conf"
        ;;
      ferron)
        assert_contains "$INSTALL_DIR/docker-compose.yml" "container_name: nd-proxy" "ferron uses nd-proxy"
        assert_contains "$INSTALL_DIR/docker-compose.yml" "ferronserver/ferron:3.0.0-rc.11-alpine" "ferron image"
        assert_contains "$INSTALL_DIR/docker-compose.yml" "ND_FERRON_ADMIN_TOKEN:" "ferron admin token generated"
        assert_contains "$INSTALL_DIR/docker-compose.yml" "--config-adapter ferronconf" "ferronconf adapter flag"
        assert_contains "$INSTALL_DIR/docker-compose.yml" "ferron_acme: {}" "ferron_acme volume"
        test -f "$DATA_DIR/proxy/ferron/sites/ferron.conf"
        ;;
    esac
  )
done

echo "==> [4/7] Testing custom --data-dir bind-mount rewriting..."
(
  export ND_SOURCE_ONLY=1
  source "$REPO_ROOT/install.sh"
  INSTALL_DIR="$TMP_ROOT/inst_custom"
  DATA_DIR="/srv/nd-custom"
  COMPOSE_URL="file://$REPO_ROOT/docker-compose.yml"
  mkdir -p "$INSTALL_DIR"
  PROXY_ENGINE="angie"
  PROXY_ENGINE_EXPLICIT=true
  touch "$INSTALL_DIR/docker-compose.yml"
  download_compose
  validate_compose_yaml "$INSTALL_DIR/docker-compose.yml"
  assert_contains "$INSTALL_DIR/docker-compose.yml" "/srv/nd-custom/proxy/angie:/etc/angie/nd:ro" "custom data-dir angie mount"
  assert_contains "$INSTALL_DIR/docker-compose.yml" "/srv/nd-custom/proxy/pki/sites:/etc/nd-pki/sites:ro" "custom data-dir pki mount"
  assert_contains "$INSTALL_DIR/docker-compose.yml" "/srv/nd-custom/workspaces:/data/workspaces:ro" "custom data-dir workspaces mount"
  assert_contains "$INSTALL_DIR/docker-compose.yml" "/srv/nd-custom/workspaces/.nd-php:/data/workspaces/.nd-php" "custom data-dir php sockets mount"
  assert_contains "$INSTALL_DIR/docker-compose.yml" "/srv/nd-custom:/data" "custom data-dir panel mount"
)

echo "==> [5/7] Testing nextdeploy-update preservation for Caddy and Angie..."
for eng in caddy angie ferron; do
  UP_INST="$TMP_ROOT/up_inst_$eng"
  UP_DATA="$TMP_ROOT/up_data_$eng"
  UP_BIN="$TMP_ROOT/nextdeploy-update-$eng"
  mkdir -p "$UP_INST" "$UP_DATA"
  (
    export ND_SOURCE_ONLY=1
    source "$REPO_ROOT/install.sh"
    INSTALL_DIR="$UP_INST"
    DATA_DIR="/srv/custom-$eng"
    COMPOSE_URL="file://$REPO_ROOT/docker-compose.yml"
    INSTALL_SCRIPT_URL="file://$REPO_ROOT/install.sh"
    PROXY_ENGINE="$eng"
    PROXY_ENGINE_EXPLICIT=true
    touch "$UP_INST/docker-compose.yml"
    download_compose
    create_update_script "$UP_BIN"
  )
  bash -n "$UP_BIN"

  # Inject panel labels, nd_shared volume, and an extra gateway port into existing compose.
  awk '
    {
      print
      if (/^[[:space:]]+image: masudranaxpert\/nextdeploy/) {
        print "        labels:"
        print "            caddy: panel.example.com"
        print "            caddy.reverse_proxy: \"{{upstreams 8080}}\""
      }
      if (/443:443\/udp/) {
        print "            - 4013:4013"
      }
      if (/:\/data\/workspaces:ro/) {
        print "            - nd_shared_media:/data/shared/media:ro"
      }
    }
    END {
      print "    nd_shared_media:"
      print "        external: true"
      print "        name: flixbd_media_data"
    }
  ' "$UP_INST/docker-compose.yml" > "$UP_INST/docker-compose.yml.patched"
  mv "$UP_INST/docker-compose.yml.patched" "$UP_INST/docker-compose.yml"

  ORIG_FERRON_TOK=""
  if [[ "$eng" == "ferron" ]]; then
    ORIG_FERRON_TOK="$(awk '/ND_FERRON_ADMIN_TOKEN/{print $2}' "$UP_INST/docker-compose.yml")"
  fi

  ND_SKIP_DOCKER_UP=1 bash "$UP_BIN"
  validate_compose_yaml "$UP_INST/docker-compose.yml"

  assert_contains "$UP_INST/docker-compose.yml" "ND_PROXY_ENGINE: $eng" "nextdeploy-update preserves engine $eng"
  assert_contains "$UP_INST/docker-compose.yml" "caddy: panel.example.com" "nextdeploy-update preserves panel labels ($eng)"
  assert_contains "$UP_INST/docker-compose.yml" "nd_shared_media:/data/shared/media:ro" "nextdeploy-update preserves nd_shared mount ($eng)"
  assert_contains "$UP_INST/docker-compose.yml" "name: flixbd_media_data" "nextdeploy-update preserves nd_shared volume ($eng)"
  assert_contains "$UP_INST/docker-compose.yml" "4013:4013" "nextdeploy-update preserves gateway port ($eng)"
  assert_contains "$UP_INST/docker-compose.yml" "/srv/custom-$eng:/data" "nextdeploy-update preserves DATA_DIR ($eng)"
  if [[ "$eng" == "angie" ]]; then
    assert_contains "$UP_INST/docker-compose.yml" "container_name: nd-proxy" "nextdeploy-update keeps nd-proxy for angie"
    assert_contains "$UP_INST/docker-compose.yml" "angie_acme: {}" "nextdeploy-update keeps angie_acme"
  elif [[ "$eng" == "ferron" ]]; then
    assert_contains "$UP_INST/docker-compose.yml" "ND_FERRON_ADMIN_TOKEN: $ORIG_FERRON_TOK" "nextdeploy-update keeps ferron admin token"
  fi
done

echo "==> [5b/7] Testing nextdeploy-update self-update and re-exec..."
(
  SELF_INST="$TMP_ROOT/self_up_inst"
  SELF_BIN="$TMP_ROOT/nextdeploy-update-self"
  mkdir -p "$SELF_INST"
  export ND_SOURCE_ONLY=1
  source "$REPO_ROOT/install.sh"
  INSTALL_DIR="$SELF_INST"
  DATA_DIR="/srv/self-data"
  COMPOSE_URL="file://$REPO_ROOT/docker-compose.yml"
  INSTALL_SCRIPT_URL="file://$REPO_ROOT/install.sh"
  PROXY_ENGINE="angie"
  PROXY_ENGINE_EXPLICIT=true
  ANGIE_IMAGE="docker.angie.software/angie:0.0.0-old"
  touch "$SELF_INST/docker-compose.yml"
  download_compose
  create_update_script "$SELF_BIN"

  assert_contains "$SELF_BIN" "docker.angie.software/angie:0.0.0-old" "initial script starts with old ANGIE_IMAGE"

  SELF_OUT="$(ND_SKIP_DOCKER_UP=1 bash "$SELF_BIN")"
  echo "$SELF_OUT" | grep -Fq "Updated nextdeploy-update script to latest version; re-running..." || {
    echo "FAIL: expected self-update notice in output" >&2
    echo "$SELF_OUT" >&2
    exit 1
  }
  assert_contains "$SELF_BIN" "docker.angie.software/angie:1.12.4" "self-update refreshed ANGIE_IMAGE from install.sh"
  assert_contains "$SELF_BIN" "DATA_DIR=\"/srv/self-data\"" "self-update preserved DATA_DIR"
  assert_contains "$SELF_INST/docker-compose.yml" "docker.angie.software/angie:1.12.4" "re-execed script applied refreshed ANGIE_IMAGE"

  SELF_OUT_SECOND="$(ND_SKIP_DOCKER_UP=1 bash "$SELF_BIN")"
  if echo "$SELF_OUT_SECOND" | grep -Fq "Updated nextdeploy-update script"; then
    echo "FAIL: up-to-date nextdeploy-update must not re-update on second run" >&2
    exit 1
  fi
)

echo "==> [6/7] Testing uninstall.sh --keep-data vs full removal (mocked docker)..."
MOCK_BIN="$TMP_ROOT/mock_bin"
DOCKER_LOG="$TMP_ROOT/docker.log"
mkdir -p "$MOCK_BIN"
cat > "$MOCK_BIN/docker" <<EOF
#!/usr/bin/env bash
echo "docker \$*" >> "$DOCKER_LOG"
exit 0
EOF
chmod +x "$MOCK_BIN/docker"

# Test --keep-data preserves DATA_DIR/proxy and skips volume deletion
UN_DATA="$TMP_ROOT/un_data"
UN_INST="$TMP_ROOT/un_inst"
mkdir -p "$UN_DATA/proxy/pki/ca" "$UN_INST"
(
  export PATH="$MOCK_BIN:$PATH"
  export ND_SOURCE_ONLY=1
  source "$REPO_ROOT/uninstall.sh"
  INSTALL_DIR="$UN_INST"
  DATA_DIR="$UN_DATA"
  KEEP_DATA=true
  stop_services
  remove_images
  remove_volumes
  remove_data_dir
)
test -d "$UN_DATA/proxy/pki/ca"
assert_not_contains "$DOCKER_LOG" "volume rm" "--keep-data must not delete proxy volumes"
assert_contains "$DOCKER_LOG" "docker rm -f panel caddy nd-proxy" "uninstall stops/removes nd-proxy alongside caddy"
assert_contains "$DOCKER_LOG" "docker.angie.software/angie:1.12.4" "uninstall cleans angie image"
assert_contains "$DOCKER_LOG" "ferronserver/ferron:3.0.0-rc.11-alpine" "uninstall cleans ferron image"

# Test full uninstall (KEEP_DATA=false) removes DATA_DIR/proxy and volumes
: > "$DOCKER_LOG"
(
  export PATH="$MOCK_BIN:$PATH"
  export ND_SOURCE_ONLY=1
  source "$REPO_ROOT/uninstall.sh"
  INSTALL_DIR="$UN_INST"
  DATA_DIR="$UN_DATA"
  KEEP_DATA=false
  remove_volumes
  remove_data_dir
)
if [[ -e "$UN_DATA" ]]; then
  echo "FAIL: full uninstall must delete DATA_DIR including DATA_DIR/proxy" >&2
  exit 1
fi
assert_contains "$DOCKER_LOG" "docker volume rm angie_acme" "full uninstall removes angie_acme"
assert_contains "$DOCKER_LOG" "docker volume rm ferron_acme" "full uninstall removes ferron_acme"
assert_contains "$DOCKER_LOG" "docker volume rm caddy_data" "full uninstall removes caddy_data"

echo "==> [7/7] Checking line endings on modified shell scripts..."
if grep -q $'\r' "$REPO_ROOT/install.sh" "$REPO_ROOT/uninstall.sh" "$SCRIPT_DIR/test_install_proxy.sh"; then
  echo "FAIL: CRLF line endings detected in shell script" >&2
  exit 1
fi

echo "PASS: All install/uninstall/compose multi-proxy tests passed."
