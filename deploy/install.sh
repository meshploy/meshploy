#!/usr/bin/env bash
# =============================================================================
#  Meshploy Installation Script
#  https://github.com/meshploy/meshploy
#
#  Can be run directly:
#    curl -fsSL https://raw.githubusercontent.com/meshploy/meshploy/main/get.sh \
#      -o /tmp/get.sh && sudo bash /tmp/get.sh
#
#  If config files are not found alongside the script (e.g. running from /tmp),
#  the repo is cloned to /opt/meshploy and the script re-execs from there.
# =============================================================================
set -euo pipefail

REINSTALL=false
WIPE_DATA=false
AUTO_MODE=false
for arg in "$@"; do
  case "$arg" in
    --reinstall) REINSTALL=true ;;
    --wipe-data) WIPE_DATA=true ;;
    --auto)      AUTO_MODE=true ;;
  esac
done

# ── Self-bootstrap ────────────────────────────────────────────────────────────
# install.sh is always invoked via get.sh which downloads the deploy/ folder
# first. If someone runs install.sh directly without the config files present,
# tell them to use get.sh instead.
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
if [[ ! -f "$SCRIPT_DIR/coredns/Corefile" ]]; then
  echo "Config files not found. Please run via get.sh:"
  echo "  curl -fsSL https://raw.githubusercontent.com/meshploy/meshploy/main/get.sh -o /tmp/get.sh && sudo bash /tmp/get.sh"
  exit 1
fi

# ── Colours ───────────────────────────────────────────────────────────────────
RED='\033[0;31m';  GREEN='\033[0;32m'; YELLOW='\033[1;33m'
BLUE='\033[0;34m'; CYAN='\033[0;36m';  BOLD='\033[1m';  RESET='\033[0m'; DIM='\033[2m'

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
  if $AUTO_MODE; then
    local existing="${!var:-}"
    [[ -z "$existing" ]] && existing="$default"
    if [[ -n "$existing" ]]; then
      info "Auto: $prompt → $existing"
      printf -v "$var" '%s' "$existing"
      return
    fi
    die "Auto mode: $var is required but not set. Export it before running, or remove --auto."
  fi
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
  if $AUTO_MODE; then
    local existing="${!var:-}"
    if [[ -n "$existing" ]]; then
      info "Auto: $prompt → (set from environment)"
      printf -v "$var" '%s' "$existing"
      return
    fi
    die "Auto mode: $var is required but not set. Export it before running, or remove --auto."
  fi
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
  if $AUTO_MODE; then
    info "Auto: $prompt → $default"
    [[ "$default" =~ ^[Yy]$ ]]
    return
  fi
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

# ── Container runtime detection ──────────────────────────────────────────────
# Supports Docker (preferred) and Podman. CRI-O is a k8s-level runtime and is
# not applicable here — the platform services run via Compose, not k8s.
CONTAINER_RUNTIME=""
COMPOSE_CMD=""

if command -v docker &>/dev/null && ! docker --version 2>/dev/null | grep -qi "podman"; then
  # Real Docker (not podman-docker shim)
  if ! docker compose version &>/dev/null 2>&1; then
    die "Docker found but Compose v2 plugin missing. Install Docker Engine ≥ 24."
  fi
  CONTAINER_RUNTIME="docker"
  COMPOSE_CMD="docker compose"
  success "Docker $(docker --version | awk '{print $3}' | tr -d ',') + Compose $(docker compose version --short)"

elif command -v podman &>/dev/null; then
  # Podman — must be rootful (running as root/sudo) for host-gateway and port binding
  if ! podman compose version &>/dev/null 2>&1; then
    warn "Podman found but 'podman compose' is not available."
    warn "Install podman-compose: pip3 install podman-compose  OR  dnf/apt install podman-compose"
    if ! ask_yn "Continue anyway? (compose commands will fail until podman-compose is installed)"; then
      die "Aborted."
    fi
  fi
  CONTAINER_RUNTIME="podman"
  COMPOSE_CMD="podman compose"
  success "Podman $(podman --version | awk '{print $3}')"

else
  warn "Neither Docker nor Podman found."
  echo -e "  ${CYAN}1)${RESET} Install Docker (recommended)"
  echo -e "  ${CYAN}2)${RESET} Install Podman"
  RUNTIME_CHOICE="1"
  ask RUNTIME_CHOICE "Choose runtime [1/2]" "1"

  if [[ "$RUNTIME_CHOICE" == "1" ]]; then
    info "Installing Docker…"
    curl -fsSL https://get.docker.com | sh
    systemctl enable --now docker
    CONTAINER_RUNTIME="docker"
    COMPOSE_CMD="docker compose"
    success "Docker installed"
  elif [[ "$RUNTIME_CHOICE" == "2" ]]; then
    info "Installing Podman…"
    if command -v dnf &>/dev/null;       then dnf install -y podman podman-compose
    elif command -v apt-get &>/dev/null; then apt-get install -y podman podman-compose
    elif command -v zypper &>/dev/null;  then zypper install -y podman python3-podman-compose
    elif command -v pacman &>/dev/null;  then pacman -Sy --noconfirm podman python-podman-compose
    else die "Cannot auto-install Podman — package manager not recognised. Install manually then re-run."; fi
    CONTAINER_RUNTIME="podman"
    COMPOSE_CMD="podman compose"
    success "Podman installed"
  else
    die "Invalid selection '${RUNTIME_CHOICE}'. Enter 1 (Docker) or 2 (Podman)."
  fi
fi

# ── Host gateway IP detection ────────────────────────────────────────────────
# Maps host.meshploy.internal inside containers → host's bridge gateway IP so
# the API container can reach k3s on port 6443.
#
# Docker: bridge gateway from the docker0 network (typically 172.17.0.1)
# Podman: bridge gateway from the default podman network (typically 10.88.0.1)
#
# NOTE: ip route show default gives the internet-facing gateway — that is
# deliberately NOT used here.
HOST_GATEWAY_IP=""
if [[ "$CONTAINER_RUNTIME" == "docker" ]]; then
  # Pulling a tiny image forces Docker to initialise the bridge network on a
  # fresh install before we inspect it.
  docker pull hello-world &>/dev/null || true
  HOST_GATEWAY_IP=$(docker network inspect bridge \
    --format '{{range .IPAM.Config}}{{.Gateway}}{{end}}' 2>/dev/null || true)
elif [[ "$CONTAINER_RUNTIME" == "podman" ]]; then
  # Force Podman to initialise its default network on a fresh install.
  podman pull hello-world &>/dev/null || true
  HOST_GATEWAY_IP=$(podman network inspect podman \
    --format '{{range .Subnets}}{{.Gateway}}{{end}}' 2>/dev/null || true)
else
  die "Unsupported container runtime: '${CONTAINER_RUNTIME}'. Expected 'docker' or 'podman'."
fi
[[ -z "$HOST_GATEWAY_IP" ]] && die "Could not detect container bridge gateway IP. Start ${CONTAINER_RUNTIME} first, then retry."
success "Host gateway IP (${CONTAINER_RUNTIME} bridge): ${BOLD}${HOST_GATEWAY_IP}${RESET}"

cd "$SCRIPT_DIR"

# ── Node type ─────────────────────────────────────────────────────────────────
header "Node type"
echo
echo -e "  ${BOLD}1)${RESET} Master  — runs the Meshploy control plane, DNS, proxy, and Headscale"
echo -e "  ${BOLD}2)${RESET} Worker  — joins an existing Meshploy mesh as a compute node"
echo
if $AUTO_MODE && [[ -n "${NODE_TYPE:-}" ]]; then
  case "$NODE_TYPE" in
    1|master|Master) NODE_TYPE="master" ;;
    2|worker|Worker) NODE_TYPE="worker" ;;
    *) die "Auto mode: NODE_TYPE must be 'master' or 'worker', got: '$NODE_TYPE'" ;;
  esac
  info "Auto: node type → $NODE_TYPE"
else
  printf "  ${BOLD}Select node type${RESET} [1/2]: "
  read -r NODE_TYPE
  case "$NODE_TYPE" in
    1|master|Master) NODE_TYPE="master" ;;
    2|worker|Worker) NODE_TYPE="worker" ;;
    *) die "Invalid selection. Enter 1 (master) or 2 (worker)." ;;
  esac
fi
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

  # ── Migrate existing .env if it's missing HOST_GATEWAY_IP ───────────────────
  # Servers installed before runtime-aware bridge detection was added won't have
  # this variable. The compose file needs it for the extra_hosts entry.
  if [[ -f ".env" ]] && ! grep -q "^HOST_GATEWAY_IP=" .env; then
    echo "HOST_GATEWAY_IP=${HOST_GATEWAY_IP}" >> .env
    success ".env upgraded: added HOST_GATEWAY_IP=${HOST_GATEWAY_IP}"
  fi

  # ── Handle existing installation ───────────────────────────────────────────
  CADDY_VOLUME="meshploy_caddy_data"
  if $CONTAINER_RUNTIME volume inspect "$CADDY_VOLUME" &>/dev/null; then
    if $WIPE_DATA; then
      info "Wiping existing data volumes…"
      $COMPOSE_CMD down --remove-orphans 2>/dev/null || true
      $CONTAINER_RUNTIME volume rm meshploy_caddy_data meshploy_caddy_config meshploy_postgres_data 2>/dev/null || true
      success "Volumes wiped — starting fresh"
    elif $REINSTALL; then
      info "Existing installation detected — updating images and preserving data."
      $COMPOSE_CMD down --remove-orphans 2>/dev/null || true
    else
      # Fresh install with existing volume — ask user
      echo
      warn "Existing Meshploy data detected (TLS certs + database)."
      if ask_yn "Wipe existing data for a clean install?" "n"; then
        $COMPOSE_CMD down --remove-orphans 2>/dev/null || true
        $CONTAINER_RUNTIME volume rm meshploy_caddy_data meshploy_caddy_config meshploy_postgres_data 2>/dev/null || true
        success "Volumes wiped — starting fresh"
      else
        info "Preserving existing data — continuing install."
        $COMPOSE_CMD down --remove-orphans 2>/dev/null || true
      fi
    fi
  fi

  # ── Install k3s server (control plane) ─────────────────────────────────────
  # Must happen before writing .env so K3S_TOKEN is available.
  header "Installing k3s"
  if command -v k3s &>/dev/null && systemctl is-active --quiet k3s 2>/dev/null; then
    success "k3s server already running: $(k3s --version | head -1)"
  else
    info "Installing k3s server…"
    curl -sfL https://get.k3s.io | INSTALL_K3S_EXEC="--disable=traefik --disable=servicelb --node-ip=${MESH_IP}" sh -
    success "k3s server installed and started"
  fi

  # ── Fix CoreDNS upstream on systemd-resolved hosts (Ubuntu 22.04+) ──────────
  # k3s defaults CoreDNS to "forward . /etc/resolv.conf". On Ubuntu 22.04+
  # systemd-resolved puts 127.0.0.53 there — a loopback address unreachable
  # from inside pod network namespaces. Point k3s at the real upstream file.
  if [[ -f /run/systemd/resolve/resolv.conf ]]; then
    mkdir -p /etc/rancher/k3s
    echo 'kubelet-arg: ["--resolv-conf=/run/systemd/resolve/resolv.conf"]' \
      >> /etc/rancher/k3s/config.yaml
    success "Configured k3s to use /run/systemd/resolve/resolv.conf for pod DNS"
  fi

  # ── Configure containerd to trust the built-in registry (HTTP) ─────────────
  # registry:2 runs on the gateway at MESH_IP:5000 inside the WireGuard mesh.
  # K3s containerd needs an explicit mirror entry so it pulls over HTTP without TLS.
  info "Configuring containerd registry mirror for built-in registry…"
  mkdir -p /etc/rancher/k3s
  cat > /etc/rancher/k3s/registries.yaml <<REGEOF
mirrors:
  "${MESH_IP}:5000":
    endpoint:
      - "http://${MESH_IP}:5000"
REGEOF
  success "Wrote /etc/rancher/k3s/registries.yaml (mirror: ${MESH_IP}:5000)"

  # ── Allow container bridge networks to reach k3s API (port 6443) ────────────
  # The Meshploy API container connects to k3s at host.meshploy.internal:6443.
  # Traffic originates from the container bridge subnet — UFW and firewalld
  # block this by default.
  #   Docker bridge: 172.16.0.0/12  (172.17.x.x – 172.31.x.x)
  #   Podman bridge: 10.88.0.0/16
  if command -v ufw &>/dev/null && ufw status | grep -q "Status: active"; then
    if ! ufw status | grep -q "6443"; then
      ufw allow from 172.16.0.0/12 to any port 6443 comment "k3s API — Docker bridge" >/dev/null
      ufw allow from 10.88.0.0/16  to any port 6443 comment "k3s API — Podman bridge" >/dev/null
      success "UFW: allowed container bridge networks → port 6443"
    else
      success "UFW: port 6443 already allowed"
    fi
  elif command -v firewall-cmd &>/dev/null && firewall-cmd --state 2>/dev/null | grep -q "running"; then
    if ! firewall-cmd --list-rich-rules | grep -q "6443"; then
      firewall-cmd --permanent --add-rich-rule='rule family="ipv4" source address="172.16.0.0/12" port port="6443" protocol="tcp" accept'
      firewall-cmd --permanent --add-rich-rule='rule family="ipv4" source address="10.88.0.0/16" port port="6443" protocol="tcp" accept'
      firewall-cmd --reload >/dev/null
      success "firewalld: allowed container bridge networks → port 6443"
    else
      success "firewalld: port 6443 already allowed"
    fi
  fi

  # Read the node token (written by k3s on first start)
  K3S_TOKEN_FILE="/var/lib/rancher/k3s/server/node-token"
  MAX_K3S_WAIT=30; K3S_WAITED=0
  until [[ -f "$K3S_TOKEN_FILE" ]]; do
    sleep 2; K3S_WAITED=$((K3S_WAITED+2))
    [[ $K3S_WAITED -ge $MAX_K3S_WAIT ]] && die "k3s node token not found after ${MAX_K3S_WAIT}s. Check: journalctl -u k3s"
    printf "."
  done
  K3S_TOKEN="$(cat "$K3S_TOKEN_FILE")"
  success "k3s node token retrieved"

  # ── Write config files ──────────────────────────────────────────────────────
  header "Writing configuration files"

  GATEWAY_HOSTNAME=$(hostname)

  # .env
  cat > .env <<ENVEOF
DOMAIN=${DOMAIN}
PUBLIC_IP=${PUBLIC_IP}
MESH_IP=${MESH_IP}
GATEWAY_HOSTNAME=${GATEWAY_HOSTNAME}
POSTGRES_PASSWORD=${POSTGRES_PASSWORD}
POSTGRES_DB=meshploy
POSTGRES_USER=meshploy
JWT_SECRET=${JWT_SECRET}
ENCRYPTION_KEY=${ENCRYPTION_KEY}
API_BASE_URL=https://api.${DOMAIN}
FRONTEND_URL=https://app.${DOMAIN}
K3S_TOKEN=${K3S_TOKEN}
CONTAINER_RUNTIME=${CONTAINER_RUNTIME}
HOST_GATEWAY_IP=${HOST_GATEWAY_IP}
# Fill in after first start: $COMPOSE_CMD exec headscale headscale apikeys create
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
  for tpl in "coredns/zones/{DOMAIN}" "coredns/zones/internal.{DOMAIN}" "coredns/zones/_acme-challenge.{DOMAIN}" "coredns/zones/_acme-challenge.internal.{DOMAIN}"; do
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
    echo "$GHCR_TOKEN" | $CONTAINER_RUNTIME login ghcr.io --username "$GHCR_USER" --password-stdin \
      && success "Logged in to ghcr.io as ${BOLD}${GHCR_USER}${RESET}" \
      || die "$CONTAINER_RUNTIME login failed — check your username and token."
  else
    warn "Skipping registry login. '$COMPOSE_CMD pull' will fail if images are private."
  fi

  # ── Phase 1: Start core services (no mesh IP needed yet) ────────────────────
  # CoreDNS and Caddy bind to the WireGuard mesh IP (100.64.x.x) which doesn't
  # exist until Tailscale joins the mesh. Start them in phase 2.
  header "Starting core services"
  info "Pulling images…"
  $COMPOSE_CMD pull
  info "Starting postgres, headscale, api, web, proxy…"
  DOMAIN="$DOMAIN" $COMPOSE_CMD up -d postgres headscale api web proxy
  success "Core services started"

  # ── Wait for Headscale ──────────────────────────────────────────────────────
  header "Waiting for Headscale to be ready"
  MAX_WAIT=60; WAITED=0
  until $COMPOSE_CMD exec -T headscale headscale version &>/dev/null; do
    sleep 2; WAITED=$((WAITED+2))
    [[ $WAITED -ge $MAX_WAIT ]] && die "Headscale did not start within ${MAX_WAIT}s. Check: $COMPOSE_CMD logs headscale"
    printf "."
  done
  echo; success "Headscale is ready"

  # ── Create Headscale user + pre-auth key ────────────────────────────────────
  header "Setting up Headscale"
  HEADSCALE_USER="meshploy"

  if ! $COMPOSE_CMD exec -T headscale headscale users list 2>/dev/null | grep -q "$HEADSCALE_USER"; then
    $COMPOSE_CMD exec -T headscale headscale users create "$HEADSCALE_USER" &>/dev/null
    success "Headscale user '${HEADSCALE_USER}' created"
  else
    success "Headscale user '${HEADSCALE_USER}' already exists"
  fi

  # v0.28+ --user flag requires numeric ID, not a username string
  HEADSCALE_USER_ID="$($COMPOSE_CMD exec -T headscale \
    headscale users list -o json 2>/dev/null \
    | python3 -c "
import sys, json
for u in json.load(sys.stdin):
    if u['name'] == '$HEADSCALE_USER':
        print(u['id']); break
" || true)"
  [[ -z "$HEADSCALE_USER_ID" ]] && die "Could not resolve Headscale user ID for '${HEADSCALE_USER}'"

  _PREAUTH_RAW="$($COMPOSE_CMD exec -T headscale \
    headscale preauthkeys create --user "$HEADSCALE_USER_ID" --expiration 1h --reusable \
    2>&1 || true)"
  PREAUTH_KEY="$(echo "$_PREAUTH_RAW" | grep -oE 'hskey-auth-[A-Za-z0-9_-]+' | head -1 || true)"
  if [[ -z "$PREAUTH_KEY" ]]; then
    error "headscale preauthkeys output was:"
    echo "$_PREAUTH_RAW" >&2
    die "Failed to generate pre-auth key. Check: $COMPOSE_CMD logs headscale"
  fi
  success "Pre-auth key generated (reusable, 1h): ${BOLD}${PREAUTH_KEY}${RESET}"

  _APIKEY_RAW="$($COMPOSE_CMD exec -T headscale headscale apikeys create 2>&1 || true)"
  HEADSCALE_API_KEY="$(echo "$_APIKEY_RAW" | grep -oE '[A-Za-z0-9_-]{40,}' | head -1 || true)"
  if [[ -z "$HEADSCALE_API_KEY" ]]; then
    error "headscale apikeys output was:"
    echo "$_APIKEY_RAW" >&2
    die "Failed to generate API key. Check: $COMPOSE_CMD logs headscale"
  fi
  sed -i "s|HEADSCALE_API_KEY=|HEADSCALE_API_KEY=${HEADSCALE_API_KEY}|" .env
  success "Headscale API key written to .env"

  # The API container started before the key was generated — recreate it so it
  # picks up the updated HEADSCALE_API_KEY from .env.
  info "Restarting API with Headscale credentials…"
  $COMPOSE_CMD up -d --force-recreate api
  success "API restarted"

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
  # Use localhost directly — Caddy/DNS not running yet (Phase 2 starts after this).
  # Headscale is already reachable at 127.0.0.1:8085 inside the container network.
  info "Connecting to http://127.0.0.1:8085 (direct, pre-DNS)…"
  tailscale up \
    --login-server="http://127.0.0.1:8085" \
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
  # Ensure external caddy volumes exist before compose up (suppresses the
  # "volume already exists but was not created by Docker Compose" warning).
  $CONTAINER_RUNTIME volume create meshploy_caddy_data  &>/dev/null || true
  $CONTAINER_RUNTIME volume create meshploy_caddy_config &>/dev/null || true
  DOMAIN="$DOMAIN" $COMPOSE_CMD up -d coredns caddy
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
  echo -e "    1. Headscale pre-auth key (valid 1h, reusable):"
  echo -e "       ${BOLD}${CYAN}${PREAUTH_KEY}${RESET}"
  echo -e "    2. Node registration token (shown in dashboard → Cluster):"
  echo -e "       Generate one at ${CYAN}https://app.${DOMAIN}${RESET} → Cluster → Add a worker node"
  echo -e "    3. k3s cluster join token (also shown in dashboard → Cluster):"
  echo -e "       ${BOLD}${CYAN}${K3S_TOKEN}${RESET}"
  echo -e "    Then on the worker machine run:"
  echo -e "    ${BOLD}curl -fsSL https://raw.githubusercontent.com/meshploy/meshploy/main/get.sh -o /tmp/get.sh${RESET}"
  echo -e "    ${BOLD}sudo bash /tmp/get.sh${RESET}"
  echo
  echo -e "  ${BOLD}Next steps${RESET}"
  echo -e "    1. Point your domain's NS records to this server (${PUBLIC_IP})"
  echo -e "    2. Verify DNS:  dig @${PUBLIC_IP} ${DOMAIN} A"
  echo -e "    3. Check TLS:   curl -I https://api.${DOMAIN}"
  echo -e "    4. Check mesh:  tailscale status"
  hr

  # ── Wait for TLS certificates ─────────────────────────────────────────────────
  # Shown after the summary so keys are always visible even if you Ctrl+C.
  # Caddy uses DNS-01 ACME challenges for wildcard certs (*.domain + *.internal.domain).
  # CoreDNS must propagate the _acme-challenge TXT records before Let's Encrypt
  # can verify them. This typically takes 1–3 minutes.
  echo
  echo -e "  ${YELLOW}Waiting for Caddy to obtain wildcard TLS certificates (DNS-01 ACME).${RESET}"
  echo -e "  ${YELLOW}This typically takes 1–3 minutes. Press Ctrl+C to skip — certs will${RESET}"
  echo -e "  ${YELLOW}finish in the background and the dashboard will be available shortly.${RESET}"
  echo
  TLS_MAX_WAIT=300   # 5 minutes
  TLS_INTERVAL=5
  TLS_WAITED=0
  TLS_OK=0
  while [[ $TLS_WAITED -lt $TLS_MAX_WAIT ]]; do
    if curl -sf -k --max-time 5 "https://app.${DOMAIN}" -o /dev/null 2>/dev/null; then
      if curl -sf --max-time 5 "https://app.${DOMAIN}" -o /dev/null 2>/dev/null; then
        TLS_OK=1
        break
      fi
    fi
    sleep $TLS_INTERVAL
    TLS_WAITED=$((TLS_WAITED + TLS_INTERVAL))
    MINS=$((TLS_WAITED / 60)); SECS=$((TLS_WAITED % 60))
    printf "\r  ${CYAN}→${RESET}  Waiting for TLS… %02d:%02d elapsed" "$MINS" "$SECS"
  done
  echo

  if [[ $TLS_OK -eq 1 ]]; then
    success "TLS certificates issued — ${CYAN}https://app.${DOMAIN}${RESET} is live!"
  else
    warn "TLS not confirmed after ${TLS_MAX_WAIT}s — still provisioning in background."
    warn "Monitor: $COMPOSE_CMD logs -f caddy"
  fi

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

  # ── Node registration token ─────────────────────────────────────────────────
  # After joining the mesh, the worker can reach the master API directly over
  # WireGuard (100.64.0.1:4000) without going through the public internet.
  # The registration token is generated in the Meshploy dashboard → Cluster.
  echo
  echo -e "  ${BOLD}Node registration token${RESET}"
  echo -e "  Find it in the Meshploy dashboard under ${CYAN}Cluster → Add a worker node${RESET}."
  echo
  ask_secret MESHPLOY_TOKEN "Node registration token (mreg-...)"

  MESHPLOY_API_URL="${MESHPLOY_API_URL:-http://100.64.0.1:4000}"
  ask MESHPLOY_API_URL "Meshploy API URL (mesh)" "$MESHPLOY_API_URL"

  # ── Join the mesh ───────────────────────────────────────────────────────────
  header "Joining the Meshploy mesh"
  info "Connecting to ${HEADSCALE_URL}…"

  # Warn if already connected to a *different* login server so the user can decide
  # whether to switch. --force-reauth is always passed unconditionally below because
  # tailscale refuses to re-authenticate with an authkey without it, even when the
  # login server hasn't changed.
  EXISTING_URL="$(tailscale status --json 2>/dev/null | grep -m1 '"LoginServerURL"' | cut -d'"' -f4 || true)"
  if [[ -n "$EXISTING_URL" && "$EXISTING_URL" != "$HEADSCALE_URL" ]]; then
    warn "This node is connected to a different network: ${EXISTING_URL}"
    warn "Switching to ${HEADSCALE_URL} — it will disconnect from the current network."
    warn "To rejoin it later: tailscale up --login-server=${EXISTING_URL} --force-reauth"
    echo
    if ! ask_yn "Switch networks and join the Meshploy mesh?"; then
      die "Aborted — node not joined to mesh."
    fi
  fi

  # --force-reauth re-authenticates silently with the authkey (no browser).
  # --reset clears any pre-existing non-default settings (e.g. exit-node,
  # shields-up) that would cause tailscale to reject the up command.
  sudo tailscale up \
    --login-server="$HEADSCALE_URL" \
    --authkey="$PREAUTH_KEY" \
    --hostname="$NODE_HOSTNAME" \
    --accept-routes \
    --force-reauth \
    --reset \
    || die "tailscale up failed — check: sudo tailscale status"

  # Wait up to 15 s for the WireGuard IP to be assigned.
  MESH_IP_ASSIGNED=""
  for _i in $(seq 1 15); do
    MESH_IP_ASSIGNED="$(tailscale ip -4 2>/dev/null || true)"
    [[ -n "$MESH_IP_ASSIGNED" ]] && break
    sleep 1
  done
  if [[ -z "$MESH_IP_ASSIGNED" ]]; then
    die "No mesh IP assigned after 15 s. Check: sudo tailscale status"
  fi
  success "Joined mesh as '${NODE_HOSTNAME}' — mesh IP: ${BOLD}${MESH_IP_ASSIGNED}${RESET}"

  # ── Node role selection ──────────────────────────────────────────────────────
  echo
  echo -e "  ${BOLD}Node scheduling role${RESET}"
  echo -e "  ${CYAN}1)${RESET} workload_builder ${DIM}(default)${RESET} — runs customer workloads AND build jobs"
  echo -e "  ${CYAN}2)${RESET} workload          — customer workloads only"
  echo -e "  ${CYAN}3)${RESET} builder           — build jobs only (tainted, workloads won't land here)"
  echo
  NODE_ROLE_CHOICE="1"
  ask NODE_ROLE_CHOICE "Choose role [1/2/3]" "1"
  case "$NODE_ROLE_CHOICE" in
    2) NODE_MESH_ROLE="workload" ;;
    3) NODE_MESH_ROLE="builder" ;;
    *) NODE_MESH_ROLE="workload_builder" ;;
  esac
  info "Node mesh role: ${BOLD}${NODE_MESH_ROLE}${RESET}"

  # ── Self-register with the Meshploy API ─────────────────────────────────────
  # Now on the mesh, so we can reach the master's API at its WireGuard IP.
  header "Registering node with Meshploy"

  # Wait for the API to be reachable over the WireGuard mesh.
  # After tailscale up, kernel routes may take a few seconds to propagate.
  _API_READY=0
  for _i in $(seq 1 12); do
    if curl -s --max-time 4 "${MESHPLOY_API_URL}/api/v1/auth/login" -o /dev/null 2>/dev/null; then
      _API_READY=1
      break
    fi
    [[ $_i -eq 1 ]] && info "Waiting for API to be reachable at ${MESHPLOY_API_URL}…"
    sleep 5
  done

  if [[ $_API_READY -eq 0 ]]; then
    warn "Cannot reach ${MESHPLOY_API_URL} after 60 s."
    warn "Diagnostics:"
    warn "  tailscale status:  $(tailscale status --json 2>/dev/null | python3 -c "import sys,json; s=json.load(sys.stdin); print('connected' if s.get('BackendState')=='Running' else s.get('BackendState','unknown'))" 2>/dev/null || echo 'unknown')"
    warn "  ping gateway:      $(ping -c1 -W2 100.64.0.1 &>/dev/null && echo 'ok' || echo 'FAILED')"
    warn "Auto-registration skipped. Register this node manually from the dashboard."
  else
    # Note: -f is intentionally omitted so HTTP error bodies (e.g. 401 invalid token)
    # are captured in _REG_RESPONSE and shown to the user instead of silently discarded.
    _REG_RESPONSE="$(curl -s \
      --max-time 15 \
      -X POST "${MESHPLOY_API_URL}/api/v1/nodes/self-register" \
      -H "Content-Type: application/json" \
      -d "{\"token\":\"${MESHPLOY_TOKEN}\",\"name\":\"${NODE_HOSTNAME}\",\"tailscale_ip\":\"${MESH_IP_ASSIGNED}\",\"mesh_role\":\"${NODE_MESH_ROLE}\"}" \
      2>&1 || true)"

    _NODE_ID="$(echo "$_REG_RESPONSE" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('id',''))" 2>/dev/null || true)"
    if [[ -n "$_NODE_ID" ]]; then
      success "Node '${NODE_HOSTNAME}' registered in Meshploy (role: ${NODE_MESH_ROLE})"
      # Save node identity so uninstall.sh can self-deregister via the API.
      mkdir -p /etc/meshploy
      cat > /etc/meshploy/node.conf <<NODECONF
NODE_ID=${_NODE_ID}
NODE_NAME=${NODE_HOSTNAME}
MESHPLOY_API_URL=${MESHPLOY_API_URL}
MESHPLOY_TOKEN=${MESHPLOY_TOKEN}
NODECONF
      chmod 600 /etc/meshploy/node.conf
      success "Node identity saved to /etc/meshploy/node.conf"
    else
      warn "Auto-registration failed. You can register manually in the dashboard."
      warn "API response: ${_REG_RESPONSE:-<no response>}"
    fi
  fi

  # ── Optionally join k3s cluster ─────────────────────────────────────────────
  echo
  if ask_yn "Join this node to the k3s cluster now?"; then
    ask_secret K3S_JOIN_TOKEN "k3s node token (from master summary or dashboard → Cluster)"
    K3S_SERVER_URL="${K3S_SERVER_URL:-https://100.64.0.1:6443}"
    ask K3S_SERVER_URL "k3s server URL" "$K3S_SERVER_URL"

    # Extract host+port from the server URL to test reachability before installing.
    _K3S_HOST="$(echo "$K3S_SERVER_URL" | sed 's|https\?://||' | cut -d/ -f1 | cut -d: -f1)"
    _K3S_PORT="$(echo "$K3S_SERVER_URL" | sed 's|https\?://||' | cut -d/ -f1 | cut -s -d: -f2)"
    _K3S_PORT="${_K3S_PORT:-6443}"

    if ! (echo >/dev/tcp/"$_K3S_HOST"/"$_K3S_PORT") 2>/dev/null; then
      warn "Cannot reach k3s server at ${_K3S_HOST}:${_K3S_PORT} over the mesh."
      warn "Diagnostics:"
      warn "  ping gateway: $(ping -c1 -W2 "$_K3S_HOST" &>/dev/null && echo 'ok' || echo 'FAILED')"
      warn "  If ping fails, the WireGuard route to the master is not yet active."
      warn "  Try: sudo tailscale status   and   sudo tailscale ping ${_K3S_HOST}"
      if ! ask_yn "Proceed anyway?"; then
        info "Skipped. Re-run the script or join manually later."
      else
        _do_k3s_join=1
      fi
    else
      _do_k3s_join=1
    fi

    if [[ "${_do_k3s_join:-0}" -eq 1 ]]; then
      header "Joining k3s cluster"
      info "Installing k3s agent and joining ${K3S_SERVER_URL}…"
      if ! curl -sfL https://get.k3s.io | \
          K3S_URL="$K3S_SERVER_URL" \
          K3S_TOKEN="$K3S_JOIN_TOKEN" \
          K3S_NODE_NAME="$NODE_HOSTNAME" \
          sh -s - agent \
            --node-ip="${MESH_IP_ASSIGNED}"; then
        error "k3s agent install failed."
        warn "Last log lines:"
        journalctl -u k3s-agent --no-pager -n 20 2>/dev/null || true
        die "Fix the error above and re-run: curl -sfL https://get.k3s.io | K3S_URL=\"${K3S_SERVER_URL}\" K3S_TOKEN=\"<token>\" sh -s - agent"
      fi

      # Wait for the agent to come up
      MAX_K3S_WAIT=30; K3S_WAITED=0
      until systemctl is-active --quiet k3s-agent 2>/dev/null; do
        sleep 2; K3S_WAITED=$((K3S_WAITED+2))
        [[ $K3S_WAITED -ge $MAX_K3S_WAIT ]] && { warn "k3s-agent not active yet — check: journalctl -u k3s-agent"; break; }
        printf "."
      done
      echo
      systemctl is-active --quiet k3s-agent \
        && success "k3s agent is running — node joined the cluster" \
        || warn "k3s agent may still be starting. Check: systemctl status k3s-agent"

      # ── Configure containerd to trust the built-in registry (HTTP) ───────────
      # _K3S_HOST is the gateway mesh IP extracted from K3S_SERVER_URL above.
      info "Configuring containerd registry mirror for built-in registry…"
      mkdir -p /etc/rancher/k3s
      cat > /etc/rancher/k3s/registries.yaml <<REGEOF
mirrors:
  "${_K3S_HOST}:5000":
    endpoint:
      - "http://${_K3S_HOST}:5000"
REGEOF
      success "Wrote /etc/rancher/k3s/registries.yaml (mirror: ${_K3S_HOST}:5000)"
      systemctl restart k3s-agent
      success "k3s-agent restarted to pick up registry config"
    fi
  else
    info "Skipped. You can join the cluster later from the Meshploy dashboard → Cluster."
  fi

  echo
  hr
  echo -e "  ${BOLD}${GREEN}✔  Worker node is ready!${RESET}"
  hr
  echo -e "  ${BOLD}Node${RESET}       ${CYAN}${NODE_HOSTNAME}${RESET}"
  echo -e "  ${BOLD}Mesh IP${RESET}    ${CYAN}${MESH_IP_ASSIGNED}${RESET}"
  echo
  echo -e "  ${BOLD}Next steps${RESET}"
  echo -e "    1. Check connectivity from master:  tailscale ping ${NODE_HOSTNAME}"
  echo -e "    2. View node in dashboard:          ${CYAN}Nodes → ${NODE_HOSTNAME}${RESET}"
  hr

fi
