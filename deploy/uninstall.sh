#!/usr/bin/env bash
# =============================================================================
#  Meshploy Uninstall Script
#  Removes the Meshploy stack, data, and mesh configuration from this node.
#
#  Usage:
#    bash uninstall.sh           — interactive (asks before each destructive step)
#    bash uninstall.sh --yes     — non-interactive (skips all confirmations)
#    bash uninstall.sh --reinstall — uninstall then immediately re-run install.sh
#
#  On the gateway it also removes k3s, Tailscale, node_exporter, /opt/meshploy
#  and the meshploy CLI, each after asking. --reinstall keeps all of those.
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

# Where install.sh put things. Overridable so the uninstaller can be exercised
# against a scratch directory instead of a real install.
MESHPLOY_DIR="${MESHPLOY_DIR:-/opt/meshploy}"
MESHPLOY_CLI="${MESHPLOY_CLI:-/usr/local/bin/meshploy}"

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

# remove_tailscale_package: the package and its state. Shared by both roles.
remove_tailscale_package() {
  sudo apt-get remove --purge -y tailscale 2>/dev/null \
    || sudo dnf remove -y tailscale 2>/dev/null \
    || sudo yum remove -y tailscale 2>/dev/null \
    || true
  sudo rm -rf /var/lib/tailscale /etc/tailscale
}

# Resolve deploy dir — works whether run from /tmp, the repo, or /opt/meshploy/deploy
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]:-}")" && pwd)"
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
  if sudo test -f /etc/meshploy/node.conf 2>/dev/null; then
    eval "$(sudo cat /etc/meshploy/node.conf)"
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
          sudo rm -f /etc/meshploy/node.conf
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
      remove_tailscale_package
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
    if confirm "Delete all volumes? (${BOLD}this deletes the database, TLS certificates and images pushed to the built-in registry${RESET})"; then
      # postgres and registry are project-scoped, so they carry the compose
      # directory's name; the Caddy volumes are declared external with fixed
      # names, which a directory-derived name would miss when run elsewhere.
      $CONTAINER_RUNTIME volume rm \
        "$(basename "$(pwd)")_postgres_data" \
        "$(basename "$(pwd)")_registry_data" \
        meshploy_caddy_data \
        meshploy_caddy_config \
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

  # ── Remove k3s (server) ─────────────────────────────────────────────────────
  # The gateway runs the k3s server, and everything deployed on this node lives
  # in it: stopping the containers above leaves the cluster, its workloads and
  # its ports (6443, 10250) running. Skipped on --reinstall, which has to keep
  # every workload and its volume data.
  if ! $REINSTALL; then
    header "Removing k3s"
    if command -v k3s-uninstall.sh &>/dev/null; then
      info "Worker nodes joined to this gateway lose their control plane. Remove them first:"
      info "  meshploy node remove <name> <user@host>"
      if confirm "Run k3s-uninstall.sh? (${BOLD}deletes the cluster and every workload and volume on this node${RESET})"; then
        sudo k3s-uninstall.sh || true
        success "k3s removed"
      else
        warn "k3s kept"
      fi
    else
      info "k3s not installed; skipping"
    fi
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

  # ── Remove Tailscale ────────────────────────────────────────────────────────
  # No `tailscale logout` here: this node's control server is the Headscale
  # stopped and deleted above, so there is nothing left to log out of, and the
  # call would only wait on a server that is gone.
  if ! $REINSTALL && command -v tailscale &>/dev/null; then
    header "Tailscale"
    if confirm "Remove the Tailscale package and its state?"; then
      remove_tailscale_package
      success "Tailscale removed"
    else
      info "Tailscale kept"
    fi
  fi

  # ── Remove images (optional) ────────────────────────────────────────────────
  header "Container images"
  if confirm "Remove pulled Meshploy images? (saves disk space)"; then
    # Every tag of every Meshploy image, not just :latest: an edge install runs
    # :main, and upgrades leave older tags behind.
    _IMAGES="$($CONTAINER_RUNTIME images --format '{{.Repository}}:{{.Tag}}' 2>/dev/null \
      | grep '^ghcr.io/meshploy/' | grep -v ':<none>$' || true)"
    if [[ -n "$_IMAGES" ]]; then
      # shellcheck disable=SC2086 # one image reference per word, by design
      $CONTAINER_RUNTIME rmi $_IMAGES 2>/dev/null || true
    fi
    success "Images removed"
  else
    info "Images kept"
  fi

fi

# ── node_exporter (both roles) ────────────────────────────────────────────────
# install.sh installs it on every node as a systemd service on port 9100.
if ! $REINSTALL && { command -v node_exporter &>/dev/null || [[ -f /etc/systemd/system/node_exporter.service ]]; }; then
  header "node_exporter"
  if confirm "Remove node_exporter (the metrics service on port 9100)?"; then
    sudo systemctl disable --now node_exporter 2>/dev/null || true
    sudo rm -f /etc/systemd/system/node_exporter.service /usr/local/bin/node_exporter
    sudo systemctl daemon-reload 2>/dev/null || true
    success "node_exporter removed"
  else
    info "node_exporter kept"
  fi
fi

# ── Installation directory and CLI (gateway) ──────────────────────────────────
# Last, because this script and the CLI that launched it live there. Only the
# install location is ever removed, never the directory the script was run
# from: run from a repo checkout, that would be the checkout itself.
REMOVED_DIR=false
REMOVED_CLI=false
if ! $WORKER && ! $REINSTALL; then
  if [[ -d "$MESHPLOY_DIR" ]]; then
    header "Installation directory"
    if confirm "Delete ${MESHPLOY_DIR} (compose files, Caddy and CoreDNS configuration)?"; then
      cd /
      rm -rf "$MESHPLOY_DIR"
      REMOVED_DIR=true
      success "${MESHPLOY_DIR} removed"
    else
      info "${MESHPLOY_DIR} kept"
    fi
  fi
  if [[ -e "$MESHPLOY_CLI" ]]; then
    header "meshploy CLI"
    if confirm "Remove the meshploy CLI (${MESHPLOY_CLI}) and its aliases?"; then
      find "$(dirname "$MESHPLOY_CLI")" -maxdepth 1 -type l -lname "$MESHPLOY_CLI" -delete 2>/dev/null || true
      rm -f "$MESHPLOY_CLI"
      REMOVED_CLI=true
      success "meshploy CLI removed"
    else
      info "CLI kept"
    fi
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
elif $REMOVED_DIR; then
  echo -e "  To install again:"
  echo -e "  ${BOLD}sudo bash -c \"\$(curl -fsSL https://meshploy.com/install.sh)\"${RESET}"
else
  echo -e "  To reinstall:  ${BOLD}bash install.sh${RESET}"
  echo -e "  To reinstall in one command:"
  echo -e "  ${BOLD}bash uninstall.sh --reinstall${RESET}"
fi
if $REMOVED_CLI; then
  echo
  echo -e "  Each user's CLI login is kept in ~/.meshploy; remove it with:  ${BOLD}rm -rf ~/.meshploy${RESET}"
fi
if ! $WORKER && ! $REINSTALL; then
  echo -e "  Firewall rules the installer opened (80, 443, 53) are left in place."
fi
hr
