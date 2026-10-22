package cmdutil

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/client/clienttest"
	"github.com/kupecloud/kupe-cli/internal/config"
	"github.com/kupecloud/kupe-cli/internal/printer"
)

// singleton is a service whose one resource is always named "registry".
var singleton = Service[client.Registry, client.CreateRegistryRequest, client.PatchRegistryRequest]{
	Noun: "registry",
	API:  client.Interface.Registries,
	Phase: func(r *client.Registry) string {
		if r.Status == nil {
			return ""
		}
		return r.Status.Phase
	},
	Printer: printer.Resource[client.Registry]{
		Columns: func(bool) printer.Columns {
			return printer.Columns{{Name: "NAME", Get: func(any) string { return "registry" }}}
		},
		DetailColumns: func(bool) printer.Columns {
			return printer.Columns{{Name: "Name", Get: func(any) string { return "registry" }}}
		},
		Name: func(r *client.Registry) string { return r.Name },
	},
	Singleton: client.RegistryName,
}

func singletonFactory(t *testing.T, fake *clienttest.Fake) *cli.Factory {
	t.Helper()
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	cfg := &config.Config{APIVersion: config.APIVersion, Kind: config.Kind, CurrentContext: "acme",
		Contexts: []config.Context{{Name: "acme", APIURL: "https://test", Tenant: "acme", TokenRef: "plaintext"}}}
	if err := cfg.Save(cfgPath); err != nil {
		t.Fatal(err)
	}
	io, _, _ := cli.Test()
	f := cli.NewFactory(io, &cli.GlobalFlags{ConfigPath: cfgPath})
	f.Client = func() (client.Interface, error) { return fake, nil }
	return f
}

func execute(cmd *cobra.Command, args ...string) error {
	cmd.SetContext(context.Background())
	cmd.SetArgs(args)
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	return cmd.Execute()
}

func TestSingleton_BuiltCommandsTakeNoName(t *testing.T) {
	f := singletonFactory(t, clienttest.New())
	for _, cmd := range []*cobra.Command{
		singleton.GetCmd(f, Help{}),
		singleton.WaitCmd(f, Help{}),
		singleton.DeleteCmd(f, Delete{}),
	} {
		if strings.Contains(cmd.Use, "NAME") {
			t.Errorf("%s: Use = %q; a singleton takes no NAME", cmd.Name(), cmd.Use)
		}
		if err := cmd.Args(cmd, []string{"registry"}); err == nil {
			t.Errorf("%s accepted a name argument", cmd.Name())
		}
	}
}

func TestSingleton_DeleteActsOnTheOneName(t *testing.T) {
	fake := clienttest.New()
	fake.RegistriesByName["registry"] = &client.Registry{Name: "registry"}
	f := singletonFactory(t, fake)
	if err := execute(singleton.DeleteCmd(f, Delete{WaitTimeout: time.Second}), "--yes"); err != nil {
		t.Fatal(err)
	}
	if fake.Calls[0] != "DeleteRegistry:registry" {
		t.Fatalf("calls = %v", fake.Calls)
	}
	if _, ok := fake.RegistriesByName["registry"]; ok {
		t.Fatal("registry not deleted")
	}
}

func TestSingleton_WaitHintsLeaveTheNameOut(t *testing.T) {
	err := singleton.MapWaitErr(context.Canceled, "registry", "create")
	var ce *cli.Error
	if !errors.As(err, &ce) {
		t.Fatalf("err = %v", err)
	}
	if ce.Msg != "stopped waiting; the registry is still being created on Kupe Cloud" {
		t.Fatalf("msg = %q", ce.Msg)
	}
	for _, want := range []string{"check status:  kupe registry get\n", "resume wait:   kupe registry wait\n", "abandon:       kupe registry delete"} {
		if !strings.Contains(ce.Hint+"\n", want) {
			t.Fatalf("hint %q missing %q", ce.Hint, want)
		}
	}
	// A named service keeps its names.
	named := MapWaitErr(context.Canceled, "database", "orders-db", "create")
	if !strings.Contains(named.Error(), `database "orders-db" is still being created`) {
		t.Fatalf("named = %v", named)
	}
}
