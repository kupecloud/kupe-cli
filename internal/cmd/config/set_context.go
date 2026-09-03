package config

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/config"
)

type setContextOpts struct {
	apiURL    string
	signupURL string
	tenant    string
	token     string
}

func newSetContextCmd(f *cli.Factory) *cobra.Command {
	opts := &setContextOpts{}

	cmd := &cobra.Command{
		Use:   "set-context NAME",
		Short: "Create or update a context",
		Long: `Create a new context or update specific fields of an existing one.

Only the flags passed are applied; other fields are left untouched.
Use "kupe auth login" as the higher-level path for creating a context with
a token in one step.`,
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			name := args[0]
			cfg, err := f.Config()
			if err != nil {
				return cli.Wrap(cli.ExitGeneral, "loading config", err)
			}

			ctx := cfg.Context(name)
			if ctx == nil {
				ctx = &config.Context{Name: name}
			}
			if opts.apiURL != "" {
				ctx.APIURL = opts.apiURL
			}
			if opts.signupURL != "" {
				ctx.SignupURL = config.NormalizeURL(opts.signupURL)
			}
			if opts.tenant != "" {
				ctx.Tenant = opts.tenant
			}
			if opts.token != "" {
				mgr, err := f.Auth()
				if err != nil {
					return cli.Wrap(cli.ExitGeneral, "initialising auth manager", err)
				}
				ref, err := mgr.Set(name, opts.token)
				if err != nil {
					return cli.Wrap(cli.ExitGeneral, "storing token", err)
				}
				ctx.TokenRef = ref
			}
			cfg.SetContext(*ctx)

			path, err := f.ConfigPath()
			if err != nil {
				return cli.Wrap(cli.ExitGeneral, "resolving config path", err)
			}
			if err := cfg.Save(path); err != nil {
				return cli.Wrap(cli.ExitGeneral, "saving config", err)
			}
			fmt.Fprintf(f.IOStreams.ErrOut, "Context %q updated.\n", name)
			return nil
		},
	}

	cmd.Flags().StringVar(&opts.apiURL, "api-url", "", "API base URL")
	cmd.Flags().StringVar(&opts.signupURL, "signup-url", "", "Signup service base URL used by \"kupe user delete\" (env: KUPE_SIGNUP_URL, default "+config.DefaultSignupURL+")")
	cmd.Flags().StringVar(&opts.tenant, "tenant", "", "Tenant")
	cmd.Flags().StringVar(&opts.token, "token", "", "API token to store")
	return cmd
}
