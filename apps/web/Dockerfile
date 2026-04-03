# ── Build stage ────────────────────────────────────────────────────────────────
# Build context: apps/web/ (no monorepo dependency)
#
#   docker build -t ghcr.io/meshploy/web:latest apps/web/
#
# API URL is NOT baked in — it is injected at container startup via the
# API_URL environment variable (written to /config.js by the entrypoint).
#
FROM node:22-alpine AS builder

WORKDIR /app

COPY package.json package-lock.json* ./
RUN npm ci --prefer-offline

COPY . .
RUN npm run build

# ── Runtime stage ──────────────────────────────────────────────────────────────
FROM nginx:1.27-alpine

# SPA routing — serve index.html for all non-file requests
RUN printf 'server {\n\
    listen 80;\n\
    root /usr/share/nginx/html;\n\
    index index.html;\n\
    location /config.js {\n\
        add_header Cache-Control "no-store";\n\
    }\n\
    location / {\n\
        try_files $uri $uri/ /index.html;\n\
    }\n\
    location ~* \\.(js|css|png|jpg|ico|woff2?)$ {\n\
        expires 1y;\n\
        add_header Cache-Control "public, immutable";\n\
    }\n\
}\n' > /etc/nginx/conf.d/default.conf

COPY --from=builder /app/dist /usr/share/nginx/html

# Entrypoint: write runtime config.js then start nginx
COPY docker-entrypoint.sh /docker-entrypoint.sh
RUN chmod +x /docker-entrypoint.sh

EXPOSE 80

ENTRYPOINT ["/docker-entrypoint.sh"]
