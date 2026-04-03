#!/usr/bin/env bash
# Meshploy — unified entry point
#
#   Install:    sudo bash get.sh
#   Uninstall:  sudo bash get.sh --uninstall
#   Reinstall:  sudo bash get.sh --reinstall
#
set -euo pipefail
exec < /dev/tty

REPO_BASE="https://github.com/meshploy/meshploy"
INSTALL_DIR="/opt/meshploy"

RED='\033[0;31m'; GREEN='\033[0;32m'; CYAN='\033[0;36m'; BOLD='\033[1m'; RESET='\033[0m'
info()    { echo -e "${CYAN}  →${RESET}  $*"; }
success() { echo -e "${GREEN}  ✔${RESET}  $*"; }
die()     { echo -e "${RED}  ✘${RESET}  $*" >&2; exit 1; }

MODE="install"
for arg in "$@"; do
  case "$arg" in
    --uninstall) MODE="uninstall" ;;
    --reinstall) MODE="reinstall" ;;
  esac
done

[[ "$(uname -s)" != "Linux" ]] && die "Meshploy requires Linux."
[[ "$EUID" -ne 0 ]] && die "Please run as root: sudo bash get.sh"

# ── Git ───────────────────────────────────────────────────────────────────────
if ! command -v git &>/dev/null; then
  info "Installing git..."
  apt-get update -qq && apt-get install -y -qq git \
    || yum install -y git \
    || die "Could not install git. Install it manually and re-run."
  success "git installed."
fi

# ── Repo URL (PAT optional — required only while repo is private) ─────────────
if [[ -n "${GITHUB_PAT:-}" ]]; then
  REPO="https://${GITHUB_PAT}@github.com/meshploy/meshploy"
else
  REPO="$REPO_BASE"
fi

# ── Clone / update repo ───────────────────────────────────────────────────────
export GIT_TERMINAL_PROMPT=0
if [[ -d "$INSTALL_DIR/.git" ]]; then
  info "Updating ${INSTALL_DIR}..."
  git -C "$INSTALL_DIR" remote set-url origin "$REPO"
  git -C "$INSTALL_DIR" fetch --quiet origin main
  git -C "$INSTALL_DIR" checkout --quiet origin/main -- .
  success "Updated."
else
  info "Cloning Meshploy into ${INSTALL_DIR}..."
  git clone --depth 1 "$REPO" "$INSTALL_DIR"
  success "Cloned."
fi

cd "$INSTALL_DIR/deploy"

# ── Dispatch ──────────────────────────────────────────────────────────────────
case "$MODE" in
  install)
    # Docker only needed for install/reinstall
    if ! command -v docker &>/dev/null; then
      info "Installing Docker..."
      curl -fsSL https://get.docker.com | sh
      systemctl enable --now docker
      success "Docker installed."
    fi
    exec bash install.sh
    ;;
  uninstall)
    exec bash uninstall.sh
    ;;
  reinstall)
    exec bash uninstall.sh --reinstall
    ;;
esac
