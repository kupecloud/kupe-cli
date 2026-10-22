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

// subject is what a wait or a hint is about: one named resource of a
// service, or a service's only resource (Service.Singleton), whose commands
// take no name.
type subject struct {
	noun, name string
	singleton  bool
}

// named is the subject for the resource name of the service noun.
func named(noun, name string) subject { return subject{noun: noun, name: name} }

// label is the progress label: "database orders-db", or "registry".
func (s subject) label() string {
	if s.singleton {
		return s.noun
	}
	return s.noun + " " + s.name
}

// command is the command a hint gives: "kupe database get orders-db", or
// "kupe registry get" for a singleton.
func (s subject) command(verb string) string {
	if s.singleton {
		return "kupe " + s.noun + " " + verb
	}
	return "kupe " + s.noun + " " + verb + " " + s.name
}

// phrase names the subject in a sentence: database "orders-db", or "the
// registry".
func (s subject) phrase() string {
	if s.singleton {
		return "the " + s.noun
	}
	return fmt.Sprintf("%s %q", s.noun, s.name)
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
	return waitForPhase(ctx, streams, named(noun, name), target, get, timeout)
}

func waitForPhase[T any](ctx context.Context, streams *cli.IOStreams, subj subject, target string, get PhaseGetter[T], timeout time.Duration) (T, error) {
	var latest T
	poll := PollPhase(get, func(resource T, phase string) (bool, error) {
		latest = resource
		if phase == target {
			return true, nil
		}
		if phase == client.PhaseDegraded || phase == client.PhaseFailed {
			return false, &TerminalPhaseError{Noun: subj.noun, Name: subj.name, Phase: phase}
		}
		return false, nil
	})
	err := ux.WaitFor(ctx, streams, ux.WaitForOpts{
		Label:   subj.label(),
		Poll:    poll,
		Timeout: timeout,
	})
	return latest, err
}

// Converge says when a service's spec change has taken effect, for
// WaitForConverged.
type Converge[T any] struct {
	// ObservedGeneration reads status.observedGeneration: the generation
	// the operator last applied. The operator advances it only on a pass
	// that applied the spec, so a failed pass never reads as converged.
	ObservedGeneration func(T) int64
	// Settled, when set, must also hold: for a database, its Ready
	// condition, since the operator applies a change before its instances
	// have rolled onto it and the phase stays Running meanwhile.
	Settled func(T) bool
	// DegradedGrace is how long a Degraded phase is ridden out before it
	// ends the wait; 0 ends it at once. A single-instance database is
	// Degraded while its only instance restarts to apply a CPU or memory
	// change.
	DegradedGrace time.Duration
}

// WaitForConverged is the wait behind `update --wait`: it polls get until
// the operator has applied the spec at generation (observedGeneration
// caught up), the phase is Running and converge.Settled holds.
//
// observedGeneration, not a phase transition, is the signal: an in-place
// change (a resize the operator applies to a quota) never leaves Running,
// so waiting for the phase to change would hang. A generation of 0 (an
// older server that returns none) disables that check, and the wait
// degrades to "phase is Running". Degraded beyond converge.DegradedGrace,
// once observedGeneration has caught up, and Terminating at any time end the
// wait with a *TerminalPhaseError. Returns the last
// resource seen (the zero value if none was).
func WaitForConverged[T any](ctx context.Context, streams *cli.IOStreams, noun, name string, get PhaseGetter[T], converge Converge[T], generation int64, timeout time.Duration) (T, error) {
	return waitForConverged(ctx, streams, named(noun, name), get, converge, generation, timeout)
}

func waitForConverged[T any](ctx context.Context, streams *cli.IOStreams, subj subject, get PhaseGetter[T], converge Converge[T], generation int64, timeout time.Duration) (T, error) {
	var latest T
	var degradedSince time.Time
	poll := PollPhase(get, func(resource T, phase string) (bool, error) {
		latest = resource
		if phase != client.PhaseDegraded {
			degradedSince = time.Time{}
		}
		// A status from before the operator saw this change says nothing
		// about the change: the first poll can read the pre-PATCH status, and
		// a resource Degraded before an update may be healed by it. Degraded
		// counts only once observedGeneration has caught up (as in the
		// Terraform provider, 5b74022).
		applied := generation <= 0 || converge.ObservedGeneration(resource) >= generation
		switch phase {
		case client.PhaseDegraded:
			if !applied {
				degradedSince = time.Time{}
				return false, nil
			}
			if degradedSince.IsZero() {
				degradedSince = time.Now()
			}
			if time.Since(degradedSince) >= converge.DegradedGrace {
				return false, &TerminalPhaseError{Noun: subj.noun, Name: subj.name, Phase: phase}
			}
		case client.PhaseTerminating:
			return false, &TerminalPhaseError{Noun: subj.noun, Name: subj.name, Phase: phase}
		case client.PhaseRunning:
			if !applied {
				return false, nil
			}
			return converge.Settled == nil || converge.Settled(resource), nil
		}
		return false, nil
	})
	err := ux.WaitFor(ctx, streams, ux.WaitForOpts{
		Label:         subj.label(),
		PhaseOverride: "Updating",
		Poll:          poll,
		Timeout:       timeout,
	})
	return latest, err
}

// WaitForGone polls until get returns a plain 404. It reports "Deleting"
// while the resource still exists, whatever its phase. A 404 that carries
// an error code is a refusal (e.g. a feature switched off on the platform),
// not proof the resource is gone, so it ends the wait as an error.
func WaitForGone[T any](ctx context.Context, streams *cli.IOStreams, noun, name string, get PhaseGetter[T], timeout time.Duration) error {
	return waitForGone(ctx, streams, named(noun, name), get, timeout)
}

func waitForGone[T any](ctx context.Context, streams *cli.IOStreams, subj subject, get PhaseGetter[T], timeout time.Duration) error {
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
		Label:    subj.label(),
		DoneVerb: "deleted",
		Poll:     TolerateTransient(poll),
		Timeout:  timeout,
	})
}

// RunWait is the body of a `kupe <noun> wait NAME --for PHASE` command. On
// success it prints "<noun>/<name> <phase>" to stdout.
func RunWait[T any](ctx context.Context, streams *cli.IOStreams, noun, name, forPhase string, get PhaseGetter[T], timeout time.Duration) error {
	return runWait(ctx, streams, named(noun, name), forPhase, get, timeout)
}

func runWait[T any](ctx context.Context, streams *cli.IOStreams, subj subject, forPhase string, get PhaseGetter[T], timeout time.Duration) error {
	target := NormalizePhase(forPhase)
	switch target {
	case "":
		return cli.MisuseError(fmt.Sprintf("unsupported --for value %q", forPhase))
	case PhaseDeleted:
		if err := waitForGone(ctx, streams, subj, get, timeout); err != nil {
			return mapWaitErr(err, subj, "delete")
		}
	default:
		if _, err := waitForPhase(ctx, streams, subj, target, get, timeout); err != nil {
			return mapWaitErr(err, subj, "wait")
		}
	}
	fmt.Fprintf(streams.Out, "%s/%s %s\n", subj.noun, subj.name, strings.ToLower(target))
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
// *TerminalPhaseError: the API answered, and the resource has failed. A
// stalled attempt (client.ErrAttemptTimeout) is not an APIError, so it is
// transient.
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
	return mapWaitErr(err, named(noun, name), verb)
}

func mapWaitErr(err error, subj subject, verb string) error {
	if err == nil {
		return nil
	}
	check := "check status:  " + subj.command("get")
	resume := "resume wait:   " + subj.command("wait")
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
				fmt.Sprintf("stopped waiting; %s is still being %sd on Kupe Cloud", subj.phrase(), verb)).
				WithHint(check + "\n" + resume)
		case "wait":
			return cli.New(cli.ExitInterrupted,
				fmt.Sprintf("stopped waiting; %s may still be transitioning on Kupe Cloud", subj.phrase())).
				WithHint(check)
		default:
			return cli.New(cli.ExitInterrupted,
				fmt.Sprintf("stopped waiting; %s is still being created on Kupe Cloud", subj.phrase())).
				WithHint(strings.Join([]string{
					check,
					resume,
					"abandon:       " + subj.command("delete"),
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
