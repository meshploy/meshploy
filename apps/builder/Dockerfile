# Meshploy Builder Image
# Runs as the build container inside K8s Jobs.
# Contains: git, nixpacks, railpack, buildah, buildkitd, buildctl.
# Runs as root — build jobs are ephemeral and mesh-internal.
#
# Build: docker build -t ghcr.io/meshploy/builder:latest apps/builder/
# Push:  docker push ghcr.io/meshploy/builder:latest

FROM alpine:3.22

# ── System deps ────────────────────────────────────────────────────────────────
# Alpine's git uses OpenSSL — no GnuTLS issues.
# gcompat + lib64 symlink: allow glibc-linked binaries (e.g. railpack's mise) to
# exec on musl. gcompat provides the shim; lib64 provides the expected path.
RUN apk add --no-cache \
    bash \
    ca-certificates \
    curl \
    git \
    tar \
    buildah \
    fuse-overlayfs \
    buildkit \
    mise \
    gcompat \
    libc6-compat && \
    mkdir -p /lib64 && \
    ln -sf /lib/ld-linux-x86-64.so.2 /lib64/ld-linux-x86-64.so.2

# ── Buildah root config ────────────────────────────────────────────────────────
# Running as root: use native overlay. Storage path matches the PVC mount point
# in the K8s Job spec (/var/lib/containers/storage) so layers persist across builds.
RUN mkdir -p /root/.config/containers && \
    printf '[storage]\ndriver = "overlay"\ngraphRoot = "/var/lib/containers/storage"\n' \
    > /root/.config/containers/storage.conf

# ── buildctl (BuildKit client) ────────────────────────────────────────────────
# Alpine's buildkit apk only ships buildkitd. buildctl is in the same upstream
# release tarball — extract it to match the installed daemon version.
RUN BUILDKIT_VERSION="$(buildkitd --version | awk '{print $3}')" && \
    curl -fsSL \
      "https://github.com/moby/buildkit/releases/download/v${BUILDKIT_VERSION}/buildkit-v${BUILDKIT_VERSION}.linux-amd64.tar.gz" \
    | tar -xz -C /usr/local/bin --strip-components=1 bin/buildctl && \
    buildctl --version

# ── Nixpacks ───────────────────────────────────────────────────────────────────
RUN curl -fsSL https://nixpacks.com/install.sh | bash && \
    nixpacks --version

# ── Railpack ──────────────────────────────────────────────────────────────────
RUN curl -fsSL https://raw.githubusercontent.com/railwayapp/railpack/main/install.sh | bash && \
    railpack --version

# ── Pre-seed railpack's mise cache with the musl binary ───────────────────────
# Railpack downloads a versioned mise binary at runtime. On Alpine (musl) it
# downloads the glibc build which fails. Pre-download the musl build to the
# exact path railpack checks so it skips the download.
# Update MISE_VERSION if the version in the error message changes.
RUN MISE_VERSION="2026.3.17" && \
    mkdir -p /tmp/railpack/mise && \
    curl -fsSL \
      "https://github.com/jdx/mise/releases/download/v${MISE_VERSION}/mise-v${MISE_VERSION}-linux-x64-musl" \
      -o "/tmp/railpack/mise/mise-${MISE_VERSION}" && \
    chmod +x "/tmp/railpack/mise/mise-${MISE_VERSION}"

# ── Build script ──────────────────────────────────────────────────────────────
COPY meshploy-build /usr/local/bin/meshploy-build
RUN chmod +x /usr/local/bin/meshploy-build

WORKDIR /root

ENTRYPOINT ["/usr/local/bin/meshploy-build"]
