# Contributing to kupe-cli

Thanks for taking the time to contribute. This file is the short version —
[CLAUDE.md](./CLAUDE.md) is the source of truth for code conventions, the
[docs/](./docs/) directory for design and architecture.

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
make snapshot        # local goreleaser run (no publish)
make manpages        # generate man(1) into man/man1/
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

## Code conventions (the short list)

- **Don't write to `os.Stdout`/`os.Stderr` directly.** Always go through
  `factory.IOStreams` so commands stay unit-testable.
- **Don't hand-roll YAML surgery on kubeconfigs.** Use `clientcmd.Merge`.
- **Don't skip error wrapping.** `fmt.Errorf("…: %w", err)` always.
- **Don't leak tokens** to logs, error messages, or `-v` output. The HTTP
  middleware strips `Authorization` headers before logging — verify any new
  tracing you add does the same.
- **Don't import Viper.** We use a hand-written YAML loader and the
  precedence helper in `internal/config`.

The full list lives in [CLAUDE.md](./CLAUDE.md#donts).

## Releasing

See [RELEASING.md](./RELEASING.md). TL;DR: `main` is always shippable;
semantic-release tags releases on green CI, goreleaser attaches artifacts.

## Code of Conduct

By participating you agree to follow the principles in
https://www.contributor-covenant.org/version/2/1/code_of_conduct/. Be kind,
assume good faith, focus on the work.
