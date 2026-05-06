package auth

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"golang.org/x/oauth2"
)

// DeviceFlow runs an OAuth 2.0 Device Authorization Grant (RFC 8628) login
// against the Authentik kupe-cli public client.
//
// The flow is purely HTTP — no localhost listener, no port binding, no
// redirect — which makes it work identically on a developer laptop, an
// SSH session, a CI runner, or a remote dev container. The CLI:
//
//  1. POSTs to the issuer's device_authorization_endpoint to get a
//     device_code, user_code, verification URL, and polling interval.
//  2. Calls the prompt callback so the caller can echo the user_code +
//     verification URL to the user (typically stderr).
//  3. Best-effort opens the user's browser at verification_uri_complete
//     (the URL with the code pre-filled) so the local-laptop happy path
//     is one click. Failure is fine — the prompt has already shown the
//     code and URL.
//  4. Polls the token endpoint until the user approves, the code expires,
//     or the user denies. golang.org/x/oauth2 handles the
//     authorization_pending / slow_down RFC 8628 error mapping.
func DeviceFlow(ctx context.Context, prompt DevicePrompt, issuer, clientID, scopes string) (OIDCTokenSet, error) {
	disc, err := Discover(ctx, issuer)
	if err != nil {
		return OIDCTokenSet{}, err
	}
	if disc.DeviceAuthorizationEndpoint == "" {
		return OIDCTokenSet{}, fmt.Errorf("issuer %s does not advertise a device_authorization_endpoint — device flow is unsupported by this provider, use --method token instead", issuer)
	}

	cfg := &oauth2.Config{
		ClientID: clientID,
		Endpoint: oauth2.Endpoint{
			DeviceAuthURL: disc.DeviceAuthorizationEndpoint,
			TokenURL:      disc.TokenEndpoint,
			AuthStyle:     oauth2.AuthStyleInParams, // public client, no secret
		},
		Scopes: strings.Fields(scopes),
	}

	da, err := cfg.DeviceAuth(ctx)
	if err != nil {
		return OIDCTokenSet{}, fmt.Errorf("requesting device authorization: %w", err)
	}

	if prompt != nil {
		// time.Until rounded to the nearest second — the prompt typically
		// formats this as "10m" / "9m30s", and sub-second precision is
		// noise.
		expiresIn := time.Until(da.Expiry).Round(time.Second)
		prompt(da.UserCode, da.VerificationURI, da.VerificationURIComplete, expiresIn)
	}

	// Best-effort browser launch. verification_uri_complete embeds the
	// user_code so the user only has to confirm; bare verification_uri
	// requires them to type the code on the page. Either failing is fine —
	// the prompt has already shown both.
	target := da.VerificationURIComplete
	if target == "" {
		target = da.VerificationURI
	}
	_ = browserOpener(target)

	tok, err := cfg.DeviceAccessToken(ctx, da)
	if err != nil {
		return OIDCTokenSet{}, fmt.Errorf("waiting for device code approval: %w", err)
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

// DevicePrompt is invoked once when the device authorization response
// arrives. The caller (typically login.go) formats this into the
// user-facing message, e.g.:
//
//	To finish signing in, visit:
//	  https://auth.kupe.cloud/device
//	and enter the code:
//	  A1B2-C3D4
//	Waiting for approval (code expires in 10m)…
//
// verificationURIComplete is the IdP's URL with the code already
// embedded as a query param — preferred for the best-effort browser
// launch, but the textual prompt should still show the bare URI + code
// so users on a different device can type them.
//
// expiresIn is da.Expiry - time.Now() rounded to the nearest second; the
// prompt should surface it because the CLI's polling context terminates
// at this deadline and a user who AFKs past the window only sees
// "context deadline exceeded" otherwise.
type DevicePrompt func(userCode, verificationURI, verificationURIComplete string, expiresIn time.Duration)

// browserOpener is the function DeviceFlow calls to launch the user's
// default browser. Production wires it to openBrowser; tests swap in a
// no-op via SetBrowserOpenerForTest so they don't fire the real browser
// every time the suite runs.
var browserOpener = openBrowser

// SetBrowserOpenerForTest replaces browserOpener and returns a function
// that restores the previous value. Test-only; production code never
// calls this.
func SetBrowserOpenerForTest(fn func(string) error) func() {
	prev := browserOpener
	browserOpener = fn
	return func() { browserOpener = prev }
}

// openBrowser launches the user's default browser pointing at u. Best-effort:
// returns nil on success and a wrapped error on failure, but callers should
// ignore the error and rely on the prompt as the primary path — headless
// SSH, CI, and remote dev containers have no browser, and that's a valid
// device-flow login path (user copies the URL and code to a phone or other
// laptop).
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
	// Don't Wait — the browser is long-running; the CLI needs to keep
	// polling for the device code approval.
	return nil
}
