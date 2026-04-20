package member

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/client/clienttest"
	"github.com/kupecloud/kupe-cli/internal/config"
)

func factoryWith(t *testing.T, fake *clienttest.Fake) *cli.Factory {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := &config.Config{
		APIVersion:     config.APIVersion,
		Kind:           config.Kind,
		CurrentContext: "acme",
		Contexts: []config.Context{
			{Name: "acme", APIURL: "https://test", Tenant: "acme", TokenRef: "plaintext"},
		},
	}
	if err := cfg.Save(cfgPath); err != nil {
		t.Fatal(err)
	}
	io, _, _ := cli.Test()
	flags := &cli.GlobalFlags{ConfigPath: cfgPath}
	f := cli.NewFactory(io, flags)
	f.Client = func() (client.Interface, error) { return fake, nil }
	return f
}

func exec(cmd *cobra.Command, args ...string) error {
	cmd.SetContext(context.Background())
	cmd.SetArgs(args)
	return cmd.Execute()
}

func TestList(t *testing.T) {
	fake := clienttest.New()
	fake.Members["alice@x.com"] = &client.Member{Email: "alice@x.com", Role: "admin"}
	f := factoryWith(t, fake)
	if err := exec(newListCmd(f)); err != nil {
		t.Fatal(err)
	}
	out := f.IOStreams.Out.(interface{ String() string }).String()
	if !strings.Contains(out, "alice@x.com") || !strings.Contains(out, "admin") {
		t.Fatalf("list wrong:\n%s", out)
	}
}

func TestAddDefaultRole(t *testing.T) {
	fake := clienttest.New()
	f := factoryWith(t, fake)
	if err := exec(newAddCmd(f), "bob@x.com"); err != nil {
		t.Fatalf("add: %v", err)
	}
	if fake.Members["bob@x.com"].Role != "readonly" {
		t.Fatalf("default role not readonly: %+v", fake.Members["bob@x.com"])
	}
}

func TestAddRejectsInvalidRole(t *testing.T) {
	f := factoryWith(t, clienttest.New())
	err := exec(newAddCmd(f), "x@y.com", "--role", "superuser")
	if err == nil {
		t.Fatal("expected misuse error")
	}
	if code := cli.ExitCode(err); code != cli.ExitMisuse {
		t.Fatalf("exit = %d", code)
	}
}

func TestAddConflict(t *testing.T) {
	fake := clienttest.New()
	fake.Members["dup@x.com"] = &client.Member{Email: "dup@x.com", Role: "admin"}
	f := factoryWith(t, fake)
	err := exec(newAddCmd(f), "dup@x.com", "--role", "admin")
	if err == nil {
		t.Fatal("expected conflict")
	}
	if code := cli.ExitCode(err); code != cli.ExitConflict {
		t.Fatalf("exit = %d", code)
	}
}

func TestUpdateRole(t *testing.T) {
	fake := clienttest.New()
	fake.Members["x@x.com"] = &client.Member{Email: "x@x.com", Role: "readonly"}
	f := factoryWith(t, fake)
	if err := exec(newUpdateCmd(f), "x@x.com", "--role", "admin"); err != nil {
		t.Fatalf("update: %v", err)
	}
	if fake.Members["x@x.com"].Role != "admin" {
		t.Fatalf("role not updated: %+v", fake.Members["x@x.com"])
	}
}

func TestUpdateRequiresRoleFlag(t *testing.T) {
	f := factoryWith(t, clienttest.New())
	err := exec(newUpdateCmd(f), "x@x.com")
	if err == nil {
		t.Fatal("expected missing-flag error")
	}
	// Cobra's required-flag check returns exit 2 via misuse classification.
	// It's surfaced as a non-nil error — just confirm that.
}

func TestRemoveNonInteractiveRequiresYes(t *testing.T) {
	fake := clienttest.New()
	fake.Members["x@x.com"] = &client.Member{Email: "x@x.com", Role: "admin"}
	f := factoryWith(t, fake)
	err := exec(newRemoveCmd(f), "x@x.com")
	if err == nil {
		t.Fatal("expected misuse error")
	}
	if code := cli.ExitCode(err); code != cli.ExitMisuse {
		t.Fatalf("exit = %d", code)
	}
}

func TestRemoveYes(t *testing.T) {
	fake := clienttest.New()
	fake.Members["x@x.com"] = &client.Member{Email: "x@x.com", Role: "admin"}
	f := factoryWith(t, fake)
	if err := exec(newRemoveCmd(f), "x@x.com", "--yes"); err != nil {
		t.Fatal(err)
	}
	if _, still := fake.Members["x@x.com"]; still {
		t.Fatal("member not removed")
	}
}

func TestRemoveNotFound(t *testing.T) {
	f := factoryWith(t, clienttest.New())
	err := exec(newRemoveCmd(f), "ghost@x.com", "--yes")
	if err == nil {
		t.Fatal("expected 404")
	}
	if code := cli.ExitCode(err); code != cli.ExitNotFound {
		t.Fatalf("exit = %d", code)
	}
}

func TestListJSON(t *testing.T) {
	fake := clienttest.New()
	fake.Members["a@a.com"] = &client.Member{Email: "a@a.com", Role: "admin"}
	f := factoryWith(t, fake)
	if err := exec(newListCmd(f), "-o", "json"); err != nil {
		t.Fatal(err)
	}
	var got []client.Member
	if err := json.Unmarshal([]byte(f.IOStreams.Out.(interface{ String() string }).String()), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Role != "admin" {
		t.Fatalf("unexpected: %+v", got)
	}
}
