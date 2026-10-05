// Package cmd wires the Cobra command tree for the kupe CLI. Every subcommand
// lives in its own subpackage; this package owns the root command, global-flag
// binding, and the Execute entrypoint called from main.
package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/build"
	"github.com/kupecloud/kupe-cli/internal/cli"
	apikeycmd "github.com/kupecloud/kupe-cli/internal/cmd/apikey"
	authcmd "github.com/kupecloud/kupe-cli/internal/cmd/auth"
	clustercmd "github.com/kupecloud/kupe-cli/internal/cmd/cluster"
	configcmd "github.com/kupecloud/kupe-cli/internal/cmd/config"
	invoicecmd "github.com/kupecloud/kupe-cli/internal/cmd/invoice"
	membercmd "github.com/kupecloud/kupe-cli/internal/cmd/member"
	plancmd "github.com/kupecloud/kupe-cli/internal/cmd/plan"
	secretcmd "github.com/kupecloud/kupe-cli/internal/cmd/secret"
	tenantcmd "github.com/kupecloud/kupe-cli/internal/cmd/tenant"
	usercmd "github.com/kupecloud/kupe-cli/internal/cmd/user"
	"github.com/kupecloud/kupe-cli/internal/config"
	"github.com/kupecloud/kupe-cli/internal/update"
)

// Execute runs the root command with the given context and returns the exit
// code. The caller (main) passes this to os.Exit.
func Execute(ctx context.Context) int {
	io := cli.System()
	flags := &cli.GlobalFlags{}
	root := newRootCmd(io, flags)

	err := root.ExecuteContext(ctx)

	// Version notice, after the command has run (M-11). Deliberately last and
	// deliberately silent on failure — see internal/update. Gated on
	// SpinnersEnabled, which is already "stderr is a TTY, not CI, not --quiet,
	// no KUPE_NO_PROGRESS", so scripts and CI logs never see it.
	if notice := updateNotice(ctx, io); notice != "" {
		fmt.Fprintf(io.ErrOut, "\n%s\n", notice)
	}

	if err != nil {
		// Cobra has already surfaced the error message; add a hint block for
		// any *cli.Error that carries one. Multi-line hints are split and
		// each line is indented so the block reads as a coherent group
		// rather than wrapping into one wall of text.
		var e *cli.Error
		if errors.As(err, &e) && e.Hint != "" {
			for _, line := range strings.Split(e.Hint, "\n") {
				fmt.Fprintf(io.ErrOut, "  %s\n", line)
			}
		}
	}
	return cli.ExitCode(err)
}

// updateNotice returns the "newer version available" message, or "" when the
// check is suppressed or has nothing to report. It resolves the cache path from
// the config location so one directory holds everything the CLI writes; an
// unresolvable path disables caching rather than the check.
func updateNotice(ctx context.Context, io *cli.IOStreams) string {
	if !io.SpinnersEnabled {
		return ""
	}
	if os.Getenv("KUPE_NO_UPDATE_CHECK") != "" {
		return ""
	}
	cachePath := ""
	if configPath, err := config.DefaultPath(); err == nil {
		cachePath = update.DefaultCachePath(configPath)
	}
	checker := &update.Checker{CachePath: cachePath}
	return checker.Notice(ctx, build.Version)
}

func newRootCmd(io *cli.IOStreams, flags *cli.GlobalFlags) *cobra.Command {
	root := &cobra.Command{
		Use:   "kupe",
		Short: "Official CLI for Kupe Cloud",
		Long: `kupe is the command-line interface for Kupe Cloud.

Run "kupe auth login" to get started, then "kupe cluster create NAME"
to provision a cluster.

Kupe is in alpha: free to use, no SLA, and nothing is charged.
What that means: https://kupe.cloud/alpha

Full reference: https://docs.kupe.cloud/platform/kupe-cli/`,
		Version:       fmt.Sprintf("%s (commit %s, built %s)", build.Version, build.Commit, build.Date),
		SilenceUsage:  true,
		SilenceErrors: false,

		PersistentPreRunE: func(_ *cobra.Command, _ []string) error {
			// Apply flag overrides to IOStreams. Construction-time defaults
			// already honour env vars (NO_COLOR, CI); flags trump both.
			if flags.NoColor {
				io.SetColorEnabled(false)
			}
			if flags.Quiet {
				io.SetSpinnersEnabled(false)
			}
			return nil
		},
	}

	flags.Bind(root)
	root.SetHelpTemplate(helpTemplate)

	factory := cli.NewFactory(io, flags)

	root.AddCommand(newVersionCmd(io))
	root.AddCommand(newCompletionCmd())
	root.AddCommand(newManCmd(io))
	root.AddCommand(authcmd.NewCmd(factory))
	root.AddCommand(configcmd.NewCmd(factory))
	root.AddCommand(clustercmd.NewCmd(factory))
	root.AddCommand(apikeycmd.NewCmd(factory))
	root.AddCommand(secretcmd.NewCmd(factory))
	root.AddCommand(membercmd.NewCmd(factory))
	root.AddCommand(tenantcmd.NewCmd(factory))
	root.AddCommand(invoicecmd.NewCmd(factory))
	root.AddCommand(plancmd.NewCmd(factory))
	root.AddCommand(usercmd.NewCmd(factory))

	root.SetOut(os.Stdout)
	root.SetErr(os.Stderr)
	return root
}

// helpTemplate trims Cobra's default to remove the noisy trailing "Use ..."
// suggestion line.
const helpTemplate = `{{with (or .Long .Short)}}{{. | trimTrailingWhitespaces}}

{{end}}{{if or .Runnable .HasSubCommands}}{{.UsageString}}{{end}}`
