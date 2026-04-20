package cli

import (
	"context"
	"errors"
	"fmt"

	"github.com/kupecloud/kupe-cli/internal/client"
)

// Exit codes. Stable across versions, documented in docs/design.md. Client
// errors from internal/client map to these via the Is* helpers once the
// client lands (Phase 2); until then, commands return typed Error values
// directly via the helpers below.
const (
	ExitOK          = 0
	ExitGeneral     = 1
	ExitMisuse      = 2
	ExitAuth        = 3
	ExitNotFound    = 4
	ExitConflict    = 5
	ExitRateLimited = 6
	ExitUnavailable = 7
	ExitTimeout     = 8
	ExitInterrupted = 130
)

// Error is the CLI's typed error. Commands construct these via the helpers
// below; ExitCode unwraps them to pick the right process exit code. A Hint
// is an optional single-line actionable suggestion, shown below the main
// error message on stderr.
type Error struct {
	Code int
	Msg  string
	Hint string
	Err  error
}

// New constructs an Error with the given code and message.
func New(code int, msg string) *Error { return &Error{Code: code, Msg: msg} }

// Wrap returns a new Error that wraps err with the given code and message.
// The wrapped error is surfaced via errors.Unwrap.
func Wrap(code int, msg string, err error) *Error {
	return &Error{Code: code, Msg: msg, Err: err}
}

// WithHint returns a copy of e with the given hint attached.
func (e *Error) WithHint(hint string) *Error {
	out := *e
	out.Hint = hint
	return &out
}

func (e *Error) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Msg, e.Err)
	}
	return e.Msg
}

// Unwrap allows errors.Is / errors.As to see through the wrapper.
func (e *Error) Unwrap() error { return e.Err }

// AuthError returns an exit-3 Error. Used for missing tokens, invalid creds,
// 401/403 responses.
func AuthError(msg string) *Error { return New(ExitAuth, msg) }

// NotFoundError returns an exit-4 Error.
func NotFoundError(msg string) *Error { return New(ExitNotFound, msg) }

// ConflictError returns an exit-5 Error. Used for 409 (already exists) and
// 412 (ETag mismatch) once the HTTP client lands.
func ConflictError(msg string) *Error { return New(ExitConflict, msg) }

// MisuseError returns an exit-2 Error. Used for bad flag combinations and
// missing required arguments that Cobra itself didn't catch.
func MisuseError(msg string) *Error { return New(ExitMisuse, msg) }

// TimeoutError returns an exit-8 Error, for --wait-timeout exhaustion.
func TimeoutError(msg string) *Error { return New(ExitTimeout, msg) }

// ExitCode maps an error from a command's RunE to a process exit code.
// Nil → 0; a *cli.Error → its Code; a context.Canceled → 130 (user
// interrupt via SIGINT/SIGTERM, matches Unix convention); a
// context.DeadlineExceeded → 8 (timeout); a *client.APIError →
// status-class mapping (401/403 → 3, 404 → 4, 409/412 → 5, 429 → 6,
// 503 → 7); anything else → 1.
//
// Context errors are handled BEFORE client errors: a Ctrl+C during an
// HTTP request surfaces as context.Canceled wrapped by the client's
// "executing request" error, and we want the user-facing exit code to
// reflect intent (interrupt) not incident class (general failure).
func ExitCode(err error) int {
	if err == nil {
		return ExitOK
	}
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	switch {
	case errors.Is(err, context.Canceled):
		return ExitInterrupted
	case errors.Is(err, context.DeadlineExceeded):
		return ExitTimeout
	case client.IsUnauthorized(err), client.IsForbidden(err):
		return ExitAuth
	case client.IsNotFound(err):
		return ExitNotFound
	case client.IsConflict(err), client.IsPreconditionFailed(err):
		return ExitConflict
	case client.IsRateLimited(err):
		return ExitRateLimited
	case client.IsUnavailable(err):
		return ExitUnavailable
	case client.IsValidation(err):
		return ExitMisuse
	}
	return ExitGeneral
}
