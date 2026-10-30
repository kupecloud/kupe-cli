package cmdutil

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/ux"
)

func TestIsTransientWaitErr_ClassifiesErrors(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"503", &client.APIError{StatusCode: 503}, true},
		{"502", &client.APIError{StatusCode: 502}, true},
		{"500", &client.APIError{StatusCode: 500}, true},
		{"429", &client.APIError{StatusCode: 429}, true},
		{"404 terminal", &client.APIError{StatusCode: 404}, false},
		{"401 terminal", &client.APIError{StatusCode: 401}, false},
		{"403 terminal", &client.APIError{StatusCode: 403}, false},
		{"400 terminal", &client.APIError{StatusCode: 400}, false},
		{"409 terminal", &client.APIError{StatusCode: 409}, false},
		{"network error transient", errors.New("dial tcp: connection refused"), true},
		{"context canceled not transient", context.Canceled, false},
		{"deadline not transient", context.DeadlineExceeded, false},
		// C4: a mid-wait OIDC refresh timeout is a transient auth fault, not
		// the wait's own deadline — it must ride the grace, and must NOT be
		// wrapped as context.DeadlineExceeded (else the spinner maps it to
		// ErrWaitTimeout / exit 8).
		{"auth refresh timeout transient", cli.ErrAuthRefreshTimeout, true},
		{"wrapped auth refresh timeout transient", fmt.Errorf("resolving auth token: %w", cli.ErrAuthRefreshTimeout), true},
		// A resource that failed is an answer from kupe-api, not a blip.
		{"stalled request attempt transient", fmt.Errorf("executing request: %w", client.ErrAttemptTimeout), true},
		{"terminal phase not transient", &TerminalPhaseError{Noun: "database", Name: "orders-db", Phase: client.PhaseDegraded}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := IsTransientWaitErr(c.err); got != c.want {
				t.Fatalf("IsTransientWaitErr(%v) = %v; want %v", c.err, got, c.want)
			}
		})
	}
}

// TestTolerateTransient_SwallowsTransient proves consecutive transient
// errors are swallowed (no error returned, last phase reported).
func TestTolerateTransient_SwallowsTransient(t *testing.T) {
	calls := 0
	base := func(context.Context) (string, bool, error) {
		calls++
		switch calls {
		case 1:
			return "Provisioning", false, nil // establishes lastPhase
		case 2:
			return "", false, &client.APIError{StatusCode: 503} // transient
		default:
			return "Running", true, nil
		}
	}
	wrapped := TolerateTransient(base)

	if phase, done, err := wrapped(context.Background()); err != nil || done || phase != "Provisioning" {
		t.Fatalf("call 1: (%q,%v,%v)", phase, done, err)
	}
	// Transient error: swallowed, last phase reported, not done.
	if phase, done, err := wrapped(context.Background()); err != nil || done || phase != "Provisioning" {
		t.Fatalf("call 2 (transient): want (Provisioning,false,nil), got (%q,%v,%v)", phase, done, err)
	}
	if phase, done, err := wrapped(context.Background()); err != nil || !done || phase != "Running" {
		t.Fatalf("call 3: want (Running,true,nil), got (%q,%v,%v)", phase, done, err)
	}
}

func TestTolerateTransient_PassesTerminalThrough(t *testing.T) {
	terminal := &client.APIError{StatusCode: 404}
	wrapped := TolerateTransient(func(context.Context) (string, bool, error) {
		return "", false, terminal
	})
	_, _, err := wrapped(context.Background())
	if !errors.Is(err, terminal) {
		t.Fatalf("terminal error should pass through; got %v", err)
	}
}

// TestMapWaitErr_ExitCodesAndWording pins the exit codes for the ways a wait
// ends early (terminal phase → 1, timeout → 8, Ctrl-C → 130, lost API → 7)
// and that the hints name the command noun, so a database wait never points
// at cluster commands.
func TestMapWaitErr_ExitCodesAndWording(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		noun, verb string
		wantCode   int
		wantText   []string
		rejectText string
	}{
		{"degraded exits 1 naming the resource, not lost contact",
			&TerminalPhaseError{Noun: "database", Name: "prod", Phase: client.PhaseDegraded}, "database", "create", cli.ExitGeneral,
			[]string{`database "prod" entered Degraded state`, "kupe database get prod"}, "lost contact"},
		{"failed exits 1 naming the resource, not lost contact",
			&TerminalPhaseError{Noun: "cluster", Name: "prod", Phase: client.PhaseFailed}, "cluster", "wait", cli.ExitGeneral,
			[]string{`cluster "prod" entered Failed state`, "kupe cluster get prod"}, "lost contact"},
		{"a wrapped terminal phase still exits 1",
			fmt.Errorf("polling: %w", &TerminalPhaseError{Noun: "cluster", Name: "prod", Phase: client.PhaseDegraded}), "cluster", "update", cli.ExitGeneral,
			[]string{"entered Degraded state", "kupe cluster get prod"}, "lost contact"},
		{"timeout exits 8 without Ctrl-C wording", ux.ErrWaitTimeout, "cluster", "create", cli.ExitTimeout,
			[]string{"kupe cluster get prod", "kupe cluster wait prod"}, "stopped waiting"},
		{"interrupt during create explains the operation continues", context.Canceled, "database", "create", cli.ExitInterrupted,
			[]string{"stopped waiting", "database \"prod\" is still being created", "kupe database delete prod"}, "cluster"},
		{"interrupt during delete resumes with --for=Deleted", context.Canceled, "database", "delete", cli.ExitInterrupted,
			[]string{"still being deleted", "kupe database wait prod --for=Deleted"}, ""},
		{"interrupt during update", context.Canceled, "cluster", "update", cli.ExitInterrupted,
			[]string{"still being updated"}, ""},
		{"interrupt during wait", context.Canceled, "database", "wait", cli.ExitInterrupted,
			[]string{"may still be transitioning", "kupe database get prod"}, ""},
		{"lost API exits 7", &client.APIError{StatusCode: 503}, "database", "create", cli.ExitUnavailable,
			[]string{"lost contact", "kupe database wait prod"}, ""},
		{"network error exits 7", errors.New("dial tcp: connection refused"), "cluster", "create", cli.ExitUnavailable,
			[]string{"lost contact", "kupe cluster get prod", "kupe cluster wait prod"}, ""},
		{"a 404 passes through", &client.APIError{StatusCode: 404, Message: "database not found"}, "database", "wait", cli.ExitNotFound,
			nil, "lost contact"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := MapWaitErr(tt.err, tt.noun, "prod", tt.verb)
			if code := cli.ExitCode(err); code != tt.wantCode {
				t.Fatalf("exit = %d; want %d (%v)", code, tt.wantCode, err)
			}
			text := err.Error()
			var cliErr *cli.Error
			if errors.As(err, &cliErr) {
				text += "\n" + cliErr.Hint
			}
			for _, want := range tt.wantText {
				if !strings.Contains(text, want) {
					t.Errorf("missing %q in:\n%s", want, text)
				}
			}
			if tt.rejectText != "" && strings.Contains(text, tt.rejectText) {
				t.Errorf("unexpected %q in:\n%s", tt.rejectText, text)
			}
		})
	}
}

func TestNormalizePhase_AcceptsFriendlySpellings(t *testing.T) {
	tests := map[string]string{
		"running": client.PhaseRunning, "RUNNING": client.PhaseRunning, "Pending": client.PhasePending,
		"provisioning": client.PhaseProvisioning, "upgrading": client.PhaseUpgrading,
		"degraded": client.PhaseDegraded, "terminating": client.PhaseTerminating,
		"deleted": PhaseDeleted, "gone": PhaseDeleted, "bogus": "", "": "",
	}
	for in, want := range tests {
		if got := NormalizePhase(in); got != want {
			t.Errorf("NormalizePhase(%q) = %q; want %q", in, got, want)
		}
	}
}

// scriptedGetter returns successive phases; "" stands for a 404.
func scriptedGetter(phases ...string) PhaseGetter[string] {
	i := 0
	return func(context.Context) (string, string, error) {
		phase := phases[min(i, len(phases)-1)]
		i++
		if phase == "" {
			return "", "", &client.APIError{StatusCode: 404}
		}
		return "resource-" + phase, phase, nil
	}
}

func TestWaitForPhase(t *testing.T) {
	tests := []struct {
		name         string
		phases       []string
		target       string
		wantErr      string
		wantTerminal bool
		wantLast     string
	}{
		{"returns the resource once the target is reached", []string{"Running"}, client.PhaseRunning, "", false, "resource-Running"},
		{"degraded fails a wait for running", []string{"Degraded"}, client.PhaseRunning, "entered Degraded state", true, "resource-Degraded"},
		{"degraded satisfies a wait for degraded", []string{"Degraded"}, client.PhaseDegraded, "", false, "resource-Degraded"},
		{"a failed binding fails a wait for ready", []string{"Failed"}, client.PhaseReady, "entered Failed state", true, "resource-Failed"},
		{"a 404 is terminal", []string{""}, client.PhaseRunning, "Not Found", false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			io, _, _ := cli.Test()
			last, err := WaitForPhase(context.Background(), io, "database", "orders-db", tt.target, scriptedGetter(tt.phases...), 5*time.Second)
			if tt.wantErr == "" && err != nil {
				t.Fatalf("WaitForPhase: %v", err)
			}
			if tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("err = %v; want containing %q", err, tt.wantErr)
			}
			var terminal *TerminalPhaseError
			if got := errors.As(err, &terminal); got != tt.wantTerminal {
				t.Fatalf("err = %v is a *TerminalPhaseError: %v; want %v", err, got, tt.wantTerminal)
			}
			if last != tt.wantLast {
				t.Fatalf("last = %q; want %q", last, tt.wantLast)
			}
		})
	}
}

func TestWaitForGone(t *testing.T) {
	refused := &client.APIError{StatusCode: 404, Code: "databases_unavailable", Message: "managed databases are not available on this platform"}
	tests := []struct {
		name    string
		get     PhaseGetter[string]
		wantErr bool
	}{
		{"a plain 404 means gone", scriptedGetter(""), false},
		{"a 404 with an error code is a refusal, not gone", func(context.Context) (string, string, error) { return "", "", refused }, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			io, _, _ := cli.Test()
			err := WaitForGone(context.Background(), io, "database", "orders-db", tt.get, 5*time.Second)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v; wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestRunWait(t *testing.T) {
	tests := []struct {
		name     string
		forPhase string
		phases   []string
		wantOut  string
		wantCode int
	}{
		{"prints the reached phase", "running", []string{"Running"}, "database/orders-db running\n", cli.ExitOK},
		{"prints deleted once gone", "deleted", []string{""}, "database/orders-db deleted\n", cli.ExitOK},
		{"rejects an unknown phase as misuse", "bogus", []string{"Running"}, "", cli.ExitMisuse},
		{"degraded fails a wait for running with exit 1", "running", []string{"Degraded"}, "", cli.ExitGeneral},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			io, out, _ := cli.Test()
			err := RunWait(context.Background(), io, "database", "orders-db", tt.forPhase, scriptedGetter(tt.phases...), 5*time.Second)
			if code := cli.ExitCode(err); code != tt.wantCode {
				t.Fatalf("exit = %d; want %d (%v)", code, tt.wantCode, err)
			}
			if out.String() != tt.wantOut {
				t.Fatalf("stdout = %q; want %q", out.String(), tt.wantOut)
			}
		})
	}
}

// observed is a resource as WaitForConverged sees it.
type observed struct {
	phase      string
	generation int64
	settled    bool
}

// observedSeq returns each entry in turn on successive polls, repeating the
// last.
func observedSeq(seq ...observed) PhaseGetter[observed] {
	i := 0
	return func(context.Context) (observed, string, error) {
		o := seq[min(i, len(seq)-1)]
		i++
		return o, o.phase, nil
	}
}

func TestWaitForConverged(t *testing.T) {
	converge := func(grace time.Duration) Converge[observed] {
		return Converge[observed]{
			ObservedGeneration: func(o observed) int64 { return o.generation },
			Settled:            func(o observed) bool { return o.settled },
			DegradedGrace:      grace,
		}
	}
	running := func(gen int64, settled bool) observed { return observed{client.PhaseRunning, gen, settled} }
	tests := []struct {
		name       string
		seq        []observed
		grace      time.Duration
		generation int64
		wantPhase  string // the terminal phase; "" = converged
	}{
		{"waits for observedGeneration to catch up", []observed{running(4, true), running(5, true)}, 0, 5, ""},
		{"waits until settled at the generation", []observed{running(5, false), running(5, true)}, 0, 5, ""},
		{"generation 0 disables the generation check", []observed{running(0, true)}, 0, 0, ""},
		{"degraded fails at once without a grace", []observed{{client.PhaseDegraded, 5, false}}, 0, 5, client.PhaseDegraded},
		{"degraded within the grace is ridden out", []observed{{client.PhaseDegraded, 5, false}, running(5, true)}, time.Hour, 5, ""},
		{"degraded beyond the grace fails", []observed{{client.PhaseDegraded, 5, false}}, time.Millisecond, 5, client.PhaseDegraded},
		{"terminating fails", []observed{{client.PhaseTerminating, 5, false}}, time.Hour, 5, client.PhaseTerminating},
		// The first poll reads the status from before the PATCH: a Degraded
		// the operator has not yet acted on must not fail a zero-grace
		// cluster wait, and the change may heal it.
		{"degraded before the change is seen is waited out", []observed{{client.PhaseDegraded, 4, false}, running(5, true)}, 0, 5, ""},
		{"terminating fails even before the change is seen", []observed{{client.PhaseTerminating, 4, false}}, time.Hour, 5, client.PhaseTerminating},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			io, _, _ := cli.Test()
			last, err := WaitForConverged(context.Background(), io, "database", "orders-db",
				observedSeq(tt.seq...), converge(tt.grace), tt.generation, 20*time.Second)
			if tt.wantPhase == "" {
				if err != nil {
					t.Fatalf("WaitForConverged: %v", err)
				}
				if want := tt.seq[len(tt.seq)-1]; last != want {
					t.Fatalf("last = %+v; want %+v", last, want)
				}
				return
			}
			var terminal *TerminalPhaseError
			if !errors.As(err, &terminal) || terminal.Phase != tt.wantPhase {
				t.Fatalf("err = %v; want a terminal %s", err, tt.wantPhase)
			}
		})
	}
}

func TestWaitForConverged_TimesOutWhenNeverApplied(t *testing.T) {
	io, _, _ := cli.Test()
	stale := observed{client.PhaseRunning, 6, true}
	_, err := WaitForConverged(context.Background(), io, "cluster", "prod", observedSeq(stale),
		Converge[observed]{ObservedGeneration: func(o observed) int64 { return o.generation }}, 7, 300*time.Millisecond)
	if !errors.Is(err, ux.ErrWaitTimeout) {
		t.Fatalf("err = %v; want ux.ErrWaitTimeout", err)
	}
}

// TestWaitForPhaseDone_OnlyChangesItsOwnSuccessLine pins that a wait given
// a done verb ends with it, and a plain WaitForPhase keeps "ready".
func TestWaitForPhaseDone_OnlyChangesItsOwnSuccessLine(t *testing.T) {
	io, _, errOut := cli.Test()
	if _, err := WaitForPhaseDone(context.Background(), io, "database backup", "b1", client.PhaseCompleted, "completed",
		scriptedGetter(client.PhaseCompleted), 5*time.Second); err != nil {
		t.Fatalf("WaitForPhaseDone: %v", err)
	}
	if !strings.Contains(errOut.String(), "database backup b1 completed\n") {
		t.Errorf("stderr = %q; want the done verb", errOut.String())
	}
	io, _, errOut = cli.Test()
	if _, err := WaitForPhase(context.Background(), io, "database", "orders-db", client.PhaseRunning,
		scriptedGetter(client.PhaseRunning), 5*time.Second); err != nil {
		t.Fatalf("WaitForPhase: %v", err)
	}
	if !strings.Contains(errOut.String(), "database orders-db ready\n") {
		t.Errorf("stderr = %q; want the default verb", errOut.String())
	}
}
