package cmdutil

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
)

// apiRefusal answers every request with status and kupe-api's error body
// (internal/server/response.go writeError): a coded refusal carries code,
// severity and message beside error; an uncoded one only error.
func apiRefusal(t *testing.T, status int, code, message string) error {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		body := map[string]string{"error": message}
		if code != "" {
			body["code"], body["severity"], body["message"] = code, "error", message
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(srv.Close)
	// A POST: the client never retries one, so a 503 reaches the caller as is.
	_, _, err := client.New(srv.URL, "acme", "token", "test").Databases().
		Create(context.Background(), client.CreateDatabaseRequest{Name: "orders-db"})
	if err == nil {
		t.Fatal("the fake API refused nothing")
	}
	return err
}

func TestTranslateCommon(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		code     string
		message  string
		wantExit int
		wantText string // "" = the API message
		wantHint string // substring; "" = no hint
	}{
		{"creation paused", 400, client.CodeCreationPaused,
			"Database creation is temporarily paused while we expand platform capacity. Please try again later.",
			cli.ExitUnavailable, "", "status.kupe.cloud"},
		{"databases switched off", 404, client.DatabaseCodeUnavailable, "databases are not available on this platform",
			cli.ExitNotFound, "", "a platform setting, not an outage"},
		{"registries switched off", 404, client.RegistryCodeUnavailable, "registries are not available on this platform",
			cli.ExitNotFound, "", "an existing registry can still be listed, unprotected (--deletion-protection=false) and deleted"},
		{"older server: registries switched off without a code", 404, "", "registries are not enabled on this platform",
			cli.ExitNotFound, "", "a platform setting, not an outage"},
		{"terminating", 409, client.CodeTerminating, `database "orders-db" is being deleted`,
			cli.ExitConflict, "", "kupe database wait orders-db --for deleted"},
		{"tenant being deleted", 409, client.CodeTenantTerminating, "tenant acme is being deleted; cannot create new databases",
			cli.ExitConflict, "", ""},
		{"tenant suspended", 409, client.CodeTenantSuspended, "tenant acme is suspended; contact support before creating new databases",
			cli.ExitConflict, "", "contact support"},
		{"payment setup", 409, client.CodePaymentSetupRequired, "tenant acme is awaiting payment setup; add a payment method before creating databases",
			cli.ExitConflict, "", "Billing page of the Kupe console"},
		{"plan limit", 400, client.CodePlanLimitReached, "maximum database limit reached: your plan allows 1 database(s)",
			cli.ExitConflict, "", `kupe database delete <name>`},
		{"a coded refusal without a message gets the CLI sentence", 409, client.CodePaymentSetupRequired, "",
			cli.ExitConflict, "add a payment method before creating or growing databases: Conflict", "Billing page"},
		{"quota still being set up (503, no code)", 503, "", "the tenant's database limit is still being set up; retry in a few seconds",
			cli.ExitUnavailable, "service unavailable: the tenant's database limit is still being set up; retry in a few seconds",
			"retry in a few seconds"},
		{"older server: tenant gate sentence without a code", 400, "",
			"tenant tenant-acme/acme is suspended; contact support before creating new databases",
			cli.ExitConflict, "", "contact support"},
		{"older server: binding of a database being deleted", 400, "", `database "orders-db" is being deleted; it cannot be bound`,
			cli.ExitConflict, "", "--for deleted"},
		{"a code outside the table is not matched by its sentence", 409, client.DatabaseDeleteCodeProtected,
			"database has deletionProtection enabled; tenant acme is suspended; contact support",
			cli.ExitConflict, "", ""},
		{"anything else passes through", 400, "", "spec.resources.cpu: must be at least 250m",
			cli.ExitMisuse, "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			apiErr := apiRefusal(t, tt.status, tt.code, tt.message)
			err := TranslateCommon("database", "orders-db", apiErr)
			if got := cli.ExitCode(err); got != tt.wantExit {
				t.Fatalf("exit = %d; want %d (%v)", got, tt.wantExit, err)
			}
			want := tt.wantText
			if want == "" {
				want = tt.message
			}
			if got := err.Error(); got != want {
				t.Fatalf("text = %q; want %q", got, want)
			}
			var ce *cli.Error
			hint := ""
			if errors.As(err, &ce) {
				hint = ce.Hint
			}
			if (tt.wantHint == "") != (hint == "") || !strings.Contains(hint, tt.wantHint) {
				t.Fatalf("hint = %q; want %q", hint, tt.wantHint)
			}
			if !client.IsAPIError(err) {
				t.Fatal("the API error must stay in the chain")
			}
		})
	}
}

func TestTranslateCommon_PassesThroughNonAPIErrors(t *testing.T) {
	transport := errors.New("dial tcp: connection refused")
	if got := TranslateCommon("cluster", "prod", transport); !errors.Is(got, transport) || client.IsAPIError(got) {
		t.Fatalf("a transport error should pass through unchanged, got %v", got)
	}
	if TranslateCommon("cluster", "prod", nil) != nil {
		t.Fatal("nil should stay nil")
	}
}

func TestTranslateCommon_TerminatingWithoutANameHasNoHint(t *testing.T) {
	err := TranslateCommon("database", "", &client.APIError{StatusCode: 409, Code: client.CodeTerminating, Message: "x is being deleted"})
	var ce *cli.Error
	if !errors.As(err, &ce) || ce.Hint != "" {
		t.Fatalf("err = %#v; want no hint without a name", err)
	}
}

func TestTranslateCommon_FallbackPluralOfRegistry(t *testing.T) {
	err := TranslateCommon("registry", "", &client.APIError{StatusCode: 409, Code: client.CodeTenantSuspended})
	if got := err.Error(); !strings.HasPrefix(got, "your tenant is suspended; it cannot take new or larger registries") {
		t.Fatalf("text = %q", got)
	}
	if got := plural("database"); got != "databases" {
		t.Fatalf("plural(database) = %q", got)
	}
}

func TestAppRefusalRowDoesNotCaptureUnrelatedLegacyErrors(t *testing.T) {
	for _, tc := range []struct {
		code, message string
		status, want  int
	}{
		{"apps_unavailable", "apps are not available on this platform", 404, 7},
		{"", "apps are not available on this platform", 404, 7},
		{"", "cluster not found", 404, 4},
		{"", "cluster already exists", 409, 5},
		{"", "invalid cluster name", 400, 2},
	} {
		err := TranslateCommon("app", "shop", &client.APIError{StatusCode: tc.status, Code: tc.code, Message: tc.message})
		if cli.ExitCode(err) != tc.want {
			t.Errorf("%s: exit=%d want=%d err=%v", tc.message, cli.ExitCode(err), tc.want, err)
		}
	}
}

func TestAppsUnavailableFallbackPreservesAPIError(t *testing.T) {
	for _, message := range []string{"apps are temporarily unavailable for this tenant", "", " \t "} {
		err := apiRefusal(t, 404, "apps_unavailable", message)
		var original *client.APIError
		if !errors.As(err, &original) {
			t.Fatal("missing API error")
		}
		original.RequestID = "app-request-123"
		original.Field = "app"
		beforeBody := string(original.Body())
		beforeMessage := original.Message
		got := TranslateCommon("app", "shop", err)
		want := message
		if strings.TrimSpace(message) == "" {
			want = "apps are not available on this platform"
		}
		if cli.ExitCode(got) != 7 || got.Error() != want+" (request-id: app-request-123)" {
			t.Fatalf("message=%q got=%v", message, got)
		}
		var api *client.APIError
		if !errors.As(got, &api) || api.StatusCode != 404 || api.Code != "apps_unavailable" || api.RequestID != original.RequestID || api.Field != original.Field || api.Method != original.Method || api.Path != original.Path || string(api.Body()) != beforeBody {
			t.Fatalf("API fields lost: %+v", api)
		}
		if original.Message != beforeMessage || string(original.Body()) != beforeBody {
			t.Fatal("original API error mutated")
		}
	}
}
