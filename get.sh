#!/usr/bin/env bash
# Meshploy — one-line installer
#
#   curl -fsSL https://raw.githubusercontent.com/meshploy/meshploy/main/get.sh | bash
#
set -euo pipefail

REPO="https://github.com/meshploy/meshploy"
INSTALL_DIR="/opt/meshploy"

RED='\033[0;31m'; GREEN='\033[0;32m'; CYAN='\033[0;36m'; BOLD='\033[1m'; RESET='\033[0m'
info()    { echo -e "${CYAN}  →${RESET}  $*"; }
success() { echo -e "${GREEN}  ✔${RESET}  $*"; }
die()     { echo -e "${RED}  ✘${RESET}  $*" >&2; exit 1; }

[[ "$(uname -s)" != "Linux" ]] && die "Meshploy requires Linux."
[[ "$EUID" -ne 0 ]] && die "Please run as root: sudo bash <(curl -fsSL ...)"

# ── Docker ────────────────────────────────────────────────────────────────────
if ! command -v docker &>/dev/null; then
  info "Installing Docker..."
  curl -fsSL https://get.docker.com | sh
  systemctl enable --now docker
  success "Docker installed."
fi

# ── Git ───────────────────────────────────────────────────────────────────────
if ! command -v git &>/dev/null; then
  info "Installing git..."
  apt-get update -qq && apt-get install -y -qq git \
    || yum install -y git \
    || die "Could not install git. Install it manually and re-run."
  success "git installed."
fi

# ── Clone / update repo ───────────────────────────────────────────────────────
if [[ -d "$INSTALL_DIR/.git" ]]; then
  info "Updating existing installation at ${INSTALL_DIR}..."
  git -C "$INSTALL_DIR" pull --quiet --ff-only
  success "Updated."
else
  info "Cloning Meshploy into ${INSTALL_DIR}..."
  git clone --depth 1 "$REPO" "$INSTALL_DIR"
  success "Cloned."
fi

# ── Hand off to the interactive installer ────────────────────────────────────
cd "$INSTALL_DIR/deploy"
exec bash install.sh
