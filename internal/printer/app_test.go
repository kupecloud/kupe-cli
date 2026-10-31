package printer

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
)

func TestAppEnvironmentFailedDeploymentAndRecoveredActive(t *testing.T) {
	io, _, _ := cli.Test()
	io.Out = &bytes.Buffer{}
	env := &client.AppEnvironment{App: "shop", Environment: "production", Cluster: "europe", Status: &client.AppEnvironmentStatus{Phase: "Degraded", Deployment: &client.AppDeployment{Release: &client.AppReleaseReference{Name: "shop-production-2"}, Phase: "Failed", Reason: "HealthCheckFailed"}, RequestedRelease: &client.AppReleaseReference{Name: "shop-production-2"}, ActiveRelease: &client.AppReleaseReference{Name: "shop-production-1"}, LastSuccessfulRelease: &client.AppReleaseReference{Name: "shop-production-1"}, Recovery: &client.AppRecovery{Phase: "Recovered", Release: &client.AppReleaseReference{Name: "shop-production-1"}}, Domains: []client.AppDomainStatus{{Host: "shop.example.com", Phase: "Pending", Record: "CNAME shop.example.com target.example.com"}}, Secrets: []client.AppSecretStatus{{Name: "payments", Phase: "Pending", MissingKeys: []string{"API_KEY"}}}}}
	env.Status.Address = "https://shop.example.com"
	env.Status.Namespace = "app-shop-production"
	env.Domains = []string{"shop.example.com"}
	if err := AppEnvironment.One(io, &Format{Kind: Table}, env); err != nil {
		t.Fatal(err)
	}
	got := io.Out.(*bytes.Buffer).String()
	path := filepath.Join("testdata", "app-environment-recovered.golden")
	if *updateGolden {
		if err := os.WriteFile(path, []byte(got), 0600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	for _, text := range []string{"Degraded", "shop-production-2 Failed HealthCheckFailed", "Active release:", "shop-production-1", "Recovered shop-production-1", "CNAME shop.example.com target.example.com", "payments Pending (missing: API_KEY)"} {
		if !strings.Contains(got, text) {
			t.Errorf("missing %q in %s", text, got)
		}
	}
}
