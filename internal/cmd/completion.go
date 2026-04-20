package cmd

import (
	"github.com/spf13/cobra"
)

func newCompletionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                   "completion [bash|zsh|fish|powershell]",
		Short:                 "Generate shell completion script",
		Long:                  completionLong,
		DisableFlagsInUseLine: true,
		ValidArgs:             []string{"bash", "zsh", "fish", "powershell"},
		Args:                  cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			switch args[0] {
			case "bash":
				return cmd.Root().GenBashCompletionV2(cmd.OutOrStdout(), true)
			case "zsh":
				return cmd.Root().GenZshCompletion(cmd.OutOrStdout())
			case "fish":
				return cmd.Root().GenFishCompletion(cmd.OutOrStdout(), true)
			case "powershell":
				return cmd.Root().GenPowerShellCompletionWithDesc(cmd.OutOrStdout())
			}
			return nil
		},
	}
	return cmd
}

const completionLong = `Generate a shell completion script and write it to stdout.

Examples:
  # Bash
  kupe completion bash > /etc/bash_completion.d/kupe

  # Zsh (write into a directory on $fpath)
  kupe completion zsh > "${fpath[1]}/_kupe"

  # Fish
  kupe completion fish > ~/.config/fish/completions/kupe.fish

  # PowerShell
  kupe completion powershell > $PROFILE.d/kupe.ps1`
