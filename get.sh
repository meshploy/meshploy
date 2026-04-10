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
#     sudo bash -c "$(curl -fsSL URL)" _ --cli-only   # install/update CLI binary only
#
set -euo pipefail

INSTALL_DIR="/opt/meshploy"
BRANCH="${MESHPLOY_BRANCH:-main}"
CLI_BIN="/usr/local/bin/meshploy"
REPO="meshploy/meshploy"

RED='\033[0;31m'; GREEN='\033[0;32m'; CYAN='\033[0;36m'; BOLD='\033[1m'; RESET='\033[0m'
info()    { echo -e "${CYAN}  →${RESET}  $*"; }
success() { echo -e "${GREEN}  ✔${RESET}  $*"; }
die()     { echo -e "${RED}  ✘${RESET}  $*" >&2; exit 1; }

MODE="install"
WIPE_DATA=false
CLI_ONLY=false
for arg in "$@"; do
  case "$arg" in
    --uninstall)  MODE="uninstall" ;;
    --reinstall)  MODE="reinstall" ;;
    --wipe-data)  WIPE_DATA=true ;;
    --cli-only)   CLI_ONLY=true ;;
  esac
done

[[ "$(uname -s)" != "Linux" ]] && die "Meshploy requires Linux."
[[ "$EUID" -ne 0 ]] && die "Please run as root: sudo bash get.sh"

# ── Detect architecture ───────────────────────────────────────────────────────
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64)  CLI_ARCH="amd64" ;;
  aarch64) CLI_ARCH="arm64" ;;
  *)       die "Unsupported architecture: $ARCH" ;;
esac

# ── Download CLI binary ───────────────────────────────────────────────────────
if [[ -n "${GITHUB_PAT:-}" ]]; then
  CLI_URL="https://${GITHUB_PAT}@api.github.com/repos/${REPO}/releases/tags/cli-latest"
  AUTH_HEADER="Authorization: token ${GITHUB_PAT}"
else
  CLI_URL="https://api.github.com/repos/${REPO}/releases/tags/cli-latest"
  AUTH_HEADER=""
fi

info "Downloading Meshploy CLI (linux/${CLI_ARCH})…"
if [[ -n "$AUTH_HEADER" ]]; then
  ASSET_URL=$(curl -fsSL -H "$AUTH_HEADER" "$CLI_URL" \
    | grep -o "\"browser_download_url\":[[:space:]]*\"[^\"]*meshploy-linux-${CLI_ARCH}\"" \
    | grep -o 'https://[^"]*' || true)
else
  ASSET_URL=$(curl -fsSL "$CLI_URL" \
    | grep -o "\"browser_download_url\":[[:space:]]*\"[^\"]*meshploy-linux-${CLI_ARCH}\"" \
    | grep -o 'https://[^"]*' || true)
fi

if [[ -z "${ASSET_URL:-}" ]]; then
  die "Could not find a CLI release asset for linux/${CLI_ARCH}. \
Is this a development branch? Set MESHPLOY_BRANCH or download manually."
fi

if [[ -n "$AUTH_HEADER" ]]; then
  curl -fsSL -H "$AUTH_HEADER" -H "Accept: application/octet-stream" -L -o "$CLI_BIN" "$ASSET_URL"
else
  curl -fsSL -L -o "$CLI_BIN" "$ASSET_URL"
fi
chmod +x "$CLI_BIN"
success "meshploy CLI installed at ${CLI_BIN}"

# ── CLI-only mode — stop here ─────────────────────────────────────────────────
if $CLI_ONLY; then
  echo
  echo -e "  ${BOLD}meshploy$(${CLI_BIN} --version 2>/dev/null || true)${RESET}"
  echo -e "  Run ${BOLD}meshploy --help${RESET} to get started."
  exit 0
fi

# ── Download deploy/ via tarball ──────────────────────────────────────────────
# Only the deploy/ directory is needed — source code ships in Docker images.
# curl + tar are always available; no git required.

if [[ -n "${GITHUB_PAT:-}" ]]; then
  TARBALL_URL="https://${GITHUB_PAT}@api.github.com/repos/${REPO}/tarball/${BRANCH}"
else
  TARBALL_URL="https://api.github.com/repos/${REPO}/tarball/${BRANCH}"
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

# ── Dispatch via CLI ──────────────────────────────────────────────────────────
case "$MODE" in
  install|reinstall)
    if [[ "$MODE" == "reinstall" ]]; then
      EXTRA_FLAGS="--reinstall"
      $WIPE_DATA && EXTRA_FLAGS="$EXTRA_FLAGS --wipe-data"
      exec "$CLI_BIN" node install $EXTRA_FLAGS
    fi
    exec "$CLI_BIN" node install
    ;;
  uninstall)
    exec "$CLI_BIN" node uninstall
    ;;
esac
