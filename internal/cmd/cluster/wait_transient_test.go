package cluster

import (
	"context"
	"errors"
	"testing"

	"github.com/kupecloud/kupe-cli/internal/client"
)

func TestIsTransientWaitErr(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"503", &client.APIError{StatusCode: 503}, true},
		{"502", &client.APIError{StatusCode: 502}, true},
		{"500", &client.APIError{StatusCode: 500}, true},
		{"429", &client.APIError{StatusCode: 429}, true},
		{"404 terminal", &client.APIError{StatusCode: 404}, false},
		{"401 terminal", &client.APIError{StatusCode: 401}, false},
		{"403 terminal", &client.APIError{StatusCode: 403}, false},
		{"400 terminal", &client.APIError{StatusCode: 400}, false},
		{"409 terminal", &client.APIError{StatusCode: 409}, false},
		{"network error transient", errors.New("dial tcp: connection refused"), true},
		{"context canceled not transient", context.Canceled, false},
		{"deadline not transient", context.DeadlineExceeded, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isTransientWaitErr(c.err); got != c.want {
				t.Fatalf("isTransientWaitErr(%v) = %v; want %v", c.err, got, c.want)
			}
		})
	}
}

// TestTolerateTransientSwallowsThenSurfaces proves consecutive transient
// errors are swallowed (no error returned, last phase reported) and that a
// terminal error passes straight through.
func TestTolerateTransientSwallowsTransient(t *testing.T) {
	calls := 0
	base := func(context.Context) (string, bool, error) {
		calls++
		switch calls {
		case 1:
			return "Provisioning", false, nil // establishes lastPhase
		case 2:
			return "", false, &client.APIError{StatusCode: 503} // transient
		default:
			return "Running", true, nil
		}
	}
	wrapped := tolerateTransient(base)

	if phase, done, err := wrapped(context.Background()); err != nil || done || phase != "Provisioning" {
		t.Fatalf("call 1: (%q,%v,%v)", phase, done, err)
	}
	// Transient error: swallowed, last phase reported, not done.
	if phase, done, err := wrapped(context.Background()); err != nil || done || phase != "Provisioning" {
		t.Fatalf("call 2 (transient): want (Provisioning,false,nil), got (%q,%v,%v)", phase, done, err)
	}
	if phase, done, err := wrapped(context.Background()); err != nil || !done || phase != "Running" {
		t.Fatalf("call 3: want (Running,true,nil), got (%q,%v,%v)", phase, done, err)
	}
}

func TestTolerateTransientPassesTerminalThrough(t *testing.T) {
	terminal := &client.APIError{StatusCode: 404}
	wrapped := tolerateTransient(func(context.Context) (string, bool, error) {
		return "", false, terminal
	})
	_, _, err := wrapped(context.Background())
	if !errors.Is(err, terminal) {
		t.Fatalf("terminal error should pass through; got %v", err)
	}
}
