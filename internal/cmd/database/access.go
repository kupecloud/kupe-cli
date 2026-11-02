package database

import (
	"regexp"
	"slices"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
)

var declaredRolePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

func validateBindingRole(role string) error {
	if slices.Contains(client.DatabaseRoles, role) {
		return nil
	}
	if role != client.DatabaseRoleCustom && len(role) <= 63 && declaredRolePattern.MatchString(role) {
		return nil
	}
	return cli.MisuseError("invalid --role " + role).WithHint("use --role owner, readwrite, readonly or a role declared on the database; name the declared role instead of custom")
}
func bindingCredentials(dedicated bool) *client.BindingCredentials {
	if !dedicated {
		return nil
	}
	return &client.BindingCredentials{Dedicated: true}
}
func dedicatedIrreversible(err error) error {
	return cli.WrapAPI(cli.ExitMisuse, "dedicated credentials cannot be turned off once enabled", err).WithHint("keep dedicated credentials, or delete the binding and add another")
}
