package cluster

import (
	"errors"
	"strings"
	"testing"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
)

func TestTranslateClusterErr(t *testing.T) {
	tests := []struct {
		name     string
		err      *client.APIError
		wantExit int
		wantMsg  string
		wantHint string
		notHint  string
	}{
		{"creation_paused code shows the API message alone", &client.APIError{StatusCode: 400, Code: client.CodeCreationPaused,
			Message: "Cluster creation is temporarily paused while we expand platform capacity. Please try again later.", RequestID: "req-1"},
			cli.ExitUnavailable, "Cluster creation is temporarily paused while we expand platform capacity. Please try again later. (request-id: req-1)",
			"retry later; status updates are posted at https://status.kupe.cloud", ""},
		{"creation_paused without a message falls back to the CLI sentence", &client.APIError{StatusCode: 400, Code: client.CodeCreationPaused, RequestID: "req-1"},
			cli.ExitUnavailable, "cluster creation is paused: Bad Request (request-id: req-1)",
			"retry later; status updates are posted at https://status.kupe.cloud", ""},
		{"older server: webhook wording without a code", &client.APIError{StatusCode: 400,
			Message: "Cluster creation is temporarily paused while we expand platform capacity. Please try again later."},
			cli.ExitUnavailable, "Cluster creation is temporarily paused while we expand platform capacity. Please try again later.",
			"retry later; status updates are posted at https://status.kupe.cloud", ""},
		{"spec rejection shows the API text alone and names every resource flag", &client.APIError{StatusCode: 400,
			Message: `invalid cluster "prod": check field values and constraints`},
			cli.ExitMisuse, `invalid cluster "prod": check field values and constraints`,
			"check --cpu-limit (e.g. 2), --memory-limit (e.g. 8Gi), --storage-limit (e.g. 50Gi), and --version values", ""},
		{"an invalid name passes through without the spec hint", &client.APIError{StatusCode: 400,
			Message: `invalid cluster name "a--b": must not contain "--"`},
			cli.ExitMisuse, `invalid cluster name "a--b": must not contain "--"`, "", "--cpu-limit"},
		{"a too-long name passes through without the spec hint", &client.APIError{StatusCode: 400,
			Message: `cluster name "x" too long: at most 48 characters for this tenant`},
			cli.ExitMisuse, `cluster name "x" too long: at most 48 characters for this tenant`, "", "--cpu-limit"},
		{"a coded refusal keeps the API text even where a sentence rule would match", &client.APIError{StatusCode: 400, Code: "CLUSTER_DEDICATED_UNSUPPORTED",
			Message: `invalid cluster "prod": dedicated clusters are not available yet`},
			cli.ExitMisuse, `invalid cluster "prod": dedicated clusters are not available yet`, "", "--cpu-limit"},
		{"an unrelated validation error passes through", &client.APIError{StatusCode: 400, Message: "plan does not allow HA"},
			cli.ExitMisuse, "plan does not allow HA", "", ""},
		{"plan limit by code", &client.APIError{StatusCode: 400, Code: client.CodePlanLimitReached,
			Message: "maximum cluster limit reached: starter plan allows 3 cluster(s), currently have 3"},
			cli.ExitConflict, "maximum cluster limit reached: starter plan allows 3 cluster(s), currently have 3",
			`delete a cluster ("kupe cluster delete <name>") or upgrade your plan`, ""},
		{"older server: plan limit sentence without a code", &client.APIError{StatusCode: 400,
			Message: "maximum cluster limit reached: starter plan allows 3 cluster(s), currently have 3"},
			cli.ExitConflict, "maximum cluster limit reached: starter plan allows 3 cluster(s), currently have 3",
			`delete a cluster ("kupe cluster delete <name>") or upgrade your plan`, "--cpu-limit"},
		{"tenant being deleted by code", &client.APIError{StatusCode: 409, Code: client.CodeTenantTerminating,
			Message: "tenant acme is being deleted; cannot grow clusters"},
			cli.ExitConflict, "tenant acme is being deleted; cannot grow clusters", "", ""},
		{"older server: tenant being deleted without a code", &client.APIError{StatusCode: 400,
			Message: "tenant tenant-acme/acme is being deleted; cannot create new ManagedClusters"},
			cli.ExitConflict, "tenant tenant-acme/acme is being deleted; cannot create new ManagedClusters", "", ""},
		{"update of a terminating cluster", &client.APIError{StatusCode: 409, Code: client.CodeTerminating,
			Message: `cluster "prod" is being deleted`},
			cli.ExitConflict, `cluster "prod" is being deleted`,
			"it can no longer be changed; to wait until it is gone:  kupe cluster wait prod --for deleted", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := translateClusterErr(tt.err, "prod")
			if code := cli.ExitCode(err); code != tt.wantExit {
				t.Fatalf("exit = %d; want %d (%v)", code, tt.wantExit, err)
			}
			if err.Error() != tt.wantMsg {
				t.Fatalf("message = %q; want %q", err.Error(), tt.wantMsg)
			}
			if tt.err.Message != "" && strings.Count(err.Error(), tt.err.Message) != 1 {
				t.Fatalf("the API message should appear exactly once: %q", err.Error())
			}
			var ce *cli.Error
			hint := ""
			if errors.As(err, &ce) {
				hint = ce.Hint
			}
			if !strings.Contains(hint, tt.wantHint) {
				t.Fatalf("hint %q missing %q", hint, tt.wantHint)
			}
			if tt.notHint != "" && strings.Contains(hint, tt.notHint) {
				t.Fatalf("hint %q should not mention %q", hint, tt.notHint)
			}
		})
	}
}
