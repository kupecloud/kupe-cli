package clienttest

import (
	"context"
	"testing"

	"github.com/kupecloud/kupe-cli/internal/client"
)

func TestAppReleaseFakeRetriesCurrentConfigurationAndClonesHistory(t *testing.T) {
	fake := New()
	fake.AppEnvironmentsByApp["shop"] = map[string]*client.AppEnvironment{"production": {UID: "uid", Generation: 4, DeploymentRevision: 2, Web: &client.AppWeb{Image: "digest"}}}
	accepted, _, err := fake.RetryAppEnvironment(context.Background(), "shop", "production", "etag-fake")
	if err != nil || accepted.Generation != 5 || accepted.DeploymentRevision != 3 || fake.AppEnvironmentsByApp["shop"]["production"].Web.Image != "digest" {
		t.Fatalf("accepted=%+v err=%v", accepted, err)
	}
	_, _, err = fake.RetryAppEnvironment(context.Background(), "shop", "production", "stale")
	if !client.IsPreconditionFailed(err) || fake.AppEnvironmentsByApp["shop"]["production"].DeploymentRevision != 3 {
		t.Fatalf("stale retry err=%v", err)
	}
	fake.AppReleases = []client.AppRelease{{Name: "first", Number: 1}, {Name: "second", Number: 2, Runtime: client.AppReleaseRuntime{Secrets: []client.AppReleaseSecret{{Name: "db", Keys: []string{"KEY"}}}}}}
	items, err := fake.ListAppReleases(context.Background(), "shop", "production")
	if err != nil || items[0].Name != "second" {
		t.Fatalf("items=%+v err=%v", items, err)
	}
	items[0].Runtime.Secrets[0].Keys[0] = "changed"
	if fake.AppReleases[1].Runtime.Secrets[0].Keys[0] != "KEY" {
		t.Fatal("history result aliases fake storage")
	}
}
