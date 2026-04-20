---
title: "Design Principles"
description: "UX principles, command grammar, interactive-vs-CI duality, global flags, and exit-code contract for the kupe CLI"
owner: platform-team
lastReviewed: "2026-04-20"
sidebar:
  order: 2
---

## Design principles

The CLI's design is governed by four rules, in priority order. Every design decision in this document (and every future one) should be traceable back to one of these.

1. **Two audiences, one binary.** A developer at a TTY and a CI pipeline both run the same commands. The binary adapts — it does not ship two modes behind a flag.
2. **Fast by default.** `kupe cluster list` with a warm config should render in under 300ms. We never block startup on non-essential work (no update checks, no telemetry, no config migrations on every run).
3. **Don't surprise kubectl users.** Output formats, `-o` flag semantics, exit codes, and error shapes match kubectl wherever kubectl has a convention. Users are already context-switching between the two; we don't make that switch more expensive than it has to be.
4. **Plain is the contract; rich is the treat.** Machine-parseable output is the documented behavior. Pretty tables, colors, spinners, and prompts are strictly additive TTY affordances that must never change stdout bytes a script would parse.

## Command grammar: noun-verb

`kupe <noun> <verb> [positional] [--flags]`

```
kupe cluster create prod --type shared --version 1.32
kupe cluster kubeconfig prod --merge
kupe apikey create --name ci --role admin
kupe config use-context staging
```

The noun is always a singular concrete resource (`cluster`, `apikey`, `config`). The verb is a short action. The **positional argument is always the resource name** — never a `--name` flag. This matches gh, fly, hcloud, gcloud, and stripe — every modern platform CLI.

### Why not verb-noun (kubectl style)?

kubectl uses verb-noun (`kubectl get pods`) because it's a generic REST client for ~50 different resource kinds that all share the same set of verbs. The verb is the unit of meaning: "get anything, of type pods".

`kupe` is a platform CLI with one primary noun (`cluster`) and a handful of supporting ones. Users will type `kupe cluster <tab>` far more often than they type `kupe <verb>`. Noun-verb gives better tab-completion scoping ("I've picked my resource, now show me what I can do with it") and matches the mental model of every peer tool.

We keep kubectl compatibility where it matters — output formats and exit codes — but not ordering.

### Verbs by category

| Verb | Purpose | Notes |
|------|---------|-------|
| `list` | Plural list of resources | Returns a table by default; `-o name` for pipe-friendly names |
| `get` | Single resource detail | Accepts one positional arg |
| `create` | Create a new resource | Positional arg is the name; waits by default |
| `delete` | Remove a resource | Prompts on TTY, requires `--yes` or non-TTY stdin; waits by default |
| `update` | Modify an existing resource | ETag/If-Match handled transparently; `--force` skips |
| `kubeconfig` | Retrieve a kubeconfig | Cluster-only; special-case verb |
| `wait` | Block until a resource reaches a phase | `--for=running\|deleted`, `--timeout=30m` |

Rare verbs: `auth login`/`auth logout`/`auth whoami`/`auth get-token`, `config view`/`config use-context`/`config set-context`/`config get`/`config set`/`config delete-context`/`config current-context`. These follow the same grammar.

## Interactive vs CI: the duality contract

The CLI detects its environment on every invocation via two signals:

- **TTY** — `golang.org/x/term.IsTerminal(fd)` on stdout (for rendering), stderr (for prompts/progress), and stdin (for interactive reads).
- **Explicit signals** — `NO_COLOR`, `CI=true`, `--no-color`, `-q/--quiet`, `-o json`.

Based on these, the CLI makes these decisions:

| Feature | Conditions for "on" |
|---------|---------------------|
| ANSI color | stdout is TTY AND `NO_COLOR` unset AND `--no-color` unset AND `TERM != dumb` |
| Spinners / progress bars | stderr is TTY AND `CI` unset AND `-q` unset AND `KUPE_NO_PROGRESS` unset |
| Confirmation prompts | stdin AND stderr are both TTY AND `--yes`/`-y` unset |
| Table output default | Always on unless `-o` explicitly set to a non-table format |
| Password / token hidden input | stdin is TTY |

The critical rule: **nothing about the data written to stdout depends on the TTY state.** The only difference between a TTY run and a CI run with `-o table` is that colors and column padding behave differently — the same columns in the same order, still parseable with `awk`.

`-o json` produces byte-identical output in both environments.

### Contract summary

| Stream | Contents | Contract |
|--------|----------|----------|
| **stdout** | Data. Resource payloads, command results, kubeconfig YAML, `ExecCredential` JSON | Parseable. Stable schema per `-o` format. |
| **stderr** | Status, progress, prompts, logs, errors | Free-form. Assume no parser. |
| **exit code** | Success / failure class | Stable per the table below. |

A caller running `kupe cluster list -o json | jq` should never see progress text contaminating stdout. A caller running `kupe cluster list 2>/dev/null` should get a clean table.

## Global flags

| Flag | Env | Default | Description |
|------|-----|---------|-------------|
| `--api-url URL` | `KUPE_API_URL` | `https://api.kupe.cloud` | Override API base URL |
| `--token TOKEN` | `KUPE_API_TOKEN` | — | Bearer token; bypasses config |
| `--tenant NAME` | `KUPE_TENANT` | — | Override context's tenant |
| `--context NAME` | `KUPE_CONTEXT` | current | Use named context from config |
| `--config PATH` | `KUPE_CONFIG` | `~/.config/kupe/config.yaml` | Config file path |
| `-o, --output FMT` | — | `table` | Output format |
| `--no-color` | `NO_COLOR` | off | Disable ANSI colors |
| `-q, --quiet` | — | off | Suppress status/progress |
| `-v, --verbose` | — | off | Debug logging to stderr |
| `-h, --help` | — | — | Show help |
| `--version` | — | — | Show version |

`-o` accepts: `table` (default for TTY), `wide`, `json`, `yaml`, `name`, `go-template=...`, `go-template-file=...`, `jsonpath=...`, `jsonpath-file=...`. See [output.md](./output.md) for the full spec.

## Per-command flags

Local flags are scoped to the command. Convention:

- `--wait` / `--wait-timeout` on any command that triggers an async phase change.
- `--yes` / `-y` on any destructive command (skips confirmation).
- `--force` on `update` commands (skips ETag check — use with care).
- `--if-match ETAG` on `update` commands (explicit optimistic locking — advanced users).
- `--dry-run=client|server` on any write command. `server` performs a validation-only request (if the API supports it); `client` just renders what would be sent.

## Confirmation prompts

Destructive commands (`cluster delete`, `apikey delete`, `config delete-context`) prompt on TTY:

```
$ kupe cluster delete prod
? This will delete cluster "prod" (tenant acme-corp). Type the name to confirm: prod
✓ cluster prod deleted
```

When `stdin` or `stderr` is not a TTY, the prompt is **not shown**. Instead, the command requires `--yes`:

```
$ echo | kupe cluster delete prod
Error: refusing to delete without --yes in non-interactive mode
```

This prevents a silently failing CI job (prompt can't be answered → command hangs or auto-fails) and prevents a shell redirection accident from silently deleting infrastructure.

`--yes`/`-y` bypasses the prompt in both modes. There is no opposite flag — if you don't want to delete, don't run delete.

## Exit codes

Inspired by kubectl and gh. Stable across versions.

| Code | Meaning | Example |
|------|---------|---------|
| `0` | Success | Any command that completed as requested |
| `1` | General error | Network failure, internal error, unclassified 5xx |
| `2` | Misuse | Unknown flag, missing required arg, invalid combination (Cobra sets this automatically for flag errors) |
| `3` | Auth error | 401 Unauthorized, 403 Forbidden, missing token, logged-out context |
| `4` | Not found | 404 on the requested resource |
| `5` | Conflict | 409 (already exists, concurrent modification) or 412 (ETag mismatch) |
| `6` | Rate limited | 429; rare because the client retries once internally |
| `7` | Unavailable | 503 (e.g., cluster kubeconfig requested before provisioning completes) |
| `8` | Timeout | `--wait-timeout` elapsed before the resource reached the target phase |
| `130` | Interrupt | User pressed Ctrl+C (standard Unix convention, `128 + SIGINT`) |

Mapping happens in `internal/cli/exit.go` from the typed error helpers on the client (`IsUnauthorized`, `IsNotFound`, etc.) matching the error classification in [kupe-api/internal/errors/errors.go](../../kupe-api/internal/errors/errors.go).

## Error output

Errors go to stderr with a single-line summary and optional hint:

```
Error: cluster "prod" not found
  run "kupe cluster list" to see available clusters
  (request-id: 7a3b9e41-...)
```

Request ID is always appended when the response included `X-Request-Id`, so support tickets can reference it.

With `-v`, full error chain and HTTP details (minus the token) are printed at the end.

With `-o json`, errors are printed as a JSON object on stderr:

```json
{"error":"cluster \"prod\" not found","requestId":"7a3b9e41-...","exitCode":4}
```

The exit code is in the JSON and also matches the process exit code, so both `jq '.exitCode'` and `$?` work.

## Comparison with peer CLIs

| Behavior | `kupe` | `gh` | `fly` | `hcloud` | `kubectl` |
|----------|--------|------|-------|----------|-----------|
| Order | noun-verb | noun-verb | noun-verb | noun-verb | verb-noun |
| Default output | table | table | table | table | table |
| JSON flag | `-o json` | `--json` | `--json` | `-o json` | `-o json` |
| Wait on create | yes | n/a | yes | n/a | no |
| Token env | `KUPE_API_TOKEN` | `GH_TOKEN` | `FLY_API_TOKEN` | `HCLOUD_TOKEN` | n/a |
| Config contexts | yes | yes (hosts) | yes (apps) | yes (contexts) | yes (contexts) |
| Keyring | yes | yes | no | no | no |
| Completion | yes | yes | yes | yes | yes |

The biggest divergence is `-o json` (kubectl-style) instead of `--json` (gh/fly-style). We chose `-o json` because the full kubectl output format family (`go-template`, `jsonpath`, `yaml`, `name`, `wide`) only makes sense under a single `-o` umbrella, and importing `k8s.io/cli-runtime` gives us all of them for free.

## Versioning and stability

The CLI follows semver.

- **`v0.x`** — interface can change. Deprecated flags will be kept working for at least one minor version with a `stderr` deprecation notice.
- **`v1.0`** — achieved when (a) the full terraform-provider-kupe feature set has CLI parity, (b) the TUI has shipped, and (c) the platform commits to endpoint stability for a tagged API version.
- Breaking flag renames are always accompanied by a hidden alias for one minor version.
- `-o json` schemas are considered part of the public interface and require a major version bump to change field names.

## Out of scope (explicitly)

- **Editing CRDs directly.** That's `kubectl` once you're inside a vcluster.
- **Local cluster emulation** (à la `kind` / `minikube`). Out of scope; Kupe is a managed service, not a local dev tool.
- **Plugin system.** Cobra supports one; we don't need it in v1. Revisit post-v1 if there's demand.
- **Shell wrappers** (e.g., an alias auto-setter). Users install completion manually.
- **Auto-update.** Package managers handle updates; re-running `brew upgrade kupe` or `scoop update kupe` is the expected path.
- **Telemetry.** Not in v1. Revisit with an opt-in model if we need it.
