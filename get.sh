#!/usr/bin/env bash
# Meshploy — unified entry point
#
#   Public repo:
#     sudo bash -c "$(curl -fsSL https://raw.githubusercontent.com/meshploy/meshploy/main/get.sh)"
#
#   Private repo (while in development):
#     export GITHUB_PAT=ghp_xxxx
#     sudo -E bash -c "$(curl -fsSL "https://${GITHUB_PAT}@raw.githubusercontent.com/meshploy/meshploy/main/get.sh")"
#
#   Flags (append after a bare --):
#     sudo bash -c "$(curl -fsSL URL)" _ --reinstall
#     sudo bash -c "$(curl -fsSL URL)" _ --reinstall --wipe-data
#     sudo bash -c "$(curl -fsSL URL)" _ --uninstall
#
set -euo pipefail

INSTALL_DIR="/opt/meshploy"
BRANCH="${MESHPLOY_BRANCH:-main}"

RED='\033[0;31m'; GREEN='\033[0;32m'; CYAN='\033[0;36m'; BOLD='\033[1m'; RESET='\033[0m'
info()    { echo -e "${CYAN}  →${RESET}  $*"; }
success() { echo -e "${GREEN}  ✔${RESET}  $*"; }
die()     { echo -e "${RED}  ✘${RESET}  $*" >&2; exit 1; }

MODE="install"
WIPE_DATA=false
for arg in "$@"; do
  case "$arg" in
    --uninstall)  MODE="uninstall" ;;
    --reinstall)  MODE="reinstall" ;;
    --wipe-data)  WIPE_DATA=true ;;
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
    if [[ "$MODE" == "reinstall" ]]; then
      EXTRA_FLAGS="--reinstall"
      $WIPE_DATA && EXTRA_FLAGS="$EXTRA_FLAGS --wipe-data"
      exec bash install.sh $EXTRA_FLAGS
    fi
    exec bash install.sh
    ;;
  uninstall)
    exec bash uninstall.sh
    ;;
esac
