package database

import (
	"strings"
	"testing"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
)

// TestTranslators_ShowTheAPIMessageOnce pins that every database translator
// renders kupe-api's message once, as the whole error text, adding only its
// exit code and hint; and that the CLI's own sentence appears only when the
// API sent no message.
func TestTranslators_ShowTheAPIMessageOnce(t *testing.T) {
	const name = "orders-db"
	const (
		unavailableHint = "this is a platform setting, not an outage, so retrying will not help; " +
			"existing databases can still be listed, unprotected (--deletion-protection=false) and deleted"
		pausedHint      = "retry later; status updates are posted at https://status.kupe.cloud"
		shrinkHint      = "pass a --storage at least the current size, shown by:  kupe db get orders-db"
		protectedHint   = "turn it off first:  kupe db update orders-db --deletion-protection=false"
		retryHint       = "retry:  kupe database delete orders-db"
		pausedMessage   = "Database creation is temporarily paused while we expand platform capacity. Please try again later."
		protectMessage  = `database "orders-db" has deletionProtection enabled: set it to false before deleting`
		terminatingHint = "it can no longer be changed; to wait until it is gone:  kupe database wait orders-db --for deleted"
		retryLaterHint  = "retry in a few seconds; status updates are posted at https://status.kupe.cloud"
		nameHint        = "choose a shorter name: database names may be at most 44 characters, and fewer when the tenant name is over 12"
		nameMessage     = `database name "orders-db" too long: at most 44 characters for this tenant`
	)
	translate := func(err error) error { return translateErr(err, name) }
	create := func(err error) error { return translateCreateErr(err, name) }
	update := func(err error) error { return translateUpdateErr(err, name) }
	del := func(err error) error { return translateDeleteErr(err, name) }
	upgrade := func(err error) error { return translateUpgradeErr(err, name) }
	rotate := func(err error) error { return translateRotateErr(err, name) }

	tests := []struct {
		name      string
		translate func(error) error
		status    int
		code      string
		message   string // the API's message; "" exercises the fallback
		wantText  string
		wantHint  string
		wantExit  int
	}{
		{"databases unavailable", translate, 404, client.DatabaseCodeUnavailable,
			"databases are not available on this platform",
			"databases are not available on this platform (request-id: req-1)", unavailableHint, cli.ExitNotFound},
		{"databases unavailable without a message", translate, 404, client.DatabaseCodeUnavailable, "",
			"databases are not available on this platform: Not Found (request-id: req-1)", unavailableHint, cli.ExitNotFound},
		{"creation paused", translate, 400, client.CodeCreationPaused, pausedMessage,
			pausedMessage + " (request-id: req-1)", pausedHint, cli.ExitUnavailable},
		{"creation paused without a message", translate, 400, client.CodeCreationPaused, "",
			"database creation is paused: Bad Request (request-id: req-1)", pausedHint, cli.ExitUnavailable},
		{"name too long for the tenant", create, 400, "DATABASE_NAME_TOO_LONG", nameMessage,
			nameMessage + " (request-id: req-1)", nameHint, cli.ExitMisuse},
		{"name too long for the tenant without a message", create, 400, "DATABASE_NAME_TOO_LONG", "",
			`database name "orders-db" is too long for this tenant: Bad Request (request-id: req-1)`, nameHint, cli.ExitMisuse},
		{"create keeps the shared refusals", create, 400, client.CodeCreationPaused, pausedMessage,
			pausedMessage + " (request-id: req-1)", pausedHint, cli.ExitUnavailable},
		{"storage shrink", update, 400, client.DatabaseCodeStorageShrinkUnsupported,
			"storage can only grow: 5Gi is smaller than the current 10Gi",
			"storage can only grow: 5Gi is smaller than the current 10Gi (request-id: req-1)", shrinkHint, cli.ExitMisuse},
		{"storage shrink without a message", update, 400, client.DatabaseCodeStorageShrinkUnsupported, "",
			"database storage can grow but never shrink: Bad Request (request-id: req-1)", shrinkHint, cli.ExitMisuse},
		{"deletion protection", del, 409, client.DatabaseDeleteCodeProtected, protectMessage,
			protectMessage + " (request-id: req-1)", protectedHint, cli.ExitConflict},
		{"deletion protection without a message", del, 409, client.DatabaseDeleteCodeProtected, "",
			`database "orders-db" has deletion protection on: Conflict (request-id: req-1)`, protectedHint, cli.ExitConflict},
		{"changed while deleting", del, 409, "", `database "orders-db" has been modified, please retry`,
			`database "orders-db" has been modified, please retry (request-id: req-1)`, retryHint, cli.ExitConflict},
		{"changed while deleting without a message", del, 409, "", "",
			`database "orders-db" changed while it was being deleted: Conflict (request-id: req-1)`, retryHint, cli.ExitConflict},
		{"major upgrade unavailable", upgrade, 400, client.DatabaseCodeMajorUpgradeUnavailable,
			"major version upgrades are not available yet",
			"major version upgrades are not available yet (request-id: req-1)", "", cli.ExitUnavailable},
		{"major upgrade unavailable without a message", upgrade, 400, client.DatabaseCodeMajorUpgradeUnavailable, "",
			"the platform refused the major version upgrade: Bad Request (request-id: req-1)", "", cli.ExitUnavailable},
		{"rotate while deleting", rotate, 409, "", `database "orders-db" is being deleted`,
			`database "orders-db" is being deleted (request-id: req-1)`, "", cli.ExitConflict},
		{"rotate while deleting without a message", rotate, 409, "", "",
			`cannot rotate credentials of database "orders-db": Conflict (request-id: req-1)`, "", cli.ExitConflict},
		{"rotate of a terminating database", rotate, 409, client.CodeTerminating, `database "orders-db" is being deleted`,
			`database "orders-db" is being deleted (request-id: req-1)`, terminatingHint, cli.ExitConflict},
		{"update of a terminating database", update, 409, client.CodeTerminating, `database "orders-db" is being deleted`,
			`database "orders-db" is being deleted (request-id: req-1)`, terminatingHint, cli.ExitConflict},
		{"plan limit on create", translate, 400, client.CodePlanLimitReached,
			"maximum database limit reached: your plan allows 1 database(s)",
			"maximum database limit reached: your plan allows 1 database(s) (request-id: req-1)",
			`delete a database ("kupe database delete <name>") or upgrade your plan`, cli.ExitConflict},
		{"service quota still being set up", translate, 503, "",
			"the tenant's database limit is still being set up; retry in a few seconds",
			"service unavailable: the tenant's database limit is still being set up; retry in a few seconds (request-id: req-1)",
			retryLaterHint, cli.ExitUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.translate(&client.APIError{StatusCode: tt.status, Code: tt.code, Message: tt.message, RequestID: "req-1"})
			if got := err.Error(); got != tt.wantText {
				t.Fatalf("text = %q; want %q", got, tt.wantText)
			}
			if tt.message != "" && strings.Count(err.Error(), tt.message) != 1 {
				t.Fatalf("the API message should appear exactly once: %q", err.Error())
			}
			if got := hint(err); got != tt.wantHint {
				t.Fatalf("hint = %q; want %q", got, tt.wantHint)
			}
			assertExit(t, err, tt.wantExit)
		})
	}
}
