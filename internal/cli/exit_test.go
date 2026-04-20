package cli

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestExitCode(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"nil", nil, 0},
		{"plain", errors.New("plain"), ExitGeneral},
		{"auth", AuthError("nope"), ExitAuth},
		{"not-found", NotFoundError("nope"), ExitNotFound},
		{"conflict", ConflictError("nope"), ExitConflict},
		{"misuse", MisuseError("nope"), ExitMisuse},
		{"timeout", TimeoutError("nope"), ExitTimeout},
		{"wrapped auth", fmt.Errorf("context: %w", AuthError("nope")), ExitAuth},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExitCode(tt.err); got != tt.want {
				t.Fatalf("ExitCode(%v) = %d; want %d", tt.err, got, tt.want)
			}
		})
	}
}

// Fix 4 regression: context errors must map to exit codes so automation
// can distinguish user interrupt (130) from timeout (8) from general
// failure (1). Previously both were 1 and CI was blind to intent.
func TestExitCodeMapsContextErrors(t *testing.T) {
	if got := ExitCode(context.Canceled); got != ExitInterrupted {
		t.Errorf("context.Canceled → %d; want %d", got, ExitInterrupted)
	}
	if got := ExitCode(context.DeadlineExceeded); got != ExitTimeout {
		t.Errorf("context.DeadlineExceeded → %d; want %d", got, ExitTimeout)
	}
	// Wrapped forms (e.g., from client.go's "executing request: %w" path)
	// must map correctly too.
	wrapped := fmt.Errorf("executing request: %w", context.Canceled)
	if got := ExitCode(wrapped); got != ExitInterrupted {
		t.Errorf("wrapped Canceled → %d; want %d", got, ExitInterrupted)
	}
	wrappedTO := fmt.Errorf("reading response: %w", context.DeadlineExceeded)
	if got := ExitCode(wrappedTO); got != ExitTimeout {
		t.Errorf("wrapped DeadlineExceeded → %d; want %d", got, ExitTimeout)
	}
}

func TestWrapUnwrap(t *testing.T) {
	inner := errors.New("inner")
	wrapped := Wrap(ExitGeneral, "ctx", inner)
	if !errors.Is(wrapped, inner) {
		t.Fatal("Unwrap did not surface inner error")
	}
}

func TestWithHint(t *testing.T) {
	base := NotFoundError("thing not found")
	hinted := base.WithHint("run kupe cluster list")
	if hinted.Hint != "run kupe cluster list" {
		t.Fatal("hint not attached")
	}
	if base.Hint != "" {
		t.Fatal("WithHint must not mutate the receiver")
	}
	if hinted.Code != ExitNotFound {
		t.Fatal("WithHint lost the code")
	}
}
