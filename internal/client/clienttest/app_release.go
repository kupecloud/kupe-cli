package clienttest

import (
	"context"
	"slices"

	"github.com/kupecloud/kupe-cli/internal/client"
)

func (f *Fake) RetryAppEnvironment(_ context.Context, app, env, etag string) (*client.AppRetry, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("RetryAppEnvironment:" + app + "/" + env)
	f.AppRetryETags = append(f.AppRetryETags, etag)
	if f.RetryAppEnvironmentErr != nil {
		return nil, "", f.RetryAppEnvironmentErr
	}
	if f.AppRetryResponse != nil {
		return cloneAppValue(f.AppRetryResponse), "etag-fake", nil
	}
	current := f.AppEnvironmentsByApp[app][env]
	if current == nil {
		return nil, "", &client.APIError{StatusCode: 404, Message: "environment not found"}
	}
	if current.Status != nil && current.Status.Phase == client.PhaseTerminating {
		return nil, "", &client.APIError{StatusCode: 409, Code: client.CodeTerminating, Message: "environment is being deleted"}
	}
	if etag != "" && etag != "etag-fake" {
		return nil, "", &client.APIError{StatusCode: 412, Code: "precondition_failed"}
	}
	if current.DeploymentRevision >= 2147483647 {
		return nil, "", &client.APIError{StatusCode: 400, Field: "deploymentRevision", Message: "the environment cannot be retried again; change its configuration to deploy"}
	}
	if current.DeploymentRevision == 0 {
		current.DeploymentRevision = 1
	}
	current.DeploymentRevision++
	current.Generation++
	return &client.AppRetry{App: app, Environment: env, DeploymentRevision: current.DeploymentRevision, Generation: current.Generation}, "etag-fake", nil
}
func (f *Fake) ListAppReleases(_ context.Context, app, env string) ([]client.AppRelease, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("ListAppReleases:" + app + "/" + env)
	if f.ListAppReleasesErr != nil {
		return nil, f.ListAppReleasesErr
	}
	items := f.AppReleases
	if len(f.AppReleaseSequences) > 0 {
		items = f.AppReleaseSequences[0]
		if len(f.AppReleaseSequences) > 1 {
			f.AppReleaseSequences = f.AppReleaseSequences[1:]
		}
	}
	copy := cloneAppValue(&items)
	slices.SortStableFunc(*copy, func(a, b client.AppRelease) int {
		if a.Number > b.Number {
			return -1
		}
		if a.Number < b.Number {
			return 1
		}
		return 0
	})
	return *copy, nil
}
func (f *Fake) ResolveAppImage(_ context.Context, image string) (*client.ImageResolution, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("ResolveAppImage")
	f.ResolvedImages = append(f.ResolvedImages, image)
	if f.ResolveAppImageErr != nil {
		return nil, f.ResolveAppImageErr
	}
	return cloneAppValue(f.ImageResolution), nil
}
