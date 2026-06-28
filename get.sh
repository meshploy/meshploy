#!/usr/bin/env bash
# Meshploy — unified entry point
#
#   Stable install (default):
#     sudo bash -c "$(curl -fsSL https://raw.githubusercontent.com/meshploy/meshploy/main/get.sh)"
#
#   Edge install (main branch builds):
#     sudo bash -c "$(curl -fsSL https://raw.githubusercontent.com/meshploy/meshploy/main/get.sh)" _ --edge
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
#     sudo bash -c "$(curl -fsSL URL)" _ --edge        # use edge builds from main
#
set -euo pipefail

INSTALL_DIR="/opt/meshploy"
CLI_BIN="/usr/local/bin/meshploy"
REPO="meshploy/meshploy"

RED='\033[0;31m'; GREEN='\033[0;32m'; CYAN='\033[0;36m'; BOLD='\033[1m'; RESET='\033[0m'
info()    { echo -e "${CYAN}  →${RESET}  $*"; }
success() { echo -e "${GREEN}  ✔${RESET}  $*"; }
die()     { echo -e "${RED}  ✘${RESET}  $*" >&2; exit 1; }

MODE="install"
WIPE_DATA=false
CLI_ONLY=false
EDGE=false
for arg in "$@"; do
  case "$arg" in
    --uninstall)  MODE="uninstall" ;;
    --reinstall)  MODE="reinstall" ;;
    --wipe-data)  WIPE_DATA=true ;;
    --cli-only)   CLI_ONLY=true ;;
    --edge)       EDGE=true ;;
  esac
done

# MESHPLOY_BRANCH env var overrides channel (kept for backwards compatibility).
if [[ -n "${MESHPLOY_BRANCH:-}" ]]; then
  EDGE=true
  BRANCH="$MESHPLOY_BRANCH"
fi

[[ "$(uname -s)" != "Linux" ]] && die "Meshploy requires Linux."
[[ "$EUID" -ne 0 ]] && die "Please run as root: sudo bash get.sh"

# ── Detect architecture ───────────────────────────────────────────────────────
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64)  CLI_ARCH="amd64" ;;
  aarch64) CLI_ARCH="arm64" ;;
  *)       die "Unsupported architecture: $ARCH" ;;
esac

# ── Resolve channel ───────────────────────────────────────────────────────────
if [[ -n "${GITHUB_PAT:-}" ]]; then
  AUTH_HEADER="Authorization: token ${GITHUB_PAT}"
else
  AUTH_HEADER=""
fi

if $EDGE; then
  CLI_RELEASE_URL="https://api.github.com/repos/${REPO}/releases/tags/cli-latest"
  BRANCH="${BRANCH:-main}"
  MESHPLOY_CHANNEL="main"
  info "Channel: edge (main)"
else
  CLI_RELEASE_URL="https://api.github.com/repos/${REPO}/releases/latest"
  MESHPLOY_CHANNEL="latest"
  # Resolve latest stable release tag for the deploy config tarball.
  if [[ -n "$AUTH_HEADER" ]]; then
    LATEST_TAG=$(curl -fsSL -H "$AUTH_HEADER" "https://api.github.com/repos/${REPO}/releases/latest" \
      | python3 -c "import json,sys; print(json.load(sys.stdin)['tag_name'])" 2>/dev/null \
      || echo "main")
  else
    LATEST_TAG=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
      | python3 -c "import json,sys; print(json.load(sys.stdin)['tag_name'])" 2>/dev/null \
      || echo "main")
  fi
  BRANCH="$LATEST_TAG"
  info "Channel: stable (${LATEST_TAG})"
fi

# ── Download CLI binary ───────────────────────────────────────────────────────
info "Downloading Meshploy CLI (linux/${CLI_ARCH})…"
if [[ -n "$AUTH_HEADER" ]]; then
  RELEASE_JSON=$(curl -fsSL -H "$AUTH_HEADER" "$CLI_RELEASE_URL")
else
  RELEASE_JSON=$(curl -fsSL "$CLI_RELEASE_URL")
fi

# Extract the API asset URL for authenticated downloads.
# Private repos: browser_download_url redirects through S3 which drops the
# Authorization header → 404. The API asset URL works correctly with the token.
TARGET_ASSET="meshploy-linux-${CLI_ARCH}"
if command -v python3 &>/dev/null; then
  ASSET_URL=$(printf '%s' "$RELEASE_JSON" | python3 -c "
import json, sys
assets = json.load(sys.stdin).get('assets', [])
match = next((a for a in assets if a['name'] == '${TARGET_ASSET}'), None)
if match:
    print(match.get('url') or match.get('browser_download_url', ''))
" 2>/dev/null || true)
else
  ASSET_URL=$(printf '%s' "$RELEASE_JSON" \
    | grep -o "\"url\":\"https://api\.github\.com/repos/[^\"]*assets/[0-9]*\"" \
    | grep -o 'https://[^"]*' | head -1 || true)
fi

if [[ -z "${ASSET_URL:-}" ]]; then
  die "Could not find a CLI release asset for linux/${CLI_ARCH}. \
Is this a development branch? Set MESHPLOY_BRANCH or use --edge."
fi

if [[ -n "$AUTH_HEADER" ]]; then
  curl -fsSL --connect-timeout 15 --max-time 120 \
    -H "$AUTH_HEADER" -H "Accept: application/octet-stream" -L -o "$CLI_BIN" "$ASSET_URL" \
    || die "CLI download failed. Check your connection and retry."
else
  curl -fsSL --connect-timeout 15 \
    -H "Accept: application/octet-stream" -L -o "$CLI_BIN" "$ASSET_URL" \
    || die "CLI download failed. Check your connection and retry."
fi
chmod +x "$CLI_BIN"
success "meshploy CLI installed at ${CLI_BIN}"

# ── CLI-only mode — stop here ─────────────────────────────────────────────────
if $CLI_ONLY; then
  echo
  echo -e "  ${BOLD}$("$CLI_BIN" version 2>/dev/null || true)${RESET}"
  echo -e "  Run ${BOLD}meshploy --help${RESET} to get started."
  exit 0
fi

# ── Download deploy/ via tarball ──────────────────────────────────────────────
# Only the deploy/ directory is needed — source code ships in Docker images.
# curl + tar are always available; no git required.

TARBALL_URL="https://api.github.com/repos/${REPO}/tarball/${BRANCH}"

mkdir -p "$INSTALL_DIR"

info "Downloading Meshploy deploy config (${BRANCH})…"
if [[ -n "$AUTH_HEADER" ]]; then
  curl -fsSL --connect-timeout 15 -H "$AUTH_HEADER" "$TARBALL_URL" \
    | tar -xz --strip-components=2 -C "$INSTALL_DIR" --wildcards "*/deploy" \
    || die "Deploy config download failed. Check your connection and retry."
else
  curl -fsSL --connect-timeout 15 "$TARBALL_URL" \
    | tar -xz --strip-components=2 -C "$INSTALL_DIR" --wildcards "*/deploy" \
    || die "Deploy config download failed. Check your connection and retry."
fi
success "Deploy config ready at ${INSTALL_DIR}/"

cd "$INSTALL_DIR"

# ── Dispatch via CLI ──────────────────────────────────────────────────────────
case "$MODE" in
  install|reinstall)
    export MESHPLOY_CHANNEL
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
