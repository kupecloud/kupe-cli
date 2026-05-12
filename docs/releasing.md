# Releasing — cutting a kupe-cli release

This document is the runbook for shipping the CLI. Read
[distribution.md](./distribution.md) for the design rationale;
this file is the operational "what do I click / run".

## One-time setup

Do this once, per org, before the first release.

### 1. Create the Homebrew tap repo

- GitHub: **`kupecloud/homebrew-tap`** (must start with `homebrew-`; the
  `-tap` suffix lets users type `kupecloud/tap/kupe`).
- Empty repo, public. No formula is needed — GoReleaser writes it on the
  first release.

### 2. Create the Scoop bucket repo

- GitHub: **`kupecloud/scoop-bucket`** (name is conventional; can be
  anything).
- Empty, public. Same story: GoReleaser writes `bucket/kupe.json` on the
  first release.

### 3. Mint two fine-grained PATs

Both PATs are **fine-grained**, **scoped to a single repo**, **contents:
write** only. Use the `kupecloud-bot` machine account if one exists;
otherwise create them under a service user.

| PAT | Repo | Used for |
|-----|------|----------|
| `HOMEBREW_TAP_TOKEN` | `kupecloud/homebrew-tap` | GoReleaser pushes the generated cask |
| `SCOOP_BUCKET_TOKEN` | `kupecloud/scoop-bucket` | GoReleaser pushes the generated manifest |

Add both as **repository secrets** on `kupecloud/kupe-cli`
(`Settings → Secrets and variables → Actions → New repository secret`).

### 4. Host the install script

The [`scripts/install.sh`](../scripts/install.sh) in this repo is the canonical
source. `https://get.kupe.cloud` is a **Cloudflare Redirect Rule** on the
`kupe.cloud` zone that 302s to:

```text
https://raw.githubusercontent.com/kupecloud/kupe-cli/main/scripts/install.sh
```

Users see the rewritten URL transparently when they run:

```bash
curl -fsSL https://get.kupe.cloud | sh
```

`curl -L` follows the redirect, GitHub serves the script straight from `main`.
No separate repo, no Worker, no Pages project — updates ship the moment the
PR lands. The script then fetches the latest release tag from the GitHub API
and downloads the matching `kupe_<version>_<os>_<arch>.tar.gz` from the
releases page.

To change which branch/path the redirect targets, edit the Redirect Rule
in the Cloudflare dashboard (`kupe.cloud` zone → Rules → Redirect Rules).

### 5. Cosign keyless + Syft

No setup required — the `.github/workflows/publish.yaml` installs both via
their official GitHub Actions (`sigstore/cosign-installer`,
`anchore/sbom-action/download-syft`). Signatures use GitHub OIDC (the
workflow requests `id-token: write`).

---

## Cutting a release

Releases are **automatic** — merging a conventional commit into `main`
triggers the pipeline. No manual tagging.

### 1. Author a conventional commit

The commit message drives the version bump via
[conventional-changelog's ruleset](../github-workflows/.github/workflows/semantic-release.yaml):

| Prefix | Bump | Example |
|--------|------|---------|
| `feat:` | minor | `feat: add kupe secret rotate` |
| `fix:` / `perf:` / `refactor:` | patch | `fix: retry 429 once with Retry-After` |
| `docs:` / `chore:` / `ci:` / `test:` / `style:` | none | `docs: update releasing.md` |
| `BREAKING CHANGE:` in body | major | add footer `BREAKING CHANGE: -o json shape changed` |

### 2. Merge to main

```bash
make test lint sec          # clean checkout on the PR branch
make snapshot               # sanity-check the goreleaser config
# … PR review → squash-merge to main
```

`make snapshot` runs GoReleaser with `--skip=publish,sign,sbom` and drops
the six archives + manifests into `dist/`. Inspect
`dist/homebrew/Casks/kupe.rb` and `dist/scoop/kupe.json` — those are
exactly what'll land in the tap/bucket.

### 3. Pipeline takes over

`.github/workflows/main.yaml` runs on every merge to `main`:

1. `go-lint`, `action-lint`, `unit-tests`, `gosec` — the usual CI gate.
2. `release` — delegates to `kupecloud/github-workflows/.github/workflows/semantic-release.yaml`, which:
   - Analyses commits since the last tag.
   - If a release-worthy commit is present, creates tag `vX.Y.Z`, generates a
     Conventional-Changelog release body, and publishes a GitHub Release.
   - Emits `new_release_published=true` + `new_release_version=X.Y.Z`.
3. `publish` — local [`publish.yaml`](./.github/workflows/publish.yaml),
   conditional on `new_release_published=='true'`. Checks out `vX.Y.Z`,
   runs `goreleaser release --clean`. With `release.mode: append` in the
   goreleaser config, goreleaser:
   - Builds 6 archives (darwin/linux/windows × amd64/arm64).
   - Signs `kupe_<version>_checksums.txt` with Cosign keyless.
   - Generates per-archive SPDX SBOMs via Syft.
   - Appends all artifacts to the existing GitHub Release.
   - Pushes the cask to `kupecloud/homebrew-tap`.
   - Pushes the manifest to `kupecloud/scoop-bucket`.

### Manual release (break-glass)

If semantic-release is misbehaving, you can cut a release by hand:

```bash
git tag -a v0.1.0 -m "kupe-cli v0.1.0"
git push origin v0.1.0
```

In that case you'll also need to manually trigger publish.yaml via
`workflow_dispatch` (not currently wired — see the "Hotfix path" below).

### 3. Verify

Wait ~5 minutes for the release job to finish, then:

```bash
# Homebrew path
brew tap kupecloud/tap
brew install kupe
kupe version

# Scoop path (Windows)
scoop bucket add kupe https://github.com/kupecloud/scoop-bucket
scoop install kupe

# Install script
curl -fsSL https://get.kupe.cloud | sh
kupe version

# Signature verification (optional, recommended for docs)
VERSION=0.1.0
cosign verify-blob \
  --certificate-identity-regexp "^https://github.com/kupecloud/kupe-cli" \
  --certificate-oidc-issuer "https://token.actions.githubusercontent.com" \
  --signature https://github.com/kupecloud/kupe-cli/releases/download/v${VERSION}/kupe_${VERSION}_checksums.txt.sig \
  <(curl -fsSL https://github.com/kupecloud/kupe-cli/releases/download/v${VERSION}/kupe_${VERSION}_checksums.txt)
```

`kupe version` should print the new tag, the short commit, and the commit
date — all populated by `-ldflags` during the release build.

### 4. Announce

- Post the release link in the internal Slack.
- Update [docs-public](../docs-public/) install guide if anything about
  the UX changed.

---

## What can go wrong

| Symptom | Likely cause | Fix |
|---------|--------------|-----|
| Release job fails at "publishing to homebrew-tap" | `HOMEBREW_TAP_TOKEN` missing / wrong scope | Regenerate PAT with contents:write on `kupecloud/homebrew-tap` |
| `brew install kupecloud/tap/kupe` 404s for a platform | Archive missing for that OS/arch — likely a build failure | Check the release job's "build binaries" step |
| `cosign verify-blob` fails | OIDC identity regex doesn't match (e.g., repo moved) | Update the `--certificate-identity-regexp` in docs |
| Install script prints "could not resolve latest version" | GitHub API rate limit (unauthenticated calls are 60/h per IP) | Pass `--version X.Y.Z` explicitly, or wait |
| `make snapshot` fails on `syft` / `cosign` | Local machine doesn't have them | Expected — snapshot uses `--skip=sign,sbom`; ignore unless running `goreleaser release` directly |
| Binary prints `dev (commit none, built 0001-01-01...)` | Snapshot build, not a real release | Use a signed release; snapshots don't populate ldflags |

---

## Hotfix path

For urgent patches (e.g., leaked-token rotation, security CVE):

```bash
git checkout main
git pull
git checkout -b hotfix/token-leak
# ... fix ...
git commit -m "fix: rotate leaked admin token"  # fix: → patch bump
# ... PR → merge to main
```

On merge, semantic-release cuts `vX.Y.Z+1` and the publish pipeline attaches
artifacts. Turnaround is ~8 minutes from merge to `brew upgrade kupe`
working.

If `main` is frozen and you need to patch an older line (rare), branch off
the old tag, cherry-pick the fix, and push the branch + a manual tag. See
"Manual release (break-glass)" above.

---

## Rolling back

GitHub Releases can be deleted, but Homebrew users with the old formula
cached will still have a working install (the tap repo's `kupe.rb`
contains URLs + SHAs pinned to the deleted release — `brew install` will
404 until you either re-publish or push a corrected formula).

Safer procedure:

1. Cut `v0.1.2` reverting the bad change. Normal release.
2. Delete the bad v0.1.1 release from GitHub (keep the tag so history is
   preserved — use `git push origin :refs/tags/v0.1.1` only if the tag
   itself was misplaced).

Never force-push over a published tag.
