#!/usr/bin/env bash
# Builds Meshploy's own tailscaled and tailscale from Tailscale's source at the
# pinned version, with meshploy.patch applied.
#
# Why a patched build exists: Meshploy runs its mesh on a tailscaled of its own
# (interface meshploy0, its own state and socket), so a machine can keep a
# Tailscale of its own on tailscale0 untouched. Two stock daemons on one host
# fight over the same policy-routing table and rules and the same ts-*
# firewall chains. The patch gives Meshploy's its own table (53), rule
# priorities (53xx), chain names (mp-*) and subnet-route mark, and its CGNAT
# rule returns rather than drops. Nothing else changes: its 100.100.100.100
# route lives in its own table, which a stock daemon's rules are checked
# before, so a machine's own Tailscale keeps its DNS while it runs.
#
# Refuses to build when Tailscale's LICENSE is not the one accepted in
# LICENSE.sha256: a licence change is a decision, not a rebuild.
#
#   deploy/tailscale/build.sh <out-dir> [goarch]      (default goarch: amd64)
#
# Environment: TAILSCALE_VERSION overrides VERSION (used by the bump workflow).
set -euo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
out="${1:?usage: build.sh <out-dir> [goarch]}"
arch="${2:-amd64}"
version="${TAILSCALE_VERSION:-$(cat "$here/VERSION")}"
mkdir -p "$out"
out="$(cd "$out" && pwd)"

src="$(mktemp -d)"
trap 'rm -rf "$src"' EXIT
git -c advice.detachedHead=false clone --quiet --depth 1 --branch "v${version}" https://github.com/tailscale/tailscale.git "$src"

want="$(cat "$here/LICENSE.sha256")"
got="$(sha256sum "$src/LICENSE" | cut -d' ' -f1)"
if [[ "$got" != "$want" ]]; then
  echo "Tailscale's LICENSE at v${version} is not the one accepted (${got}, want ${want})." >&2
  echo "Read it; if it is still acceptable, update deploy/tailscale/LICENSE.sha256." >&2
  exit 1
fi

git -C "$src" apply --whitespace=nowarn "$here/meshploy.patch"

# The version the binaries report, so `meshploy mesh version` says what runs.
ldflags="-s -w -X tailscale.com/version.longStamp=${version}-meshploy -X tailscale.com/version.shortStamp=${version}"
for cmd in tailscaled tailscale; do
  (cd "$src" && CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -trimpath -ldflags "$ldflags" \
    -o "$out/meshploy-${cmd}-linux-${arch}" "./cmd/${cmd}")
done
cp "$src/LICENSE" "$out/TAILSCALE-LICENSE"
echo "built meshploy-tailscaled and meshploy-tailscale ${version} for linux/${arch} in ${out}"
