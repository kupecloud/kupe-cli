package database

import (
	"errors"
	"fmt"
	"slices"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
)

func postgresFromNames(roles, extensions []string, setRoles, setExtensions bool) *client.DatabasePostgres {
	if !setRoles && !setExtensions {
		return nil
	}
	out := &client.DatabasePostgres{}
	if setRoles {
		list := make([]client.DeclaredRole, 0, len(roles))
		for _, name := range roles {
			list = append(list, client.DeclaredRole{Name: name})
		}
		out.Roles = &list
	}
	if setExtensions {
		list := make([]client.Extension, 0, len(extensions))
		for _, name := range extensions {
			list = append(list, client.Extension{Name: name})
		}
		out.Extensions = &list
	}
	return out
}

func validateListEdits(opts *updateOpts) error {
	for _, edit := range []struct {
		noun        string
		add, remove []string
	}{{"role", opts.addRoles, opts.removeRoles}, {"extension", opts.addExtensions, opts.removeExtensions}} {
		for _, name := range edit.add {
			if slices.Contains(edit.remove, name) {
				return cli.MisuseError(fmt.Sprintf("%s %s is both added and removed", edit.noun, name))
			}
		}
	}
	return nil
}

// editedPostgres runs after each read, including the retry after a concurrent update.
// Only the list being edited is sent; an empty list remains an explicit clear.
func editedPostgres(current *client.DatabasePostgres, opts *updateOpts) *client.DatabasePostgres {
	var roles, extensions []string
	if current != nil {
		if current.Roles != nil {
			for _, r := range *current.Roles {
				roles = append(roles, r.Name)
			}
		}
		if current.Extensions != nil {
			for _, e := range *current.Extensions {
				extensions = append(extensions, e.Name)
			}
		}
	}
	return postgresFromNames(editNames(roles, opts.addRoles, opts.removeRoles), editNames(extensions, opts.addExtensions, opts.removeExtensions), len(opts.addRoles)+len(opts.removeRoles) > 0, len(opts.addExtensions)+len(opts.removeExtensions) > 0)
}

func editNames(current, add, remove []string) []string {
	out := make([]string, 0, len(current)+len(add))
	for _, name := range current {
		if !slices.Contains(remove, name) {
			out = append(out, name)
		}
	}
	for _, name := range add {
		if !slices.Contains(out, name) {
			out = append(out, name)
		}
	}
	return out
}

// postgresWaitErr includes the platform's tenant-facing explanation for an
// unapplied role or extension change, while preserving the wait's exit code.
func postgresWaitErr(err error, db *client.Database) error {
	var out *cli.Error
	if db == nil || db.Status == nil || !errors.As(err, &out) {
		return err
	}
	for _, condition := range db.Status.Conditions {
		if (condition.Type == "RolesReady" || condition.Type == "ExtensionsReady") && condition.Status == "False" && condition.Message != "" {
			return out.WithHint(condition.Message + "\n" + out.Hint)
		}
	}
	return err
}
