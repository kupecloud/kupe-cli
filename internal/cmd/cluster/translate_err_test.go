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
		{"creation_paused code", &client.APIError{StatusCode: 400, Code: client.CodeCreationPaused, Message: "try again later"},
			cli.ExitUnavailable, "cluster creation is paused: try again later", "status.kupe.cloud", ""},
		{"older server: webhook wording without a code", &client.APIError{StatusCode: 400,
			Message: "Cluster creation is temporarily paused while we expand platform capacity. Please try again later."},
			cli.ExitUnavailable, "cluster creation is paused", "status.kupe.cloud", ""},
		{"field reason keeps the API text and names the flag", &client.APIError{StatusCode: 400,
			Message: `invalid cluster "prod": resources.cpu: must be at most 8 cores`},
			cli.ExitMisuse, "resources.cpu: must be at most 8 cores", "check --cpu-limit (e.g. 2 or 500m)", "--memory-limit"},
		{"several field reasons name each flag", &client.APIError{StatusCode: 400,
			Message: `invalid cluster "prod": resources.memory: must be at least 1Gi; version: is not supported`},
			cli.ExitMisuse, "cluster spec rejected", "check --memory-limit (e.g. 8Gi), --version", "--cpu-limit"},
		{"older server: spec-prefixed field", &client.APIError{StatusCode: 400,
			Message: `invalid cluster "prod": spec.resources.storage: Invalid value: "50": bad`},
			cli.ExitMisuse, "cluster spec rejected", "check --storage-limit (e.g. 50Gi)", "--cpu-limit"},
		{"no field named falls back to every resource flag", &client.APIError{StatusCode: 400,
			Message: `invalid cluster "prod": check field values and constraints`},
			cli.ExitMisuse, "check field values and constraints", "--cpu-limit (e.g. 2), --memory-limit (e.g. 8Gi), --storage-limit (e.g. 50Gi), and --version", ""},
		{"an unrelated validation error passes through", &client.APIError{StatusCode: 400, Message: "plan does not allow HA"},
			cli.ExitMisuse, "plan does not allow HA", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := translateClusterErr(tt.err)
			if code := cli.ExitCode(err); code != tt.wantExit {
				t.Fatalf("exit = %d; want %d (%v)", code, tt.wantExit, err)
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Fatalf("message %q missing %q", err.Error(), tt.wantMsg)
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
