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

// authRefreshTimeout bounds the OIDC discovery+refresh round-trip so a stalled
// IdP can't hang a kupe invocation (and, via the exec-plugin kubeconfig, every
// kubectl call) indefinitely. Matches the kupe-api client's per-attempt
// budget. See KC-3.
const authRefreshTimeout = 30 * time.Second

// ErrAuthRefreshTimeout marks an OIDC refresh that hit authRefreshTimeout. It
// is deliberately NOT wrapped as context.DeadlineExceeded: the refresh runs on
// its own background-derived deadline (not the caller's wait context), so a
// mid-wait IdP stall must be classified as a *transient auth fault* that rides
// the wait's transient grace — never confused with the wait's own deadline
// (which the spinner maps to ErrWaitTimeout / exit 8). See KC-4 / C4.
var ErrAuthRefreshTimeout = errors.New("OIDC token refresh timed out")

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

	// SignupClient returns a client for the signup service (self-service
	// user operations such as `kupe user delete`). Scoped to the resolved
	// SignupURL and the same bearer token as Client — the signup service
	// only accepts OIDC tokens, which the command layer enforces up front.
	// Tests inject a clienttest.FakeSignup here.
	SignupClient func() (client.SignupInterface, error)
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
		resolvedErr  error
	)
	f.Resolved = func() (*config.Resolved, error) {
		resolvedOnce.Do(func() {
			c, err := f.Config()
			if err != nil {
				resolvedErr = err // capture so we don't re-call Config below
				return
			}
			resolved = config.Resolve(config.Flags{
				APIURL:  flags.APIURL,
				Token:   flags.Token,
				Tenant:  flags.Tenant,
				Context: flags.Context,
			}, env, c)
		})
		return resolved, resolvedErr
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
			if err := config.ValidateKupeURL(r.APIURL); err != nil {
				clientErr = MisuseError(err.Error())
				return
			}
			if r.Tenant == "" {
				clientErr = AuthError("no tenant set; pass --tenant, set KUPE_TENANT, or run kupe auth login")
				return
			}
			tok, err := f.Token()
			if err != nil {
				clientErr = TokenResolutionError(err)
				return
			}
			opts := []client.Option{}
			if flags.Verbose {
				opts = append(opts, client.WithTrace(verboseTrace(io)))
			}
			// For OIDC contexts, hand the client a per-request token source
			// instead of a fixed string. resolveToken refreshes the stored
			// set when it's within the skew of expiry, so a long-running
			// command (e.g. `cluster create --wait` spanning hours) that
			// outlives its access token transparently refreshes mid-flight
			// rather than 401ing and aborting the wait (MEDIUM-4). Apikey and
			// direct-token contexts never expire mid-command, so they keep the
			// static token and its cheaper single resolution.
			staticTok := tok
			if r.AuthMethod == config.AuthMethodOIDC && r.DirectToken == "" {
				// f.TokenWithExpiry (resolveToken) is wired before any command
				// runs; referencing the field defers the lookup to call time,
				// avoiding a forward reference to the closure defined below.
				opts = append(opts, client.WithTokenSource(func(context.Context) (string, error) {
					t, _, terr := f.TokenWithExpiry()
					return t, terr
				}))
				staticTok = ""
			}
			cli = client.New(r.APIURL, r.Tenant, staticTok, UserAgent(), opts...)
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

	var (
		signupOnce sync.Once
		signupCli  client.SignupInterface
		signupErr  error
	)
	f.SignupClient = func() (client.SignupInterface, error) {
		signupOnce.Do(func() {
			r, err := f.Resolved()
			if err != nil {
				signupErr = err
				return
			}
			if err := config.ValidateKupeURL(r.SignupURL); err != nil {
				signupErr = MisuseError(err.Error())
				return
			}
			tok, err := f.Token()
			if err != nil {
				signupErr = TokenResolutionError(err)
				return
			}
			opts := []client.Option{}
			if flags.Verbose {
				opts = append(opts, client.WithTrace(verboseTrace(io)))
			}
			staticTok := tok
			if r.AuthMethod == config.AuthMethodOIDC && r.DirectToken == "" {
				opts = append(opts, client.WithTokenSource(func(context.Context) (string, error) {
					t, _, terr := f.TokenWithExpiry()
					return t, terr
				}))
				staticTok = ""
			}
			signupCli = client.NewSignup(r.SignupURL, staticTok, UserAgent(), opts...)
		})
		return signupCli, signupErr
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
		// Bound the refresh so a wedged IdP can't hang every kubectl call that
		// rides the exec-plugin kubeconfig (KC-3). RefreshLocked serialises the
		// refresh across concurrent processes and only deletes the stored
		// credential when the refresh token that failed is still the one on
		// disk — so a lost rotation race can't clobber the winner (KC-1).
		refreshCtx, cancel := context.WithTimeout(context.Background(), authRefreshTimeout)
		defer cancel()
		fresh, err := m.RefreshLocked(refreshCtx, r.ContextName, ctx.TokenRef, r.OIDCIssuer, r.OIDCClientID, ts)
		if err != nil {
			if errors.Is(err, auth.ErrRefreshFailed) {
				return "", time.Time{}, auth.ErrNotFound
			}
			// The refresh's own budget fired (refreshCtx derives from
			// context.Background(), so this is never the caller's wait
			// deadline). Surface a distinct transient sentinel instead of a
			// bare DeadlineExceeded so a mid-wait blip rides the wait's
			// transient grace rather than being misreported as the wait
			// timeout (ErrWaitTimeout / exit 8). See C4.
			if refreshCtx.Err() == context.DeadlineExceeded && errors.Is(err, context.DeadlineExceeded) {
				return "", time.Time{}, fmt.Errorf("%w after %s", ErrAuthRefreshTimeout, authRefreshTimeout)
			}
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

// TokenResolutionError maps a token-resolution failure to a precise,
// actionable CLI error instead of collapsing every cause into "not logged in"
// (KC-4). ErrNotFound (no credential, or a refresh that exhausted the token)
// stays a login prompt; a keyring outage and any other cause (network during
// refresh, corrupt blob) get their own guidance so a transient blip isn't
// mistaken for an expired session.
func TokenResolutionError(err error) error {
	switch {
	case errors.Is(err, auth.ErrNotFound):
		return AuthError("not logged in (or your session expired); run \"kupe auth login\"")
	case errors.Is(err, auth.ErrKeyringUnavailable):
		return AuthError("could not read credentials from the OS keyring").
			WithHint("set KUPE_STORAGE=plaintext to use the file-based credential store, or export KUPE_API_TOKEN")
	default:
		return Wrap(ExitAuth, "resolving credentials", err)
	}
}
