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
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
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
	// body is the raw response body, kept so endpoint-specific parsers can
	// pull structured detail (e.g. the tenant list on signup's 409) without
	// the generic envelope having to know every shape. Never rendered.
	body []byte
}

// Body returns the raw response body of the failed request (may be empty).
func (e *APIError) Body() []byte { return e.body }

// ErrorCode returns the canonical error code carried by err's APIError,
// lower-cased for case-insensitive comparison, or "" when err is not an
// APIError or the server sent a legacy envelope without a code.
func ErrorCode(err error) string {
	e := asAPI(err)
	if e == nil {
		return ""
	}
	return strings.ToLower(e.Code)
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

// StatusCode is the HTTP status of err's APIError, 0 when err is not one.
func StatusCode(err error) int {
	if e := asAPI(err); e != nil {
		return e.StatusCode
	}
	return 0
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

// IsServerError reports whether err is any 5xx from kupe-api (500-599).
func IsServerError(err error) bool {
	e := asAPI(err)
	return e != nil && e.StatusCode >= 500 && e.StatusCode <= 599
}

// CodeCreationPaused is the 400 kupe-api returns from POST /clusters and
// POST /databases while the platform pauses creation.
const CodeCreationPaused = "creation_paused"

// Codes kupe-api returns for refusals every managed service can get, from
// its own checks or mapped from the operator's admission denials (kupe-api
// internal/errors/errors.go). Compare via ErrorCode.
const (
	// CodeTerminating: 409 for a change to a cluster or database that is
	// being deleted, a rotation of one, or a binding of a database that is.
	CodeTerminating = "terminating"
	// CodeTenantTerminating: 409 for creating or growing a service while
	// the tenant is being deleted.
	CodeTenantTerminating = "tenant_terminating"
	// CodeTenantSuspended: 409 for creating or growing a service while the
	// tenant is suspended.
	CodeTenantSuspended = "tenant_suspended"
	// CodePaymentSetupRequired: 409 for creating or growing a service
	// before the tenant has added a payment method.
	CodePaymentSetupRequired = "payment_setup_required"
	// CodePlanLimitReached: 400 for creating a cluster or database the
	// tenant's plan has no room for.
	CodePlanLimitReached = "plan_limit_reached"
)

// creationPausedText identifies the same refusal from servers that predate
// CodeCreationPaused. Lower-case: the API and webhook capitalise it
// differently.
const creationPausedText = "creation is temporarily paused"

// IsCreationPaused reports whether err is the platform refusing to create a
// cluster or database for now. The code is authoritative; the message match
// only covers older servers that send a 400 without it.
func IsCreationPaused(err error) bool {
	if ErrorCode(err) == CodeCreationPaused {
		return true
	}
	e := asAPI(err)
	return e != nil && e.StatusCode == http.StatusBadRequest &&
		strings.Contains(strings.ToLower(e.Message), creationPausedText)
}

// IsUnknownRoute reports whether err is a 404 that kupe-api's handlers did
// not answer: they always send a JSON error body, so a 404 whose body is
// something else (Go's "404 page not found") means the route does not exist
// on this kupe-api, a release that predates the endpoint.
func IsUnknownRoute(err error) bool {
	e := asAPI(err)
	return e != nil && e.StatusCode == http.StatusNotFound && len(e.body) > 0 && !json.Valid(e.body)
}

// IsAPIError reports whether err is (or wraps) an *APIError — i.e. the server
// returned a structured non-2xx response, as opposed to a transport/network
// failure that never reached an HTTP status.
func IsAPIError(err error) bool {
	return asAPI(err) != nil
}
