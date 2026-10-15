package printer

import (
	"bytes"
	"strings"
	"testing"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
)

func TestVersionDisplay(t *testing.T) {
	tests := []struct {
		name                         string
		requested, running, previous string
		want                         string
	}{
		{"nothing known", "", "", "", ""},
		{"only requested known", "1.32", "", "", "1.32"},
		{"only running known", "", "1.32", "", "1.32"},
		{"equal", "1.32", "1.32", "", "1.32"},
		{"cluster mid-upgrade", "1.33", "1.32", "", "1.33 (running 1.32)"},
		{"database major and full version", "18", "18.0", "", "18 (running 18.0)"},
		{"database mid-upgrade", "18", "17.6", "17.6", "18 (running 17.6, upgrading from 17.6)"},
		{"database with only the running version known", "", "18.0", "", "18.0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := versionDisplay(tt.requested, tt.running, tt.previous); got != tt.want {
				t.Fatalf("versionDisplay(%q, %q, %q) = %q; want %q", tt.requested, tt.running, tt.previous, got, tt.want)
			}
		})
	}
}

func TestRowOf_NeverNil(t *testing.T) {
	var nilCluster *client.Cluster
	for _, v := range []any{nilCluster, "not a cluster", nil} {
		if got := rowOf[client.Cluster](v); got == nil {
			t.Fatalf("rowOf(%#v) = nil; want the zero value", v)
		}
	}
	if got := rowOf[client.Cluster](client.Cluster{Name: "prod"}).Name; got != "prod" {
		t.Fatalf("value row name = %q; want prod", got)
	}
	c := &client.Cluster{Name: "prod"}
	if got := rowOf[client.Cluster](c); got != c {
		t.Fatal("a pointer row should be returned as is")
	}
}

func TestResource_RendersOneAndList(t *testing.T) {
	io, _, _ := cli.Test()
	out := io.Out.(*bytes.Buffer)
	db := client.Database{Name: "orders-db", Engine: "postgres", Version: "18",
		Status: &client.DatabaseStatus{Phase: client.PhaseRunning, CurrentVersion: "18.0"}}

	name, _ := Parse("name")
	if err := Database.List(io, name, []client.Database{db}); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != "orders-db\n" {
		t.Fatalf("-o name list = %q; want orders-db", got)
	}

	out.Reset()
	table, _ := Parse("")
	if err := Database.One(io, table, &db); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); !strings.Contains(got, "18 (running 18.0)") {
		t.Fatalf("detail view missing the version display:\n%s", got)
	}
}
