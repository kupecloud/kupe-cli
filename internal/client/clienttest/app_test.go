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
