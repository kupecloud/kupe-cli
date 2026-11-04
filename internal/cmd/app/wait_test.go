package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/client/clienttest"
	"github.com/kupecloud/kupe-cli/internal/cmd/cmdutil"
)

func recordedRelease(name, phase string, number, revision int64, env *client.AppEnvironment) client.AppRelease {
	return client.AppRelease{Name: name, Number: number, DeploymentRevision: revision, Trigger: "Deploy", Runtime: client.AppReleaseRuntime{AppWeb: *env.Web}, Status: client.AppReleaseStatus{Phase: phase}}
}

func TestWaitUsesRequestedReleaseOutcomeIncludingRecoveryAndSupersession(t *testing.T) {
	for _, tc := range []struct {
		name, phase, recovery string
		code                  int
		phrase                string
	}{
		{"success", "Succeeded", "", 0, ""},
		{"failed", "Failed", "", 1, "release shop-production-2 failed"},
		{"recovered", "Failed", "Succeeded", 1, "release shop-production-2 failed; the previous release is serving again"},
		{"recovering", "Failed", "Deploying", 1, "release shop-production-2 failed; recovery: Deploying"},
		{"superseded", "Superseded", "", 1, "current requested release is shop-production-3"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := releaseFake()
			e := fake.AppEnvironmentsByApp["shop"]["production"]
			e.Status.RequestedRelease = &client.AppReleaseReference{Name: "shop-production-2"}
			old := recordedRelease("shop-production-1", "Succeeded", 1, 1, e)
			target := recordedRelease("shop-production-2", tc.phase, 2, 1, e)
			if tc.recovery != "" {
				target.Status.Recovery = &client.AppReleaseRecovery{Phase: tc.recovery, Release: &client.AppReleaseReference{Name: old.Name}}
			}
			fake.AppReleases = []client.AppRelease{old, target}
			if tc.phase == "Superseded" {
				newer := recordedRelease("shop-production-3", "Deploying", 3, 2, e)
				fake.AppReleases = append(fake.AppReleases, newer)
				// Pin 2 at the first read; after that the requested release is 3.
				first := *e
				later := *e
				status := *e.Status
				status.RequestedRelease = &client.AppReleaseReference{Name: newer.Name}
				later.Status = &status
				fake.GetAppEnvironmentSeq["shop"]["production"] = []*client.AppEnvironment{&first, &later}
			}
			out, _, err := run(factoryWith(fake), "wait", "shop", "production", "--timeout", "20ms", "-o", "json")
			if cli.ExitCode(err) != tc.code {
				t.Fatalf("exit=%d err=%v out=%s", cli.ExitCode(err), err, out)
			}
			if tc.phrase != "" && !strings.Contains(err.Error(), tc.phrase) {
				t.Fatalf("err=%v", err)
			}
			if tc.phase == "Failed" {
				// The failure is printed once: the sentence, without the
				// terminal phase repeated after it.
				if err.Error() != tc.phrase {
					t.Fatalf("err=%q, want %q", err, tc.phrase)
				}
				var terminal *cmdutil.TerminalPhaseError
				if !errors.As(err, &terminal) || terminal.Name != target.Name {
					t.Fatalf("failed release lost terminal identity: %v", err)
				}
			}
			for _, call := range fake.Calls {
				if strings.HasPrefix(call, "Update") || strings.HasPrefix(call, "Retry") {
					t.Fatal("wait wrote: " + call)
				}
			}
		})
	}
}

func TestWaitRequestedReleaseSnapshotUsesNewerNameWithoutInventingReplacement(t *testing.T) {
	fake := releaseFake()
	e := fake.AppEnvironmentsByApp["shop"]["production"]
	target := recordedRelease("shop-production-1", "Superseded", 1, 1, e)
	fake.AppReleases = []client.AppRelease{target}
	_, _, err := run(factoryWith(fake), "wait", "shop", "production", "--timeout", "20ms")
	if cli.ExitCode(err) != 1 || !strings.Contains(err.Error(), "newer release name is not available") {
		t.Fatalf("err=%v", err)
	}
}

func TestWriteWaitDoesNotAcceptOldObservedGenerationOrWrongRetryRevision(t *testing.T) {
	for _, tc := range []struct {
		name               string
		observed, revision int64
	}{
		{"old-observation", 1, 2}, {"old-revision", 2, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := releaseFake()
			e := fake.AppEnvironmentsByApp["shop"]["production"]
			e.Generation, e.Status.ObservedGeneration = 2, tc.observed
			e.Status.RequestedRelease = &client.AppReleaseReference{Name: "shop-production-2"}
			fake.AppReleases = []client.AppRelease{recordedRelease("shop-production-2", "Succeeded", 2, tc.revision, e)}
			f := factoryWith(fake)
			_, err := waitForRelease(context.Background(), f.IOStreams, fake, "shop", "production", releaseTarget{uid: e.UID, generation: 2, revision: 2, previous: "shop-production-1", web: e.Web}, 5*time.Millisecond)
			var ce *cli.Error
			if cli.ExitCode(err) != 8 || !errors.As(err, &ce) || !strings.Contains(ce.Hint, "kupe app wait shop production") {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestWaitPinsMatchingRevisionThenKeepsFailedRequestedOutcome(t *testing.T) {
	fake := releaseFake()
	e := fake.AppEnvironmentsByApp["shop"]["production"]
	e.Generation, e.DeploymentRevision, e.Status.ObservedGeneration = 2, 2, 2
	e.Status.RequestedRelease = &client.AppReleaseReference{Name: "shop-production-2"}
	e.Status.ActiveRelease = &client.AppReleaseReference{Name: "shop-production-1"}
	e.Status.Recovery = &client.AppRecovery{Phase: "Succeeded", Release: e.Status.ActiveRelease}
	fake.AppReleases = []client.AppRelease{recordedRelease("shop-production-2", "Failed", 2, 2, e), recordedRelease("shop-production-1", "Succeeded", 1, 1, e)}
	f := factoryWith(fake)
	_, err := waitForRelease(context.Background(), f.IOStreams, fake, "shop", "production", releaseTarget{uid: e.UID, generation: 2, revision: 2, previous: "shop-production-1", web: e.Web}, 20*time.Millisecond)
	if cli.ExitCode(err) != 1 || !strings.Contains(err.Error(), "previous release is serving again") {
		t.Fatalf("err=%v", err)
	}
}

func TestWaitRejectsIncarnationReplacementBetweenEnvironmentAndHistoryGET(t *testing.T) {
	gets := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		e := releaseEnvironment()
		if strings.HasSuffix(r.URL.Path, "/releases") {
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []client.AppRelease{recordedRelease("shop-production-1", "Succeeded", 1, 1, e)}})
			return
		}
		gets++
		if gets > 1 {
			e.UID = "replacement-uid"
		}
		_ = json.NewEncoder(w).Encode(e)
	}))
	defer server.Close()
	f := factoryWith(clienttest.New())
	f.Client = func() (client.Interface, error) { return client.New(server.URL, "acme", "", "test"), nil }
	_, _, err := run(f, "wait", "shop", "production", "--timeout", "20ms")
	if cli.ExitCode(err) != 1 || !strings.Contains(err.Error(), "replaced") || gets != 2 {
		t.Fatalf("gets=%d err=%v", gets, err)
	}
}

// Approved v1 contract: a later identical runtime cannot prove which accepted
// generation created its release; non-deploying changes show why this error must
// not assert Superseded.
func TestPrePinGenerationRaceNeverFollowsLaterIdenticalRuntime(t *testing.T) {
	for _, control := range []string{"A-B-A", "domains-only", "protection-only"} {
		t.Run(control, func(t *testing.T) {
			var mu sync.Mutex
			current := releaseEnvironment()
			var history []client.AppRelease
			patches := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				if strings.HasSuffix(r.URL.Path, "/releases") {
					_ = json.NewEncoder(w).Encode(map[string]any{"items": history})
					return
				}
				w.Header().Set("ETag", `"current-environment-uid.1"`)
				if r.Method == "GET" {
					_ = json.NewEncoder(w).Encode(current)
					return
				}
				if r.Method != "PATCH" {
					t.Errorf("method=%s", r.Method)
					return
				}
				patches++
				var patch client.PatchAppEnvironmentRequest
				if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
					t.Error(err)
					return
				}
				current.Web = patch.Web
				current.Generation = 2
				// Return accepted A(g). Advance other writers before the first
				// observation, while retaining the accepted response's snapshot.
				_ = json.NewEncoder(w).Encode(current)
				current.Status.RequestedRelease = &client.AppReleaseReference{Name: "shop-production-2"}
				first := recordedRelease("shop-production-2", "Succeeded", 2, 1, current)
				history = []client.AppRelease{first}
				if control == "A-B-A" {
					history[0].Status.Phase = "Superseded"
					current.Generation = 3
					b := *current.Web
					b.Image = digest
					current.Web = &b
					middle := recordedRelease("shop-production-3", "Superseded", 3, 1, current)
					current.Generation = 4
					current.Web = patch.Web
					current.Status.RequestedRelease = &client.AppReleaseReference{Name: "shop-production-4"}
					history = append(history, middle, recordedRelease("shop-production-4", "Succeeded", 4, 1, current))
				} else {
					current.Generation = 3
					if control == "domains-only" {
						current.Domains = []string{"new.example.com"}
					} else {
						current.DeletionProtection = true
					}
				}
				current.Status.ObservedGeneration = current.Generation
			}))
			defer server.Close()
			f := factoryWith(clienttest.New())
			f.Client = func() (client.Interface, error) { return client.New(server.URL, "acme", "", "test"), nil }
			out, _, err := run(f, "deploy", "shop", "production", "--image", nextDigest, "--yes", "--wait-timeout", "100ms", "-o", "json")
			if cli.ExitCode(err) != 7 || !strings.Contains(err.Error(), "check its current state before deploying again") || strings.Contains(err.Error(), "was superseded") || strings.TrimSpace(out) != "" || patches != 1 {
				t.Fatalf("patches=%d out=%s err=%v", patches, out, err)
			}
		})
	}
}

func TestReleaseRuntimeMatchUsesDefaultsQuantitiesAndReferencedSecrets(t *testing.T) {
	e := releaseEnvironment()
	e.Web.Resources.Requests.CPU = "1000m"
	e.Web.HealthChecks = nil
	e.Web.Env = append(e.Web.Env, client.AppEnvVar{Name: "SECRET", ValueFrom: &client.AppValueFrom{SecretKeyRef: client.AppSecretKeyRef{Secret: "database", Key: "KEY"}}})
	r := recordedRelease("shop-production-2", "Succeeded", 2, 1, e)
	resources := *e.Web.Resources
	resources.Requests.CPU = "1"
	r.Runtime.Resources = &resources
	r.Runtime.HealthChecks = &client.AppHealthChecks{Readiness: &client.AppProbe{TCP: &struct{}{}}}
	r.Runtime.Secrets = []client.AppReleaseSecret{{Name: "database", Path: "apps/database", Keys: []string{"KEY"}}}
	target := releaseTarget{revision: 1, web: e.Web, secrets: []client.AppSecret{{Name: "database", Path: "apps/database"}, {Name: "unused", Path: "apps/unused"}}}
	if !matchesRelease(&r, target) {
		t.Fatal("equivalent defaulted workload was not matched")
	}
	r.Runtime.Secrets[0].Path = "apps/new-database"
	if matchesRelease(&r, target) {
		t.Fatal("different referenced secret matched")
	}
}

func TestDeployAndRetryUseSharedReleaseWait(t *testing.T) {
	for _, verb := range []string{"deploy", "retry"} {
		t.Run(verb, func(t *testing.T) {
			fake := releaseFake()
			before := releaseEnvironment()
			final := releaseEnvironment()
			final.Generation, final.Status.ObservedGeneration = 2, 2
			final.Status.RequestedRelease = &client.AppReleaseReference{Name: "shop-production-2"}
			args := []string{verb, "shop", "production", "--yes", "--wait-timeout", "20ms", "-o", "json"}
			if verb == "deploy" {
				final.Web.Image = nextDigest
				args = append(args, "--image", nextDigest)
			} else {
				final.DeploymentRevision = 2
			}
			fake.GetAppEnvironmentSeq["shop"]["production"] = []*client.AppEnvironment{before, final}
			fake.AppReleases = []client.AppRelease{recordedRelease("shop-production-2", "Succeeded", 2, final.DeploymentRevision, final)}
			out, _, err := run(factoryWith(fake), args...)
			var got client.AppEnvironment
			if err != nil {
				t.Fatal(err)
			}
			if verb == "deploy" {
				var result deployResult
				if err := json.Unmarshal([]byte(out), &result); err != nil || result.Release != "shop-production-2" || result.Image != nextDigest {
					t.Fatalf("out=%s err=%v", out, err)
				}
			} else if err := json.Unmarshal([]byte(out), &got); err != nil || got.Generation != 2 || requestedRelease(&got) != "shop-production-2" {
				t.Fatalf("out=%s err=%v", out, err)
			}
		})
	}
}

func TestWaitObservesReturnedGenerationBeforeAcceptingNewRelease(t *testing.T) {
	fake := releaseFake()
	old := releaseEnvironment()
	old.Generation = 2
	final := releaseEnvironment()
	final.Generation, final.Status.ObservedGeneration = 2, 2
	final.Status.RequestedRelease = &client.AppReleaseReference{Name: "shop-production-2"}
	fake.GetAppEnvironmentSeq["shop"]["production"] = []*client.AppEnvironment{old, final}
	fake.AppReleases = []client.AppRelease{recordedRelease("shop-production-2", "Succeeded", 2, 1, final)}
	f := factoryWith(fake)
	item, err := waitForRelease(context.Background(), f.IOStreams, fake, "shop", "production", releaseTarget{uid: old.UID, generation: 2, revision: 1, previous: "shop-production-1", web: old.Web}, 3*time.Second)
	if err != nil || item.Status.ObservedGeneration != 2 {
		t.Fatalf("item=%+v err=%v", item, err)
	}
}

func TestWaitCancellationStopsReadOnlyObservation(t *testing.T) {
	fake := releaseFake()
	f := factoryWith(fake)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cmd := NewCmd(f)
	cmd.SetArgs([]string{"wait", "shop", "production"})
	cmd.SilenceErrors, cmd.SilenceUsage = true, true
	err := cmd.ExecuteContext(ctx)
	if cli.ExitCode(err) != 130 || len(fake.Calls) != 0 {
		t.Fatalf("calls=%v err=%v", fake.Calls, err)
	}
}
