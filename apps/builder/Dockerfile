# Meshploy Builder Image
# Runs as the build container inside K8s Jobs.
# Contains: git, nixpacks, railpack, buildah (rootless OCI build + push).
#
# Build: docker build -t ghcr.io/meshploy/builder:latest apps/builder/
# Push:  docker push ghcr.io/meshploy/builder:latest

FROM alpine:3.22

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
    fuse-overlayfs \
    passt \
    mise \
    gcompat \
    libc6-compat \
    buildkit && \
    mkdir -p /lib64 && \
    ln -sf /lib/ld-linux-x86-64.so.2 /lib64/ld-linux-x86-64.so.2

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

# ── Pre-seed railpack's mise cache with the musl binary ───────────────────────
# Railpack downloads a versioned mise binary to /tmp/railpack/mise/mise-<ver>
# at runtime and validates it before use. On Alpine (musl), it downloads the
# glibc build which fails. We pre-download the musl build to the same path so
# railpack finds it already there and skips the download.
# Update MISE_VERSION when upgrading railpack if the version in the error changes.
RUN MISE_VERSION="2026.3.17" && \
    mkdir -p /tmp/railpack/mise && \
    curl -fsSL \
      "https://github.com/jdx/mise/releases/download/v${MISE_VERSION}/mise-v${MISE_VERSION}-linux-x64-musl" \
      -o "/tmp/railpack/mise/mise-${MISE_VERSION}" && \
    chmod +x "/tmp/railpack/mise/mise-${MISE_VERSION}" && \
    chown -R builder:builder /tmp/railpack

# ── Build script ──────────────────────────────────────────────────────────────
COPY meshploy-build /usr/local/bin/meshploy-build
RUN chmod +x /usr/local/bin/meshploy-build

USER builder
WORKDIR /home/builder

ENTRYPOINT ["/usr/local/bin/meshploy-build"]
