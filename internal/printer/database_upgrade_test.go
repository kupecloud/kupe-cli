package printer

import (
	"strings"
	"testing"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
)

func TestDatabaseDetail_UpgradeRow(t *testing.T) {
	db := func(conds ...client.Condition) *client.Database {
		return &client.Database{Name: "orders-db", Version: "18", Status: &client.DatabaseStatus{Phase: "Running", CurrentVersion: "17.6", Conditions: conds}}
	}
	tests := []struct {
		name string
		db   *client.Database
		want string // "" means no Upgrade row
	}{
		{"waiting shows the check that is not met",
			db(client.Condition{Type: "Ready", Status: "False", Reason: "UpgradeWaiting"},
				client.Condition{Type: "UpgradeInProgress", Status: "True", Reason: "UpgradeWaiting", Message: "waiting for a backup from the last 26 hours"}),
			"waiting for a backup from the last 26 hours"},
		{"a condition with no message shows its reason",
			db(client.Condition{Type: "UpgradeInProgress", Status: "True", Reason: "PreUpgradeBackup"}), "PreUpgradeBackup"},
		{"rolled back shows Ready's message",
			db(client.Condition{Type: "Ready", Status: "False", Reason: "UpgradeRolledBack", Message: "rolled back before any data changed"},
				client.Condition{Type: "UpgradeInProgress", Status: "False", Reason: "UpgradeRolledBack"}),
			"rolled back before any data changed"},
		{"failed shows Ready's message",
			db(client.Condition{Type: "Ready", Status: "False", Reason: "UpgradeFailed", Message: "the database still runs 17"}),
			"the database still runs 17"},
		{"a finished upgrade has no row",
			db(client.Condition{Type: "Ready", Status: "True", Reason: "DatabaseHealthy"},
				client.Condition{Type: "UpgradeInProgress", Status: "False", Reason: "UpgradeSucceeded", Message: "upgraded"}), ""},
		{"an unrelated unhealthy database has no row",
			db(client.Condition{Type: "Ready", Status: "False", Reason: "DatabaseNotHealthy", Message: "instance down"}), ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			io, out, _ := cli.Test()
			format, err := Parse("table")
			if err != nil {
				t.Fatal(err)
			}
			if err := Database.One(io, format, tt.db); err != nil {
				t.Fatal(err)
			}
			var got string
			found := false
			for _, line := range strings.Split(out.String(), "\n") {
				if rest, ok := strings.CutPrefix(line, "Upgrade:"); ok {
					got, found = strings.TrimSpace(rest), true
				}
			}
			if found != (tt.want != "") || got != tt.want {
				t.Fatalf("Upgrade row = %q (shown %v); want %q\n%s", got, found, tt.want, out.String())
			}
		})
	}
}
