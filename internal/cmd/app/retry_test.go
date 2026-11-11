package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
)

func TestRetryReturnsNewRevisionAndUsesReviewedETag(t *testing.T) {
	fake := releaseFake()
	out, review, err := run(factoryWith(fake), "retry", "shop", "production", "--yes", "--wait=false", "-o", "json")
	if err != nil || len(fake.AppRetryETags) != 1 || fake.AppRetryETags[0] != "etag-fake" || !strings.Contains(out, `"deploymentRevision": 2`) || !strings.Contains(review, "Review retry") {
		t.Fatalf("out=%s review=%s err=%v", out, review, err)
	}
}

func TestRetryTreatsAbsentStoredRevisionAsOne(t *testing.T) {
	fake := releaseFake()
	fake.AppEnvironmentsByApp["shop"]["production"].DeploymentRevision = 0
	out, _, err := run(factoryWith(fake), "retry", "shop", "production", "--yes", "--wait=false", "-o", "json")
	if err != nil || !strings.Contains(out, `"deploymentRevision": 2`) {
		t.Fatalf("out=%s err=%v", out, err)
	}
}

func TestReleaseWriteFailuresKeepExitIdentityWithoutEchoingBodyOrTransport(t *testing.T) {
	const sentinel = "synthetic-private-error-output"
	for _, tc := range []struct {
		name   string
		err    error
		code   int
		phrase string
	}{
		{"transport", errors.New(sentinel), 7, "outcome is unknown"},
		{"body", &client.APIError{StatusCode: 502, Message: sentinel, RequestID: "request-safe"}, 7, "outcome is unknown"},
		{"setup", &client.RequestNotSentError{Err: errors.New(sentinel)}, 7, "request was not sent"},
		{"credentials", &client.RequestNotSentError{Err: errors.New(sentinel), TokenResolution: true}, 3, "request was not sent"},
		{"cancel", context.Canceled, 130, "outcome is unknown"},
		{"terminating", &client.APIError{StatusCode: 409, Code: "terminating", Message: sentinel}, 5, "being deleted"},
		{"max", &client.APIError{StatusCode: 400, Field: "deploymentRevision", Message: sentinel}, 2, "cannot be retried again"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := releaseFake()
			fake.RetryAppEnvironmentErr = tc.err
			out, review, err := run(factoryWith(fake), "retry", "shop", "production", "--yes", "--wait=false")
			if cli.ExitCode(err) != tc.code || !strings.Contains(err.Error(), tc.phrase) || strings.Contains(out+review+err.Error(), sentinel) || !errors.Is(err, tc.err) {
				t.Fatalf("out=%s review=%s err=%v exit=%d", out, review, err, cli.ExitCode(err))
			}
		})
	}
}

func TestReleaseEnvironmentSelection(t *testing.T) {
	for _, args := range [][]string{{"deploy", "shop", "--image", nextDigest, "--yes", "--wait=false"}, {"retry", "shop", "--yes", "--wait=false"}, {"wait", "shop", "--timeout", "1ms"}, {"releases", "shop"}} {
		t.Run(strings.Join(args, "/"), func(t *testing.T) {
			fake := releaseFake()
			fake.AppEnvironmentsByApp["shop"]["staging"] = releaseEnvironment()
			fake.AppEnvironmentsByApp["shop"]["staging"].Environment = "staging"
			_, _, err := run(factoryWith(fake), args...)
			if cli.ExitCode(err) != 2 || !strings.Contains(err.Error(), "production, staging") || len(fake.AppRetryETags) != 0 || len(fake.AppEnvironmentPatches) != 0 {
				t.Fatalf("args=%v err=%v calls=%v", args, err, fake.Calls)
			}
			fake = releaseFake()
			_, _, err = run(factoryWith(fake), args...)
			if cli.ExitCode(err) != 0 && cli.ExitCode(err) != 8 {
				t.Fatalf("sole env: args=%v err=%v", args, err)
			}
		})
	}
}

func TestNewAppCommandsPreserveAppsUnavailableMessageAndFallback(t *testing.T) {
	for _, args := range [][]string{
		{"deploy", "shop", "production", "--image", nextDigest, "--yes", "--wait=false"},
		{"deploy", "shop", "production", "--image", "hello:release", "--yes", "--wait=false"},
		{"retry", "shop", "production", "--yes", "--wait=false"},
		{"wait", "shop", "production", "--timeout", "1m"},
		{"releases", "shop", "production"},
		{"releases", "shop"},
		{"image", "resolve", "hello:release"},
	} {
		t.Run(strings.Join(args, "/"), func(t *testing.T) {
			for _, message := range []string{"apps are not available on this platform", "apps are temporarily unavailable for this tenant", "", " \t "} {
				fake := releaseFake()
				refusal := &client.APIError{StatusCode: 404, Code: "apps_unavailable", Message: message, RequestID: "apps-request-123"}
				fake.GetAppEnvironmentErr = refusal
				fake.ListAppEnvironmentsErr = refusal
				fake.ListAppReleasesErr = refusal
				fake.ResolveAppImageErr = refusal
				f := factoryWith(fake)
				if args[0] == "wait" {
					f = onePollFactory(t, fake, 1) // the refusal ends the wait on its first read
				}
				_, _, err := run(f, args...)
				want := message
				if strings.TrimSpace(message) == "" {
					want = "apps are not available on this platform"
				}
				want += " (request-id: apps-request-123)"
				if cli.ExitCode(err) != 7 || err.Error() != want {
					t.Fatalf("args=%v err=%v exit=%d want=%q", args, err, cli.ExitCode(err), want)
				}
				if refusal.Message != message {
					t.Fatal("original API error was mutated")
				}
				if len(fake.AppEnvironmentPatches) != 0 || len(fake.AppRetryETags) != 0 {
					t.Fatalf("apps-unavailable path wrote: %v", fake.Calls)
				}
			}
		})
	}
}

func TestRetryHelpSaysItAppliesSavedValues(t *testing.T) {
	cmd := newRetryCmd(nil)
	if !strings.Contains(cmd.Long, "fetches fresh secret values") {
		t.Fatal(cmd.Long)
	}
}
