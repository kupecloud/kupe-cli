package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/oauth2"
)

// OIDCTokenSet is what the CLI persists for an OIDC-authenticated context.
// It serialises as a JSON blob into the same keyring slot the apikey path
// uses; IsOIDCBlob distinguishes the two on read.
type OIDCTokenSet struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	IDToken      string    `json:"id_token,omitempty"`
	Expiry       time.Time `json:"expiry"`
}

// refreshSkew is how early we treat an access token as expired. Avoids
// returning a token that will 401 mid-request because the clock crept up
// to its expiry between Token() and the API call.
const refreshSkew = 30 * time.Second

// Valid reports whether the access token is non-empty and not within the
// refresh skew of expiry.
func (t OIDCTokenSet) Valid() bool {
	return t.AccessToken != "" && time.Now().Add(refreshSkew).Before(t.Expiry)
}

// Marshal returns the JSON form stored in the keyring/plaintext file.
// The keyring/plaintext storage layer needs the token set serialised;
// both backends mode-protect the data at rest.
func (t OIDCTokenSet) Marshal() (string, error) {
	b, err := json.Marshal(t) //#nosec G117 -- intentional: see doc comment.
	if err != nil {
		return "", fmt.Errorf("marshalling OIDC token set: %w", err)
	}
	return string(b), nil
}

// IsOIDCBlob returns true if s looks like a serialised OIDCTokenSet (a JSON
// object) rather than a raw kupe_... API key. We check the first
// non-whitespace byte; storage never round-trips arbitrary user input.
func IsOIDCBlob(s string) bool {
	return strings.HasPrefix(strings.TrimSpace(s), "{")
}

// UnmarshalOIDC parses a stored blob. Returns an error if s isn't a JSON
// object (callers should IsOIDCBlob first when both shapes are possible).
func UnmarshalOIDC(s string) (OIDCTokenSet, error) {
	var t OIDCTokenSet
	if err := json.Unmarshal([]byte(s), &t); err != nil {
		return OIDCTokenSet{}, fmt.Errorf("unmarshalling OIDC token set: %w", err)
	}
	return t, nil
}

// ErrRefreshFailed is returned by Refresh when the issuer rejects the
// refresh token (typically invalid_grant after Authentik's 30-day TTL or
// after the user revoked their session). Callers should clear the stored
// token and ask the user to log in again.
var ErrRefreshFailed = errors.New("OIDC refresh token rejected")

// Discovery is a minimal subset of the OIDC discovery document the CLI
// needs to build the authorize and token URLs. We hit
// {issuer}/.well-known/openid-configuration rather than hardcoding paths
// so we stay correct against any compliant IdP — Authentik in particular
// puts authorize/token at the realm level (/application/o/authorize/),
// not under the application slug.
type Discovery struct {
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	UserinfoEndpoint      string `json:"userinfo_endpoint,omitempty"`
	JWKSURI               string `json:"jwks_uri,omitempty"`
}

// Discover fetches the OIDC discovery document at
// {issuer}/.well-known/openid-configuration and returns the endpoints
// the CLI uses. The issuer string here is the iss-claim URL (Authentik
// app URL: {base}/application/o/{slug}/).
func Discover(ctx context.Context, issuer string) (*Discovery, error) {
	docURL, err := url.JoinPath(issuer, ".well-known/openid-configuration")
	if err != nil {
		return nil, fmt.Errorf("building discovery URL: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, docURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("building discovery request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching OIDC discovery: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("OIDC discovery at %s returned %d", docURL, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("reading OIDC discovery: %w", err)
	}
	var d Discovery
	if err := json.Unmarshal(body, &d); err != nil {
		return nil, fmt.Errorf("parsing OIDC discovery: %w", err)
	}
	if d.AuthorizationEndpoint == "" || d.TokenEndpoint == "" {
		return nil, fmt.Errorf("OIDC discovery missing authorization/token endpoint")
	}
	return &d, nil
}

// Refresh exchanges a refresh_token for a new token set against the
// discovered token endpoint. On invalid_grant the function returns
// ErrRefreshFailed; on transport or other errors it returns the raw error.
func Refresh(ctx context.Context, issuer, clientID string, current OIDCTokenSet) (OIDCTokenSet, error) {
	if current.RefreshToken == "" {
		return OIDCTokenSet{}, fmt.Errorf("no refresh token stored: %w", ErrRefreshFailed)
	}

	disc, err := Discover(ctx, issuer)
	if err != nil {
		return OIDCTokenSet{}, fmt.Errorf("refreshing OIDC token: %w", err)
	}

	cfg := &oauth2.Config{
		ClientID: clientID,
		Endpoint: oauth2.Endpoint{
			TokenURL:  disc.TokenEndpoint,
			AuthStyle: oauth2.AuthStyleInParams, // public client, no secret
		},
	}
	src := cfg.TokenSource(ctx, &oauth2.Token{RefreshToken: current.RefreshToken})
	tok, err := src.Token()
	if err != nil {
		// oauth2 wraps invalid_grant in a *RetrieveError; the body
		// contains "invalid_grant" verbatim. Match on substring rather
		// than reflecting on the type — the surface is stable enough.
		if strings.Contains(err.Error(), "invalid_grant") {
			return OIDCTokenSet{}, fmt.Errorf("%w: %w", ErrRefreshFailed, err)
		}
		return OIDCTokenSet{}, fmt.Errorf("refreshing OIDC token: %w", err)
	}

	out := OIDCTokenSet{
		AccessToken:  tok.AccessToken,
		RefreshToken: firstNonEmpty(tok.RefreshToken, current.RefreshToken),
		Expiry:       tok.Expiry,
	}
	if idTok, ok := tok.Extra("id_token").(string); ok && idTok != "" {
		out.IDToken = idTok
	} else {
		out.IDToken = current.IDToken
	}
	return out, nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// EmailFromIDToken parses the JWT payload and returns the "email" claim.
// Returns empty string if the token is malformed or the claim is absent.
// Signature is NOT verified — kupe-api validates every request server-side
// against Authentik's JWKS, so the CLI only uses the email claim cosmetically
// (Context.User, login confirmation message).
func EmailFromIDToken(idToken string) string {
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var claims struct {
		Email string `json:"email"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return ""
	}
	return claims.Email
}
