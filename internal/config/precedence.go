package config

import (
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/kupecloud/kupe-cli/internal/build"
)

// Env collects the environment variables the CLI honours for config
// resolution. Loaded once per invocation by LoadEnv.
type Env struct {
	APIURL       string
	Token        string
	Tenant       string
	Context      string
	Config       string
	OIDCBaseURL  string
	OIDCClientID string
}

// LoadEnv reads the KUPE_* variables the CLI cares about.
func LoadEnv() Env {
	return Env{
		APIURL:       os.Getenv("KUPE_API_URL"),
		Token:        os.Getenv("KUPE_API_TOKEN"),
		Tenant:       os.Getenv("KUPE_TENANT"),
		Context:      os.Getenv("KUPE_CONTEXT"),
		Config:       os.Getenv("KUPE_CONFIG"),
		OIDCBaseURL:  os.Getenv("KUPE_OIDC_BASE_URL"),
		OIDCClientID: os.Getenv("KUPE_OIDC_CLIENT_ID"),
	}
}

// Flags is the subset of global flags that participates in config/token
// resolution. Callers populate it from cli.GlobalFlags before passing in.
type Flags struct {
	APIURL  string
	Token   string
	Tenant  string
	Context string
}

// Resolved is the effective configuration for a single command invocation,
// after flag/env/file/default merging. Token resolution is separate — see
// internal/auth — because it involves the OS keyring.
type Resolved struct {
	APIURL      string
	Tenant      string
	ContextName string
	// DirectToken is the flag/env-provided token (if any) that bypasses
	// the keyring. Empty string means "look up by context".
	DirectToken string
	// AuthMethod is the resolved context's auth method (oidc or apikey).
	// Empty when no context exists yet (first-time login).
	AuthMethod string
	// OIDCBaseURL / OIDCClientID are the effective OIDC parameters for
	// the resolved context. Precedence: env > context > build default.
	// The flag-level overrides for these are login-time-only (they get
	// persisted into the context) so they don't appear in the global
	// Flags struct above.
	//
	// OIDCIssuer is the computed full issuer URL — the CLI never
	// accepts a full issuer from a user; it composes one from base+clientID.
	// Stored here so consumers (auth.Refresh, auth.DeviceFlow) can
	// pass it without recomputing.
	OIDCBaseURL  string
	OIDCClientID string
	OIDCIssuer   string
}

// Resolve merges flags, env vars, and config-file context into the final
// values a command should use. Order: flag > env > context > default.
//
// If tenant cannot be resolved from any source, Resolve returns nil (callers
// should treat this as a misuse; the "no tenant" error lives in the command
// layer so it can include command-specific hints).
//
// Note: cfg may be nil (first run, no config file), in which case only
// flags and env contribute.
func Resolve(flags Flags, env Env, cfg *Config) *Resolved {
	r := &Resolved{}

	// Context name first — the context (if any) provides defaults for the
	// rest.
	r.ContextName = FirstNonEmpty(flags.Context, env.Context)
	if r.ContextName == "" && cfg != nil {
		r.ContextName = cfg.CurrentContext
	}

	var ctx *Context
	if cfg != nil && r.ContextName != "" {
		ctx = cfg.Context(r.ContextName)
	}

	// APIURL
	r.APIURL = FirstNonEmpty(flags.APIURL, env.APIURL)
	if r.APIURL == "" && ctx != nil {
		r.APIURL = ctx.APIURL
	}
	if r.APIURL == "" {
		r.APIURL = DefaultAPIURL
	}
	r.APIURL = NormalizeURL(r.APIURL)

	// Tenant
	r.Tenant = FirstNonEmpty(flags.Tenant, env.Tenant)
	if r.Tenant == "" && ctx != nil {
		r.Tenant = ctx.Tenant
	}

	// DirectToken — flag and env bypass the keyring. If neither is set,
	// callers must look up the token via the context's TokenRef.
	r.DirectToken = FirstNonEmpty(flags.Token, env.Token)

	// OIDC parameters: env > context > build default.
	if ctx != nil {
		r.AuthMethod = ctx.AuthMethod
	}
	r.OIDCBaseURL = env.OIDCBaseURL
	if r.OIDCBaseURL == "" && ctx != nil {
		r.OIDCBaseURL = ctx.OIDCBaseURL
	}
	if r.OIDCBaseURL == "" {
		r.OIDCBaseURL = build.OIDCBaseURL
	}
	r.OIDCBaseURL = NormalizeURL(r.OIDCBaseURL)
	r.OIDCClientID = env.OIDCClientID
	if r.OIDCClientID == "" && ctx != nil {
		r.OIDCClientID = ctx.OIDCClientID
	}
	if r.OIDCClientID == "" {
		r.OIDCClientID = build.OIDCClientID
	}
	r.OIDCIssuer = BuildIssuerURL(r.OIDCBaseURL, r.OIDCClientID)

	return r
}

// NormalizeURL prepends https:// to a bare host[:port] value so users
// can pass URL-typed flags (--api-url, --oidc-base-url) without the
// scheme. An explicit http:// is preserved (dev local), any non-empty
// value already containing "://" is returned unchanged, and empty input
// returns empty so callers can decide what default applies.
func NormalizeURL(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if strings.Contains(s, "://") {
		return s
	}
	return "https://" + s
}

// ValidateKupeURL ensures a URL points at a kupe.cloud host. Returns
// nil for accepted hostnames, a descriptive error otherwise. Empty
// input is accepted (callers handle "unset" elsewhere). Apply
// NormalizeURL first so a bare host[:port] value gets a scheme.
//
// Strict: hostname must be exactly "kupe.cloud" or end in ".kupe.cloud".
// This prevents users typing wrong hostnames (api.kup.cloud, evil.com)
// or being phished into sending tokens to attacker-controlled hosts.
//
// Indirected through a package-level var so tests using httptest
// (random localhost ports) can swap in a permissive validator via
// SetURLValidatorForTest.
var ValidateKupeURL = strictKupeURL

// SetURLValidatorForTest replaces ValidateKupeURL and returns a
// restorer. Test-only: production code never calls this. Used by
// httptest-driven tests that need to point the CLI at a localhost
// fake server.
func SetURLValidatorForTest(fn func(string) error) func() {
	prev := ValidateKupeURL
	ValidateKupeURL = fn
	return func() { ValidateKupeURL = prev }
}

func strictKupeURL(s string) error {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	u, err := url.Parse(s)
	if err != nil {
		return fmt.Errorf("invalid URL %q: %w", s, err)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("URL %q has no host", s)
	}
	if host == "kupe.cloud" || strings.HasSuffix(host, ".kupe.cloud") {
		return nil
	}
	return fmt.Errorf("URL %q is not a Kupe endpoint (host must be kupe.cloud or *.kupe.cloud)", s)
}

// BuildIssuerURL composes the full Authentik OIDC issuer URL the CLI
// uses for OAuth2 endpoints and JWT issuer validation. The kupe-cli
// Authentik application's slug equals its client_id by convention (see
// kupe/authentik/templates/configmap-blueprints.yaml), so the issuer is
// always {baseURL}/application/o/{clientID}/.
//
// Lives in the config package rather than internal/auth so that the
// resolver can populate Resolved.OIDCIssuer eagerly without dragging the
// auth package into the import graph of every callsite.
func BuildIssuerURL(baseURL, clientID string) string {
	if baseURL == "" || clientID == "" {
		return ""
	}
	trimmed := baseURL
	for trimmed != "" && trimmed[len(trimmed)-1] == '/' {
		trimmed = trimmed[:len(trimmed)-1]
	}
	return trimmed + "/application/o/" + clientID + "/"
}

// FirstNonEmpty returns the first non-empty string in values, or empty
// when all are empty. Used by Resolve and by login flows to express
// flag > env > context > default precedence chains compactly.
func FirstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
