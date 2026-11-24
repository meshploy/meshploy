#!/usr/bin/env bash
# =============================================================================
#  Meshploy server template: each boot until setup is done
#
#  Run by meshploy-firstboot.service. On a server made from the template it
#  creates what is this server's alone (the setup token, its public address)
#  and starts the setup page. Once Meshploy is installed it turns itself off.
# =============================================================================
set -euo pipefail

INSTALL_DIR="/opt/meshploy"
ENV_FILE="${INSTALL_DIR}/.env"

# Installed: the setup page's work is done, and so is this.
if [[ -f "$ENV_FILE" ]] && grep -q '^DOMAIN=.' "$ENV_FILE"; then
  systemctl disable meshploy-firstboot.service >/dev/null 2>&1 || true
  rm -f /etc/update-motd.d/60-meshploy /etc/profile.d/meshploy-setup.sh /root/meshploy-setup.txt
  exit 0
fi
# Called as the setup page stops (--after-setup): only the tidying above.
[[ "${1:-}" == "--after-setup" ]] && exit 0

umask 077
touch "$ENV_FILE"
chmod 600 "$ENV_FILE"

# The token that opens the setup page and, later, the first account. Made
# here, never in the image, so no two servers share one. Kept across reboots
# until setup is done.
if ! grep -q '^SETUP_TOKEN=' "$ENV_FILE"; then
  printf 'SETUP_TOKEN=ms_%s\n' "$(openssl rand -hex 16)" >> "$ENV_FILE"
fi
# This server's public address, which the setup page and its DNS checks use.
# Asked again on every boot until set: a network that comes up late is
# answered on the next boot rather than written down empty.
if ! grep -q '^PUBLIC_IP=.' "$ENV_FILE"; then
  ip="$(curl -4 -fsS --max-time 10 --retry 5 --retry-delay 3 --retry-all-errors https://ifconfig.me 2>/dev/null || true)"
  [[ -n "$ip" ]] && { sed -i '/^PUBLIC_IP=/d' "$ENV_FILE"; printf 'PUBLIC_IP=%s\n' "$ip" >> "$ENV_FILE"; }
fi
# The image tags pulled into the template, so the installation finds them.
if ! grep -q '^MESHPLOY_CHANNEL=' "$ENV_FILE" && [[ -f "${INSTALL_DIR}/template/channel" ]]; then
  printf 'MESHPLOY_CHANNEL=%s\n' "$(cat "${INSTALL_DIR}/template/channel")" >> "$ENV_FILE"
fi

# Not known yet is not a reason to stop: the page still serves on the real
# address, and the next boot asks again.
ip="$(grep -E '^PUBLIC_IP=' "$ENV_FILE" | tail -1 | cut -d= -f2- || true)"
cat > /root/meshploy-setup.txt <<EOF
Finish setting up Meshploy in a browser:

    http://${ip:-<this server's address>}:9000

The page asks for this server's setup token. Show it with:

    sudo meshploy setup-token show

You need a domain (or a subdomain) whose DNS you can change. The page shows
the exact records to add and checks them before it installs.
EOF

systemctl start meshploy-setup.service
