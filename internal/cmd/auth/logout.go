package auth

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
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
		if cfg.CurrentContext == "" {
			return cli.NotFoundError("no current context set; pass --context or --all")
		}
		targets = []string{cfg.CurrentContext}
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
