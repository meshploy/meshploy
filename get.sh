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
#     sudo bash -c "$(curl -fsSL URL)" _ --dns-mode=ondemand  # self-managed DNS
#                                                     # (wildcard A record, no NS delegation)
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
DNS_MODE_FLAG=""
for arg in "$@"; do
  case "$arg" in
    --uninstall)  MODE="uninstall" ;;
    --reinstall)  MODE="reinstall" ;;
    --wipe-data)  WIPE_DATA=true ;;
    --cli-only)   CLI_ONLY=true ;;
    --edge)       EDGE=true ;;
    --dns-mode=*)
      DNS_MODE_FLAG="${arg#*=}"
      case "$DNS_MODE_FLAG" in
        delegation|ondemand) ;;
        *) die "--dns-mode must be 'delegation' or 'ondemand', got '${DNS_MODE_FLAG}'" ;;
      esac
      ;;
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

fetch_asset() {  # fetch_asset <url> <output file>
  if [[ -n "$AUTH_HEADER" ]]; then
    curl -fsSL --connect-timeout 15 --max-time 120 \
      -H "$AUTH_HEADER" -H "Accept: application/octet-stream" -L -o "$2" "$1"
  else
    curl -fsSL --connect-timeout 15 --max-time 120 \
      -H "Accept: application/octet-stream" -L -o "$2" "$1"
  fi
}

# Downloaded beside the binary and moved into place only once it checks out,
# so a bad download never replaces a working CLI.
CLI_TMP="$(mktemp "${CLI_BIN}.XXXXXX")"
fetch_asset "$ASSET_URL" "$CLI_TMP" \
  || { rm -f "$CLI_TMP"; die "CLI download failed. Check your connection and retry."; }

# Checked against the release's SHA256SUMS when it publishes one. The file
# comes from the same release, so this catches a corrupted or truncated
# download, not a compromised release.
SUMS_URL=""
if command -v python3 &>/dev/null; then
  SUMS_URL=$(printf '%s' "$RELEASE_JSON" | python3 -c "
import json, sys
assets = json.load(sys.stdin).get('assets', [])
match = next((a for a in assets if a['name'] == 'SHA256SUMS'), None)
if match:
    print(match.get('url') or match.get('browser_download_url', ''))
" 2>/dev/null || true)
fi
if [[ -n "$SUMS_URL" ]] && command -v sha256sum &>/dev/null; then
  SUMS_TMP="$(mktemp)"
  fetch_asset "$SUMS_URL" "$SUMS_TMP" \
    || { rm -f "$CLI_TMP" "$SUMS_TMP"; die "Could not download the release's SHA256SUMS. Check your connection and retry."; }
  WANT=$(awk -v f="$TARGET_ASSET" '$2 == f || $2 == "*" f { print $1 }' "$SUMS_TMP")
  rm -f "$SUMS_TMP"
  GOT=$(sha256sum "$CLI_TMP" | awk '{ print $1 }')
  if [[ -z "$WANT" || "$WANT" != "$GOT" ]]; then
    rm -f "$CLI_TMP"
    die "The downloaded CLI does not match the release's SHA256SUMS. Nothing was installed; retry the download."
  fi
  success "CLI download verified against SHA256SUMS"
else
  warn "This release publishes no SHA256SUMS, so the CLI download was not verified."
fi

chmod +x "$CLI_TMP"
mv -f "$CLI_TMP" "$CLI_BIN"
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
    # Built as an array so each flag stays one argument, and so a fresh install
    # can carry --dns-mode too — it is the only way to choose the self-managed
    # DNS path, and it was previously reachable on neither path.
    EXTRA_FLAGS=()
    if [[ "$MODE" == "reinstall" ]]; then
      EXTRA_FLAGS+=("--reinstall")
      $WIPE_DATA && EXTRA_FLAGS+=("--wipe-data")
    fi
    [[ -n "$DNS_MODE_FLAG" ]] && EXTRA_FLAGS+=("--dns-mode=${DNS_MODE_FLAG}")
    # Expanding an empty array under `set -u` is an error on bash < 4.4, which
    # is still what ships on older LTS images, so branch on the length.
    if [[ ${#EXTRA_FLAGS[@]} -gt 0 ]]; then
      exec "$CLI_BIN" node install "${EXTRA_FLAGS[@]}"
    fi
    exec "$CLI_BIN" node install
    ;;
  uninstall)
    exec "$CLI_BIN" node uninstall
    ;;
esac
