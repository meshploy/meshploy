#!/usr/bin/env bash
# Smoke test for the builder image: builds the app in ./app with each builder
# Meshploy offers, the way a build pod does, pushes it to a plain-HTTP
# registry as the built-in one is, then runs what came out.
#
# The builder's own defaults are what broke before (Buildah without its
# network backend, short image names that resolved nowhere), and the API's
# tests fake the builder, so only building something real finds them.
#
# Usage: apps/builder/smoke/run.sh <builder image>
# Needs Docker, and network to pull base images. Runs privileged, as the build
# pod does.

set -euo pipefail

IMAGE="${1:?usage: run.sh <builder image>}"
HERE="$(cd "$(dirname "$0")" && pwd)"
PORT="${SMOKE_REGISTRY_PORT:-5055}"
REGISTRY="127.0.0.1:${PORT}"
RUN="smoke-$$"
WORK="$(mktemp -d)"

log() { echo -e "\033[1;34m[smoke]\033[0m $*"; }
fail() { echo -e "\033[1;31m[smoke] FAILED:\033[0m $*" >&2; exit 1; }

cleanup() {
    docker rm -f "${RUN}-registry" >/dev/null 2>&1 || true
    docker volume rm -f "${RUN}-buildah" "${RUN}-buildkit" >/dev/null 2>&1 || true
    rm -rf "${WORK}"
}
trap cleanup EXIT

# The app as a repository to clone: the build script clones its source, and a
# local one keeps the test off any git host.
cp -r "${HERE}/app" "${WORK}/repo"
git -C "${WORK}/repo" init -q -b main
git -C "${WORK}/repo" add -A
git -C "${WORK}/repo" -c user.name=smoke -c user.email=smoke@meshploy.invalid commit -q -m "smoke app"
# The repository belongs to the runner's user and the build runs as root, so
# git would refuse to read it.
printf '[safe]\n\tdirectory = *\n' > "${WORK}/gitconfig"

log "Starting a plain-HTTP registry on ${REGISTRY}"
docker run -d --rm --name "${RUN}-registry" -p "${REGISTRY}:5000" registry:2 >/dev/null

# One build, as the API's build job runs it: the same variables, the cache
# paths on volumes as they are on the build pod's claim.
build() {
    local builder="$1" dir="$2" dest="$3"
    log "Building ${dir} with ${builder}"
    docker run --rm --privileged --network host \
        -v "${WORK}/repo:/src:ro" \
        -v "${WORK}/gitconfig:/root/.gitconfig:ro" \
        -v "${RUN}-buildah:/var/lib/containers/storage" \
        -v "${RUN}-buildkit:/tmp/buildkit-root" \
        -e GIT_URL=file:///src \
        -e GIT_REPO=smoke/app \
        -e GIT_BRANCH=main \
        -e ROOT_DIR="${dir}" \
        -e BUILDER="${builder}" \
        -e IMAGE_DEST="${dest}" \
        -e REGISTRY_HOST="${REGISTRY}" \
        -e BUILD_ENV_VARS="GREETING=from-a-build-arg" \
        "${IMAGE}" || fail "${builder} build of ${dir}"
}

# Runs what the registry holds, not anything left on this machine.
expect() {
    local dest="$1" want="$2" got
    docker rmi -f "${dest}" >/dev/null 2>&1 || true
    docker pull -q "${dest}" >/dev/null || fail "${dest} is not in the registry"
    got="$(docker run --rm "${dest}" 2>&1)" || fail "${dest} did not run: ${got}"
    [[ "${got}" == *"${want}"* ]] || fail "${dest} printed '${got}', wanted '${want}'"
    log "${dest} printed: ${got}"
}

build dockerfile dockerfile "${REGISTRY}/smoke/dockerfile:ci"
expect "${REGISTRY}/smoke/dockerfile:ci" "dockerfile build says from-a-build-arg"

build railpack railpack "${REGISTRY}/smoke/railpack:ci"
expect "${REGISTRY}/smoke/railpack:ci" "railpack build says"

# A service saved with Nixpacks, from an API from before it was retired, still
# builds, with Railpack.
build nixpacks railpack "${REGISTRY}/smoke/retired:ci"
expect "${REGISTRY}/smoke/retired:ci" "railpack build says"

log "All builds passed"
