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
WORKER=false
for arg in "$@"; do
  case "$arg" in
    --yes)       YES=true ;;
    --reinstall) REINSTALL=true; YES=true ;;
    --worker)    WORKER=true ;;
  esac
done

# Auto-detect worker: no docker-compose.yml in deploy dir and k3s-agent is present
if ! $WORKER && ! [[ -f "/opt/meshploy/deploy/docker-compose.yml" ]] && command -v k3s &>/dev/null && systemctl is-enabled k3s-agent &>/dev/null 2>&1; then
  WORKER=true
fi

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

# ── Detect container runtime ──────────────────────────────────────────────────
CONTAINER_RUNTIME=$(grep '^CONTAINER_RUNTIME=' .env 2>/dev/null | cut -d= -f2 || true)
if [[ -z "$CONTAINER_RUNTIME" ]]; then
  if command -v docker &>/dev/null && ! docker --version 2>/dev/null | grep -qi "podman"; then
    CONTAINER_RUNTIME="docker"
  elif command -v podman &>/dev/null; then
    CONTAINER_RUNTIME="podman"
  else
    CONTAINER_RUNTIME="docker"
  fi
fi
COMPOSE_CMD="$CONTAINER_RUNTIME compose"

if $WORKER; then
  # ==========================================================================
  #  WORKER uninstall — removes k3s agent + Tailscale from this node
  # ==========================================================================
  echo -e "  Detected: ${BOLD}worker node${RESET}"
  echo -e "  This will remove k3s agent and disconnect from the WireGuard mesh."
  echo

  # ── Remove k3s agent ────────────────────────────────────────────────────────
  header "Removing k3s agent"
  if command -v k3s-agent-uninstall.sh &>/dev/null; then
    if confirm "Run k3s-agent-uninstall.sh? (stops agent, removes binaries and data)"; then
      sudo k3s-agent-uninstall.sh || true
      success "k3s agent removed"
    fi
  elif systemctl is-enabled k3s-agent &>/dev/null 2>&1; then
    if confirm "Stop and disable k3s-agent service?"; then
      sudo systemctl stop k3s-agent 2>/dev/null || true
      sudo systemctl disable k3s-agent 2>/dev/null || true
      sudo rm -f /etc/systemd/system/k3s-agent.service /etc/systemd/system/k3s-agent.service.env
      sudo systemctl daemon-reload
      sudo rm -rf /var/lib/rancher/k3s /etc/rancher/k3s
      success "k3s agent removed"
    fi
  else
    info "k3s agent not installed — skipping"
  fi

  # ── Deregister from Meshploy ─────────────────────────────────────────────────
  # Must happen before tailscale logout — the API is reachable over the mesh.
  header "Deregistering from Meshploy"
  if [[ -f /etc/meshploy/node.conf ]]; then
    # shellcheck source=/dev/null
    source /etc/meshploy/node.conf
    if [[ -n "${NODE_ID:-}" && -n "${MESHPLOY_API_URL:-}" && -n "${MESHPLOY_TOKEN:-}" ]]; then
      if confirm "Remove '${NODE_NAME:-$NODE_ID}' from Meshploy, Headscale, and k3s cluster?"; then
        _DEREG_STATUS="$(curl -s -o /dev/null -w "%{http_code}" \
          --max-time 10 \
          -X DELETE "${MESHPLOY_API_URL}/api/v1/nodes/self-deregister" \
          -H "Content-Type: application/json" \
          -d "{\"token\":\"${MESHPLOY_TOKEN}\",\"node_id\":\"${NODE_ID}\"}" \
          2>/dev/null || echo "000")"
        if [[ "$_DEREG_STATUS" == "200" || "$_DEREG_STATUS" == "204" ]]; then
          success "Node deregistered — removed from Meshploy DB, Headscale, and k3s cluster"
          rm -f /etc/meshploy/node.conf
        else
          warn "Deregister API returned HTTP ${_DEREG_STATUS} — node may still appear in dashboard"
          warn "Delete it manually: Nodes → ${NODE_NAME:-$NODE_ID} → Remove"
        fi
      fi
    fi
  else
    info "No node identity found (/etc/meshploy/node.conf) — skipping API deregister"
    warn "If this node appears in the Meshploy dashboard, delete it manually."
  fi

  # ── Disconnect from mesh ─────────────────────────────────────────────────────
  header "Disconnecting from WireGuard mesh"
  if command -v tailscale &>/dev/null; then
    if confirm "Log out and disconnect from Headscale mesh?"; then
      sudo tailscale logout 2>/dev/null || true
      sudo tailscale down 2>/dev/null || true
      success "Disconnected from mesh"
    fi
  else
    info "Tailscale not installed — skipping"
  fi

  # ── Remove Tailscale (optional) ──────────────────────────────────────────────
  header "Tailscale binaries"
  if command -v tailscale &>/dev/null; then
    if confirm "Remove Tailscale package?"; then
      sudo apt-get remove --purge -y tailscale 2>/dev/null \
        || sudo yum remove -y tailscale 2>/dev/null \
        || true
      sudo rm -rf /var/lib/tailscale /etc/tailscale
      success "Tailscale removed"
    else
      info "Tailscale kept (you can reconnect to a different mesh later)"
    fi
  fi

else
  # ==========================================================================
  #  MASTER uninstall — tears down the full control plane stack
  # ==========================================================================

  # ── Stop and remove Compose stack ──────────────────────────────────────────
  header "Stopping ${CONTAINER_RUNTIME^} Compose stack"
  if [[ -f "docker-compose.yml" ]]; then
    if confirm "Remove containers and networks?"; then
      DOMAIN="${DOMAIN:-}" $COMPOSE_CMD down --remove-orphans 2>/dev/null || true
      success "Containers stopped and removed"
    fi
  else
    warn "docker-compose.yml not found — skipping"
  fi

  # ── Remove volumes (database + caddy TLS data) ─────────────────────────────
  # Skipped during --reinstall: caddy TLS certs (rate-limited by Let's Encrypt)
  # and database data are preserved so reinstall continues from a clean state.
  if ! $REINSTALL; then
    header "Removing volumes"
    if confirm "Delete all volumes? (${BOLD}this deletes the database and TLS certificates${RESET})"; then
      $CONTAINER_RUNTIME volume rm \
        "$(basename "$SCRIPT_DIR")_postgres_data" \
        "$(basename "$SCRIPT_DIR")_caddy_data" \
        "$(basename "$SCRIPT_DIR")_caddy_config" \
        2>/dev/null || true
      success "Volumes removed"
    else
      warn "Volumes kept — data is preserved"
    fi
  fi

  # ── Remove generated config files ──────────────────────────────────────────
  header "Removing generated configuration files"
  if confirm "Delete generated zone files, .env, and substituted configs?"; then
    find coredns/zones/ -type f ! -name '*{DOMAIN}*' -delete 2>/dev/null || true
    rm -f .env
    success "Generated files removed"
  else
    warn "Config files kept"
  fi

  # ── Remove Headscale data ───────────────────────────────────────────────────
  header "Removing Headscale data"
  if confirm "Delete Headscale state? (${BOLD}removes all nodes, keys, and ACLs${RESET})"; then
    rm -rf headscale/data/*
    success "Headscale data removed"
  else
    warn "Headscale data kept"
  fi

  # ── Disconnect from mesh ────────────────────────────────────────────────────
  header "Disconnecting from WireGuard mesh"
  if command -v tailscale &>/dev/null; then
    if confirm "Disconnect this node from the Tailscale/Headscale mesh?"; then
      tailscale down 2>/dev/null || true
      success "Disconnected from mesh"
    fi
  else
    info "Tailscale not installed — skipping"
  fi

  # ── Remove images (optional) ────────────────────────────────────────────────
  header "Container images"
  if confirm "Remove pulled Meshploy images? (saves disk space)"; then
    $CONTAINER_RUNTIME rmi \
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
if $WORKER; then
  echo -e "  To re-join as a worker:  ${BOLD}bash install.sh${RESET}"
else
  echo -e "  To reinstall:  ${BOLD}bash install.sh${RESET}"
  echo -e "  To reinstall in one command:"
  echo -e "  ${BOLD}bash uninstall.sh --reinstall${RESET}"
fi
hr
