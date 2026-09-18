#!/usr/bin/env bash
# Uploading a release's binaries, one file at a time with retries.
#
# `--clobber` removes the old asset just before uploading its replacement, so
# doing it per file keeps that window to one binary for a few seconds rather
# than wiping every asset up front. A stalled upload then costs a retry, not
# the release: on 2026-09-17 the batch form left `cli-latest` with arm64 and no
# amd64 until the job was re-run, and `get.sh` on amd64 got a 404 in between.
#
# Sourced by the workflow so the edge release and a tagged one cannot drift.
upload_binaries() {
  local tag="$1"
  local failed=""

  for file in dist/*; do
    local name
    name=$(basename "$file")
    for attempt in 1 2 3; do
      if gh release upload "$tag" "$file" --clobber; then
        name=""
        break
      fi
      echo "::warning::upload of ${name} to ${tag} failed (attempt ${attempt})"
      sleep $((attempt * 10))
    done
    if [ -n "$name" ]; then
      failed="${failed} ${name}"
    fi
  done

  if [ -n "$failed" ]; then
    echo "::error::could not upload to ${tag}:${failed}"
    return 1
  fi
}
