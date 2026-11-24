#!/bin/sh
# The login message on a server made from the Meshploy template: where setup
# is, until it is done. Silent once Meshploy is installed.
ENV_FILE=/opt/meshploy/.env
if [ -f "$ENV_FILE" ] && grep -q '^DOMAIN=.' "$ENV_FILE" 2>/dev/null; then
  exit 0
fi
ip=$(grep -E '^PUBLIC_IP=' "$ENV_FILE" 2>/dev/null | tail -1 | cut -d= -f2-)
printf '\n'
printf '  \033[1mMeshploy is ready to set up.\033[0m\n'
printf '\n'
printf '  Open   \033[36mhttp://%s:9000\033[0m\n' "${ip:-<this server>}"
printf '  Token  \033[36msudo meshploy setup-token show\033[0m\n'
printf '\n'
printf '  You need a domain whose DNS you can change; the page shows the\n'
printf '  records to add and checks them before it installs.\n'
printf '\n'
