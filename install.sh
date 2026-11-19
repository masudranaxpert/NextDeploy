#!/usr/bin/env bash
# =============================================================================
#  NextDeploy — Install script (branch main)
#  Docker image masudranaxpert/nextdeploy:latest; compose file from branch main.
#
#  Usage:
#    curl -fsSL https://raw.githubusercontent.com/masudranaxpert/NextDeploy/main/install.sh | sudo bash
#    sudo bash install.sh [--domain panel.example.com] [--email admin@example.com] [--proxy caddy|angie|ferron]
# =============================================================================
set -euo pipefail

# ── Colors ────────────────────────────────────────────────────────────────────
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
BOLD='\033[1m'
RESET='\033[0m'

# ── Config defaults ───────────────────────────────────────────────────────────
INSTALL_DIR="/opt/nextdeploy"
DATA_DIR="/data"
COMPOSE_URL="${NEXTDEPLOY_COMPOSE_URL:-https://raw.githubusercontent.com/masudranaxpert/NextDeploy/main/docker-compose.yml}"
INSTALL_SCRIPT_URL="${NEXTDEPLOY_INSTALL_URL:-https://raw.githubusercontent.com/masudranaxpert/NextDeploy/main/install.sh}"
# Panel image tag (must match docker-main.yml policy for main)
PANEL_IMAGE_TAG="latest"
PANEL_DOMAIN=""
CADDY_EMAIL=""
PROXY_ENGINE=""
PROXY_ENGINE_EXPLICIT=false
ANGIE_IMAGE="docker.angie.software/angie:1.12.4"
FERRON_IMAGE="ferronserver/ferron:3.0.0-rc.11-alpine"
MIN_DOCKER_VERSION="24"

# ── Helpers ────────────────────────────────────────────────────────────────────
info()    { echo -e "${CYAN}[INFO]${RESET}  $*"; }
success() { echo -e "${GREEN}[OK]${RESET}    $*"; }
warn()    { echo -e "${YELLOW}[WARN]${RESET}  $*"; }
error()   { echo -e "${RED}[ERROR]${RESET} $*" >&2; }
die()     { error "$*"; exit 1; }

banner() {
  echo -e "${BOLD}${BLUE}"
  echo "  ███╗   ██╗███████╗██╗  ██╗████████╗██████╗ ███████╗██████╗ ██╗      ██████╗ ██╗   ██╗"
  echo "  ████╗  ██║██╔════╝╚██╗██╔╝╚══██╔══╝██╔══██╗██╔════╝██╔══██╗██║     ██╔═══██╗╚██╗ ██╔╝"
  echo "  ██╔██╗ ██║█████╗   ╚███╔╝    ██║   ██║  ██║█████╗  ██████╔╝██║     ██║   ██║ ╚████╔╝ "
  echo "  ██║╚██╗██║██╔══╝   ██╔██╗    ██║   ██║  ██║██╔══╝  ██╔═══╝ ██║     ██║   ██║  ╚██╔╝  "
  echo "  ██║ ╚████║███████╗██╔╝ ██╗   ██║   ██████╔╝███████╗██║     ███████╗╚██████╔╝   ██║   "
  echo "  ╚═╝  ╚═══╝╚══════╝╚═╝  ╚═╝   ╚═╝   ╚═════╝ ╚══════╝╚═╝     ╚══════╝ ╚═════╝    ╚═╝   "
  echo -e "${RESET}"
  echo -e "  ${BOLD}Self-hosted Docker deployment panel${RESET}"
  echo -e "  ${CYAN}https://github.com/masudranaxpert/NextDeploy${RESET}"
  echo ""
}

validate_proxy_engine() {
  case "${1:-}" in
    caddy|angie|ferron) return 0 ;;
    *) return 1 ;;
  esac
}

# ── Argument parsing ──────────────────────────────────────────────────────────
parse_args() {
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --domain)
        [[ $# -ge 2 ]] || die "--domain requires a value"
        PANEL_DOMAIN="$2"; shift 2 ;;
      --domain=*)
        PANEL_DOMAIN="${1#--domain=}"; shift ;;
      --email)
        [[ $# -ge 2 ]] || die "--email requires a value"
        CADDY_EMAIL="$2"; shift 2 ;;
      --email=*)
        CADDY_EMAIL="${1#--email=}"; shift ;;
      --dir)
        [[ $# -ge 2 ]] || die "--dir requires a value"
        INSTALL_DIR="$2"; shift 2 ;;
      --dir=*)
        INSTALL_DIR="${1#--dir=}"; shift ;;
      --data-dir)
        [[ $# -ge 2 ]] || die "--data-dir requires a value"
        DATA_DIR="$2"; shift 2 ;;
      --data-dir=*)
        DATA_DIR="${1#--data-dir=}"; shift ;;
      --proxy)
        [[ $# -ge 2 ]] || die "--proxy requires a value: caddy, angie, or ferron"
        PROXY_ENGINE="$(echo "$2" | tr '[:upper:]' '[:lower:]' | tr -d '[:space:]')"
        validate_proxy_engine "$PROXY_ENGINE" || die "Invalid --proxy '$2'. Choose: caddy, angie, or ferron."
        PROXY_ENGINE_EXPLICIT=true
        shift 2 ;;
      --proxy=*)
        PROXY_ENGINE="$(echo "${1#--proxy=}" | tr '[:upper:]' '[:lower:]' | tr -d '[:space:]')"
        validate_proxy_engine "$PROXY_ENGINE" || die "Invalid --proxy '${1#--proxy=}'. Choose: caddy, angie, or ferron."
        PROXY_ENGINE_EXPLICIT=true
        shift ;;
      --help|-h)
        echo "Usage: install.sh [options]"
        echo ""
        echo "Options:"
        echo "  --domain DOMAIN       Panel domain (e.g. panel.example.com)"
        echo "  --email  EMAIL        Let's Encrypt email for HTTPS"
        echo "  --dir    PATH         Install directory (default: /opt/nextdeploy)"
        echo "  --data-dir PATH       Data directory (default: /data)"
        echo "  --proxy ENGINE        Reverse proxy engine: caddy | angie | ferron (default: caddy)"
        echo "  --help                Show this help"
        exit 0
        ;;
      *) warn "Unknown option: $1"; shift ;;
    esac
  done
}

choose_proxy_engine() {
  if [[ "$PROXY_ENGINE_EXPLICIT" == true ]]; then
    validate_proxy_engine "$PROXY_ENGINE" || die "Invalid proxy engine: $PROXY_ENGINE"
  elif [[ -s "$INSTALL_DIR/docker-compose.yml" ]]; then
    PROXY_ENGINE="$(detect_proxy_engine_in_compose "$INSTALL_DIR/docker-compose.yml")"
    info "Preserving existing proxy engine from docker-compose.yml: $PROXY_ENGINE"
  elif [[ -t 0 ]]; then
    echo ""
    echo "Choose reverse proxy engine:"
    echo "  1) Caddy  (default, simplest; label-driven)"
    echo "  2) Angie  (lowest RAM per connection, best for small VPS / high traffic)"
    echo "  3) Ferron (experimental: fastest, but missing idle/write timeouts)"
    local choice=""
    read -rp "[1]: " choice || choice=""
    choice="$(echo "$choice" | tr '[:upper:]' '[:lower:]' | tr -d '[:space:]')"
    case "$choice" in
      ""|1|caddy)  PROXY_ENGINE="caddy" ;;
      2|angie)     PROXY_ENGINE="angie" ;;
      3|ferron)    PROXY_ENGINE="ferron" ;;
      *)           die "Invalid selection '$choice'. Expected 1 (caddy), 2 (angie), or 3 (ferron)." ;;
    esac
  else
    PROXY_ENGINE="caddy"
  fi

  if [[ "$PROXY_ENGINE" == "ferron" ]]; then
    warn "Ferron is experimental: HTTP/2 idle timeout, write timeout, and upstream reload fixes are pending."
  fi
  info "Reverse proxy engine: $PROXY_ENGINE"
}

# ── Checks ────────────────────────────────────────────────────────────────────
check_root() {
  if [[ $EUID -ne 0 ]]; then
    die "This script must be run as root. Try: sudo bash install.sh"
  fi
}

check_os() {
  if [[ "$(uname -s)" != "Linux" ]]; then
    die "NextDeploy only supports Linux."
  fi
  info "OS: $(. /etc/os-release 2>/dev/null && echo "$PRETTY_NAME" || uname -sr)"
}

check_arch() {
  ARCH=$(uname -m)
  case "$ARCH" in
    x86_64|amd64)  ;;
    aarch64|arm64) warn "ARM64 detected — make sure the Docker image supports it." ;;
    *)             die "Unsupported architecture: $ARCH" ;;
  esac
  info "Architecture: $ARCH"
}

check_docker() {
  if ! command -v docker &>/dev/null; then
    warn "Docker not found. Installing Docker..."
    install_docker
    return
  fi
  DOCKER_VER=$(docker version --format '{{.Server.Version}}' 2>/dev/null | cut -d. -f1 || echo "0")
  if [[ "$DOCKER_VER" -lt "$MIN_DOCKER_VERSION" ]]; then
    die "Docker version $DOCKER_VER is too old. Need $MIN_DOCKER_VERSION+. Run: curl -fsSL https://get.docker.com | sh"
  fi
  success "Docker $(docker version --format '{{.Server.Version}}' 2>/dev/null) found"
}

check_docker_compose() {
  if ! docker compose version &>/dev/null; then
    die "Docker Compose V2 not found. Install it: https://docs.docker.com/compose/install/"
  fi
  success "Docker Compose V2 found"
}

check_ports() {
  for port in 80 443 8080; do
    if ss -tlnp 2>/dev/null | grep -q ":${port} " || \
       netstat -tlnp 2>/dev/null | grep -q ":${port} "; then
      warn "Port $port is already in use. This may cause conflicts."
    fi
  done
}

check_memory() {
  MEM_KB=$(grep MemTotal /proc/meminfo 2>/dev/null | awk '{print $2}' || echo 0)
  MEM_MB=$((MEM_KB / 1024))
  if [[ $MEM_MB -lt 512 ]]; then
    warn "Less than 512MB RAM detected ($MEM_MB MB). NextDeploy may run slowly."
  else
    success "Memory: ${MEM_MB} MB"
  fi
}

tune_system() {
  info "Checking kernel and network tuning..."
  local current_cc
  current_cc=$(sysctl -n net.ipv4.tcp_congestion_control 2>/dev/null || echo "")
  if [[ "$current_cc" != "bbr" ]]; then
    info "Enabling Google BBR + FQ network pacing..."
    modprobe tcp_bbr 2>/dev/null || true
    if ! grep -q 'tcp_congestion_control=bbr' /etc/sysctl.conf 2>/dev/null; then
      echo 'net.core.default_qdisc=fq' >> /etc/sysctl.conf
      echo 'net.ipv4.tcp_congestion_control=bbr' >> /etc/sysctl.conf
      echo 'net.ipv4.tcp_slow_start_after_idle=0' >> /etc/sysctl.conf
      echo 'net.core.somaxconn=65535' >> /etc/sysctl.conf
    fi
    sysctl -p 2>/dev/null || true
    if [[ "$(sysctl -n net.ipv4.tcp_congestion_control 2>/dev/null)" == "bbr" ]]; then
      success "Google BBR congestion control enabled"
    fi
  else
    success "TCP BBR is already active"
  fi
}

# ── Docker install (if missing) ───────────────────────────────────────────────
install_docker() {
  info "Installing Docker via official script..."
  if command -v curl &>/dev/null; then
    curl -fsSL https://get.docker.com | sh
  elif command -v wget &>/dev/null; then
    wget -qO- https://get.docker.com | sh
  else
    die "Neither curl nor wget found. Install Docker manually: https://docs.docker.com/engine/install/"
  fi
  systemctl enable --now docker 2>/dev/null || true
  success "Docker installed"
}

# ── Install & Compose Helpers ─────────────────────────────────────────────────
create_directories() {
  info "Creating directories..."
  mkdir -p "$INSTALL_DIR"
  mkdir -p "$DATA_DIR" "$DATA_DIR/workspaces" "$DATA_DIR/workspaces/.nd-php"
  chmod 750 "$DATA_DIR"
  if [[ "$PROXY_ENGINE" == "angie" || "$PROXY_ENGINE" == "ferron" ]]; then
    mkdir -p "$DATA_DIR/proxy/pki/ca" "$DATA_DIR/proxy/pki/sites" "$DATA_DIR/proxy/pki/imported"
    chmod 700 "$DATA_DIR/proxy/pki/ca"
    chmod 755 "$DATA_DIR/proxy/pki/sites" "$DATA_DIR/proxy/pki/imported"
    mkdir -p "$DATA_DIR/proxy/$PROXY_ENGINE"
  fi
  # Ensure a regular file exists before first docker compose up (avoids Docker creating docker-compose.yml as a directory).
  touch "$INSTALL_DIR/docker-compose.yml"
  success "Directories created: $INSTALL_DIR, $DATA_DIR"
}

seed_proxy_bootstrap() {
  local engine="${1:-$PROXY_ENGINE}"
  local data_dir="${2:-$DATA_DIR}"
  case "$engine" in
    angie)
      local base="$data_dir/proxy/angie"
      local boot="$base/sites.gen-bootstrap"
      mkdir -p "$base"
      if [[ ! -f "$base/sites/angie.conf" ]]; then
        mkdir -p "$boot"
        cat > "$boot/angie.conf" <<'EOF'
user root;
worker_processes auto;
worker_rlimit_nofile 1048576;
pid /run/angie/angie.pid;
error_log stderr warn;
events { worker_connections 65535; }
http {
    include /etc/angie/mime.types;
    access_log off;
    server {
        listen 127.0.0.1:2019;
        location /status/ { api /status/; allow 127.0.0.1; deny all; }
    }
    server {
        listen 80 default_server;
        return 404;
    }
}
EOF
        chmod 644 "$boot/angie.conf"
        ln -sfn "sites.gen-bootstrap" "$base/sites"
        info "Seeded bootstrap Angie config at $base/sites/angie.conf"
      fi
      ;;
    ferron)
      local base="$data_dir/proxy/ferron"
      local boot="$base/sites.gen-bootstrap"
      mkdir -p "$base"
      if [[ ! -f "$base/sites/ferron.conf" ]]; then
        mkdir -p "$boot"
        cat > "$boot/ferron.conf" <<'EOF'
{
    admin {
        listen "127.0.0.1:8081"
        auth_token "{{env.ND_FERRON_ADMIN_TOKEN}}"
    }
    http {
        protocols h1 h2
        timeout "3660s"
    }
    strict_hosts
}
*:80 {
    status 404
}
EOF
        chmod 644 "$boot/ferron.conf"
        ln -sfn "sites.gen-bootstrap" "$base/sites"
        info "Seeded bootstrap Ferron config at $base/sites/ferron.conf"
      fi
      ;;
    *) ;;
  esac
}

generate_random_token() {
  if command -v openssl &>/dev/null; then
    openssl rand -hex 32
  elif [[ -r /dev/urandom ]]; then
    head -c 32 /dev/urandom | od -An -tx1 | tr -d ' \n'
  else
    printf '%s-%s' "$(date +%s)" "$$"
  fi
}

detect_proxy_engine_in_compose() {
  local f="$1"
  [[ -s "$f" ]] || { echo "caddy"; return 0; }
  awk '
    function ltrim(s) { sub(/^[[:space:]]+/, "", s); return s }
    function get_indent(s) { return length(s) - length(ltrim(s)) }
    /^services:[[:space:]]*$/ { in_svc=1; svc=""; next }
    in_svc && /^[a-zA-Z0-9_-]+:/ && get_indent($0) == 0 { in_svc=0 }
    in_svc && /^[[:space:]]+(caddy|nd-proxy|proxy):[[:space:]]*$/ {
      svc=ltrim($0); sub(/:[[:space:]]*$/, "", svc)
      svc_indent=get_indent($0); next
    }
    in_svc && svc != "" && NF > 0 && get_indent($0) <= svc_indent { svc="" }
    in_svc && svc != "" && /^[[:space:]]+image:[[:space:]]*/ {
      line=$0; sub(/^[[:space:]]+image:[[:space:]]*/, "", line)
      if (line ~ /ferron/) { found="ferron" }
      else if (line ~ /angie/) { found="angie" }
      else if (line ~ /caddy/) { found="caddy" }
    }
    /ND_PROXY_ENGINE/ {
      val=$0; sub(/^.*ND_PROXY_ENGINE[=:][[:space:]]*/, "", val)
      gsub(/["'\''[:space:]]/, "", val)
      if (val == "angie" || val == "ferron" || val == "caddy") env_engine=val
    }
    END {
      if (found != "") print found
      else if (env_engine != "") print env_engine
      else print "caddy"
    }
  ' "$f"
}

extract_ferron_token_from_compose() {
  local f="$1"
  [[ -s "$f" ]] || return 0
  awk '
    /ND_FERRON_ADMIN_TOKEN/ {
      val=$0
      sub(/^.*ND_FERRON_ADMIN_TOKEN[=:][[:space:]]*/, "", val)
      gsub(/^["'\'']|["'\''[:space:]]+$/, "", val)
      if (val != "") { print val; exit }
    }
  ' "$f"
}

emit_nd_proxy_service_yaml() {
  local engine="$1"
  local ferron_token="${2:-}"
  if [[ "$engine" == "angie" ]]; then
    cat <<EOF
    nd-proxy:
        command:
            - sh
            - -c
            - while [ ! -f /etc/angie/nd/sites/angie.conf ]; do sleep 0.2; done; exec angie -c /etc/angie/nd/sites/angie.conf -g 'daemon off;'
        container_name: nd-proxy
        image: ${ANGIE_IMAGE}
        networks:
            - nextdeploy
        ports:
            - 80:80
            - 443:443/tcp
            - 443:443/udp
        restart: unless-stopped
        ulimits:
            nofile:
                soft: 1048576
                hard: 1048576
        volumes:
            - /var/run/docker.sock:/var/run/docker.sock
            - /data/proxy/angie:/etc/angie/nd:ro
            - /data/proxy/pki/sites:/etc/nd-pki/sites:ro
            - /data/proxy/pki/imported:/etc/nd-pki/imported:ro
            - angie_acme:/var/lib/angie/acme
            - /data/workspaces:/data/workspaces:ro
            - /data/workspaces/.nd-php:/data/workspaces/.nd-php
EOF
  elif [[ "$engine" == "ferron" ]]; then
    [[ -n "$ferron_token" ]] || ferron_token="$(generate_random_token)"
    cat <<EOF
    nd-proxy:
        command:
            - sh
            - -c
            - while [ ! -f /etc/ferron/nd/sites/ferron.conf ]; do sleep 0.2; done; exec /usr/local/bin/ferron run -c /etc/ferron/nd/sites/ferron.conf --config-adapter ferronconf
        container_name: nd-proxy
        environment:
            ND_FERRON_ADMIN_TOKEN: "${ferron_token}"
        image: ${FERRON_IMAGE}
        networks:
            - nextdeploy
        ports:
            - 80:80
            - 443:443/tcp
            - 443:443/udp
        restart: unless-stopped
        user: "0:0"
        volumes:
            - /data/proxy/ferron:/etc/ferron/nd:ro
            - /data/proxy/pki/sites:/etc/nd-pki/sites:ro
            - /data/proxy/pki/imported:/etc/nd-pki/imported:ro
            - ferron_acme:/var/cache/ferron-acme
            - /data/workspaces:/data/workspaces:ro
            - /data/workspaces/.nd-php:/data/workspaces/.nd-php
EOF
  fi
}

apply_proxy_engine_to_compose() {
  local f="$1"
  local engine="$2"
  local ferron_token="${3:-}"
  [[ -f "$f" ]] || return 0

  local svc_tmp="" vol_line="    caddy_data: {}"
  if [[ "$engine" == "angie" || "$engine" == "ferron" ]]; then
    svc_tmp=$(mktemp)
    emit_nd_proxy_service_yaml "$engine" "$ferron_token" >"$svc_tmp"
    if [[ "$engine" == "angie" ]]; then
      vol_line="    angie_acme: {}"
    else
      vol_line="    ferron_acme: {}"
    fi
  fi

  local out
  out=$(mktemp)
  awk -v engine="$engine" -v sf="$svc_tmp" -v vol_line="$vol_line" '
    function ltrim(s) { sub(/^[[:space:]]+/, "", s); return s }
    function get_indent(s) { return length(s) - length(ltrim(s)) }
    BEGIN {
      if (sf != "") {
        while ((getline line < sf) > 0) nd_svc = nd_svc line "\n"
        close(sf)
        sub(/\n$/, "", nd_svc)
      }
      in_services=0; in_volumes=0; skip_proxy=0; skip_deps=0
      in_panel=0; in_env=0; env_done=0; svc_replaced=0
    }
    {
      cur = get_indent($0)
      if (cur == 0 && NF > 0) {
        if (in_panel && in_env && !env_done) {
          if (env_list) printf "%s- ND_PROXY_ENGINE=%s\n", env_child_indent, engine
          else printf "%sND_PROXY_ENGINE: %s\n", env_child_indent, engine
          env_done=1
        }
        skip_proxy=0; skip_deps=0; in_panel=0; in_env=0
        in_services = ($0 ~ /^services:[[:space:]]*$/)
        in_volumes  = ($0 ~ /^volumes:[[:space:]]*$/)
        print; next
      }

      if (in_services) {
        if (skip_proxy) {
          if (NF > 0 && cur <= proxy_indent) {
            skip_proxy=0
          } else {
            next
          }
        }
        if (engine != "caddy" && /^[[:space:]]+(caddy|nd-proxy|proxy):[[:space:]]*$/) {
          skip_proxy=1; proxy_indent=cur
          if (!svc_replaced && nd_svc != "") {
            print nd_svc
            svc_replaced=1
          }
          next
        }
        if (/^[[:space:]]+panel:[[:space:]]*$/) {
          in_panel=1; panel_indent=cur; in_env=0; env_done=0; skip_deps=0
          print; next
        }
        if (in_panel && NF > 0 && cur <= panel_indent) {
          if (in_env && !env_done) {
            if (env_list) printf "%s- ND_PROXY_ENGINE=%s\n", env_child_indent, engine
            else printf "%sND_PROXY_ENGINE: %s\n", env_child_indent, engine
            env_done=1
          }
          in_panel=0; in_env=0; skip_deps=0
        }
        if (in_panel) {
          if (skip_deps) {
            if (NF > 0 && cur <= deps_indent) {
              skip_deps=0
            } else {
              next
            }
          }
          if (/^[[:space:]]+depends_on:[[:space:]]*$/) {
            skip_deps=1; deps_indent=cur; next
          }
          if (/^[[:space:]]+environment:[[:space:]]*$/) {
            in_env=1; env_indent=cur; env_list=0; env_child_indent="            "
            print; next
          }
          if (in_env) {
            if (NF > 0 && cur <= env_indent) {
              if (!env_done) {
                if (env_list) printf "%s- ND_PROXY_ENGINE=%s\n", env_child_indent, engine
                else printf "%sND_PROXY_ENGINE: %s\n", env_child_indent, engine
                env_done=1
              }
              in_env=0
            } else {
              if (NF > 0) {
                env_child_indent = substr($0, 1, cur)
                if (ltrim($0) ~ /^- /) env_list=1
              }
              if ($0 ~ /ND_PROXY_ENGINE[=:]/) {
                if (!env_done) {
                  if (env_list) printf "%s- ND_PROXY_ENGINE=%s\n", env_child_indent, engine
                  else printf "%sND_PROXY_ENGINE: %s\n", env_child_indent, engine
                  env_done=1
                }
                next
              }
            }
          }
        }
      }

      if (in_volumes && engine != "caddy" && /^[[:space:]]+(caddy_data|angie_acme|ferron_acme):[[:space:]]*(\{\})?[[:space:]]*$/) {
        if (!vol_replaced) {
          print vol_line
          vol_replaced=1
        }
        next
      }

      print
    }
    END {
      if (in_panel && in_env && !env_done) {
        if (env_list) printf "%s- ND_PROXY_ENGINE=%s\n", env_child_indent, engine
        else printf "%sND_PROXY_ENGINE: %s\n", env_child_indent, engine
      }
    }
  ' "$f" >"$out"
  mv "$out" "$f"
  if [[ -n "$svc_tmp" ]]; then
    rm -f "$svc_tmp"
  fi
}

merge_proxy_shared_volumes_and_ports_from_previous_compose() {
  local old_cf="$1"
  local new_cf="$2"
  [[ -f "$old_cf" && -f "$new_cf" ]] || return 0

  local mounts_tmp ports_tmp blocks_tmp out_tmp
  mounts_tmp=$(mktemp)
  ports_tmp=$(mktemp)
  blocks_tmp=$(mktemp)

  awk -v mf="$mounts_tmp" -v pf="$ports_tmp" '
    function ltrim(s) { sub(/^[[:space:]]+/, "", s); return s }
    function get_indent(s) { return length(s) - length(ltrim(s)) }
    /^services:[[:space:]]*$/ { in_svc=1; in_proxy=0; next }
    in_svc && get_indent($0) == 0 && NF > 0 { in_svc=0; in_proxy=0 }
    in_svc && /^[[:space:]]+(caddy|nd-proxy|proxy):[[:space:]]*$/ {
      in_proxy=1; p_indent=get_indent($0); in_vols=0; in_ports=0; next
    }
    in_proxy && NF > 0 && get_indent($0) <= p_indent { in_proxy=0; in_vols=0; in_ports=0 }
    in_proxy && /^[[:space:]]+volumes:[[:space:]]*$/ { in_vols=1; v_indent=get_indent($0); in_ports=0; next }
    in_proxy && /^[[:space:]]+ports:[[:space:]]*$/ { in_ports=1; pt_indent=get_indent($0); in_vols=0; next }
    in_proxy && in_vols {
      if (NF > 0 && get_indent($0) <= v_indent) { in_vols=0 }
      else if (/^[[:space:]]+- / && (index($0, "nd_shared_") > 0 || index($0, "/data/shared/") > 0)) {
        print > mf
      }
    }
    in_proxy && in_ports {
      if (NF > 0 && get_indent($0) <= pt_indent) { in_ports=0 }
      else if (/^[[:space:]]+- /) {
        p = ltrim($0); sub(/^- /, "", p); gsub(/["'\''[:space:]]/, "", p)
        if (p != "80:80" && p != "443:443/tcp" && p != "443:443/udp" && p != "443:443") {
          print > pf
        }
      }
    }
  ' "$old_cf" || true

  awk '
    function ltrim(s) { sub(/^[[:space:]]+/, "", s); return s }
    function get_indent(s) { return length(s) - length(ltrim(s)) }
    /^volumes:[[:space:]]*$/ { in_v=1; in_nd=0; next }
    in_v && get_indent($0) == 0 && NF > 0 { in_v=0; in_nd=0 }
    in_v && /^[[:space:]]+nd_shared_/ {
      in_nd=1; nd_indent=get_indent($0); print; next
    }
    in_v && in_nd {
      if (NF > 0 && get_indent($0) <= nd_indent) { in_nd=0 }
      else { print; next }
    }
  ' "$old_cf" >"$blocks_tmp" || true

  if [[ -s "$mounts_tmp" ]] && ! grep -q 'nd_shared_' "$new_cf" 2>/dev/null; then
    out_tmp=$(mktemp)
    awk -v mf="$mounts_tmp" '
      BEGIN { while ((getline l < mf) > 0) { n++; m[n] = l }; close(mf) }
      {
        print
        if (!done && /:[[:space:]]*\/data\/workspaces:ro[[:space:]]*$/) {
          for (i = 1; i <= n; i++) print m[i]
          done = 1
        }
      }
    ' "$new_cf" >"$out_tmp"
    mv "$out_tmp" "$new_cf"
  fi

  if [[ -s "$ports_tmp" ]]; then
    out_tmp=$(mktemp)
    awk -v pf="$ports_tmp" '
      BEGIN { while ((getline l < pf) > 0) { n++; p[n] = l }; close(pf) }
      {
        print
        if (!done && /^[[:space:]]*-[[:space:]]+443:443\/udp[[:space:]]*$/) {
          for (i = 1; i <= n; i++) print p[i]
          done = 1
        }
      }
    ' "$new_cf" >"$out_tmp"
    mv "$out_tmp" "$new_cf"
  fi

  if [[ -s "$blocks_tmp" ]] && ! grep -q '^[[:space:]]*nd_shared_' "$new_cf" 2>/dev/null; then
    out_tmp=$(mktemp)
    awk -v bf="$blocks_tmp" '
      BEGIN { while ((getline l < bf) > 0) blk = blk l "\n"; close(bf) }
      /^volumes:[[:space:]]*$/ { v = 1; print; next }
      v && /^[[:space:]]+(caddy_data|angie_acme|ferron_acme):[[:space:]]*(\{\})?[[:space:]]*$/ {
        print
        if (!done && length(blk) > 2) { printf "%s", blk; done = 1 }
        next
      }
      { print }
    ' "$new_cf" >"$out_tmp"
    mv "$out_tmp" "$new_cf"
  fi

  rm -f "$mounts_tmp" "$ports_tmp" "$blocks_tmp"
}

fetch_compose_to() {
  local dst="$1"
  local url="${2:-$COMPOSE_URL}"
  if [[ "$url" == file://* ]]; then
    cp "${url#file://}" "$dst"
  elif command -v curl &>/dev/null; then
    curl -fsSL "$url" -o "$dst"
  elif command -v wget &>/dev/null; then
    wget -qO "$dst" "$url"
  else
    return 1
  fi
}

download_compose() {
  info "Downloading docker-compose.yml..."
  if [[ -d "$INSTALL_DIR/docker-compose.yml" ]]; then
    warn "Removing mistaken directory $INSTALL_DIR/docker-compose.yml (Docker bind-mount artifact)"
    rm -rf "$INSTALL_DIR/docker-compose.yml"
  fi
  local prev_bak="" ferron_tok=""
  if [[ -s "$INSTALL_DIR/docker-compose.yml" ]]; then
    prev_bak=$(mktemp)
    cp "$INSTALL_DIR/docker-compose.yml" "$prev_bak"
    ferron_tok="$(extract_ferron_token_from_compose "$prev_bak" || true)"
  fi
  local tmp_cf
  tmp_cf=$(mktemp)
  if ! fetch_compose_to "$tmp_cf" "$COMPOSE_URL"; then
    rm -f "$prev_bak" "$tmp_cf"
    die "curl or wget required to download compose file."
  fi

  apply_proxy_engine_to_compose "$tmp_cf" "$PROXY_ENGINE" "$ferron_tok"

  if [[ -n "$prev_bak" ]]; then
    if merge_upstream_compose_keep_panel_labels "$prev_bak" "$tmp_cf"; then
      info "Preserved panel labels from existing docker-compose.yml"
    fi
    merge_proxy_shared_volumes_and_ports_from_previous_compose "$prev_bak" "$tmp_cf"
    rm -f "$prev_bak"
  fi
  mv "$tmp_cf" "$INSTALL_DIR/docker-compose.yml"

  patch_data_dir_bind_mounts_in_compose
  patch_nextdeploy_image_in_compose "$PANEL_IMAGE_TAG"
  drop_legacy_panel_stack_env_in_compose
  success "docker-compose.yml written to $INSTALL_DIR (engine: $PROXY_ENGINE)"
}

patch_data_dir_bind_mounts_in_compose() {
  local f="${1:-$INSTALL_DIR/docker-compose.yml}"
  local data_dir="${2:-$DATA_DIR}"
  [[ -f "$f" ]] || return 0
  if [[ "$data_dir" == "/data" ]]; then
    return 0
  fi
  sed -i -E \
    -e "s|^([[:space:]]*-[[:space:]]*)/data/workspaces([^:]*):|\1${data_dir}/workspaces\2:|g" \
    -e "s|^([[:space:]]*-[[:space:]]*)/data/proxy/([^:]+):|\1${data_dir}/proxy/\2:|g" \
    -e "s|^([[:space:]]*-[[:space:]]*)/data:/data\$|\1${data_dir}:/data|g" \
    "$f"
  info "Patched host data bind mounts to $data_dir in $(basename "$f")"
}

# Drop legacy env keys from older compose files. The panel now discovers the
# host compose source and compose project from the running container metadata.
drop_legacy_panel_stack_env_in_compose() {
  local f="${1:-$INSTALL_DIR/docker-compose.yml}"
  [[ -f "$f" ]] || return 0
  sed -i '/^[[:space:]]*PANEL_HOST_INSTALL_DIR:/d' "$f" 2>/dev/null || true
  sed -i '/^[[:space:]]*PANEL_STACK_COMPOSE_PROJECT:/d' "$f" 2>/dev/null || true
  sed -i '/^[[:space:]]*PANEL_STACK_COMPOSE_FILE:/d' "$f" 2>/dev/null || true
  info "Removed legacy panel stack compose envs"
}

# Ensure compose panel image uses :latest (matches docker-main.yml policy on main).
patch_nextdeploy_image_in_compose() {
  local tag="$1"
  local f="${2:-$INSTALL_DIR/docker-compose.yml}"
  [[ -f "$f" ]] || return 0
  if grep -qE 'masudranaxpert/nextdeploy' "$f" 2>/dev/null; then
    sed -i -E "s|masudranaxpert/nextdeploy(-v2)?:[a-zA-Z0-9._-]+|masudranaxpert/nextdeploy:${tag}|g" "$f"
  fi
}

# Extract the panel service "labels:" block (Caddy / panel domain) from an existing compose file.
extract_panel_labels_block() {
  local f="$1"
  [[ -f "$f" ]] || return 0
  awk '
    function ltrim(s) { sub(/^[[:space:]]+/, "", s); return s }
    function get_indent(s) { return length(s) - length(ltrim(s)) }
    /^[[:space:]]+panel:[[:space:]]*$/ {
      in_panel=1; panel_indent=get_indent($0); in_labels=0; labels_indent=-1; next
    }
    in_panel {
      cur=get_indent($0)
      if (NF > 0 && cur <= panel_indent) { in_panel=0; in_labels=0; next }
      if (!in_labels && /^[[:space:]]+labels:[[:space:]]*$/) {
        in_labels=1; labels_indent=cur; print; next
      }
      if (in_labels) {
        if (NF > 0 && cur <= labels_indent && !/^[[:space:]]+labels:/) {
          in_labels=0; next
        }
        print
      }
    }
  ' "$f"
}

# After downloading upstream compose to new_cf, re-apply labels from old_cf (in-place).
merge_upstream_compose_keep_panel_labels() {
  local old_cf="$1"
  local new_cf="$2"
  local labels_tmp out
  labels_tmp=$(mktemp)
  extract_panel_labels_block "$old_cf" >"$labels_tmp" || true
  if [[ ! -s "$labels_tmp" ]]; then
    rm -f "$labels_tmp"
    return 1
  fi
  out=$(mktemp)
  awk -v lf="$labels_tmp" '
    function ltrim(s) { sub(/^[[:space:]]+/, "", s); return s }
    function get_indent(s) { return length(s) - length(ltrim(s)) }
    BEGIN {
      while ((getline line < lf) > 0) lbl = lbl line "\n"
      close(lf); sub(/\n$/, "", lbl); in_panel=0
    }
    {
      if (/^[[:space:]]+panel:[[:space:]]*$/) {
        in_panel=1; panel_indent=get_indent($0); print; next
      }
      if (in_panel && NF > 0 && get_indent($0) <= panel_indent) { in_panel=0 }
      print
      if (in_panel && /^[[:space:]]+image:/) { print lbl }
    }
  ' "$new_cf" >"$out"
  mv "$out" "$new_cf"
  rm -f "$labels_tmp"
  return 0
}

pull_images() {
  info "Pulling Docker images (this may take a minute)..."
  cd "$INSTALL_DIR"
  docker compose pull
  success "Images pulled"
}

start_services() {
  info "Starting NextDeploy services..."
  cd "$INSTALL_DIR"
  # Images were pulled; omit --build so production uses registry tags (not a local Dockerfile build).
  docker compose up -d
  success "Services started"
}

create_systemd_service() {
  info "Creating systemd service for auto-start..."
  cat > /etc/systemd/system/nextdeploy.service <<EOF
[Unit]
Description=NextDeploy Panel
Requires=docker.service
After=docker.service network-online.target
StartLimitIntervalSec=0

[Service]
Type=oneshot
RemainAfterExit=yes
WorkingDirectory=${INSTALL_DIR}
ExecStart=/usr/bin/docker compose up -d --remove-orphans
ExecStop=/usr/bin/docker compose down
TimeoutStartSec=120
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF

  systemctl daemon-reload
  systemctl enable nextdeploy.service
  success "Systemd service created and enabled (nextdeploy.service)"
}

create_update_script() {
  local target="${1:-/usr/local/bin/nextdeploy-update}"
  mkdir -p "$(dirname "$target")"
  cat > "$target" <<EOF
#!/usr/bin/env bash
set -euo pipefail
INSTALL_DIR="${INSTALL_DIR}"
DATA_DIR="${DATA_DIR}"
COMPOSE_URL="${COMPOSE_URL}"
INSTALL_SCRIPT_URL="${INSTALL_SCRIPT_URL}"
PANEL_IMAGE_TAG="${PANEL_IMAGE_TAG}"
ANGIE_IMAGE="${ANGIE_IMAGE}"
FERRON_IMAGE="${FERRON_IMAGE}"
EOF
  cat >> "$target" <<'UPDATE_EOF'

generate_random_token() {
  if command -v openssl &>/dev/null; then
    openssl rand -hex 32
  elif [[ -r /dev/urandom ]]; then
    head -c 32 /dev/urandom | od -An -tx1 | tr -d ' \n'
  else
    printf '%s-%s' "$(date +%s)" "$$"
  fi
}

detect_proxy_engine_in_compose() {
  local f="$1"
  [[ -s "$f" ]] || { echo "caddy"; return 0; }
  awk '
    function ltrim(s) { sub(/^[[:space:]]+/, "", s); return s }
    function get_indent(s) { return length(s) - length(ltrim(s)) }
    /^services:[[:space:]]*$/ { in_svc=1; svc=""; next }
    in_svc && /^[a-zA-Z0-9_-]+:/ && get_indent($0) == 0 { in_svc=0 }
    in_svc && /^[[:space:]]+(caddy|nd-proxy|proxy):[[:space:]]*$/ {
      svc=ltrim($0); sub(/:[[:space:]]*$/, "", svc)
      svc_indent=get_indent($0); next
    }
    in_svc && svc != "" && NF > 0 && get_indent($0) <= svc_indent { svc="" }
    in_svc && svc != "" && /^[[:space:]]+image:[[:space:]]*/ {
      line=$0; sub(/^[[:space:]]+image:[[:space:]]*/, "", line)
      if (line ~ /ferron/) { found="ferron" }
      else if (line ~ /angie/) { found="angie" }
      else if (line ~ /caddy/) { found="caddy" }
    }
    /ND_PROXY_ENGINE/ {
      val=$0; sub(/^.*ND_PROXY_ENGINE[=:][[:space:]]*/, "", val)
      gsub(/["'\''[:space:]]/, "", val)
      if (val == "angie" || val == "ferron" || val == "caddy") env_engine=val
    }
    END {
      if (found != "") print found
      else if (env_engine != "") print env_engine
      else print "caddy"
    }
  ' "$f"
}

extract_ferron_token_from_compose() {
  local f="$1"
  [[ -s "$f" ]] || return 0
  awk '
    /ND_FERRON_ADMIN_TOKEN/ {
      val=$0
      sub(/^.*ND_FERRON_ADMIN_TOKEN[=:][[:space:]]*/, "", val)
      gsub(/^["'\'']|["'\''[:space:]]+$/, "", val)
      if (val != "") { print val; exit }
    }
  ' "$f"
}

emit_nd_proxy_service_yaml() {
  local engine="$1"
  local ferron_token="${2:-}"
  if [[ "$engine" == "angie" ]]; then
    cat <<EOF
    nd-proxy:
        command:
            - sh
            - -c
            - while [ ! -f /etc/angie/nd/sites/angie.conf ]; do sleep 0.2; done; exec angie -c /etc/angie/nd/sites/angie.conf -g 'daemon off;'
        container_name: nd-proxy
        image: ${ANGIE_IMAGE}
        networks:
            - nextdeploy
        ports:
            - 80:80
            - 443:443/tcp
            - 443:443/udp
        restart: unless-stopped
        ulimits:
            nofile:
                soft: 1048576
                hard: 1048576
        volumes:
            - /var/run/docker.sock:/var/run/docker.sock
            - /data/proxy/angie:/etc/angie/nd:ro
            - /data/proxy/pki/sites:/etc/nd-pki/sites:ro
            - /data/proxy/pki/imported:/etc/nd-pki/imported:ro
            - angie_acme:/var/lib/angie/acme
            - /data/workspaces:/data/workspaces:ro
            - /data/workspaces/.nd-php:/data/workspaces/.nd-php
EOF
  elif [[ "$engine" == "ferron" ]]; then
    [[ -n "$ferron_token" ]] || ferron_token="$(generate_random_token)"
    cat <<EOF
    nd-proxy:
        command:
            - sh
            - -c
            - while [ ! -f /etc/ferron/nd/sites/ferron.conf ]; do sleep 0.2; done; exec /usr/local/bin/ferron run -c /etc/ferron/nd/sites/ferron.conf --config-adapter ferronconf
        container_name: nd-proxy
        environment:
            ND_FERRON_ADMIN_TOKEN: "${ferron_token}"
        image: ${FERRON_IMAGE}
        networks:
            - nextdeploy
        ports:
            - 80:80
            - 443:443/tcp
            - 443:443/udp
        restart: unless-stopped
        user: "0:0"
        volumes:
            - /data/proxy/ferron:/etc/ferron/nd:ro
            - /data/proxy/pki/sites:/etc/nd-pki/sites:ro
            - /data/proxy/pki/imported:/etc/nd-pki/imported:ro
            - ferron_acme:/var/cache/ferron-acme
            - /data/workspaces:/data/workspaces:ro
            - /data/workspaces/.nd-php:/data/workspaces/.nd-php
EOF
  fi
}

apply_proxy_engine_to_compose() {
  local f="$1"
  local engine="$2"
  local ferron_token="${3:-}"
  [[ -f "$f" ]] || return 0

  local svc_tmp="" vol_line="    caddy_data: {}"
  if [[ "$engine" == "angie" || "$engine" == "ferron" ]]; then
    svc_tmp=$(mktemp)
    emit_nd_proxy_service_yaml "$engine" "$ferron_token" >"$svc_tmp"
    if [[ "$engine" == "angie" ]]; then
      vol_line="    angie_acme: {}"
    else
      vol_line="    ferron_acme: {}"
    fi
  fi

  local out
  out=$(mktemp)
  awk -v engine="$engine" -v sf="$svc_tmp" -v vol_line="$vol_line" '
    function ltrim(s) { sub(/^[[:space:]]+/, "", s); return s }
    function get_indent(s) { return length(s) - length(ltrim(s)) }
    BEGIN {
      if (sf != "") {
        while ((getline line < sf) > 0) nd_svc = nd_svc line "\n"
        close(sf)
        sub(/\n$/, "", nd_svc)
      }
      in_services=0; in_volumes=0; skip_proxy=0; skip_deps=0
      in_panel=0; in_env=0; env_done=0; svc_replaced=0
    }
    {
      cur = get_indent($0)
      if (cur == 0 && NF > 0) {
        if (in_panel && in_env && !env_done) {
          if (env_list) printf "%s- ND_PROXY_ENGINE=%s\n", env_child_indent, engine
          else printf "%sND_PROXY_ENGINE: %s\n", env_child_indent, engine
          env_done=1
        }
        skip_proxy=0; skip_deps=0; in_panel=0; in_env=0
        in_services = ($0 ~ /^services:[[:space:]]*$/)
        in_volumes  = ($0 ~ /^volumes:[[:space:]]*$/)
        print; next
      }

      if (in_services) {
        if (skip_proxy) {
          if (NF > 0 && cur <= proxy_indent) {
            skip_proxy=0
          } else {
            next
          }
        }
        if (engine != "caddy" && /^[[:space:]]+(caddy|nd-proxy|proxy):[[:space:]]*$/) {
          skip_proxy=1; proxy_indent=cur
          if (!svc_replaced && nd_svc != "") {
            print nd_svc
            svc_replaced=1
          }
          next
        }
        if (/^[[:space:]]+panel:[[:space:]]*$/) {
          in_panel=1; panel_indent=cur; in_env=0; env_done=0; skip_deps=0
          print; next
        }
        if (in_panel && NF > 0 && cur <= panel_indent) {
          if (in_env && !env_done) {
            if (env_list) printf "%s- ND_PROXY_ENGINE=%s\n", env_child_indent, engine
            else printf "%sND_PROXY_ENGINE: %s\n", env_child_indent, engine
            env_done=1
          }
          in_panel=0; in_env=0; skip_deps=0
        }
        if (in_panel) {
          if (skip_deps) {
            if (NF > 0 && cur <= deps_indent) {
              skip_deps=0
            } else {
              next
            }
          }
          if (/^[[:space:]]+depends_on:[[:space:]]*$/) {
            skip_deps=1; deps_indent=cur; next
          }
          if (/^[[:space:]]+environment:[[:space:]]*$/) {
            in_env=1; env_indent=cur; env_list=0; env_child_indent="            "
            print; next
          }
          if (in_env) {
            if (NF > 0 && cur <= env_indent) {
              if (!env_done) {
                if (env_list) printf "%s- ND_PROXY_ENGINE=%s\n", env_child_indent, engine
                else printf "%sND_PROXY_ENGINE: %s\n", env_child_indent, engine
                env_done=1
              }
              in_env=0
            } else {
              if (NF > 0) {
                env_child_indent = substr($0, 1, cur)
                if (ltrim($0) ~ /^- /) env_list=1
              }
              if ($0 ~ /ND_PROXY_ENGINE[=:]/) {
                if (!env_done) {
                  if (env_list) printf "%s- ND_PROXY_ENGINE=%s\n", env_child_indent, engine
                  else printf "%sND_PROXY_ENGINE: %s\n", env_child_indent, engine
                  env_done=1
                }
                next
              }
            }
          }
        }
      }

      if (in_volumes && engine != "caddy" && /^[[:space:]]+(caddy_data|angie_acme|ferron_acme):[[:space:]]*(\{\})?[[:space:]]*$/) {
        if (!vol_replaced) {
          print vol_line
          vol_replaced=1
        }
        next
      }

      print
    }
    END {
      if (in_panel && in_env && !env_done) {
        if (env_list) printf "%s- ND_PROXY_ENGINE=%s\n", env_child_indent, engine
        else printf "%sND_PROXY_ENGINE: %s\n", env_child_indent, engine
      }
    }
  ' "$f" >"$out"
  mv "$out" "$f"
  if [[ -n "$svc_tmp" ]]; then
    rm -f "$svc_tmp"
  fi
}

extract_panel_labels_block() {
  local f="$1"
  [[ -f "$f" ]] || return 0
  awk '
    function ltrim(s) { sub(/^[[:space:]]+/, "", s); return s }
    function get_indent(s) { return length(s) - length(ltrim(s)) }
    /^[[:space:]]+panel:[[:space:]]*$/ {
      in_panel=1; panel_indent=get_indent($0); in_labels=0; labels_indent=-1; next
    }
    in_panel {
      cur=get_indent($0)
      if (NF > 0 && cur <= panel_indent) { in_panel=0; in_labels=0; next }
      if (!in_labels && /^[[:space:]]+labels:[[:space:]]*$/) {
        in_labels=1; labels_indent=cur; print; next
      }
      if (in_labels) {
        if (NF > 0 && cur <= labels_indent && !/^[[:space:]]+labels:/) {
          in_labels=0; next
        }
        print
      }
    }
  ' "$f"
}

merge_upstream_compose_keep_panel_labels() {
  local old_cf="$1"
  local new_cf="$2"
  local labels_tmp out
  labels_tmp=$(mktemp)
  extract_panel_labels_block "$old_cf" >"$labels_tmp" || true
  if [[ ! -s "$labels_tmp" ]]; then
    rm -f "$labels_tmp"
    return 1
  fi
  out=$(mktemp)
  awk -v lf="$labels_tmp" '
    function ltrim(s) { sub(/^[[:space:]]+/, "", s); return s }
    function get_indent(s) { return length(s) - length(ltrim(s)) }
    BEGIN {
      while ((getline line < lf) > 0) lbl = lbl line "\n"
      close(lf); sub(/\n$/, "", lbl); in_panel=0
    }
    {
      if (/^[[:space:]]+panel:[[:space:]]*$/) {
        in_panel=1; panel_indent=get_indent($0); print; next
      }
      if (in_panel && NF > 0 && get_indent($0) <= panel_indent) { in_panel=0 }
      print
      if (in_panel && /^[[:space:]]+image:/) { print lbl }
    }
  ' "$new_cf" >"$out"
  mv "$out" "$new_cf"
  rm -f "$labels_tmp"
  return 0
}

merge_proxy_shared_volumes_and_ports_from_previous_compose() {
  local old_cf="$1"
  local new_cf="$2"
  [[ -f "$old_cf" && -f "$new_cf" ]] || return 0

  local mounts_tmp ports_tmp blocks_tmp out_tmp
  mounts_tmp=$(mktemp)
  ports_tmp=$(mktemp)
  blocks_tmp=$(mktemp)

  awk -v mf="$mounts_tmp" -v pf="$ports_tmp" '
    function ltrim(s) { sub(/^[[:space:]]+/, "", s); return s }
    function get_indent(s) { return length(s) - length(ltrim(s)) }
    /^services:[[:space:]]*$/ { in_svc=1; in_proxy=0; next }
    in_svc && get_indent($0) == 0 && NF > 0 { in_svc=0; in_proxy=0 }
    in_svc && /^[[:space:]]+(caddy|nd-proxy|proxy):[[:space:]]*$/ {
      in_proxy=1; p_indent=get_indent($0); in_vols=0; in_ports=0; next
    }
    in_proxy && NF > 0 && get_indent($0) <= p_indent { in_proxy=0; in_vols=0; in_ports=0 }
    in_proxy && /^[[:space:]]+volumes:[[:space:]]*$/ { in_vols=1; v_indent=get_indent($0); in_ports=0; next }
    in_proxy && /^[[:space:]]+ports:[[:space:]]*$/ { in_ports=1; pt_indent=get_indent($0); in_vols=0; next }
    in_proxy && in_vols {
      if (NF > 0 && get_indent($0) <= v_indent) { in_vols=0 }
      else if (/^[[:space:]]+- / && (index($0, "nd_shared_") > 0 || index($0, "/data/shared/") > 0)) {
        print > mf
      }
    }
    in_proxy && in_ports {
      if (NF > 0 && get_indent($0) <= pt_indent) { in_ports=0 }
      else if (/^[[:space:]]+- /) {
        p = ltrim($0); sub(/^- /, "", p); gsub(/["'\''[:space:]]/, "", p)
        if (p != "80:80" && p != "443:443/tcp" && p != "443:443/udp" && p != "443:443") {
          print > pf
        }
      }
    }
  ' "$old_cf" || true

  awk '
    function ltrim(s) { sub(/^[[:space:]]+/, "", s); return s }
    function get_indent(s) { return length(s) - length(ltrim(s)) }
    /^volumes:[[:space:]]*$/ { in_v=1; in_nd=0; next }
    in_v && get_indent($0) == 0 && NF > 0 { in_v=0; in_nd=0 }
    in_v && /^[[:space:]]+nd_shared_/ {
      in_nd=1; nd_indent=get_indent($0); print; next
    }
    in_v && in_nd {
      if (NF > 0 && get_indent($0) <= nd_indent) { in_nd=0 }
      else { print; next }
    }
  ' "$old_cf" >"$blocks_tmp" || true

  if [[ -s "$mounts_tmp" ]] && ! grep -q 'nd_shared_' "$new_cf" 2>/dev/null; then
    out_tmp=$(mktemp)
    awk -v mf="$mounts_tmp" '
      BEGIN { while ((getline l < mf) > 0) { n++; m[n] = l }; close(mf) }
      {
        print
        if (!done && /:[[:space:]]*\/data\/workspaces:ro[[:space:]]*$/) {
          for (i = 1; i <= n; i++) print m[i]
          done = 1
        }
      }
    ' "$new_cf" >"$out_tmp"
    mv "$out_tmp" "$new_cf"
  fi

  if [[ -s "$ports_tmp" ]]; then
    out_tmp=$(mktemp)
    awk -v pf="$ports_tmp" '
      BEGIN { while ((getline l < pf) > 0) { n++; p[n] = l }; close(pf) }
      {
        print
        if (!done && /^[[:space:]]*-[[:space:]]+443:443\/udp[[:space:]]*$/) {
          for (i = 1; i <= n; i++) print p[i]
          done = 1
        }
      }
    ' "$new_cf" >"$out_tmp"
    mv "$out_tmp" "$new_cf"
  fi

  if [[ -s "$blocks_tmp" ]] && ! grep -q '^[[:space:]]*nd_shared_' "$new_cf" 2>/dev/null; then
    out_tmp=$(mktemp)
    awk -v bf="$blocks_tmp" '
      BEGIN { while ((getline l < bf) > 0) blk = blk l "\n"; close(bf) }
      /^volumes:[[:space:]]*$/ { v = 1; print; next }
      v && /^[[:space:]]+(caddy_data|angie_acme|ferron_acme):[[:space:]]*(\{\})?[[:space:]]*$/ {
        print
        if (!done && length(blk) > 2) { printf "%s", blk; done = 1 }
        next
      }
      { print }
    ' "$new_cf" >"$out_tmp"
    mv "$out_tmp" "$new_cf"
  fi

  rm -f "$mounts_tmp" "$ports_tmp" "$blocks_tmp"
}

self_update_script() {
  [[ "${ND_SELF_UPDATED:-0}" == "1" ]] && return 0
  [[ "${ND_SKIP_SELF_UPDATE:-0}" == "1" ]] && return 0
  if [[ "$COMPOSE_URL" == file://* && "$INSTALL_SCRIPT_URL" != file://* ]]; then
    return 0
  fi

  local self_path="${BASH_SOURCE[0]:-$0}"
  [[ -n "$self_path" && -f "$self_path" ]] || return 0

  local self_dir self_base
  self_dir="$(cd "$(dirname "$self_path")" 2>/dev/null && pwd)" || return 0
  self_base="$(basename "$self_path")"
  self_path="${self_dir}/${self_base}"

  local tmp_install tmp_new
  tmp_install="$(mktemp)"
  tmp_new="$(mktemp)"

  local fetched=0
  if [[ "$INSTALL_SCRIPT_URL" == file://* ]]; then
    if cp "${INSTALL_SCRIPT_URL#file://}" "$tmp_install" 2>/dev/null; then
      fetched=1
    fi
  elif command -v curl &>/dev/null; then
    if curl -fsSL --max-time 15 "$INSTALL_SCRIPT_URL" -o "$tmp_install" 2>/dev/null; then
      fetched=1
    fi
  elif command -v wget &>/dev/null; then
    if wget -qO "$tmp_install" --timeout=15 "$INSTALL_SCRIPT_URL" 2>/dev/null; then
      fetched=1
    fi
  fi

  if [[ "$fetched" != "1" ]] || [[ ! -s "$tmp_install" ]] || ! bash -n "$tmp_install" 2>/dev/null; then
    rm -f "$tmp_install" "$tmp_new"
    return 0
  fi

  if ! (
    ND_SOURCE_ONLY=1
    _orig_install_dir="$INSTALL_DIR"
    _orig_data_dir="$DATA_DIR"
    _orig_compose_url="$COMPOSE_URL"
    _orig_install_url="$INSTALL_SCRIPT_URL"
    _orig_panel_tag="$PANEL_IMAGE_TAG"
    # shellcheck disable=SC1090
    source "$tmp_install"
    declare -F create_update_script >/dev/null 2>&1 || exit 1
    INSTALL_DIR="$_orig_install_dir"
    DATA_DIR="$_orig_data_dir"
    COMPOSE_URL="$_orig_compose_url"
    INSTALL_SCRIPT_URL="$_orig_install_url"
    PANEL_IMAGE_TAG="$_orig_panel_tag"
    create_update_script "$tmp_new" >/dev/null
  ) 2>/dev/null; then
    rm -f "$tmp_install" "$tmp_new"
    return 0
  fi

  rm -f "$tmp_install"
  if [[ ! -s "$tmp_new" ]] || ! bash -n "$tmp_new" 2>/dev/null || ! grep -q 'self_update_script' "$tmp_new" 2>/dev/null; then
    rm -f "$tmp_new"
    return 0
  fi

  if cmp -s "$self_path" "$tmp_new" 2>/dev/null; then
    rm -f "$tmp_new"
    return 0
  fi

  local stage_file="${self_path}.new.$$"
  if cp "$tmp_new" "$stage_file" 2>/dev/null && chmod +x "$stage_file" 2>/dev/null && mv -f "$stage_file" "$self_path" 2>/dev/null; then
    rm -f "$tmp_new"
    echo "[NextDeploy] Updated nextdeploy-update script to latest version; re-running..."
    export ND_SELF_UPDATED=1
    exec "$self_path" "$@"
  fi

  echo "[NextDeploy] WARN: New nextdeploy-update version available, but could not update ${self_path} (run with sudo)." >&2
  rm -f "$stage_file" "$tmp_new" 2>/dev/null || true
  return 0
}

self_update_script "$@"

echo "[NextDeploy] Refreshing docker-compose.yml from repository..."
PREV_BAK=""
ACTIVE_ENGINE="caddy"
FERRON_TOK=""
if [[ -f "$INSTALL_DIR/docker-compose.yml" && ! -d "$INSTALL_DIR/docker-compose.yml" ]]; then
  PREV_BAK=$(mktemp)
  cp "$INSTALL_DIR/docker-compose.yml" "$PREV_BAK"
  ACTIVE_ENGINE="$(detect_proxy_engine_in_compose "$PREV_BAK")"
  FERRON_TOK="$(extract_ferron_token_from_compose "$PREV_BAK" || true)"
fi
if [[ -d "$INSTALL_DIR/docker-compose.yml" ]]; then
  echo "[NextDeploy] Removing invalid docker-compose.yml directory..."
  rm -rf "$INSTALL_DIR/docker-compose.yml"
fi
if [[ "$COMPOSE_URL" == file://* ]]; then
  cp "${COMPOSE_URL#file://}" "$INSTALL_DIR/docker-compose.yml.tmp"
elif command -v curl &>/dev/null; then
  curl -fsSL "$COMPOSE_URL" -o "$INSTALL_DIR/docker-compose.yml.tmp"
elif command -v wget &>/dev/null; then
  wget -qO "$INSTALL_DIR/docker-compose.yml.tmp" "$COMPOSE_URL"
else
  echo "[NextDeploy] WARN: curl/wget missing — keeping existing docker-compose.yml"
  rm -f "$PREV_BAK"
  exit 0
fi

apply_proxy_engine_to_compose "$INSTALL_DIR/docker-compose.yml.tmp" "$ACTIVE_ENGINE" "$FERRON_TOK"

if [[ -n "$PREV_BAK" ]]; then
  if merge_upstream_compose_keep_panel_labels "$PREV_BAK" "$INSTALL_DIR/docker-compose.yml.tmp"; then
    echo "[NextDeploy] Preserved panel labels from previous compose file."
  fi
  merge_proxy_shared_volumes_and_ports_from_previous_compose "$PREV_BAK" "$INSTALL_DIR/docker-compose.yml.tmp" || true
  rm -f "$PREV_BAK"
fi
mv "$INSTALL_DIR/docker-compose.yml.tmp" "$INSTALL_DIR/docker-compose.yml"
if [[ "$DATA_DIR" != "/data" ]] && [[ -f "$INSTALL_DIR/docker-compose.yml" ]]; then
  sed -i -E \
    -e "s|^([[:space:]]*-[[:space:]]*)/data/workspaces([^:]*):|\1${DATA_DIR}/workspaces\2:|g" \
    -e "s|^([[:space:]]*-[[:space:]]*)/data/proxy/([^:]+):|\1${DATA_DIR}/proxy/\2:|g" \
    -e "s|^([[:space:]]*-[[:space:]]*)/data:/data\$|\1${DATA_DIR}:/data|g" \
    "$INSTALL_DIR/docker-compose.yml"
fi
if [[ -f "$INSTALL_DIR/docker-compose.yml" ]]; then
  sed -i '/^[[:space:]]*PANEL_HOST_INSTALL_DIR:/d' "$INSTALL_DIR/docker-compose.yml" 2>/dev/null || true
  sed -i '/^[[:space:]]*PANEL_STACK_COMPOSE_PROJECT:/d' "$INSTALL_DIR/docker-compose.yml" 2>/dev/null || true
  sed -i '/^[[:space:]]*PANEL_STACK_COMPOSE_FILE:/d' "$INSTALL_DIR/docker-compose.yml" 2>/dev/null || true
fi
if grep -qE 'masudranaxpert/nextdeploy' "$INSTALL_DIR/docker-compose.yml" 2>/dev/null; then
  sed -i -E "s|masudranaxpert/nextdeploy(-v2)?:[a-zA-Z0-9._-]+|masudranaxpert/nextdeploy:${PANEL_IMAGE_TAG}|g" "$INSTALL_DIR/docker-compose.yml"
fi
echo "[NextDeploy] Active proxy engine preserved: ${ACTIVE_ENGINE}"
if [[ "${ND_SKIP_DOCKER_UP:-0}" != "1" ]]; then
  echo "[NextDeploy] Pulling images (tag: ${PANEL_IMAGE_TAG})..."
  cd "$INSTALL_DIR"
  docker compose pull
  docker compose up -d --remove-orphans
  docker image prune -f
fi
echo "[NextDeploy] Update complete!"
UPDATE_EOF
  chmod +x "$target"
  success "Update script created: $target"
}

create_logs_script() {
  cat > /usr/local/bin/nextdeploy-logs <<EOF
#!/usr/bin/env bash
cd "${INSTALL_DIR}"
docker compose logs -f --tail=100 "\$@"
EOF
  chmod +x /usr/local/bin/nextdeploy-logs
  success "Log helper created: nextdeploy-logs"
}

wait_for_panel() {
  info "Waiting for panel to become ready..."
  local retries=30
  local i=0
  while [[ $i -lt $retries ]]; do
    if curl -sf http://localhost:8080/login &>/dev/null; then
      success "Panel is up!"
      return 0
    fi
    sleep 2
    i=$((i+1))
    echo -n "."
  done
  echo ""
  warn "Panel did not respond within 60s. Check logs: nextdeploy-logs"
}

get_public_ip() {
  local ip=""
  if command -v curl &>/dev/null; then
    ip=$(curl -4 -fsSL --max-time 3 https://api.ipify.org 2>/dev/null || \
         curl -4 -fsSL --max-time 3 https://ifconfig.io 2>/dev/null || \
         curl -4 -fsSL --max-time 3 https://icanhazip.com 2>/dev/null)
  elif command -v wget &>/dev/null; then
    ip=$(wget -qO- --timeout=3 https://api.ipify.org 2>/dev/null || \
         wget -qO- --timeout=3 https://ifconfig.io 2>/dev/null)
  fi

  if [[ "$ip" =~ ^[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}$ ]]; then
    echo "$ip"
  fi
}

get_private_ip() {
  ip route get 1.1.1.1 2>/dev/null | awk '{print $7; exit}' || \
  hostname -I 2>/dev/null | awk '{print $1}' || \
  echo ""
}

print_summary() {
  local pub_ip priv_ip display_ip
  pub_ip=$(get_public_ip)
  priv_ip=$(get_private_ip)
  display_ip="${pub_ip:-$priv_ip}"
  [[ -z "$display_ip" ]] && display_ip="YOUR_SERVER_IP"

  echo ""
  echo -e "${BOLD}${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${RESET}"
  echo -e "${BOLD}${GREEN}  NextDeploy installed successfully${RESET}"
  echo -e "${BOLD}${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${RESET}"
  echo ""
  echo -e "  ${BOLD}Panel URL:${RESET}      http://${display_ip}:8080"
  if [[ -n "$pub_ip" && -n "$priv_ip" && "$pub_ip" != "$priv_ip" ]]; then
    echo -e "  ${BOLD}Internal IP:${RESET}    http://${priv_ip}:8080 (LAN / local network)"
  fi
  if [[ -n "$PANEL_DOMAIN" ]]; then
    echo -e "  ${BOLD}Custom domain:${RESET}  https://${PANEL_DOMAIN}  (after DNS + panel settings)"
  fi
  echo ""
  echo -e "  ${BOLD}Install dir:${RESET}    $INSTALL_DIR"
  echo -e "  ${BOLD}Data dir:${RESET}       $DATA_DIR"
  echo -e "  ${BOLD}Proxy engine:${RESET}   $PROXY_ENGINE"
  echo ""
  echo -e "  ${BOLD}Useful commands:${RESET}"
  echo -e "    ${CYAN}nextdeploy-update${RESET}          Pull images and restart stack"
  echo -e "    ${CYAN}nextdeploy-logs${RESET}            Follow live logs"
  echo -e "    ${CYAN}systemctl status nextdeploy${RESET}  Service status"
  echo -e "    ${CYAN}cd $INSTALL_DIR && docker compose down${RESET}  Stop services"
  echo ""
  echo -e "  ${YELLOW}First run:${RESET} Open the URL above to create your admin account."
  echo ""
}

# ── Main ──────────────────────────────────────────────────────────────────────
main() {
  banner
  parse_args "$@"

  echo -e "${BOLD}Checking requirements...${RESET}"
  check_root
  check_os
  check_arch
  check_memory
  check_ports
  check_docker
  check_docker_compose
  tune_system
  choose_proxy_engine

  echo ""
  echo -e "${BOLD}Installing NextDeploy...${RESET}"
  create_directories
  seed_proxy_bootstrap "$PROXY_ENGINE" "$DATA_DIR"
  download_compose
  pull_images
  start_services

  if command -v systemctl &>/dev/null; then
    create_systemd_service
  else
    warn "systemd not found — skipping auto-start service."
  fi

  create_update_script
  create_logs_script
  wait_for_panel
  print_summary
}

if [[ "${ND_SOURCE_ONLY:-0}" != "1" ]]; then
  main "$@"
fi
