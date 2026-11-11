package cmd

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/config"
)

// printErrorHint writes the hint block for err to stderr, each line indented
// under the error Cobra has already printed. Nothing is written when there
// is no hint.
func printErrorHint(io *cli.IOStreams, flags *cli.GlobalFlags, err error) {
	hint := errorHint(err, cli.NewFactory(io, flags), flags, config.LoadEnv())
	if hint == "" {
		return
	}
	for _, line := range strings.Split(hint, "\n") {
		fmt.Fprintf(io.ErrOut, "  %s\n", line)
	}
}

// errorHint returns the hint block Execute prints under a failed command:
// the command's own *cli.Error hint when it set one, otherwise a
// tenant-access hint when kupe-api refused the tenant, otherwise "".
func errorHint(err error, f *cli.Factory, flags *cli.GlobalFlags, env config.Env) string {
	var e *cli.Error
	if errors.As(err, &e) && e.Hint != "" {
		return e.Hint
	}
	if client.ErrorCode(err) != client.CodeTenantAccessDenied {
		return ""
	}
	// An unreadable config still gets the generic advice.
	cfg, _ := f.Config()
	return tenantAccessHint(flags, env, cfg)
}

// tenantAccessHint explains a tenant_access_denied refusal. The tenant the
// CLI sent comes from --tenant / KUPE_TENANT or the current context, so the
// fix is to change whichever one picked it: name it, and list the other
// contexts the user could switch to.
func tenantAccessHint(flags *cli.GlobalFlags, env config.Env, cfg *config.Config) string {
	const login = "or log in to the tenant you want: kupe auth login --tenant NAME"
	switch {
	case flags.Tenant != "":
		return "the tenant comes from --tenant; check the name\n" + login
	case env.Tenant != "":
		return "the tenant comes from KUPE_TENANT; check or unset it\n" + login
	}

	r := config.Resolve(config.Flags{Context: flags.Context}, config.Env{Context: env.Context}, cfg)
	var lines []string
	if r.ContextName != "" {
		lines = append(lines, "current context: "+r.ContextName)
	}
	var others []string
	if cfg != nil {
		for _, c := range cfg.Contexts {
			if c.Name != r.ContextName {
				others = append(others, c.Name)
			}
		}
	}
	if len(others) > 0 {
		slices.Sort(others)
		lines = append(lines, fmt.Sprintf("switch with: kupe config use-context NAME (your contexts: %s)", strings.Join(others, ", ")))
	}
	lines = append(lines, login)
	return strings.Join(lines, "\n")
}
