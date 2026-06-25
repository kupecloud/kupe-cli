package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestOIDCTokenSetValid(t *testing.T) {
	cases := []struct {
		name string
		ts   OIDCTokenSet
		want bool
	}{
		{"empty", OIDCTokenSet{}, false},
		{"expired", OIDCTokenSet{AccessToken: "x", Expiry: time.Now().Add(-time.Minute)}, false},
		{"within skew", OIDCTokenSet{AccessToken: "x", Expiry: time.Now().Add(refreshSkew / 2)}, false},
		{"fresh", OIDCTokenSet{AccessToken: "x", Expiry: time.Now().Add(time.Hour)}, true},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.ts.Valid(); got != tt.want {
				t.Errorf("Valid() = %v; want %v", got, tt.want)
			}
		})
	}
}

func TestIsOIDCBlob(t *testing.T) {
	cases := map[string]bool{
		`{"access_token":"x"}`: true,
		"  {":                  true,
		"kupe_abc123":          false,
		"":                     false,
		"random":               false,
	}
	for in, want := range cases {
		if got := IsOIDCBlob(in); got != want {
			t.Errorf("IsOIDCBlob(%q) = %v; want %v", in, got, want)
		}
	}
}

func TestMarshalUnmarshalRoundTrip(t *testing.T) {
	want := OIDCTokenSet{
		AccessToken:  "access",
		RefreshToken: "refresh",
		IDToken:      "idtok-not-persisted",
		Expiry:       time.Now().Add(time.Hour).Truncate(time.Second).UTC(),
	}
	s, err := want.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if !IsOIDCBlob(s) {
		t.Fatalf("marshalled output should be detected as OIDC blob: %q", s)
	}
	if strings.Contains(s, "idtok-not-persisted") {
		t.Fatalf("id_token leaked into persisted blob: %q", s)
	}
	got, err := UnmarshalOIDC(s)
	if err != nil {
		t.Fatal(err)
	}
	if got.AccessToken != want.AccessToken || got.RefreshToken != want.RefreshToken || !got.Expiry.Equal(want.Expiry) {
		t.Fatalf("round-trip mismatch:\n got=%+v\nwant=%+v", got, want)
	}
	if got.IDToken != "" {
		t.Errorf("IDToken should not survive round-trip; got %q", got.IDToken)
	}
}

func TestEmailFromIDToken(t *testing.T) {
	mkJWT := func(claims map[string]any) string {
		hdr := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))
		body, _ := json.Marshal(claims)
		payload := base64.RawURLEncoding.EncodeToString(body)
		return hdr + "." + payload + ".sig"
	}
	if got := EmailFromIDToken(mkJWT(map[string]any{"email": "user@example.com"})); got != "user@example.com" {
		t.Fatalf("got %q", got)
	}
	if got := EmailFromIDToken("not-a-jwt"); got != "" {
		t.Fatalf("expected empty for malformed token, got %q", got)
	}
	if got := EmailFromIDToken(mkJWT(map[string]any{"sub": "no-email-claim"})); got != "" {
		t.Fatalf("expected empty for missing email, got %q", got)
	}
}

func TestDiscoverReturnsEndpoints(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/application/o/kupe-cli/.well-known/openid-configuration" {
			t.Errorf("unexpected discovery path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		// Issuer must equal the requested issuer (KC-9), so echo the server URL.
		fmt.Fprintf(w, `{
            "issuer": "%s/application/o/kupe-cli/",
            "authorization_endpoint": "%s/application/o/authorize/",
            "token_endpoint": "%s/application/o/token/",
            "jwks_uri": "%s/application/o/kupe-cli/jwks/"
        }`, srv.URL, srv.URL, srv.URL, srv.URL)
	}))
	defer srv.Close()

	d, err := Discover(context.Background(), srv.URL+"/application/o/kupe-cli/")
	if err != nil {
		t.Fatal(err)
	}
	if d.AuthorizationEndpoint != srv.URL+"/application/o/authorize/" {
		t.Errorf("AuthorizationEndpoint = %q", d.AuthorizationEndpoint)
	}
	if d.TokenEndpoint != srv.URL+"/application/o/token/" {
		t.Errorf("TokenEndpoint = %q", d.TokenEndpoint)
	}
}

// TestDiscoverRejectsIssuerMismatch covers the KC-9 guard: a discovery doc
// whose issuer doesn't match the requested issuer is rejected.
func TestDiscoverRejectsIssuerMismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{
            "issuer": "https://evil.example/application/o/kupe-cli/",
            "authorization_endpoint": "https://evil.example/application/o/authorize/",
            "token_endpoint": "https://evil.example/application/o/token/"
        }`)
	}))
	defer srv.Close()

	_, err := Discover(context.Background(), srv.URL+"/application/o/kupe-cli/")
	if err == nil || !strings.Contains(err.Error(), "does not match expected issuer") {
		t.Fatalf("want issuer-mismatch error, got %v", err)
	}
}

func TestDiscoverFailsOnMissingEndpoints(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{"issuer":"https://example/application/o/kupe-cli/"}`)
	}))
	defer srv.Close()

	_, err := Discover(context.Background(), srv.URL+"/application/o/kupe-cli/")
	if err == nil {
		t.Fatal("expected error on missing endpoints")
	}
}

func TestDiscoverFailsOnNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	_, err := Discover(context.Background(), srv.URL+"/application/o/kupe-cli/")
	if err == nil {
		t.Fatal("expected error on 404")
	}
}

// fakeAuthentik mounts a discovery doc at the standard well-known path
// and a token handler at /application/o/token/, simulating just enough
// of Authentik for Refresh's two HTTP calls.
func fakeAuthentik(t *testing.T, tokenHandler http.HandlerFunc) (issuer string, srv *httptest.Server) {
	t.Helper()
	mux := http.NewServeMux()
	srv = httptest.NewServer(mux)
	mux.HandleFunc("/application/o/kupe-cli/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{
            "issuer":"%s/application/o/kupe-cli/",
            "authorization_endpoint":"%s/application/o/authorize/",
            "token_endpoint":"%s/application/o/token/",
            "jwks_uri":"%s/application/o/kupe-cli/jwks/"
        }`, srv.URL, srv.URL, srv.URL, srv.URL)
	})
	mux.HandleFunc("/application/o/token/", tokenHandler)
	return srv.URL + "/application/o/kupe-cli/", srv
}

// TestRefreshSuccess simulates Authentik's discovery + /token endpoints
// returning a new access + refresh token. The CLI must persist the
// rotated refresh token if the server returns one.
func TestRefreshSuccess(t *testing.T) {
	issuer, srv := fakeAuthentik(t, func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if got := r.Form.Get("grant_type"); got != "refresh_token" {
			t.Errorf("grant_type = %q; want refresh_token", got)
		}
		if got := r.Form.Get("refresh_token"); got != "old-refresh" {
			t.Errorf("refresh_token = %q; want old-refresh", got)
		}
		if got := r.Form.Get("client_id"); got != "kupe-cli" {
			t.Errorf("client_id = %q; want kupe-cli", got)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{"access_token":"new-access","refresh_token":"new-refresh","expires_in":3600,"id_token":"new-id"}`)
	})
	defer srv.Close()

	current := OIDCTokenSet{
		AccessToken:  "expired-access",
		RefreshToken: "old-refresh",
		IDToken:      "old-id",
		Expiry:       time.Now().Add(-time.Minute),
	}
	got, err := Refresh(context.Background(), issuer, "kupe-cli", current)
	if err != nil {
		t.Fatal(err)
	}
	if got.AccessToken != "new-access" || got.RefreshToken != "new-refresh" || got.IDToken != "new-id" {
		t.Fatalf("unexpected token set: %+v", got)
	}
	if got.Expiry.Before(time.Now()) {
		t.Fatalf("expiry not in future: %v", got.Expiry)
	}
}

// TestRefreshKeepsExistingRefreshTokenIfNotRotated covers Authentik's
// behaviour when refresh_token rotation is disabled — the response omits
// refresh_token, so we keep the previous one.
func TestRefreshKeepsExistingRefreshTokenIfNotRotated(t *testing.T) {
	issuer, srv := fakeAuthentik(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{"access_token":"rotated-access","expires_in":3600}`)
	})
	defer srv.Close()

	got, err := Refresh(context.Background(), issuer, "kupe-cli",
		OIDCTokenSet{RefreshToken: "stable-refresh", IDToken: "stable-id", Expiry: time.Now().Add(-time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	if got.RefreshToken != "stable-refresh" {
		t.Fatalf("RefreshToken = %q; want stable-refresh", got.RefreshToken)
	}
	if got.IDToken != "stable-id" {
		t.Fatalf("IDToken = %q; want stable-id (no id_token in response)", got.IDToken)
	}
}

// TestRefreshInvalidGrantSurfacesErrRefreshFailed proves the refresh path
// returns the sentinel error, so the factory can clear the stored blob.
func TestRefreshInvalidGrantSurfacesErrRefreshFailed(t *testing.T) {
	issuer, srv := fakeAuthentik(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintln(w, `{"error":"invalid_grant","error_description":"Token expired"}`)
	})
	defer srv.Close()

	_, err := Refresh(context.Background(), issuer, "kupe-cli",
		OIDCTokenSet{RefreshToken: "old", Expiry: time.Now().Add(-time.Minute)})
	if !errors.Is(err, ErrRefreshFailed) {
		t.Fatalf("expected ErrRefreshFailed; got %v", err)
	}
	if !strings.Contains(err.Error(), "invalid_grant") {
		t.Fatalf("error should mention invalid_grant; got %v", err)
	}
}

func TestRefreshNoRefreshTokenIsErrRefreshFailed(t *testing.T) {
	_, err := Refresh(context.Background(), "https://example.invalid/", "kupe-cli", OIDCTokenSet{})
	if !errors.Is(err, ErrRefreshFailed) {
		t.Fatalf("expected ErrRefreshFailed; got %v", err)
	}
}
