// Package cli owns the runtime plumbing shared by every command: the Factory
// that resolves config + client on demand, the IOStreams abstraction that
// gates color/spinners/prompts, the global flag struct bound on root, and the
// error-to-exit-code mapping. Subcommand packages depend on cli; cli depends
// on config and auth but never on a specific command.
package cli

import (
	"bytes"
	"io"
	"os"

	"golang.org/x/term"
)

// IOStreams is the CLI's stdin/stdout/stderr abstraction. Every command writes
// through these streams — never os.Stdout / os.Stderr directly — so tests can
// capture output and production behaviour stays consistent.
//
// ColorEnabled, SpinnersEnabled, and PromptsEnabled are computed once at
// construction time from a combination of TTY detection and environment
// signals (NO_COLOR, CI, KUPE_NO_PROGRESS). Flag overrides (--no-color,
// --quiet, --yes) are applied after construction via the Set* methods.
type IOStreams struct {
	In     io.Reader
	Out    io.Writer
	ErrOut io.Writer

	stdinIsTTY  bool
	stdoutIsTTY bool
	stderrIsTTY bool

	ColorEnabled    bool
	SpinnersEnabled bool
	PromptsEnabled  bool
}

// System returns an IOStreams bound to the process's real file descriptors,
// with TTY/env-driven flags detected.
func System() *IOStreams {
	stdinIsTTY := isTerminal(os.Stdin)
	stdoutIsTTY := isTerminal(os.Stdout)
	stderrIsTTY := isTerminal(os.Stderr)

	return &IOStreams{
		In:              os.Stdin,
		Out:             os.Stdout,
		ErrOut:          os.Stderr,
		stdinIsTTY:      stdinIsTTY,
		stdoutIsTTY:     stdoutIsTTY,
		stderrIsTTY:     stderrIsTTY,
		ColorEnabled:    defaultColorEnabled(stdoutIsTTY),
		SpinnersEnabled: defaultSpinnersEnabled(stderrIsTTY),
		PromptsEnabled:  stdinIsTTY && stderrIsTTY,
	}
}

// Test returns an IOStreams backed by bytes.Buffers with all TTY-gated
// features disabled. Use this in unit tests for deterministic output.
func Test() (*IOStreams, *bytes.Buffer, *bytes.Buffer) {
	in := &bytes.Buffer{}
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	return &IOStreams{
		In:              in,
		Out:             out,
		ErrOut:          errOut,
		ColorEnabled:    false,
		SpinnersEnabled: false,
		PromptsEnabled:  false,
	}, out, errOut
}

// IsStdinTTY reports whether stdin is connected to a terminal (before any
// flag overrides).
func (s *IOStreams) IsStdinTTY() bool { return s.stdinIsTTY }

// IsStdoutTTY reports whether stdout is connected to a terminal.
func (s *IOStreams) IsStdoutTTY() bool { return s.stdoutIsTTY }

// IsStderrTTY reports whether stderr is connected to a terminal.
func (s *IOStreams) IsStderrTTY() bool { return s.stderrIsTTY }

// SetColorEnabled applies a flag override (e.g., --no-color) to the cached
// color decision.
func (s *IOStreams) SetColorEnabled(enabled bool) { s.ColorEnabled = enabled }

// SetSpinnersEnabled applies a flag override (e.g., --quiet) to the spinners
// decision.
func (s *IOStreams) SetSpinnersEnabled(enabled bool) { s.SpinnersEnabled = enabled }

// SetPromptsEnabled applies a flag override (e.g., --yes) to the prompts
// decision. Passing false forces non-interactive; passing true cannot enable
// prompts on a non-TTY.
func (s *IOStreams) SetPromptsEnabled(enabled bool) {
	s.PromptsEnabled = enabled && s.stdinIsTTY && s.stderrIsTTY
}

func defaultColorEnabled(stdoutIsTTY bool) bool {
	if !stdoutIsTTY {
		return false
	}
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if os.Getenv("TERM") == "dumb" {
		return false
	}
	return true
}

func defaultSpinnersEnabled(stderrIsTTY bool) bool {
	if !stderrIsTTY {
		return false
	}
	if os.Getenv("CI") != "" {
		return false
	}
	if os.Getenv("KUPE_NO_PROGRESS") != "" {
		return false
	}
	return true
}

// isTerminal reports whether the given *os.File (or anything providing Fd())
// is a terminal. Returns false for buffers, pipes, redirects, and non-files.
func isTerminal(f any) bool {
	type fder interface{ Fd() uintptr }
	if fd, ok := f.(fder); ok {
		return term.IsTerminal(int(fd.Fd())) //#nosec G115 -- file descriptors are bounded small ints
	}
	return false
}
