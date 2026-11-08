#!/usr/bin/env bash
# Smoke test for the builder image: builds the app in ./app with each builder
# Meshploy offers, the way a build pod does, pushes it to a plain-HTTP
# registry as the built-in one is, then runs what came out.
#
# The builder's own defaults are what broke before (Buildah without its
# network backend, short image names that resolved nowhere), and the API's
# tests fake the builder, so only building something real finds them.
#
# Each build runs twice: as root and privileged, as Community's build pod runs
# it, and as uid 1000, unprivileged with no capability added, as a cluster
# agent's does. Rootless, it also builds on a cache the root builder left.
#
# Usage: apps/builder/smoke/run.sh <builder image>
# Needs Docker, and network to pull base images.

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
    docker volume rm -f "${RUN}-buildah" "${RUN}-buildkit" "${RUN}-rootless" "${RUN}-scratch" >/dev/null 2>&1 || true
    rm -rf "${WORK}"
}
trap cleanup EXIT

# The app as a repository to clone: the build script clones its source, and a
# local one keeps the test off any git host.
cp -r "${HERE}/app" "${WORK}/repo"
git -C "${WORK}/repo" init -q -b main
git -C "${WORK}/repo" add -A
git -C "${WORK}/repo" -c user.name=smoke -c user.email=smoke@meshploy.invalid commit -q -m "smoke app"
# The repository belongs to the runner's user and the build runs as another,
# so git would refuse to read it.
printf '[safe]\n\tdirectory = *\n' > "${WORK}/gitconfig"
chmod -R a+rX "${WORK}"

log "Starting a plain-HTTP registry on ${REGISTRY}"
docker run -d --rm --name "${RUN}-registry" -p "${REGISTRY}:5000" registry:2 >/dev/null

# The rootless builds' own cache volume starts as theirs, as the cluster
# agent's pod gets it (fsGroup on a claim of its own).
docker volume create "${RUN}-rootless" >/dev/null
docker run --rm -v "${RUN}-rootless:/c" alpine:3.22 chown 1000:1000 /c
# The build pod's scratch folder, an emptyDir on the node's disk: anyone may
# write, as in an emptyDir.
docker volume create "${RUN}-scratch" >/dev/null
docker run --rm -v "${RUN}-scratch:/s" alpine:3.22 chmod 1777 /s

# How the pod runs: root and privileged, or uid 1000 unprivileged. Rootless
# BuildKit makes its own user namespace, which the default seccomp and
# AppArmor profiles forbid, so both are off; under SELinux it runs as
# container_engine_t, as the cluster agent's pod does. Nothing else is
# granted. Hosts without AppArmor or SELinux ignore what does not apply.
MODE=root
as() {
    case "${MODE}" in
      root)     echo --privileged ;;
      rootless) echo --user 1000:1000 --security-opt seccomp=unconfined --security-opt apparmor=unconfined --security-opt label=type:container_engine_t ;;
    esac
}
home() { [[ "${MODE}" == root ]] && echo /root || echo /home/build; }
# The cache volumes, at the paths they have on the build pod's claim.
CACHE=""
caches() {
    case "${CACHE}" in
      root)     echo -v "${RUN}-buildah:/var/lib/containers/storage" -v "${RUN}-buildkit:/tmp/buildkit-root" ;;
      rootless) echo -v "${RUN}-rootless:/tmp/buildkit-root" ;;
    esac
}

# One build, as the API's build job runs it: the same variables.
build() {
    local builder="$1" dir="$2" dest="$3" out="${WORK}/build.log"
    log "Building ${dir} with ${builder}, ${MODE}"
    # shellcheck disable=SC2046
    if ! docker run --rm --network host $(as) $(caches) \
        -v "${RUN}-scratch:/var/lib/meshploy-scratch:z" \
        -e BUILD_SCRATCH=/var/lib/meshploy-scratch \
        -v "${WORK}/repo:/src:ro,z" \
        -v "${WORK}/gitconfig:$(home)/.gitconfig:ro,z" \
        -e HOME="$(home)" \
        -e GIT_URL=file:///src \
        -e GIT_REPO=smoke/app \
        -e GIT_BRANCH=main \
        -e ROOT_DIR="${dir}" \
        -e BUILDER="${builder}" \
        -e IMAGE_DEST="${dest}" \
        -e REGISTRY_HOST="${REGISTRY}" \
        -e BUILD_ENV_VARS="GREETING=from-a-build-arg" \
        -e CACHE_LIMIT_MB=2000 \
        "${IMAGE}" 2>&1 | tee "${out}"; then
        fail "${builder} build of ${dir}, ${MODE}"
    fi
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

# An uploaded folder: stored as a blob in the registry, as the API stores it,
# and fetched by the build instead of a clone.
upload_build() {
    local dest="$1"
    log "Uploading dockerfile/ as a folder"
    tar -czf "${WORK}/upload.tar.gz" -C "${HERE}/app/dockerfile" .
    local digest location
    digest="sha256:$(sha256sum "${WORK}/upload.tar.gz" | cut -d' ' -f1)"
    location="$(curl -fsS -D - -o /dev/null -X POST "http://${REGISTRY}/v2/smoke/upload-source/blobs/uploads/" \
        | tr -d '\r' | sed -n 's/^[Ll]ocation: //p')"
    [[ "${location}" == http* ]] || location="http://${REGISTRY}${location}"
    curl -fsS -X PUT -H "Content-Type: application/octet-stream" --data-binary "@${WORK}/upload.tar.gz" \
        "${location}$([[ "${location}" == *\?* ]] && echo '&' || echo '?')digest=${digest}" >/dev/null \
        || fail "could not store the folder in the registry"
    log "Building the uploaded folder with dockerfile, ${MODE}"
    # shellcheck disable=SC2046
    docker run --rm --network host $(as) $(caches) \
        -e HOME="$(home)" \
        -e SOURCE_URL="http://${REGISTRY}/v2/smoke/upload-source/blobs/${digest}" \
        -e SOURCE_DIGEST="${digest}" \
        -e SOURCE_NAME=dockerfile \
        -e BUILDER=dockerfile \
        -e IMAGE_DEST="${dest}" \
        -e REGISTRY_HOST="${REGISTRY}" \
        -e BUILD_ENV_VARS="GREETING=from-an-upload" \
        "${IMAGE}" || fail "dockerfile build of the uploaded folder, ${MODE}"
    expect "${dest}" "dockerfile build says from-an-upload"
}

all_builds() {
    build dockerfile dockerfile "${REGISTRY}/smoke/dockerfile-${MODE}:ci"
    expect "${REGISTRY}/smoke/dockerfile-${MODE}:ci" "dockerfile build says from-a-build-arg"

    # The greeting is written by the app's build step from the build variable,
    # which Railpack hands BuildKit as a secret.
    build railpack railpack "${REGISTRY}/smoke/railpack-${MODE}:ci"
    expect "${REGISTRY}/smoke/railpack-${MODE}:ci" "railpack build says from-a-build-arg"

    # A service saved with Nixpacks, from an API from before it was retired,
    # still builds, with Railpack.
    build nixpacks railpack "${REGISTRY}/smoke/retired-${MODE}:ci"
    expect "${REGISTRY}/smoke/retired-${MODE}:ci" "railpack build says from-a-build-arg"

    upload_build "${REGISTRY}/smoke/uploaded-${MODE}:ci"
}

MODE=root CACHE=root all_builds

MODE=rootless CACHE=rootless all_builds
# Built again on its own cache, warm: steps are reused.
MODE=rootless CACHE=rootless build railpack railpack "${REGISTRY}/smoke/railpack-warm:ci"
grep -q CACHED "${WORK}/build.log" || fail "the rootless rebuild reused no cached step"
expect "${REGISTRY}/smoke/railpack-warm:ci" "railpack build says from-a-build-arg"

# On the cache the root builds left, which it can neither use nor remove: it
# says so and builds without it, in a root of its own on the scratch volume.
# The Node app, because that is where it matters: there overlayfs keeps it
# near the size of a cached build, where native's full copy of every step
# would take over 14GB.
MODE=rootless CACHE=root build railpack railpack "${REGISTRY}/smoke/railpack-on-root-cache:ci"
grep -q "belongs to another user" "${WORK}/build.log" || fail "the rootless build did not notice the root builder's cache"
expect "${REGISTRY}/smoke/railpack-on-root-cache:ci" "railpack build says from-a-build-arg"
scratch="$(docker run --rm -v "${RUN}-scratch:/s" alpine:3.22 sh -c \
    'ls -d /s/buildkit-private.*/runc-overlayfs >/dev/null 2>&1 && echo overlayfs; ls -d /s/buildkit-private.*/runc-native >/dev/null 2>&1 && echo native; du -sm /s | cut -f1')"
log "The cacheless build's root: $(echo ${scratch})MB"
grep -qx overlayfs <<<"${scratch}" || fail "the cacheless build did not use overlayfs on the scratch volume"
if grep -qx native <<<"${scratch}"; then fail "the cacheless build kept full copies (native)"; fi
(( $(tail -1 <<<"${scratch}") < 4000 )) || fail "the cacheless build's root is $(tail -1 <<<"${scratch}")MB"

log "All builds passed"
