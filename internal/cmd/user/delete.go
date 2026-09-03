package user

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/config"
)

type deleteOpts struct {
	confirm string
}

func newDeleteCmd(f *cli.Factory) *cobra.Command {
	opts := &deleteOpts{}

	cmd := &cobra.Command{
		Use:   "delete --confirm EMAIL",
		Short: "Permanently delete your own Kupe user account",
		Long: `Erase the user you are logged in as: the Authentik account, its sessions
and tokens, and the Grafana user row. This cannot be undone.

--confirm must equal the logged-in email ("kupe auth whoami"). The account
must not belong to any tenant: delete the tenants you own ("kupe tenant
delete") or have an admin remove you from the others first — the service
refuses otherwise and lists them.

Requires an OIDC login (API keys are refused) and a recent authentication;
if the service says the login is stale, run "kupe auth login" and retry.
On success the CLI also removes the stored credentials for the current
context, equivalent to "kupe auth logout".`,
		Example: `  kupe user delete --confirm billy@acme.com`,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDelete(cmd.Context(), f, opts)
		},
	}

	cmd.Flags().StringVar(&opts.confirm, "confirm", "", "Your email address, typed out to confirm (required; must match \"kupe auth whoami\")")
	return cmd
}

func runDelete(ctx context.Context, f *cli.Factory, opts *deleteOpts) error {
	io := f.IOStreams

	r, err := f.Resolved()
	if err != nil {
		return cli.Wrap(cli.ExitGeneral, "resolving context", err)
	}
	if cli.IsAPIKeyAuth(r) {
		return cli.AuthError("deleting your user requires an OIDC login; API keys cannot delete a user").
			WithHint("unset KUPE_API_TOKEN if set, then run \"kupe auth login\" (device-code login) and retry")
	}

	confirm := strings.TrimSpace(opts.confirm)
	if confirm == "" {
		return cli.MisuseError("--confirm is required: type your email address to confirm deletion").
			WithHint("kupe auth whoami   shows the logged-in user")
	}

	// The stored context (when the token came from it, not from --token /
	// KUPE_API_TOKEN) knows the login email and is what gets logged out on
	// success.
	var (
		cfg     *config.Config
		current *config.Context
	)
	if r.DirectToken == "" {
		cfg, err = f.Config()
		if err != nil {
			return cli.Wrap(cli.ExitGeneral, "loading config", err)
		}
		current = cfg.Context(r.ContextName)
	}
	known := ""
	if current != nil {
		known = current.User
	}
	switch {
	case known != "" && !strings.EqualFold(confirm, known):
		return cli.MisuseError(fmt.Sprintf("--confirm %q does not match the logged-in user %q (kupe auth whoami)", confirm, known))
	case known == "":
		fmt.Fprintln(io.ErrOut, "note: the logged-in email is not recorded locally; the signup service will check --confirm against your token.")
	}

	signup, err := f.SignupClient()
	if err != nil {
		return err
	}

	fmt.Fprintf(io.ErrOut, "\nDeleting user %q will:\n", confirm)
	fmt.Fprintln(io.ErrOut, "  - Permanently remove the account, its sessions and tokens, and its Grafana user.")
	fmt.Fprintln(io.ErrOut, "  - Log this CLI out of the current context.")
	fmt.Fprintln(io.ErrOut)

	if err := signup.DeleteUser(ctx, client.DeleteUserRequest{Confirm: confirm}); err != nil {
		return mapDeleteErr(err)
	}

	// Local logout: the account is gone, so the stored credential is dead
	// weight (and the refresh token would only 401 at the IdP — no revoke
	// call is attempted).
	if current != nil && current.TokenRef != "" {
		mgr, err := f.Auth()
		if err != nil {
			return cli.Wrap(cli.ExitGeneral, "initialising auth manager", err)
		}
		if err := mgr.DeleteByRef(current.Name, current.TokenRef); err != nil {
			return cli.Wrap(cli.ExitGeneral, fmt.Sprintf("removing stored credentials for %q", current.Name), err)
		}
		current.TokenRef = ""
		path, err := f.ConfigPath()
		if err != nil {
			return cli.Wrap(cli.ExitGeneral, "resolving config path", err)
		}
		if err := cfg.Save(path); err != nil {
			return cli.Wrap(cli.ExitGeneral, "saving config", err)
		}
		fmt.Fprintf(io.ErrOut, "Logged out of %q.\n", current.Name)
	}

	fmt.Fprintf(io.Out, "user/%s deleted\n", confirm)
	return nil
}

// mapDeleteErr translates signup's refusals into typed CLI errors. The 409
// carries the tenants still holding the user; everything else keeps the
// client's default exit mapping.
func mapDeleteErr(err error) error {
	var memberships *client.TenantMembershipsError
	switch {
	case errors.As(err, &memberships):
		msg := "your user still belongs to one or more tenants"
		if len(memberships.Tenants) > 0 {
			msg = fmt.Sprintf("your user still belongs to %d tenant(s): %s", len(memberships.Tenants), strings.Join(memberships.Tenants, ", "))
		}
		return cli.Wrap(cli.ExitConflict, msg, memberships.Err).
			WithHint("delete each tenant you own (kupe tenant delete --confirm NAME) or ask an admin to remove you (kupe member remove), then retry")
	case client.IsForbidden(err):
		return cli.Wrap(cli.ExitAuth, "the signup service refused to delete your user", err).
			WithHint("user deletion needs a recent OIDC login by the account owner; run \"kupe auth login\" again and retry within 10 minutes")
	case client.IsRateLimited(err):
		return cli.Wrap(cli.ExitRateLimited, "user deletion is rate limited (one request per minute)", err).
			WithHint("wait a minute and retry")
	case client.IsValidation(err):
		return cli.Wrap(cli.ExitMisuse, "the signup service rejected the confirmation", err).
			WithHint("--confirm must equal the email on your login exactly (kupe auth whoami)")
	}
	return err
}
