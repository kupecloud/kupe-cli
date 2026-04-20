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
		if phase != "" && phase != lastPhase {
			fmt.Fprintf(io.ErrOut, "[%s] %s\n", humaniseElapsed(time.Since(started)), phase)
			lastPhase = phase
		}
		if done {
			fmt.Fprintf(io.ErrOut, "[%s] %s ready\n", humaniseElapsed(time.Since(started)), opts.Label)
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
