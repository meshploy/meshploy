#!/usr/bin/env sh
#
# Assert that every Go module in go.work is COPYed into each Dockerfile that
# runs `go work sync`.
#
# Why this exists: `go work sync` reads the manifest of EVERY module listed in
# go.work, including ones the image never builds. Adding a module to go.work
# without adding a COPY line therefore breaks the image — but not `go build`,
# `go vet`, or `go test`, which resolve modules straight from the filesystem.
# The failure surfaces only in a Docker build, which is exactly where it is
# most expensive to discover. It has already bitten twice.
#
# Run from the repo root:
#
#   sh scripts/check-workspace-dockerfiles.sh
#
# Exits non-zero and names the missing COPY lines.

set -eu

cd "$(dirname "$0")/.."

fail=0

# Module paths from the `use (...)` block, normalised without the leading "./".
# Handles both the block form and a single-line `use ./path`.
modules=$(awk '
  /^use[[:space:]]*\(/          { inblock = 1; next }
  inblock && /^[[:space:]]*\)/  { inblock = 0; next }
  inblock {
    sub(/\/\/.*/, "")
    gsub(/^[[:space:]]+|[[:space:]]+$/, "")
    if ($0 != "") print
    next
  }
  /^use[[:space:]]+[^(]/ { print $2 }
' go.work | sed 's|^\./||')

if [ -z "$modules" ]; then
  echo "::error::could not parse any modules from go.work — has its format changed?"
  exit 1
fi

dockerfiles=$(grep -rl 'go work sync' --include='Dockerfile*' . || true)

if [ -z "$dockerfiles" ]; then
  echo "::error::no Dockerfile runs 'go work sync' — this guard is checking nothing."
  exit 1
fi

for df in $dockerfiles; do
  df=${df#./}
  echo "checking $df"

  # Only COPY lines count. A path in a comment is not a COPY.
  copies=$(grep -E '^[[:space:]]*COPY[[:space:]]' "$df" || true)

  # Forward: every workspace module must be copied in.
  for m in $modules; do
    if ! printf '%s\n' "$copies" | grep -q "$m/go\.mod"; then
      echo "::error file=$df::$df runs 'go work sync' but never COPYs $m/go.mod — add: COPY $m/go.mod $m/go.sum* ./$m/"
      fail=1
    fi
  done

  # Reverse: a COPYed module that left go.work is a stale line that will fail
  # the build on a path that no longer exists.
  copied=$(printf '%s\n' "$copies" \
    | tr ' ' '\n' \
    | sed -n 's|^\./||; s|/go\.mod$||p')
  for c in $copied; do
    # -F -x: exact whole-line match against the newline-separated module list.
    if ! printf '%s\n' "$modules" | grep -Fxq "$c"; then
      echo "::error file=$df::$df COPYs $c/go.mod but $c is not in go.work — remove the line or add the module."
      fail=1
    fi
  done
done

if [ "$fail" -ne 0 ]; then
  echo
  echo "go.work and the Dockerfiles have drifted apart. Keep them in step."
  exit 1
fi

echo "OK: every go.work module is COPYed into each Dockerfile that runs 'go work sync'."
