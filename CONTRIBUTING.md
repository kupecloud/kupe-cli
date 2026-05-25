# Contributing to kupe-cli

Thanks for taking the time to contribute. This file covers conventions and
the day-to-day workflow. Detailed reference material lives in [docs/](./docs/):

- **[docs/architecture.md](./docs/architecture.md)** — runtime model, package
  layout, request lifecycle, async-operation behavior.
- **[docs/design.md](./docs/design.md)** — UX principles, exit codes, output
  contract (read this before adding a new command).
- **[docs/auth.md](./docs/auth.md)** — token lifecycle, config precedence,
  storage backends.
- **[docs/api-client.md](./docs/api-client.md)** — HTTP client conventions.
- **[docs/output.md](./docs/output.md)** — TTY vs CI rendering rules.
- **[docs/testing.md](./docs/testing.md)** — unit, printer, and live-test
  patterns.
- **[docs/releasing.md](./docs/releasing.md)** — release pipeline runbook.

## Before you start

For non-trivial changes (new commands, new flags, new external dependencies),
**open a GitHub issue first** so we can agree on the approach before you
spend time on a PR. Small fixes (typos, doc clarifications, contained bug
fixes) are fine to send directly.

For security-impacting issues, please follow [SECURITY.md](./SECURITY.md)
instead of opening a public issue.

## Local development

```bash
make build           # build bin/kupe with dev ldflags
make test            # unit tests
make lint            # golangci-lint
make gosec           # security static analysis
make govulncheck     # Go vulnerability check
make snapshot        # local goreleaser run (no publish)
make manpages        # generate completions and man(1) pages
```

Hit a live development environment by exporting:

```bash
export KUPE_API_TOKEN=kupe_…
export KUPE_API_URL=https://api.dev.int.kupe.cloud
export KUPE_TENANT=kupe-test
```

For OIDC end-to-end testing, see [docs/testing.md](./docs/testing.md).

## Pull-request guidelines

- **Conventional commits.** PR titles should match `feat|fix|refactor|docs|test|chore: …`.
  `feat:` triggers a minor release; `fix:` triggers a patch.
- **Single-line subjects.** Keep the title under ~70 chars and put detail in
  the body. The body should explain *why*, not *what* (the diff shows what).
- **Tests.** New code paths need test coverage. We prefer table-driven tests
  using `cli.Test()` IOStreams for command-level testing. Live tests
  (`test/live/`) are gated behind the `live` build tag and run via
  `make test-live` against a real kupe-api.
- **No vendored deps without a reason.** Run `go mod tidy && go mod vendor`
  if you've changed imports; the diff in `vendor/modules.txt` should be
  clean.
- **Pre-commit hooks** run `golangci-lint`, `gofmt`, `gosec`, `govulncheck`,
  and `markdownlint`. Set up `pre-commit` to catch issues locally before CI
  does.

## Command + UX conventions

- **Noun-verb command order.** `kupe cluster create`, not `kupe create
  cluster`. Matches `gh`, `fly`, `hcloud`.
- **Positional arg for the resource name.** `kupe cluster get NAME`, never
  `--name`.
- **Exit codes.** `0` success, `1` general, `2` misuse, `3` auth, `4` not-
  found, `5` conflict, `6` rate-limited, `7` unavailable. Defined in
  [internal/cli/exit.go](./internal/cli/exit.go) and surveyed in
  [docs/design.md](./docs/design.md).
- **Structured stderr errors.** `Error: message\n  (request-id: abc123)\n`
  — request ID surfaced for support tracing.
- **Data → stdout, status/progress/prompts → stderr.** `-o json` always
  produces parseable stdout.
- **No telemetry.** Period.

## Code conventions / things not to do

- **Don't write to `os.Stdout`/`os.Stderr` directly.** Always go through
  `factory.IOStreams` so commands stay unit-testable.
- **Don't print spinners or progress without checking
  `iostreams.SpinnersEnabled`.** They must auto-disable on non-TTY, in CI,
  with `-q`/`-o json`/`NO_COLOR`.
- **Don't hand-roll YAML surgery on kubeconfigs.** Use `clientcmd.Merge`
  via `internal/kubeconfig.Merge`.
- **Don't skip error wrapping.** `fmt.Errorf("…: %w", err)` always.
- **Don't leak tokens** to logs, error messages, or `-v`/`--verbose`
  output. The HTTP middleware strips `Authorization` headers before
  logging — any new tracing must do the same.
- **Don't import Viper.** We use a hand-written YAML loader and the
  precedence helper in `internal/config`.
- **Don't add a command without updating [docs/commands.md](./docs/commands.md).**

## Releasing

See [docs/releasing.md](./docs/releasing.md). TL;DR: `main` is always
shippable; semantic-release tags releases on green CI, goreleaser attaches
artifacts.

## Code of Conduct

By participating you agree to follow the principles in
https://www.contributor-covenant.org/version/2/1/code_of_conduct/. Be kind,
assume good faith, focus on the work.
