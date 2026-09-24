#!/bin/bash
# =============================================================================
#  Meshploy: join a Mac to the mesh as a mesh-only node
#
#  A Mac cannot be a Kubernetes node, so it joins the WireGuard mesh and
#  nothing else: routes reach its ports over the mesh (Node + port targets),
#  and the machine itself stays unreachable from the internet.
#
#  Run with the command the console gives (Cluster -> Add a node -> Mesh only
#  -> macOS), which carries a single-use provisioning token:
#
#    curl -fsSL https://api.<your-domain>/join/macos.sh | sudo bash -s -- --token=mprov-...
#
#  Leave the mesh and remove the node from Meshploy:
#
#    curl -fsSL https://api.<your-domain>/join/macos.sh | sudo bash -s -- --uninstall
#
#  Written for the bash 3.2 that ships with macOS, and for a Mac with nothing
#  extra installed: JSON is read with osascript, since python3 on a fresh Mac
#  is only a stub that asks to install the developer tools.
# =============================================================================
set -euo pipefail

PROVISION_TOKEN=""
NODE_NAME=""
UNINSTALL=false
# The gateway fills this line in with its own address when it serves the
# script at /join/macos.sh. Matched literally by ServeJoinScript.
MESHPLOY_API_BASE="${MESHPLOY_API_BASE:-}"
for arg in "$@"; do
  case "$arg" in
    --token=*)   PROVISION_TOKEN="${arg#*=}" ;;
    --api=*)     MESHPLOY_API_BASE="${arg#*=}" ;;
    --name=*)    NODE_NAME="${arg#*=}" ;;
    --uninstall) UNINSTALL=true ;;
    *) echo "join/macos.sh: unknown argument '${arg}'" >&2; exit 1 ;;
  esac
done

CONF_DIR="/etc/meshploy"
CONF="${CONF_DIR}/node.conf"
NE_VERSION="1.8.2"
NE_BIN="/usr/local/bin/node_exporter"
NE_LABEL="io.meshploy.node-exporter"
NE_PLIST="/Library/LaunchDaemons/${NE_LABEL}.plist"

GREEN='\033[0;32m'; YELLOW='\033[1;33m'; RED='\033[0;31m'; CYAN='\033[0;36m'; BOLD='\033[1m'; RESET='\033[0m'
info()    { echo -e "${CYAN}  →${RESET}  $*"; }
success() { echo -e "${GREEN}  ✔${RESET}  $*"; }
warn()    { echo -e "${YELLOW}  !${RESET}  $*"; }
die()     { echo -e "${RED}  ✖${RESET}  $*" >&2; exit 1; }
header()  { echo; echo -e "${BOLD}$*${RESET}"; }

# json_get <json> <key>: one top-level string field, or empty.
json_get() {
  osascript -l JavaScript -e 'function run(a){try{var v=JSON.parse(a[0])[a[1]];return v==null?"":String(v)}catch(e){return ""}}' "$1" "$2" 2>/dev/null || true
}

[[ "$(uname -s)" == "Darwin" ]] || die "This script is for macOS. On Linux, use install.sh; on Windows, join/windows.ps1."
[[ "$(id -u)" -eq 0 ]] || die "Run it with sudo: it installs a system service and joins a network."

# ── Tailscale ────────────────────────────────────────────────────────────────
# Two ways a Mac runs it, and they cannot run side by side. The Tailscale app
# (App Store or standalone) is used if it is there, but it only runs while
# someone is logged in. Otherwise the open-source tailscaled is installed from
# Homebrew as a system daemon, which runs from boot with nobody logged in -
# what a machine serving the mesh wants.
TS_APP="/Applications/Tailscale.app/Contents/MacOS/Tailscale"
find_tailscale() {
  if [[ -x "$TS_APP" ]]; then
    TS="$TS_APP"; TS_KIND="app"
  elif command -v tailscale >/dev/null 2>&1; then
    TS="$(command -v tailscale)"; TS_KIND="daemon"
  else
    TS=""; TS_KIND=""
  fi
}

# ── Leave ────────────────────────────────────────────────────────────────────
if $UNINSTALL; then
  header "Leaving the Meshploy mesh"
  if [[ -f "$CONF" ]]; then
    NODE_ID="$(grep -E '^NODE_ID=' "$CONF" | cut -d= -f2- || true)"
    API_URL="$(grep -E '^MESHPLOY_API_URL=' "$CONF" | cut -d= -f2- || true)"
    NODE_SECRET="$(grep -E '^NODE_SECRET=' "$CONF" | cut -d= -f2- || true)"
    # Before logging out: the API is reached over the mesh.
    if [[ -n "$NODE_ID" && -n "$API_URL" && -n "$NODE_SECRET" ]]; then
      _STATUS="$(curl -s -o /dev/null -w '%{http_code}' --max-time 10 \
        -X DELETE "${API_URL}/api/v1/nodes/self-deregister" \
        -H "Content-Type: application/json" \
        -d "{\"node_secret\":\"${NODE_SECRET}\",\"node_id\":\"${NODE_ID}\"}" || echo 000)"
      if [[ "$_STATUS" == "200" || "$_STATUS" == "204" ]]; then
        success "Removed from Meshploy"
      else
        warn "Meshploy answered HTTP ${_STATUS}; remove the node in the console: Nodes -> Remove"
      fi
    else
      warn "No node identity in ${CONF}; remove the node in the console if it is listed."
    fi
  else
    warn "No ${CONF}: this Mac was not joined by this script, or has already left."
  fi

  if [[ -f "$NE_PLIST" ]]; then
    launchctl bootout system "$NE_PLIST" 2>/dev/null || true
    rm -f "$NE_PLIST" "$NE_BIN"
    success "node_exporter removed"
  fi

  find_tailscale
  if [[ -n "$TS" ]]; then
    "$TS" logout 2>/dev/null || true
    success "Logged out of the mesh. Tailscale itself is left installed."
  fi
  rm -rf "$CONF_DIR"
  success "This Mac has left the Meshploy mesh."
  exit 0
fi

# ── Join ─────────────────────────────────────────────────────────────────────
[[ -n "$PROVISION_TOKEN" ]] || die "A provisioning token is required: copy the command from the console (Cluster -> Add a node -> Mesh only -> macOS)."
[[ "$PROVISION_TOKEN" == mprov-* ]] || die "That is not a provisioning token (mprov-...)."
[[ -n "$MESHPLOY_API_BASE" ]] || die "The gateway's address is needed: pass --api=https://api.<your-domain>"

header "Asking ${MESHPLOY_API_BASE} for this Mac's mesh credentials"
PROV="$(curl -s --max-time 20 -X POST "${MESHPLOY_API_BASE}/api/v1/nodes/provision" \
  -H "Content-Type: application/json" -d "{\"token\":\"${PROVISION_TOKEN}\"}" || true)"
HEADSCALE_URL="$(json_get "$PROV" headscale_url)"
PREAUTH_KEY="$(json_get "$PROV" preauth_key)"
API_MESH_URL="$(json_get "$PROV" api_mesh_url)"
TOKEN_ROLE="$(json_get "$PROV" mesh_role)"
if [[ -z "$HEADSCALE_URL" || -z "$PREAUTH_KEY" ]]; then
  # The API answers the same way for every bad token on purpose.
  die "The gateway refused this token. It may be used, expired, or for another server. Response: ${PROV:-<none>}"
fi
API_MESH_URL="${API_MESH_URL:-http://100.64.0.1:4000}"
if [[ -n "$TOKEN_ROLE" && "$TOKEN_ROLE" != "mesh" ]]; then
  warn "This token was made for a '${TOKEN_ROLE}' node. A Mac cannot be in the cluster, so it joins as mesh only."
fi
success "Provisioned: mesh at ${HEADSCALE_URL}"

header "Tailscale"
find_tailscale
if [[ -z "$TS" ]]; then
  _BREW=""
  for _b in /opt/homebrew/bin/brew /usr/local/bin/brew; do [[ -x "$_b" ]] && _BREW="$_b" && break; done
  [[ -n "$_BREW" ]] || die "Tailscale is not installed, and neither is Homebrew to install it with. Install one of them (https://brew.sh, or the Tailscale app), then run this again."
  _BREW_USER="${SUDO_USER:-}"
  [[ -n "$_BREW_USER" && "$_BREW_USER" != "root" ]] || die "Homebrew refuses to run as root. Run this with sudo from your own account, not a root shell."
  info "Installing the open-source tailscaled with Homebrew (as ${_BREW_USER})…"
  sudo -u "$_BREW_USER" -H "$_BREW" install tailscale
  _PREFIX="$(sudo -u "$_BREW_USER" -H "$_BREW" --prefix)"
  TS="${_PREFIX}/bin/tailscale"; TS_KIND="daemon"
  _TSD="${_PREFIX}/bin/tailscaled"
  [[ -x "$_TSD" ]] || _TSD="${_PREFIX}/sbin/tailscaled"
  [[ -x "$_TSD" ]] || die "Homebrew installed tailscale but no tailscaled was found under ${_PREFIX}."
  # A launchd system daemon: runs from boot, whoever is logged in.
  "$_TSD" install-system-daemon
  success "tailscaled installed as a system daemon"
elif [[ "$TS_KIND" == "app" ]]; then
  warn "Using the Tailscale app. It runs only while someone is logged in to this Mac,"
  warn "so the node goes offline at the login screen. For a Mac that serves the mesh"
  warn "unattended, quit the app and use Homebrew's tailscaled instead."
else
  success "Tailscale found: ${TS}"
fi

# The daemon takes a moment to answer after it is installed.
# status --json answers once the daemon is up, logged in or not.
for _i in 1 2 3 4 5 6 7 8 9 10; do "$TS" status --json >/dev/null 2>&1 && break; sleep 1; done

EXISTING_URL="$("$TS" status --json 2>/dev/null | grep -m1 '"LoginServerURL"' | cut -d'"' -f4 || true)"
if [[ -n "$EXISTING_URL" && "$EXISTING_URL" != "$HEADSCALE_URL" ]]; then
  warn "This Mac is on another Tailscale network (${EXISTING_URL}); joining Meshploy's replaces it."
fi

if [[ -z "$NODE_NAME" ]]; then
  NODE_NAME="$(scutil --get LocalHostName 2>/dev/null || hostname -s)"
fi
NODE_NAME="$(echo "$NODE_NAME" | tr '[:upper:]' '[:lower:]' | tr -c 'a-z0-9-\n' '-' | sed -e 's/--*/-/g' -e 's/^-//' -e 's/-$//')"
[[ -n "$NODE_NAME" ]] || NODE_NAME="mac"

header "Joining the Meshploy mesh as '${NODE_NAME}'"
"$TS" up \
  --login-server="$HEADSCALE_URL" \
  --authkey="$PREAUTH_KEY" \
  --hostname="$NODE_NAME" \
  --accept-routes \
  --force-reauth \
  --reset \
  || die "tailscale up failed. Check: ${TS} status"

MESH_IP=""
for _i in $(seq 1 15); do
  MESH_IP="$("$TS" ip -4 2>/dev/null | head -1 || true)"
  [[ -n "$MESH_IP" ]] && break
  sleep 1
done
[[ -n "$MESH_IP" ]] || die "No mesh address after 15 s. Check: ${TS} status"
success "On the mesh at ${MESH_IP}"

header "Registering with Meshploy"
_READY=false
for _i in $(seq 1 12); do
  if curl -s --max-time 4 -o /dev/null "${API_MESH_URL}/api/v1/auth/login"; then _READY=true; break; fi
  [[ $_i -eq 1 ]] && info "Waiting for ${API_MESH_URL} over the mesh…"
  sleep 5
done
$_READY || die "Cannot reach ${API_MESH_URL} over the mesh after 60 s, so this Mac is on the mesh but not registered. A token hands out its mesh key once: fix the connection, then run a new command from the console."

REG="$(curl -s --max-time 15 -X POST "${API_MESH_URL}/api/v1/nodes/self-register" \
  -H "Content-Type: application/json" \
  -d "{\"token\":\"${PROVISION_TOKEN}\",\"name\":\"${NODE_NAME}\",\"tailscale_ip\":\"${MESH_IP}\",\"mesh_role\":\"mesh\",\"os\":\"darwin\"}" || true)"
NODE_ID="$(json_get "$REG" id)"
NODE_SECRET="$(json_get "$REG" node_secret)"
[[ -n "$NODE_ID" ]] || die "Meshploy did not register this Mac. Response: ${REG:-<none>}"

mkdir -p "$CONF_DIR"
( umask 077
  printf 'NODE_ID=%s\nNODE_NAME=%s\nMESHPLOY_API_URL=%s\nNODE_SECRET=%s\n' \
    "$NODE_ID" "$NODE_NAME" "$API_MESH_URL" "$NODE_SECRET" > "$CONF" )
chmod 600 "$CONF"
success "Registered as '${NODE_NAME}' (mesh only). Identity kept in ${CONF}, for leaving later."

# ── Metrics ──────────────────────────────────────────────────────────────────
# node_exporter builds for macOS, listening on the mesh address only.
header "Metrics"
case "$(uname -m)" in
  arm64)  NE_ARCH="arm64" ;;
  x86_64) NE_ARCH="amd64" ;;
  *)      NE_ARCH="" ;;
esac
if [[ -z "$NE_ARCH" ]]; then
  warn "No node_exporter build for $(uname -m); the node page will show no metrics."
else
  _TMP="$(mktemp -d)"
  _NAME="node_exporter-${NE_VERSION}.darwin-${NE_ARCH}"
  curl -fsSL "https://github.com/prometheus/node_exporter/releases/download/v${NE_VERSION}/${_NAME}.tar.gz" -o "${_TMP}/ne.tgz"
  tar -xzf "${_TMP}/ne.tgz" -C "$_TMP"
  mkdir -p "$(dirname "$NE_BIN")"
  install -m 755 "${_TMP}/${_NAME}/node_exporter" "$NE_BIN"
  rm -rf "$_TMP"

  cat > "$NE_PLIST" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>${NE_LABEL}</string>
  <key>ProgramArguments</key>
  <array>
    <string>${NE_BIN}</string>
    <string>--web.listen-address=${MESH_IP}:9100</string>
  </array>
  <key>RunAtLoad</key><true/>
  <!-- At boot the mesh address may not exist yet; launchd retries until it does. -->
  <key>KeepAlive</key><true/>
  <key>ThrottleInterval</key><integer>10</integer>
</dict>
</plist>
PLIST
  chmod 644 "$NE_PLIST"
  launchctl bootout system "$NE_PLIST" 2>/dev/null || true
  launchctl bootstrap system "$NE_PLIST"

  # The application firewall blocks incoming connections to unsigned programs.
  _FW="/usr/libexec/ApplicationFirewall/socketfilterfw"
  if [[ -x "$_FW" ]] && "$_FW" --getglobalstate 2>/dev/null | grep -qi enabled; then
    "$_FW" --add "$NE_BIN" >/dev/null 2>&1 || true
    "$_FW" --unblockapp "$NE_BIN" >/dev/null 2>&1 || true
  fi
  success "node_exporter listening on ${MESH_IP}:9100"
fi

echo
echo -e "${BOLD}${GREEN}  ✔  This Mac is a mesh-only node.${RESET}"
echo
echo -e "  Node      ${CYAN}${NODE_NAME}${RESET}"
echo -e "  Mesh IP   ${CYAN}${MESH_IP}${RESET}"
echo
echo "  Route to a port on it: Project -> Routes -> New route -> Node + port."
echo "  If macOS's firewall is on, allow the program serving that port"
echo "  (System Settings -> Network -> Firewall), or connections will be refused."
echo "  A Mac that sleeps goes offline; keep it awake if it serves anything."
