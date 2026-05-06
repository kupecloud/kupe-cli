package auth

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// discoveryWithDeviceEndpoint returns a discovery handler advertising all
// three endpoints the device flow needs. Shared by the happy-path and
// polling tests.
func discoveryWithDeviceEndpoint(t *testing.T, mux *http.ServeMux, srv *httptest.Server) {
	t.Helper()
	mux.HandleFunc("/application/o/kupe-cli/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{
            "issuer":"%s/application/o/kupe-cli/",
            "authorization_endpoint":"%s/application/o/authorize/",
            "token_endpoint":"%s/application/o/token/",
            "device_authorization_endpoint":"%s/application/o/device/"
        }`, srv.URL, srv.URL, srv.URL, srv.URL)
	})
}

// TestDeviceFlowHappyPath simulates Authentik returning a device code and
// then approving on the very first /token poll. Verifies the prompt is
// invoked with the right values and the token set is populated.
func TestDeviceFlowHappyPath(t *testing.T) {
	t.Cleanup(SetBrowserOpenerForTest(func(string) error { return nil }))

	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	discoveryWithDeviceEndpoint(t, mux, srv)

	mux.HandleFunc("/application/o/device/", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if got := r.Form.Get("client_id"); got != "kupe-cli" {
			t.Errorf("device-auth client_id=%q", got)
		}
		if got := r.Form.Get("scope"); !strings.Contains(got, "offline_access") {
			t.Errorf("device-auth scope=%q (want offline_access)", got)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{
			"device_code":"dev-123",
			"user_code":"WDJB-MJHT",
			"verification_uri":"%s/device",
			"verification_uri_complete":"%s/device?code=WDJB-MJHT",
			"expires_in":600,
			"interval":1
		}`, srv.URL, srv.URL)
	})

	mux.HandleFunc("/application/o/token/", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if got := r.Form.Get("grant_type"); got != "urn:ietf:params:oauth:grant-type:device_code" {
			t.Errorf("token grant_type=%q", got)
		}
		if got := r.Form.Get("device_code"); got != "dev-123" {
			t.Errorf("token device_code=%q", got)
		}
		if got := r.Form.Get("client_id"); got != "kupe-cli" {
			t.Errorf("token client_id=%q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{"access_token":"acc","refresh_token":"ref","id_token":"id","expires_in":3600}`)
	})

	issuer := srv.URL + "/application/o/kupe-cli/"

	var gotCode, gotURI, gotComplete string
	prompt := func(userCode, verificationURI, verificationURIComplete string, _ time.Duration) {
		gotCode = userCode
		gotURI = verificationURI
		gotComplete = verificationURIComplete
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ts, err := DeviceFlow(ctx, prompt, issuer, "kupe-cli", "openid email offline_access")
	if err != nil {
		t.Fatalf("DeviceFlow: %v", err)
	}
	if ts.AccessToken != "acc" || ts.RefreshToken != "ref" || ts.IDToken != "id" {
		t.Fatalf("token set wrong: %+v", ts)
	}
	if gotCode != "WDJB-MJHT" {
		t.Errorf("prompt user_code=%q", gotCode)
	}
	if !strings.HasSuffix(gotURI, "/device") {
		t.Errorf("prompt verification_uri=%q", gotURI)
	}
	if !strings.Contains(gotComplete, "code=WDJB-MJHT") {
		t.Errorf("prompt verification_uri_complete=%q", gotComplete)
	}
}

// TestDeviceFlowPolls verifies the CLI tolerates the IdP returning
// authorization_pending — golang.org/x/oauth2 honours the response and
// retries after the advertised interval.
func TestDeviceFlowPolls(t *testing.T) {
	t.Cleanup(SetBrowserOpenerForTest(func(string) error { return nil }))

	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	discoveryWithDeviceEndpoint(t, mux, srv)

	mux.HandleFunc("/application/o/device/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{
			"device_code":"dev-123",
			"user_code":"AAAA-BBBB",
			"verification_uri":"%s/device",
			"expires_in":600,
			"interval":1
		}`, srv.URL)
	})

	var calls atomic.Int32
	mux.HandleFunc("/application/o/token/", func(w http.ResponseWriter, _ *http.Request) {
		n := calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if n == 1 {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintln(w, `{"error":"authorization_pending"}`)
			return
		}
		fmt.Fprintln(w, `{"access_token":"acc","refresh_token":"ref","id_token":"id","expires_in":3600}`)
	})

	issuer := srv.URL + "/application/o/kupe-cli/"

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ts, err := DeviceFlow(ctx, nil, issuer, "kupe-cli", "openid")
	if err != nil {
		t.Fatalf("DeviceFlow: %v", err)
	}
	if ts.AccessToken != "acc" {
		t.Fatalf("token set wrong: %+v", ts)
	}
	if got := calls.Load(); got < 2 {
		t.Errorf("/token hit %d times; want at least 2 (one pending, one success)", got)
	}
}

// TestDeviceFlowMissingEndpoint verifies the explicit error when the IdP
// does not advertise a device_authorization_endpoint in its discovery
// document. The error message must guide the user toward --method token.
func TestDeviceFlowMissingEndpoint(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	mux.HandleFunc("/application/o/kupe-cli/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// No device_authorization_endpoint field.
		fmt.Fprintf(w, `{
            "issuer":"%s/application/o/kupe-cli/",
            "authorization_endpoint":"%s/application/o/authorize/",
            "token_endpoint":"%s/application/o/token/"
        }`, srv.URL, srv.URL, srv.URL)
	})

	issuer := srv.URL + "/application/o/kupe-cli/"

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_, err := DeviceFlow(ctx, nil, issuer, "kupe-cli", "openid")
	if err == nil {
		t.Fatal("expected error when device_authorization_endpoint is missing")
	}
	if !strings.Contains(err.Error(), "device_authorization_endpoint") {
		t.Errorf("error should name the missing field; got %v", err)
	}
	if !strings.Contains(err.Error(), "--method token") {
		t.Errorf("error should hint at --method token escape hatch; got %v", err)
	}
}

// TestDeviceFlowAccessDenied verifies user-denial surfaces as a clear error
// rather than a polling timeout.
func TestDeviceFlowAccessDenied(t *testing.T) {
	t.Cleanup(SetBrowserOpenerForTest(func(string) error { return nil }))

	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	discoveryWithDeviceEndpoint(t, mux, srv)

	mux.HandleFunc("/application/o/device/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{
			"device_code":"dev-123",
			"user_code":"AAAA-BBBB",
			"verification_uri":"%s/device",
			"expires_in":600,
			"interval":1
		}`, srv.URL)
	})

	mux.HandleFunc("/application/o/token/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintln(w, `{"error":"access_denied","error_description":"user denied"}`)
	})

	issuer := srv.URL + "/application/o/kupe-cli/"

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := DeviceFlow(ctx, nil, issuer, "kupe-cli", "openid")
	if err == nil {
		t.Fatal("expected error when user denies the device authorization")
	}
	if !strings.Contains(err.Error(), "access_denied") {
		t.Errorf("error should surface access_denied; got %v", err)
	}
}
