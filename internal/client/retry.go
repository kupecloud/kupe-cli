package client

import (
	"context"
	"errors"
	"math/rand/v2"
	"net"
	"net/http"
	"time"
)

// retryPolicy is the CLI's exponential-backoff policy for idempotent
// requests. Safe-by-default: only GET/DELETE/HEAD retry; POST/PATCH/PUT
// fail fast to avoid duplicate resource creation.
type retryPolicy struct {
	maxAttempts    int
	initialBackoff time.Duration
	maxBackoff     time.Duration
}

// defaultRetryPolicy: 3 attempts total, 100ms → 400ms → 1600ms with equal
// jitter. Documented in docs/api-client.md.
var defaultRetryPolicy = retryPolicy{
	maxAttempts:    3,
	initialBackoff: 100 * time.Millisecond,
	maxBackoff:     1600 * time.Millisecond,
}

// shouldRetry reports whether a request should be retried given the HTTP
// response and/or transport error.
func (p retryPolicy) shouldRetry(method string, resp *http.Response, err error) bool {
	// Never retry writes — their bodies might have already committed
	// server-side even if the response didn't make it back to us.
	switch method {
	case http.MethodPost, http.MethodPatch, http.MethodPut:
		return false
	}

	// Transport errors: retry on timeout / temporary / connection errors.
	// Context cancellation is not retried — caller meant to stop.
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return false
		}
		var netErr net.Error
		if errors.As(err, &netErr) {
			return true
		}
		return true
	}

	// Status-code based retry. 429 is handled separately in doWithRetry
	// with Retry-After semantics, so we don't retry it here.
	if resp != nil {
		switch resp.StatusCode {
		case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
			return true
		}
	}
	return false
}

// backoff computes the wait before attempt N (zero-indexed) using equal
// jitter: wait = rand(base/2, base) where base doubles each attempt up to
// maxBackoff.
func (p retryPolicy) backoff(attempt int) time.Duration {
	base := p.initialBackoff
	for i := 0; i < attempt; i++ {
		base *= 2
		if base >= p.maxBackoff {
			base = p.maxBackoff
			break
		}
	}
	// Equal jitter: at least base/2, up to base.
	half := base / 2
	return half + time.Duration(rand.Int64N(int64(half+1))) //#nosec G404 -- jitter is not security-sensitive
}

// sleepCtx waits for d or until ctx is cancelled. Returns ctx.Err() on
// cancellation, nil otherwise.
func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
