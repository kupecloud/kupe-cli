// Package cmdutil holds helpers shared by the command packages: waiting on
// long-running operations and validating resource quantities.
package cmdutil

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/ux"
)

// PhaseDeleted is the pseudo-phase a wait reports once the resource is gone
// (the API returns 404). It is not a server-side phase.
const PhaseDeleted = "Deleted"

// TerminalPhaseError reports that a waited-on resource entered a phase that
// waiting does not clear: Degraded, or a binding's Failed. It is the
// resource's outcome, not a fault reaching kupe-api, so it must never be
// classified as transient.
type TerminalPhaseError struct {
	Noun, Name, Phase string
}

func (e *TerminalPhaseError) Error() string {
	return fmt.Sprintf("%s %q entered %s state", e.Noun, e.Name, e.Phase)
}

// PhaseGetter fetches a resource and reports its lifecycle phase. Clusters
// and databases share the phase vocabulary in the client.Phase* constants.
type PhaseGetter[T any] func(ctx context.Context) (resource T, phase string, err error)

// PollPhase builds a ux.PollFunc from get and decide. Transient fetch
// errors are ridden out (TolerateTransient); an error from decide is
// terminal, so a resource that has failed (e.g. Degraded) stops the wait at
// once instead of being retried as if the API were flaky.
func PollPhase[T any](get PhaseGetter[T], decide func(resource T, phase string) (done bool, err error)) ux.PollFunc {
	var latest T
	fetched := false
	fetch := TolerateTransient(func(ctx context.Context) (string, bool, error) {
		resource, phase, err := get(ctx)
		if err != nil {
			return "", false, err
		}
		latest, fetched = resource, true
		return phase, false, nil
	})
	return func(ctx context.Context) (string, bool, error) {
		fetched = false
		phase, _, err := fetch(ctx)
		if err != nil || !fetched {
			// A failed fetch, or a transient one being ridden out.
			return phase, false, err
		}
		done, err := decide(latest, phase)
		return phase, done, err
	}
}

// WaitForPhase polls get until the phase equals target. Degraded and a
// binding's Failed end the wait with a *TerminalPhaseError unless they are
// the target. Returns the last observed resource (the zero value if none was
// seen).
func WaitForPhase[T any](ctx context.Context, streams *cli.IOStreams, noun, name, target string, get PhaseGetter[T], timeout time.Duration) (T, error) {
	var latest T
	poll := PollPhase(get, func(resource T, phase string) (bool, error) {
		latest = resource
		if phase == target {
			return true, nil
		}
		if phase == client.PhaseDegraded || phase == client.PhaseFailed {
			return false, &TerminalPhaseError{Noun: noun, Name: name, Phase: phase}
		}
		return false, nil
	})
	err := ux.WaitFor(ctx, streams, ux.WaitForOpts{
		Label:   noun + " " + name,
		Poll:    poll,
		Timeout: timeout,
	})
	return latest, err
}

// WaitForGone polls until get returns a plain 404. It reports "Deleting"
// while the resource still exists, whatever its phase. A 404 that carries
// an error code is a refusal (e.g. a feature switched off on the platform),
// not proof the resource is gone, so it ends the wait as an error.
func WaitForGone[T any](ctx context.Context, streams *cli.IOStreams, noun, name string, get PhaseGetter[T], timeout time.Duration) error {
	poll := func(ctx context.Context) (string, bool, error) {
		if _, _, err := get(ctx); err != nil {
			if client.IsNotFound(err) && client.ErrorCode(err) == "" {
				return PhaseDeleted, true, nil
			}
			return "", false, err
		}
		return "Deleting", false, nil
	}
	return ux.WaitFor(ctx, streams, ux.WaitForOpts{
		Label:    noun + " " + name,
		DoneVerb: "deleted",
		Poll:     TolerateTransient(poll),
		Timeout:  timeout,
	})
}

// RunWait is the body of a `kupe <noun> wait NAME --for PHASE` command. On
// success it prints "<noun>/<name> <phase>" to stdout.
func RunWait[T any](ctx context.Context, streams *cli.IOStreams, noun, name, forPhase string, get PhaseGetter[T], timeout time.Duration) error {
	target := NormalizePhase(forPhase)
	switch target {
	case "":
		return cli.MisuseError(fmt.Sprintf("unsupported --for value %q", forPhase))
	case PhaseDeleted:
		if err := WaitForGone(ctx, streams, noun, name, get, timeout); err != nil {
			return MapWaitErr(err, noun, name, "delete")
		}
	default:
		if _, err := WaitForPhase(ctx, streams, noun, name, target, get, timeout); err != nil {
			return MapWaitErr(err, noun, name, "wait")
		}
	}
	fmt.Fprintf(streams.Out, "%s/%s %s\n", noun, name, strings.ToLower(target))
	return nil
}

// NormalizePhase maps friendly, case-insensitive spellings onto the
// server-side phase constants (or PhaseDeleted). Returns "" for unknowns.
func NormalizePhase(s string) string {
	switch strings.ToLower(s) {
	case "running":
		return client.PhaseRunning
	case "deleted", "gone":
		return PhaseDeleted
	case "pending":
		return client.PhasePending
	case "provisioning":
		return client.PhaseProvisioning
	case "upgrading":
		return client.PhaseUpgrading
	case "degraded":
		return client.PhaseDegraded
	case "terminating":
		return client.PhaseTerminating
	}
	return ""
}

// transientWaitGrace is how long a wait loop tolerates *consecutive* transient
// API errors (5xx, 429, network/transport) before giving up. Covers a rolling
// kupe-api deploy, an LB blip, or a brief 502 without aborting a long
// `--wait`. A first success resets the window.
const transientWaitGrace = 90 * time.Second

// IsTransientWaitErr reports whether err is a transient failure a wait loop
// should ride out rather than treat as terminal: any non-APIError (network /
// transport / context-independent I/O) or a 5xx / 429 from kupe-api. A 404,
// 401, 403, or 400 is authoritative and must stay terminal — callers handle
// 404 themselves (e.g. delete-wait treats it as success). So is a
// *TerminalPhaseError: the API answered, and the resource has failed.
func IsTransientWaitErr(err error) bool {
	if err == nil {
		return false
	}
	var terminal *TerminalPhaseError
	if errors.As(err, &terminal) {
		return false
	}
	// A mid-wait OIDC refresh that hit its own 30s budget is a transient auth
	// fault (an IdP blip on the exec-plugin token path), not the wait's own
	// deadline — ride it out on the grace instead of aborting the whole
	// --wait and misattributing it as a wait timeout (exit 8).
	if errors.Is(err, cli.ErrAuthRefreshTimeout) {
		return true
	}
	// Context cancellation/deadline is handled by the waiter itself; never
	// swallow it here.
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if client.IsUnavailable(err) || client.IsRateLimited(err) || client.IsServerError(err) {
		return true
	}
	// A typed 4xx (401/403/404/400/409/412) is authoritative — terminal.
	// Anything left that isn't a typed APIError is a transport/network error
	// (the client already exhausted its short internal retry) — transient.
	return !client.IsAPIError(err)
}

// TolerateTransient wraps a poll function so consecutive transient errors are
// swallowed (the loop keeps polling, surfacing the last known phase) until
// transientWaitGrace elapses, after which the error becomes terminal. A
// successful poll resets the window. Terminal errors pass through immediately.
func TolerateTransient(poll ux.PollFunc) ux.PollFunc {
	var firstFailure time.Time
	lastPhase := ""
	return func(ctx context.Context) (string, bool, error) {
		phase, done, err := poll(ctx)
		if err == nil {
			firstFailure = time.Time{}
			lastPhase = phase
			return phase, done, nil
		}
		if !IsTransientWaitErr(err) {
			return phase, done, err
		}
		now := time.Now()
		if firstFailure.IsZero() {
			firstFailure = now
		}
		if now.Sub(firstFailure) > transientWaitGrace {
			return phase, false, err
		}
		// Keep waiting: report the last known phase, no error.
		return lastPhase, false, nil
	}
}

// MapWaitErr translates errors out of a long-running wait into typed CLI
// errors with actionable hints:
//
//   - timeout: still in flight server-side, exit 8
//   - cancellation (Ctrl-C): the API call already succeeded; the resource
//     is being created/updated/deleted on Kupe Cloud regardless of whether
//     we kept watching. Say so explicitly, exit 130
//   - the resource entered a terminal phase (*TerminalPhaseError): it has
//     failed, so exit 1 naming it and its phase, with the check-status hint
//   - a transient API error that outlived the grace window: exit 7 with the
//     same check-status / resume-wait guidance
//
// noun is the command noun ("cluster", "database"); verb is "create",
// "update", "delete" or "wait" and drives the wording.
func MapWaitErr(err error, noun, name, verb string) error {
	if err == nil {
		return nil
	}
	check := fmt.Sprintf("check status:  kupe %s get %s", noun, name)
	resume := fmt.Sprintf("resume wait:   kupe %s wait %s", noun, name)
	if verb == "delete" {
		resume += " --for=Deleted"
	}
	if errors.Is(err, ux.ErrWaitTimeout) {
		return cli.TimeoutError(err.Error()).WithHint(check + "\n" + resume)
	}
	if errors.Is(err, context.Canceled) {
		switch verb {
		case "delete", "update":
			return cli.New(cli.ExitInterrupted,
				fmt.Sprintf("stopped waiting; %s %q is still being %sd on Kupe Cloud", noun, name, verb)).
				WithHint(check + "\n" + resume)
		case "wait":
			return cli.New(cli.ExitInterrupted,
				fmt.Sprintf("stopped waiting; %s %q may still be transitioning on Kupe Cloud", noun, name)).
				WithHint(check)
		default:
			return cli.New(cli.ExitInterrupted,
				fmt.Sprintf("stopped waiting; %s %q is still being created on Kupe Cloud", noun, name)).
				WithHint(strings.Join([]string{
					check,
					resume,
					fmt.Sprintf("abandon:       kupe %s delete %s", noun, name),
				}, "\n"))
		}
	}
	var terminal *TerminalPhaseError
	if errors.As(err, &terminal) {
		return cli.New(cli.ExitGeneral, terminal.Error()).WithHint(check)
	}
	if client.IsUnavailable(err) || client.IsRateLimited(err) || client.IsServerError(err) || !client.IsAPIError(err) {
		return cli.Wrap(cli.ExitUnavailable, "lost contact with kupe-api while waiting", err).WithHint(check + "\n" + resume)
	}
	return err
}
