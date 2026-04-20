package client

import (
	"net/http"
	"testing"
	"time"
)

func TestParseRetryAfterSeconds(t *testing.T) {
	now := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		in   string
		want time.Duration
	}{
		{"", 0},
		{"0", 0},
		{"-1", 0},
		{"5", 5 * time.Second},
		{"10", 10 * time.Second},
		{"999", maxRetryAfter}, // clamped
		{"not-a-number", 0},
	}
	for _, tt := range tests {
		if got := parseRetryAfter(tt.in, now); got != tt.want {
			t.Errorf("parseRetryAfter(%q) = %v; want %v", tt.in, got, tt.want)
		}
	}
}

func TestParseRetryAfterHTTPDate(t *testing.T) {
	now := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)

	// 15s in the future → 15s.
	future := now.Add(15 * time.Second).UTC().Format(http.TimeFormat)
	if d := parseRetryAfter(future, now); d < 14*time.Second || d > 15*time.Second {
		t.Errorf("future HTTP-date: got %v, want ~15s", d)
	}

	// Past timestamp → 0.
	past := now.Add(-1 * time.Minute).UTC().Format(http.TimeFormat)
	if d := parseRetryAfter(past, now); d != 0 {
		t.Errorf("past HTTP-date: got %v, want 0", d)
	}

	// Way in the future → clamped.
	waaayFuture := now.Add(1 * time.Hour).UTC().Format(http.TimeFormat)
	if d := parseRetryAfter(waaayFuture, now); d != maxRetryAfter {
		t.Errorf("clamped HTTP-date: got %v, want %v", d, maxRetryAfter)
	}
}
