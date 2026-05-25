# Kupe CLI

The official command-line interface for [Kupe Cloud](https://kupe.cloud)

```bash
kupe cluster create prod --version 1.32 \
  --cpu-limit 2 --memory-limit 8Gi --storage-limit 50Gi
kupe cluster kubeconfig prod --merge
kubectl get pods
```

## Install

### macOS

Install script (no prerequisites, no sudo):

```bash
curl -fsSL https://get.kupe.cloud | sh
```

Installs to `~/.local/bin/kupe`. macOS doesn't have `~/.local/bin` on `PATH` by default; the script prints shell-specific one-liners to add it.

Or via Homebrew (auto-updates + completions + man page):

```bash
brew install kupecloud/tap/kupe
```

### Linux

Install script (no prerequisites, no sudo):

```bash
curl -fsSL https://get.kupe.cloud | sh
```

Installs to `~/.local/bin/kupe`. On most modern distros (Ubuntu since 18.04, Fedora, etc.) `~/.local/bin` is already on `PATH`.

Or via Homebrew (Linuxbrew):

```bash
brew install kupecloud/tap/kupe
```

### Windows

```powershell
scoop bucket add kupe https://github.com/kupecloud/scoop-bucket
scoop install kupe
```

### Other ways (any platform)

From source (requires Go 1.26+):

```bash
go install github.com/kupecloud/kupe-cli/cmd/kupe@latest
```

Direct download — pre-built binaries for darwin/linux/windows × amd64/arm64 are attached to each [GitHub release](https://github.com/kupecloud/kupe-cli/releases). Each release also ships a Cosign-signed `checksums.txt` and SBOMs (SPDX JSON) per archive.

### Install script — flags

```bash
# Pin a version (default: latest)
curl -fsSL https://get.kupe.cloud | sh -s -- --version 1.1.3

# System-wide install instead of ~/.local/bin (will sudo)
curl -fsSL https://get.kupe.cloud | sh -s -- --install-dir /usr/local/bin
```

## Code signing status

> While the wider Kupe Cloud platform is still phasing through alpha, the
> `kupe` binary is not yet Apple-notarized or Authenticode-signed. We strip
> the relevant OS attributes automatically on the install paths that can do
> so (install script, Homebrew cask) — full signing will land alongside the
> platform GA.

- **macOS** — binaries are not yet Apple-notarized. The install script and Homebrew cask both strip `com.apple.quarantine` automatically, so the curl-pipe and `brew install` paths don't trigger Gatekeeper. If you downloaded a binary directly from a release page and hit the "could not verify" dialog, run once: `xattr -dr com.apple.quarantine "$(which kupe)"`.
- **Windows** — binaries are not yet Authenticode-signed, so SmartScreen shows "Windows protected your PC" on first run. Click **More info → Run anyway**. Azure Trusted Signing will replace this workaround.
- **Linux** — no warning. Binaries are checksum-verified by the install script and via Cosign keyless signing on `checksums.txt` for users who want to verify manually (`cosign verify-blob …`).

## Quickstart

```bash
# 1. Log in. The default --method=oidc runs an OAuth2 device-code flow:
#    the CLI prints a short user code and a verification URL, opens your
#    browser (best-effort), and waits for you to approve. Works on a
#    laptop, an SSH session, or a CI runner — no localhost listener.
#    Use --method=token to paste a long-lived API key from
#    https://console.kupe.cloud/settings/api-keys (CI / scripts).
kupe auth login --tenant acme-corp

# 2. Create a cluster
kupe cluster create dev --cpu-limit 2 --memory-limit 8Gi --storage-limit 50Gi

# 3. Merge its kubeconfig into ~/.kube/config
kupe cluster kubeconfig dev --merge

# 4. Use it
kubectl --context kupe-<tenant>-dev get pods -A

# 5. Tear it down
kupe cluster delete dev
```

Full getting-started guide: [docs.kupe.cloud/cli/getting-started](https://docs.kupe.cloud/cli/getting-started).

## CI usage

```bash
# Token from env var — no config file needed
export KUPE_API_TOKEN="kupe_abc_..."
export KUPE_TENANT="acme-corp"

# JSON output, no TTY niceties
kupe cluster create "ci-$GITHUB_SHA" \
  --cpu-limit 2 --memory-limit 8Gi --storage-limit 50Gi \
  --wait -o json
```

The CLI auto-detects non-TTY environments and switches off colors, spinners, and prompts. See [docs/design.md](./docs/design.md) for the full interactive-vs-CI contract.

## Command overview

| Command | Purpose |
| ------- | ------- |
| `kupe auth login` | Sign in with OIDC or store an API token |
| `kupe auth whoami` | Show who you're logged in as |
| `kupe cluster list` | List clusters in the current tenant |
| `kupe cluster create NAME` | Create a new cluster |
| `kupe cluster kubeconfig NAME` | Fetch or merge a kubeconfig |
| `kupe cluster delete NAME` | Delete a cluster |
| `kupe apikey create` | Mint a new API token |
| `kupe config use-context NAME` | Switch between environments / tenants |

Run `kupe --help` or see [docs/commands.md](./docs/commands.md) for the full reference.

## Configuration

Config lives at `~/.config/kupe/config.yaml`. Tokens are stored in the OS keyring (Keychain / Secret Service / Credential Manager), not in the config file.

All settings can be overridden by flags or `KUPE_*` environment variables. See [docs/auth.md](./docs/auth.md).

`kupe cluster create --type` is still supported; it defaults to `shared`, so most examples omit it. Use `--type dedicated` only when you need a dedicated cluster type.

## Documentation

- [docs/architecture.md](./docs/architecture.md) — runtime model and package layout
- [docs/design.md](./docs/design.md) — UX principles, exit codes, command grammar
- [docs/commands.md](./docs/commands.md) — full command reference
- [docs/auth.md](./docs/auth.md) — authentication, config, and token storage
- [docs/api-client.md](./docs/api-client.md) — HTTP client conventions
- [docs/output.md](./docs/output.md) — `-o` formats and TTY behavior
- [docs/testing.md](./docs/testing.md) — unit, golden, and live-test patterns
- [docs/distribution.md](./docs/distribution.md) — release pipeline design
- [docs/releasing.md](./docs/releasing.md) — release runbook
- [docs/tui.md](./docs/tui.md) — planned k9s-like TUI mode

## Contributing

- [CONTRIBUTING.md](./CONTRIBUTING.md) — workflow, commit/PR rules, code conventions
- [SECURITY.md](./SECURITY.md) — vulnerability disclosure policy

## Development

```bash
make build       # Build the binary into ./bin/kupe
make test        # Unit tests
make lint        # golangci-lint
make gosec       # Security static analysis
make govulncheck # Go vulnerability check
make snapshot    # Build a release locally (no publish)
make manpages    # Generate completions and man(1) pages
```

Hit a live development environment:

```bash
export KUPE_API_TOKEN=kupe_…
export KUPE_API_URL=https://api.dev.int.kupe.cloud
export KUPE_TENANT=kupe-test
go run ./cmd/kupe cluster list
```

## License

Apache 2.0.
