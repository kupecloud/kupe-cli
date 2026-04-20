---
title: "TUI Mode (Phase 2)"
description: "Planned k9s-like full-screen TUI for kupe, framework choice, parity targets, and keybindings"
owner: platform-team
lastReviewed: "2026-04-20"
sidebar:
  order: 7
---

This doc captures the **Phase 2 design** for `kupe tui` — a full-screen terminal UI for managing Kupe resources interactively, loosely modeled on [k9s](https://k9scli.io/). **Nothing in this doc is implemented in v1.** The goal is to fix architectural choices early so the non-TUI CLI can share UX primitives (spinner, progress, palette) without painting us into a corner for the TUI.

If you're implementing the CLI, read this only to avoid introducing incompatible abstractions in `internal/ux/` and `internal/printer/`.

## What `kupe tui` is for

The non-TUI CLI is already great for "I want one thing done now" — `kupe cluster create`, `kupe cluster kubeconfig`, `kupe cluster list`. The TUI is for **live inspection and fleet-wide operations**:

- "Which of my clusters are Degraded right now?"
- "I want to delete five ephemeral clusters — let me pick them from a list."
- "Upgrade all staging clusters to 1.33, one at a time."
- "I want to see live resource usage across my tenant."

k9s covers this for raw Kubernetes; `kupe tui` covers it for Kupe resources (clusters, members, secrets, api keys).

## Framework choice: Bubbletea

We chose [charmbracelet/bubbletea](https://github.com/charmbracelet/bubbletea) over the [rivo/tview](https://github.com/rivo/tview) + [gdamore/tcell](https://github.com/gdamore/tcell) stack that k9s uses.

### Why Bubbletea

- **Unified rendering stack.** The non-TUI CLI already uses `bubbles/spinner` and `bubbles/progress` for long-op rendering. Staying on the same family means a single palette, a single animation style, and one set of mental models. Introducing tview would mean maintaining two rendering pipelines.
- **Industry momentum.** GitLab's glab has [an accepted proposal](https://gitlab.com/groups/gitlab-org/-/epics/19748) to migrate from tview to Bubbletea. New TUIs in the Go ecosystem (Charm tools, `gh copilot`, kagent) overwhelmingly pick Bubbletea. The community-maintained components are better maintained.
- **Component completeness.** For every k9s pattern, `bubbles` has the piece we need:
  - `bubbles/table` — the resource list view.
  - `bubbles/viewport` — scrollable detail/log panes.
  - `bubbles/list` — fuzzy-filtered lists for command mode.
  - `bubbles/help` — context-sensitive keybinding hints at the bottom.
  - `bubbles/key` — declarative keymap definitions.
  - `bubbles/textinput` — for `/` filter and `:` command mode.
- **Elm architecture testability.** Bubbletea's `update/view/cmd` model maps cleanly to table tests. No widget graph to stand up in tests.
- **Lip Gloss for styling.** Declarative, composable styles. Much simpler than tview's nested box styling.

### Why not tview

- We'd be the odd one out internally — every other rich UX in the CLI uses Charm libraries.
- tview's widget-graph model is heavier than Bubbletea's message-loop model.
- tcell is more cross-platform than `golang.org/x/term` in theory, but Bubbletea wraps `golang.org/x/term` + [Charm's termenv](https://github.com/muesli/termenv) which handle every target we care about (macOS Terminal.app, iTerm2, Windows Terminal, Alacritty, WezTerm, tmux, screen).

### Why not raw tcell

tcell is a rendering primitive, not a framework. We'd be rebuilding half of Bubbletea. No.

## Application architecture

Single Bubbletea program, one root `tea.Model`. Root holds a small FSM of "pages":

```
root
├── pageList        — cluster list (or whichever resource kind is active)
├── pageDetail      — single-resource detail view
├── pageYAML        — raw YAML of the resource
├── pageConfirm     — destructive-action confirmation modal
├── pageCommand     — `:` command mode
└── pageFilter      — `/` filter input
```

Each page is its own `tea.Model` with its own `Update`/`View`. The root delegates messages to the active page and handles global keys (quit, page switch, help).

```go
type Root struct {
    active  Page
    pages   map[PageID]Page
    client  client.Interface
    style   *Style
    help    help.Model
    keys    keyMap
    stream  <-chan Event  // async polling updates
}

func (r Root) Init() tea.Cmd {
    return tea.Batch(r.active.Init(), r.startPoller())
}

func (r Root) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    // Global keys, then delegate to active.Update(msg)
}
```

## Live refresh

Every listable resource has a background poll at ~5 seconds interval. Implemented as a `tea.Cmd` that does one poll, emits an `updatedMsg`, and re-schedules itself:

```go
func pollClusters(cli client.Interface) tea.Cmd {
    return tea.Tick(5*time.Second, func(t time.Time) tea.Msg {
        list, err := cli.ListClusters(context.Background())
        return clustersUpdatedMsg{items: list, err: err}
    })
}
```

A single poll runs at a time (no overlapping), and polls are cancelled when the user leaves the list page. On error, the list preserves last-known state and shows an error banner.

## k9s parity targets

| Feature | k9s | `kupe tui` v1 |
|---------|-----|---------------|
| Resource list with j/k nav | ✓ | ✓ |
| `:resource` command mode switches kind | `:pods`, `:deploy` | `:clusters`, `:members`, `:apikeys`, `:secrets` |
| `/` filter on current list | ✓ | ✓ |
| Enter → detail view | ✓ | ✓ |
| `y` → raw YAML | ✓ | ✓ |
| `d` → describe/events | ✓ | ✓ (status conditions + events stub) |
| `e` → edit ($EDITOR) | ✓ | ✓ for mutable fields (version, resources) |
| `Ctrl-d` → delete (with confirm) | ✓ | ✓ |
| `l` → logs | ✓ | ✗ — out of scope; logs live in the vcluster |
| `s` → shell into resource | ✓ | ✗ — kubeconfig + `kubectl exec` is the path |
| `?` help | ✓ | ✓ |
| `ESC` back | ✓ | ✓ |
| Tenant / context switcher | n/a | `::` command mode — `:context prod`, `:context staging` |
| Last-known-good state on poll error | ✓ | ✓ |

Explicit non-goals for v1:

- Log streaming (it's a vcluster concern, handled by `kubectl`).
- Exec/shell into pods (same).
- Multi-tenant fan-out (each TUI session is scoped to one context).

## Keybinding scheme

Defined declaratively via `bubbles/key.Binding`, surfaced via `bubbles/help`:

```go
type keyMap struct {
    Up, Down          key.Binding
    Enter, Back       key.Binding
    YAML, Describe    key.Binding
    Edit, Delete      key.Binding
    Filter, Command   key.Binding
    Help, Quit        key.Binding
}
```

Defaults:

| Binding | Keys |
|---------|------|
| Up / Down | `k` / `j`, `↑` / `↓` |
| Enter (detail) | `enter`, `l` |
| Back | `esc`, `h` |
| YAML | `y` |
| Describe | `d` |
| Edit | `e` |
| Delete | `ctrl-d` |
| Filter | `/` |
| Command | `:` |
| Help | `?` |
| Quit | `q`, `ctrl-c` |

Users can override via a TUI-specific section of the config file (Phase 2 extension to the schema in [auth.md](./auth.md)).

## Rendering tips / constraints

- **Fixed-width columns.** `bubbles/table` needs column widths up front; resize handled via a window-size subscriber. Avoid unicode box-drawing chars — many CI terminals render them weird (this is Phase 2 so less relevant, but the habit carries over from the CLI table printer).
- **Alternate screen.** Bubbletea enters the alternate screen (`\e[?1049h`) by default. On quit, restores the main screen — the user's scrollback is preserved. Good default.
- **Mouse.** Bubbletea supports mouse; for v1 TUI, enable mouse wheel only (list scrolling). No click targets — keyboard remains primary.
- **Colors.** Use the same palette as the non-TUI CLI (`internal/ux/style.go`). `Running` green, `Provisioning` yellow, `Degraded` red, `Pending` faint.

## Testing

Bubbletea programs test cleanly with [`teatest`](https://github.com/charmbracelet/x/tree/main/exp/teatest):

```go
tm := teatest.NewTestModel(t, Root{...})
tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
    return bytes.Contains(out, []byte("prod"))
})
```

No live `kupe-api` needed — the fake client from `internal/client/clienttest/` (see [testing.md](./testing.md)) is injected into the Root. Every keypress and its resulting screen render can be snapshot-tested.

## Launch ergonomics

`kupe tui` launches the TUI with the current config's context. Flags:

| Flag | Description |
|------|-------------|
| `--context NAME` | Use a different context for the session. |
| `--kind CLUSTER\|APIKEY\|SECRET\|MEMBER` | Start on a specific resource kind. Default: cluster. |
| `--no-mouse` | Disable mouse support. |

No argument-less launcher — always via `kupe tui`, no `kubetui`-style separate binary. Same CLI, same global flags, same auth stack.

## Out of scope for TUI v1

These are explicitly *not* being planned — they'd change the scope dramatically and should be separate RFCs:

- Plugin system (à la k9s plugins). Revisit post-TUI-v1 if there's demand.
- Live metrics dashboards (CPU/memory graphs). The Console is the right surface for that; the TUI stays lightweight.
- Multi-tenant tabs. Adds a lot of UX complexity for a workflow that's already achievable with tmux + two `kupe tui` invocations.
- Custom theming via config. Start with one palette; users who want more can set `NO_COLOR` or override via `KUPE_PALETTE=high-contrast` if we add one.
- Inline config editing (editing `~/.config/kupe/config.yaml` from the TUI). Low value; `kupe config` subcommands handle it.

## References

- [charmbracelet/bubbletea](https://github.com/charmbracelet/bubbletea) — framework.
- [charmbracelet/bubbles](https://github.com/charmbracelet/bubbles) — widgets (spinner, table, list, viewport, …).
- [charmbracelet/lipgloss](https://github.com/charmbracelet/lipgloss) — styling.
- [charmbracelet/x/exp/teatest](https://github.com/charmbracelet/x/tree/main/exp/teatest) — test harness.
- [k9s](https://k9scli.io/) — inspiration, not imitation target.
