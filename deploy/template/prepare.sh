#!/usr/bin/env bash
# =============================================================================
#  Meshploy server template: image build
#
#  Prepares a machine to be saved as a provider's image ("Ubuntu 24.04 with
#  Meshploy"). Everything every server needs is done here, once: Docker, the
#  CLI, the deploy files and the container images. Nothing that must differ
#  between servers is: no setup token, no secrets, no k3s, no mesh. Those are
#  made on each server's first boot (firstboot.sh), and the installation runs
#  from the setup page once the customer gives it a domain.
#
#  Run by get.sh --template, as root, on the machine the image is made from:
#    sudo bash -c "$(curl -fsSL https://raw.githubusercontent.com/meshploy/meshploy/main/get.sh)" _ --template
# =============================================================================
set -euo pipefail

INSTALL_DIR="/opt/meshploy"
TEMPLATE_DIR="${INSTALL_DIR}/template"
CHANNEL="${MESHPLOY_CHANNEL:-latest}"

info()    { echo -e "\033[0;36m  →\033[0m  $*"; }
success() { echo -e "\033[0;32m  ✔\033[0m  $*"; }
die()     { echo -e "\033[0;31m  ✘\033[0m  $*" >&2; exit 1; }

[[ "$EUID" -eq 0 ]] || die "Run as root."
[[ -x /usr/local/bin/meshploy ]] || die "The meshploy CLI is not installed; run this through get.sh --template."
command -v systemctl >/dev/null || die "systemd is required."

# A template is made from a clean machine. One that already runs Meshploy
# would carry its secrets into every server made from the image.
if [[ -f "${INSTALL_DIR}/.env" ]] && grep -q '^DOMAIN=.' "${INSTALL_DIR}/.env"; then
  die "Meshploy is already installed here, so its secrets would be in the image. Make the template from a fresh machine."
fi

export DEBIAN_FRONTEND=noninteractive

# ── Docker ───────────────────────────────────────────────────────────────────
if ! command -v docker >/dev/null || docker --version 2>/dev/null | grep -qi podman; then
  info "Installing Docker…"
  curl -fsSL https://get.docker.com | sh
fi
systemctl enable --now docker >/dev/null
docker compose version >/dev/null 2>&1 || die "Docker is installed without the Compose v2 plugin."
success "Docker $(docker --version | awk '{print $3}' | tr -d ',')"

# ── Images ───────────────────────────────────────────────────────────────────
# Pulled now so a customer's first boot does not wait for them. The channel is
# kept beside them: the installation later must ask for these same tags.
info "Pulling the Meshploy images (${CHANNEL})…"
cd "$INSTALL_DIR"
MESHPLOY_CHANNEL="$CHANNEL" docker compose pull --quiet --ignore-pull-failures
mkdir -p "$TEMPLATE_DIR"
printf '%s\n' "$CHANNEL" > "${TEMPLATE_DIR}/channel"
success "Images pulled"

# ── First boot ───────────────────────────────────────────────────────────────
install -m 0644 "${TEMPLATE_DIR}/meshploy-firstboot.service" /etc/systemd/system/meshploy-firstboot.service
install -m 0644 "${TEMPLATE_DIR}/meshploy-setup.service" /etc/systemd/system/meshploy-setup.service
chmod 0755 "${TEMPLATE_DIR}/firstboot.sh" "${TEMPLATE_DIR}/motd.sh"
systemctl daemon-reload
systemctl enable meshploy-firstboot.service >/dev/null
# The login message tells whoever signs in where setup is, until it is done.
if [[ -d /etc/update-motd.d ]]; then
  ln -sf "${TEMPLATE_DIR}/motd.sh" /etc/update-motd.d/60-meshploy
else
  install -m 0644 /dev/null /etc/profile.d/meshploy-setup.sh
  printf '[ "$(id -u)" = 0 ] && %s\n' "${TEMPLATE_DIR}/motd.sh" > /etc/profile.d/meshploy-setup.sh
fi
success "First boot prepared"

# ── Nothing of this machine's own ────────────────────────────────────────────
# Whatever a run on this machine left behind, so no server starts with it.
rm -f "${INSTALL_DIR}/.env" "${INSTALL_DIR}/.setup-state.json" /root/meshploy-setup.txt

echo
success "Ready to be saved as an image."
echo "  Each server made from it creates its own setup token on first boot and"
echo "  serves the setup page at http://<its address>:9000."
echo "  Clean the machine the way your platform asks before saving the image"
echo "  (cloud-init, SSH host keys, logs, machine ID)."
