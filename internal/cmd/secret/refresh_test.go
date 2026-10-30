package secret

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/client/clienttest"
)

func refreshedSecret(token string, delivered bool) *client.Secret {
	s := deliveredSecret()
	s.Status.Refresh = &client.SecretRefreshStatus{RequestedAt: token, Pending: !delivered}
	if delivered {
		s.Status.Refresh.ObservedAt = token
		s.Status.SyncStatuses[0].DeliveredRefresh = token
	} else {
		s.Status.SyncStatuses[0].Phase = "Pending"
	}
	return s
}

func TestRefreshWaitsAndRendersJSON(t *testing.T) {
	fake := clienttest.New()
	fake.Secrets["db"] = deliveredSecret()
	fake.RefreshSecretRequestedAt = "first"
	fake.GetSecretSeq["db"] = []*client.Secret{refreshedSecret("first", true)}
	f := factoryWith(t, fake)
	if err := exec(newRefreshCmd(f), "db", "-o", "json"); err != nil {
		t.Fatal(err)
	}
	var got refreshResult
	if err := json.Unmarshal(f.IOStreams.Out.(*bytes.Buffer).Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Secret != "db" || got.RequestedAt != "first" || !got.Delivered {
		t.Fatalf("got=%+v", got)
	}
}

func TestRefreshFollowsNewerTokenOnce(t *testing.T) {
	fake := clienttest.New()
	fake.Secrets["db"] = deliveredSecret()
	fake.RefreshSecretRequestedAt = "first"
	fake.GetSecretSeq["db"] = []*client.Secret{refreshedSecret("first", false), refreshedSecret("second", false), refreshedSecret("second", true)}
	f := factoryWith(t, fake)
	if err := exec(newRefreshCmd(f), "db", "--wait-timeout=5s", "-o", "json"); err != nil {
		t.Fatal(err)
	}
	notice := f.IOStreams.ErrOut.(*bytes.Buffer).String()
	if strings.Count(notice, "A newer refresh (second) replaced this one; waiting for it instead.") != 1 {
		t.Fatalf("notice=%q", notice)
	}
	var got refreshResult
	if err := json.Unmarshal(f.IOStreams.Out.(*bytes.Buffer).Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.RequestedAt != "second" || !got.Delivered {
		t.Fatalf("got=%+v", got)
	}
}

func TestRefreshTimeoutProviderErrorAndNoProgress(t *testing.T) {
	for _, tt := range []struct {
		name     string
		s        *client.Secret
		wantCode int
		wantText string
	}{
		{name: "timeout", s: refreshedSecret("first", false), wantCode: cli.ExitTimeout},
		{name: "older API", s: deliveredSecret(), wantText: "secret/db refresh requested"},
		{name: "Deleting", s: &client.Secret{Name: "db", Status: &client.SecretStatus{Phase: "Deleting"}}, wantCode: cli.ExitGeneral},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fake := clienttest.New()
			fake.Secrets["db"] = deliveredSecret()
			fake.RefreshSecretRequestedAt = "first"
			fake.GetSecretSeq["db"] = []*client.Secret{tt.s}
			f := factoryWith(t, fake)
			err := exec(newRefreshCmd(f), "db", "--wait-timeout=1ms")
			if cli.ExitCode(err) != tt.wantCode {
				t.Fatalf("err=%v code=%d", err, cli.ExitCode(err))
			}
			if tt.wantText != "" && !strings.Contains(f.IOStreams.Out.(*bytes.Buffer).String(), tt.wantText) {
				t.Fatal("missing legacy acknowledgement")
			}
			if tt.name == "older API" && strings.Count(strings.Join(fake.Calls, " "), "GetSecret:db") != 1 {
				t.Fatalf("legacy API waited: %v", fake.Calls)
			}
		})
	}
}

func TestRefreshRidesOutProviderAndTransientFailures(t *testing.T) {
	fake := clienttest.New()
	fake.Secrets["db"] = deliveredSecret()
	fake.RefreshSecretRequestedAt = "first"
	failed := refreshedSecret("first", false)
	failed.Status.Phase = "Degraded"
	failed.Status.SyncStatuses[0].Phase = "Failed"
	failed.Status.SyncStatuses[0].Reason = "ProviderError"
	fake.GetSecretSeq["db"] = []*client.Secret{failed, failed, refreshedSecret("first", true)}
	fake.GetSecretErrors = []error{nil, &client.APIError{StatusCode: 503}, nil, nil}
	f := factoryWith(t, fake)
	if err := exec(newRefreshCmd(f), "db", "--wait-timeout=8s"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.IOStreams.Out.(*bytes.Buffer).String(), "secret/db refreshed") {
		t.Fatal("missing refreshed")
	}
}

func TestRefreshConflictAndTerminatingPOST(t *testing.T) {
	for _, post := range []bool{false, true} {
		t.Run(map[bool]string{true: "POST terminating", false: "target conflict"}[post], func(t *testing.T) {
			fake := clienttest.New()
			fake.Secrets["db"] = deliveredSecret()
			fake.RefreshSecretRequestedAt = "first"
			if post {
				fake.RefreshSecretErr = &client.APIError{StatusCode: 409, Code: "terminating"}
			} else {
				s := refreshedSecret("first", false)
				s.Status.SyncStatuses[0].Phase = "Failed"
				s.Status.SyncStatuses[0].Reason = "TargetConflict"
				fake.GetSecretSeq["db"] = []*client.Secret{s}
			}
			f := factoryWith(t, fake)
			err := exec(newRefreshCmd(f), "db", "--wait-timeout=1ms")
			if cli.ExitCode(err) != cli.ExitGeneral {
				t.Fatalf("err=%v", err)
			}
			if !post && !strings.Contains(err.Error(), "c/ns") {
				t.Fatal("target not named")
			}
		})
	}
}

func TestRefreshDelivered(t *testing.T) {
	s := refreshedSecret("token", true)
	if !refreshDelivered(s, "token") {
		t.Fatal("delivered token not accepted")
	}
	if refreshDelivered(s, "older") {
		t.Fatal("wrong token accepted")
	}
	s.Status.SyncStatuses = nil
	if refreshDelivered(s, "token") {
		t.Fatal("missing target accepted")
	}
	s.Sync = nil
	if !refreshDelivered(s, "token") {
		t.Fatal("zero targets must finish once observed")
	}
}

func TestRefreshNoWaitDoesNotGet(t *testing.T) {
	fake := clienttest.New()
	fake.Secrets["db"] = deliveredSecret()
	f := factoryWith(t, fake)
	if err := exec(newRefreshCmd(f), "db", "--wait=false"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(fake.Calls, " "), "GetSecret:") {
		t.Fatal("no-wait read status")
	}
}
