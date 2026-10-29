package secret

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/printer"
)

func newValuesCmd(f *cli.Factory) *cobra.Command {
	cmd := &cobra.Command{Use: "values", Short: "Read entry metadata or replace secret values"}
	cmd.AddCommand(newValuesGetCmd(f), newValuesSetCmd(f))
	return cmd
}

func newValuesGetCmd(f *cli.Factory) *cobra.Command {
	var path, output string
	cmd := &cobra.Command{Use: "get (NAME | --path PATH)", Short: "Read a Vault entry's metadata, never its values", RunE: func(cmd *cobra.Command, args []string) error {
		if err := validateVaultTarget(args, path); err != nil {
			return err
		}
		format, err := printer.Resolve(f, output)
		if err != nil {
			return err
		}
		api, err := f.Client()
		if err != nil {
			return err
		}
		resolved, err := resolveVaultPath(cmd.Context(), api, args, path)
		if err != nil {
			return err
		}
		entry, err := api.GetVaultEntry(cmd.Context(), resolved)
		if err != nil {
			return translateVaultErr(err, resolved, 0, false)
		}
		return printer.RenderOne(f.IOStreams.Out, format, entry, printer.VaultEntryColumns(), func(e *client.VaultEntry) string { return e.Path })
	}}
	cmd.Flags().StringVar(&path, "path", "", "Vault path instead of a secret definition name")
	cmd.Flags().StringVarP(&output, "output", "o", "", printer.OutputHelpGet)
	return cmd
}

func newValuesSetCmd(f *cli.Factory) *cobra.Command {
	var path, file, output string
	var stdin bool
	var version int64
	cmd := &cobra.Command{Use: "set (NAME | --path PATH) --expected-version N (--file FILE | --stdin)", Short: "Replace secret values with a version check", Long: `Replace all values at a Vault path. Keys omitted from the input are removed.
Read a JSON object of string values from a file or stdin, up to 512 KiB.
Use version 0 to create an entry only if it does not exist. No values are shown.`, Example: `  kupe secret values set orders --expected-version 3 --file values.json
  kupe secret values set --path team/orders --expected-version 0 --stdin <<'JSON'
  {"FIELD_A": "synthetic-example"}
  JSON`, RunE: func(cmd *cobra.Command, args []string) error {
		if err := validateVaultTarget(args, path); err != nil {
			return err
		}
		if !cmd.Flags().Changed("expected-version") || version < 0 {
			return cli.MisuseError("--expected-version is required and must be non-negative")
		}
		if (file != "") == stdin {
			return cli.MisuseError("provide exactly one of --file FILE or --stdin")
		}
		format, err := printer.Resolve(f, output)
		if err != nil {
			return err
		}
		input := f.IOStreams.In
		if file != "" {
			handle, openErr := os.Open(file) // #nosec G304 -- the user explicitly selects this local input file
			if openErr != nil {
				return cli.MisuseError("could not open values file")
			}
			defer func() { _ = handle.Close() }() // Read-only input; all read errors are handled below.
			input = handle
		}
		values, err := decodeVaultValues(input)
		if err != nil {
			return err
		}
		api, err := f.Client()
		if err != nil {
			return err
		}
		resolved, err := resolveVaultPath(cmd.Context(), api, args, path)
		if err != nil {
			return err
		}
		result, err := api.PutVaultValues(cmd.Context(), resolved, values, version)
		if err != nil {
			return translateVaultErr(err, resolved, version, false)
		}
		if len(result.RefreshRequested) > 0 {
			fmt.Fprintf(f.IOStreams.ErrOut, "Refresh requested for %s. Check delivery with:\n", strings.Join(result.RefreshRequested, ", "))
			for _, name := range result.RefreshRequested {
				fmt.Fprintf(f.IOStreams.ErrOut, "  kupe secret wait %s --for delivered\n", name)
			}
		}
		if format.Kind == printer.Table || format.Kind == printer.Wide {
			fmt.Fprintf(f.IOStreams.Out, "secret values at %s saved as version %d\n", result.Path, result.Version)
			return nil
		}
		return printer.RenderOne(f.IOStreams.Out, format, result, nil, func(r *client.VaultWriteResult) string { return r.Path })
	}}
	cmd.Flags().StringVar(&path, "path", "", "Vault path instead of a secret definition name")
	cmd.Flags().StringVar(&file, "file", "", "Read values from this JSON file")
	cmd.Flags().BoolVar(&stdin, "stdin", false, "Read values as JSON from stdin")
	cmd.Flags().Int64Var(&version, "expected-version", 0, "Expected version; 0 creates an entry only if absent (required)")
	cmd.Flags().StringVarP(&output, "output", "o", "", printer.OutputHelpGet)
	return cmd
}
