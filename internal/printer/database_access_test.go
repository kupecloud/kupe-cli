package printer

import (
	"strings"
	"testing"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
)

func TestAccessPrinterCredentialsAndNamespaceRotation(t *testing.T) {
	b := &client.DatabaseBinding{Name: "b", Role: "custom", RoleName: "report_ro", Credentials: &client.BindingCredentials{Dedicated: true}, Namespaces: []string{"api", "worker"}, Status: &client.DatabaseBindingStatus{Rotation: &client.BindingRotation{RequestedAt: "opaque", AppliedAt: "previous", Pending: true}, Namespaces: []client.DatabaseBindingNamespaceStatus{{Name: "api", DeliveredRotation: "opaque"}, {Name: "worker", DeliveredRotation: "previous"}}}}
	rows := bindingRows(b)
	if bindingRole(b) != "report_ro" || bindingCredentialsLabel(b) != "dedicated" || bindingNamespaceRotation(rows[0]) != "delivered" || bindingNamespaceRotation(rows[1]) != "pending" {
		t.Fatal("dedicated role/namespace progress not rendered")
	}
	io, out, _ := cli.Test()
	format, err := Parse("table")
	if err != nil {
		t.Fatal(err)
	}
	if err := DatabaseBinding.One(io, format, b); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"Credentials:", "dedicated", "Rotation requested:", "opaque", "ROTATION", "pending"} {
		if !strings.Contains(out.String(), text) {
			t.Errorf("missing %q: %s", text, out.String())
		}
	}
	b.Credentials = nil
	if bindingNamespaceRotation(rows[0]) != "" {
		t.Fatal("shared binding reported dedicated rotation")
	}
}

func TestAccessPrinterPostgresLists(t *testing.T) {
	roles := []client.DeclaredRole{{Name: "reporting"}}
	ext := []client.Extension{{Name: "vector"}}
	db := &client.Database{Postgres: &client.DatabasePostgres{Roles: &roles, Extensions: &ext}}
	if postgresRoleNames(db) != "reporting" || postgresExtensionNames(db) != "vector" {
		t.Fatal("postgres lists missing")
	}
	if postgresRoleNames(&client.Database{}) != "" || postgresExtensionNames(&client.Database{}) != "" {
		t.Fatal("omitted postgres lists invented")
	}
}
