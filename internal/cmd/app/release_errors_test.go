package app

import (
	"errors"
	"testing"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
)

// The release commands show kupe-api's own refusal once (Errors rule 19) and
// fall back to the status text only when the API sent no message.
func TestReleaseCommandsShowAPIMessageOnce(t *testing.T) {
	fake := releaseFake()
	fake.ListAppEnvironmentsErr = &client.APIError{StatusCode: 404, Message: `app "shopp" not found`, RequestID: "req-404"}
	_, _, err := run(factoryWith(fake), "releases", "shopp")
	if cli.ExitCode(err) != cli.ExitNotFound || err.Error() != `app "shopp" not found (request-id: req-404)` {
		t.Fatalf("exit=%d err=%v", cli.ExitCode(err), err)
	}

	for _, tc := range []struct {
		name  string
		err   *client.APIError
		write bool
		code  int
		want  string
	}{
		{
			name:  "field refusal on a write",
			err:   &client.APIError{StatusCode: 400, Code: "validation_failed", Field: "web.port", Message: "web.port must be between 1 and 65535", RequestID: "req-400"},
			write: true, code: cli.ExitMisuse,
			want: "web.port must be between 1 and 65535 (request-id: req-400)",
		},
		{
			name: "forbidden",
			err:  &client.APIError{StatusCode: 403, Message: "only tenant admins can deploy apps", RequestID: "req-403"},
			code: cli.ExitAuth, want: "only tenant admins can deploy apps (request-id: req-403)",
		},
		{
			name: "no message keeps the status text",
			err:  &client.APIError{StatusCode: 404, RequestID: "req-empty"},
			code: cli.ExitNotFound, want: "Not Found: request failed (request-id: req-empty)",
		},
		{
			name:  "retry revision keeps its own sentence",
			err:   &client.APIError{StatusCode: 400, Field: "deploymentRevision", Message: "deploymentRevision must increase by one", RequestID: "req-rev"},
			write: true, code: cli.ExitMisuse,
			want: "the environment cannot be retried again; change its configuration to deploy: request failed (request-id: req-rev)",
		},
		{
			name:  "server error on a write keeps the unknown outcome",
			err:   &client.APIError{StatusCode: 500, Message: "internal detail", RequestID: "req-500"},
			write: true, code: cli.ExitUnavailable,
			want: "request outcome is unknown; inspect the environment and releases before retrying: request failed (request-id: req-500)",
		},
		{
			name: "server error on a read",
			err:  &client.APIError{StatusCode: 503, Message: "internal detail", RequestID: "req-503"},
			code: cli.ExitUnavailable,
			want: "kupe-api is temporarily unavailable: request failed (request-id: req-503)",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := releaseRequestError(tc.err, "shop", "production", tc.write)
			var api *client.APIError
			if cli.ExitCode(got) != tc.code || got.Error() != tc.want || !errors.As(got, &api) {
				t.Fatalf("exit=%d err=%q", cli.ExitCode(got), got)
			}
		})
	}
}
