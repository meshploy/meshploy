#!/usr/bin/env bash
# Ships a local build to a gateway, for testing what has not reached GitHub:
# builds the images and the CLI here, copies them with the repository's deploy/
# to the server, and upgrades it from them with `server-upgrade --from`, which
# stages, backs up, health-checks and rolls back like any upgrade.
#
# The server then runs on the local channel. The console still offers the
# next edge build, and taking it (or `sudo meshploy server-upgrade --edge`)
# puts the server back on the published images.
#
# Usage: scripts/ship.sh <ssh-host> [component...]
#   components: api web proxy caddy builder cli   (default: api web proxy cli)
#   An image not shipped runs what the server has on the local tag, or edge.
#
# Environment:
#   CONTAINER             docker or podman (default: whichever is installed)
#   LICENSE_PUBLIC_KEYS   baked into the API as CI does; empty means a licence
#                         does not verify on this build
#
# Needs ssh access with sudo on the server, and rsync on both ends. The
# gateway must be installed already: this upgrades it, it does not install.

set -euo pipefail

HOST="${1:?usage: scripts/ship.sh <ssh-host> [api web proxy caddy builder cli]}"
shift
if [[ $# -gt 0 ]]; then COMPONENTS=("$@"); else COMPONENTS=(api web proxy cli); fi

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

log() { echo -e "\033[1;34m[ship]\033[0m $*"; }
die() { echo -e "\033[1;31m[ship]\033[0m $*" >&2; exit 1; }

CONTAINER="${CONTAINER:-$(command -v docker >/dev/null && echo docker || echo podman)}"
command -v "$CONTAINER" >/dev/null || die "no docker or podman here"

for c in "${COMPONENTS[@]}"; do
  case "$c" in api|web|proxy|caddy|builder|cli) ;; *) die "unknown component: $c" ;; esac
done

# Stamped like an edge build, so the console reads the commit it came from.
SHA="$(git rev-parse --short=7 HEAD)"
VERSION="$(cat VERSION)+${SHA}"
if [[ -n "$(git status --porcelain)" ]]; then
  log "The tree has uncommitted changes: they are shipped, stamped as ${VERSION}."
fi

case "$(ssh "$HOST" uname -m)" in
  x86_64) ARCH=amd64 ;;
  aarch64) ARCH=arm64 ;;
  *) die "unsupported architecture on ${HOST}" ;;
esac

BUNDLE="$(mktemp -d)"
trap 'rm -rf "$BUNDLE"' EXIT
mkdir -p "$BUNDLE/images"

# The server skips these too; they are rendered or kept there, never shipped.
rsync -a deploy/ "$BUNDLE/deploy/" \
  --exclude .env --exclude headscale/data --exclude headscale/config/config.yaml \
  --exclude caddy/Caddyfile --exclude caddy/conf.d --exclude coredns/Corefile --exclude coredns/zones

build_image() {
  local name="$1"; shift
  local tag="ghcr.io/meshploy/${name}:local"
  log "Building ${name} (${ARCH})…"
  "$CONTAINER" build --platform "linux/${ARCH}" -t "$tag" "$@"
  "$CONTAINER" save "$tag" | gzip -1 > "$BUNDLE/images/${name}.tar.gz"
}

for c in "${COMPONENTS[@]}"; do
  case "$c" in
    api)
      build_image api -f apps/api/Dockerfile \
        --build-arg VERSION="$VERSION" --build-arg CHANNEL=edge \
        --build-arg LICENSE_PUBLIC_KEYS="${LICENSE_PUBLIC_KEYS:-}" . ;;
    web)     build_image web -f apps/web/Dockerfile --build-context help=packages/help apps/web ;;
    proxy)   build_image proxy -f apps/proxy/Dockerfile . ;;
    caddy)   build_image caddy -f deploy/caddy/Dockerfile deploy/caddy ;;
    builder) build_image builder -f apps/builder/Dockerfile . ;;
    cli)
      log "Building the CLI (linux/${ARCH})…"
      LDFLAGS="-s -w -X 'github.com/meshploy/apps/cli/cmd.Version=${VERSION}' -X 'github.com/meshploy/apps/cli/cmd.Channel=edge'"
      if command -v go >/dev/null; then
        (cd apps/cli && CGO_ENABLED=0 GOOS=linux GOARCH="$ARCH" go build -ldflags="$LDFLAGS" -o "$BUNDLE/meshploy" .)
      else
        # No Go here: build in the toolchain's image, caches kept in volumes.
        "$CONTAINER" run --rm -v "$ROOT:/src:z" -v meshploy-ship-gomod:/go/pkg/mod -v meshploy-ship-gocache:/root/.cache/go-build \
          -w /src/apps/cli -e CGO_ENABLED=0 -e GOOS=linux -e GOARCH="$ARCH" -e GOFLAGS=-buildvcs=false \
          -e LDFLAGS="$LDFLAGS" \
          docker.io/library/golang:1.25 sh -c 'go build -ldflags="$LDFLAGS" -o /src/.ship-meshploy .'
        mv .ship-meshploy "$BUNDLE/meshploy"
      fi ;;
  esac
done

log "Sending to ${HOST}: $(du -sh "$BUNDLE" | cut -f1)…"
ssh "$HOST" 'mkdir -p /tmp/meshploy-ship'
rsync -a --partial --delete "$BUNDLE/" "${HOST}:/tmp/meshploy-ship/"

ssh -t "$HOST" 'set -e
  if [ -f /tmp/meshploy-ship/meshploy ]; then
    sudo install -m 0755 /tmp/meshploy-ship/meshploy /usr/local/bin/meshploy
    echo "✔  CLI $(meshploy version 2>/dev/null | head -1)"
  fi
  if ! sudo test -f /opt/meshploy/.env; then
    echo "Meshploy is not installed on this server yet: install it first, then ship again." >&2
    exit 1
  fi
  sudo meshploy server-upgrade --from /tmp/meshploy-ship
  rm -rf /tmp/meshploy-ship'
log "Shipped ${VERSION} to ${HOST}"
