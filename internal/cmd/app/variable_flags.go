package app

import (
	"fmt"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/spf13/cobra"
)

const setEnvHelp = "Set a literal environment variable: NAME=VALUE"
const envReferenceHelp = "Set a secret reference: VAR=SECRET:KEY"
const unsetEnvHelp = "Remove an environment variable by name"

func registerLiteralVariables(cmd *cobra.Command, o *envOptions, legacy bool) {
	cmd.Flags().StringArrayVar(&o.env, "set-env", nil, setEnvHelp)
	if legacy {
		cmd.Flags().StringArrayVar(&o.env, "env", nil, setEnvHelp)
		_ = cmd.Flags().MarkHidden("env")
	}
}

func normalizeVariableFlags(cmd *cobra.Command, o *envOptions, streams *cli.IOStreams) error {
	if cmd.Flags().Changed("env") && cmd.Flags().Changed("set-env") {
		return cli.MisuseError("pass --set-env or the deprecated --env, not both")
	}
	o.literalFlag = "--set-env"
	if cmd.Flags().Changed("env") {
		o.literalFlag = "--env"
		fmt.Fprintln(streams.ErrOut, "Flag --env has been deprecated, use --set-env")
	}
	return nil
}

func literalVariableFlag(o *envOptions) string {
	if o.literalFlag != "" {
		return o.literalFlag
	}
	return "--set-env"
}
