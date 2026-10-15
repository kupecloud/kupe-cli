package cmdutil

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
)

// stallingDatabaseAPI serves GET database requests from a real kupe-api
// client. The first stalls requests hang until the client gives up on them;
// later ones answer Running. stalls < 0 hangs every request.
func stallingDatabaseAPI(t *testing.T, stalls int64) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := hits.Add(1)
		if stalls < 0 || n <= stalls {
			select {
			case <-r.Context().Done():
			case <-time.After(10 * time.Second):
			}
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"orders-db","status":{"phase":"Running"}}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

// waitForDatabase runs a create-style wait for Running against srv with the
// given per-attempt HTTP timeout, and returns the wait's mapped CLI error.
func waitForDatabase(ctx context.Context, srv *httptest.Server, attemptTimeout, waitTimeout time.Duration) error {
	api := client.New(srv.URL, "acme", "kupe_test", "kupe-cli-test/0.0.0",
		client.WithHTTPClient(&http.Client{Timeout: attemptTimeout}))
	get := func(ctx context.Context) (*client.Database, string, error) {
		db, _, err := api.Databases().Get(ctx, "orders-db")
		if err != nil {
			return nil, "", err
		}
		return db, db.Status.Phase, nil
	}
	io, _, _ := cli.Test()
	_, err := WaitForPhase(ctx, io, "database", "orders-db", client.PhaseRunning, get, waitTimeout)
	return MapWaitErr(err, "database", "orders-db", "create")
}

// TestWait_StalledRequests separates a stalled HTTP attempt (the client's
// per-attempt timeout: kupe-api is slow, keep waiting) from the wait's own
// deadline (exit 8) and from Ctrl-C (exit 130).
func TestWait_StalledRequests(t *testing.T) {
	tests := []struct {
		name           string
		stalls         int64
		attemptTimeout time.Duration
		waitTimeout    time.Duration
		cancelAfter    time.Duration
		wantCode       int
		wantRequests   int64 // 0: not checked
	}{
		{"one stalled attempt is retried and the wait succeeds",
			1, 100 * time.Millisecond, 5 * time.Second, 0, cli.ExitOK, 2},
		// The client gives up after its 3 attempts; the next poll succeeds.
		{"stalls that outlast the client's retries ride the wait's grace",
			3, 100 * time.Millisecond, 5 * time.Second, 0, cli.ExitOK, 4},
		{"the wait's own deadline mid-request exits 8",
			-1, 3 * time.Second, 300 * time.Millisecond, 0, cli.ExitTimeout, 0},
		{"Ctrl-C mid-request exits 130",
			-1, 3 * time.Second, 5 * time.Second, 200 * time.Millisecond, cli.ExitInterrupted, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, hits := stallingDatabaseAPI(t, tt.stalls)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tt.cancelAfter > 0 {
				time.AfterFunc(tt.cancelAfter, cancel)
			}
			err := waitForDatabase(ctx, srv, tt.attemptTimeout, tt.waitTimeout)
			if code := cli.ExitCode(err); code != tt.wantCode {
				t.Fatalf("exit = %d; want %d (err: %v; requests: %d)", code, tt.wantCode, err, hits.Load())
			}
			if tt.wantRequests != 0 && hits.Load() != tt.wantRequests {
				t.Fatalf("requests = %d; want %d", hits.Load(), tt.wantRequests)
			}
		})
	}
}
