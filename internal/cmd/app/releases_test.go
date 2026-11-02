package app

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
)

func TestReleaseHistoryNewestFirstAndSingleRetainedRelease(t *testing.T) {
	for _, format := range []string{"table", "json"} {
		fake := releaseFake()
		e := fake.AppEnvironmentsByApp["shop"]["production"]
		first := recordedRelease("shop-production-1", "Succeeded", 1, 1, e)
		second := recordedRelease("shop-production-2", "Failed", 2, 2, e)
		second.Trigger = "Retry"
		second.Runtime.Image = nextDigest
		second.Status.StartedAt = "2026-11-02T10:00:00Z"
		second.Status.CompletedAt = "2026-11-02T10:10:00Z"
		second.Status.Reason = "DeadlineExceeded"
		second.Status.Recovery = &client.AppReleaseRecovery{Phase: "Succeeded", Release: &client.AppReleaseReference{Name: first.Name}}
		fake.AppReleases = []client.AppRelease{first, second}
		out, _, err := run(factoryWith(fake), "releases", "shop", "production", "-o", format)
		if err != nil {
			t.Fatal(err)
		}
		if format == "json" {
			var got []client.AppRelease
			if err := json.Unmarshal([]byte(out), &got); err != nil || len(got) != 2 || got[0].Number != 2 || got[1].Number != 1 {
				t.Fatalf("out=%s err=%v", out, err)
			}
		} else if strings.Index(out, "Retry") > strings.Index(out, "Deploy") || !strings.Contains(out, "DeadlineExceeded") || !strings.Contains(out, "2026-11-02T10:10:00Z") {
			t.Fatal(out)
		}
		out, _, err = run(factoryWith(fake), "releases", "shop", "production", "2", "-o", format)
		if err != nil || !strings.Contains(out, "Retry") || !strings.Contains(out, nextDigest) {
			t.Fatalf("out=%s err=%v", out, err)
		}
	}
}

func TestReleaseHistoryPrunedNumberAndEmptyList(t *testing.T) {
	fake := releaseFake()
	_, _, err := run(factoryWith(fake), "releases", "shop", "production", "7")
	if cli.ExitCode(err) != 4 || !strings.Contains(err.Error(), "retained history") {
		t.Fatalf("err=%v", err)
	}
	out, _, err := run(factoryWith(fake), "releases", "shop", "-o", "json")
	if err != nil || strings.TrimSpace(out) != "[]" {
		t.Fatalf("out=%s err=%v", out, err)
	}
	_, _, err = run(factoryWith(fake), "releases", "shop", "production", "0")
	if cli.ExitCode(err) != 2 {
		t.Fatalf("err=%v", err)
	}
}

func TestReleaseNumberAliasSupportsOmittedAndNumericEnvironment(t *testing.T) {
	fake := releaseFake()
	e := fake.AppEnvironmentsByApp["shop"]["production"]
	fake.AppReleases = []client.AppRelease{recordedRelease("shop-production-7", "Succeeded", 7, 1, e)}
	out, _, err := run(factoryWith(fake), "releases", "shop", "--number", "7", "-o", "json")
	if err != nil || !strings.Contains(out, `"number": 7`) {
		t.Fatalf("out=%s err=%v", out, err)
	}
	e.Environment = "7"
	fake.AppEnvironmentsByApp["shop"] = map[string]*client.AppEnvironment{"7": e}
	fake.Calls = nil
	_, _, err = run(factoryWith(fake), "releases", "shop", "7", "--number", "7")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, call := range fake.Calls {
		if call == "ListAppReleases:shop/7" {
			found = true
		}
	}
	if !found {
		t.Fatalf("numeric environment was not selected: %v", fake.Calls)
	}
	fake.AppEnvironmentsByApp["shop"]["staging"] = releaseEnvironment()
	fake.AppEnvironmentsByApp["shop"]["staging"].Environment = "staging"
	_, _, err = run(factoryWith(fake), "releases", "shop", "--number", "7")
	if cli.ExitCode(err) != 2 || !strings.Contains(err.Error(), "7, staging") {
		t.Fatalf("err=%v", err)
	}
	_, _, err = run(factoryWith(fake), "releases", "shop", "7", "7", "--number", "7")
	if cli.ExitCode(err) != 2 || !strings.Contains(err.Error(), "not both") {
		t.Fatalf("err=%v", err)
	}
}
