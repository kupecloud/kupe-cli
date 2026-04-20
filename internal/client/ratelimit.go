package client

import (
	"net/http"
	"strconv"
	"time"
)

// maxRetryAfter caps the Retry-After wait. A hostile server could otherwise
// hang the CLI indefinitely.
const maxRetryAfter = 30 * time.Second

// defaultRetryAfter applies when the server returns 429 without a
// Retry-After header. Matches the value many tools (gh, helm) use.
const defaultRetryAfter = 5 * time.Second

// parseRetryAfter interprets an HTTP Retry-After value. Accepts both forms
// defined by RFC 7231 §7.1.3: delta-seconds (integer) and HTTP-date. Returns
// zero if the header is absent or unparsable; callers should fall back to
// defaultRetryAfter in that case. The returned duration is clamped to
// maxRetryAfter.
func parseRetryAfter(header string, now time.Time) time.Duration {
	if header == "" {
		return 0
	}
	// delta-seconds form.
	if secs, err := strconv.Atoi(header); err == nil {
		if secs < 0 {
			return 0
		}
		d := time.Duration(secs) * time.Second
		if d > maxRetryAfter {
			d = maxRetryAfter
		}
		return d
	}
	// HTTP-date form. Subtract against the caller-supplied `now` so the
	// function is deterministic under test.
	if t, err := http.ParseTime(header); err == nil {
		if !t.After(now) {
			return 0
		}
		d := t.Sub(now)
		if d > maxRetryAfter {
			d = maxRetryAfter
		}
		return d
	}
	return 0
}
