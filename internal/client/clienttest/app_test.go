package clienttest

import (
	"context"
	"errors"
	"testing"

	"github.com/kupecloud/kupe-cli/internal/client"
)

func TestAppFakeRMWUsesSharedRetryPolicy(t *testing.T) {
	fake := New()
	fake.AppsByName["shop"] = &client.App{Name: "shop", DeletionProtection: true}
	fake.UpdateAppErrors = []error{&client.APIError{StatusCode: 412}, nil}
	p := false
	_, err := fake.Apps().UpdateRMW(context.Background(), "shop", func(*client.App) *client.PatchAppRequest { return &client.PatchAppRequest{DeletionProtection: &p} })
	if err != nil || fake.AppsByName["shop"].DeletionProtection {
		t.Fatalf("err=%v app=%+v", err, fake.AppsByName["shop"])
	}
	gets := 0
	for _, call := range fake.Calls {
		if call == "GetApp:shop" {
			gets++
		}
	}
	if gets != 2 {
		t.Fatalf("calls=%v", fake.Calls)
	}
	fake.UpdateAppErrors = []error{&client.APIError{StatusCode: 412}, &client.APIError{StatusCode: 412}}
	_, err = fake.Apps().UpdateRMW(context.Background(), "shop", func(*client.App) *client.PatchAppRequest { return &client.PatchAppRequest{DeletionProtection: &p} })
	if !errors.Is(err, client.ErrRMWContention) || !client.IsPreconditionFailed(err) {
		t.Fatalf("err=%v", err)
	}
}

func TestGenericFakeResourceUsesSharedRMWPolicy(t *testing.T) {
	fake := New()
	fake.DatabasesByName["orders"] = &client.Database{Name: "orders"}
	fake.UpdateDatabaseErr = &client.APIError{StatusCode: 412}
	p := false
	_, err := fake.Databases().UpdateRMW(context.Background(), "orders", func(*client.Database) *client.PatchDatabaseRequest {
		return &client.PatchDatabaseRequest{DeletionProtection: &p}
	})
	if !errors.Is(err, client.ErrRMWContention) {
		t.Fatalf("err=%v", err)
	}
	gets := 0
	for _, call := range fake.Calls {
		if call == "GetDatabase:orders" {
			gets++
		}
	}
	if gets != 2 {
		t.Fatalf("calls=%v", fake.Calls)
	}
}

// The fake applies an environment patch as a JSON merge patch, as the server
// does: members the patch omits stay, a null removes, and a probe's absent
// alternative is cleared.
func TestAppEnvironmentFakeMergesWebPatch(t *testing.T) {
	fake := New()
	replicas := int32(3)
	fake.AppEnvironmentsByApp["shop"] = map[string]*client.AppEnvironment{"production": {
		Name: "shop-production", App: "shop", Environment: "production", Generation: 1,
		Web: &client.AppWeb{
			Image: "img", Port: 8080, Replicas: &replicas,
			HealthChecks: &client.AppHealthChecks{
				Readiness: &client.AppProbe{TCP: &struct{}{}},
				Liveness:  &client.AppProbe{TCP: &struct{}{}},
			},
		},
	}}
	envs := fake.AppEnvironments("shop")
	_, etag, err := envs.Get(context.Background(), "production")
	if err != nil {
		t.Fatal(err)
	}
	// No replicas in the patch, readiness switched from tcp to http, liveness dropped.
	got, _, err := envs.Update(context.Background(), "production", etag, client.PatchAppEnvironmentRequest{Web: &client.AppWeb{
		Image: "img2", Port: 8080,
		HealthChecks: &client.AppHealthChecks{Readiness: &client.AppProbe{HTTP: &client.AppHTTPProbe{Path: "/up"}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	w := got.Web
	if w.Image != "img2" || w.Replicas == nil || *w.Replicas != 3 {
		t.Fatalf("omitted replicas must be retained: %+v", w)
	}
	if w.HealthChecks == nil || w.HealthChecks.Readiness == nil || w.HealthChecks.Readiness.TCP != nil || w.HealthChecks.Readiness.HTTP == nil || w.HealthChecks.Liveness != nil {
		t.Fatalf("probe merge wrong: %+v", w.HealthChecks)
	}
}
