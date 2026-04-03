#!/usr/bin/env bash
# =============================================================================
#  Meshploy Uninstall Script
#  Removes the Meshploy stack, data, and mesh configuration from this node.
#
#  Usage:
#    bash uninstall.sh           — interactive (asks before each destructive step)
#    bash uninstall.sh --yes     — non-interactive (skips all confirmations)
#    bash uninstall.sh --reinstall — uninstall then immediately re-run install.sh
# =============================================================================
set -euo pipefail
exec < /dev/tty

RED='\033[0;31m';  GREEN='\033[0;32m'; YELLOW='\033[1;33m'
BLUE='\033[0;34m'; CYAN='\033[0;36m';  BOLD='\033[1m';  RESET='\033[0m'

info()    { echo -e "${CYAN}  →${RESET}  $*"; }
success() { echo -e "${GREEN}  ✔${RESET}  $*"; }
warn()    { echo -e "${YELLOW}  ⚠${RESET}  $*"; }
die()     { echo -e "${RED}  ✘${RESET}  $*" >&2; exit 1; }
header()  { echo -e "\n${BOLD}${BLUE}▸ $*${RESET}"; }
hr()      { echo -e "${BLUE}────────────────────────────────────────────────────────${RESET}"; }

YES=false
REINSTALL=false
for arg in "$@"; do
  case "$arg" in
    --yes)       YES=true ;;
    --reinstall) REINSTALL=true; YES=true ;;
  esac
done

confirm() {
  # confirm <prompt> — skipped when --yes is passed
  if $YES; then return 0; fi
  echo -e -n "  ${BOLD}$1${RESET} [y/N]: "
  read -r yn
  [[ "$yn" =~ ^[Yy]$ ]]
}

# Resolve deploy dir — works whether run from /tmp, the repo, or /opt/meshploy/deploy
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEPLOY_DIR="/opt/meshploy/deploy"
if [[ -f "$DEPLOY_DIR/docker-compose.yml" ]]; then
  cd "$DEPLOY_DIR"
elif [[ -f "$SCRIPT_DIR/docker-compose.yml" ]]; then
  cd "$SCRIPT_DIR"
else
  warn "Could not find Meshploy deploy directory. Looked in:"
  warn "  $DEPLOY_DIR"
  warn "  $SCRIPT_DIR"
  warn "Docker Compose steps will be skipped."
fi

# ── Banner ────────────────────────────────────────────────────────────────────
clear
echo -e "${BOLD}${RED}"
cat <<'EOF'
  __  __           _     ____  _
 |  \/  | ___  ___| |__ |  _ \| | ___  _   _
 | |\/| |/ _ \/ __| '_ \| |_) | |/ _ \| | | |
 | |  | |  __/\__ \ | | |  __/| | (_) | |_| |
 |_|  |_|\___||___/_| |_|_|   |_|\___/ \__, |
                                         |___/
EOF
echo -e "${RESET}"
echo -e "  ${BOLD}Uninstall Script${RESET}"
hr
echo
warn "This will stop all Meshploy services and remove all data."
warn "This action is ${BOLD}irreversible${RESET} unless you have backups."
echo

if ! $YES; then
  printf "  ${BOLD}Type 'yes' to continue${RESET}: "
  read -r confirm_input
  [[ "$confirm_input" == "yes" ]] || { info "Aborted."; exit 0; }
fi

# ── Stop and remove Docker Compose stack ─────────────────────────────────────
header "Stopping Docker Compose stack"
if [[ -f "docker-compose.yml" ]]; then
  if confirm "Remove containers and networks?"; then
    DOMAIN="${DOMAIN:-}" docker compose down --remove-orphans 2>/dev/null || true
    success "Containers stopped and removed"
  fi
else
  warn "docker-compose.yml not found — skipping"
fi

# ── Remove volumes (database + caddy TLS data) ───────────────────────────────
header "Removing Docker volumes"
if confirm "Delete all volumes? (${BOLD}this deletes the database and TLS certificates${RESET})"; then
  docker volume rm \
    "$(basename "$SCRIPT_DIR")_postgres_data" \
    "$(basename "$SCRIPT_DIR")_caddy_data" \
    "$(basename "$SCRIPT_DIR")_caddy_config" \
    2>/dev/null || true
  success "Volumes removed"
else
  warn "Volumes kept — data is preserved"
fi

# ── Remove generated config files ────────────────────────────────────────────
header "Removing generated configuration files"
if confirm "Delete generated zone files, .env, and substituted configs?"; then
  # Zone files (substituted copies — not the templates)
  find coredns/zones/ -type f ! -name '*{DOMAIN}*' -delete 2>/dev/null || true
  # .env
  rm -f .env
  # Do NOT delete Corefile or headscale/config/config.yaml — they are the
  # templates that install.sh substitutes in place. get.sh restores them via
  # git checkout before reinstalling.
  success "Generated files removed"
else
  warn "Config files kept"
fi

# ── Remove Headscale data ─────────────────────────────────────────────────────
header "Removing Headscale data"
if confirm "Delete Headscale state? (${BOLD}removes all nodes, keys, and ACLs${RESET})"; then
  rm -rf headscale/data/*
  success "Headscale data removed"
else
  warn "Headscale data kept"
fi

# ── Disconnect from mesh ──────────────────────────────────────────────────────
header "Disconnecting from WireGuard mesh"
if command -v tailscale &>/dev/null; then
  if confirm "Disconnect this node from the Tailscale/Headscale mesh?"; then
    tailscale down 2>/dev/null || true
    success "Disconnected from mesh"
  fi
else
  info "Tailscale not installed — skipping"
fi

# ── Remove Docker images (optional) ──────────────────────────────────────────
header "Docker images"
if confirm "Remove pulled Meshploy images from local Docker? (saves disk space)"; then
  docker rmi \
    ghcr.io/meshploy/api:latest \
    ghcr.io/meshploy/web:latest \
    ghcr.io/meshploy/proxy:latest \
    ghcr.io/meshploy/caddy:latest \
    ghcr.io/meshploy/builder:latest \
    2>/dev/null || true
  success "Images removed"
else
  info "Images kept"
fi

# ── Done ──────────────────────────────────────────────────────────────────────
echo
hr
echo -e "  ${BOLD}${GREEN}✔  Meshploy uninstalled.${RESET}"
hr

if $REINSTALL; then
  echo
  info "Reinstalling…"
  exec bash "$(pwd)/install.sh"
fi

echo
echo -e "  To reinstall:  ${BOLD}bash install.sh${RESET}"
echo -e "  To reinstall in one command:"
echo -e "  ${BOLD}bash uninstall.sh --reinstall${RESET}"
hr
