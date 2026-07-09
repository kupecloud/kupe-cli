package cli

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

// TestErrAuthRefreshTimeoutNotDeadline locks the C4 invariant: an OIDC-refresh
// timeout is identifiable as ErrAuthRefreshTimeout but MUST NOT satisfy
// errors.Is(err, context.DeadlineExceeded). Otherwise the wait spinner
// (classifySpinnerRunErr / runSpinner) would map a mid-wait IdP blip to
// ErrWaitTimeout (exit 8), and ExitCode would map the bare error to
// ExitTimeout — both misreporting the wait's own deadline.
func TestErrAuthRefreshTimeoutNotDeadline(t *testing.T) {
	// As surfaced by the token source (fmt.Errorf "%w after 30s") and then
	// re-wrapped by the client ("resolving auth token: %w").
	err := fmt.Errorf("resolving auth token: %w",
		fmt.Errorf("%w after %s", ErrAuthRefreshTimeout, authRefreshTimeout))

	if !errors.Is(err, ErrAuthRefreshTimeout) {
		t.Fatalf("want errors.Is(err, ErrAuthRefreshTimeout) = true; got false (%v)", err)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("auth-refresh timeout must NOT wrap context.DeadlineExceeded (would map to ErrWaitTimeout / exit 8): %v", err)
	}
	if got := ExitCode(err); got == ExitTimeout {
		t.Fatalf("auth-refresh timeout must not map to ExitTimeout(%d) — that is the wait-deadline code", ExitTimeout)
	}
}
