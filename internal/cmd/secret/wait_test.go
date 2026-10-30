package secret

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/client/clienttest"
	"github.com/kupecloud/kupe-cli/internal/cmd/cmdutil"
)

func deliveredSecret() *client.Secret {
	return &client.Secret{Name: "db", Generation: 2, Sync: []client.SyncTarget{{Cluster: "c", Namespace: "ns"}}, Status: &client.SecretStatus{Phase: "Active", ObservedGeneration: 2, SyncStatuses: []client.SecretTargetStatus{{Cluster: "c", Namespace: "ns", Phase: "Synced", Reason: "Delivered"}}}}
}

func TestSecretDelivered(t *testing.T) {
	tests := []struct {
		name string
		edit func(*client.Secret)
		want bool
	}{
		{"all Synced", func(*client.Secret) {}, true},
		{"stale observed generation", func(s *client.Secret) { s.Status.ObservedGeneration = 1 }, false},
		{"Pending target", func(s *client.Secret) { s.Status.SyncStatuses[0].Phase = "Pending" }, false},
		{"missing target", func(s *client.Secret) { s.Status.SyncStatuses = nil }, false},
		{"Withdrawing", func(s *client.Secret) {
			s.Status.SyncStatuses = append(s.Status.SyncStatuses, client.SecretTargetStatus{Cluster: "old", Namespace: "ns", Phase: "Withdrawing"})
		}, false},
		{"Degraded", func(s *client.Secret) { s.Status.Phase = "Degraded" }, false},
		{"zero targets", func(s *client.Secret) { s.Sync = nil; s.Status.SyncStatuses = nil }, true},
		{"no status", func(s *client.Secret) { s.Status = nil }, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := deliveredSecret()
			tt.edit(s)
			if got := secretDelivered(s); got != tt.want {
				t.Fatalf("delivered=%v, want%v", got, tt.want)
			}
		})
	}
	if secretDelivered(nil) {
		t.Fatal("nil secret delivered")
	}
}

func TestDeliveryTerminalOnlyCurrentConflictOrDeleting(t *testing.T) {
	for _, tt := range []struct {
		name, phase, reason  string
		stale, removed, want bool
	}{
		{name: "provider can recover", phase: "Failed", reason: "ProviderError"},
		{name: "unknown can recover", phase: "Failed", reason: "FutureReason"},
		{name: "current conflict", phase: "Failed", reason: "TargetConflict", want: true},
		{name: "current conflict with older observed generation", phase: "Failed", reason: "TargetConflict", stale: true, want: true},
		{name: "removed conflict", phase: "Failed", reason: "TargetConflict", removed: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := deliveredSecret()
			s.Status.Phase = "Degraded"
			s.Status.SyncStatuses[0].Phase = tt.phase
			s.Status.SyncStatuses[0].Reason = tt.reason
			if tt.stale {
				s.Status.ObservedGeneration--
			}
			if tt.removed {
				s.Sync = nil
			}
			err := secretTerminal(s)
			var terminal *cmdutil.TerminalPhaseError
			if errors.As(err, &terminal) != tt.want {
				t.Fatalf("terminal=%v", err)
			}
			if tt.want && !strings.Contains(err.Error(), "c/ns") {
				t.Fatal("target dropped")
			}
		})
	}
	s := deliveredSecret()
	s.Status.Phase = "Deleting"
	s.Status.ObservedGeneration = 0
	var terminal *cmdutil.TerminalPhaseError
	if !errors.As(secretTerminal(s), &terminal) {
		t.Fatal("Deleting did not terminate")
	}
}

func TestCreateAndUpdateWaitByDefault(t *testing.T) {
	for _, verb := range []string{"create", "update"} {
		t.Run(verb, func(t *testing.T) {
			fake := clienttest.New()
			fake.Secrets["db"] = &client.Secret{Name: "db"}
			fake.GetSecretSeq["db"] = []*client.Secret{deliveredSecret()}
			f := factoryWith(t, fake)
			args := []string{"db", "--sync", "c:ns"}
			cmd := newUpdateCmd(f)
			if verb == "create" {
				delete(fake.Secrets, "db")
				cmd = newCreateCmd(f)
				args = append(args, "--path", "kv/db")
			}
			if err := exec(cmd, args...); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(f.IOStreams.ErrOut.(*bytes.Buffer).String(), "delivered") {
				t.Fatal("did not wait for delivery")
			}
			if !strings.Contains(f.IOStreams.Out.(*bytes.Buffer).String(), "Active") {
				t.Fatal("did not render final state")
			}
		})
	}
}

func TestDeliveryWaitTimeoutAndProviderRecovery(t *testing.T) {
	pending := deliveredSecret()
	pending.Status.Phase = "Degraded"
	pending.Status.SyncStatuses[0].Phase = "Failed"
	pending.Status.SyncStatuses[0].Reason = "ProviderError"
	fake := clienttest.New()
	fake.GetSecretSeq["db"] = []*client.Secret{pending}
	f := factoryWith(t, fake)
	err := exec(newWaitCmd(f), "db", "--timeout=1ms")
	if cli.ExitCode(err) != cli.ExitTimeout || !strings.Contains(err.Error(), "not delivered") {
		t.Fatalf("err=%v", err)
	}
	var ce *cli.Error
	if !errors.As(err, &ce) || !strings.Contains(ce.Hint, "keeps trying") || !strings.Contains(ce.Hint, "kupe secret get db") {
		t.Fatalf("hint=%+v", err)
	}
	fake.GetSecretSeq["db"] = []*client.Secret{pending, deliveredSecret()}
	f = factoryWith(t, fake)
	if err := exec(newWaitCmd(f), "db", "--timeout=5s"); err != nil {
		t.Fatal(err)
	}
}

func TestSecretDeleteWaitsForPlain404(t *testing.T) {
	for _, tt := range []struct {
		name     string
		nowait   bool
		apiErr   error
		timeout  bool
		wantCode int
		wantText string
	}{
		{name: "default waits", wantText: "secret/db deleted"},
		{name: "no wait", nowait: true, wantText: "secret/db delete requested"},
		{name: "timeout keeps removing", timeout: true, wantCode: cli.ExitTimeout},
		{name: "coded 404 is refusal", apiErr: &client.APIError{StatusCode: 404, Code: "secrets_unavailable"}, wantCode: cli.ExitNotFound},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fake := clienttest.New()
			fake.Secrets["db"] = deliveredSecret()
			fake.GetSecretErr = tt.apiErr
			fake.KeepDeletedSecret = tt.timeout || tt.apiErr != nil
			f := factoryWith(t, fake)
			args := []string{"db", "--yes", "--wait-timeout=1ms"}
			if tt.nowait {
				args = append(args, "--wait=false")
			}
			err := exec(newDeleteCmd(f), args...)
			if cli.ExitCode(err) != tt.wantCode {
				t.Fatalf("err=%v code=%d", err, cli.ExitCode(err))
			}
			out := f.IOStreams.Out.(*bytes.Buffer).String()
			if tt.wantText != "" && !strings.Contains(out, tt.wantText) {
				t.Fatalf("out=%q", out)
			}
			if tt.timeout {
				var ce *cli.Error
				if !errors.As(err, &ce) || !strings.Contains(ce.Hint, "keeps removing copies") {
					t.Fatalf("err=%+v", err)
				}
			}
			calls := strings.Join(fake.Calls, " ")
			if !tt.nowait && !strings.Contains(calls, "GetSecret:db") {
				t.Fatal("no confirming GET")
			}
			if tt.nowait && strings.Contains(calls, "GetSecret:db") {
				t.Fatal("unexpected wait")
			}
		})
	}
}

func TestSecretWaitDeletedAndMisuse(t *testing.T) {
	f := factoryWith(t, clienttest.New())
	if err := exec(newWaitCmd(f), "db", "--for=deleted"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.IOStreams.Out.(*bytes.Buffer).String(), "secret/db deleted") {
		t.Fatal("missing gone confirmation")
	}
	if err := exec(newWaitCmd(f), "db", "--for=running"); cli.ExitCode(err) != cli.ExitMisuse {
		t.Fatalf("err=%v", err)
	}
}

func TestWaitForDeliveryReturnsTerminalType(t *testing.T) {
	fake := clienttest.New()
	s := deliveredSecret()
	s.Status.Phase = "Deleting"
	fake.Secrets["db"] = s
	f := factoryWith(t, fake)
	_, err := waitForDelivery(context.Background(), f.IOStreams, fake, "db", time.Millisecond)
	var terminal *cmdutil.TerminalPhaseError
	if !errors.As(err, &terminal) {
		t.Fatalf("err=%v", err)
	}
}

func TestCurrentTargetConflictExplainsHowToChooseAnotherName(t *testing.T) {
	fake := clienttest.New()
	s := deliveredSecret()
	s.Status.ObservedGeneration = 1
	s.Status.SyncStatuses[0].Phase = "Failed"
	s.Status.SyncStatuses[0].Reason = "TargetConflict"
	fake.Secrets["db"] = s
	f := factoryWith(t, fake)
	err := exec(newWaitCmd(f), "db", "--timeout=1ms")
	var ce *cli.Error
	if !errors.As(err, &ce) || ce.Code != cli.ExitGeneral || !strings.Contains(ce.Msg, "c/ns") {
		t.Fatalf("err=%+v", err)
	}
	const hint = "A secret with this name already exists in the namespace and was not created by Kupe. Choose another name with --sync CLUSTER:NAMESPACE:SECRET_NAME."
	if !strings.Contains(ce.Hint, hint) {
		t.Fatalf("hint=%q", ce.Hint)
	}
}
