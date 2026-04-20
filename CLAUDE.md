# CLAUDE.md - Kupe CLI

## Project Overview

`kupe` is the official command-line interface for the Kupe multi-tenant Kubernetes SaaS platform. It is the fourth access path alongside the web console, the Terraform provider, and direct REST API calls — targeted at the first-time user spinning up a cluster and the daily developer who manages clusters from their terminal.

The CLI wraps [kupe-api](../kupe-api/) and adds rich interactive UX for humans (spinners, colored status, confirmation prompts, a future k9s-like TUI) while staying fully scriptable for CI pipelines (stable stdout, structured `-o json` output, predictable exit codes, no ANSI).

Read [docs/architecture.md](./docs/architecture.md) first — it is the source of truth for runtime structure. Read [docs/design.md](./docs/design.md) for UX principles before adding new commands.

## Tech Stack

- **Language**: Go 1.26.2 (match all other kupe Go repos)
- **CLI framework**: [`github.com/spf13/cobra`](https://github.com/spf13/cobra)
- **Printers / IOStreams**: `k8s.io/cli-runtime/pkg/genericclioptions` + `k8s.io/cli-runtime/pkg/printers` (the kubectl printer stack — table/wide/json/yaml/name/go-template/jsonpath)
- **TTY detection**: `golang.org/x/term`
- **Config parsing**: `gopkg.in/yaml.v3` (no Viper — hand-written loader)
- **Keyring**: `github.com/zalando/go-keyring`
- **Rich UX**: `github.com/charmbracelet/bubbletea` + `bubbles` + `lipgloss` for TTY spinners/progress; plain stderr fallback for CI
- **Kubeconfig merge**: `k8s.io/client-go/tools/clientcmd`
- **Module**: `github.com/kupecloud/kupe-cli`
- **Binary**: `kupe`
- **Dependencies**: vendored (`go mod vendor`, build with `-mod=vendor`) — matches kupe-api convention
- **Release tooling**: `goreleaser` with Cosign keyless signing + syft SBOMs

## Project Structure

```
kupe-cli/
├── cmd/kupe/main.go              # Thin entrypoint — calls internal/cmd.Execute()
├── internal/
│   ├── cmd/                      # Cobra command tree (one package per noun)
│   │   ├── root.go               # Root command, global flags, factory wiring
│   │   ├── version.go
│   │   ├── completion.go
│   │   ├── auth/                 # login | logout | whoami | get-token
│   │   ├── config/               # view | get | set | use-context | set-context | delete-context | current-context
│   │   ├── cluster/              # list | get | create | delete | update | kubeconfig | wait
│   │   ├── apikey/               # list | create | delete
│   │   ├── secret/               # list | get | create | update | delete
│   │   └── member/               # list | add | update | remove
│   ├── cli/
│   │   ├── factory.go            # Constructs Config, Client, IOStreams on demand
│   │   ├── iostreams.go          # stdin/stdout/stderr + TTY + color detection
│   │   └── exit.go               # Error → exit-code mapping
│   ├── client/                   # HTTP client (lifted from terraform-provider-kupe)
│   ├── config/                   # YAML config loader, multi-context, precedence
│   ├── auth/                     # Keyring + plaintext token storage
│   ├── printer/                  # PrintFlags wrapper, per-resource table specs
│   ├── kubeconfig/               # Build + merge kubeconfig YAML
│   ├── ux/                       # Bubbletea spinner/progress (TTY) + plain fallback (CI)
│   └── build/info.go             # Version, Commit, Date (set by ldflags)
├── test/
│   ├── golden/                   # Golden files for printer tests
│   └── e2e/                      # go-vcr cassettes + optional live kupe-api tests
├── docs/                         # Internal docs (Starlight-sourced)
├── chart/                        # Empty — CLI is not deployed as a chart
├── Makefile
├── .goreleaser.yaml
└── vendor/
```

## Commands (v1)

```
kupe version
kupe completion [bash|zsh|fish|powershell]
kupe auth login|logout|whoami|get-token
kupe config view|get|set|use-context|set-context|delete-context|current-context
kupe cluster list|get|create|delete|update|kubeconfig|wait
kupe apikey list|create|delete
kupe secret list|get|create|update|delete
kupe member list|add|update|remove
```

`kupe tui` is Phase 8 (see [docs/tui.md](./docs/tui.md)).

## Global Flags

| Flag | Env | Description |
|------|-----|-------------|
| `--api-url` | `KUPE_API_URL` | Override API base URL (default `https://api.kupe.cloud`) |
| `--token` | `KUPE_API_TOKEN` | Bearer token — bypasses config entirely (CI mode) |
| `--tenant` | `KUPE_TENANT` | Override current context's tenant |
| `--context` | `KUPE_CONTEXT` | Named context from config |
| `--config` | `KUPE_CONFIG` | Config file path (default `~/.config/kupe/config.yaml`) |
| `-o, --output` | — | `table` (default), `wide`, `json`, `yaml`, `name`, `go-template=…`, `jsonpath=…` |
| `--no-color` | `NO_COLOR` | Disable ANSI color (also auto-off on non-TTY) |
| `-q, --quiet` | — | Suppress spinners/progress/info; data still printed |
| `-v, --verbose` | — | Debug logging to stderr |

## Config Precedence

```
token:   --token > KUPE_API_TOKEN > keyring[ctx] > plaintext[ctx] > (error)
apiUrl:  --api-url > KUPE_API_URL > contexts[ctx].apiUrl > default
tenant:  --tenant > KUPE_TENANT > contexts[ctx].tenant > (error)
```

See [docs/auth.md](./docs/auth.md) for full details.

## Authentication

- **v1 login**: API key only. User pastes a token from the console; CLI validates via `GET /api/v1/tenants/{tenant}` and stores in the OS keyring.
- **Env var `KUPE_API_TOKEN`** is the CI short-circuit — when set, the config file is bypassed entirely.
- **OIDC device flow** is Phase 1.5, pending Authentik device-code endpoint.
- **Exec-plugin kubeconfig**: `kupe cluster kubeconfig NAME --exec` emits a kubeconfig that shells back to `kupe auth get-token`, returning an `ExecCredential` per `client.authentication.k8s.io/v1`.

## API Client

- Lifted from [terraform-provider-kupe/internal/client/](../terraform-provider-kupe/internal/client/) — a working, tested Go client already exists. Copy, rebrand `User-Agent`, and extend with retry/backoff, 429 handling, and typed error helpers.
- Error types mirror [kupe-api/internal/errors/errors.go](../kupe-api/internal/errors/errors.go) classification: `IsUnauthorized`, `IsForbidden`, `IsNotFound`, `IsValidation`, `IsConflict`, `IsPreconditionFailed`, `IsRateLimited`, `IsUnavailable`.
- Phase 2: extract into a shared `github.com/kupecloud/kupe-go-client` module used by both CLI and tf provider.

## Interactive-vs-CI Duality

The CLI auto-detects TTY on stdout using `golang.org/x/term.IsTerminal`. When stdout is not a TTY, or when `NO_COLOR`, `CI=true`, `--quiet`, or `-o json` is set:

- Colors off.
- Spinners/progress off (fall back to one-line-per-phase plain stderr ticks).
- Confirmation prompts off (destructive ops require `--yes` instead).
- Table output stays the default (matches kubectl), but is plain-rendered.

Contract: **data → stdout, status/progress/prompts → stderr**. `-o json` always produces parseable stdout.

## Long-Running Operations

`kupe cluster create|delete|update` wait by default (`--wait=true --wait-timeout=30m`), polling `status.phase` every 2s (exponential to 10s cap). `--wait=false` returns immediately with just the resource name.

## Common Commands

```bash
make build          # Build local binary
make test           # Run unit tests
make test-update    # Update golden files
make lint           # golangci-lint
make sec            # gosec + govulncheck
make vendor         # go mod vendor

# Build a release locally (no publish)
make snapshot       # goreleaser release --snapshot --clean --skip=publish

# Run CLI against a local dev kupe-api
KUPE_API_URL=http://localhost:8080 \
KUPE_API_TOKEN=kupe_dev_... \
KUPE_TENANT=dev \
  go run ./cmd/kupe cluster list
```

## Conventions

- **Noun-verb command ordering** — `kupe cluster create`, not `kupe create cluster`. Matches gh/fly/hcloud.
- **Positional arg for the resource name** — `kupe cluster get NAME`, never `--name`.
- **Exit codes** — `0` success, `1` general, `2` misuse, `3` auth, `4` not-found, `5` conflict (see [docs/design.md](./docs/design.md)).
- **Structured errors on stderr** — `Error: message\n  (request-id: abc123)\n` — request ID surfaced for support tracing.
- **No telemetry in v1**.
- **No self-update in v1** — use Homebrew / Scoop / install script.

## Don'ts

- Don't import Viper.
- Don't hand-roll YAML surgery on kubeconfigs — use `clientcmd.Merge`.
- Don't write directly to `os.Stdout` / `os.Stderr` — always go through the factory's `IOStreams` (makes commands unit-testable).
- Don't print progress/spinners without checking `iostreams.SpinnersEnabled`.
- Don't leak tokens to logs, error messages, or `-v` output.
- Don't add a command without updating [docs/commands.md](./docs/commands.md).
- Don't skip error wrapping (`fmt.Errorf("...: %w", err)`).

## Related Repos

| Repo | Relationship |
|------|--------------|
| [kupe-api](../kupe-api/) | Backend REST API. Source of truth for endpoints via `api/swagger.json`. |
| [terraform-provider-kupe](../terraform-provider-kupe/) | Sibling client. `internal/client/` is the origin of the CLI's HTTP client. |
| [kupe-control-operator](../kupe-control-operator/) | Owns CRD types in `api/v1alpha1/`. Source of truth for field shapes. |
| [console](../console/) | Web UI (Headlamp plugin). Parallel path, same API backend. |
| [docs-public](../docs-public/) | User-facing docs. CLI quickstart + getting-started guides live there. |
| [docs-internal](../docs-internal/) | Starlight site that consumes this repo's `docs/` directory. |
