package printer

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
)

func TestAppReleasePrinterSeparatesFailedOutcomeAndRecovery(t *testing.T) {
	for _, kind := range []Kind{Table, JSON} {
		io, out, _ := cli.Test()
		item := &client.AppRelease{Name: "shop-prod-2", Number: 2, Trigger: "Retry", Runtime: client.AppReleaseRuntime{AppWeb: client.AppWeb{Image: "registry.example/app@sha256:test"}}, Status: client.AppReleaseStatus{Phase: "Failed", Reason: "SyncFailed", Recovery: &client.AppReleaseRecovery{Phase: "Succeeded", Release: &client.AppReleaseReference{Name: "shop-prod-1"}}}}
		if err := AppRelease.One(io, &Format{Kind: kind}, item); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), "Failed") || !strings.Contains(out.String(), "Succeeded") || !strings.Contains(out.String(), "Retry") || !strings.Contains(out.String(), "registry.example/app@sha256:test") {
			t.Fatal(out.String())
		}
		if kind == JSON {
			var got map[string]any
			if err := json.Unmarshal(out.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			runtime := got["runtime"].(map[string]any)
			if runtime["image"] == nil || runtime["web"] != nil {
				t.Fatalf("runtime=%v", runtime)
			}
		}
	}
}
