# kupe

The official command-line interface for [Kupe](https://kupe.cloud) — managed Kubernetes clusters in seconds.

```bash
kupe cluster create prod --type shared --version 1.32
kupe cluster kubeconfig prod --merge
kubectl get pods
```

## Install

### macOS / Linux (Homebrew)

```bash
brew tap kupecloud/tap
brew install kupe
```

(Or, as a one-liner: `brew install kupecloud/tap/kupe`.)

### Windows (Scoop)

```bash
scoop bucket add kupe https://github.com/kupecloud/scoop-bucket
scoop install kupe
```

### Install script

```bash
curl -fsSL https://get.kupe.cloud | sh
```

### From source

```bash
go install github.com/kupecloud/kupe-cli/cmd/kupe@latest
```

### Binary release

Pre-built binaries for darwin / linux / windows × amd64 / arm64 are attached to each [GitHub release](https://github.com/kupecloud/kupe-cli/releases).

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
kupe cluster create dev --type shared

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
kupe cluster create "ci-$GITHUB_SHA" --type shared --wait -o json
```

The CLI auto-detects non-TTY environments and switches off colors, spinners, and prompts. See [docs/design.md](./docs/design.md) for the full interactive-vs-CI contract.

## Command overview

| Command | Purpose |
| ------- | ------- |
| `kupe auth login` | Authenticate with an API token |
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
make snapshot    # Build a release locally (no publish)
make manpages    # Generate man(1) pages into man/man1/
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
