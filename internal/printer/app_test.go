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

func TestAppEnvironmentShowsSecretChanges(t *testing.T) {
	const message = "secret changed since this release: payments. Pods started before the change keep the old value until you redeploy"
	for _, tc := range []struct {
		name   string
		status string
		want   bool
	}{{"changed", "True", true}, {"not true", "False", false}} {
		t.Run(tc.name, func(t *testing.T) {
			io, _, _ := cli.Test()
			io.Out = &bytes.Buffer{}
			env := &client.AppEnvironment{App: "shop", Environment: "production", Status: &client.AppEnvironmentStatus{Conditions: []client.Condition{{Type: "SecretsChanged", Status: tc.status, Message: message}}}}
			if err := AppEnvironment.One(io, &Format{Kind: Table}, env); err != nil {
				t.Fatal(err)
			}
			got := strings.Join(strings.Fields(io.Out.(*bytes.Buffer).String()), " ")
			if strings.Contains(got, message) != tc.want || !strings.Contains(got, "Secret changes:") {
				t.Fatalf("got %s", got)
			}
		})
	}
}

func TestAppEnvironmentShowsDatabases(t *testing.T) {
	io, _, _ := cli.Test()
	io.Out = &bytes.Buffer{}
	env := &client.AppEnvironment{App: "shop", Environment: "production",
		Databases: []client.AppDatabase{{Name: "main", Database: "orders", LogicalDatabase: "shop", Env: "DATABASE_URL"}, {Name: "reports", Database: "orders", Env: "REPORTS_URL"}},
		Status: &client.AppEnvironmentStatus{Databases: []client.AppDatabaseStatus{
			{Name: "main", Env: "DATABASE_URL", Phase: "Ready", RuntimeSecret: "main-app", MigrationSecret: "main-migrate"},
			{Name: "reports", Env: "REPORTS_URL", Phase: "Pending", Message: "waiting for the database connection to be ready"}}}}
	if err := AppEnvironment.One(io, &Format{Kind: Table}, env); err != nil {
		t.Fatal(err)
	}
	got := io.Out.(*bytes.Buffer).String()
	for _, text := range []string{"main orders/shop DATABASE_URL", "reports orders REPORTS_URL", "main Ready", "reports Pending (waiting for the database connection to be ready)"} {
		if !strings.Contains(got, text) {
			t.Errorf("missing %q in %s", text, got)
		}
	}
	if strings.Contains(got, "main-app") || strings.Contains(got, "main-migrate") {
		t.Errorf("table shows secret names: %s", got)
	}
	io, _, _ = cli.Test()
	io.Out = &bytes.Buffer{}
	if err := AppEnvironment.One(io, &Format{Kind: JSON}, env); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{`"runtimeSecret": "main-app"`, `"migrationSecret": "main-migrate"`, `"logicalDatabase": "shop"`} {
		if !strings.Contains(io.Out.(*bytes.Buffer).String(), text) {
			t.Errorf("json missing %q", text)
		}
	}
}

func TestAppPods(t *testing.T) {
	two := int32(2)
	for _, tc := range []struct {
		runtime *client.AppRuntime
		want    string
	}{
		{nil, ""},
		{&client.AppRuntime{Replicas: &two}, "2"},
		{&client.AppRuntime{Replicas: &two, Autoscaling: &client.AppAutoscaling{MinPods: 2, MaxPods: 6, CPUPercent: 60}}, "2 to 6, aiming for 60% CPU"},
		{&client.AppRuntime{Autoscaling: &client.AppAutoscaling{MinPods: 1, MaxPods: 3}}, "1 to 3, aiming for 70% CPU"},
	} {
		if got := appPods(tc.runtime); got != tc.want {
			t.Errorf("%+v: got %q want %q", tc.runtime, got, tc.want)
		}
	}
}
