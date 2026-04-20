package auth

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/config"
)

// tenantNameRE loosely validates a tenant name: lowercase DNS-ish label.
// Authoritative validation happens server-side when Phase 2 lands and the
// CLI calls GET /tenants/{tenant} after storing the token.
var tenantNameRE = regexp.MustCompile(`^[a-z][a-z0-9-]{1,61}[a-z0-9]$`)

// tokenPrefix is the API-key prefix kupe-api expects. OIDC tokens (Phase 1.5)
// will also flow through this command but don't carry the prefix.
const tokenPrefix = "kupe_"

type loginOpts struct {
	tenant     string
	token      string
	apiURL     string
	context    string
	setDefault bool
}

func newLoginCmd(f *cli.Factory) *cobra.Command {
	opts := &loginOpts{}

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Authenticate and store an API token for the current or a new context",
		Long: `Authenticate against Kupe by supplying an API token.

Interactive (TTY):
  Prompts for the tenant and token. Token input is hidden.

Scripted:
  Pass --tenant and --token explicitly. If KUPE_API_TOKEN is already set,
  this command refuses to run — the env-var path is already authenticated
  and a login would be a no-op.

Context name defaults to the tenant name. Use --context to override when you
want more than one context per tenant (e.g., two API environments).`,
		Example: `  # Interactive on your laptop
  kupe auth login

  # Scripted bootstrap for a CI machine
  kupe auth login --tenant acme --token kupe_... --context prod --set-default`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runLogin(cmd, f, opts)
		},
	}

	cmd.Flags().StringVar(&opts.tenant, "tenant", "", "Tenant to authenticate against")
	cmd.Flags().StringVar(&opts.token, "token", "", "API token (format kupe_...). If unset, prompt on a TTY.")
	cmd.Flags().StringVar(&opts.apiURL, "api-url", "", "API base URL for this context (default "+config.DefaultAPIURL+")")
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

	// Token: flag > prompt (hidden).
	token := strings.TrimSpace(opts.token)
	if token == "" {
		if !io.PromptsEnabled {
			return cli.MisuseError("--token is required in non-interactive mode")
		}
		got, err := promptSecret(io, "Paste your API token (create one at https://app.kupe.cloud/settings/api-keys):\n  ")
		if err != nil {
			return err
		}
		token = got
	}
	if !strings.HasPrefix(token, tokenPrefix) {
		// Don't fail hard — OIDC JWTs are coming in Phase 1.5 and won't have
		// the prefix. Warn on stderr so accidents are visible.
		fmt.Fprintf(io.ErrOut, "warning: token does not start with %q; proceeding anyway\n", tokenPrefix)
	}

	// Context name defaults to the tenant name.
	contextName := opts.context
	if contextName == "" {
		contextName = tenant
	}

	// Load (or create empty) config.
	cfg, err := f.Config()
	if err != nil {
		return cli.Wrap(cli.ExitGeneral, "loading config", err)
	}

	apiURL := opts.apiURL
	if apiURL == "" {
		// Inherit from the existing context if present, else leave empty
		// (Resolve will apply the default).
		if existing := cfg.Context(contextName); existing != nil {
			apiURL = existing.APIURL
		}
	}

	// Resolve effective API URL once (defaulting kicks in here so we can
	// validate against the correct endpoint before persisting).
	effectiveAPIURL := apiURL
	if effectiveAPIURL == "" {
		effectiveAPIURL = config.DefaultAPIURL
	}

	// Validate the token before persisting anything. A bad token should not
	// leave stale state on disk or in the keyring.
	api := client.New(effectiveAPIURL, tenant, token, cli.UserAgent())
	t, _, err := api.GetTenant(cmd.Context())
	if err != nil {
		return loginValidationError(err, effectiveAPIURL, tenant)
	}

	// Store the token only after server-side validation succeeded.
	mgr, err := f.Auth()
	if err != nil {
		return cli.Wrap(cli.ExitGeneral, "initialising auth manager", err)
	}
	ref, err := mgr.Set(contextName, token)
	if err != nil {
		return cli.Wrap(cli.ExitGeneral, "storing token", err)
	}

	// Update (or create) the context.
	existing := cfg.Context(contextName)
	ctx := config.Context{
		Name:     contextName,
		APIURL:   apiURL,
		Tenant:   tenant,
		TokenRef: ref,
	}
	if existing != nil {
		ctx.User = existing.User // preserved; OIDC phase populates this from JWT
	}
	cfg.SetContext(ctx)

	// Set currentContext when requested, or when this is the only context.
	if opts.setDefault || len(cfg.Contexts) == 1 || cfg.CurrentContext == "" {
		cfg.CurrentContext = contextName
	}

	path, err := f.ConfigPath()
	if err != nil {
		return cli.Wrap(cli.ExitGeneral, "resolving config path", err)
	}
	if err := cfg.Save(path); err != nil {
		return cli.Wrap(cli.ExitGeneral, "saving config", err)
	}

	label := tenant
	if t.DisplayName != "" {
		label = fmt.Sprintf("%s (%s)", t.DisplayName, tenant)
	}
	fmt.Fprintf(io.ErrOut, "Logged in to tenant %s.\n", label)
	fmt.Fprintf(io.ErrOut, "  Context %q saved (%s storage).", contextName, ref)
	if cfg.CurrentContext == contextName {
		fmt.Fprint(io.ErrOut, " Set as current.")
	}
	fmt.Fprintln(io.ErrOut)
	return nil
}

// loginValidationError translates a GetTenant error during login into a
// user-friendly cli.Error. Surfaces the API URL and tenant name in the
// message so a user who misconfigured --api-url sees which endpoint was
// tried, not just "401".
func loginValidationError(err error, apiURL, tenant string) error {
	switch {
	case client.IsUnauthorized(err):
		return cli.AuthError(fmt.Sprintf("invalid API token at %s (server returned 401)", apiURL)).
			WithHint("verify the token at https://app.kupe.cloud/settings/api-keys")
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
