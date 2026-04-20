package secret

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

func TestListEmpty(t *testing.T) {
	f := factoryWith(t, clienttest.New())
	if err := exec(newListCmd(f)); err != nil {
		t.Fatalf("list: %v", err)
	}
	out := f.IOStreams.Out.(interface{ String() string }).String()
	if !strings.Contains(out, "NAME") {
		t.Fatalf("missing header:\n%s", out)
	}
}

func TestCreateAndList(t *testing.T) {
	fake := clienttest.New()
	f := factoryWith(t, fake)
	if err := exec(newCreateCmd(f), "db", "--path", "kv/db"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, ok := fake.Secrets["db"]; !ok {
		t.Fatal("secret not persisted")
	}

	// List should include it.
	f = factoryWith(t, fake)
	if err := exec(newListCmd(f)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.IOStreams.Out.(interface{ String() string }).String(), "db") {
		t.Fatal("list did not show db")
	}
}

func TestCreateRequiresPath(t *testing.T) {
	f := factoryWith(t, clienttest.New())
	err := exec(newCreateCmd(f), "db")
	if err == nil {
		t.Fatal("expected misuse error")
	}
	if code := cli.ExitCode(err); code != cli.ExitMisuse {
		t.Fatalf("exit = %d; want %d", code, cli.ExitMisuse)
	}
}

func TestCreateWithSyncTargets(t *testing.T) {
	fake := clienttest.New()
	f := factoryWith(t, fake)
	if err := exec(newCreateCmd(f), "db", "--path", "kv/db",
		"--sync", "prod:default",
		"--sync", "staging:default:renamed",
	); err != nil {
		t.Fatalf("create: %v", err)
	}
	s := fake.Secrets["db"]
	if len(s.Sync) != 2 {
		t.Fatalf("want 2 sync targets, got %d", len(s.Sync))
	}
	if s.Sync[1].SecretName != "renamed" {
		t.Fatalf("third field mis-parsed: %+v", s.Sync[1])
	}
}

func TestCreateRejectsBadSync(t *testing.T) {
	f := factoryWith(t, clienttest.New())
	err := exec(newCreateCmd(f), "db", "--path", "kv/db", "--sync", "only-one-part")
	if err == nil {
		t.Fatal("expected misuse error")
	}
	if code := cli.ExitCode(err); code != cli.ExitMisuse {
		t.Fatalf("exit = %d", code)
	}
}

func TestGetJSON(t *testing.T) {
	fake := clienttest.New()
	fake.Secrets["db"] = &client.Secret{
		Name: "db", SecretPath: "kv/db",
		Status: &client.SecretStatus{Phase: "Active"},
	}
	f := factoryWith(t, fake)
	if err := exec(newGetCmd(f), "db", "-o", "json"); err != nil {
		t.Fatalf("get -o json: %v", err)
	}
	var got client.Secret
	if err := json.Unmarshal([]byte(f.IOStreams.Out.(interface{ String() string }).String()), &got); err != nil {
		t.Fatal(err)
	}
	if got.Name != "db" {
		t.Fatalf("unexpected: %+v", got)
	}
}

func TestUpdateReplacesSyncList(t *testing.T) {
	fake := clienttest.New()
	fake.Secrets["db"] = &client.Secret{
		Name: "db", SecretPath: "kv/db",
		Sync: []client.SyncTarget{{Cluster: "old", Namespace: "default"}},
	}
	f := factoryWith(t, fake)
	if err := exec(newUpdateCmd(f), "db", "--sync", "new:default"); err != nil {
		t.Fatalf("update: %v", err)
	}
	s := fake.Secrets["db"]
	if len(s.Sync) != 1 || s.Sync[0].Cluster != "new" {
		t.Fatalf("sync not replaced: %+v", s.Sync)
	}
}

func TestUpdateRequiresSync(t *testing.T) {
	fake := clienttest.New()
	fake.Secrets["db"] = &client.Secret{Name: "db", SecretPath: "kv/db"}
	f := factoryWith(t, fake)
	err := exec(newUpdateCmd(f), "db")
	if err == nil {
		t.Fatal("expected misuse error")
	}
	if code := cli.ExitCode(err); code != cli.ExitMisuse {
		t.Fatalf("exit = %d", code)
	}
}

func TestDeleteNonInteractiveRequiresYes(t *testing.T) {
	fake := clienttest.New()
	fake.Secrets["db"] = &client.Secret{Name: "db", SecretPath: "kv/db"}
	f := factoryWith(t, fake)
	err := exec(newDeleteCmd(f), "db")
	if err == nil {
		t.Fatal("expected misuse error")
	}
	if code := cli.ExitCode(err); code != cli.ExitMisuse {
		t.Fatalf("exit = %d", code)
	}
}

func TestDeleteYesRemoves(t *testing.T) {
	fake := clienttest.New()
	fake.Secrets["db"] = &client.Secret{Name: "db", SecretPath: "kv/db"}
	f := factoryWith(t, fake)
	if err := exec(newDeleteCmd(f), "db", "--yes"); err != nil {
		t.Fatal(err)
	}
	if _, still := fake.Secrets["db"]; still {
		t.Fatal("secret still present")
	}
}

func TestGetNotFoundMapsToExit4(t *testing.T) {
	f := factoryWith(t, clienttest.New())
	err := exec(newGetCmd(f), "ghost")
	if err == nil {
		t.Fatal("expected error")
	}
	if code := cli.ExitCode(err); code != cli.ExitNotFound {
		t.Fatalf("exit = %d", code)
	}
}

func TestParseSyncTargets(t *testing.T) {
	tests := []struct {
		in   []string
		ok   bool
		want []client.SyncTarget
	}{
		{[]string{}, true, []client.SyncTarget{}},
		{[]string{"a:b"}, true, []client.SyncTarget{{Cluster: "a", Namespace: "b"}}},
		{[]string{"a:b:c"}, true, []client.SyncTarget{{Cluster: "a", Namespace: "b", SecretName: "c"}}},
		{[]string{"only"}, false, nil},
		{[]string{":b"}, false, nil},
		{[]string{"a:"}, false, nil},
		{[]string{"a:b:c:d"}, false, nil},
	}
	for _, tt := range tests {
		got, err := parseSyncTargets(tt.in)
		if tt.ok && err != nil {
			t.Errorf("parseSyncTargets(%v) err: %v", tt.in, err)
			continue
		}
		if !tt.ok && err == nil {
			t.Errorf("parseSyncTargets(%v) want error", tt.in)
			continue
		}
		if tt.ok && len(got) != len(tt.want) {
			t.Errorf("parseSyncTargets(%v) = %+v; want %+v", tt.in, got, tt.want)
		}
	}
}
