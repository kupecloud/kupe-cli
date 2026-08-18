package auth

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/kupecloud/kupe-cli/internal/auth"
	"github.com/kupecloud/kupe-cli/internal/build"
	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/config"
)

// tenantNameRE loosely validates a tenant name: lowercase DNS-ish label.
// Authoritative validation happens server-side (the CLI calls
// GET /tenants/{tenant} after storing the token). Mirror kupe-api's rule
// exactly — `^[a-z][a-z0-9-]{0,61}[a-z0-9]$`, i.e. 2-63 chars — so a legal
// 2-char tenant isn't rejected client-side before the server is ever asked
// (KC-22; authoritative pattern in kupe-api/docs/security.md).
var tenantNameRE = regexp.MustCompile(`^[a-z][a-z0-9-]{0,61}[a-z0-9]$`)

// tokenPrefix is the API-key prefix kupe-api expects. OIDC access tokens
// flow through the same login command but use the OIDC method path and
// don't carry this prefix.
const tokenPrefix = "kupe_"

const (
	methodOIDC   = "oidc"
	methodToken  = "token"
	methodAPIKey = "apikey" // alias for "token"
)

type loginOpts struct {
	tenant       string
	token        string
	apiURL       string
	context      string
	method       string
	oidcBaseURL  string
	oidcClientID string
	setDefault   bool
}

func newLoginCmd(f *cli.Factory) *cobra.Command {
	opts := &loginOpts{}

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Authenticate and store credentials for the current or a new context",
		Long: `Authenticate against Kupe.

Two methods are supported:

  --method oidc (default)
    Runs an OAuth 2.0 Device Authorization (RFC 8628) flow against Authentik.
    The CLI prints a short user code and a verification URL; you open the
    URL on any device with a browser (your laptop tries to open it for you
    automatically) and approve. The CLI stores the resulting access and
    refresh tokens; the refresh token rotates transparently on subsequent
    commands.

    Works identically on a laptop, an SSH session, a CI runner, or a remote
    dev container — no localhost listener is required.

  --method token
    Reads a long-lived API key (format kupe_...) and stores it. Use this for
    CI machines and automation; pair with --token / KUPE_API_TOKEN to skip
    the prompt.

If KUPE_API_TOKEN is already set, this command refuses to run — the env-var
path is already authenticated and a login would be a no-op.

Context name defaults to the tenant name. Use --context to override when you
want more than one context per tenant (e.g., two API environments).`,
		Example: `  # Interactive OIDC login on your laptop
  kupe auth login --tenant acme

  # Same, against a non-default Authentik (dev/staging override).
  # Bare hostnames are fine — the CLI prepends https:// when missing.
  kupe auth login --tenant acme \
      --api-url api.dev.int.kupe.cloud \
      --oidc-base-url auth.dev.int.kupe.cloud

  # Store a long-lived API key as a context. Omit --token to be prompted for
  # it without echo — passing it inline leaves the token in shell history and
  # process listings. For pure CI auth, prefer exporting KUPE_API_TOKEN (no
  # login or config file needed at all).
  kupe auth login --method token --tenant acme --context prod --set-default`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runLogin(cmd, f, opts)
		},
	}

	cmd.Flags().StringVar(&opts.tenant, "tenant", "", "Tenant to authenticate against")
	cmd.Flags().StringVar(&opts.method, "method", methodOIDC, "Auth method: oidc (device flow, default) or token (long-lived API key)")
	cmd.Flags().StringVar(&opts.token, "token", "", "API token (format kupe_...). Required with --method=token in non-interactive mode.")
	cmd.Flags().StringVar(&opts.apiURL, "api-url", "", "API base URL for this context, with or without scheme (default "+strings.TrimPrefix(config.DefaultAPIURL, "https://")+")")
	cmd.Flags().StringVar(&opts.oidcBaseURL, "oidc-base-url", "", "Authentik base URL, with or without scheme (env: KUPE_OIDC_BASE_URL, default "+strings.TrimPrefix(build.OIDCBaseURL, "https://")+"). Issuer is built as {base}/application/o/{client-id}/.")
	cmd.Flags().StringVar(&opts.oidcClientID, "oidc-client-id", "", "OIDC public client_id (env: KUPE_OIDC_CLIENT_ID, default "+build.OIDCClientID+")")
	cmd.Flags().StringVar(&opts.context, "context", "", "Context name (default: tenant name)")
	cmd.Flags().BoolVar(&opts.setDefault, "set-default", false, "Mark this context as the current one")

	return cmd
}

func runLogin(cmd *cobra.Command, f *cli.Factory, opts *loginOpts) error {
	io := f.IOStreams

	// Refuse when KUPE_API_TOKEN is already exporting a token — that path is
	// scripted auth and doesn't need a config file.
	if os.Getenv("KUPE_API_TOKEN") != "" {
		return cli.AuthError("KUPE_API_TOKEN is set; the CLI is already authenticated via the env var. Unset it if you want to store credentials via kupe auth login.")
	}

	method := strings.ToLower(strings.TrimSpace(opts.method))
	if method == methodAPIKey {
		method = methodToken
	}
	if method != methodOIDC && method != methodToken {
		return cli.MisuseError(fmt.Sprintf("unknown --method %q (want oidc or token)", opts.method))
	}

	// Tenant: flag > prompt.
	tenant := strings.TrimSpace(opts.tenant)
	if tenant == "" {
		if !io.PromptsEnabled {
			return cli.MisuseError("--tenant is required in non-interactive mode")
		}
		got, err := promptLine(io, "Tenant: ")
		if err != nil {
			return err
		}
		tenant = got
	}
	if !tenantNameRE.MatchString(tenant) {
		return cli.MisuseError(fmt.Sprintf("invalid tenant name %q (must be lowercase DNS label, e.g. acme-corp)", tenant))
	}

	// Context name defaults to the tenant name.
	contextName := opts.context
	if contextName == "" {
		contextName = tenant
	}

	// Load (or create empty) config — needed to inherit per-context overrides.
	cfg, err := f.Config()
	if err != nil {
		return cli.Wrap(cli.ExitGeneral, "loading config", err)
	}

	apiURL := config.NormalizeURL(opts.apiURL)
	if apiURL == "" {
		if existing := cfg.Context(contextName); existing != nil {
			apiURL = existing.APIURL
		}
	}
	if err := config.ValidateKupeURL(apiURL); err != nil {
		return cli.MisuseError(err.Error())
	}
	effectiveAPIURL := apiURL
	if effectiveAPIURL == "" {
		effectiveAPIURL = config.DefaultAPIURL
	}

	switch method {
	case methodOIDC:
		return runOIDCLogin(cmd, f, opts, cfg, contextName, tenant, apiURL, effectiveAPIURL)
	default:
		return runTokenLogin(cmd, f, opts, cfg, contextName, tenant, apiURL, effectiveAPIURL)
	}
}

func runTokenLogin(cmd *cobra.Command, f *cli.Factory, opts *loginOpts, cfg *config.Config, contextName, tenant, apiURL, effectiveAPIURL string) error {
	io := f.IOStreams

	// Token: flag > prompt (hidden).
	token := strings.TrimSpace(opts.token)
	if token == "" {
		if !io.PromptsEnabled {
			return cli.MisuseError("--token is required in non-interactive mode")
		}
		got, err := promptSecret(io, "Paste your API token (create one at https://console.kupe.cloud/settings/api-keys):\n  ")
		if err != nil {
			return err
		}
		token = got
	}
	if !strings.HasPrefix(token, tokenPrefix) {
		fmt.Fprintf(io.ErrOut, "warning: token does not start with %q; proceeding anyway\n", tokenPrefix)
	}

	api := client.New(effectiveAPIURL, tenant, token, cli.UserAgent())
	t, _, err := api.GetTenant(cmd.Context())
	if err != nil {
		return loginValidationError(err, effectiveAPIURL, tenant)
	}

	mgr, err := f.Auth()
	if err != nil {
		return cli.Wrap(cli.ExitGeneral, "initialising auth manager", err)
	}

	// Token-login over an existing OIDC context: best-effort revoke the
	// previous refresh token at the IdP before overwriting it locally,
	// mirroring the OIDC re-login and delete-context paths (LOW-2). Otherwise
	// a still-valid refresh token is orphaned at the IdP with no local copy
	// left to revoke. Non-fatal — the new login proceeds regardless.
	if prev := cfg.Context(contextName); prev != nil && prev.AuthMethod == config.AuthMethodOIDC && prev.TokenRef != "" {
		RevokeOIDCRefreshToken(io, mgr, prev)
	}

	ref, err := mgr.Set(contextName, token)
	if err != nil {
		return cli.Wrap(cli.ExitGeneral, "storing token", err)
	}

	existing := cfg.Context(contextName)
	ctx := config.Context{
		Name:       contextName,
		APIURL:     apiURL,
		Tenant:     tenant,
		TokenRef:   ref,
		AuthMethod: config.AuthMethodAPIKey,
	}
	if existing != nil {
		ctx.User = existing.User
	}
	cfg.SetContext(ctx)

	if opts.setDefault || len(cfg.Contexts) == 1 || cfg.CurrentContext == "" {
		cfg.CurrentContext = contextName
	}

	if err := saveConfig(f, cfg); err != nil {
		return err
	}

	label := tenant
	if t.DisplayName != "" {
		label = fmt.Sprintf("%s (%s)", t.DisplayName, tenant)
	}
	// promptSecret already prints a trailing newline after the hidden
	// password input, so we don't add another blank here. The OIDC path
	// does need an explicit one because its URL line doesn't end with
	// blank space.
	fmt.Fprintf(io.ErrOut, "Logged in to tenant %s.\n", label)
	printPlaintextWarningIfFallback(f, ref)
	return nil
}

func runOIDCLogin(cmd *cobra.Command, f *cli.Factory, opts *loginOpts, cfg *config.Config, contextName, tenant, apiURL, effectiveAPIURL string) error {
	io := f.IOStreams

	// Resolve OIDC parameters: flag > existing context > build default.
	var ctxBase, ctxClient string
	if existing := cfg.Context(contextName); existing != nil {
		ctxBase, ctxClient = existing.OIDCBaseURL, existing.OIDCClientID
	}
	baseURL := config.FirstNonEmpty(config.NormalizeURL(opts.oidcBaseURL), ctxBase, build.OIDCBaseURL)
	clientID := config.FirstNonEmpty(strings.TrimSpace(opts.oidcClientID), ctxClient, build.OIDCClientID)
	if err := config.ValidateKupeURL(baseURL); err != nil {
		return cli.MisuseError(err.Error())
	}
	issuer := config.BuildIssuerURL(baseURL, clientID)

	prompt := func(userCode, verificationURI, verificationURIComplete string, expiresIn time.Duration) {
		fmt.Fprintln(io.ErrOut, "To finish signing in, open the following URL in any browser:")
		fmt.Fprintf(io.ErrOut, "\n    %s\n\n", verificationURI)
		fmt.Fprintln(io.ErrOut, "and enter this code:")
		fmt.Fprintf(io.ErrOut, "\n    %s\n\n", userCode)
		if verificationURIComplete != "" && verificationURIComplete != verificationURI {
			fmt.Fprintln(io.ErrOut, "(or open this URL — it has the code pre-filled:)")
			fmt.Fprintf(io.ErrOut, "    %s\n\n", verificationURIComplete)
		}
		if expiresIn > 0 {
			fmt.Fprintf(io.ErrOut, "Waiting for approval (code expires in %s)...\n", expiresIn)
		} else {
			fmt.Fprintln(io.ErrOut, "Waiting for approval...")
		}
	}

	ts, err := auth.DeviceFlow(cmd.Context(), prompt, issuer, clientID, build.OIDCScopes)
	if err != nil {
		return cli.Wrap(cli.ExitGeneral, "OIDC login", err)
	}

	// Validate against the API with the freshly-issued access token before
	// persisting anything — same contract as the token path.
	api := client.New(effectiveAPIURL, tenant, ts.AccessToken, cli.UserAgent())
	t, _, err := api.GetTenant(cmd.Context())
	if err != nil {
		return loginValidationError(err, effectiveAPIURL, tenant)
	}

	blob, err := ts.Marshal()
	if err != nil {
		return cli.Wrap(cli.ExitGeneral, "encoding OIDC token set", err)
	}
	mgr, err := f.Auth()
	if err != nil {
		return cli.Wrap(cli.ExitGeneral, "initialising auth manager", err)
	}

	// Re-login over an existing OIDC context: best-effort revoke the previous
	// refresh token at the IdP before overwriting it locally, mirroring what
	// logout does (KC-14). Non-fatal — the new login proceeds regardless.
	if prev := cfg.Context(contextName); prev != nil && prev.AuthMethod == config.AuthMethodOIDC && prev.TokenRef != "" {
		RevokeOIDCRefreshToken(io, mgr, prev)
	}

	ref, err := mgr.Set(contextName, blob)
	if err != nil {
		return cli.Wrap(cli.ExitGeneral, "storing OIDC token set", err)
	}

	existing := cfg.Context(contextName)
	ctx := config.Context{
		Name:         contextName,
		APIURL:       apiURL,
		Tenant:       tenant,
		TokenRef:     ref,
		AuthMethod:   config.AuthMethodOIDC,
		User:         auth.EmailFromIDToken(ts.IDToken),
		OIDCBaseURL:  nonDefault(baseURL, build.OIDCBaseURL),
		OIDCClientID: nonDefault(clientID, build.OIDCClientID),
	}
	if existing != nil && ctx.User == "" {
		ctx.User = existing.User
	}
	cfg.SetContext(ctx)

	if opts.setDefault || len(cfg.Contexts) == 1 || cfg.CurrentContext == "" {
		cfg.CurrentContext = contextName
	}

	if err := saveConfig(f, cfg); err != nil {
		return err
	}

	label := tenant
	if t.DisplayName != "" {
		label = fmt.Sprintf("%s (%s)", t.DisplayName, tenant)
	}
	fmt.Fprintln(io.ErrOut)
	if ctx.User != "" {
		fmt.Fprintf(io.ErrOut, "Logged in to tenant %s as %s.\n", label, ctx.User)
	} else {
		fmt.Fprintf(io.ErrOut, "Logged in to tenant %s.\n", label)
	}
	printPlaintextWarningIfFallback(f, ref)
	return nil
}

// printPlaintextWarningIfFallback emits a multi-line warning banner when
// token storage fell back to the plaintext credentials file. Keyring is
// the silent default; this warning only surfaces the fallback case —
// keeping it visually distinct (blank line + WARNING prefix + indented
// guidance) because a plaintext token on disk is a security concern users
// must see, not skim past in normal output.
func printPlaintextWarningIfFallback(f *cli.Factory, ref string) {
	if ref != config.TokenRefPlaintext {
		return
	}
	credsPath := "the plaintext credentials file" //#nosec G101 -- a static fallback label for the warning text, not a credential.
	if cfgPath, err := f.ConfigPath(); err == nil {
		credsPath = auth.DefaultCredentialsPath(cfgPath)
	}
	out := f.IOStreams.ErrOut
	fmt.Fprintln(out)
	fmt.Fprintln(out, "WARNING: OS keyring rejected the credential — fell back to a plaintext file.")
	fmt.Fprintf(out, "         Token written to %s (mode 0600).\n", credsPath)
	fmt.Fprintln(out, "         Investigate the keyring failure (`security` / `secret-tool` / `wincred`)")
	fmt.Fprintln(out, "         OR set KUPE_STORAGE=plaintext to silence this warning if intentional (e.g. CI).")
}

// nonDefault returns v unless it equals the build-time default, in which
// case it returns "" — that keeps the config file uncluttered with values
// users don't need to see.
func nonDefault(v, def string) string {
	if v == def {
		return ""
	}
	return v
}

func saveConfig(f *cli.Factory, cfg *config.Config) error {
	path, err := f.ConfigPath()
	if err != nil {
		return cli.Wrap(cli.ExitGeneral, "resolving config path", err)
	}
	if err := cfg.Save(path); err != nil {
		return cli.Wrap(cli.ExitGeneral, "saving config", err)
	}
	return nil
}

// loginValidationError translates a GetTenant error during login into a
// user-friendly cli.Error. Surfaces the API URL and tenant name in the
// message so a user who misconfigured --api-url sees which endpoint was
// tried, not just "401".
func loginValidationError(err error, apiURL, tenant string) error {
	switch {
	case client.IsUnauthorized(err):
		// Surface kupe-api's own reason: a 401 after a successful device
		// flow is NOT an API-key problem (e.g. "token missing groups claim"
		// from an outdated CLI that didn't request the groups scope), and
		// pointing at the API-keys page sent people the wrong way.
		msg := fmt.Sprintf("kupe-api at %s rejected the credential (401)", apiURL)
		var apiErr *client.APIError
		if errors.As(err, &apiErr) && apiErr.Message != "" {
			msg += ": " + apiErr.Message
		}
		return cli.AuthError(msg).
			WithHint("OIDC login: make sure you run the latest kupe (curl -fsSL https://get.kupe.cloud | sh) — " +
				"older builds don't request the groups scope; API token login: verify the token at https://console.kupe.cloud/settings/api-keys")
	case client.IsForbidden(err):
		return cli.AuthError(fmt.Sprintf("token does not grant access to tenant %q at %s (403)", tenant, apiURL)).
			WithHint("check the tenant name and the token's role")
	case client.IsNotFound(err):
		return cli.NotFoundError(fmt.Sprintf("tenant %q not found at %s", tenant, apiURL)).
			WithHint("check the tenant name spelling and --api-url")
	}
	return cli.Wrap(cli.ExitGeneral, fmt.Sprintf("validating token with %s", apiURL), err)
}

// promptLine prints a prompt to stderr and reads a line from stdin. Trims
// the trailing newline.
func promptLine(io *cli.IOStreams, prompt string) (string, error) {
	fmt.Fprint(io.ErrOut, prompt)
	r := bufio.NewReader(io.In)
	line, err := r.ReadString('\n')
	if err != nil {
		return "", cli.Wrap(cli.ExitGeneral, "reading input", err)
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// promptSecret reads a line from stdin with echo suppressed when stdin is a
// real terminal. Falls back to unbuffered ReadString for tests that pipe
// into bytes.Buffers.
func promptSecret(io *cli.IOStreams, prompt string) (string, error) {
	fmt.Fprint(io.ErrOut, prompt)
	type fder interface{ Fd() uintptr }
	//#nosec G115 -- file descriptors are bounded small ints
	if fd, ok := io.In.(fder); ok && term.IsTerminal(int(fd.Fd())) {
		b, err := term.ReadPassword(int(fd.Fd())) //#nosec G115 -- file descriptors are bounded small ints
		fmt.Fprintln(io.ErrOut)
		if err != nil {
			return "", cli.Wrap(cli.ExitGeneral, "reading password", err)
		}
		return strings.TrimRight(string(b), "\r\n"), nil
	}
	// Non-TTY path: plain line read (testing or unusual pipes).
	return promptLine(io, "")
}
