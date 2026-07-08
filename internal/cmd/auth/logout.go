package auth

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/auth"
	"github.com/kupecloud/kupe-cli/internal/build"
	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/config"
)

type logoutOpts struct {
	context string
	all     bool
}

func newLogoutCmd(f *cli.Factory) *cobra.Command {
	opts := &logoutOpts{}

	cmd := &cobra.Command{
		Use:   "logout",
		Short: "Remove stored credentials for a context",
		Long: `Delete the stored token for one or all contexts.

Without flags, logs out of the current context. The context itself is kept
in the config file (its tokenRef is cleared) so re-login is a single command.
Use "kupe config delete-context" to remove a context entirely.`,
		Example: `  # Log out of the current context
  kupe auth logout

  # Log out of a specific context
  kupe auth logout --context staging

  # Log out of every context
  kupe auth logout --all`,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runLogout(f, opts)
		},
	}

	cmd.Flags().StringVar(&opts.context, "context", "", "Context to log out of (default: current)")
	cmd.Flags().BoolVar(&opts.all, "all", false, "Log out of every context")

	return cmd
}

func runLogout(f *cli.Factory, opts *logoutOpts) error {
	io := f.IOStreams

	cfg, err := f.Config()
	if err != nil {
		return cli.Wrap(cli.ExitGeneral, "loading config", err)
	}
	mgr, err := f.Auth()
	if err != nil {
		return cli.Wrap(cli.ExitGeneral, "initialising auth manager", err)
	}

	var targets []string
	switch {
	case opts.all:
		for _, ctx := range cfg.Contexts {
			targets = append(targets, ctx.Name)
		}
	case opts.context != "":
		if cfg.Context(opts.context) == nil {
			return cli.NotFoundError(fmt.Sprintf("context %q not found", opts.context))
		}
		targets = []string{opts.context}
	default:
		// Resolve through the same flag > KUPE_CONTEXT > config.CurrentContext
		// chain every other command uses, so `KUPE_CONTEXT=staging kupe auth
		// logout` doesn't silently log out of the config's currentContext
		// (KC-12).
		target := cfg.CurrentContext
		if r, rerr := f.Resolved(); rerr == nil && r != nil && r.ContextName != "" {
			target = r.ContextName
		}
		if target == "" {
			return cli.NotFoundError("no current context set; pass --context or --all")
		}
		if cfg.Context(target) == nil {
			return cli.NotFoundError(fmt.Sprintf("context %q not found", target))
		}
		targets = []string{target}
	}

	if len(targets) == 0 {
		fmt.Fprintln(io.ErrOut, "No contexts to log out of.")
		return nil
	}

	for _, name := range targets {
		ctx := cfg.Context(name)
		if ctx == nil {
			continue
		}
		// For OIDC contexts, best-effort revoke the refresh token at the
		// IdP before deleting local state. Failures (network, IdP without
		// a revocation endpoint, already-revoked tokens) print a warning
		// but never block local logout — the user expects `auth logout`
		// to always succeed locally.
		if ctx.AuthMethod == config.AuthMethodOIDC {
			RevokeOIDCRefreshToken(io, mgr, ctx)
		}
		if err := mgr.DeleteByRef(name, ctx.TokenRef); err != nil {
			return cli.Wrap(cli.ExitGeneral, fmt.Sprintf("removing token for %q", name), err)
		}
		ctx.TokenRef = ""
		fmt.Fprintf(io.ErrOut, "Logged out of %q.\n", name)
	}

	path, err := f.ConfigPath()
	if err != nil {
		return cli.Wrap(cli.ExitGeneral, "resolving config path", err)
	}
	if err := cfg.Save(path); err != nil {
		return cli.Wrap(cli.ExitGeneral, "saving config", err)
	}
	return nil
}

// RevokeOIDCRefreshToken loads the stored OIDC blob, calls the IdP's
// revocation endpoint (RFC 7009), and prints a warning on failure. It never
// blocks the caller's local cleanup — local logout/delete proceeds regardless.
// Exported so `config delete-context` can revoke before dropping the local
// credential, matching the logout/re-login posture (LOW-2).
func RevokeOIDCRefreshToken(io *cli.IOStreams, mgr *auth.Manager, ctx *config.Context) {
	stored, err := mgr.GetByRef(ctx.Name, ctx.TokenRef)
	if err != nil {
		// Token already gone (e.g. keyring deleted manually) — nothing to
		// revoke. Silent.
		return
	}
	if !auth.IsOIDCBlob(stored) {
		// Defensive: AuthMethod says oidc but storage isn't an OIDC blob.
		// Skip revoke; local cleanup will still happen.
		return
	}
	ts, err := auth.UnmarshalOIDC(stored)
	if err != nil || ts.RefreshToken == "" {
		return
	}

	baseURL := config.FirstNonEmpty(ctx.OIDCBaseURL, build.OIDCBaseURL)
	clientID := config.FirstNonEmpty(ctx.OIDCClientID, build.OIDCClientID)
	issuer := config.BuildIssuerURL(baseURL, clientID)

	// Bound the revocation call so a slow IdP doesn't hang logout.
	rctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := auth.Revoke(rctx, issuer, clientID, ts.RefreshToken, "refresh_token"); err != nil {
		fmt.Fprintf(io.ErrOut, "  Note: refresh token revocation at %s failed (%v) — local logout proceeds.\n", issuer, err)
	}
}
