// Package build holds build-time metadata injected via -ldflags at link time.
// The values here are the dev defaults used when building without goreleaser
// or the Makefile.
package build

// These variables are overwritten by ldflags during a release build.
// See .goreleaser.yaml and Makefile for the injection points.
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"

	// OIDCIssuer is the default Authentik OIDC issuer URL the CLI hits
	// when the user runs `kupe auth login --method oidc` without an
	// override. Dev users override per-context (--oidc-issuer flag at
	// login or KUPE_OIDC_ISSUER env var) since dev is staff-only behind
	// the internal LB.
	OIDCIssuer = "https://auth.kupe.cloud/application/o/kupe-cli/"

	// OIDCClientID is the public OAuth2 client_id registered in
	// Authentik for the CLI. See kupe/authentik/templates/configmap-blueprints.yaml.
	OIDCClientID = "kupe-cli"

	// OIDCScopes are the scopes requested during the auth-code+PKCE flow.
	// offline_access is required for the refresh_token grant; the rest
	// match the kupe-cli Authentik application's property mappings
	// (openid, email, profile, kupe-groups, kupe-tenants).
	OIDCScopes = "openid email profile offline_access"
)
