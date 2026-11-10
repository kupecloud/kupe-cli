package printer

import (
	"strings"
	"testing"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
)

// A binding whose namespaces were delivered differently (one through the
// pooler, one direct) prints each namespace's own host.
func TestDatabaseBinding_PrintsEachNamespacesHost(t *testing.T) {
	b := &client.DatabaseBinding{
		Name: "orders-db-prod-readwrite", Role: "readwrite", Namespaces: []string{"api", "jobs"},
		Status: &client.DatabaseBindingStatus{Namespaces: []client.DatabaseBindingNamespaceStatus{
			{Name: "api", Phase: "Ready", Host: "orders-db--pooler.api.svc.cluster.local"},
			{Name: "jobs", Phase: "Ready", Host: "orders-db.jobs.svc.cluster.local"},
		}},
	}
	for _, kind := range []string{"table", "wide"} {
		io, out, _ := cli.Test()
		format, _ := Parse(kind)
		if err := DatabaseBinding.One(io, format, b); err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"orders-db--pooler.api.svc.cluster.local", "orders-db.jobs.svc.cluster.local"} {
			if !strings.Contains(out.String(), want) {
				t.Errorf("%s: missing host %q in:\n%s", kind, want, out)
			}
		}
	}
	io, out, _ := cli.Test()
	wide, _ := Parse("wide")
	if err := DatabaseBinding.List(io, wide, []client.DatabaseBinding{*b}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"orders-db--pooler.api.svc.cluster.local", "orders-db.jobs.svc.cluster.local"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("list: missing host %q in:\n%s", want, out)
		}
	}
}
