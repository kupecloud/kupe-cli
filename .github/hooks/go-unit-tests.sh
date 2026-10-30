#!/usr/bin/env bash
# go vet, then the unit tests with the race detector and a coverage report.
# Same commands as the `Run unit tests` job of github-workflows
# go-unit-tests.yaml (kupecloud/github-workflows#14 moved it here). Coverage
# is reported, not gated (standards/testing-and-gates rule 15): the total is
# printed at the end.
set -euo pipefail

export GOWORK=off

go vet ./...

cover="$(mktemp)"
trap 'rm -f "$cover"' EXIT

# -race needs cgo; runner images default CGO_ENABLED=0.
CGO_ENABLED=1 go test -race -coverprofile="$cover" ./...

total="$(go tool cover -func="$cover" | awk 'END {print $3}')"
echo "Total coverage: ${total}"
