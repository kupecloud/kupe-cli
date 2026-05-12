package ux

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/kupecloud/kupe-cli/internal/cli"
)

// runPlain is the CI-mode progress renderer: one line on stderr per phase
// transition, prefixed with elapsed time, no ANSI escapes. Never clears
// lines — scrollback stays readable.
func runPlain(ctx context.Context, io *cli.IOStreams, opts WaitForOpts) error {
	started := time.Now()
	interval := opts.Interval
	var lastPhase string

	for {
		phase, done, err := opts.Poll(ctx)
		if err != nil {
			return err
		}
		// In override mode the user has told us the live phase is unhelpful
		// (e.g. update where phase stays Running). Print the override once
		// up front and never echo the live phase to avoid a misleading
		// "Running" log line during a no-transition update.
		if opts.PhaseOverride != "" {
			if lastPhase == "" {
				fmt.Fprintf(io.ErrOut, "[%s] %s\n", humaniseElapsed(time.Since(started)), opts.PhaseOverride)
				lastPhase = opts.PhaseOverride
			}
		} else if phase != "" && phase != lastPhase {
			fmt.Fprintf(io.ErrOut, "[%s] %s\n", humaniseElapsed(time.Since(started)), phase)
			lastPhase = phase
		}
		if done {
			fmt.Fprintf(io.ErrOut, "[%s] %s %s\n", humaniseElapsed(time.Since(started)), opts.Label, opts.DoneVerb)
			return nil
		}

		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return ErrWaitTimeout
			}
			return ctx.Err()
		case <-time.After(interval):
			interval = nextInterval(interval, opts.Max)
		}
	}
}
