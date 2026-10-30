package secret

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
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
	if err := exec(newCreateCmd(f), "db", "--path", "kv/db", "--wait=false"); err != nil {
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
		"--sync", "staging:default:renamed", "--wait=false",
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
	if err := exec(newUpdateCmd(f), "db", "--sync", "new:default", "--wait=false"); err != nil {
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

func TestSyncKeyValidation(t *testing.T) {
	tooMany := make([]string, 65)
	for i := range tooMany {
		tooMany[i] = fmt.Sprintf("c:ns:KEY_%d", i)
	}
	tests := []struct {
		name string
		keys []string
		want bool
	}{
		{"unknown target", []string{"other:ns:key"}, false},
		{"empty", []string{"c:ns:"}, false},
		{"repeated", []string{"c:ns:key", "c:ns:key"}, false},
		{"65 keys", tooMany, false},
		{"dot", []string{"c:ns:."}, false},
		{"two dots", []string{"c:ns:.."}, false},
		{"leading two dots", []string{"c:ns:..a"}, false},
		{"space", []string{"c:ns:a b"}, false},
		{"254 bytes", []string{"c:ns:" + strings.Repeat("a", 254)}, false},
		{"valid", []string{"c:ns:API_KEY", "c:ns:tls.crt", "c:ns:a-b", "c:ns:a..b"}, true},
	}
	for _, verb := range []string{"create", "update"} {
		for _, tt := range tests {
			t.Run(verb+"/"+tt.name, func(t *testing.T) {
				fake := clienttest.New()
				fake.Secrets["db"] = &client.Secret{Name: "db"}
				f := factoryWith(t, fake)
				cmd := newUpdateCmd(f)
				args := []string{"db", "--sync", "c:ns", "--wait=false"}
				if verb == "create" {
					delete(fake.Secrets, "db")
					cmd = newCreateCmd(f)
					args = append(args, "--path", "kv/db")
				}
				for _, key := range tt.keys {
					args = append(args, "--sync-key", key)
				}
				err := exec(cmd, args...)
				if tt.want {
					if err != nil {
						t.Fatal(err)
					}
					if len(fake.Secrets["db"].Sync[0].Keys) != len(tt.keys) {
						t.Fatal("keys missing")
					}
				} else if cli.ExitCode(err) != cli.ExitMisuse || len(fake.Calls) != 0 {
					t.Fatalf("err=%v calls=%v", err, fake.Calls)
				}
			})
		}
	}
}

func TestSyncKeyTotalLimit(t *testing.T) {
	var targets []client.SyncTarget
	var keys []string
	for i := range 17 {
		name := fmt.Sprintf("c%d", i)
		targets = append(targets, client.SyncTarget{Cluster: name, Namespace: "ns"})
		for k := range 64 {
			keys = append(keys, fmt.Sprintf("%s:ns:K%d", name, k))
		}
	}
	if _, err := selectKeys(targets, keys, nil); err == nil {
		t.Fatal("1,088 keys accepted")
	}
	targets = targets[:16]
	// Fresh selections prove the accepted boundary independently of the failed parse.
	for i := range targets {
		targets[i].Keys = nil
	}
	if _, err := selectKeys(targets, keys[:1024], nil); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateKeepsOrReplacesTargetKeys(t *testing.T) {
	for _, tt := range []struct {
		name   string
		extra  []string
		want   []string
		misuse bool
	}{
		{name: "keep", want: []string{"EXISTING"}},
		{name: "replace", extra: []string{"--sync-key", "c:ns:NEW"}, want: []string{"NEW"}},
		{name: "all", extra: []string{"--all-keys", "c:ns"}},
		{name: "conflicting flags", extra: []string{"--sync-key", "c:ns:NEW", "--all-keys", "c:ns"}, misuse: true},
		{name: "unknown all target", extra: []string{"--all-keys", "other:ns"}, misuse: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fake := clienttest.New()
			fake.Secrets["db"] = &client.Secret{Name: "db", Sync: []client.SyncTarget{{Cluster: "c", Namespace: "ns", Keys: []string{"EXISTING"}}}}
			f := factoryWith(t, fake)
			args := append([]string{"db", "--sync", "c:ns", "--wait=false"}, tt.extra...)
			err := exec(newUpdateCmd(f), args...)
			if tt.misuse {
				if cli.ExitCode(err) != cli.ExitMisuse || len(fake.Calls) != 0 {
					t.Fatalf("err=%v calls=%v", err, fake.Calls)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			got := fake.Secrets["db"].Sync[0].Keys
			if !slices.Equal(got, tt.want) {
				t.Fatalf("keys=%v want%v", got, tt.want)
			}
		})
	}
}

func TestUpdateCombinedPreservedKeysLimit(t *testing.T) {
	fake := clienttest.New()
	current := &client.Secret{Name: "db"}
	args := []string{"db", "--wait=false"}
	for i := range 17 {
		cluster := fmt.Sprintf("c%d", i)
		target := client.SyncTarget{Cluster: cluster, Namespace: "ns"}
		args = append(args, "--sync", cluster+":ns")
		for k := range 64 {
			target.Keys = append(target.Keys, fmt.Sprintf("KEY_%d", k))
		}
		current.Sync = append(current.Sync, target)
	}
	// The existing valid sixteen-target selection plus 64 new keys exceeds the total.
	current.Sync = current.Sync[:16]
	for k := range 64 {
		args = append(args, "--sync-key", fmt.Sprintf("c16:ns:KEY_%d", k))
	}
	fake.Secrets["db"] = current
	f := factoryWith(t, fake)
	if err := exec(newUpdateCmd(f), args...); cli.ExitCode(err) != cli.ExitMisuse {
		t.Fatalf("err=%v", err)
	}
	if strings.Contains(strings.Join(fake.Calls, " "), "UpdateSecret:db") {
		t.Fatal("over-limit selection was patched")
	}
}

func TestInvalidSyncKeyUsesApprovedMessage(t *testing.T) {
	f := factoryWith(t, clienttest.New())
	err := exec(newCreateCmd(f), "db", "--path", "apps/shop/api", "--sync", "c:ns", "--sync-key", "c:ns:..key", "--wait=false")
	const want = "invalid --sync-key for target \"c:ns\": keys use 1–253 letters, digits, '-', '_' or '.', and cannot be '.', '..' or start with '..'"
	if err == nil || err.Error() != want {
		t.Fatalf("err=%v want%q", err, want)
	}
}
