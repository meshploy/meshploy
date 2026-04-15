# Meshploy Builder Image
# Runs as the build container inside K8s Jobs.
# Contains: git, nixpacks, railpack, buildah (rootless OCI build + push).
#
# Build: docker build -t ghcr.io/meshploy/builder:latest apps/builder/
# Push:  docker push ghcr.io/meshploy/builder:latest

FROM alpine:3.21

# ── System deps ────────────────────────────────────────────────────────────────
# Alpine's git uses OpenSSL — no GnuTLS issues.
RUN apk add --no-cache \
    bash \
    ca-certificates \
    curl \
    git \
    tar \
    buildah \
    netavark \
    aardvark-dns \
    fuse-overlayfs

# ── Nixpacks ───────────────────────────────────────────────────────────────────
RUN curl -fsSL https://nixpacks.com/install.sh | bash && \
    nixpacks --version

# ── Railpack ──────────────────────────────────────────────────────────────────
RUN curl -fsSL https://raw.githubusercontent.com/railwayapp/railpack/main/install.sh | bash && \
    railpack --version

# ── Buildah rootless setup ────────────────────────────────────────────────────
RUN adduser -D builder && \
    echo "builder:100000:65536" >> /etc/subuid && \
    echo "builder:100000:65536" >> /etc/subgid && \
    mkdir -p /home/builder/.config/containers && \
    printf '[storage]\ndriver = "overlay"\n[storage.options]\nmount_program = "/usr/bin/fuse-overlayfs"\n' \
    > /home/builder/.config/containers/storage.conf && \
    chown -R builder:builder /home/builder/.config

# ── Build script ──────────────────────────────────────────────────────────────
COPY meshploy-build /usr/local/bin/meshploy-build
RUN chmod +x /usr/local/bin/meshploy-build

USER builder
WORKDIR /home/builder

ENTRYPOINT ["/usr/local/bin/meshploy-build"]
