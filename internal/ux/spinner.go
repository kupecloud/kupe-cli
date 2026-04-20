package ux

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/kupecloud/kupe-cli/internal/cli"
)

// spinnerModel is a standalone Bubbletea program that owns one stderr line
// during a poll loop. The renderer ticks spinner.Tick + a pollMsg on an
// interval; pollMsg updates the model's phase and schedules the next poll.
// On a terminal state (done or err), the model stores the final phase/err
// and asks the runtime to quit, returning control to the command.
type spinnerModel struct {
	spinner  spinner.Model
	label    string
	phase    string
	started  time.Time
	interval time.Duration
	maxInt   time.Duration
	poll     PollFunc
	done     bool
	err      error
	ctx      context.Context //nolint:containedctx // lifetime matches the tea.Program, cancelled externally via WithContext
}

type pollMsg pollResult

func (m spinnerModel) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, m.scheduleImmediate())
}

// scheduleImmediate returns a Cmd that polls once on the next event-loop
// iteration — used at startup so the first status appears fast, before the
// initial interval elapses.
func (m spinnerModel) scheduleImmediate() tea.Cmd {
	return func() tea.Msg {
		phase, done, err := m.poll(m.ctx)
		return pollMsg{Phase: phase, Done: done, Err: err}
	}
}

// schedulePoll returns a Cmd that sleeps for `d`, then polls. Separate from
// scheduleImmediate so backoff happens between polls, not before the first.
func (m spinnerModel) schedulePoll(d time.Duration) tea.Cmd {
	return func() tea.Msg {
		t := time.NewTimer(d)
		defer t.Stop()
		select {
		case <-t.C:
		case <-m.ctx.Done():
			return pollMsg{Err: m.ctx.Err()}
		}
		phase, done, err := m.poll(m.ctx)
		return pollMsg{Phase: phase, Done: done, Err: err}
	}
}

func (m spinnerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case pollMsg:
		if msg.Err != nil {
			m.err = msg.Err
			m.done = true
			return m, tea.Quit
		}
		if msg.Phase != "" {
			m.phase = msg.Phase
		}
		if msg.Done {
			m.done = true
			return m, tea.Quit
		}
		// Schedule next poll with backoff.
		m.interval = nextInterval(m.interval, m.maxInt)
		return m, m.schedulePoll(m.interval)
	}
	return m, nil
}

func (m spinnerModel) View() string {
	if m.done {
		return "" // clear the line — final status is printed by runSpinner.
	}
	phase := m.phase
	if phase == "" {
		phase = "waiting"
	}
	elapsed := humaniseElapsed(time.Since(m.started))
	return fmt.Sprintf("%s %s %s  %s",
		m.spinner.View(),
		PhaseStyle(phase).Render(phase),
		DefaultPalette.Dim.Render("["+elapsed+"]"),
		DefaultPalette.Dim.Render(m.label),
	)
}

// runSpinner is the TTY-mode progress renderer: a live single-line spinner.
// Returns on the same terminal state rules as runPlain.
func runSpinner(ctx context.Context, io *cli.IOStreams, opts WaitForOpts) error {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = DefaultPalette.Dim

	m := spinnerModel{
		spinner:  sp,
		label:    opts.Label,
		started:  time.Now(),
		interval: opts.Interval,
		maxInt:   opts.Max,
		poll:     opts.Poll,
		ctx:      ctx,
	}

	// Render to stderr so data on stdout stays uncluttered. Use the
	// alternate-screen-off mode — we want to share the terminal with the
	// command that will print the result after us.
	p := tea.NewProgram(m, tea.WithOutput(io.ErrOut), tea.WithInput(io.In), tea.WithContext(ctx))
	finalModel, err := p.Run()
	if err != nil {
		if errors.Is(err, tea.ErrProgramKilled) {
			return context.Canceled
		}
		return err
	}

	final, _ := finalModel.(spinnerModel)
	if final.err != nil {
		if errors.Is(final.err, context.DeadlineExceeded) {
			return ErrWaitTimeout
		}
		return final.err
	}
	// Print a final status line so the user keeps a record on scrollback.
	fmt.Fprintf(io.ErrOut, "%s %s\n",
		DefaultPalette.Success.Render("✓"),
		fmt.Sprintf("%s ready (%s)", opts.Label, humaniseElapsed(time.Since(final.started))),
	)
	return nil
}
