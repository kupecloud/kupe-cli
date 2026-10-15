package printer

import (
	"testing"

	"github.com/kupecloud/kupe-cli/internal/client"
)

// TestPlanColumns_DatabaseLimit pins how the database limit reads in both
// plan views: an explicit 0 (unlimited, as for clusters) prints as 0, and
// a limit the server did not report prints as "-", never as a 0 that would
// claim unlimited.
func TestPlanColumns_DatabaseLimit(t *testing.T) {
	tests := []struct {
		name         string
		maxDatabases *int64
		want         string
	}{
		{name: "limited plan", maxDatabases: new(int64(5)), want: "5"},
		{name: "unlimited plan", maxDatabases: new(int64(0)), want: "0"},
		{name: "not reported by the server", maxDatabases: nil, want: "-"},
	}
	views := []struct {
		name    string
		columns Columns
		header  string
	}{
		{name: "list", columns: PlanColumns(), header: "MAX-DATABASES"},
		{name: "get", columns: PlanDetailColumns(), header: "Max Databases"},
	}
	for _, view := range views {
		for _, tt := range tests {
			t.Run(view.name+"/"+tt.name, func(t *testing.T) {
				col := columnNamed(t, view.columns, view.header)
				p := client.Plan{Name: "pro", MaxClusters: 5, MaxDatabases: tt.maxDatabases}
				if got := col.Get(p); got != tt.want {
					t.Errorf("%s = %q, want %q", view.header, got, tt.want)
				}
			})
		}
	}
}

func columnNamed(t *testing.T, cols Columns, name string) Column {
	t.Helper()
	for _, c := range cols {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("no %q column", name)
	return Column{}
}
