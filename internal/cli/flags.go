package cli

import "github.com/spf13/cobra"

// GlobalFlags is the struct of persistent flags bound on the root command
// and inherited by every subcommand. The values are populated by Cobra
// during flag parsing; the Factory reads them at command-run time.
//
// -o / --output is deliberately NOT a global flag. Each command declares
// its own local -o because the set of supported formats differs per
// command (e.g. `auth get-token` only emits JSON; table-printable
// commands support table/wide/json/yaml/name). A persistent root -o
// would be silently shadowed by the local -o and confuse users who set
// both. Commands that want a configured default for -o resolve
// preferences.output via the Factory — see factory.DefaultOutput().
type GlobalFlags struct {
	APIURL     string
	Token      string
	Tenant     string
	Context    string
	ConfigPath string
	NoColor    bool
	Quiet      bool
	Verbose    bool
}

// Bind registers every global flag on cmd's persistent-flag set. Called once
// from Execute during root-command construction.
func (g *GlobalFlags) Bind(cmd *cobra.Command) {
	cmd.PersistentFlags().StringVar(&g.APIURL, "api-url", "", "Kupe API base URL (env: KUPE_API_URL)")
	cmd.PersistentFlags().StringVar(&g.Token, "token", "", "API token; bypasses config (env: KUPE_API_TOKEN)")
	cmd.PersistentFlags().StringVar(&g.Tenant, "tenant", "", "Tenant to target (env: KUPE_TENANT)")
	cmd.PersistentFlags().StringVar(&g.Context, "context", "", "Named context from the config file (env: KUPE_CONTEXT)")
	cmd.PersistentFlags().StringVar(&g.ConfigPath, "config", "", "Config file path (default ~/.config/kupe/config.yaml; env: KUPE_CONFIG)")
	cmd.PersistentFlags().BoolVar(&g.NoColor, "no-color", false, "Disable ANSI color (also auto-disabled on non-TTY; honours NO_COLOR)")
	cmd.PersistentFlags().BoolVarP(&g.Quiet, "quiet", "q", false, "Suppress status, progress, and non-essential stderr output")
	cmd.PersistentFlags().BoolVarP(&g.Verbose, "verbose", "v", false, "Enable debug logging to stderr")
}
