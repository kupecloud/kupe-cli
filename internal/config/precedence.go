package config

import (
	"os"

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
	OIDCIssuer   string
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
		OIDCIssuer:   os.Getenv("KUPE_OIDC_ISSUER"),
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
	// OIDCIssuer / OIDCClientID are the effective OIDC parameters for the
	// resolved context. Precedence: env > context > build default. The
	// flag-level overrides for these are login-time-only (they get
	// persisted into the context) so they don't appear in the global
	// Flags struct above.
	OIDCIssuer   string
	OIDCClientID string
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
	r.ContextName = firstNonEmpty(flags.Context, env.Context)
	if r.ContextName == "" && cfg != nil {
		r.ContextName = cfg.CurrentContext
	}

	var ctx *Context
	if cfg != nil && r.ContextName != "" {
		ctx = cfg.Context(r.ContextName)
	}

	// APIURL
	r.APIURL = firstNonEmpty(flags.APIURL, env.APIURL)
	if r.APIURL == "" && ctx != nil {
		r.APIURL = ctx.APIURL
	}
	if r.APIURL == "" {
		r.APIURL = DefaultAPIURL
	}

	// Tenant
	r.Tenant = firstNonEmpty(flags.Tenant, env.Tenant)
	if r.Tenant == "" && ctx != nil {
		r.Tenant = ctx.Tenant
	}

	// DirectToken — flag and env bypass the keyring. If neither is set,
	// callers must look up the token via the context's TokenRef.
	r.DirectToken = firstNonEmpty(flags.Token, env.Token)

	// OIDC parameters: env > context > build default.
	if ctx != nil {
		r.AuthMethod = ctx.AuthMethod
	}
	r.OIDCIssuer = env.OIDCIssuer
	if r.OIDCIssuer == "" && ctx != nil {
		r.OIDCIssuer = ctx.OIDCIssuer
	}
	if r.OIDCIssuer == "" {
		r.OIDCIssuer = build.OIDCIssuer
	}
	r.OIDCClientID = env.OIDCClientID
	if r.OIDCClientID == "" && ctx != nil {
		r.OIDCClientID = ctx.OIDCClientID
	}
	if r.OIDCClientID == "" {
		r.OIDCClientID = build.OIDCClientID
	}

	return r
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
