# Meshploy Builder Image
# Runs as the build container inside K8s Jobs.
# Contains: git, nixpacks, railpack, buildah (rootless OCI build + push).
#
# Build: docker build -t ghcr.io/meshploy/builder:latest apps/builder/
# Push:  docker push ghcr.io/meshploy/builder:latest

FROM debian:bookworm-slim

# ── System deps ────────────────────────────────────────────────────────────────
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    git \
    buildah \
    fuse-overlayfs \
    && rm -rf /var/lib/apt/lists/*

# ── Nixpacks ───────────────────────────────────────────────────────────────────
RUN curl -fsSL https://nixpacks.com/install.sh | bash && \
    nixpacks --version

# ── Railpack ──────────────────────────────────────────────────────────────────
# Install from GitHub releases — railpack.io DNS is unreliable in CI builds.
RUN RAILPACK_VERSION=$(curl -fsSL https://api.github.com/repos/railwayapp/railpack/releases/latest \
      | grep '"tag_name"' | head -1 | sed 's/.*"tag_name": "\(.*\)".*/\1/') && \
    curl -fsSL "https://github.com/railwayapp/railpack/releases/download/${RAILPACK_VERSION}/railpack-x86_64-unknown-linux-musl" \
      -o /usr/local/bin/railpack && \
    chmod +x /usr/local/bin/railpack && \
    railpack --version

# ── Buildah rootless setup ────────────────────────────────────────────────────
RUN useradd -m builder && \
    mkdir -p /home/builder/.config/containers && \
    echo '[storage]\ndriver = "overlay"\n[storage.options]\nmount_program = "/usr/bin/fuse-overlayfs"' \
    > /home/builder/.config/containers/storage.conf

# ── Build script ──────────────────────────────────────────────────────────────
COPY meshploy-build /usr/local/bin/meshploy-build
RUN chmod +x /usr/local/bin/meshploy-build

USER builder
WORKDIR /home/builder

ENTRYPOINT ["/usr/local/bin/meshploy-build"]
