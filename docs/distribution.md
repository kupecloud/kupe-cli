---
title: "Distribution & Releases"
description: "goreleaser matrix, Homebrew tap, Scoop bucket, install script, signing, SBOMs, and CI release pipeline for the kupe CLI"
owner: platform-team
lastReviewed: "2026-04-20"
sidebar:
  order: 9
---

The CLI is shipped as pre-built statically linked binaries via goreleaser, a Homebrew tap, a Scoop bucket, and a `curl | sh` install script. All releases are signed and ship with SBOMs. This doc is the reference for the release pipeline — anyone cutting a release should be able to run through it end-to-end from here.

## Release matrix

Built per release by [goreleaser](https://goreleaser.com/):

| OS | Arch | Format |
|----|------|--------|
| darwin | amd64 | tar.gz |
| darwin | arm64 | tar.gz |
| linux | amd64 | tar.gz |
| linux | arm64 | tar.gz |
| windows | amd64 | zip |
| windows | arm64 | zip |

Optional (Phase 2): `.deb` / `.rpm` via `nfpms`, published to `pkg.kupe.cloud` as simple apt/yum repos.

### Version injection

`main` imports `internal/build`:

```go
// internal/build/info.go
package build

var (
    Version   = "dev"
    Commit    = "none"
    Date      = "unknown"
    GoVersion = runtime.Version()
)
```

Goreleaser's `ldflags` overrides at link time:

```yaml
builds:
  - main: ./cmd/kupe
    binary: kupe
    env: [CGO_ENABLED=0]
    ldflags:
      - -s -w
      - -X github.com/kupecloud/kupe-cli/internal/build.Version={{.Version}}
      - -X github.com/kupecloud/kupe-cli/internal/build.Commit={{.ShortCommit}}
      - -X github.com/kupecloud/kupe-cli/internal/build.Date={{.CommitDate}}
    goos: [darwin, linux, windows]
    goarch: [amd64, arm64]
```

`kupe version -o json` reads these values — see [commands.md](./commands.md).

### Static linking and CGO

`CGO_ENABLED=0` across all targets. The keyring library (`zalando/go-keyring`) uses OS-native binaries (`security` on macOS, `dbus` over `secretstore` on Linux, `wincred` on Windows) via subprocess or pure-Go syscalls — no C dependencies. This keeps cross-compiles clean and avoids libc versioning trouble on old Linux distros.

## `.goreleaser.yaml` outline

```yaml
project_name: kupe   # binary + Homebrew formula name (`brew install kupecloud/tap/kupe`)

before:
  hooks:
    - go mod vendor
    - go test ./...

builds:
  - id: kupe
    main: ./cmd/kupe
    binary: kupe
    env: [CGO_ENABLED=0]
    ldflags: [...]  # see above
    goos: [darwin, linux, windows]
    goarch: [amd64, arm64]

archives:
  - id: kupe
    name_template: "kupe_{{ .Version }}_{{ .Os }}_{{ .Arch }}"
    format_overrides:
      - goos: windows
        format: zip
    files:
      - README.md
      - LICENSE

checksum:
  name_template: "kupe_{{ .Version }}_checksums.txt"
  algorithm: sha256

signs:
  - cmd: cosign
    args: ["sign-blob", "--yes", "--output-signature=${signature}", "${artifact}"]
    artifacts: checksum

sboms:
  - artifacts: archive
    documents:
      - "{{ .ArtifactName }}.sbom.spdx.json"

brews:
  - tap:
      owner: kupecloud
      name: homebrew-tap
    homepage: "https://kupe.cloud"
    description: "Official CLI for Kupe managed Kubernetes clusters"
    license: "Apache-2.0"
    test: |
      system "#{bin}/kupe", "version"
    install: |
      bin.install "kupe"

scoops:
  - bucket:
      owner: kupecloud
      name: scoop-bucket
    homepage: "https://kupe.cloud"
    description: "Official CLI for Kupe managed Kubernetes clusters"
    license: "Apache-2.0"

release:
  github:
    owner: kupecloud
    name: kupe-cli
  draft: false
  prerelease: auto
  footer: |
    ## Install

    ```bash
    brew install kupecloud/tap/kupe
    # or
    curl -fsSL https://get.kupe.cloud | sh
    ```

    Full changelog: https://github.com/kupecloud/kupe-cli/compare/{{ .PreviousTag }}...{{ .Tag }}

changelog:
  use: github
  sort: asc
  filters:
    exclude:
      - "^docs:"
      - "^chore:"
      - "^test:"
```

## Signing

[Cosign](https://github.com/sigstore/cosign) keyless signing on the `checksums.txt`:

```bash
cosign sign-blob --yes --output-signature kupe_0.1.0_checksums.txt.sig kupe_0.1.0_checksums.txt
```

- **Keyless** — no long-lived private key. Cosign uses the Sigstore OIDC identity from the GitHub Actions token (`id-token: write` permission). Verification goes through Fulcio + Rekor transparency log.
- **What's signed** — the `checksums.txt`, not individual archives. An archive is trusted transitively: verify the checksum file is signed, verify the archive matches the listed hash.

Consumers verify with:

```bash
cosign verify-blob \
  --certificate-identity-regexp "^https://github.com/kupecloud/kupe-cli" \
  --certificate-oidc-issuer "https://token.actions.githubusercontent.com" \
  --signature kupe_0.1.0_checksums.txt.sig \
  kupe_0.1.0_checksums.txt
```

Documented on the release page and in [docs-public](../../docs-public/) install guide.

## SBOMs

[`anchore/syft`](https://github.com/anchore/syft), integrated natively into goreleaser. One SPDX JSON per archive:

```
kupe_0.1.0_darwin_arm64.tar.gz.sbom.spdx.json
```

Published to GitHub Release alongside the archive. Vulnerability scanning can consume SBOMs via `grype sbom:kupe_0.1.0_darwin_arm64.tar.gz.sbom.spdx.json`.

## Homebrew tap

Tap repo: [`kupecloud/homebrew-tap`](https://github.com/kupecloud/homebrew-tap) (needs creating; see [Open items](#open-items)).

Goreleaser auto-pushes an updated formula on each release. Users install with:

```bash
brew install kupecloud/tap/kupe
# or
brew tap kupecloud/tap && brew install kupe
```

Upgrades via `brew upgrade kupe`. No self-update logic in the CLI.

### macOS universal binary

Not for v1. Separate `darwin_amd64` and `darwin_arm64` binaries. Users on Intel Macs get amd64; users on Apple Silicon get arm64. Rosetta works as a fallback.

## Scoop bucket

Bucket repo: [`kupecloud/scoop-bucket`](https://github.com/kupecloud/scoop-bucket).

```powershell
scoop bucket add kupe https://github.com/kupecloud/scoop-bucket
scoop install kupe
scoop update kupe
```

Auto-managed by goreleaser.

## Install script

Hosted at `https://get.kupe.cloud`. Simple POSIX shell script:

1. Detect `$OSTYPE` → darwin / linux.
2. Detect `uname -m` → amd64 / arm64.
3. Fetch `https://github.com/kupecloud/kupe-cli/releases/latest/download/kupe_<version>_<os>_<arch>.tar.gz`.
4. Fetch `kupe_<version>_checksums.txt` and verify.
5. Extract and move `kupe` to `/usr/local/bin/kupe` (or `~/.local/bin/kupe` with `--user`, or `$KUPE_INSTALL_DIR`).
6. `chmod +x` and print success.

```bash
curl -fsSL https://get.kupe.cloud | sh
curl -fsSL https://get.kupe.cloud | sh -s -- --version=0.1.0
curl -fsSL https://get.kupe.cloud | sh -s -- --user
```

Script lives in a separate repo or the `docs-public` repo under `static/`. Cloudflare fronts `get.kupe.cloud` and serves the script with a 5-minute cache.

### No arbitrary code execution from sub-shells

The script does not `curl | sh` into nested shells or pull additional scripts. Everything it needs is in the one downloaded binary. Users who don't trust the script can read it at `https://get.kupe.cloud` before running.

## Versioning policy

- **Semver 2.0.0**.
- **v0.x** — interface can change. Deprecated flags get a `stderr` deprecation warning for at least one minor version before removal.
- **v1.0** gate:
  - CLI has parity with terraform-provider-kupe v1.
  - TUI has shipped.
  - `kupe-api` has committed to a tagged stable version with an endpoint-stability SLA.
- **Patch releases** fix bugs only — no new flags or commands.
- **Minor releases** add commands, flags, and non-breaking output additions.
- **Major releases** break `-o json` schemas or remove commands/flags.

### Tag scheme

Git tags prefixed `v`: `v0.1.0`, `v0.2.0-rc.1`, `v1.0.0`. Goreleaser strips the `v` for archive names.

### Release notes

Auto-generated from commit messages by goreleaser (`changelog.use: github`). Commit messages follow a light convention:

```
feat: add kupe cluster wait command
fix: restore --wait=false default for cluster delete
docs: document exec-plugin kubeconfig mode
chore: bump go to 1.26.2
```

`feat:` and `fix:` show up in release notes; `docs:`/`chore:`/`test:` are filtered out.

## GitHub Actions

### `.github/workflows/ci.yaml` (PRs)

- Checkout, setup-go 1.26.2, vendor, lint, test, sec, snapshot build.
- Cancels in-progress runs on subsequent pushes to the same PR.

### `.github/workflows/release.yaml` (tags)

- Triggers on `push: tags: ["v*"]`.
- Permissions: `contents: write`, `packages: write`, `id-token: write` (for Cosign keyless).
- Steps: checkout with full history (for changelog), setup-go 1.26.2, setup-cosign, setup-syft, `goreleaser release --clean`.
- Secrets required:
  - `GITHUB_TOKEN` (default).
  - `HOMEBREW_TAP_TOKEN` — PAT with `repo` on `kupecloud/homebrew-tap`.
  - `SCOOP_BUCKET_TOKEN` — PAT with `repo` on `kupecloud/scoop-bucket`.
- Duration target: under 10 minutes.

### `.github/workflows/docs.yaml` (PRs to docs/)

- Validates frontmatter shape (required keys present).
- Checks relative links resolve inside the repo.
- Warns on `lastReviewed` older than 6 months.

## Reproducible builds

Goreleaser writes `buildDate` from the commit author date (`{{ .CommitDate }}`), not wall-clock time. Combined with fixed Go version, vendored deps, and `-trimpath` in ldflags, two builds from the same commit produce byte-identical binaries.

Verifiable by checking out a release tag and re-running goreleaser locally — the resulting `dist/kupe_<ver>_<os>_<arch>.tar.gz` should match the published archive's sha256 in `kupe_<ver>_checksums.txt`. (No dedicated `make` target yet; if reproducibility ever needs CI enforcement, add one then.)

## Uninstalling

Documented in README and docs-public. Steps:

1. `brew uninstall kupe` or `scoop uninstall kupe` (or `rm /usr/local/bin/kupe` for install-script users).
2. `rm -rf ~/.config/kupe/`.
3. On macOS: `security delete-generic-password -s kupe-cli` per context.
4. On Linux: `secret-tool clear service kupe-cli` per context.
5. On Windows: `cmdkey /delete:kupe-cli:<context>` per context.

No automated uninstaller in v1 — Homebrew/Scoop handle the binary; users own the config and secrets.

## Open items

1. **`kupecloud` GitHub org tap repos.** Confirm `kupecloud/homebrew-tap` and `kupecloud/scoop-bucket` exist (or create them) before the first release.
2. **`get.kupe.cloud` infrastructure.** Cloudflare page or a static bucket behind Cloudflare. Infrastructure ticket separate.
3. **Deb/RPM repos.** `pkg.kupe.cloud` is not scoped for v1. Add via `nfpms` + a simple apt/yum repo when demand justifies it.
4. **Windows signing.** Long-term we want an Authenticode-signed binary. Deferred — the Cosign signature on the checksum file is enough for initial users. Windows Defender may warn on first run; document it in the install guide.
5. **ARM Mac universal binaries.** Not scoped. Re-evaluate after first release telemetry shows install distribution (if we ever turn on telemetry).
