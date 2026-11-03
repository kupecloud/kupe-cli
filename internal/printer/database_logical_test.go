package printer

import (
	"strings"
	"testing"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
)

func TestLogicalDatabaseOutput(t *testing.T) {
	entries := []client.LogicalDatabase{{Name: "orders_db", Schemas: []client.DatabaseSchema{{Name: "sales"}}}, {Name: "analytics", Schemas: []client.DatabaseSchema{{Name: "rollup"}}}}
	db := &client.Database{Name: "orders-db", Postgres: &client.DatabasePostgres{Databases: &entries}, Status: &client.DatabaseStatus{Databases: []client.LogicalDatabaseStatus{{Name: "orders_db", Phase: "Ready"}, {Name: "analytics", Phase: "Pending"}}}}
	for _, kind := range []string{"table", "json", "yaml"} {
		io, out, _ := cli.Test()
		format, err := Parse(kind)
		if err != nil {
			t.Fatal(err)
		}
		if err := Database.One(io, format, db); err != nil {
			t.Fatal(err)
		}
		for _, text := range []string{"orders_db", "sales", "analytics", "rollup", "Pending"} {
			if !strings.Contains(out.String(), text) {
				t.Errorf("%s missing %s: %s", kind, text, out)
			}
		}
	}
	one := []client.LogicalDatabase{{Name: "orders_db"}}
	db.Postgres.Databases = &one
	io, out, _ := cli.Test()
	format, _ := Parse("table")
	if err := Database.One(io, format, db); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "Databases:") {
		t.Fatalf("single database changed table: %s", out)
	}
	b := &client.DatabaseBinding{Name: "b", LogicalDatabase: "analytics", Namespaces: []string{"reports"}}
	for _, kind := range []string{"table", "wide", "json", "yaml"} {
		io, out, _ := cli.Test()
		format, _ := Parse(kind)
		if err := DatabaseBinding.One(io, format, b); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), "analytics") {
			t.Errorf("%s lost selector: %s", kind, out)
		}
	}
	io, out, _ = cli.Test()
	wide, _ := Parse("wide")
	if err := DatabaseBinding.List(io, wide, []client.DatabaseBinding{*b}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "LOGICAL DATABASE") || !strings.Contains(out.String(), "analytics") {
		t.Fatalf("wide: %s", out)
	}
}
