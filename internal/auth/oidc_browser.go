package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"golang.org/x/oauth2"
)

// BrowserFlow runs a single auth-code + PKCE login against the Authentik
// kupe-cli application. It binds a localhost listener on a random port,
// builds the authorization URL with a PKCE challenge, opens the user's
// browser, and waits for the redirect callback. On success it exchanges
// the code for a token set and returns it.
//
// The listener is bound on 127.0.0.1 (not "localhost") so that the URL
// matches Authentik's redirect_uri regex http://localhost:.* — Authentik
// resolves "localhost" host case-insensitively but the registered regex
// is anchored to "localhost", so we send "localhost" in the redirect_uri
// while binding on 127.0.0.1 to avoid IPv6 surprises on dual-stack hosts.
func BrowserFlow(ctx context.Context, prompt PromptFn, issuer, clientID, scopes string) (OIDCTokenSet, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return OIDCTokenSet{}, fmt.Errorf("binding localhost callback listener: %w", err)
	}
	defer func() { _ = listener.Close() }()

	port := listener.Addr().(*net.TCPAddr).Port
	redirectURI := fmt.Sprintf("http://localhost:%d/callback", port)

	state, err := randomHex(16)
	if err != nil {
		return OIDCTokenSet{}, err
	}
	verifier := oauth2.GenerateVerifier()

	cfg := &oauth2.Config{
		ClientID:    clientID,
		RedirectURL: redirectURI,
		Endpoint: oauth2.Endpoint{
			AuthURL:   JoinIssuerPath(issuer, "authorize"),
			TokenURL:  JoinIssuerPath(issuer, "token"),
			AuthStyle: oauth2.AuthStyleInParams,
		},
		Scopes: strings.Fields(scopes),
	}
	// Refresh tokens come from the offline_access scope (OIDC standard),
	// not Google's access_type=offline param — so we don't pass
	// oauth2.AccessTypeOffline here.
	authURL := cfg.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier))

	if prompt != nil {
		prompt(authURL)
	}
	_ = browserOpener(authURL)

	type result struct {
		code string
		err  error
	}
	done := make(chan result, 1)

	srv := &http.Server{
		ReadHeaderTimeout: 5 * time.Second,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/callback" {
				http.NotFound(w, r)
				return
			}
			q := r.URL.Query()
			if errCode := q.Get("error"); errCode != "" {
				msg := errCode
				if d := q.Get("error_description"); d != "" {
					msg = errCode + ": " + d
				}
				writeBrowserResponse(w, false, msg)
				done <- result{err: fmt.Errorf("authentik returned %s", msg)}
				return
			}
			if got := q.Get("state"); got != state {
				writeBrowserResponse(w, false, "state mismatch")
				done <- result{err: errors.New("state mismatch in OIDC callback (possible CSRF)")}
				return
			}
			code := q.Get("code")
			if code == "" {
				writeBrowserResponse(w, false, "missing authorization code")
				done <- result{err: errors.New("OIDC callback missing authorization code")}
				return
			}
			writeBrowserResponse(w, true, "")
			done <- result{code: code}
		}),
	}
	go func() { _ = srv.Serve(listener) }()
	defer func() {
		shutCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
	}()

	select {
	case <-ctx.Done():
		return OIDCTokenSet{}, ctx.Err()
	case res := <-done:
		if res.err != nil {
			return OIDCTokenSet{}, res.err
		}
		tok, err := cfg.Exchange(ctx, res.code, oauth2.VerifierOption(verifier))
		if err != nil {
			return OIDCTokenSet{}, fmt.Errorf("exchanging authorization code: %w", err)
		}
		out := OIDCTokenSet{
			AccessToken:  tok.AccessToken,
			RefreshToken: tok.RefreshToken,
			Expiry:       tok.Expiry,
		}
		if idTok, ok := tok.Extra("id_token").(string); ok {
			out.IDToken = idTok
		}
		return out, nil
	}
}

// PromptFn is invoked once with the authorization URL so the caller can
// echo it to stderr ("Opening your browser to ... If it doesn't open,
// visit this URL manually."). nil disables the prompt.
type PromptFn func(authURL string)

func randomHex(nBytes int) (string, error) {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generating random bytes: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// browserOpener is the function BrowserFlow calls to launch the user's
// default browser. Production wires it to openBrowser; tests swap in a
// no-op via SetBrowserOpenerForTest so they don't fire the real browser
// every time the suite runs.
var browserOpener = openBrowser

// SetBrowserOpenerForTest replaces browserOpener and returns a function
// that restores the previous value. Test-only; production code never
// calls this. Kept exported (rather than a build-tag dance) because
// internal packages can't import it from tests otherwise.
func SetBrowserOpenerForTest(fn func(string) error) func() {
	prev := browserOpener
	browserOpener = fn
	return func() { browserOpener = prev }
}

// openBrowser launches the user's default browser pointing at u. Best-effort:
// returns nil on success and a wrapped error on failure, but callers should
// ignore the error and rely on the URL prompt as a fallback (some CI shells
// and SSH sessions have no browser at all and that's a valid login path —
// the user just copy-pastes the URL into their laptop browser).
func openBrowser(u string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", u) //#nosec G204 -- u is a URL we built from a config-controlled issuer; the executable name is fixed.
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", u) //#nosec G204 -- see darwin case.
	default: // linux, *bsd
		cmd = exec.Command("xdg-open", u) //#nosec G204 -- see darwin case.
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("launching browser: %w", err)
	}
	// We don't Wait — the browser is a long-running process and the user
	// needs the CLI to keep running its callback server.
	return nil
}

// writeBrowserResponse renders the post-callback HTML the user sees in
// their browser tab. Kept inline (not a template file) so the binary stays
// self-contained.
func writeBrowserResponse(w http.ResponseWriter, ok bool, errMsg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if ok {
		_, _ = io.WriteString(w, `<!doctype html>
<title>Kupe CLI — login complete</title>
<style>body{font-family:system-ui,sans-serif;max-width:32rem;margin:4rem auto;padding:0 1rem;color:#1a1a1a}h1{color:#0a7}</style>
<h1>You're signed in.</h1>
<p>You can close this tab and return to your terminal.</p>`)
		return
	}
	w.WriteHeader(http.StatusBadRequest)
	// #nosec G705 -- errMsg passes through htmlEscape immediately below.
	_, _ = fmt.Fprintf(w, `<!doctype html>
<title>Kupe CLI — login failed</title>
<style>body{font-family:system-ui,sans-serif;max-width:32rem;margin:4rem auto;padding:0 1rem;color:#1a1a1a}h1{color:#c00}pre{background:#f5f5f5;padding:0.5rem 1rem;border-radius:4px}</style>
<h1>Login failed</h1>
<p>The authorization server returned an error:</p>
<pre>%s</pre>
<p>Return to your terminal — the CLI has logged the same error.</p>`, htmlEscape(errMsg))
}

func htmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&#39;")
	return r.Replace(s)
}
