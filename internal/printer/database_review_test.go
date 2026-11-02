package printer

import (
	"strings"
	"testing"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
)

// F-3C-02: report the API's aggregate bool, without inferring it from tokens.
func TestAccessReviewRotationPendingSummary(t *testing.T) {
	for _, tc := range []struct {
		name     string
		rotation *client.BindingRotation
		want     string
	}{{"pending", &client.BindingRotation{RequestedAt: "same", AppliedAt: "same", Pending: true}, "true"}, {"completed", &client.BindingRotation{RequestedAt: "different", AppliedAt: "previous", Pending: false}, "false"}, {"shared absent", nil, ""}} {
		t.Run(tc.name, func(t *testing.T) {
			b := &client.DatabaseBinding{Name: "b", Status: &client.DatabaseBindingStatus{Rotation: tc.rotation}}
			if tc.rotation != nil {
				b.Credentials = &client.BindingCredentials{Dedicated: true}
			}
			io, out, _ := cli.Test()
			format, err := Parse("table")
			if err != nil {
				t.Fatal(err)
			}
			if err := DatabaseBinding.One(io, format, b); err != nil {
				t.Fatal(err)
			}
			found := false
			for _, line := range strings.Split(out.String(), "\n") {
				if strings.HasPrefix(line, "Rotation pending:") {
					found = true
					if strings.TrimSpace(strings.TrimPrefix(line, "Rotation pending:")) != tc.want {
						t.Fatalf("row=%q", line)
					}
				}
			}
			if !found {
				t.Fatalf("pending row missing: %s", out.String())
			}
		})
	}
}
