#!/bin/sh
# Writes the third-party notices for the builder image, from inside it.
#
# The builder is a distribution rather than a linked binary, so the notices
# cannot be generated from a build graph the way the API's are. What it ships
# comes from two places, and both are read here rather than listed by hand:
#
#   - Alpine packages, from the package database the image already carries. It
#     records a licence per package, and no text: Alpine does not ship licence
#     files, so each entry names its SPDX licence and where the source lives.
#   - Four tools downloaded during the build. Their licence texts are fetched
#     from the same releases the binaries came from, at the versions installed.
#
# Run during `docker build`, after the tools are installed.
set -eu

out=/usr/share/meshploy/THIRD-PARTY-NOTICES-builder.md
mkdir -p /usr/share/meshploy

fetch() {
    curl --retry 3 --retry-delay 2 -fsSL "$1"
}

# A tool that is not installed is not shipped, so it is not listed.
tool_section() {
    name="$1"; version="$2"; url="$3"; home="$4"
    printf '## %s %s\n\n%s\n\n```\n' "$name" "$version" "$home" >> "$out"
    fetch "$url" >> "$out"
    printf '```\n\n' >> "$out"
}

cat > "$out" <<'HEADER'
# Third-party notices: Meshploy builder

Generated inside the image by `apps/builder/notices.sh`. Do not edit by hand.

This image bundles the software below. Each is used under its own licence.
Meshploy's own code is licensed separately: see the LICENSE file beside this
one.

## Alpine packages

Installed from Alpine Linux, which records a licence per package rather than
shipping its text. Each package's source and licence text are at
https://pkgs.alpinelinux.org/packages, under the name and version given here.

| Package | Version | Licence |
|---|---|---|
HEADER

awk '
    /^P:/ { p = substr($0, 3) }
    /^V:/ { v = substr($0, 3) }
    /^L:/ { l = substr($0, 3); if (l == "") l = "not stated"; printf "| %s | %s | %s |\n", p, v, l }
' /lib/apk/db/installed | sort >> "$out"

printf '\n' >> "$out"

if command -v buildctl >/dev/null 2>&1; then
    v="$(buildctl --version | awk '{print $3}' | sed 's/^v//')"
    tool_section "BuildKit (buildctl)" "$v" \
        "https://raw.githubusercontent.com/moby/buildkit/v${v}/LICENSE" \
        "https://github.com/moby/buildkit"
fi

if command -v nixpacks >/dev/null 2>&1; then
    v="$(nixpacks --version | awk '{print $2}' | sed 's/^v//')"
    tool_section "Nixpacks" "$v" \
        "https://raw.githubusercontent.com/railwayapp/nixpacks/v${v}/LICENSE" \
        "https://github.com/railwayapp/nixpacks"
fi

if command -v railpack >/dev/null 2>&1; then
    v="$(railpack --version | awk '{print $NF}' | sed 's/^v//')"
    tool_section "Railpack" "$v" \
        "https://raw.githubusercontent.com/railwayapp/railpack/v${v}/LICENSE" \
        "https://github.com/railwayapp/railpack"
fi

# mise arrives twice: as an Alpine package, and as the musl build pre-seeded for
# railpack. The Alpine row above covers the package; this covers the download.
seeded="$(ls /tmp/railpack/mise/mise-* 2>/dev/null | head -1 || true)"
if [ -n "$seeded" ]; then
    v="$(basename "$seeded" | sed 's/^mise-//')"
    tool_section "mise (pre-seeded for Railpack)" "$v" \
        "https://raw.githubusercontent.com/jdx/mise/v${v}/LICENSE" \
        "https://github.com/jdx/mise"
fi

echo "wrote $out"
