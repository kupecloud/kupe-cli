package printer

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/kupecloud/kupe-cli/internal/client"
)

func supportCluster(date, reason, msg string) *client.Cluster {
	st := &client.ClusterStatus{Phase: client.PhaseRunning, VersionEndOfSupport: date}
	if reason != "" {
		st.Conditions = []client.ClusterCondition{
			{Type: "Ready", Status: "True", Reason: "Ok"},
			{Type: client.ConditionVersionEndOfSupport, Reason: reason, Message: msg},
		}
	}
	return &client.Cluster{Name: "prod", Type: "shared", Version: "1.34", Status: st}
}

func TestVersionSupportDisplay(t *testing.T) {
	tests := []struct {
		name string
		c    *client.Cluster
		want string
	}{
		{"unset", supportCluster("", "", ""), ""},
		{"nil status", &client.Cluster{Name: "x"}, ""},
		{"supported", supportCluster("2027-02-28", "Supported", "m"), "supported until 2027-02-28"},
		{"supported no date", supportCluster("", "Supported", "m"), "supported"},
		{"approaching", supportCluster("2026-12-01", "EndOfSupportApproaching", "m"), "ends 2026-12-01: upgrade the cluster before then"},
		{"reached", supportCluster("2026-11-01", "EndOfSupportReached", "m"), "ended 2026-11-01: upgrade the cluster to a newer version"},
		{"unsupported uses message", supportCluster("", "Unsupported", "Kubernetes 1.30 is no longer supported."), "Kubernetes 1.30 is no longer supported."},
		{"date without condition", supportCluster("2027-01-01", "", ""), "ends 2027-01-01"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := versionSupportDisplay(tt.c); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestClusterGetSupportOutputs(t *testing.T) {
	set := supportCluster("2027-02-28", "Supported", "Kubernetes 1.34 is supported until 2027-02-28.")
	unset := supportCluster("", "", "")

	var tbl bytes.Buffer
	if err := PrintDetails(&tbl, set, clusterDetailColumns(false)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(tbl.String(), "Version Support:") || !strings.Contains(tbl.String(), "supported until 2027-02-28") {
		t.Errorf("table missing support line:\n%s", tbl.String())
	}
	tbl.Reset()
	if err := PrintDetails(&tbl, unset, clusterDetailColumns(false)); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(tbl.String(), "Version Support") {
		t.Errorf("unset must omit the row:\n%s", tbl.String())
	}

	b, err := json.Marshal(set)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"versionEndOfSupport":"2027-02-28"`, `"type":"VersionEndOfSupport"`, `"reason":"Supported"`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("json missing %s: %s", want, b)
		}
	}
	b, _ = json.Marshal(unset)
	if strings.Contains(string(b), "versionEndOfSupport") {
		t.Errorf("unset json must omit the date: %s", b)
	}
}
