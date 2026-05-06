package cmd

import (
	"encoding/json"
	"fmt"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/build"
	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/printer"
)

type versionInfo struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildDate string `json:"buildDate"`
	GoVersion string `json:"goVersion"`
	Platform  string `json:"platform"`
}

func newVersionCmd(io *cli.IOStreams) *cobra.Command {
	var (
		output string
		short  bool
	)

	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print build, commit, and platform information",
		Long: `Print the kupe CLI version, git commit, build date, and Go runtime information.

For CI scripts, use --short for a bare semver value (e.g. "0.1.0") or
-o json for a structured payload.`,
		Example: `  kupe version              # human-readable line
  kupe version --short      # 0.1.0   (useful as $(kupe version --short))
  kupe version -o json      # {"version": "0.1.0", ...}`,
		RunE: func(_ *cobra.Command, _ []string) error {
			info := versionInfo{
				Version:   build.Version,
				Commit:    build.Commit,
				BuildDate: build.Date,
				GoVersion: runtime.Version(),
				Platform:  runtime.GOOS + "/" + runtime.GOARCH,
			}

			if short && output != "" {
				return cli.MisuseError("--short and --output are mutually exclusive")
			}
			if short {
				_, err := fmt.Fprintln(io.Out, info.Version)
				return err
			}

			switch output {
			case "json":
				enc := json.NewEncoder(io.Out)
				enc.SetIndent("", "  ")
				return enc.Encode(info)
			case "", "text":
				_, err := fmt.Fprintf(io.Out,
					"kupe version %s (commit %s, built %s, %s %s)\n",
					info.Version, info.Commit, info.BuildDate, info.GoVersion, info.Platform,
				)
				return err
			default:
				return cli.MisuseError(fmt.Sprintf("unsupported output format %q (expected one of: text, json)", output))
			}
		},
	}

	cmd.Flags().StringVarP(&output, "output", "o", "", printer.OutputHelpToggle)
	cmd.Flags().BoolVar(&short, "short", false, "Print only the version (e.g. 0.1.0) — useful in shell scripts")
	return cmd
}
