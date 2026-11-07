package app

import "github.com/spf13/cobra"

const setEnvHelp = "Set a literal environment variable: NAME=VALUE"
const envReferenceHelp = "Set a secret reference: VAR=SECRET:KEY"
const unsetEnvHelp = "Remove an environment variable by name"

func registerLiteralVariables(cmd *cobra.Command, o *envOptions) {
	cmd.Flags().StringArrayVar(&o.env, "set-env", nil, setEnvHelp)
}
