#!/bin/sh
# Generates shell completions and man(1) pages into ./completions/ and
# ./manpages/ ahead of the goreleaser archive step. Pattern mirrors
# goreleaser/goreleaser's own scripts/completions_and_manpages.sh.
#
# Used by:
#   - .goreleaser.yaml `before.hooks` on every release / snapshot
#   - `make manpages` Makefile target for local previews
#
# Output layout (consumed by archives.files in .goreleaser.yaml and by
# the homebrew_casks `manpages:` / `completions:` stanzas):
#
#   completions/kupe.bash
#   completions/kupe.zsh
#   completions/kupe.fish
#   manpages/kupe.1.gz                    (root command)
#   manpages/kupe-<subcmd>.1.gz           (one per subcommand)
#
# We gzip the manpages (-n keeps the result reproducible by stripping
# the original filename + mtime from the gzip header — important so
# release artifacts hash identically when rebuilt from the same commit).

set -eu

cd "$(git rev-parse --show-toplevel)"

rm -rf completions manpages
mkdir -p completions manpages

# Build flags must match Makefile so the version embedded in --version
# output matches the rest of the release.
GOFLAGS="-mod=vendor"
LDFLAGS="-s -w \
  -X github.com/kupecloud/kupe-cli/internal/build.Version=${VERSION:-dev} \
  -X github.com/kupecloud/kupe-cli/internal/build.Commit=${COMMIT:-$(git rev-parse --short HEAD 2>/dev/null || echo unknown)} \
  -X github.com/kupecloud/kupe-cli/internal/build.Date=${DATE:-$(git log -1 --format=%cI 2>/dev/null || echo unknown)}"

for sh in bash zsh fish; do
  go run $GOFLAGS -ldflags "$LDFLAGS" ./cmd/kupe completion "$sh" \
    > "completions/kupe.$sh"
done

go run $GOFLAGS -ldflags "$LDFLAGS" ./cmd/kupe man manpages
gzip -n -f manpages/*.1
