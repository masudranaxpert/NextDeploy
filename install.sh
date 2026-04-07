#!/usr/bin/env bash
# =============================================================================
#  NextDeploy — Install script (branch php-panel only — PHP Panel line)
#  Docker image :php-panel; compose file from branch php-panel.
#
#  Usage:
#    curl -fsSL https://raw.githubusercontent.com/masudranaxpert/NextDeploy/php-panel/install.sh | sudo bash
#    sudo bash install.sh [--domain panel.example.com] [--email admin@example.com]
#
#  Stable line without PHP Panel: use install.sh from branch main.
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
COMPOSE_URL="https://raw.githubusercontent.com/masudranaxpert/NextDeploy/php-panel/docker-compose.yml"
# Panel image tag (must match docker-php-panel.yml on this branch)
PANEL_IMAGE_TAG="php-panel"
PANEL_DOMAIN=""
CADDY_EMAIL=""
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
  echo -e "  ${BOLD}Self-hosted Docker deployment panel${RESET} — ${BOLD}PHP Panel${RESET}"
  echo -e "  ${CYAN}https://github.com/masudranaxpert/NextDeploy${RESET}"
  echo ""
}

# ── Argument parsing ──────────────────────────────────────────────────────────
parse_args() {
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --domain)   PANEL_DOMAIN="$2"; shift 2 ;;
      --email)    CADDY_EMAIL="$2";  shift 2 ;;
      --dir)      INSTALL_DIR="$2";  shift 2 ;;
      --data-dir) DATA_DIR="$2";     shift 2 ;;
      --help|-h)
        echo "Usage: install.sh [options]"
        echo ""
        echo "Options:"
        echo "  --domain DOMAIN     Panel domain (e.g. panel.example.com)"
        echo "  --email  EMAIL      Let's Encrypt email for HTTPS"
        echo "  --dir    PATH       Install directory (default: /opt/nextdeploy)"
        echo "  --data-dir PATH     Data directory (default: /data)"
        echo "  --help              Show this help"
        exit 0
        ;;
      *) warn "Unknown option: $1"; shift ;;
    esac
  done
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

# ── Install ───────────────────────────────────────────────────────────────────
create_directories() {
  info "Creating directories..."
  mkdir -p "$INSTALL_DIR"
  mkdir -p "$DATA_DIR/workspaces"
  mkdir -p "$DATA_DIR"
  chmod 750 "$DATA_DIR"
  success "Directories created: $INSTALL_DIR, $DATA_DIR"
}

download_compose() {
  info "Downloading docker-compose.yml..."
  if command -v curl &>/dev/null; then
    curl -fsSL "$COMPOSE_URL" -o "$INSTALL_DIR/docker-compose.yml"
  elif command -v wget &>/dev/null; then
    wget -qO "$INSTALL_DIR/docker-compose.yml" "$COMPOSE_URL"
  else
    die "curl or wget required to download compose file."
  fi

  # Patch DATA_DIR into compose if non-default
  if [[ "$DATA_DIR" != "/data" ]]; then
    sed -i "s|/data|${DATA_DIR}|g" "$INSTALL_DIR/docker-compose.yml"
    info "Patched DATA_DIR to $DATA_DIR in docker-compose.yml"
  fi

  patch_nextdeploy_image_in_compose "$PANEL_IMAGE_TAG"
  success "docker-compose.yml downloaded to $INSTALL_DIR"
}

# Ensure compose panel image uses :php-panel (matches docker-php-panel.yml on this branch).
patch_nextdeploy_image_in_compose() {
  local tag="$1"
  local f="$INSTALL_DIR/docker-compose.yml"
  [[ -f "$f" ]] || return 0
  if grep -q 'masudranaxpert/nextdeploy' "$f" 2>/dev/null; then
    sed -i "s|masudranaxpert/nextdeploy:[a-zA-Z0-9._-]*|masudranaxpert/nextdeploy:${tag}|g" "$f"
  fi
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
  docker compose up -d
  success "Services started"
}

create_systemd_service() {
  info "Creating systemd service for auto-start..."
  cat > /etc/systemd/system/nextdeploy.service <<EOF
[Unit]
Description=NextDeploy Panel (PHP Panel)
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
  cat > /usr/local/bin/nextdeploy-update <<EOF
#!/usr/bin/env bash
set -euo pipefail
INSTALL_DIR="${INSTALL_DIR}"
echo "[NextDeploy PHP Panel] Pulling images (tag: ${PANEL_IMAGE_TAG})..."
cd "\$INSTALL_DIR"
docker compose pull
docker compose up -d --remove-orphans
docker image prune -f
echo "[NextDeploy PHP Panel] Update complete!"
EOF
  chmod +x /usr/local/bin/nextdeploy-update
  success "Update script created: nextdeploy-update"
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

get_server_ip() {
  ip route get 1.1.1.1 2>/dev/null | awk '{print $7; exit}' || \
  hostname -I 2>/dev/null | awk '{print $1}' || \
  echo "YOUR_SERVER_IP"
}

print_summary() {
  local ip
  ip=$(get_server_ip)
  echo ""
  echo -e "${BOLD}${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${RESET}"
  echo -e "${BOLD}${GREEN}  NextDeploy (PHP Panel) installed successfully${RESET}"
  echo -e "${BOLD}${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${RESET}"
  echo ""
  echo -e "  ${BOLD}Panel URL:${RESET}      http://${ip}:8080"
  if [[ -n "$PANEL_DOMAIN" ]]; then
    echo -e "  ${BOLD}Custom domain:${RESET}  https://${PANEL_DOMAIN}  (after DNS + panel settings)"
  fi
  echo ""
  echo -e "  ${BOLD}Install dir:${RESET}    $INSTALL_DIR"
  echo -e "  ${BOLD}Data dir:${RESET}       $DATA_DIR"
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

  echo ""
  echo -e "${BOLD}Installing NextDeploy...${RESET}"
  create_directories
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

main "$@"
