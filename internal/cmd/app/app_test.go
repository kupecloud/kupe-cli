package app

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/client/clienttest"
	"github.com/kupecloud/kupe-cli/internal/config"
)

var updateAppGoldens = flag.Bool("update", false, "Update app command goldens")

func factoryWith(fake *clienttest.Fake) *cli.Factory {
	io, _, _ := cli.Test()
	f := cli.NewFactory(io, &cli.GlobalFlags{})
	f.Config = func() (*config.Config, error) { return &config.Config{}, nil }
	f.Client = func() (client.Interface, error) { return fake, nil }
	return f
}
func run(f *cli.Factory, args ...string) (string, string, error) {
	cmd := NewCmd(f)
	cmd.SetContext(context.Background())
	cmd.SetArgs(args)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetOut(f.IOStreams.Out)
	cmd.SetErr(f.IOStreams.ErrOut)
	err := cmd.Execute()
	return f.IOStreams.Out.(interface{ String() string }).String(), f.IOStreams.ErrOut.(interface{ String() string }).String(), err
}

func TestAppCRUDAndTypedDelete(t *testing.T) {
	fake := clienttest.New()
	f := factoryWith(fake)
	_, _, err := run(f, "create", "shop", "--no-deletion-protection", "-o", "json")
	if err != nil {
		t.Fatal(err)
	}
	if len(fake.AppCreates) != 1 || fake.AppsByName["shop"].DeletionProtection {
		t.Fatal("create protection")
	}
	f = factoryWith(fake)
	_, _, err = run(f, "delete", "shop")
	if cli.ExitCode(err) != 2 {
		t.Fatalf("no TTY: %v", err)
	}
	if len(fake.AppDeleteConfirms) != 0 {
		t.Fatal("delete before confirmation")
	}
	f = factoryWith(fake)
	f.IOStreams.PromptsEnabled = true
	f.IOStreams.In = strings.NewReader("wrong\n")
	_, _, err = run(f, "delete", "shop")
	if cli.ExitCode(err) != 2 {
		t.Fatalf("wrong confirmation: %v", err)
	}
	f = factoryWith(fake)
	_, _, err = run(f, "delete", "shop", "--yes")
	if err != nil {
		t.Fatal(err)
	}
	if len(fake.AppDeleteConfirms) != 1 || fake.AppDeleteConfirms[0] != "shop" {
		t.Fatalf("confirm=%v", fake.AppDeleteConfirms)
	}
}

func TestAppsUnavailableOnEveryCommand(t *testing.T) {
	for _, args := range [][]string{{"list"}, {"get", "shop"}, {"create", "shop"}, {"update", "shop", "--deletion-protection=false"}, {"delete", "shop", "--yes"}, {"env", "list", "shop"}, {"env", "get", "shop", "production"}, {"env", "create", "shop", "production", "--cluster", "europe", "--image", digest, "--port", "8080", "--wait=false"}, {"env", "update", "shop", "production", "--env", "A=new"}, {"env", "delete", "shop", "production", "--yes"}} {
		t.Run(strings.Join(args, "/"), func(t *testing.T) {
			fake := clienttest.New()
			e := &client.APIError{StatusCode: 404, Code: "apps_unavailable", Message: "apps are not available on this platform"}
			fake.ListAppsErr = e
			fake.GetAppErr = e
			fake.CreateAppErr = e
			fake.DeleteAppErr = e
			fake.ListAppEnvironmentsErr = e
			fake.GetAppEnvironmentErr = e
			fake.CreateAppEnvironmentErr = e
			fake.DeleteAppEnvironmentErr = e
			_, _, err := run(factoryWith(fake), args...)
			if cli.ExitCode(err) != 7 || !strings.Contains(err.Error(), e.Message) {
				t.Fatalf("err=%v exit=%d", err, cli.ExitCode(err))
			}
		})
	}
}

func TestDeleteWaitsForPlainNotFound(t *testing.T) {
	fake := clienttest.New()
	fake.AppsByName["shop"] = &client.App{Name: "shop"}
	fake.GetAppSeq["shop"] = []*client.App{{Name: "shop", Status: &client.AppStatus{Phase: client.PhaseTerminating}}, nil}
	_, _, err := run(factoryWith(fake), "delete", "shop", "--yes")
	if err != nil {
		t.Fatal(err)
	}
	gets := 0
	for _, call := range fake.Calls {
		if call == "GetApp:shop" {
			gets++
		}
	}
	if gets < 2 {
		t.Fatalf("calls=%v", fake.Calls)
	}
}

func TestEnvironmentDeleteNeedsConfirmationAndDoesNotTreatDisabledAsGone(t *testing.T) {
	fake := clienttest.New()
	_, _, err := run(factoryWith(fake), "env", "delete", "shop", "production")
	if cli.ExitCode(err) != 2 || len(fake.AppEnvironmentDeleteConfirms) > 0 {
		t.Fatalf("err=%v", err)
	}
	fake.AppEnvironmentsByApp["shop"] = map[string]*client.AppEnvironment{"production": seededEnv()}
	fake.GetAppEnvironmentErr = &client.APIError{StatusCode: 404, Code: "apps_unavailable", Message: "apps are not available on this platform"}
	_, _, err = run(factoryWith(fake), "env", "delete", "shop", "production", "--yes")
	if cli.ExitCode(err) != 7 {
		t.Fatalf("disabled falsely counted gone: %v", err)
	}
}

func TestDeleteRefusalHints(t *testing.T) {
	for _, tc := range []struct{ code, hint string }{{"deletion_protected", "--deletion-protection=false"}, {"namespace_in_use", "bindings"}} {
		t.Run(tc.code, func(t *testing.T) {
			fake := clienttest.New()
			fake.DeleteAppEnvironmentErr = &client.APIError{StatusCode: 409, Code: tc.code, Message: "operator refusal"}
			_, _, err := run(factoryWith(fake), "env", "delete", "shop", "production", "--yes")
			var ce *cli.Error
			if cli.ExitCode(err) != 5 || !errors.As(err, &ce) || !strings.Contains(ce.Hint, tc.hint) {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestAppReadFormatsAndGoldens(t *testing.T) {
	fake := clienttest.New()
	fake.AppsByName["shop"] = &client.App{Name: "shop", DeletionProtection: true, Status: &client.AppStatus{Phase: "Degraded", Environments: []client.AppEnvironmentSummary{{Name: "production", Phase: "Degraded", Address: "https://shop.example.com"}}}}
	e := seededEnv()
	e.Status = &client.AppEnvironmentStatus{Phase: "Degraded", Address: "https://shop.example.com", Namespace: "app-shop-production", Deployment: &client.AppDeployment{Release: &client.AppReleaseReference{Name: "shop-production-2"}, Phase: "Failed", Reason: "HealthCheckFailed"}, RequestedRelease: &client.AppReleaseReference{Name: "shop-production-2"}, ActiveRelease: &client.AppReleaseReference{Name: "shop-production-1"}, LastSuccessfulRelease: &client.AppReleaseReference{Name: "shop-production-1"}, Recovery: &client.AppRecovery{Phase: "Recovered", Release: &client.AppReleaseReference{Name: "shop-production-1"}}}
	e.Domains = []string{"shop.example.com"}
	fake.AppEnvironmentsByApp["shop"] = map[string]*client.AppEnvironment{"production": e}
	for _, tc := range []struct {
		name string
		args []string
	}{{"app-get", []string{"get", "shop"}}, {"env-get", []string{"env", "get", "shop", "production"}}, {"app-list", []string{"list"}}, {"env-list", []string{"env", "list", "shop"}}} {
		t.Run(tc.name, func(t *testing.T) {
			for _, format := range []string{"json", "yaml", "table"} {
				f := factoryWith(fake)
				args := append(append([]string{}, tc.args...), "-o", format)
				out, _, err := run(f, args...)
				if err != nil {
					t.Fatal(err)
				}
				if format == "json" {
					var result any
					if err := json.Unmarshal([]byte(out), &result); err != nil {
						t.Fatal(err)
					}
				}
				if format == "table" {
					path := filepath.Join("testdata", tc.name+".golden")
					if *updateAppGoldens {
						if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
							t.Fatal(err)
						}
						if err := os.WriteFile(path, []byte(out), 0600); err != nil {
							t.Fatal(err)
						}
					}
					want, err := os.ReadFile(path)
					if err != nil {
						t.Fatal(err)
					}
					if out != string(want) {
						t.Fatalf("got:\n%s\nwant:\n%s", out, want)
					}
				}
			}
		})
	}
}

func TestEnvironmentCommandsKeepInheritedGlobalFlags(t *testing.T) {
	fake := clienttest.New()
	fake.AppEnvironmentsByApp["shop"] = map[string]*client.AppEnvironment{"production": seededEnv()}
	f := factoryWith(fake)
	root := &cobra.Command{Use: "kupe"}
	root.PersistentFlags().String("tenant", "", "Tenant")
	root.AddCommand(NewCmd(f))
	root.SetArgs([]string{"--tenant", "acme", "app", "env", "get", "shop", "production", "-o", "json"})
	root.SetOut(f.IOStreams.Out)
	root.SetErr(f.IOStreams.ErrOut)
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
}
