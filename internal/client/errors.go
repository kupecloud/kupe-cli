// Package client is the HTTP client for kupe-api. Commands depend on the
// Interface type (see interface.go); production wires in a *Client via the
// Factory; tests inject a clienttest.Fake.
//
// Error shape: every non-2xx response surfaces as an *APIError carrying the
// HTTP status, server-side message, request-id (when present), and the
// method+path that failed. Use the Is* helpers to classify — never string-
// match on Error().
package client

import (
	"errors"
	"fmt"
	"net/http"
)

// errorEnvelope matches both kupe-api error body shapes:
//
//  1. Legacy:     {"error": "human message"}
//  2. Structured: {"code": "HA_DISABLE_UNSUPPORTED", "severity": "error",
//     "message": "...", "field": "spec.highAvailability", "error": "..."}
//
// kupe-api emits the structured form only for canonical error codes; other
// 4xx responses still use the legacy shape. The duplicated `error` field on
// the structured form lets clients that only read Message keep working. We
// prefer Message when present (newer field) and fall back to Error.
type errorEnvelope struct {
	Code     string `json:"code,omitempty"`
	Severity string `json:"severity,omitempty"`
	Message  string `json:"message,omitempty"`
	Field    string `json:"field,omitempty"`
	Error    string `json:"error,omitempty"`
}

// APIError is the wrapper for any non-2xx response. RequestID is the
// X-Request-Id header value if the server emitted one — surface it in
// user-visible errors so support tickets can reference it.
//
// Code/Severity/Field are populated only when kupe-api returns the
// structured canonical envelope (HA_DISABLE_UNSUPPORTED,
// CLUSTER_DEDICATED_UNSUPPORTED, etc.). Callers that need to branch on a
// specific advisory should check Code rather than string-matching Message.
type APIError struct {
	StatusCode int
	Message    string
	RequestID  string
	Method     string
	Path       string
	// Code is the canonical error code (e.g. "HA_DISABLE_UNSUPPORTED"),
	// empty for legacy/non-canonical responses.
	Code string
	// Severity is "error" or "warning" — empty when Code is empty.
	Severity string
	// Field is the dotted spec path the error applies to
	// (e.g. "spec.highAvailability"). Empty when Code is empty.
	Field string
}

// Error renders a user-facing message. We deliberately drop the "kupe api:"
// prefix and the bare status code — they add no information that the message
// itself doesn't already carry. Status codes are exposed via Is* helpers
// for code that needs to branch on them.
//
// 5xx responses prepend a class word ("internal server error" / "service
// unavailable") because the server message alone tends to be terse and
// unactionable; the request-id stays attached so users can quote it on a
// support ticket. 4xx messages stand on their own.
func (e *APIError) Error() string {
	msg := e.Message
	switch {
	case e.StatusCode >= 500 && e.StatusCode < 600:
		label := "internal server error"
		if e.StatusCode == http.StatusServiceUnavailable {
			label = "service unavailable"
		}
		if msg == "" {
			msg = label
		} else {
			msg = label + ": " + msg
		}
	case msg == "":
		msg = http.StatusText(e.StatusCode)
	}
	if e.RequestID != "" {
		msg += fmt.Sprintf(" (request-id: %s)", e.RequestID)
	}
	return msg
}

// Is lets errors.Is recognise APIError regardless of pointer identity when
// the caller only cares about "is this an APIError". Matching on status code
// goes through the IsXxx helpers below.
func (e *APIError) Is(target error) bool {
	_, ok := target.(*APIError)
	return ok
}

// asAPI pulls an *APIError out of any error chain. Returns nil if err isn't
// or doesn't wrap one.
func asAPI(err error) *APIError {
	var e *APIError
	if errors.As(err, &e) {
		return e
	}
	return nil
}

// IsUnauthorized reports whether err is a 401 from kupe-api.
func IsUnauthorized(err error) bool {
	e := asAPI(err)
	return e != nil && e.StatusCode == http.StatusUnauthorized
}

// IsForbidden reports whether err is a 403 from kupe-api.
func IsForbidden(err error) bool {
	e := asAPI(err)
	return e != nil && e.StatusCode == http.StatusForbidden
}

// IsNotFound reports whether err is a 404 from kupe-api.
func IsNotFound(err error) bool {
	e := asAPI(err)
	return e != nil && e.StatusCode == http.StatusNotFound
}

// IsValidation reports whether err is a 400 from kupe-api.
func IsValidation(err error) bool {
	e := asAPI(err)
	return e != nil && e.StatusCode == http.StatusBadRequest
}

// IsConflict reports whether err is a 409 from kupe-api.
func IsConflict(err error) bool {
	e := asAPI(err)
	return e != nil && e.StatusCode == http.StatusConflict
}

// IsPreconditionFailed reports whether err is a 412 (ETag mismatch).
func IsPreconditionFailed(err error) bool {
	e := asAPI(err)
	return e != nil && e.StatusCode == http.StatusPreconditionFailed
}

// IsRateLimited reports whether err is a 429.
func IsRateLimited(err error) bool {
	e := asAPI(err)
	return e != nil && e.StatusCode == http.StatusTooManyRequests
}

// IsUnavailable reports whether err is a 503.
func IsUnavailable(err error) bool {
	e := asAPI(err)
	return e != nil && e.StatusCode == http.StatusServiceUnavailable
}
