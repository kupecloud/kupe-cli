#!/usr/bin/env bash
# Fail when the Dockerfile's golang builder image is older than the go.mod
# `go` directive. The builder sets GOTOOLCHAIN=local, so an older builder
# breaks the image build at the next release, weeks after the go.mod bump.
# Same logic as the `go version drift` job of github-workflows go-lint.yaml
# (kupecloud/github-workflows#14 moved it here). No-ops when there is no
# Dockerfile, no go.mod, no golang: builder, or a version it cannot parse.
set -euo pipefail

if [ ! -f Dockerfile ] || [ ! -f go.mod ]; then
  echo "No Dockerfile or go.mod - nothing to check."
  exit 0
fi

gomod=$(grep -oE '^go [0-9]+(\.[0-9]+)*' go.mod | head -1 | awk '{print $2}' || true)
builder=$(grep -oE 'golang:[0-9]+(\.[0-9]+)*' Dockerfile | head -1 | cut -d: -f2 || true)

if [ -z "$gomod" ] || [ -z "$builder" ]; then
  echo "Could not parse versions (go.mod='$gomod' builder='$builder') - skipping."
  exit 0
fi

# sort -V puts the lower version first; the builder is fine when go.mod is
# the lower of the two (equal versions also land go.mod first).
lower=$(printf '%s\n%s\n' "$gomod" "$builder" | sort -V | head -1)
if [ "$lower" != "$gomod" ]; then
  echo "Dockerfile: builder image golang:$builder is OLDER than the go.mod directive (go $gomod)." >&2
  echo "The builder sets GOTOOLCHAIN=local, so the image build will fail with 'go.mod requires go >= $gomod'." >&2
  echo "Bump the Dockerfile FROM tag (and its digest, if pinned) to >= $gomod." >&2
  exit 1
fi

echo "OK: builder golang:$builder >= go.mod go $gomod"
