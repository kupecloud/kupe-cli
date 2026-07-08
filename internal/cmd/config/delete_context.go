package config

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
	authcmd "github.com/kupecloud/kupe-cli/internal/cmd/auth"
	"github.com/kupecloud/kupe-cli/internal/config"
)

func newDeleteContextCmd(f *cli.Factory) *cobra.Command {
	var yes bool

	cmd := &cobra.Command{
		Use:   "delete-context NAME",
		Short: "Remove a context and its stored token",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			name := args[0]
			io := f.IOStreams

			cfg, err := f.Config()
			if err != nil {
				return cli.Wrap(cli.ExitGeneral, "loading config", err)
			}
			ctx := cfg.Context(name)
			if ctx == nil {
				return cli.NotFoundError(fmt.Sprintf("context %q not found", name))
			}

			if err := cli.ConfirmDelete(io, yes, fmt.Sprintf("context (tenant %s)", ctx.Tenant), name); err != nil {
				return err
			}

			mgr, err := f.Auth()
			if err != nil {
				return cli.Wrap(cli.ExitGeneral, "initialising auth manager", err)
			}
			// For OIDC contexts, best-effort revoke the refresh token at the IdP
			// before dropping local state — otherwise a still-valid 30-day
			// refresh token stays live with no local copy left to revoke,
			// inconsistent with the logout/re-login posture (LOW-2). Non-fatal.
			if ctx.AuthMethod == config.AuthMethodOIDC {
				authcmd.RevokeOIDCRefreshToken(io, mgr, ctx)
			}
			if err := mgr.DeleteByRef(name, ctx.TokenRef); err != nil {
				return cli.Wrap(cli.ExitGeneral, "removing token", err)
			}
			cfg.RemoveContext(name)

			path, err := f.ConfigPath()
			if err != nil {
				return cli.Wrap(cli.ExitGeneral, "resolving config path", err)
			}
			if err := cfg.Save(path); err != nil {
				return cli.Wrap(cli.ExitGeneral, "saving config", err)
			}
			fmt.Fprintf(io.ErrOut, "Context %q removed.\n", name)
			return nil
		},
	}

	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip the interactive confirmation")
	return cmd
}
