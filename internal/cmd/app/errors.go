package app

import (
	"fmt"
	"strings"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/cmd/cmdutil"
)

func stringsContainDelimiter(name string) bool { return strings.Contains(name, "--") }
func translateErr(err error, parent, name string) error {
	if err == nil {
		return nil
	}
	command, noun := "kupe app update "+name, "app"
	if parent != "" {
		command = "kupe app env update " + parent + " " + name
		noun = "environment"
	}
	switch client.ErrorCode(err) {
	case "deletion_protected":
		return cli.WrapAPI(cli.ExitConflict, "deletion protection is on", err).WithHint(command + " --deletion-protection=false; turn off protection on every environment before deleting an app")
	case "namespace_in_use":
		return cli.WrapAPI(cli.ExitConflict, "the environment namespace is in use", err).WithHint("remove independent secret deliveries and service bindings from the namespace, then delete again")
	case "precondition_failed":
		return cli.WrapAPI(cli.ExitConflict, fmt.Sprintf("%s changed; review it before retrying", noun), err)
	}
	return cmdutil.TranslateCommon(noun, name, err)
}
