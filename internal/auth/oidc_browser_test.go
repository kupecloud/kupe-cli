package auth

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// TestBrowserFlowHappyPath simulates Authentik by:
//  1. Spinning up a fake /token endpoint that asserts PKCE + redirect_uri
//     and returns a fixed token response.
//  2. Stubbing the "browser" via PromptFn — instead of opening a real
//     browser the test parses the auth URL and immediately fires the
//     /callback request to the CLI's localhost listener.
//
// The flow then exchanges the code via the fake /token and returns a
// populated OIDCTokenSet.
func TestBrowserFlowHappyPath(t *testing.T) {
	t.Cleanup(SetBrowserOpenerForTest(func(string) error { return nil }))

	const wantCode = "test-auth-code"
	var capturedVerifier string

	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	mux.HandleFunc("/application/o/kupe-cli/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{
            "issuer":"%s/application/o/kupe-cli/",
            "authorization_endpoint":"%s/application/o/authorize/",
            "token_endpoint":"%s/application/o/token/"
        }`, srv.URL, srv.URL, srv.URL)
	})
	mux.HandleFunc("/application/o/token/", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if got := r.Form.Get("grant_type"); got != "authorization_code" {
			t.Errorf("grant_type=%q", got)
		}
		if got := r.Form.Get("code"); got != wantCode {
			t.Errorf("code=%q", got)
		}
		if got := r.Form.Get("client_id"); got != "kupe-cli" {
			t.Errorf("client_id=%q", got)
		}
		capturedVerifier = r.Form.Get("code_verifier")
		if capturedVerifier == "" {
			t.Error("code_verifier missing — PKCE not wired")
		}
		if !strings.HasPrefix(r.Form.Get("redirect_uri"), "http://localhost:") {
			t.Errorf("redirect_uri=%q", r.Form.Get("redirect_uri"))
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{"access_token":"acc","refresh_token":"ref","id_token":"id","expires_in":3600}`)
	})

	issuer := srv.URL + "/application/o/kupe-cli/"

	// PromptFn stands in for the user's browser — fire the callback
	// straight at the localhost listener with a matching state.
	prompt := func(authURL string) {
		u, err := url.Parse(authURL)
		if err != nil {
			t.Fatal(err)
		}
		q := u.Query()
		state := q.Get("state")
		if state == "" {
			t.Fatal("auth URL missing state")
		}
		if got := q.Get("code_challenge_method"); got != "S256" {
			t.Errorf("code_challenge_method=%q; want S256", got)
		}
		redirect := q.Get("redirect_uri")
		go func() {
			cb := redirect + "?code=" + wantCode + "&state=" + state
			resp, err := http.Get(cb) //nolint:gosec
			if err != nil {
				t.Errorf("callback request: %v", err)
				return
			}
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ts, err := BrowserFlow(ctx, prompt, issuer, "kupe-cli", "openid email offline_access")
	if err != nil {
		t.Fatalf("BrowserFlow: %v", err)
	}
	if ts.AccessToken != "acc" || ts.RefreshToken != "ref" || ts.IDToken != "id" {
		t.Fatalf("token set wrong: %+v", ts)
	}
	if capturedVerifier == "" {
		t.Fatal("token endpoint never received code_verifier")
	}
}

// TestBrowserFlowStateMismatch ensures CSRF-style state-substitution attacks
// are rejected: a callback with a different state must produce an error and
// no token exchange.
func TestBrowserFlowStateMismatch(t *testing.T) {
	t.Cleanup(SetBrowserOpenerForTest(func(string) error { return nil }))

	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	mux.HandleFunc("/application/o/kupe-cli/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{
            "issuer":"%s/application/o/kupe-cli/",
            "authorization_endpoint":"%s/application/o/authorize/",
            "token_endpoint":"%s/application/o/token/"
        }`, srv.URL, srv.URL, srv.URL)
	})
	mux.HandleFunc("/application/o/token/", func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("/token must not be called when state mismatches")
	})

	issuer := srv.URL + "/application/o/kupe-cli/"

	prompt := func(authURL string) {
		u, err := url.Parse(authURL)
		if err != nil {
			t.Fatal(err)
		}
		redirect := u.Query().Get("redirect_uri")
		go func() {
			cb := redirect + "?code=any&state=wrong-state"
			resp, err := http.Get(cb) //nolint:gosec
			if err == nil {
				_ = resp.Body.Close()
			}
		}()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_, err := BrowserFlow(ctx, prompt, issuer, "kupe-cli", "openid")
	if err == nil {
		t.Fatal("expected error on state mismatch")
	}
	if !strings.Contains(err.Error(), "state mismatch") {
		t.Fatalf("expected state mismatch error; got %v", err)
	}
}
