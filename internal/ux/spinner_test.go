package ux

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kupecloud/kupe-cli/internal/cli"
)

// TestClassifySpinnerRunErr pins the classification order for errors coming
// out of tea.Program.Run. Regression for the --wait-timeout bug: bubbletea
// returns ErrProgramKilled *wrapping* the external context's error
// (vendor/github.com/charmbracelet/bubbletea/tea.go), so the deadline must
// be classified before the kill sentinel or a timeout is misreported as a
// user interrupt.
func TestClassifySpinnerRunErr(t *testing.T) {
	otherErr := errors.New("renderer exploded")
	tests := []struct {
		name string
		in   error
		want error
	}{
		{
			// The exact shape bubbletea produces when tea.WithContext's
			// deadline fires: ErrProgramKilled wrapping DeadlineExceeded.
			name: "kill wrapping deadline is a timeout, not an interrupt",
			in:   fmt.Errorf("%w: %w", tea.ErrProgramKilled, context.DeadlineExceeded),
			want: ErrWaitTimeout,
		},
		{
			name: "kill wrapping cancellation stays an interrupt",
			in:   fmt.Errorf("%w: %w", tea.ErrProgramKilled, context.Canceled),
			want: context.Canceled,
		},
		{
			name: "bare kill stays an interrupt",
			in:   tea.ErrProgramKilled,
			want: context.Canceled,
		},
		{
			name: "unrelated errors pass through",
			in:   otherErr,
			want: otherErr,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifySpinnerRunErr(tt.in)
			if !errors.Is(got, tt.want) {
				t.Fatalf("classifySpinnerRunErr(%v) = %v; want %v", tt.in, got, tt.want)
			}
		})
	}
}

// TestRunSpinnerDeadlineReturnsWaitTimeout drives the real Bubbletea spinner
// path with an expiring --wait-timeout deadline and asserts the timeout
// classification. Whichever way the internal race resolves (the runtime's
// context kill or schedulePoll observing ctx.Done), the caller must see
// ErrWaitTimeout — never context.Canceled, which commands map to exit 130
// with Ctrl-C wording.
func TestRunSpinnerDeadlineReturnsWaitTimeout(t *testing.T) {
	io, _, _ := cli.Test()
	io.SpinnersEnabled = true // force the TTY renderer despite buffer streams

	poll := func(_ context.Context) (string, bool, error) {
		return "Pending", false, nil
	}
	err := WaitFor(context.Background(), io, WaitForOpts{
		Label:    "cluster prod",
		Poll:     poll,
		Interval: 1 * time.Millisecond,
		Max:      1 * time.Millisecond,
		Timeout:  50 * time.Millisecond,
	})
	if !errors.Is(err, ErrWaitTimeout) {
		t.Fatalf("want ErrWaitTimeout, got %v", err)
	}
	if errors.Is(err, context.Canceled) {
		t.Fatalf("timeout misclassified as user interrupt: %v", err)
	}
}

// TestSpinnerModelCtrlCCancels asserts the raw-mode ^C key press still maps
// to context.Canceled (→ exit 130), unchanged by the timeout fix.
func TestSpinnerModelCtrlCCancels(t *testing.T) {
	cancelled := false
	m := spinnerModel{cancelPoll: func() { cancelled = true }}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	got, ok := updated.(spinnerModel)
	if !ok {
		t.Fatalf("Update returned %T; want spinnerModel", updated)
	}
	if !errors.Is(got.err, context.Canceled) {
		t.Fatalf("err = %v; want context.Canceled", got.err)
	}
	if !cancelled {
		t.Fatal("cancelPoll was not invoked on ^C")
	}
	if code := cli.ExitCode(got.err); code != cli.ExitInterrupted {
		t.Fatalf("exit code = %d; want %d", code, cli.ExitInterrupted)
	}
}
