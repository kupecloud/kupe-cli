package secret

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
)

func newPurgeCmd(f *cli.Factory) *cobra.Command {
	var path string
	var yes bool
	var version int64
	cmd := &cobra.Command{Use: "purge --path PATH [--yes]", Short: "Permanently delete every version of a Vault entry", RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) != 0 {
			return cli.MisuseError("purge takes --path PATH, not a definition name")
		}
		if err := validateVaultPath(path); err != nil {
			return err
		}
		if cmd.Flags().Changed("expected-version") && version <= 0 {
			return cli.MisuseError("--expected-version must be positive for purge")
		}
		api, err := f.Client()
		if err != nil {
			return err
		}
		entry, err := api.GetVaultEntry(cmd.Context(), path)
		if err != nil {
			return translateVaultErr(err, path, version, false)
		}
		if len(entry.References) > 0 {
			return cli.ConflictError("cannot purge " + path + "; used by secret definitions: " + strings.Join(entry.References, ", "))
		}
		if !cmd.Flags().Changed("expected-version") {
			version = entry.Version
		}
		fmt.Fprintf(f.IOStreams.ErrOut, "permanently deletes every version of %s; this cannot be undone\n", path)
		if err := cli.ConfirmDeleteByName(f.IOStreams, yes, "Vault entry", path); err != nil {
			return err
		}
		if err := api.PurgeVaultEntry(cmd.Context(), path, version); err != nil {
			return translateVaultErr(err, path, version, true)
		}
		fmt.Fprintf(f.IOStreams.Out, "secret values at %s permanently deleted\n", path)
		return nil
	}}
	cmd.Flags().StringVar(&path, "path", "", "Vault path to purge (required)")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip typed confirmation")
	cmd.Flags().Int64Var(&version, "expected-version", 0, "Supply the script's reviewed version instead of the version just read")
	return cmd
}
