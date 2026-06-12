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
WORKSPACES_DIR="${DATA_DIR}/workspaces"
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
  echo "  ╚██████╔╝██║ ╚████║██║██║ ╚████║███████╗   ██║   ██║  ██║███████╗███████╗"
  echo "   ╚═════╝ ╚═╝  ╚═══╝╚═╝╚═╝  ╚═══╝╚══════╝   ╚═╝   ╚═╝  ╚═╝╚══════╝╚══════╝"
  echo -e "${RESET}"
  echo -e "  ${BOLD}NextDeploy Uninstaller${RESET}"
  echo ""
}

# ── Argument parsing ──────────────────────────────────────────────────────────
parse_args() {
  WORKSPACES_DIR="${DATA_DIR}/workspaces"
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --keep-data)  KEEP_DATA=true; shift ;;
      --force|-f)   FORCE=true; shift ;;
      --dir)        INSTALL_DIR="$2"; shift 2 ;;
      --data-dir)   DATA_DIR="$2"; WORKSPACES_DIR="${DATA_DIR}/workspaces"; shift 2 ;;
      --help|-h)
        echo "Usage: uninstall.sh [options]"
        echo ""
        echo "Options:"
        echo "  --keep-data    Stop apps but keep ${DATA_DIR} (workspaces + volumes on disk)"
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

is_panel_compose_project() {
  case "${1:-}" in
    ""|nextdeploy|NextDeploy) return 0 ;;
  esac
  return 1
}

workspace_path_prefix() {
  local ws
  ws="$(cd "$WORKSPACES_DIR" 2>/dev/null && pwd -P)" || ws="$WORKSPACES_DIR"
  echo "$ws"
}

path_under_workspaces() {
  local p="$1"
  local ws
  [[ -z "$p" ]] && return 1
  ws="$(workspace_path_prefix)"
  [[ "$p" == "$ws" || "$p" == "$ws/"* ]]
}

parse_compose_project_from_env() {
  local envf="$1"
  [[ -f "$envf" ]] || return 1
  local line val
  line="$(grep -E '^[[:space:]]*(export[[:space:]]+)?COMPOSE_PROJECT_NAME=' "$envf" 2>/dev/null | head -1 || true)"
  [[ -z "$line" ]] && return 1
  val="${line#*=}"
  val="$(echo "$val" | sed -E 's/^[[:space:]]*//; s/[[:space:]]*$//; s/^["'\''"]//; s/["'\''"]$//')"
  [[ -n "$val" ]] || return 1
  echo "$val"
}

# Collect compose project names for stacks deployed under DATA_DIR/workspaces.
collect_app_compose_projects() {
  local -A projects=()
  local ws_prefix
  ws_prefix="$(workspace_path_prefix)"

  if command -v docker &>/dev/null; then
    while IFS=$'\t' read -r name configs; do
      name="$(echo "$name" | xargs)"
      configs="$(echo "$configs" | xargs)"
      [[ -z "$name" ]] && continue
      is_panel_compose_project "$name" && continue
      if [[ "$configs" == *"${ws_prefix}"* || "$configs" == *"${WORKSPACES_DIR}"* ]]; then
        projects["$name"]=1
      fi
    done < <(docker compose ls --format '{{.Name}}\t{{.ConfigFiles}}' 2>/dev/null || true)

    local cid proj wd
    while IFS= read -r cid; do
      [[ -z "$cid" ]] && continue
      proj="$(docker inspect --format '{{index .Config.Labels "com.docker.compose.project"}}' "$cid" 2>/dev/null || true)"
      wd="$(docker inspect --format '{{index .Config.Labels "com.docker.compose.project.working_dir"}}' "$cid" 2>/dev/null || true)"
      proj="$(echo "$proj" | xargs)"
      wd="$(echo "$wd" | xargs)"
      [[ -z "$proj" ]] && continue
      is_panel_compose_project "$proj" && continue
      if path_under_workspaces "$wd"; then
        projects["$proj"]=1
      fi
    done < <(docker ps -aq 2>/dev/null || true)
  fi

  if [[ -d "$WORKSPACES_DIR" ]]; then
    local envf proj
    while IFS= read -r -d '' envf; do
      proj="$(parse_compose_project_from_env "$envf" || true)"
      [[ -n "$proj" ]] && projects["$proj"]=1
    done < <(find "$WORKSPACES_DIR" -maxdepth 5 -name '.env' -type f -print0 2>/dev/null || true)
  fi

  local p
  for p in "${!projects[@]}"; do
    echo "$p"
  done | sort -u
}

compose_down_project() {
  local proj="$1"
  local down_args=(down --remove-orphans)
  if [[ "$KEEP_DATA" == false ]]; then
    down_args+=( -v --rmi local )
  fi
  docker compose -p "$proj" "${down_args[@]}" 2>/dev/null || true
}

stop_all_app_stacks() {
  info "Stopping all deployed apps (containers, networks, images)..."
  local projects=()
  mapfile -t projects < <(collect_app_compose_projects)
  if [[ ${#projects[@]} -eq 0 ]]; then
    warn "No app compose projects found under ${WORKSPACES_DIR}"
    return
  fi
  local proj
  for proj in "${projects[@]}"; do
    [[ -z "$proj" ]] && continue
    info "  docker compose -p ${proj} down ..."
    compose_down_project "$proj"
  done
  success "Stopped ${#projects[@]} app stack(s)"
}

remove_leftover_workspace_containers() {
  info "Removing leftover app containers..."
  local removed=0
  local cid wd proj
  local -a known_projects=()
  mapfile -t known_projects < <(collect_app_compose_projects)
  while IFS= read -r cid; do
    [[ -z "$cid" ]] && continue
    wd="$(docker inspect --format '{{index .Config.Labels "com.docker.compose.project.working_dir"}}' "$cid" 2>/dev/null || true)"
    proj="$(docker inspect --format '{{index .Config.Labels "com.docker.compose.project"}}' "$cid" 2>/dev/null || true)"
    proj="$(echo "$proj" | xargs)"
    if path_under_workspaces "$wd"; then
      docker rm -f "$cid" 2>/dev/null && removed=$((removed + 1)) || true
      continue
    fi
    if [[ -n "$proj" ]] && ! is_panel_compose_project "$proj"; then
      local known
      for known in "${known_projects[@]}"; do
        if [[ "$known" == "$proj" ]]; then
          docker rm -f "$cid" 2>/dev/null && removed=$((removed + 1)) || true
          break
        fi
      done
    fi
  done < <(docker ps -aq 2>/dev/null || true)
  if [[ $removed -gt 0 ]]; then
    success "Removed $removed leftover container(s)"
  else
    success "No leftover app containers"
  fi
}

remove_app_docker_artifacts() {
  [[ "$KEEP_DATA" == true ]] && return
  info "Removing app Docker volumes and built images..."
  local proj
  while IFS= read -r proj; do
    [[ -z "$proj" ]] && continue
    docker volume ls -q --filter "label=com.docker.compose.project=${proj}" 2>/dev/null \
      | xargs -r docker volume rm -f 2>/dev/null || true
    docker images -q --filter "label=com.docker.compose.project=${proj}" 2>/dev/null \
      | xargs -r docker rmi -f 2>/dev/null || true
  done < <(collect_app_compose_projects)
  success "App Docker artifacts cleaned"
}

# ── Confirmation ──────────────────────────────────────────────────────────────
confirm() {
  if [[ "$FORCE" == true ]]; then
    return 0
  fi

  echo -e "${BOLD}${RED}WARNING: This will remove NextDeploy from your system.${RESET}"
  echo ""
  echo -e "  Will remove:"
  echo -e "    ${RED}-${RESET}  Panel stack (caddy, panel)"
  echo -e "    ${RED}-${RESET}  ${BOLD}All deployed apps${RESET} (containers, networks"
  if [[ "$KEEP_DATA" == false ]]; then
    echo -e "        and Docker volumes/images for those apps)"
  else
    echo -e "        — app data volumes kept on disk with ${CYAN}--keep-data${RESET})"
  fi
  echo -e "    ${RED}-${RESET}  Docker images (nextdeploy, caddy-docker-proxy)"
  echo -e "    ${RED}-${RESET}  Install directory: ${INSTALL_DIR}"
  echo -e "    ${RED}-${RESET}  Systemd service: nextdeploy.service"
  echo -e "    ${RED}-${RESET}  Helper scripts: nextdeploy-update, nextdeploy-logs"
  if [[ "$KEEP_DATA" == false ]]; then
    echo -e "    ${RED}-${RESET}  ${BOLD}Data directory: ${DATA_DIR} (workspaces, panel DB, backups)${RESET}"
  else
    echo -e "    ${GREEN}+${RESET}  Data directory: ${DATA_DIR} (keeping files on disk)"
  fi
  echo ""

  if [[ "$KEEP_DATA" == false ]]; then
    echo -e "${BOLD}${RED}  Everything under ${DATA_DIR} and all app containers will be deleted.${RESET}"
    echo -e "  To keep files on disk (for reinstall), run: ${CYAN}--keep-data${RESET}"
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
  info "Stopping NextDeploy panel stack..."
  if [[ -f "$INSTALL_DIR/docker-compose.yml" ]]; then
    cd "$INSTALL_DIR"
    docker compose down --remove-orphans 2>/dev/null || true
    success "Panel stack stopped"
  else
    warn "No docker-compose.yml at $INSTALL_DIR — stopping containers by name..."
    docker stop panel caddy 2>/dev/null || true
    docker rm -f panel caddy 2>/dev/null || true
    success "Panel containers stopped"
  fi
}

remove_images() {
  info "Removing NextDeploy Docker images..."
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
    info "Removed leftover panel images"
  fi

  success "Panel images cleaned up"
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
  if [[ "$KEEP_DATA" == false ]]; then
    docker volume prune -f 2>/dev/null || true
  fi
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
  stop_all_app_stacks
  remove_leftover_workspace_containers
  remove_app_docker_artifacts
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
