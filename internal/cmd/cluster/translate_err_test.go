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
		{"spec rejection keeps the API text and names every resource flag", &client.APIError{StatusCode: 400,
			Message: `invalid cluster "prod": check field values and constraints`},
			cli.ExitMisuse, "cluster spec rejected: invalid cluster \"prod\": check field values and constraints",
			"check --cpu-limit (e.g. 2), --memory-limit (e.g. 8Gi), --storage-limit (e.g. 50Gi), and --version values", ""},
		{"an invalid name passes through without the spec hint", &client.APIError{StatusCode: 400,
			Message: `invalid cluster name "a--b": must not contain "--"`},
			cli.ExitMisuse, `invalid cluster name "a--b": must not contain "--"`, "", "--cpu-limit"},
		{"a too-long name passes through without the spec hint", &client.APIError{StatusCode: 400,
			Message: `cluster name "x" too long: at most 48 characters for this tenant`},
			cli.ExitMisuse, `cluster name "x" too long: at most 48 characters for this tenant`, "", "--cpu-limit"},
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
