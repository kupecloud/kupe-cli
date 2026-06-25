package cluster

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/printer"
)

func newListCmd(f *cli.Factory) *cobra.Command {
	var output string
	var phase string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List clusters in the current tenant",
		Long: `List every cluster visible to the current tenant.

Output defaults to a human table; use -o name for pipe-friendly output,
or -o json / -o yaml / -o go-template=... for machine consumption.`,
		Example: `  kupe cluster list
  kupe cluster list -o wide
  kupe cluster list -o name | xargs -I{} kupe cluster delete {} --yes
  kupe cluster list --phase Running`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			pf, err := printer.Resolve(f, output)
			if err != nil {
				return err
			}
			api, err := f.Client()
			if err != nil {
				return err
			}
			list, err := api.ListClusters(cmd.Context())
			if err != nil {
				return err
			}

			if phase != "" {
				// Normalise so --phase is case-insensitive and accepts the same
				// friendly spellings as `cluster wait --for` (KC-13). An unknown
				// value is a misuse, not a silently-empty result.
				want := normalizePhase(phase)
				if want == "" || want == "Deleted" {
					return cli.MisuseError(fmt.Sprintf("unknown --phase %q", phase)).
						WithHint("valid phases: pending | provisioning | running | upgrading | degraded | terminating")
				}
				filtered := list[:0]
				for _, c := range list {
					if phaseOf(&c) == want { //nolint:gosec // range-var copy intentional for value semantics
						filtered = append(filtered, c)
					}
				}
				list = filtered
			}

			return renderList(f.IOStreams.Out, f.IOStreams.ColorEnabled, pf, list)
		},
	}

	cmd.Flags().StringVarP(&output, "output", "o", "", printer.OutputHelpList)
	cmd.Flags().StringVar(&phase, "phase", "", "Filter to clusters in this phase (client-side)")
	return cmd
}
