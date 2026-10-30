#!/usr/bin/env bash
# Fail when `go mod tidy` changes go.mod or go.sum. Same check as the
# `Check go mod tidy` job of github-workflows go-unit-tests.yaml
# (kupecloud/github-workflows#14 moved it here). The tidied files are left in
# place, so a local run also fixes them.
set -euo pipefail

before_dir="$(mktemp -d)"
trap 'rm -rf "$before_dir"' EXIT

for file in go.mod go.sum; do
  if [ -f "$file" ]; then
    cp "$file" "$before_dir/$file"
  fi
done

GOWORK=off go mod tidy

changed=0
for file in go.mod go.sum; do
  if [ -f "$before_dir/$file" ] && [ -f "$file" ]; then
    cmp -s "$before_dir/$file" "$file" || changed=1
  elif [ -f "$before_dir/$file" ] || [ -f "$file" ]; then
    changed=1
  fi
done

if [ "$changed" -eq 1 ]; then
  echo "go.mod or go.sum is not tidy. Run 'go mod tidy' and commit the changes." >&2
  for file in go.mod go.sum; do
    left=/dev/null
    right=/dev/null
    [ -f "$before_dir/$file" ] && left="$before_dir/$file"
    [ -f "$file" ] && right="$file"
    diff -u "$left" "$right" || true
  done
  exit 1
fi

echo "go.mod and go.sum are tidy"
