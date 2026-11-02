package database

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/client/clienttest"
	"github.com/kupecloud/kupe-cli/internal/ux"
)

func TestAccessBindingAddRoles(t *testing.T) {
	for _, tt := range []struct {
		role               string
		dedicated          bool
		wantRole, wantName string
		wantErr            bool
	}{
		{"readwrite", false, "readwrite", "", false}, {"report_ro", false, "custom", "report_ro", false},
		{"owner", true, "", "", true}, {"custom", false, "", "", true}, {"Report", false, "", "", true}, {"report-ro", false, "", "", true},
	} {
		t.Run(tt.role, func(t *testing.T) {
			fake := clienttest.New()
			fake.DatabasesByName["orders-db"] = running("orders-db")
			f := factoryWith(t, fake)
			args := []string{"orders-db", "--cluster", "prod", "--namespace", "orders-api", "--role", tt.role, "--wait=false", "-o", "json"}
			if tt.dedicated {
				args = append(args, "--dedicated")
			}
			_, _, err := run(f, newBindingAddCmd(f), args...)
			if tt.wantErr {
				if cli.ExitCode(err) != cli.ExitMisuse {
					t.Fatalf("err=%v", err)
				}
				if len(fake.DatabaseBindings) != 0 {
					t.Fatal("invalid role created binding")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			name := "orders-db-prod-" + strings.ReplaceAll(tt.role, "_", "-")
			b := fake.DatabaseBindings[name]
			if b == nil || b.Role != tt.wantRole || b.RoleName != tt.wantName || b.Dedicated() != (tt.wantName != "" || tt.dedicated) {
				t.Fatalf("binding=%+v", b)
			}
		})
	}
}

func TestAccessBindingUpdatePresentFields(t *testing.T) {
	for _, tt := range []struct {
		name           string
		args           []string
		already        bool
		wantNamespaces bool
		wantPatch      bool
	}{
		{"credentials only", []string{"--dedicated"}, false, false, true},
		{"both", []string{"--dedicated", "--add-namespace", "orders-worker"}, false, true, true},
		{"already dedicated", []string{"--dedicated"}, true, false, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fake := clienttest.New()
			fake.DatabaseBindings["b"] = &client.DatabaseBinding{Name: "b", Database: "orders-db", Role: "readwrite", Namespaces: []string{"orders-api"}, Credentials: bindingCredentials(tt.already)}
			f := factoryWith(t, fake)
			args := append([]string{"orders-db", "b", "--wait=false", "-o", "json"}, tt.args...)
			_, _, err := run(f, newBindingUpdateCmd(f), args...)
			if err != nil {
				t.Fatal(err)
			}
			if (len(fake.BindingPatches) > 0) != tt.wantPatch {
				t.Fatalf("patches=%v", fake.BindingPatches)
			}
			if tt.wantPatch {
				p := fake.BindingPatches[0]
				if (p.Namespaces != nil) != tt.wantNamespaces || p.Credentials == nil || !p.Credentials.Dedicated {
					t.Fatalf("patch=%+v", p)
				}
			}
			if len(fake.DatabaseBindings["b"].Namespaces) == 0 {
				t.Fatal("credentials patch erased namespaces")
			}
		})
	}
	fake := clienttest.New()
	f := factoryWith(t, fake)
	_, _, err := run(f, newBindingUpdateCmd(f), "orders-db", "b", "--dedicated=false")
	if cli.ExitCode(err) != cli.ExitMisuse {
		t.Fatalf("err=%v", err)
	}
}

func TestAccessPostgresEdits(t *testing.T) {
	roles := []client.DeclaredRole{{Name: "reporting"}}
	ext := []client.Extension{{Name: "vector"}}
	current := &client.DatabasePostgres{Roles: &roles, Extensions: &ext}
	out := editedPostgres(current, &updateOpts{removeExtensions: []string{"vector"}})
	if out.Roles != nil || out.Extensions == nil || len(*out.Extensions) != 0 {
		t.Fatalf("clear=%+v", out)
	}
	out = editedPostgres(current, &updateOpts{addRoles: []string{"reporting", "auditor"}, removeRoles: []string{"missing"}})
	if out.Extensions != nil || len(*out.Roles) != 2 || len(roles) != 1 {
		t.Fatalf("roles=%+v", out)
	}
	if validateListEdits(&updateOpts{addRoles: []string{"x"}, removeRoles: []string{"x"}}) == nil {
		t.Fatal("same add/remove accepted")
	}
}

func TestAccessBindingGenerationWait(t *testing.T) {
	b := &client.DatabaseBinding{Generation: 2, Namespaces: []string{"orders-api"}, Status: &client.DatabaseBindingStatus{ObservedGeneration: 1, Phase: client.PhaseReady, Namespaces: []client.DatabaseBindingNamespaceStatus{{Name: "orders-api", Phase: client.PhaseReady}}}}
	if bindingPhase(b) == client.PhaseReady {
		t.Fatal("stale generation ready")
	}
	b.Status.ObservedGeneration = 2
	if bindingPhase(b) != client.PhaseReady {
		t.Fatal("current generation not ready")
	}
}

func TestAccessBindingRotationWait(t *testing.T) {
	fake := clienttest.New()
	b := &client.DatabaseBinding{Name: "b", Database: "orders-db", Namespaces: []string{"orders-api", "orders-worker"}, Credentials: bindingCredentials(true), Status: &client.DatabaseBindingStatus{Phase: client.PhaseReady, ObservedRotation: "opaque", Rotation: &client.BindingRotation{RequestedAt: "opaque", AppliedAt: "opaque"}, Namespaces: []client.DatabaseBindingNamespaceStatus{{Name: "orders-api", Phase: client.PhaseReady, DeliveredRotation: "opaque"}, {Name: "orders-worker", Phase: client.PhaseReady, DeliveredRotation: "old"}}}}
	fake.DatabaseBindings["b"] = b
	io, _, stderr := cli.Test()
	result := &bindingRotation{BindingCredentialRotation: client.BindingCredentialRotation{RequestedAt: "opaque"}, Pending: true}
	err := waitForBindingRotation(context.Background(), io, fake, "orders-db", "b", result, time.Millisecond)
	if !errors.Is(err, ux.ErrWaitTimeout) || !strings.Contains(stderr.String(), "orders-worker") {
		t.Fatalf("err=%v progress=%s", err, stderr.String())
	}
	b.Status.Namespaces[1].DeliveredRotation = "opaque"
	if err := waitForBindingRotation(context.Background(), io, fake, "orders-db", "b", result, time.Second); err != nil {
		t.Fatal(err)
	}
	if result.Pending || result.AppliedAt != "opaque" {
		t.Fatalf("result=%+v", result)
	}
	b.Status.Namespaces[1].DeliveredRotation = "old"
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = waitForBindingRotation(ctx, io, fake, "orders-db", "b", result, time.Second)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel err=%v", err)
	}
}

func TestAccessBindingRotateSharedRefusal(t *testing.T) {
	fake := clienttest.New()
	fake.DatabaseBindings["b"] = &client.DatabaseBinding{Name: "b", Database: "orders-db", Role: "readwrite"}
	f := factoryWith(t, fake)
	_, _, err := run(f, newBindingRotateCredentialsCmd(f), "orders-db", "b", "--yes", "--wait=false", "-o", "json")
	if cli.ExitCode(err) != cli.ExitConflict {
		t.Fatalf("err=%v", err)
	}
}

func TestAccessCreateAndUpdatePostgres(t *testing.T) {
	fake := clienttest.New()
	f := factoryWith(t, fake)
	out, _, err := run(f, newCreateCmd(f), "orders-db", "--cpu", "500m", "--memory", "1Gi", "--storage", "10Gi", "--roles", "reporting,auditor", "--extensions", "vector", "--wait=false", "-o", "json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"postgres"`) || fake.DatabasesByName["orders-db"].Postgres == nil {
		t.Fatalf("out=%s", out)
	}
	db := fake.DatabasesByName["orders-db"]
	db.Status = &client.DatabaseStatus{Phase: client.PhaseRunning, Conditions: []client.Condition{{Type: "ExtensionsReady", Status: "False", Message: "extensions are still applying"}}}
	// An extension-only change must enter the convergence wait, not return the PATCH.
	_, _, err = run(f, newUpdateCmd(f), "orders-db", "--remove-extension", "vector", "--wait-timeout", "1ms", "-o", "json")
	if cli.ExitCode(err) != cli.ExitTimeout || !strings.Contains(err.Error()+func() string {
		var ce *cli.Error
		if errors.As(err, &ce) {
			return ce.Hint
		}
		return ""
	}(), "extensions are still applying") {
		t.Fatalf("extension-only update returned early: %v", err)
	}
	if db.Postgres.Extensions == nil || len(*db.Postgres.Extensions) != 0 || len(*db.Postgres.Roles) != 2 {
		t.Fatalf("postgres=%+v", db.Postgres)
	}
}

func TestAccessRotationFollowsNewerAndTerminates(t *testing.T) {
	fake := clienttest.New()
	ready := &client.DatabaseBinding{Name: "b", Database: "orders-db", Namespaces: []string{"orders-api"}, Status: &client.DatabaseBindingStatus{Phase: client.PhaseReady, ObservedRotation: "new-opaque", Rotation: &client.BindingRotation{RequestedAt: "new-opaque", AppliedAt: "new-opaque"}, Namespaces: []client.DatabaseBindingNamespaceStatus{{Name: "orders-api", Phase: client.PhaseReady, DeliveredRotation: "new-opaque"}}}}
	fake.DatabaseBindings["b"] = ready
	io, _, stderr := cli.Test()
	result := &bindingRotation{BindingCredentialRotation: client.BindingCredentialRotation{RequestedAt: "old-opaque"}, Pending: true}
	if err := waitForBindingRotation(context.Background(), io, fake, "orders-db", "b", result, time.Second); err != nil {
		t.Fatal(err)
	}
	if result.RequestedAt != "new-opaque" || result.Pending || !strings.Contains(stderr.String(), "newer binding rotation") {
		t.Fatalf("result=%+v stderr=%s", result, stderr.String())
	}
	ready.Status.Phase = client.PhaseTerminating
	ready.Generation = 3
	err := waitForBindingRotation(context.Background(), io, fake, "orders-db", "b", result, time.Second)
	if err == nil {
		t.Fatal("terminating binding reported success")
	}
}

func TestAccessErrors(t *testing.T) {
	for _, tt := range []struct {
		code string
		exit int
	}{
		{client.DatabaseCodeRoleReserved, cli.ExitMisuse}, {client.DatabaseCodeExtensionNotAllowed, cli.ExitMisuse}, {client.DatabaseCodeRoleInUse, cli.ExitConflict}, {client.DatabaseCodeRoleNotDeclared, cli.ExitMisuse}, {client.DatabaseCodeCredentialsNotDedicated, cli.ExitConflict}, {client.DatabaseCodeDedicatedCredentialsIrreversible, cli.ExitMisuse}, {client.DatabaseCodeInvalidBindingCredentials, cli.ExitMisuse},
	} {
		t.Run(tt.code, func(t *testing.T) {
			err := translateErr(&client.APIError{StatusCode: 400, Code: tt.code, Message: "exact platform message"}, "orders-db")
			var ce *cli.Error
			if cli.ExitCode(err) != tt.exit || !errors.As(err, &ce) || ce.Error() != "exact platform message" || ce.Hint == "" {
				t.Fatalf("err=%+v", err)
			}
		})
	}
}

func TestAccessDedicatedUpdateWaitsForObservedGeneration(t *testing.T) {
	fake := clienttest.New()
	fake.DatabaseBindings["b"] = &client.DatabaseBinding{Name: "b", Database: "orders-db", Role: "readwrite", Generation: 2, Namespaces: []string{"api"}, Status: &client.DatabaseBindingStatus{ObservedGeneration: 2, Phase: client.PhaseReady, Namespaces: []client.DatabaseBindingNamespaceStatus{{Name: "api", Phase: client.PhaseReady}}}}
	f := factoryWith(t, fake)
	_, _, err := run(f, newBindingUpdateCmd(f), "orders-db", "b", "--dedicated", "--wait-timeout", "1ms")
	if cli.ExitCode(err) != cli.ExitTimeout {
		t.Fatalf("dedicated update accepted stale generation: %v", err)
	}
	if !fake.DatabaseBindings["b"].Dedicated() || fake.DatabaseBindings["b"].Generation != 3 {
		t.Fatal("dedicated update was not sent")
	}
}

func TestAccessBindingRotateAcknowledgementAndPrompt(t *testing.T) {
	fake := clienttest.New()
	fake.DatabaseBindings["b"] = &client.DatabaseBinding{Name: "b", Database: "orders-db", Credentials: bindingCredentials(true)}
	f := factoryWith(t, fake)
	out, _, err := run(f, newBindingRotateCredentialsCmd(f), "orders-db", "b", "--yes", "--wait=false", "-o", "json")
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{`"database": "orders-db"`, `"binding": "b"`, `"requestedAt": "rotation-token"`, `"pending": true`} {
		if !strings.Contains(out, text) {
			t.Fatalf("acknowledgement missing %s: %s", text, out)
		}
	}
	if strings.Contains(out, "appliedAt") || strings.Contains(out, "password") {
		t.Fatalf("unexpected fields: %s", out)
	}
	before := len(fake.Calls)
	f = factoryWith(t, fake)
	_, _, err = run(f, newBindingRotateCredentialsCmd(f), "orders-db", "b", "--wait=false")
	if cli.ExitCode(err) != cli.ExitMisuse || len(fake.Calls) != before {
		t.Fatalf("noninteractive prompt issued rotation: err=%v calls=%v", err, fake.Calls)
	}
}

func TestAccessDedicatedRefusalParity(t *testing.T) {
	const message = "dedicated credentials cannot be turned off once enabled"
	local := dedicatedIrreversible(nil)
	remote := translateErr(&client.APIError{StatusCode: 400, Code: client.DatabaseCodeDedicatedCredentialsIrreversible, Message: message}, "orders-db")
	if local.Error() != message || remote.Error() != message || cli.ExitCode(local) != cli.ExitMisuse || cli.ExitCode(remote) != cli.ExitMisuse {
		t.Fatalf("local=%v remote=%v", local, remote)
	}
}

func TestAccessStaleGenerationDoesNotHideBindingFailure(t *testing.T) {
	b := &client.DatabaseBinding{Name: "b", Database: "orders-db", Generation: 3, Namespaces: []string{"api"}, Status: &client.DatabaseBindingStatus{ObservedGeneration: 2, Phase: client.PhaseFailed, Message: "namespace has a conflicting Secret", Conditions: []client.Condition{{Type: "Conflict", Status: "True"}}, Namespaces: []client.DatabaseBindingNamespaceStatus{{Name: "api", Phase: client.PhaseFailed}}}}
	if bindingPhase(b) != client.PhaseFailed {
		t.Fatal("failed delivery hidden by generation wait")
	}
	fake := clienttest.New()
	fake.DatabaseBindings["b"] = b
	io, _, _ := cli.Test()
	err := waitForBindingRotation(context.Background(), io, fake, "orders-db", "b", &bindingRotation{BindingCredentialRotation: client.BindingCredentialRotation{RequestedAt: "opaque"}}, time.Second)
	if cli.ExitCode(err) != cli.ExitConflict || !strings.Contains(err.Error(), "conflicting Secret") {
		t.Fatalf("err=%v", err)
	}
}

func TestAccessDeleteHintSeparatesCredentials(t *testing.T) {
	fake := clienttest.New()
	fake.DatabaseBindings["b"] = &client.DatabaseBinding{Name: "b", Database: "orders-db", Credentials: bindingCredentials(true)}
	f := factoryWith(t, fake)
	_, stderr, err := run(f, newBindingDeleteCmd(f), "orders-db", "b", "--yes")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stderr, "With shared credentials") || !strings.Contains(stderr, "Dedicated logins are revoked") {
		t.Fatalf("hint=%s", stderr)
	}
}
