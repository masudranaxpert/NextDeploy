#!/usr/bin/env bash
# =============================================================================
#  NextDeploy — Uninstall Script
#  Usage: sudo bash uninstall.sh [--keep-data] [--force]
#  From URL (force, no prompt): curl -fsSL URL | sudo bash -s -- --force
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

# ── Config ────────────────────────────────────────────────────────────────────
INSTALL_DIR="/opt/nextdeploy"
DATA_DIR="/data"
KEEP_DATA=false
FORCE=false

# ── Helpers ────────────────────────────────────────────────────────────────────
info()    { echo -e "${CYAN}[INFO]${RESET}  $*"; }
success() { echo -e "${GREEN}[OK]${RESET}    $*"; }
warn()    { echo -e "${YELLOW}[WARN]${RESET}  $*"; }
error()   { echo -e "${RED}[ERROR]${RESET} $*" >&2; }
die()     { error "$*"; exit 1; }

banner() {
  echo -e "${BOLD}${RED}"
  echo "  ██╗   ██╗███╗   ██╗██╗███╗   ██╗███████╗████████╗ █████╗ ██╗     ██╗     "
  echo "  ██║   ██║████╗  ██║██║████╗  ██║██╔════╝╚══██╔══╝██╔══██╗██║     ██║     "
  echo "  ██║   ██║██╔██╗ ██║██║██╔██╗ ██║███████╗   ██║   ███████║██║     ██║     "
  echo "  ██║   ██║██║╚██╗██║██║██║╚██╗██║╚════██║   ██║   ██╔══██║██║     ██║     "
  echo "  ╚██████╔╝██║ ╚████║██║██║ ╚████║███████║   ██║   ██║  ██║███████╗███████╗"
  echo "   ╚═════╝ ╚═╝  ╚═══╝╚═╝╚═╝  ╚═══╝╚══════╝   ╚═╝   ╚═╝  ╚═╝╚══════╝╚══════╝"
  echo -e "${RESET}"
  echo -e "  ${BOLD}NextDeploy Uninstaller${RESET}"
  echo ""
}

# ── Argument parsing ──────────────────────────────────────────────────────────
parse_args() {
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --keep-data)  KEEP_DATA=true; shift ;;
      --force|-f)   FORCE=true; shift ;;
      --dir)        INSTALL_DIR="$2"; shift 2 ;;
      --data-dir)   DATA_DIR="$2"; shift 2 ;;
      --help|-h)
        echo "Usage: uninstall.sh [options]"
        echo ""
        echo "Options:"
        echo "  --keep-data    Keep $DATA_DIR (apps, databases, workspaces)"
        echo "  --force, -f    Skip confirmation prompt"
        echo "  --dir PATH     Install directory (default: /opt/nextdeploy)"
        echo "  --data-dir PATH Data directory (default: /data)"
        echo "  --help         Show this help"
        exit 0
        ;;
      *) warn "Unknown option: $1"; shift ;;
    esac
  done
}

check_root() {
  if [[ $EUID -ne 0 ]]; then
    die "This script must be run as root. Try: sudo bash uninstall.sh"
  fi
}

# ── Confirmation ──────────────────────────────────────────────────────────────
confirm() {
  if [[ "$FORCE" == true ]]; then
    return 0
  fi

  echo -e "${BOLD}${RED}WARNING: This will remove NextDeploy from your system.${RESET}"
  echo ""
  echo -e "  Will remove:"
  echo -e "    ${RED}-${RESET}  Running containers (caddy, panel)"
  echo -e "    ${RED}-${RESET}  Docker images (nextdeploy, caddy-docker-proxy)"
  echo -e "    ${RED}-${RESET}  Install directory: ${INSTALL_DIR}"
  echo -e "    ${RED}-${RESET}  Systemd service: nextdeploy.service"
  echo -e "    ${RED}-${RESET}  Helper scripts: nextdeploy-update, nextdeploy-logs"
  if [[ "$KEEP_DATA" == false ]]; then
    echo -e "    ${RED}-${RESET}  ${BOLD}Data directory: ${DATA_DIR} (all app data)${RESET}"
  else
    echo -e "    ${GREEN}+${RESET}  Data directory: ${DATA_DIR} (keeping)"
  fi
  echo ""

  if [[ "$KEEP_DATA" == false ]]; then
    echo -e "${BOLD}${RED}  App data, databases, and workspaces under ${DATA_DIR} will be deleted.${RESET}"
    echo -e "  To keep data, run: ${CYAN}--keep-data${RESET}"
    echo ""
  fi

  read -rp "  Type 'yes' to confirm uninstall: " CONFIRM
  if [[ "$CONFIRM" != "yes" ]]; then
    echo "Uninstall cancelled."
    exit 0
  fi
}

# ── Uninstall steps ───────────────────────────────────────────────────────────
stop_services() {
  info "Stopping NextDeploy services..."
  if [[ -f "$INSTALL_DIR/docker-compose.yml" ]]; then
    cd "$INSTALL_DIR"
    docker compose down --remove-orphans 2>/dev/null || true
    success "Docker Compose services stopped"
  else
    warn "No docker-compose.yml at $INSTALL_DIR — stopping containers by name..."
    docker stop panel caddy 2>/dev/null || true
    docker rm -f panel caddy 2>/dev/null || true
  fi
}

remove_images() {
  info "Removing Docker images..."
  if docker image inspect masudranaxpert/nextdeploy:latest &>/dev/null 2>&1; then
    docker rmi masudranaxpert/nextdeploy:latest 2>/dev/null || true
  fi

  for tag in latest stable; do
    docker rmi "masudranaxpert/nextdeploy:${tag}" 2>/dev/null || true
  done

  docker rmi lucaslorentz/caddy-docker-proxy:ci-alpine 2>/dev/null || true

  LEFTOVER=$(docker images --format '{{.Repository}}:{{.Tag}}' | grep -E 'nextdeploy|panel-local' || true)
  if [[ -n "$LEFTOVER" ]]; then
    echo "$LEFTOVER" | xargs -r docker rmi --force 2>/dev/null || true
    info "Removed leftover images"
  fi

  success "Docker images cleaned up"
}

remove_networks() {
  info "Removing NextDeploy Docker network..."
  docker network rm NextDeploy 2>/dev/null || true
  success "Network removed (or did not exist)"
}

remove_volumes() {
  info "Removing Caddy data volume..."
  docker volume rm nextdeploy_caddy_data 2>/dev/null || \
  docker volume rm caddy_data 2>/dev/null || true
  success "Caddy volume removed (or did not exist)"
}

disable_systemd() {
  if command -v systemctl &>/dev/null; then
    info "Removing systemd service..."
    systemctl stop nextdeploy.service 2>/dev/null || true
    systemctl disable nextdeploy.service 2>/dev/null || true
    rm -f /etc/systemd/system/nextdeploy.service
    systemctl daemon-reload
    success "Systemd service removed"
  fi
}

remove_scripts() {
  info "Removing helper scripts..."
  rm -f /usr/local/bin/nextdeploy-update
  rm -f /usr/local/bin/nextdeploy-logs
  success "Helper scripts removed"
}

remove_install_dir() {
  if [[ -d "$INSTALL_DIR" ]]; then
    info "Removing install directory: $INSTALL_DIR"
    rm -rf "$INSTALL_DIR"
    success "Install directory removed"
  else
    info "Install directory not found (already removed?)"
  fi
}

remove_data_dir() {
  if [[ "$KEEP_DATA" == true ]]; then
    warn "Keeping data directory: $DATA_DIR"
    return
  fi

  if [[ -d "$DATA_DIR" ]]; then
    info "Removing data directory: $DATA_DIR ..."
    rm -rf "$DATA_DIR"
    success "Data directory removed"
  else
    info "Data directory not found: $DATA_DIR"
  fi
}

prune_docker() {
  info "Pruning unused Docker resources..."
  docker container prune -f 2>/dev/null || true
  docker network prune -f 2>/dev/null || true
  success "Docker pruned"
}

print_summary() {
  echo ""
  echo -e "${BOLD}${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${RESET}"
  echo -e "${BOLD}${GREEN}  NextDeploy uninstalled${RESET}"
  echo -e "${BOLD}${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${RESET}"
  echo ""
  if [[ "$KEEP_DATA" == true ]]; then
    echo -e "  ${YELLOW}Data preserved at:${RESET} $DATA_DIR"
    echo -e "  Reinstall with the install script; you can point the same data directory again."
  fi
  echo ""
  echo -e "  Reinstall: ${CYAN}curl -fsSL https://raw.githubusercontent.com/masudranaxpert/NextDeploy/main/install.sh | sudo bash${RESET}"
  echo ""
}

# ── Main ──────────────────────────────────────────────────────────────────────
main() {
  banner
  parse_args "$@"
  check_root
  confirm

  echo ""
  echo -e "${BOLD}Uninstalling NextDeploy...${RESET}"
  stop_services
  remove_images
  remove_networks
  remove_volumes
  disable_systemd
  remove_scripts
  remove_install_dir
  remove_data_dir
  prune_docker
  print_summary
}

main "$@"
