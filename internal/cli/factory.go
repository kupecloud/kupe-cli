package cli

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"time"

	"github.com/kupecloud/kupe-cli/internal/auth"
	"github.com/kupecloud/kupe-cli/internal/build"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/config"
)

// Factory is the dependency-injection seam between command bodies and the
// rest of the runtime. Every subcommand receives a *Factory at construction
// time. The fields are lazy functions — they memoise results on first call
// so commands that don't need the config never load it, and commands that
// call f.Config() twice only read the file once.
//
// In production, NewFactory wires real implementations. In tests, construct
// a Factory by filling fields directly with fakes.
type Factory struct {
	IOStreams *IOStreams
	Flags     *GlobalFlags

	// Config returns the parsed config file. Missing file → empty config,
	// not an error. Any I/O or parse error is surfaced.
	Config func() (*config.Config, error)

	// Resolved returns the resolved context (API URL, tenant, context name,
	// optional direct token from flag/env). Depends on Config + Env + Flags.
	Resolved func() (*config.Resolved, error)

	// ConfigPath returns the path the CLI will read from / write to. Derived
	// from --config > KUPE_CONFIG > platform default.
	ConfigPath func() (string, error)

	// Auth returns the Manager used for keyring/plaintext token storage.
	// Lazy so tests can fake the manager without touching the real keyring.
	Auth func() (*auth.Manager, error)

	// Token returns the effective bearer token: flag/env direct-token first,
	// otherwise looked up via Auth using the resolved context's TokenRef.
	// Returns auth.ErrNotFound if no source yields a token.
	Token func() (string, error)

	// TokenWithExpiry behaves like Token but also returns the access-token
	// expiry when the context is OIDC. Apikey contexts and direct-token
	// flag/env paths return a zero time.Time (no expiry hint). Used by
	// the kubeconfig exec-plugin so kubectl can avoid re-invoking the
	// CLI on every request.
	TokenWithExpiry func() (string, time.Time, error)

	// Client returns a production client.Interface scoped to the resolved
	// API URL, tenant, and token. Memoised per invocation. Tests inject a
	// clienttest.Fake by overwriting this field before running a command.
	Client func() (client.Interface, error)

	// PublicClient returns a client.Interface configured for endpoints that
	// do NOT require authentication (currently just the /api/v1/plans
	// catalog). It uses the resolved API URL when available and falls back
	// to config.DefaultAPIURL otherwise, so it never fails on missing
	// tenant or credentials. Tests override this field with a fake.
	PublicClient func() (client.Interface, error)
}

// UserAgent returns the string the CLI sends on every HTTP request. Stable
// across concurrent calls (build info is ldflags-immutable).
func UserAgent() string {
	return fmt.Sprintf("kupe-cli/%s (%s/%s) %s",
		build.Version, runtime.GOOS, runtime.GOARCH, runtime.Version())
}

// DefaultOutput returns the configured preferences.output value, or "" if
// no config file is present / the field is unset. Commands call this as a
// fallback when their local -o flag is empty — so users can set
// `preferences.output: json` once and have every list/get default to JSON
// without re-typing -o json. The command's -o flag always wins when set.
func (f *Factory) DefaultOutput() string {
	if f == nil || f.Config == nil {
		return ""
	}
	cfg, err := f.Config()
	if err != nil || cfg == nil {
		return ""
	}
	return cfg.Preferences.Output
}

// NewFactory wires a production Factory from the CLI's global flags and
// IOStreams. All field functions memoise on first call via sync.Once.
func NewFactory(io *IOStreams, flags *GlobalFlags) *Factory {
	f := &Factory{IOStreams: io, Flags: flags}

	env := config.LoadEnv()

	var (
		pathOnce   sync.Once
		configPath string
		pathErr    error
	)
	f.ConfigPath = func() (string, error) {
		pathOnce.Do(func() {
			if flags.ConfigPath != "" {
				configPath = flags.ConfigPath
				return
			}
			if env.Config != "" {
				configPath = env.Config
				return
			}
			configPath, pathErr = config.DefaultPath()
		})
		return configPath, pathErr
	}

	var (
		cfgOnce sync.Once
		cfg     *config.Config
		cfgErr  error
	)
	f.Config = func() (*config.Config, error) {
		cfgOnce.Do(func() {
			path, err := f.ConfigPath()
			if err != nil {
				cfgErr = err
				return
			}
			cfg, cfgErr = config.Load(path)
		})
		return cfg, cfgErr
	}

	var (
		resolvedOnce sync.Once
		resolved     *config.Resolved
	)
	f.Resolved = func() (*config.Resolved, error) {
		resolvedOnce.Do(func() {
			c, err := f.Config()
			if err != nil {
				return // resolved stays nil; caller will see the Config error first
			}
			resolved = config.Resolve(config.Flags{
				APIURL:  flags.APIURL,
				Token:   flags.Token,
				Tenant:  flags.Tenant,
				Context: flags.Context,
			}, env, c)
			_ = c
		})
		if resolved == nil {
			// Re-run Config to surface its error (not cached here).
			if _, err := f.Config(); err != nil {
				return nil, err
			}
		}
		return resolved, nil
	}

	var (
		authOnce sync.Once
		mgr      *auth.Manager
		authErr  error
	)
	f.Auth = func() (*auth.Manager, error) {
		authOnce.Do(func() {
			path, err := f.ConfigPath()
			if err != nil {
				authErr = err
				return
			}
			mgr = auth.NewManager(auth.DefaultCredentialsPath(path))
		})
		return mgr, authErr
	}

	var (
		clientOnce sync.Once
		cli        client.Interface
		clientErr  error
	)
	f.Client = func() (client.Interface, error) {
		clientOnce.Do(func() {
			r, err := f.Resolved()
			if err != nil {
				clientErr = err
				return
			}
			if r.Tenant == "" {
				clientErr = AuthError("no tenant set; pass --tenant, set KUPE_TENANT, or run kupe auth login")
				return
			}
			tok, err := f.Token()
			if err != nil {
				clientErr = AuthError("not logged in; run \"kupe auth login\"")
				return
			}
			opts := []client.Option{}
			if flags.Verbose {
				opts = append(opts, client.WithTrace(verboseTrace(io)))
			}
			cli = client.New(r.APIURL, r.Tenant, tok, UserAgent(), opts...)
		})
		return cli, clientErr
	}

	var (
		pubOnce sync.Once
		pubCli  client.Interface
	)
	f.PublicClient = func() (client.Interface, error) {
		pubOnce.Do(func() {
			apiURL := config.DefaultAPIURL
			if r, err := f.Resolved(); err == nil && r != nil && r.APIURL != "" {
				apiURL = r.APIURL
			}
			opts := []client.Option{}
			if flags.Verbose {
				opts = append(opts, client.WithTrace(verboseTrace(io)))
			}
			pubCli = client.New(apiURL, "", "", UserAgent(), opts...)
		})
		return pubCli, nil
	}

	resolveToken := func() (string, time.Time, error) {
		r, err := f.Resolved()
		if err != nil {
			return "", time.Time{}, err
		}
		if r.DirectToken != "" {
			return r.DirectToken, time.Time{}, nil
		}
		c, err := f.Config()
		if err != nil {
			return "", time.Time{}, err
		}
		ctx := c.Context(r.ContextName)
		if ctx == nil || ctx.TokenRef == "" {
			return "", time.Time{}, auth.ErrNotFound
		}
		m, err := f.Auth()
		if err != nil {
			return "", time.Time{}, err
		}
		stored, err := m.GetByRef(r.ContextName, ctx.TokenRef)
		if err != nil {
			return "", time.Time{}, err
		}
		if !auth.IsOIDCBlob(stored) {
			return stored, time.Time{}, nil
		}
		ts, err := auth.UnmarshalOIDC(stored)
		if err != nil {
			return "", time.Time{}, err
		}
		if ts.Valid() {
			return ts.AccessToken, ts.Expiry, nil
		}
		ctxBg := context.Background()
		fresh, err := auth.Refresh(ctxBg, r.OIDCIssuer, r.OIDCClientID, ts)
		if err != nil {
			if errors.Is(err, auth.ErrRefreshFailed) {
				_ = m.DeleteByRef(r.ContextName, ctx.TokenRef)
				return "", time.Time{}, auth.ErrNotFound
			}
			return "", time.Time{}, err
		}
		blob, err := fresh.Marshal()
		if err != nil {
			return "", time.Time{}, err
		}
		if _, err := m.Set(r.ContextName, blob); err != nil {
			return "", time.Time{}, err
		}
		return fresh.AccessToken, fresh.Expiry, nil
	}

	f.Token = func() (string, error) {
		tok, _, err := resolveToken()
		return tok, err
	}
	f.TokenWithExpiry = resolveToken

	return f
}
