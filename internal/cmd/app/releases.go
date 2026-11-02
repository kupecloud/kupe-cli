package app

import (
	"cmp"
	"slices"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/printer"
)

func newReleasesCmd(f *cli.Factory) *cobra.Command {
	var output string
	var selectedNumber int64
	cmd := &cobra.Command{Use: "releases APP [ENV [NUMBER]]", Short: "Show retained app release history", Long: `List releases newest first, or show one retained release by number.
When the app has one environment, ENV may be omitted. Use --number N to show one
release with ENV omitted. Pruned releases are absent
from history. Recovery belongs to the failed release and creates no release.`, Args: cobra.RangeArgs(1, 3), RunE: func(cmd *cobra.Command, args []string) error {
		selection := args
		var number int64
		var err error
		if cmd.Flags().Changed("number") {
			if len(args) == 3 {
				return cli.MisuseError("use either a positional release number or --number, not both")
			}
			if selectedNumber <= 0 {
				return cli.MisuseError("release number must be a positive integer")
			}
			number = selectedNumber
		}
		// Environment names may be numeric. A two-argument invocation always
		// names ENV; a single-release lookup keeps both positional names.
		if len(args) == 3 {
			number, err = strconv.ParseInt(args[2], 10, 64)
			if err != nil || number <= 0 {
				return cli.MisuseError("release number must be a positive integer")
			}
			selection = args[:2]
		}
		format, err := printer.Resolve(f, output)
		if err != nil {
			return err
		}
		api, err := f.Client()
		if err != nil {
			return err
		}
		app, env, err := selectEnvironment(cmd.Context(), api, selection)
		if err != nil {
			return err
		}
		items, err := api.ListAppReleases(cmd.Context(), app, env)
		if err != nil {
			return releaseRequestError(err, app, env, false)
		}
		slices.SortStableFunc(items, func(a, b client.AppRelease) int { return cmp.Compare(b.Number, a.Number) })
		if number > 0 {
			for _, item := range items {
				if item.Number == number {
					return printer.AppRelease.One(f.IOStreams, format, &item)
				}
			}
			return cli.NotFoundError("release is not in retained history")
		}
		if items == nil {
			items = []client.AppRelease{}
		}
		return printer.AppRelease.List(f.IOStreams, format, items)
	}}
	cmd.Flags().StringVarP(&output, "output", "o", "", printer.OutputHelpGet)
	cmd.Flags().Int64Var(&selectedNumber, "number", 0, "Show one retained release by number (allows ENV to be omitted)")
	return cmd
}
