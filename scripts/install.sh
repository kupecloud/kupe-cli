#!/bin/sh
# kupe CLI installer.
#
# Published at https://get.kupe.cloud. Users run:
#
#     curl -fsSL https://get.kupe.cloud | sh
#
# Flags:
#   --version X.Y.Z        Pin a specific release (default: latest).
#   --install-dir PATH     Install directory (default: /usr/local/bin, or
#                          ~/.local/bin with --user).
#   --user                 Install into ~/.local/bin without sudo.
#   --help                 Print this help.
#
# Environment overrides (all optional):
#   KUPE_VERSION           Same as --version.
#   KUPE_INSTALL_DIR       Same as --install-dir.
#   KUPE_REPO              github owner/name (default: kupecloud/kupe-cli).
#
# The script is POSIX sh — no bash features — so it runs under dash / busybox
# ash as well as bash. Fails fast on any error (set -eu).

set -eu

REPO=${KUPE_REPO:-kupecloud/kupe-cli}
VERSION=${KUPE_VERSION:-}
INSTALL_DIR=${KUPE_INSTALL_DIR:-}
USER_INSTALL=0

# ---------------------------------------------------------------------------
# helpers

log() { printf '%s\n' "$*" >&2; }
err() { printf 'error: %s\n' "$*" >&2; exit 1; }

usage() {
  sed -n 's/^# \{0,1\}//p' "$0" | sed -n '3,25p'
  exit 0
}

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || err "missing required command: $1"
}

detect_os() {
  case "$(uname -s)" in
    Linux)  printf 'linux' ;;
    Darwin) printf 'darwin' ;;
    *) err "unsupported OS: $(uname -s)" ;;
  esac
}

detect_arch() {
  case "$(uname -m)" in
    x86_64|amd64) printf 'amd64' ;;
    arm64|aarch64) printf 'arm64' ;;
    *) err "unsupported architecture: $(uname -m)" ;;
  esac
}

# latest_version asks the GitHub API for the most recent non-prerelease tag.
# Falls back to scraping the release HTML if jq isn't around.
latest_version() {
  api="https://api.github.com/repos/${REPO}/releases/latest"
  if command -v jq >/dev/null 2>&1; then
    curl -fsSL "$api" | jq -r '.tag_name' | sed 's/^v//'
  else
    # Grep the first "tag_name" field; strips quotes and a leading v.
    curl -fsSL "$api" \
      | grep -m1 '"tag_name"' \
      | sed -E 's/.*"tag_name"[[:space:]]*:[[:space:]]*"v?([^"]+)".*/\1/'
  fi
}

# ---------------------------------------------------------------------------
# flag parsing

while [ $# -gt 0 ]; do
  case "$1" in
    --version)       VERSION="$2"; shift 2 ;;
    --version=*)     VERSION="${1#*=}"; shift ;;
    --install-dir)   INSTALL_DIR="$2"; shift 2 ;;
    --install-dir=*) INSTALL_DIR="${1#*=}"; shift ;;
    --user)          USER_INSTALL=1; shift ;;
    -h|--help)       usage ;;
    *) err "unknown argument: $1" ;;
  esac
done

require_cmd curl
require_cmd tar
require_cmd uname

OS=$(detect_os)
ARCH=$(detect_arch)

if [ -z "$VERSION" ]; then
  log "resolving latest release tag..."
  VERSION=$(latest_version) || err "could not resolve latest version"
fi
VERSION=${VERSION#v}  # strip any leading v
[ -n "$VERSION" ] || err "version is empty"

if [ -z "$INSTALL_DIR" ]; then
  if [ "$USER_INSTALL" = 1 ]; then
    INSTALL_DIR="${HOME}/.local/bin"
  else
    INSTALL_DIR="/usr/local/bin"
  fi
fi

# ---------------------------------------------------------------------------
# download + verify

ARCHIVE="kupe_${VERSION}_${OS}_${ARCH}.tar.gz"
URL="https://github.com/${REPO}/releases/download/v${VERSION}/${ARCHIVE}"
CHECKSUMS_URL="https://github.com/${REPO}/releases/download/v${VERSION}/kupe_${VERSION}_checksums.txt"

tmp=$(mktemp -d 2>/dev/null || mktemp -d -t kupe-install)
trap 'rm -rf "$tmp"' EXIT INT TERM

log "downloading kupe ${VERSION} for ${OS}/${ARCH}..."
if ! curl -fSL --progress-bar -o "${tmp}/${ARCHIVE}" "$URL"; then
  err "download failed: $URL"
fi

log "verifying checksum..."
if ! curl -fsSL -o "${tmp}/checksums.txt" "$CHECKSUMS_URL"; then
  err "checksum download failed: $CHECKSUMS_URL"
fi

# Pick the right hashing tool — macOS uses shasum, Linux sha256sum.
if command -v sha256sum >/dev/null 2>&1; then
  hasher='sha256sum'
elif command -v shasum >/dev/null 2>&1; then
  hasher='shasum -a 256'
else
  err "need sha256sum or shasum to verify the archive"
fi

expected=$(grep " ${ARCHIVE}\$" "${tmp}/checksums.txt" | awk '{print $1}')
[ -n "$expected" ] || err "no checksum for ${ARCHIVE} in checksums.txt"

actual=$(cd "$tmp" && $hasher "$ARCHIVE" | awk '{print $1}')
if [ "$actual" != "$expected" ]; then
  err "checksum mismatch: expected $expected, got $actual"
fi

# ---------------------------------------------------------------------------
# extract + install

log "extracting..."
tar -xzf "${tmp}/${ARCHIVE}" -C "$tmp" kupe || err "archive missing 'kupe' binary"
chmod +x "${tmp}/kupe"

mkdir -p "$INSTALL_DIR" 2>/dev/null || true
if [ -w "$INSTALL_DIR" ]; then
  mv "${tmp}/kupe" "${INSTALL_DIR}/kupe"
else
  log "installing to ${INSTALL_DIR} requires sudo..."
  sudo mv "${tmp}/kupe" "${INSTALL_DIR}/kupe"
fi

log "installed: ${INSTALL_DIR}/kupe"
log "verifying..."
"${INSTALL_DIR}/kupe" version || true

# PATH hint — only when the install dir isn't on $PATH already.
case ":${PATH}:" in
  *":${INSTALL_DIR}:"*) ;;
  *) log ""; log "note: ${INSTALL_DIR} is not on your PATH — add:"; log "  export PATH=\"${INSTALL_DIR}:\$PATH\"" ;;
esac
