package ux

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kupecloud/kupe-cli/internal/cli"
)

func TestWaitForPlainLogsPhaseTransitions(t *testing.T) {
	io, _, errOut := cli.Test()
	// Test() zeros SpinnersEnabled → plain path is exercised.

	var calls int32
	poll := func(_ context.Context) (string, bool, error) {
		n := atomic.AddInt32(&calls, 1)
		switch n {
		case 1:
			return "Pending", false, nil
		case 2:
			return "Provisioning", false, nil
		case 3:
			return "Running", true, nil
		}
		return "Running", true, nil
	}
	err := WaitFor(context.Background(), io, WaitForOpts{
		Label:    "cluster prod",
		Poll:     poll,
		Interval: 1 * time.Millisecond,
		Max:      1 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("WaitFor: %v", err)
	}
	out := errOut.String()
	for _, phase := range []string{"Pending", "Provisioning", "Running"} {
		if !strings.Contains(out, phase) {
			t.Errorf("missing phase %q in output:\n%s", phase, out)
		}
	}
	if !strings.Contains(out, "cluster prod ready") {
		t.Errorf("missing final ready line:\n%s", out)
	}
}

func TestWaitForPlainDoesNotRepeatUnchangedPhase(t *testing.T) {
	io, _, errOut := cli.Test()

	var calls int32
	poll := func(_ context.Context) (string, bool, error) {
		n := atomic.AddInt32(&calls, 1)
		if n < 3 {
			return "Provisioning", false, nil
		}
		return "Running", true, nil
	}
	if err := WaitFor(context.Background(), io, WaitForOpts{
		Label:    "cluster prod",
		Poll:     poll,
		Interval: 1 * time.Millisecond,
		Max:      1 * time.Millisecond,
	}); err != nil {
		t.Fatal(err)
	}
	// Count "Provisioning" occurrences in output — should be exactly 1
	// despite the poll returning it twice.
	got := strings.Count(errOut.String(), "Provisioning")
	if got != 1 {
		t.Fatalf("Provisioning logged %d times; want 1 (only first observation):\n%s", got, errOut.String())
	}
}

func TestWaitForSurfacesPollError(t *testing.T) {
	io, _, _ := cli.Test()

	wantErr := errors.New("boom")
	poll := func(_ context.Context) (string, bool, error) {
		return "", false, wantErr
	}
	err := WaitFor(context.Background(), io, WaitForOpts{
		Label:    "cluster prod",
		Poll:     poll,
		Interval: 1 * time.Millisecond,
		Max:      1 * time.Millisecond,
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("want %v, got %v", wantErr, err)
	}
}

func TestWaitForTimeout(t *testing.T) {
	io, _, _ := cli.Test()
	poll := func(_ context.Context) (string, bool, error) {
		return "Pending", false, nil
	}
	err := WaitFor(context.Background(), io, WaitForOpts{
		Label:    "cluster prod",
		Poll:     poll,
		Interval: 1 * time.Millisecond,
		Max:      1 * time.Millisecond,
		Timeout:  20 * time.Millisecond,
	})
	if !errors.Is(err, ErrWaitTimeout) {
		t.Fatalf("want ErrWaitTimeout, got %v", err)
	}
}

// TestWaitForPlainDeadlineMidPoll pins that a poll cut short by the wait's
// own deadline reports ErrWaitTimeout (exit 8), while a deadline error from
// a poll whose wait is still live passes through untouched.
func TestWaitForPlainDeadlineMidPoll(t *testing.T) {
	tests := []struct {
		name        string
		poll        PollFunc
		wantTimeout bool
	}{
		{"the wait's deadline fires during a poll", func(ctx context.Context) (string, bool, error) {
			<-ctx.Done()
			return "", false, fmt.Errorf("executing request: %w", ctx.Err())
		}, true},
		{"a poll's own deadline while the wait is live", func(context.Context) (string, bool, error) {
			return "", false, fmt.Errorf("executing request: %w", context.DeadlineExceeded)
		}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			io, _, _ := cli.Test()
			err := WaitFor(context.Background(), io, WaitForOpts{
				Label:   "cluster prod",
				Poll:    tt.poll,
				Timeout: 50 * time.Millisecond,
			})
			if got := errors.Is(err, ErrWaitTimeout); got != tt.wantTimeout {
				t.Fatalf("errors.Is(err, ErrWaitTimeout) = %v; want %v (err: %v)", got, tt.wantTimeout, err)
			}
		})
	}
}

func TestHumaniseElapsed(t *testing.T) {
	tests := []struct {
		in   time.Duration
		want string
	}{
		{5 * time.Second, "00:05"},
		{61 * time.Second, "01:01"},
		{3665 * time.Second, "01:01:05"},
	}
	for _, tt := range tests {
		if got := humaniseElapsed(tt.in); got != tt.want {
			t.Errorf("humaniseElapsed(%v) = %q; want %q", tt.in, got, tt.want)
		}
	}
}

func TestNextInterval(t *testing.T) {
	if got := nextInterval(2*time.Second, 10*time.Second); got != 4*time.Second {
		t.Errorf("doubling: %v", got)
	}
	if got := nextInterval(8*time.Second, 10*time.Second); got != 10*time.Second {
		t.Errorf("clamp: %v", got)
	}
}
