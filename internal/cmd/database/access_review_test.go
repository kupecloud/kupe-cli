package database

import (
	"errors"
	"strings"
	"testing"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/client/clienttest"
)

// F-3C-01: only the known pre-access unknown-field 400s get platform guidance.
func TestAccessReviewUnsupportedFields(t *testing.T) {
	for _, field := range []string{"postgres", "roleName", "credentials"} {
		t.Run(field, func(t *testing.T) {
			remote := &client.APIError{StatusCode: 400, Message: `invalid request: json: unknown field "` + field + `"`, RequestID: "review-request"}
			got := translateErr(remote, "orders-db")
			var ce *cli.Error
			var original *client.APIError
			if cli.ExitCode(got) != cli.ExitUnavailable || !errors.As(got, &ce) || !strings.Contains(ce.Hint, "platform") || !errors.As(got, &original) || original != remote || !strings.Contains(got.Error(), "review-request") {
				t.Fatalf("refusal not preserved with guidance: %#v", got)
			}
		})
	}
	for _, remote := range []*client.APIError{
		{StatusCode: 400, Message: `invalid request: json: unknown field "typo"`},
		{StatusCode: 400, Message: `invalid request: json: unknown field "postgresFuture"`},
		{StatusCode: 400, Message: "invalid postgres setting"},
		{StatusCode: 422, Message: `json: unknown field "postgres"`},
		{StatusCode: 409, Message: `json: unknown field "credentials"`},
		{StatusCode: 400, Code: "other_validation", Message: `json: unknown field "postgres"`},
	} {
		var classified *cli.Error
		if got := translateErr(remote, "orders-db"); !errors.Is(got, remote) || errors.As(got, &classified) {
			t.Fatalf("unrelated refusal changed: remote=%#v got=%v", remote, got)
		}
	}
}

func TestAccessReviewUnsupportedCommandPaths(t *testing.T) {
	for _, tc := range []struct{ name, field string }{{"create", "postgres"}, {"update", "postgres"}, {"binding-add-role", "roleName"}, {"binding-add-dedicated", "credentials"}, {"binding-update", "credentials"}} {
		t.Run(tc.name, func(t *testing.T) {
			fake := clienttest.New()
			fake.DatabasesByName["orders-db"] = running("orders-db")
			fake.DatabaseBindings["b"] = &client.DatabaseBinding{Name: "b", Database: "orders-db", Role: "readwrite", Namespaces: []string{"api"}}
			remote := &client.APIError{StatusCode: 400, Message: `invalid request: json: unknown field "` + tc.field + `"`, RequestID: "original-request"}
			f := factoryWith(t, fake)
			cmd := newUpdateCmd(f)
			args := []string{"orders-db", "--add-role", "reporting", "--wait=false"}
			write := "UpdateDatabase:"
			switch tc.name {
			case "create":
				delete(fake.DatabasesByName, "orders-db")
				fake.CreateDatabaseErr = remote
				cmd = newCreateCmd(f)
				args = []string{"orders-db", "--cpu", "500m", "--memory", "1Gi", "--storage", "10Gi", "--roles", "reporting", "--wait=false"}
				write = "CreateDatabase:"
			case "update":
				fake.UpdateDatabaseErr = remote
			case "binding-add-role":
				fake.CreateDatabaseBindingErr = remote
				cmd = newBindingAddCmd(f)
				args = []string{"orders-db", "--cluster", "prod", "--namespace", "api", "--role", "reporting", "--wait=false"}
				write = "CreateDatabaseBinding:"
			case "binding-add-dedicated":
				fake.CreateDatabaseBindingErr = remote
				cmd = newBindingAddCmd(f)
				args = []string{"orders-db", "--cluster", "prod", "--namespace", "api", "--role", "readwrite", "--dedicated", "--wait=false"}
				write = "CreateDatabaseBinding:"
			case "binding-update":
				fake.UpdateDatabaseBindingErr = remote
				cmd = newBindingUpdateCmd(f)
				args = []string{"orders-db", "b", "--dedicated", "--wait=false"}
				write = "UpdateDatabaseBinding:"
			}
			_, _, err := run(f, cmd, args...)
			var ce *cli.Error
			if !errors.As(err, &ce) || !strings.Contains(ce.Hint, "platform") || !strings.Contains(err.Error(), "original-request") {
				t.Fatalf("missing platform guidance/request ID: %v", err)
			}
			count := 0
			for _, call := range fake.Calls {
				if strings.HasPrefix(call, write) {
					count++
				}
			}
			if count != 1 {
				t.Fatalf("wanted one write without fallback, got %v", fake.Calls)
			}
		})
	}
}

// F-3C-03: local owner guards must use the immutable API's exact sentence.
func TestAccessReviewOwnerRefusalParity(t *testing.T) {
	const canonical = "owner bindings use shared credentials"
	remote := translateErr(&client.APIError{StatusCode: 400, Code: client.DatabaseCodeInvalidBindingCredentials, Message: canonical}, "orders-db")
	for _, mode := range []string{"add", "update"} {
		t.Run(mode, func(t *testing.T) {
			fake := clienttest.New()
			fake.DatabasesByName["orders-db"] = running("orders-db")
			fake.DatabaseBindings["b"] = &client.DatabaseBinding{Name: "b", Database: "orders-db", Role: "owner", Namespaces: []string{"api"}}
			f := factoryWith(t, fake)
			cmd := newBindingAddCmd(f)
			args := []string{"orders-db", "--cluster", "prod", "--namespace", "api", "--role", "owner", "--dedicated", "--wait=false"}
			if mode == "update" {
				cmd = newBindingUpdateCmd(f)
				args = []string{"orders-db", "b", "--dedicated", "--wait=false"}
			}
			_, _, err := run(f, cmd, args...)
			if cli.ExitCode(err) != cli.ExitMisuse || err.Error() != canonical || err.Error() != remote.Error() {
				t.Fatalf("local=%v remote=%v", err, remote)
			}
			for _, call := range fake.Calls {
				if strings.HasPrefix(call, "CreateDatabaseBinding:") || strings.HasPrefix(call, "UpdateDatabaseBinding:") {
					t.Fatalf("owner guard issued write: %v", fake.Calls)
				}
			}
			if len(fake.Calls) != 0 {
				t.Fatalf("owner guard added requests: %v", fake.Calls)
			}
		})
	}
}
