#!/usr/bin/env bash
# Meshploy — unified entry point
#
#   Public repo:
#     curl -fsSL https://raw.githubusercontent.com/meshploy/meshploy/main/get.sh \
#       -o /tmp/get.sh && sudo bash /tmp/get.sh
#
#   Private repo (while in development):
#     export GITHUB_PAT=ghp_xxxx
#     curl -fsSL "https://${GITHUB_PAT}@raw.githubusercontent.com/meshploy/meshploy/main/get.sh" \
#       -o /tmp/get.sh && GITHUB_PAT=$GITHUB_PAT sudo -E bash /tmp/get.sh
#
#   Flags:
#     --reinstall   wipe and reinstall
#     --uninstall   remove Meshploy
#
set -euo pipefail
exec < /dev/tty

INSTALL_DIR="/opt/meshploy"
BRANCH="${MESHPLOY_BRANCH:-main}"

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

# ── Download deploy/ via tarball ──────────────────────────────────────────────
# Only the deploy/ directory is needed — source code ships in Docker images.
# curl + tar are always available; no git required.

if [[ -n "${GITHUB_PAT:-}" ]]; then
  TARBALL_URL="https://${GITHUB_PAT}@api.github.com/repos/meshploy/meshploy/tarball/${BRANCH}"
  AUTH_HEADER="Authorization: token ${GITHUB_PAT}"
else
  TARBALL_URL="https://api.github.com/repos/meshploy/meshploy/tarball/${BRANCH}"
  AUTH_HEADER=""
fi

mkdir -p "$INSTALL_DIR"

info "Downloading Meshploy deploy config (branch: ${BRANCH})…"
if [[ -n "$AUTH_HEADER" ]]; then
  curl -fsSL -H "$AUTH_HEADER" "$TARBALL_URL" \
    | tar -xz --strip-components=2 -C "$INSTALL_DIR" --wildcards "*/deploy"
else
  curl -fsSL "$TARBALL_URL" \
    | tar -xz --strip-components=2 -C "$INSTALL_DIR" --wildcards "*/deploy"
fi
success "Deploy config ready at ${INSTALL_DIR}/"

cd "$INSTALL_DIR"

# ── Dispatch ──────────────────────────────────────────────────────────────────
case "$MODE" in
  install|reinstall)
    if ! command -v docker &>/dev/null; then
      info "Installing Docker…"
      curl -fsSL https://get.docker.com | sh
      systemctl enable --now docker
      success "Docker installed."
    fi
    [[ "$MODE" == "reinstall" ]] && exec bash uninstall.sh --reinstall
    exec bash install.sh
    ;;
  uninstall)
    exec bash uninstall.sh
    ;;
esac
