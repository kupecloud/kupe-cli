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

	// OIDCBaseURL is the default Authentik base URL the CLI hits when
	// the user runs `kupe auth login --method oidc` without an override.
	// The full issuer URL is built as
	// {OIDCBaseURL}/application/o/{OIDCClientID}/ at runtime — see
	// auth.BuildIssuerURL. Dev users override per-context (--oidc-base-url
	// flag at login or KUPE_OIDC_BASE_URL env var) since dev is staff-only
	// behind the internal LB.
	OIDCBaseURL = "https://auth.kupe.cloud"

	// OIDCClientID is the public OAuth2 client_id registered in
	// Authentik for the CLI, and is also the application slug used in
	// the issuer URL path. See kupe/authentik/templates/configmap-blueprints.yaml.
	OIDCClientID = "kupe-cli"

	// OIDCScopes are the scopes requested during the device-code flow.
	// offline_access is required for the refresh_token grant. groups is
	// REQUIRED: kupe-api's OIDC gate rejects any token without a groups
	// claim ({tenant}-admins / {tenant}-readonly proves tenant scope), and
	// Authentik only emits the claim when the scope is requested — omitting
	// it fails every CLI request with 401 missing_groups.
	OIDCScopes = "openid email profile offline_access groups"
)
