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

// errorResponse matches kupe-api's JSON error body shape ({"error": "..."}).
type errorResponse struct {
	Error string `json:"error"`
}

// APIError is the wrapper for any non-2xx response. RequestID is the
// X-Request-Id header value if the server emitted one — surface it in
// user-visible errors so support tickets can reference it.
type APIError struct {
	StatusCode int
	Message    string
	RequestID  string
	Method     string
	Path       string
}

func (e *APIError) Error() string {
	base := fmt.Sprintf("kupe api: %d %s", e.StatusCode, e.Message)
	if e.RequestID != "" {
		base += fmt.Sprintf(" (request-id: %s)", e.RequestID)
	}
	return base
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
