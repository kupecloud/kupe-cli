package ux

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/kupecloud/kupe-cli/internal/cli"
)

// ErrWaitTimeout is returned when a WaitFor run exceeds its timeout.
// Commands translate this into cli.TimeoutError with exit code 8.
var ErrWaitTimeout = errors.New("wait timeout exceeded")

// PollFunc is the per-tick probe. It returns the current phase for display,
// whether the wait is done (target reached or resource gone), and any
// terminal error (e.g., Degraded state, or a GetCluster failure that isn't
// an expected 404). Transient errors (GetCluster 503) are the caller's
// responsibility to swallow — the retry loop inside the client already
// handles those.
type PollFunc func(ctx context.Context) (phase string, done bool, err error)

// WaitForOpts controls a wait run.
type WaitForOpts struct {
	// Label is the human-readable noun shown in progress text, e.g.
	// "cluster prod". Renderers append DoneVerb on success ("cluster
	// prod ready" / "cluster prod deleted").
	Label string
	// DoneVerb is the past-participle verb appended to Label on the
	// success line. Defaults to "ready" (used by create/update). Pass
	// "deleted" for a delete waiter so the line reads naturally.
	DoneVerb string
	// PhaseOverride, if non-empty, replaces the live phase column in the
	// spinner. Use it when the live phase is unhelpful to the user — e.g.
	// during an `update` that doesn't transition phase, where rendering
	// "Running" while the operator is mid-reconcile is misleading. Pass
	// "Updating" / "Upgrading" / etc. and the renderer shows it instead
	// of whatever Poll returned. Leave blank for create/delete waiters
	// where the live phase IS the story.
	PhaseOverride string
	// Poll is called once per tick.
	Poll PollFunc
	// Initial polling interval. Doubles each tick up to Max.
	Interval time.Duration
	// Max is the upper bound on the polling interval.
	Max time.Duration
	// Timeout is the hard deadline. Zero → no timeout.
	Timeout time.Duration
}

// Defaults fills zero-valued fields with the CLI's standard defaults.
func (o *WaitForOpts) Defaults() {
	if o.Interval == 0 {
		o.Interval = 2 * time.Second
	}
	if o.Max == 0 {
		o.Max = 10 * time.Second
	}
	if o.DoneVerb == "" {
		o.DoneVerb = "ready"
	}
}

// WaitFor polls opts.Poll until done, an error, a context cancellation, or
// opts.Timeout. Dispatches to the Bubbletea spinner (when streams support
// it) or a plain-text progress renderer (CI-friendly).
func WaitFor(ctx context.Context, io *cli.IOStreams, opts WaitForOpts) error {
	opts.Defaults()
	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}

	if io.SpinnersEnabled {
		return runSpinner(ctx, io, opts)
	}
	return runPlain(ctx, io, opts)
}

// pollResult carries one poll's outcome through the event loops.
type pollResult struct {
	Phase string
	Done  bool
	Err   error
}

// nextInterval computes the next polling delay given the current one.
// Exponential to Max.
func nextInterval(cur, maxD time.Duration) time.Duration {
	next := cur * 2
	if next > maxD {
		next = maxD
	}
	return next
}

// humaniseElapsed formats elapsed for the "[00:00:04]" prefix used by both
// renderers. Uses MM:SS under an hour, HH:MM:SS otherwise. Clock skew
// (negative elapsed) is clamped to zero.
func humaniseElapsed(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	d = d.Round(time.Second)
	s := int(d.Seconds())
	h, m, sec := s/3600, (s%3600)/60, s%60
	if h > 0 {
		return fmt.Sprintf("%02d:%02d:%02d", h, m, sec)
	}
	return fmt.Sprintf("%02d:%02d", m, sec)
}
