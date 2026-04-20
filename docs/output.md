---
title: "Output & Rendering"
description: "Output formats, TTY detection, color and spinner gating, and rich long-op rendering in the kupe CLI"
owner: platform-team
lastReviewed: "2026-04-20"
sidebar:
  order: 6
---

Output is the CLI's public interface. This doc specifies the formats supported, the rules that govern rich terminal affordances (colors, spinners, progress), and the stdout/stderr contract that makes the CLI safe to script.

## Output formats

The `-o` / `--output` flag accepts any of these values:

| Format | Purpose | TTY default | Notes |
|--------|---------|-------------|-------|
| `table` | Human-readable columns | Yes | Default for `list` and `get`. |
| `wide` | `table` + extra columns | No | Useful info that's noisy in the normal table. |
| `json` | `encoding/json` indent=2 | No | Stable field order per resource. |
| `yaml` | `yaml.v3` marshal | No | Same schema as `json`. |
| `name` | Bare resource names, one per line | No | Pipe-friendly: `kupe cluster list -o name \| xargs …`. |
| `go-template=...` | Go `text/template` | No | e.g., `-o go-template='{{.Status.Phase}}'`. |
| `jsonpath=...` | Planned — currently returns a helpful error directing users to `go-template=...` or `-o json \| jq`. | No | Deferred; see `internal/printer/plain.go`. |

The file-based variants kubectl supports (`go-template-file=PATH`,
`jsonpath-file=PATH`) are not yet wired to the `-o` flag. Pass the template
inline for now.

### Format stability guarantee

`json`, `yaml`, and `name` output schemas are **part of the CLI's public
interface**. Breaking field renames require a major version bump. New fields
can be added in minor versions (additive only).

`table`, `wide`, `go-template`, and error text are **not** guaranteed across
minor versions. Scripts should rely on `json`.

## Printer implementation

`internal/printer/` is hand-written — the earlier plan called for importing
`k8s.io/cli-runtime/pkg/genericclioptions.PrintFlags`, but that library
expects `runtime.Object` types (DeepCopy, GVK tracking). Our resource types
are plain Go structs, so we shipped a thin in-house dispatcher instead:

- `printer.Parse` / `printer.MustParse` — turns a `-o` string into a
  `*Format` (MustParse wraps errors as `cli.MisuseError`).
- `printer.RenderList[T]` / `printer.RenderOne[T]` — generic helpers every
  command uses. One place to add a new `-o` kind.
- Per-resource `*Columns()` functions live in `printer/{cluster,apikey,
  secret,member}.go`.

### Per-resource table columns

Each resource has a `Columns() []Column` function. A `Column` has a `Name`, an `Extractor func(any) string`, and a `WideOnly bool`.

**Cluster** ([commands.md](./commands.md) has the example output):

| Column | Source | Wide only? |
|--------|--------|------------|
| NAME | `.name` | |
| TYPE | `.type` | |
| VERSION | `.version` | |
| PHASE | `.status.phase` | |
| CPU | `.resources.cpu` | |
| MEM | `.resources.memory` | |
| AGE | `.createdAt` → relative | |
| ENDPOINT | `.status.endpoint` | ✓ |
| K8S-VERSION | `.status.kubernetesVersion` | ✓ |
| STORAGE | `.resources.storage` | ✓ |

**APIKey**:

| Column | Source | Wide only? |
|--------|--------|------------|
| ID | `.id` (truncated to 8 chars + `…`) | |
| NAME | `.displayName` | |
| ROLE | `.role` | |
| CREATED BY | `.createdBy` | |
| LAST USED | `.lastUsedAt` → relative or `never` | |
| AGE | `.createdAt` → relative | |
| EXPIRES | `.expiresAt` → relative or `never` | ✓ |
| ID-FULL | `.id` (full UUID) | ✓ |

**Secret**:

| Column | Source | Wide only? |
|--------|--------|------------|
| NAME | `.name` | |
| PHASE | `.status.phase` | |
| SYNCS | `len(.sync)` | |
| AGE | `.createdAt` → relative | |
| PATH | `.secretPath` | ✓ |
| CLUSTERS | deduped cluster names from `.sync[].cluster`, comma-joined | ✓ |

**Member**:

| Column | Source | Wide only? |
|--------|--------|------------|
| EMAIL | `.email` | |
| ROLE | `.role` | |

Column rendering uses `text/tabwriter` with 2-space padding. No unicode box characters in plain mode (breaks `awk`).

### Color in tables

Colors are applied per-cell by the `Extractor` only when `iostreams.ColorEnabled`. Convention:

| Value | Color | Library |
|-------|-------|---------|
| `Running` | green | `lipgloss.Color("10")` (ANSI bright green) |
| `Provisioning`, `Upgrading` | yellow | ANSI yellow |
| `Degraded`, `Terminating` | red | ANSI bright red |
| `Pending` | dim | ANSI faint |

The palette is defined in `internal/ux/style.go`. `NO_COLOR` or non-TTY strips colors entirely.

## TTY detection

Implemented once in `internal/cli/iostreams.go`:

```go
type IOStreams struct {
    In, Out, ErrOut  io.Writer   // buffers in tests; os.Stdin/Stdout/Stderr in prod
    stdinIsTTY       bool
    stdoutIsTTY      bool
    stderrIsTTY      bool
    ColorEnabled     bool
    SpinnersEnabled  bool
    PromptsEnabled   bool
}

func System() *IOStreams {
    // Detects once at startup using golang.org/x/term.IsTerminal(fd)
    // Computes ColorEnabled / SpinnersEnabled / PromptsEnabled per the rules below.
}
```

Rules (already referenced in [design.md](./design.md), reproduced here for completeness):

- **ColorEnabled** = `stdoutIsTTY` AND `NO_COLOR` unset AND `--no-color` unset AND `$TERM != "dumb"`.
- **SpinnersEnabled** = `stderrIsTTY` AND `CI` unset AND `-q` unset AND `KUPE_NO_PROGRESS` unset.
- **PromptsEnabled** = `stdinIsTTY` AND `stderrIsTTY` AND `--yes` unset.

Detection happens once. The `IOStreams` is threaded through the factory into every command; no function ever re-detects or reads these env vars directly.

## stdout vs stderr contract

**Every byte written to stdout is "data"**. Every byte on stderr is free-form. This contract is enforced by threading `f.IOStreams.Out` and `f.IOStreams.ErrOut` through commands and never using `os.Stdout` / `os.Stderr` directly.

| Goes to stdout | Goes to stderr |
|----------------|----------------|
| Resource renders (`table`, `json`, `yaml`, …) | Info/status messages (`✓ Logged in as …`) |
| kubeconfig YAML (`kupe cluster kubeconfig`) | Prompts (`? Tenant:`) |
| `ExecCredential` JSON (`kupe auth get-token`) | Spinners and progress |
| API key secret (one-time) | Warnings, errors, deprecations |
| `kupe completion <shell>` | `-v` debug traces |

A user doing `kupe cluster list > /tmp/clusters.txt 2>/dev/null` should get exactly the table (or JSON, etc.). Progress, spinners, and "✓ …" checkmarks go to `/dev/null`.

## Long-running operations

`cluster create`, `cluster delete`, `cluster update`, and explicit `cluster wait` use a polling loop (see [architecture.md](./architecture.md)). Rendering of progress is split two ways by `iostreams.SpinnersEnabled`:

### TTY path (`SpinnersEnabled = true`)

A standalone Bubbletea program runs during the wait. It owns one line of the terminal (stderr). The model is minimal:

```go
type model struct {
    spinner     spinner.Model
    startedAt   time.Time
    phase       string
    last        time.Time
}

func (m model) View() string {
    elapsed := time.Since(m.startedAt).Round(time.Second)
    return fmt.Sprintf("%s %s [%s]",
        m.spinner.View(),
        lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Render(m.phase),
        elapsed,
    )
}
```

A `tea.Cmd` ticks every 2s, calls `GetCluster`, and returns an `updatedMsg{phase, err}`. On a terminal phase (`Running` / gone / `Degraded`), the program quits. The wait returns and the normal command output resumes.

`bubbles/spinner.Dot` is the default frame style. There is currently no env
var to pick a different style — the spinner is a single constant. If that
turns out to matter (e.g., a terminal that renders dots poorly), we'll add
a `KUPE_SPINNER` knob at that point.

### CI path (`SpinnersEnabled = false`)

One newline-terminated status line per phase transition, on stderr, no ANSI:

```
[00:00:04] pending
[00:00:34] provisioning
[00:02:04] running
```

The timestamp is elapsed time from the command start (not wall-clock), so CI log scrollback stays useful even across replays. Lines are flushed immediately — no buffering.

If the phase hasn't changed between polls, no line is emitted. This keeps CI logs reasonable for a 15-minute cluster upgrade (~10 lines, not 450).

### Ctrl+C behavior

The command's `context.Context` is canceled by the Cobra signal handler. The waiter exits cleanly:

```
^C
cancelled while cluster prod was "provisioning"
use "kupe cluster get prod" to check status, "kupe cluster wait prod" to resume
```

Exit code is `130` (standard Unix `128 + SIGINT`). The cluster keeps provisioning — it's the operator's job, not the CLI's. This matches `kubectl apply -f` and `gh pr merge --auto` behavior.

## Error rendering

Errors go through `internal/cli/exit.go`. For table output (default):

```
Error: cluster "prod" not found
  run "kupe cluster list" to see available clusters
  (request-id: 7a3b9e41-abcd-4567-8901-abcdef123456)
```

Body:
- Line 1: `Error: <message>`
- Indented hint lines: classification-specific guidance.
- Indented `(request-id: ...)` when the error carries one.

For `-o json`, errors write to stderr as a JSON object:

```json
{
  "error": "cluster \"prod\" not found",
  "exitCode": 4,
  "requestId": "7a3b9e41-abcd-4567-8901-abcdef123456"
}
```

The exit code is both in the JSON and the process exit code. Scripts can use either.

### Hint table

Errors carry classification-specific hints appended below the main message:

| Class | Hint |
|-------|------|
| `Unauthorized` | `run "kupe auth login" to re-authenticate` |
| `Forbidden` | `your API key has role "readonly"; ask an admin for write access` |
| `NotFound` | `run "kupe <resource> list" to see available resources` |
| `Validation` | `run "kupe <cmd> --help" for flag reference` |
| `Conflict` | `another caller modified the resource; retry` (for 412) |
| | `a resource with this name already exists` (for 409) |
| `RateLimited` | `you've been rate limited; retry in <Retry-After>` |
| `Unavailable` | `the resource is not yet ready; use "kupe cluster wait <name>"` |
| Timeout | `the operation did not complete before --wait-timeout; use "kupe cluster get <name>" to check status` |

## Quiet mode (`-q` / `--quiet`)

- Turns off spinners, progress, status messages, and any stderr output that isn't an error.
- Does **not** change stdout. `-o json` still produces JSON; `table` still produces a table.
- Confirmation prompts are not allowed in `-q` mode — the command requires `--yes` or fails.

Useful for CI where you only want the final result or the error.

## Verbose mode (`-v` / `--verbose`)

- Enables debug logging to stderr.
- Logs: config file path, resolved context, HTTP method/path/status/duration, retry events, cache hits.
- Never logs tokens, `Authorization` headers, or full request/response bodies.
- Overrides `-q` for stderr output (errors and debug lines appear; info lines don't).

`-v -v` reserved for future expansion (trace-level). For now `-v` only has one level.

## Pager behavior

Large outputs (`cluster list` across many clusters, `get -o yaml` of big objects) do **not** auto-page. Users pipe to `less` themselves:

```bash
kupe cluster list | less
```

Reasons:

- Detecting a TTY + user preference is fragile (`$PAGER` unset, `$LESS` weirdness, interactions with `CI=true`).
- Scripts that happen to run on a TTY (interactive shell) should not silently invoke a pager.

A future `--pager` flag could opt in; not scoped for v1.

## Internationalization

All user-facing strings are English in v1. No `i18n` package, no message catalogs. If we localize later, the stdout data format stays English (scripts depend on it); only stderr status/prompts are translated.
