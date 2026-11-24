package printer

import (
	"strings"
	"testing"

	"github.com/kupecloud/kupe-cli/internal/client"
)

// The operator's VersionEndOfSupport messages (kupe-control-operator
// internal/controller/manageddatabase/endofsupport.go): with major upgrades
// off they say to move the data, with them on to upgrade.
const (
	msgApproachingMove    = "PostgreSQL 17 reaches end of support on 8 November 2029 and gets no fixes from then, security fixes included. Move your data to a database on a newer version before then; this one keeps running either way"
	msgReachedMove        = "PostgreSQL 17 reached end of support on 8 November 2029 and gets no more fixes, security fixes included. Move your data to a database on a newer version; this one keeps running as it is"
	msgApproachingUpgrade = "PostgreSQL 17 reaches end of support on 8 November 2029 and gets no fixes from then, security fixes included. Upgrade the database to a newer version before then; it keeps running either way"
)

func TestDatabaseVersionSupport(t *testing.T) {
	eos := func(status, reason, message string) *client.Condition {
		return &client.Condition{Type: client.ConditionVersionEndOfSupport, Status: status, Reason: reason, Message: message}
	}
	tests := []struct {
		name string
		date string
		cond *client.Condition
		want string
	}{
		{"no date and no condition has no row", "", nil, ""},
		{"unknown with no date has no row", "", eos("Unknown", "EndOfSupportUnknown", "no date for PostgreSQL 16"), ""},
		{"supported keeps the fixed text, not the message", "2030-11-14", eos("False", "Supported", "PostgreSQL 18 is supported until 14 November 2030"), "supported until 2030-11-14"},
		{"approaching shows the operator's message", "2029-11-08", eos("True", "EndOfSupportApproaching", msgApproachingMove), msgApproachingMove},
		{"approaching with upgrades on shows that message", "2029-11-08", eos("True", "EndOfSupportApproaching", msgApproachingUpgrade), msgApproachingUpgrade},
		{"reached shows the operator's message", "2029-11-08", eos("True", "EndOfSupportReached", msgReachedMove), msgReachedMove},
		{"approaching with no message is the date, no advice", "2029-11-08", eos("True", "EndOfSupportApproaching", ""), "ends 2029-11-08"},
		{"reached with no message is the date, no advice", "2029-11-08", eos("True", "EndOfSupportReached", ""), "ended 2029-11-08"},
		{"approaching with neither", "", eos("True", "EndOfSupportApproaching", ""), "end of support approaching"},
		{"reached with neither", "", eos("True", "EndOfSupportReached", ""), "ended"},
		{"date without condition", "2030-11-14", nil, "ends 2030-11-14"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := &client.DatabaseStatus{VersionEndOfSupport: tt.date}
			if tt.cond != nil {
				st.Conditions = []client.Condition{{Type: "Ready", Status: "True"}, *tt.cond}
			}
			if got := databaseVersionSupport(&client.Database{Status: st}); got != tt.want {
				t.Fatalf("databaseVersionSupport = %q; want %q", got, tt.want)
			}
		})
	}
}

// TestVersionSupport_DatabaseReadsLikeCluster: where the database line does
// not carry the operator's message (Supported, a date with no condition, and
// a notice or ended support with no message) it reads as the cluster's, less
// the cluster's upgrade advice.
func TestVersionSupport_DatabaseReadsLikeCluster(t *testing.T) {
	db := func(reason string) string {
		st := &client.DatabaseStatus{VersionEndOfSupport: "2029-11-08"}
		if reason != "" {
			st.Conditions = []client.Condition{{Type: client.ConditionVersionEndOfSupport, Status: "True", Reason: reason}}
		}
		return databaseVersionSupport(&client.Database{Status: st})
	}
	cluster := func(reason string) string {
		return versionSupportDisplay(supportCluster("2029-11-08", reason, "m"))
	}
	for _, reason := range []string{"Supported", ""} {
		if d, c := db(reason), cluster(reason); d != c {
			t.Errorf("reason %q: database %q, cluster %q", reason, d, c)
		}
	}
	for _, reason := range []string{"EndOfSupportApproaching", "EndOfSupportReached"} {
		d, c := db(reason), cluster(reason)
		if want, _, _ := strings.Cut(c, ":"); d != want {
			t.Errorf("reason %q with no message: database %q; want the cluster's %q less its advice", reason, d, c)
		}
	}
}
