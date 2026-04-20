package config

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/config"
)

// Supported key paths for `kupe config get` / `set`. Kept deliberately narrow;
// users who need more should edit the YAML directly or use set-context.
const (
	keyCurrentContext = "currentContext"
	keyPrefOutput     = "preferences.output"
	keyPrefColor      = "preferences.color"
	keyPrefWait       = "preferences.wait"
	keyPrefWaitTO     = "preferences.waitTimeout"
)

func newGetCmd(f *cli.Factory) *cobra.Command {
	return &cobra.Command{
		Use:   "get KEY",
		Short: "Read a single config value by dotted key",
		Long: `Read a single value from the config file.

Supported keys:
  currentContext
  contexts.<name>.apiUrl
  contexts.<name>.tenant
  contexts.<name>.tokenRef
  contexts.<name>.user
  preferences.output
  preferences.color
  preferences.wait
  preferences.waitTimeout`,
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			cfg, err := f.Config()
			if err != nil {
				return cli.Wrap(cli.ExitGeneral, "loading config", err)
			}
			value, err := resolveGet(cfg, args[0])
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(f.IOStreams.Out, value)
			return err
		},
	}
}

func resolveGet(cfg *config.Config, key string) (string, error) {
	switch key {
	case keyCurrentContext:
		return cfg.CurrentContext, nil
	case keyPrefOutput:
		return cfg.Preferences.Output, nil
	case keyPrefColor:
		return cfg.Preferences.Color, nil
	case keyPrefWait:
		if cfg.Preferences.Wait == nil {
			return "", nil
		}
		return fmt.Sprintf("%t", *cfg.Preferences.Wait), nil
	case keyPrefWaitTO:
		return cfg.Preferences.WaitTimeout, nil
	}

	if name, field, ok := parseContextKey(key); ok {
		ctx := cfg.Context(name)
		if ctx == nil {
			return "", cli.NotFoundError(fmt.Sprintf("context %q not found", name))
		}
		switch field {
		case "apiUrl":
			return ctx.APIURL, nil
		case "tenant":
			return ctx.Tenant, nil
		case "tokenRef":
			return ctx.TokenRef, nil
		case "user":
			return ctx.User, nil
		}
	}

	return "", cli.MisuseError(fmt.Sprintf("unknown key %q (run \"kupe config get --help\" for the supported set)", key))
}

// parseContextKey matches "contexts.NAME.FIELD" and returns (NAME, FIELD, true).
func parseContextKey(key string) (name, field string, ok bool) {
	const prefix = "contexts."
	if !strings.HasPrefix(key, prefix) {
		return "", "", false
	}
	rest := key[len(prefix):]
	idx := strings.LastIndex(rest, ".")
	if idx <= 0 || idx == len(rest)-1 {
		return "", "", false
	}
	return rest[:idx], rest[idx+1:], true
}
