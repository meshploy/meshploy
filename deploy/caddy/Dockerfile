FROM caddy:2-builder AS builder

# caddy-dns-meshploy answers a delegated domain's challenges in the zone files
# CoreDNS serves. The others answer an on-demand domain's through its DNS
# provider, one module per provider kind the edge snapshot can name.
RUN xcaddy build \
    --with github.com/meshploy/caddy-dns-meshploy \
    --with github.com/caddy-dns/cloudflare

FROM caddy:2-alpine

COPY --from=builder /usr/bin/caddy /usr/bin/caddy
# channel: main/latest
