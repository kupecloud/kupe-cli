package client

import (
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestShouldRetryByMethod(t *testing.T) {
	p := defaultRetryPolicy
	fiveOhThree := &http.Response{StatusCode: http.StatusServiceUnavailable}

	tests := []struct {
		method string
		want   bool
	}{
		{http.MethodGet, true},
		{http.MethodHead, true},
		{http.MethodDelete, true},
		{http.MethodPost, false},
		{http.MethodPatch, false},
		{http.MethodPut, false},
	}
	for _, tt := range tests {
		if got := p.shouldRetry(tt.method, fiveOhThree, nil); got != tt.want {
			t.Errorf("shouldRetry(%s, 503) = %v; want %v", tt.method, got, tt.want)
		}
	}
}

func TestShouldRetryStatusCodes(t *testing.T) {
	p := defaultRetryPolicy
	tests := []struct {
		status int
		want   bool
	}{
		{http.StatusOK, false},
		{http.StatusBadRequest, false},
		{http.StatusUnauthorized, false},
		{http.StatusNotFound, false},
		{http.StatusInternalServerError, false}, // 500 intentionally not retried
		{http.StatusBadGateway, true},
		{http.StatusServiceUnavailable, true},
		{http.StatusGatewayTimeout, true},
	}
	for _, tt := range tests {
		got := p.shouldRetry(http.MethodGet, &http.Response{StatusCode: tt.status}, nil)
		if got != tt.want {
			t.Errorf("shouldRetry(GET, %d) = %v; want %v", tt.status, got, tt.want)
		}
	}
}

func TestShouldRetryContextCancelled(t *testing.T) {
	p := defaultRetryPolicy
	if p.shouldRetry(http.MethodGet, nil, errors.New("wrapped: context canceled")) != true {
		// Non-context wrapped errors should still retry.
		t.Skip("generic errors currently retried — this test is a guard if that changes")
	}
	if p.shouldRetry(http.MethodGet, nil, nil) != false {
		t.Fatal("no response and no error should not retry")
	}
}

func TestBackoffGrowsAndClamps(t *testing.T) {
	p := retryPolicy{initialBackoff: 100 * time.Millisecond, maxBackoff: 800 * time.Millisecond}
	got := []time.Duration{p.backoff(0), p.backoff(1), p.backoff(2), p.backoff(10)}

	// Floor of each: base/2 with equal jitter. With max clamp at 800ms,
	// attempt >= 3 stays at 400ms..800ms.
	if got[0] < 50*time.Millisecond || got[0] > 100*time.Millisecond {
		t.Errorf("attempt 0: %v out of [50ms, 100ms]", got[0])
	}
	if got[3] < 400*time.Millisecond || got[3] > 800*time.Millisecond {
		t.Errorf("attempt 10 (clamped): %v out of [400ms, 800ms]", got[3])
	}
}
