package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"

	"github.com/kupecloud/kupe-cli/internal/build"
	"github.com/kupecloud/kupe-cli/internal/cli"
)

// newManCmd is a hidden helper that emits man pages for every cobra command
// in the tree. The release pipeline (Makefile target / goreleaser before-hook)
// is the primary caller — distro packagers can also run it locally.
//
// Hidden because it's not user-facing UX; users don't generate their own
// man pages. Visible in --help would just clutter the noun list.
func newManCmd(io *cli.IOStreams) *cobra.Command {
	return &cobra.Command{
		Use:    "man <output-dir>",
		Short:  "Generate man pages into <output-dir> (used by the release pipeline)",
		Hidden: true,
		Args:   cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			outDir := args[0]
			if err := os.MkdirAll(outDir, 0o750); err != nil {
				return cli.Wrap(cli.ExitGeneral, "creating man output dir", err)
			}
			// Walk to the root so the manual reflects the full command tree
			// regardless of which command is invoked.
			root := cmd.Root()
			header := &doc.GenManHeader{
				Title:   "KUPE",
				Section: "1",
				Source:  "kupe " + build.Version,
				Manual:  "Kupe CLI Manual",
				// Pin Date so consecutive runs from the same commit produce
				// byte-identical man files (matches the reproducible-build
				// goal documented in docs/distribution.md).
				Date: parseBuildDate(build.Date),
			}
			if err := doc.GenManTree(root, header, outDir); err != nil {
				return cli.Wrap(cli.ExitGeneral, "generating man pages", err)
			}
			abs, _ := filepath.Abs(outDir)
			fmt.Fprintf(io.ErrOut, "Wrote man pages to %s\n", abs)
			return nil
		},
	}
}

// parseBuildDate returns build.Date as a *time.Time when it parses as RFC3339
// (the format goreleaser emits via {{.CommitDate}}); on any failure returns
// nil, letting cobra fall back to time.Now() — which is fine for local
// `make manpages` runs and only matters for reproducibility from CI.
func parseBuildDate(s string) *time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return nil
	}
	return &t
}
