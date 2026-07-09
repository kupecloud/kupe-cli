#!/bin/sh
# kupe CLI installer.
#
# Published at https://get.kupe.cloud. Users run:
#
#     curl -fsSL https://get.kupe.cloud | sh
#
# By default installs to ~/.local/bin (no sudo). For a system-wide install,
# pass --install-dir /usr/local/bin (will sudo).
#
# Flags:
#   --version X.Y.Z        Pin a specific release (default: latest).
#   --install-dir PATH     Install directory (default: ~/.local/bin).
#                          Pass /usr/local/bin or similar for a system-wide
#                          install — the script will sudo if needed.
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

# download_with_spinner downloads $1 to $2 while showing an animated braille
# spinner on stderr (`⠋ <label>...`), then prints a check-mark line with the
# downloaded size on completion (`✓ <label> (5.6 MB)`).
#
# Falls back to a plain "downloading <label>..." message when stderr isn't a
# TTY (CI, captured output, log redirection) — spinners would just spam such
# environments with control codes.
#
# Curl is intentionally invoked with `-fsSL` (no progress bar) because the
# spinner replaces curl's progress UI entirely. POSIX-portable: no bash-isms,
# no fractional sleep required (degrades to 1s ticks if sleep 0.1 is rejected).
download_with_spinner() {
  _url=$1
  _out=$2
  _label=$3

  if [ ! -t 2 ]; then
    log "downloading ${_label}..."
    curl -fsSL -o "$_out" "$_url"
    return $?
  fi

  curl -fsSL -o "$_out" "$_url" &
  _pid=$!
  # Ctrl-C during download should kill curl and exit cleanly, not leave a
  # half-downloaded file or a runaway curl background process.
  trap 'kill "$_pid" 2>/dev/null; printf "\n" >&2; exit 130' INT TERM

  _i=0
  while kill -0 "$_pid" 2>/dev/null; do
    case $((_i % 10)) in
      0) _f='⠋' ;;
      1) _f='⠙' ;;
      2) _f='⠹' ;;
      3) _f='⠸' ;;
      4) _f='⠼' ;;
      5) _f='⠴' ;;
      6) _f='⠦' ;;
      7) _f='⠧' ;;
      8) _f='⠇' ;;
      9) _f='⠏' ;;
    esac
    printf '\r%s %s' "$_f" "$_label" >&2
    sleep 0.1 2>/dev/null || sleep 1
    _i=$((_i + 1))
  done

  if wait "$_pid"; then
    _rc=0
  else
    _rc=$?
  fi
  trap - INT TERM

  # \r returns to col 0; \033[K clears to end of line — wipes the spinner row
  # before printing the checkmark line so we don't leave half a glyph behind.
  printf '\r\033[K' >&2

  if [ "$_rc" -eq 0 ]; then
    _bytes=$(wc -c < "$_out" 2>/dev/null | tr -d ' ')
    if [ -n "$_bytes" ] && [ "$_bytes" -gt 0 ]; then
      _human=$(awk -v b="$_bytes" 'BEGIN { printf "%.1f MB", b / 1024 / 1024 }')
      printf '✓ %s (%s)\n' "$_label" "$_human" >&2
    else
      printf '✓ %s\n' "$_label" >&2
    fi
  fi

  return $_rc
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
    # --user is now the default; accept and ignore for backwards compat
    # so anyone with `... | sh -s -- --user` in their docs/scripts keeps
    # working without surprise. Quiet enough that nobody notices.
    --user)          shift ;;
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
  INSTALL_DIR="${HOME}/.local/bin"
fi

# ---------------------------------------------------------------------------
# download + verify

ARCHIVE="kupe_${VERSION}_${OS}_${ARCH}.tar.gz"
URL="https://github.com/${REPO}/releases/download/v${VERSION}/${ARCHIVE}"
CHECKSUMS_URL="https://github.com/${REPO}/releases/download/v${VERSION}/kupe_${VERSION}_checksums.txt"

tmp=$(mktemp -d 2>/dev/null || mktemp -d -t kupe-install)
trap 'rm -rf "$tmp"' EXIT INT TERM

if ! download_with_spinner "$URL" "${tmp}/${ARCHIVE}" "kupe ${VERSION} for ${OS}/${ARCH}"; then
  err "download failed: $URL"
fi

log "verifying checksum..."
if ! curl -fsSL -o "${tmp}/checksums.txt" "$CHECKSUMS_URL"; then
  err "checksum download failed: $CHECKSUMS_URL"
fi

# Best-effort cosign signature verification of the checksums file (KC-21).
# The release pipeline keyless-signs kupe_<ver>_checksums.txt, producing a
# .sig and a .pem next to it. If cosign is on PATH we verify the signature
# against the publishing workflow's identity BEFORE trusting the checksums —
# this catches a compromised release/account, not just corrupt downloads.
# When cosign is absent we fall back to checksum-only and say so.
SIG_URL="${CHECKSUMS_URL}.sig"
CERT_URL="${CHECKSUMS_URL}.pem"
# semantic-release runs publish.yaml on `main`, so the Fulcio SAN is always
# publish.yaml@refs/heads/main — there is no tag trigger. Pinning @refs/tags/v<ver>
# never matched and hard-failed every install with cosign on PATH (KC-21).
COSIGN_IDENTITY_RE="^https://github.com/${REPO}/\.github/workflows/publish\.yaml@refs/heads/main$"
COSIGN_ISSUER="https://token.actions.githubusercontent.com"
if command -v cosign >/dev/null 2>&1; then
  if curl -fsSL -o "${tmp}/checksums.txt.sig" "$SIG_URL" \
    && curl -fsSL -o "${tmp}/checksums.txt.pem" "$CERT_URL"; then
    log "verifying cosign signature..."
    if cosign verify-blob \
      --certificate "${tmp}/checksums.txt.pem" \
      --signature "${tmp}/checksums.txt.sig" \
      --certificate-identity-regexp "$COSIGN_IDENTITY_RE" \
      --certificate-oidc-issuer "$COSIGN_ISSUER" \
      "${tmp}/checksums.txt" >/dev/null 2>&1; then
      log "cosign signature OK."
    else
      err "cosign signature verification failed for checksums.txt — refusing to install"
    fi
  else
    log "note: cosign signature artifacts not found for this release; falling back to checksum-only verification."
  fi
else
  log "note: cosign not found on PATH; skipping signature verification (checksum integrity only)."
  log "      install cosign (https://docs.sigstore.dev/cosign/installation/) for supply-chain verification."
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

# macOS Gatekeeper: curl tags downloads with com.apple.quarantine, which
# triggers a "could not verify is free of malware" dialog on first run for
# unsigned binaries. We strip the attribute here. The proper fix is Apple
# Developer ID notarization (planned alongside Kupe Cloud GA); until then,
# this is the documented Homebrew-style workaround. No-op on Linux.
if [ "$OS" = "darwin" ] && command -v xattr >/dev/null 2>&1; then
  xattr -dr com.apple.quarantine "${INSTALL_DIR}/kupe" 2>/dev/null || true
fi

# PATH hint — only when the install dir isn't on $PATH already. We default
# to ~/.local/bin which is on PATH out-of-the-box on most modern Linux
# distros (Ubuntu since 18.04 picks it up via ~/.profile) but NOT on macOS,
# so Mac users will typically see this on first run.
case ":${PATH}:" in
  *":${INSTALL_DIR}:"*)
    log "verifying..."
    "${INSTALL_DIR}/kupe" version || true
    ;;
  *)
    log ""
    log "Almost done — ${INSTALL_DIR} is not on your PATH yet."
    log "Add it by running ONE of these (matching your shell):"
    log ""
    log "  bash:  echo 'export PATH=\"${INSTALL_DIR}:\$PATH\"' >> ~/.bashrc && source ~/.bashrc"
    log "  zsh:   echo 'export PATH=\"${INSTALL_DIR}:\$PATH\"' >> ~/.zshrc  && source ~/.zshrc"
    log "  fish:  fish_add_path ${INSTALL_DIR}"
    log ""
    log "Then verify: kupe version"
    ;;
esac
