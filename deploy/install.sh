#!/usr/bin/env bash
# =============================================================================
#  Meshploy Installation Script
#  https://github.com/meshploy/meshploy
#
#  Can be run directly:
#    curl -fsSL https://raw.githubusercontent.com/meshploy/meshploy/main/deploy/install.sh \
#      -o /tmp/install.sh && sudo bash /tmp/install.sh
#
#  If config files are not found alongside the script (e.g. running from /tmp),
#  the repo is cloned to /opt/meshploy and the script re-execs from there.
# =============================================================================
set -euo pipefail

# When piped via curl | bash, stdin is the pipe not the terminal.
# Reconnect stdin to the terminal so interactive prompts work correctly.
exec < /dev/tty

# ── Self-bootstrap ────────────────────────────────────────────────────────────
# If the config templates are not next to this script, clone the repo first.
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
if [[ ! -f "$SCRIPT_DIR/coredns/Corefile" ]]; then
  INSTALL_DIR="/opt/meshploy"
  REPO="https://github.com/meshploy/meshploy"
  echo "Config files not found — cloning Meshploy to ${INSTALL_DIR}..."
  if ! command -v git &>/dev/null; then
    apt-get update -qq && apt-get install -y -qq git 2>/dev/null \
      || yum install -y git 2>/dev/null \
      || { echo "git is required. Install it and re-run."; exit 1; }
  fi
  if [[ -d "$INSTALL_DIR/.git" ]]; then
    git -C "$INSTALL_DIR" fetch --quiet origin main
    git -C "$INSTALL_DIR" checkout --quiet origin/main -- .
  else
    git clone --depth 1 "$REPO" "$INSTALL_DIR"
  fi
  exec bash "$INSTALL_DIR/deploy/install.sh"
fi

# ── Colours ───────────────────────────────────────────────────────────────────
RED='\033[0;31m';  GREEN='\033[0;32m'; YELLOW='\033[1;33m'
BLUE='\033[0;34m'; CYAN='\033[0;36m';  BOLD='\033[1m';  RESET='\033[0m'

info()    { echo -e "${CYAN}  →${RESET}  $*"; }
success() { echo -e "${GREEN}  ✔${RESET}  $*"; }
warn()    { echo -e "${YELLOW}  ⚠${RESET}  $*"; }
error()   { echo -e "${RED}  ✘${RESET}  $*" >&2; }
die()     { error "$*"; exit 1; }
header()  { echo -e "\n${BOLD}${BLUE}▸ $*${RESET}"; }
hr()      { echo -e "${BLUE}────────────────────────────────────────────────────────${RESET}"; }

ask() {
  # ask <variable_name> <prompt> [default]
  local var="$1" prompt="$2" default="${3:-}"
  local display_default=""
  [[ -n "$default" ]] && display_default=" ${BLUE}[${default}]${RESET}"
  while true; do
    printf "  ${BOLD}%s${RESET}%b: " "$prompt" "$display_default"
    read -r input
    input="${input:-$default}"
    if [[ -n "$input" ]]; then
      printf -v "$var" '%s' "$input"
      return
    fi
    warn "This field is required."
  done
}

ask_secret() {
  local var="$1" prompt="$2"
  while true; do
    printf "  ${BOLD}%s${RESET} (hidden): " "$prompt"
    read -rs input; echo
    if [[ -n "$input" ]]; then
      printf -v "$var" '%s' "$input"
      return
    fi
    warn "This field is required."
  done
}

ask_yn() {
  # ask_yn <prompt> [Y/n]
  local prompt="$1" default="${2:-y}"
  local hint="[Y/n]"; [[ "$default" == "n" ]] && hint="[y/N]"
  printf "  ${BOLD}%s${RESET} %s: " "$prompt" "$hint"
  read -r yn
  yn="${yn:-$default}"
  [[ "$yn" =~ ^[Yy]$ ]]
}

# ── Banner ────────────────────────────────────────────────────────────────────
clear
echo -e "${BOLD}${BLUE}"
cat <<'EOF'
  __  __           _     ____  _
 |  \/  | ___  ___| |__ |  _ \| | ___  _   _
 | |\/| |/ _ \/ __| '_ \| |_) | |/ _ \| | | |
 | |  | |  __/\__ \ | | |  __/| | (_) | |_| |
 |_|  |_|\___||___/_| |_|_|   |_|\___/ \__, |
                                         |___/
EOF
echo -e "${RESET}"
echo -e "  ${BOLD}Zero-trust Internal Developer Platform${RESET}"
echo -e "  Installation script — version 0.1"
hr

# ── Prerequisites ─────────────────────────────────────────────────────────────
header "Checking prerequisites"

OS="$(uname -s)"
[[ "$OS" != "Linux" ]] && die "This script requires Linux."

if ! command -v docker &>/dev/null; then
  warn "Docker is not installed."
  if ask_yn "Install Docker now?"; then
    info "Installing Docker…"
    curl -fsSL https://get.docker.com | sh
    sudo systemctl enable --now docker
    success "Docker installed."
  else
    die "Docker is required. Aborting."
  fi
else
  success "Docker $(docker --version | awk '{print $3}' | tr -d ',')"
fi

if ! docker compose version &>/dev/null 2>&1; then
  die "Docker Compose v2 plugin not found. Install Docker Engine ≥ 24."
fi
success "Docker Compose $(docker compose version --short)"

cd "$SCRIPT_DIR"

# ── Node type ─────────────────────────────────────────────────────────────────
header "Node type"
echo
echo -e "  ${BOLD}1)${RESET} Master  — runs the Meshploy control plane, DNS, proxy, and Headscale"
echo -e "  ${BOLD}2)${RESET} Worker  — joins an existing Meshploy mesh as a compute node"
echo
printf "  ${BOLD}Select node type${RESET} [1/2]: "
read -r NODE_TYPE
case "$NODE_TYPE" in
  1|master|Master) NODE_TYPE="master" ;;
  2|worker|Worker) NODE_TYPE="worker" ;;
  *) die "Invalid selection. Enter 1 (master) or 2 (worker)." ;;
esac
success "Node type: ${BOLD}$NODE_TYPE${RESET}"

# =============================================================================
#  MASTER
# =============================================================================
if [[ "$NODE_TYPE" == "master" ]]; then

  header "Master configuration"

  # Auto-detect public IP
  DETECTED_IP="$(curl -4 -fsSL https://ifconfig.me 2>/dev/null || echo "")"

  ask DOMAIN       "Base domain (e.g. meshploy.example.com)"
  ask PUBLIC_IP    "Public IP of this server" "$DETECTED_IP"
  ask MESH_IP      "WireGuard mesh IP for this node" "100.64.0.1"
  ask POSTGRES_PASSWORD "Postgres password (or press enter to auto-generate)" ""

  if [[ -z "$POSTGRES_PASSWORD" ]]; then
    POSTGRES_PASSWORD="$(openssl rand -hex 16)"
    info "Generated Postgres password: ${BOLD}$POSTGRES_PASSWORD${RESET}"
  fi

  JWT_SECRET="$(openssl rand -hex 32)"
  ENCRYPTION_KEY="$(openssl rand -hex 16)"   # 32 hex chars = 16 bytes, stored as 32 char string
  info "Auto-generated JWT_SECRET and ENCRYPTION_KEY."

  echo
  hr
  echo -e "  ${BOLD}Summary${RESET}"
  hr
  echo -e "  Domain         ${CYAN}${DOMAIN}${RESET}"
  echo -e "  Public IP      ${CYAN}${PUBLIC_IP}${RESET}"
  echo -e "  Mesh IP        ${CYAN}${MESH_IP}${RESET}"
  echo -e "  Headscale URL  ${CYAN}https://headscale.${DOMAIN}${RESET}"
  echo -e "  API URL        ${CYAN}https://api.${DOMAIN}${RESET}"
  echo -e "  Dashboard URL  ${CYAN}https://app.${DOMAIN}${RESET}"
  hr
  echo
  if ! ask_yn "Proceed with this configuration?"; then
    die "Aborted."
  fi

  # ── Write config files ──────────────────────────────────────────────────────
  header "Writing configuration files"

  # .env
  cat > .env <<ENVEOF
DOMAIN=${DOMAIN}
PUBLIC_IP=${PUBLIC_IP}
MESH_IP=${MESH_IP}
POSTGRES_PASSWORD=${POSTGRES_PASSWORD}
POSTGRES_DB=meshploy
POSTGRES_USER=meshploy
JWT_SECRET=${JWT_SECRET}
ENCRYPTION_KEY=${ENCRYPTION_KEY}
API_BASE_URL=https://api.${DOMAIN}
FRONTEND_URL=https://app.${DOMAIN}
# Fill in after first start: docker compose exec headscale headscale apikeys create
HEADSCALE_API_KEY=
ENVEOF
  success ".env written"

  # Helper to substitute placeholders in a file
  substitute() {
    sed -i \
      "s|\${DOMAIN}|${DOMAIN}|g; s|{DOMAIN}|${DOMAIN}|g; \
       s|\${PUBLIC_IP}|${PUBLIC_IP}|g; s|{PUBLIC_IP}|${PUBLIC_IP}|g; \
       s|\${MESH_IP}|${MESH_IP}|g; s|{MESH_IP}|${MESH_IP}|g" \
      "$1"
  }

  # CoreDNS Corefile
  substitute coredns/Corefile
  success "coredns/Corefile configured"

  # Rename and populate zone files
  for tpl in "coredns/zones/{DOMAIN}" "coredns/zones/internal.{DOMAIN}" "coredns/zones/_acme-challenge.{DOMAIN}"; do
    if [[ -f "$tpl" ]]; then
      target="${tpl/\{DOMAIN\}/${DOMAIN}}"
      cp "$tpl" "$target"
      substitute "$target"
      success "$(basename "$target") written"
    fi
  done

  # Headscale config
  substitute headscale/config/config.yaml
  success "headscale/config/config.yaml configured"

  # Caddyfile uses {$DOMAIN} (Caddy env var syntax) — substitute at runtime via DOMAIN env var
  # We export DOMAIN so Caddy can read it; no file substitution needed for Caddyfile.
  success "caddy/Caddyfile ready (uses \${DOMAIN} env var at runtime)"

  # ── Private registry login ──────────────────────────────────────────────────
  header "Container registry"
  echo -e "  Meshploy images are hosted on GitHub Container Registry (ghcr.io)."
  echo -e "  You need a GitHub Personal Access Token with ${BOLD}read:packages${RESET} scope."
  echo -e "  Create one at: https://github.com/settings/tokens/new?scopes=read:packages"
  echo
  if ask_yn "Log in to ghcr.io now?"; then
    ask GHCR_USER "GitHub username"
    ask_secret GHCR_TOKEN "GitHub PAT (read:packages)"
    echo "$GHCR_TOKEN" | docker login ghcr.io --username "$GHCR_USER" --password-stdin \
      && success "Logged in to ghcr.io as ${BOLD}${GHCR_USER}${RESET}" \
      || die "docker login failed — check your username and token."
  else
    warn "Skipping registry login. 'docker compose pull' will fail if images are private."
  fi

  # ── Phase 1: Start core services (no mesh IP needed yet) ────────────────────
  # CoreDNS and Caddy bind to the WireGuard mesh IP (100.64.x.x) which doesn't
  # exist until Tailscale joins the mesh. Start them in phase 2.
  header "Starting core services"
  info "Pulling images…"
  docker compose pull
  info "Starting postgres, headscale, api, web, proxy…"
  DOMAIN="$DOMAIN" docker compose up -d postgres headscale api web proxy
  success "Core services started"

  # ── Wait for Headscale ──────────────────────────────────────────────────────
  header "Waiting for Headscale to be ready"
  MAX_WAIT=60; WAITED=0
  until docker compose exec -T headscale headscale version &>/dev/null; do
    sleep 2; WAITED=$((WAITED+2))
    [[ $WAITED -ge $MAX_WAIT ]] && die "Headscale did not start within ${MAX_WAIT}s. Check: docker compose logs headscale"
    printf "."
  done
  echo; success "Headscale is ready"

  # ── Create Headscale user + pre-auth key ────────────────────────────────────
  header "Setting up Headscale"
  HEADSCALE_USER="meshploy"

  if ! docker compose exec -T headscale headscale users list 2>/dev/null | grep -q "$HEADSCALE_USER"; then
    docker compose exec -T headscale headscale users create "$HEADSCALE_USER" &>/dev/null
    success "Headscale user '${HEADSCALE_USER}' created"
  else
    success "Headscale user '${HEADSCALE_USER}' already exists"
  fi

  PREAUTH_KEY="$(docker compose exec -T headscale \
    headscale preauthkeys create --user "$HEADSCALE_USER" --expiration 1h --reusable \
    2>/dev/null | grep -oE '[a-z0-9]{40,}')"
  [[ -z "$PREAUTH_KEY" ]] && die "Failed to generate pre-auth key. Check: docker compose logs headscale"
  success "Pre-auth key generated (reusable, 1h): ${BOLD}${PREAUTH_KEY}${RESET}"

  HEADSCALE_API_KEY="$(docker compose exec -T headscale \
    headscale apikeys create 2>/dev/null | grep -oE '[a-z0-9]{40,}')"
  [[ -z "$HEADSCALE_API_KEY" ]] && die "Failed to generate API key. Check: docker compose logs headscale"
  sed -i "s|HEADSCALE_API_KEY=|HEADSCALE_API_KEY=${HEADSCALE_API_KEY}|" .env
  success "Headscale API key written to .env"

  # ── Install Tailscale ───────────────────────────────────────────────────────
  header "Installing Tailscale"
  if ! command -v tailscale &>/dev/null; then
    info "Downloading and installing Tailscale…"
    curl -fsSL https://tailscale.com/install.sh | sh
    success "Tailscale installed"
  else
    success "Tailscale already installed: $(tailscale version | head -1)"
  fi

  # ── Join this machine to the mesh ───────────────────────────────────────────
  # Joining creates the WireGuard interface with the mesh IP (e.g. 100.64.0.1).
  # CoreDNS and Caddy must start AFTER this so they can bind to that IP.
  header "Joining this node to the Headscale mesh"
  info "Connecting to https://headscale.${DOMAIN}…"
  tailscale up \
    --login-server="https://headscale.${DOMAIN}" \
    --authkey="$PREAUTH_KEY" \
    --hostname="gateway" \
    --accept-routes \
    || warn "tailscale up returned non-zero — it may already be connected, check: tailscale status"
  success "This node joined the mesh as 'gateway'"

  # ── Phase 2: Start mesh-IP-dependent services ────────────────────────────────
  # CoreDNS binds to PUBLIC_IP:53 and MESH_IP:53.
  # Caddy binds to PUBLIC_IP:80/443 and MESH_IP:80/443.
  # Both require the WireGuard interface to exist before starting.
  header "Starting DNS and proxy services"
  DOMAIN="$DOMAIN" docker compose up -d coredns caddy
  success "CoreDNS and Caddy started"

  # ── Final summary ────────────────────────────────────────────────────────────
  echo
  hr
  echo -e "  ${BOLD}${GREEN}✔  Meshploy master node is ready!${RESET}"
  hr
  echo -e "  ${BOLD}Service URLs${RESET}"
  echo -e "    Dashboard   ${CYAN}https://app.${DOMAIN}${RESET}"
  echo -e "    API         ${CYAN}https://api.${DOMAIN}${RESET}"
  echo -e "    Headscale   ${CYAN}https://headscale.${DOMAIN}${RESET}"
  echo
  echo -e "  ${BOLD}To add a worker node${RESET}"
  echo -e "    Copy this pre-auth key (valid 1h, reusable):"
  echo -e "    ${BOLD}${CYAN}${PREAUTH_KEY}${RESET}"
  echo -e "    Then on the worker machine run:"
  echo -e "    ${BOLD}curl -fsSL https://raw.githubusercontent.com/meshploy/meshploy/main/deploy/install.sh | bash${RESET}"
  echo
  echo -e "  ${BOLD}Next steps${RESET}"
  echo -e "    1. Point your domain's NS records to this server (${PUBLIC_IP})"
  echo -e "    2. Verify DNS:  dig @${PUBLIC_IP} ${DOMAIN} A"
  echo -e "    3. Check TLS:   curl -I https://api.${DOMAIN}"
  echo -e "    4. Check mesh:  tailscale status"
  hr

# =============================================================================
#  WORKER
# =============================================================================
elif [[ "$NODE_TYPE" == "worker" ]]; then

  header "Worker configuration"
  ask HEADSCALE_URL  "Master's Headscale URL (e.g. https://headscale.meshploy.example.com)"
  ask_secret PREAUTH_KEY "Pre-auth key (generated on the master node)"

  echo
  hr
  echo -e "  ${BOLD}Summary${RESET}"
  hr
  echo -e "  Headscale URL  ${CYAN}${HEADSCALE_URL}${RESET}"
  echo -e "  Pre-auth key   ${CYAN}${PREAUTH_KEY:0:8}…${RESET} (truncated for display)"
  hr
  echo
  if ! ask_yn "Proceed?"; then
    die "Aborted."
  fi

  # ── Install Tailscale ───────────────────────────────────────────────────────
  header "Installing Tailscale"
  if ! command -v tailscale &>/dev/null; then
    info "Downloading and installing Tailscale…"
    curl -fsSL https://tailscale.com/install.sh | sh
    success "Tailscale installed"
  else
    success "Tailscale already installed: $(tailscale version | head -1)"
  fi

  # ── Derive a hostname from the machine's hostname ───────────────────────────
  NODE_HOSTNAME="$(hostname -s | tr '[:upper:]' '[:lower:]' | tr '_' '-')"
  ask NODE_HOSTNAME "Hostname for this node in the mesh" "$NODE_HOSTNAME"

  # ── Join the mesh ───────────────────────────────────────────────────────────
  header "Joining the Meshploy mesh"
  info "Connecting to ${HEADSCALE_URL}…"
  sudo tailscale up \
    --login-server="$HEADSCALE_URL" \
    --authkey="$PREAUTH_KEY" \
    --hostname="$NODE_HOSTNAME" \
    --accept-routes \
    || warn "tailscale up returned non-zero — it may already be connected, check: tailscale status"

  MESH_IP_ASSIGNED="$(tailscale ip -4 2>/dev/null || echo "pending")"
  success "Joined mesh as '${NODE_HOSTNAME}' — mesh IP: ${BOLD}${MESH_IP_ASSIGNED}${RESET}"

  echo
  hr
  echo -e "  ${BOLD}${GREEN}✔  Worker node is ready!${RESET}"
  hr
  echo -e "  ${BOLD}Node${RESET}       ${CYAN}${NODE_HOSTNAME}${RESET}"
  echo -e "  ${BOLD}Mesh IP${RESET}    ${CYAN}${MESH_IP_ASSIGNED}${RESET}"
  echo
  echo -e "  ${BOLD}Next steps${RESET}"
  echo -e "    1. In the Meshploy dashboard → Nodes → register this node"
  echo -e "       with IP ${BOLD}${MESH_IP_ASSIGNED}${RESET}"
  echo -e "    2. Check connectivity from master:  tailscale ping ${NODE_HOSTNAME}"
  hr

fi
