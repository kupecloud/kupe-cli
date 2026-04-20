package config

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/config"
)

func newSetCmd(f *cli.Factory) *cobra.Command {
	return &cobra.Command{
		Use:   "set KEY VALUE",
		Short: "Write a single config value by dotted key",
		Long: `Set a single config value. Accepted keys match "kupe config get".
Tokens cannot be set via this command — use "kupe auth login" or
"kupe config set-context --token" so the secret goes to the right backend.`,
		Args: cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			key, value := args[0], args[1]

			cfg, err := f.Config()
			if err != nil {
				return cli.Wrap(cli.ExitGeneral, "loading config", err)
			}
			if err := applySet(cfg, key, value); err != nil {
				return err
			}
			path, err := f.ConfigPath()
			if err != nil {
				return cli.Wrap(cli.ExitGeneral, "resolving config path", err)
			}
			if err := cfg.Save(path); err != nil {
				return cli.Wrap(cli.ExitGeneral, "saving config", err)
			}
			fmt.Fprintf(f.IOStreams.ErrOut, "Set %s = %s.\n", key, value)
			return nil
		},
	}
}

func applySet(cfg *config.Config, key, value string) error {
	switch key {
	case keyCurrentContext:
		if cfg.Context(value) == nil {
			return cli.NotFoundError(fmt.Sprintf("context %q not found", value))
		}
		cfg.CurrentContext = value
		return nil
	case keyPrefOutput:
		cfg.Preferences.Output = value
		return nil
	case keyPrefColor:
		cfg.Preferences.Color = value
		return nil
	case keyPrefWait:
		b, err := strconv.ParseBool(value)
		if err != nil {
			return cli.MisuseError(fmt.Sprintf("invalid boolean %q for preferences.wait", value))
		}
		cfg.Preferences.Wait = &b
		return nil
	case keyPrefWaitTO:
		cfg.Preferences.WaitTimeout = value
		return nil
	}

	if name, field, ok := parseContextKey(key); ok {
		ctx := cfg.Context(name)
		if ctx == nil {
			return cli.NotFoundError(fmt.Sprintf("context %q not found (use \"kupe config set-context %s\")", name, name))
		}
		switch field {
		case "apiUrl":
			ctx.APIURL = value
			return nil
		case "tenant":
			ctx.Tenant = value
			return nil
		case "user":
			ctx.User = value
			return nil
		case "tokenRef":
			return cli.MisuseError("use \"kupe auth login\" or \"kupe config set-context --token\" to change tokenRef safely")
		}
	}

	return cli.MisuseError(fmt.Sprintf("unknown or immutable key %q", key))
}
