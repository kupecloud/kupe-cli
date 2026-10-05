package update

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func releaseServer(t *testing.T, tag string, calls *int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls != nil {
			*calls++
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"tag_name": tag})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestNotice_NewerVersionAvailable(t *testing.T) {
	srv := releaseServer(t, "v1.3.0", nil)
	c := &Checker{
		URL:       srv.URL,
		CachePath: filepath.Join(t.TempDir(), "update-check.json"),
	}

	got := c.Notice(context.Background(), "1.2.0")

	if !strings.Contains(got, "v1.3.0") || !strings.Contains(got, "1.2.0") {
		t.Fatalf("Notice() = %q, want it to name both versions", got)
	}
	if !strings.Contains(got, docsURL) {
		t.Errorf("Notice() = %q, want it to point at the upgrade instructions", got)
	}
}

func TestNotice_SilentWhenCurrent(t *testing.T) {
	srv := releaseServer(t, "v1.2.0", nil)
	c := &Checker{URL: srv.URL, CachePath: filepath.Join(t.TempDir(), "update-check.json")}

	for _, current := range []string{"1.2.0", "v1.2.0", "1.3.0"} {
		if got := c.Notice(context.Background(), current); got != "" {
			t.Errorf("Notice(%q) = %q, want no notice", current, got)
		}
	}
}

// A local build must never be told it is out of date, and must not make a
// request to find that out.
func TestNotice_DevBuildAsksNothing(t *testing.T) {
	calls := 0
	srv := releaseServer(t, "v1.3.0", &calls)
	c := &Checker{URL: srv.URL, CachePath: filepath.Join(t.TempDir(), "update-check.json")}

	for _, current := range []string{"dev", "", "1.2", "not-a-version"} {
		if got := c.Notice(context.Background(), current); got != "" {
			t.Errorf("Notice(%q) = %q, want no notice", current, got)
		}
	}
	if calls != 0 {
		t.Errorf("made %d HTTP calls for non-release versions, want 0", calls)
	}
}

// The failure modes that matter: every one of them must produce no notice and
// no error, because this runs after a command the user cares about.
func TestNotice_NetworkFailuresAreSilent(t *testing.T) {
	t.Run("connection refused", func(t *testing.T) {
		c := &Checker{
			// Port 0 on the loopback is not listening.
			URL:       "http://127.0.0.1:0/releases/latest",
			CachePath: filepath.Join(t.TempDir(), "update-check.json"),
		}
		if got := c.Notice(context.Background(), "1.2.0"); got != "" {
			t.Errorf("Notice() = %q, want silence", got)
		}
	})

	t.Run("http error status", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusForbidden)
		}))
		defer srv.Close()
		c := &Checker{URL: srv.URL, CachePath: filepath.Join(t.TempDir(), "update-check.json")}
		if got := c.Notice(context.Background(), "1.2.0"); got != "" {
			t.Errorf("Notice() = %q, want silence", got)
		}
	})

	t.Run("garbage body", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("<html>rate limited</html>"))
		}))
		defer srv.Close()
		c := &Checker{URL: srv.URL, CachePath: filepath.Join(t.TempDir(), "update-check.json")}
		if got := c.Notice(context.Background(), "1.2.0"); got != "" {
			t.Errorf("Notice() = %q, want silence", got)
		}
	})

	t.Run("no cache path", func(t *testing.T) {
		srv := releaseServer(t, "v1.3.0", nil)
		c := &Checker{URL: srv.URL} // caching disabled
		if got := c.Notice(context.Background(), "1.2.0"); !strings.Contains(got, "v1.3.0") {
			t.Errorf("Notice() = %q, want the check to still work without a cache", got)
		}
	})
}

func TestNotice_UsesCacheWithinTheInterval(t *testing.T) {
	calls := 0
	srv := releaseServer(t, "v1.3.0", &calls)
	path := filepath.Join(t.TempDir(), "update-check.json")
	now := time.Now()
	c := &Checker{URL: srv.URL, CachePath: path, Now: func() time.Time { return now }}

	if got := c.Notice(context.Background(), "1.2.0"); !strings.Contains(got, "v1.3.0") {
		t.Fatalf("first Notice() = %q, want the new version", got)
	}
	// Second call, 23 hours later: answered from cache.
	now = now.Add(23 * time.Hour)
	if got := c.Notice(context.Background(), "1.2.0"); !strings.Contains(got, "v1.3.0") {
		t.Fatalf("cached Notice() = %q, want the new version", got)
	}
	if calls != 1 {
		t.Errorf("made %d HTTP calls, want 1 — the cache should have answered the second", calls)
	}

	// Past the interval: asks again.
	now = now.Add(2 * time.Hour)
	c.Notice(context.Background(), "1.2.0")
	if calls != 2 {
		t.Errorf("made %d HTTP calls after the interval elapsed, want 2", calls)
	}
}

// An offline machine must back off to once a day too, otherwise every command
// pays the timeout.
func TestNotice_FailedCheckIsAlsoRateLimited(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	now := time.Now()
	c := &Checker{
		URL:       srv.URL,
		CachePath: filepath.Join(t.TempDir(), "update-check.json"),
		Now:       func() time.Time { return now },
	}

	c.Notice(context.Background(), "1.2.0")
	c.Notice(context.Background(), "1.2.0")

	if calls != 1 {
		t.Errorf("made %d HTTP calls, want 1 — a failed check must still be recorded", calls)
	}
}

func TestNotice_CorruptCacheIsIgnored(t *testing.T) {
	path := filepath.Join(t.TempDir(), "update-check.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	srv := releaseServer(t, "v1.3.0", nil)
	c := &Checker{URL: srv.URL, CachePath: path}

	if got := c.Notice(context.Background(), "1.2.0"); !strings.Contains(got, "v1.3.0") {
		t.Errorf("Notice() = %q, want a corrupt cache to be re-fetched, not fatal", got)
	}
}

func TestIsNewer(t *testing.T) {
	tests := []struct {
		latest, current string
		want            bool
	}{
		{"v1.3.0", "1.2.0", true},
		{"1.2.1", "1.2.0", true},
		{"2.0.0", "1.99.99", true},
		{"1.2.0", "1.2.0", false},
		{"1.2.0", "1.3.0", false},
		// Double-digit components must not compare as strings ("10" < "9").
		{"1.10.0", "1.9.0", true},
		{"1.9.0", "1.10.0", false},
	}
	for _, tt := range tests {
		if got := isNewer(tt.latest, tt.current); got != tt.want {
			t.Errorf("isNewer(%q, %q) = %v, want %v", tt.latest, tt.current, got, tt.want)
		}
	}
}

func TestDefaultCachePath(t *testing.T) {
	got := DefaultCachePath(filepath.Join("/home/u", ".config", "kupe", "config.yaml"))
	want := filepath.Join("/home/u", ".config", "kupe", "update-check.json")
	if got != want {
		t.Errorf("DefaultCachePath() = %q, want %q", got, want)
	}
	if got := DefaultCachePath(""); got != "" {
		t.Errorf("DefaultCachePath(\"\") = %q, want \"\"", got)
	}
}
