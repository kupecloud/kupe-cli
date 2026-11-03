package secret

import (
	"errors"
	"testing"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
)

// Each known Vault refusal is printed once: the CLI's sentence and the
// request id, never followed by kupe-api's text for the same refusal.
func TestVaultRefusalsPrintOnce(t *testing.T) {
	for _, tc := range []struct {
		name  string
		err   *client.APIError
		purge bool
		exit  int
		want  string
	}{
		{"reserved path", &client.APIError{StatusCode: 403, Code: "secret_path_reserved", Message: "this path is reserved by the platform", RequestID: "r1"}, false, cli.ExitAuth,
			"this path is reserved by the platform (request-id: r1)"},
		{"switched off", &client.APIError{StatusCode: 404, Code: "vault_not_configured", Message: "vault is not configured", RequestID: "r2"}, false, cli.ExitUnavailable,
			"Vault is not available on this platform (request-id: r2)"},
		{"unavailable", &client.APIError{StatusCode: 503, Code: "vault_unavailable", Message: "internal server error", RequestID: "r3"}, false, cli.ExitUnavailable,
			"Vault is unavailable (request-id: r3)"},
		{"unavailable purge", &client.APIError{StatusCode: 503, Code: "vault_unavailable", Message: "internal server error"}, true, cli.ExitUnavailable,
			"Vault is unavailable"},
		{"unconfirmed", &client.APIError{StatusCode: 503, Code: "secret_write_unconfirmed", Message: "internal server error", RequestID: "r4"}, false, cli.ExitUnavailable,
			"the write may or may not have been saved (request-id: r4)"},
		{"invalid values", &client.APIError{StatusCode: 400, Code: "invalid_secret_values", Message: "values must be a non-empty map of valid keys and string values", RequestID: "r5"}, false, cli.ExitMisuse,
			"values must be a non-empty JSON object with valid keys and string values (512 KiB limit) (request-id: r5)"},
		{"too large", &client.APIError{StatusCode: 413, Message: "Request Entity Too Large"}, false, cli.ExitMisuse,
			"values input exceeds the 512 KiB limit"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := translateVaultErr(tc.err, "team/db", 3, tc.purge)
			var api *client.APIError
			if cli.ExitCode(got) != tc.exit || got.Error() != tc.want || !errors.As(got, &api) {
				t.Fatalf("exit=%d err=%q", cli.ExitCode(got), got)
			}
		})
	}
}
