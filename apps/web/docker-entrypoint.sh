#!/bin/sh
# Write runtime config before nginx starts.
# API calls from the browser are routed via path (/api/*) by Caddy,
# so the URL is always relative — no domain config required.
set -e

cat > /usr/share/nginx/html/config.js <<'EOF'
window.__MESHPLOY_CONFIG__ = {
  apiUrl: ""
};
EOF

exec nginx -g "daemon off;"
