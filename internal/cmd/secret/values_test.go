package secret

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/client/clienttest"
)

const valueSentinel = "synthetic-private-sentinel"

func TestValuesRejectInvalidBeforeAnyClient(t *testing.T) {
	for _, test := range []struct {
		name, input string
		args        []string
	}{
		{"duplicate", `{"A":"x","A":"y"}`, nil}, {"number", `{"A":1}`, nil}, {"null", `{"A":null}`, nil}, {"nested", `{"A":{}}`, nil}, {"empty", `{}`, nil}, {"invalid-key", `{"a/b":"x"}`, nil}, {"oversize", strings.Repeat(" ", 513*1024), nil}, {"trailing", `{"A":"x"} {}`, nil},
		{"missing-version", `{"A":"x"}`, []string{"--stdin"}}, {"neither", ``, []string{"--expected-version", "0"}}, {"both", ``, []string{"--expected-version", "0", "--stdin", "--file", "unused"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			fake := clienttest.New()
			f := factoryWith(t, fake)
			calls := 0
			f.Client = func() (client.Interface, error) { calls++; return fake, nil }
			f.IOStreams.In = strings.NewReader(test.input)
			args := test.args
			if args == nil {
				args = []string{"--expected-version", "0", "--stdin"}
			}
			err := exec(newValuesSetCmd(f), append([]string{"orders"}, args...)...)
			if cli.ExitCode(err) != 2 || calls != 0 {
				t.Fatalf("err=%v client calls=%d", err, calls)
			}
		})
	}
}

func TestReservedVaultPaths(t *testing.T) {
	for _, path := range []string{"encryption/x", "/secret//data///metadata/encryption/x", "secret/metadata/./encryption/x", "x/../plain", "./plain", "x/./y", "secret/data/encryption", "metadata/encryption/x"} {
		t.Run(path, func(t *testing.T) {
			fake := clienttest.New()
			f := factoryWith(t, fake)
			calls := 0
			f.Client = func() (client.Interface, error) { calls++; return fake, nil }
			err := exec(newValuesGetCmd(f), "--path", path)
			if cli.ExitCode(err) != 3 || calls != 0 || !strings.Contains(err.Error(), "this path is reserved by the platform") {
				t.Fatalf("%v calls=%d", err, calls)
			}
		})
	}
	for _, path := range []string{"secret/data/team/encryption/x", "Encryption/x", "%2E/encryption/x", "secret/data/metadata", "team/%2E%2E/x"} {
		if reservedVaultPath(path) {
			t.Errorf("allowed path reserved: %s", path)
		}
	}
}

func TestValuesSetFileAndStdin(t *testing.T) {
	for _, file := range []bool{false, true} {
		t.Run(map[bool]string{true: "file", false: "stdin"}[file], func(t *testing.T) {
			fake := clienttest.New()
			fake.Secrets["orders"] = &client.Secret{Name: "orders", SecretPath: "team/db"}
			fake.VaultWriteResult = &client.VaultWriteResult{Path: "team/db", Version: 4, RefreshRequested: []string{"orders"}}
			f := factoryWith(t, fake)
			input := `{"FIELD_A":"` + valueSentinel + `"}`
			f.IOStreams.In = strings.NewReader(input)
			args := []string{"orders", "--expected-version", "3"}
			if file {
				path := filepath.Join(t.TempDir(), "values.json")
				if err := os.WriteFile(path, []byte(input), 0600); err != nil {
					t.Fatal(err)
				}
				args = append(args, "--file", path)
			} else {
				args = append(args, "--stdin")
			}
			if err := exec(newValuesSetCmd(f), args...); err != nil {
				t.Fatal(err)
			}
			out := f.IOStreams.Out.(interface{ String() string }).String()
			stderr := f.IOStreams.ErrOut.(interface{ String() string }).String()
			if out != "secret values at team/db saved as version 4\n" || !strings.Contains(stderr, "kupe secret wait orders --for delivered") {
				t.Fatalf("out=%q err=%q", out, stderr)
			}
			if strings.Contains(out+stderr, valueSentinel) || !reflect.DeepEqual(fake.VaultVersions, []int64{3}) {
				t.Fatal("value leaked or precondition lost")
			}
		})
	}
}

func TestValuesErrorMapping(t *testing.T) {
	for _, test := range []struct {
		code         string
		status, exit int
		text, hint   string
	}{
		{"secret_version_conflict", 412, 5, "changed since version 3", "review it"}, {"secret_write_unconfirmed", 503, 7, "the write may or may not have been saved", "compare"}, {"secret_path_reserved", 403, 3, "reserved", ""}, {"precondition_required", 428, 2, "precondition", ""}, {"invalid_secret_values", 400, 2, "values", ""}, {"too_large", 413, 2, "512 KiB", ""}, {"vault_unavailable", 503, 7, "Vault", ""}, {"vault_not_configured", 404, 7, "Vault is not available on this platform", ""},
	} {
		t.Run(test.code, func(t *testing.T) {
			fake := clienttest.New()
			fake.PutVaultValuesErr = &client.APIError{StatusCode: test.status, Code: test.code, Message: "refused"}
			f := factoryWith(t, fake)
			f.IOStreams.In = strings.NewReader(`{"A":"` + valueSentinel + `"}`)
			err := exec(newValuesSetCmd(f), "--path", "team/db", "--expected-version", "3", "--stdin")
			var ce *cli.Error
			if cli.ExitCode(err) != test.exit || !errors.As(err, &ce) || !strings.Contains(ce.Msg, test.text) || !strings.Contains(ce.Hint, test.hint) {
				t.Fatalf("%#v", err)
			}
			out := f.IOStreams.Out.(interface{ String() string }).String() + f.IOStreams.ErrOut.(interface{ String() string }).String() + err.Error()
			if strings.Contains(out, valueSentinel) {
				t.Fatal("leaked input")
			}
		})
	}
}

func TestPurgeGuardsAndConfirmation(t *testing.T) {
	for _, test := range []struct {
		name, input string
		yes         bool
		refs        []string
		apierr      error
		exit        int
		wantDelete  bool
	}{
		{"referenced", "", true, []string{"orders"}, nil, 5, false}, {"wrong", "wrong\n", false, nil, nil, 2, false}, {"typed", "team/db\n", false, nil, nil, 0, true}, {"yes", "", true, nil, nil, 0, true},
		{"conflict", "", true, nil, &client.APIError{StatusCode: 412}, 5, true}, {"server-refs", "", true, nil, &client.APIError{StatusCode: 409, Code: "secret_references_exist", Message: "used by orders"}, 5, true}, {"unavailable", "", true, nil, &client.APIError{StatusCode: 503, Code: "vault_unavailable"}, 7, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			fake := clienttest.New()
			fake.VaultEntries = map[string]*client.VaultEntry{"team/db": {Path: "team/db", Version: 6, References: test.refs}}
			fake.PurgeVaultEntryErr = test.apierr
			f := factoryWith(t, fake)
			f.IOStreams.PromptsEnabled = true
			f.IOStreams.In = strings.NewReader(test.input)
			args := []string{"--path", "team/db"}
			if test.yes {
				args = append(args, "--yes")
			}
			err := exec(newPurgeCmd(f), args...)
			if cli.ExitCode(err) != test.exit {
				t.Fatalf("%v", err)
			}
			if (len(fake.VaultVersions) == 1) != test.wantDelete {
				t.Fatalf("calls=%v", fake.Calls)
			}
			if test.wantDelete && fake.VaultVersions[0] != 6 {
				t.Fatal("wrong version")
			}
			if test.name == "conflict" && !strings.Contains(err.Error(), "changed after it was read; run purge again") {
				t.Fatal(err)
			}
			if test.name == "server-refs" && !strings.Contains(err.Error(), "orders") {
				t.Fatal(err)
			}
		})
	}
}

func TestValuesMetadataOutputAndTargets(t *testing.T) {
	for _, format := range []string{"table", "json", "yaml"} {
		t.Run(format, func(t *testing.T) {
			fake := clienttest.New()
			fake.VaultEntries = map[string]*client.VaultEntry{"team/db": {Path: "team/db", Version: 3, UpdatedAt: "2026-10-29T12:00:00Z", References: []string{"orders"}}}
			fake.Secrets["orders"] = &client.Secret{SecretPath: "team/db"}
			f := factoryWith(t, fake)
			if err := exec(newValuesGetCmd(f), "orders", "-o", format); err != nil {
				t.Fatal(err)
			}
			out := f.IOStreams.Out.(interface{ String() string }).String()
			if !strings.Contains(out, "team/db") || !strings.Contains(out, "orders") || !strings.Contains(out, "3") || strings.Contains(out, valueSentinel) {
				t.Fatal(out)
			}
		})
	}
	for _, args := range [][]string{nil, {"orders", "--path", "team/db"}, {"a", "b"}, {"--path", "encryption/x"}} {
		fake := clienttest.New()
		f := factoryWith(t, fake)
		if err := exec(newValuesGetCmd(f), args...); err == nil || len(fake.Calls) != 0 {
			t.Fatalf("args=%v calls=%v err=%v", args, fake.Calls, err)
		}
	}
	fake := clienttest.New()
	fake.Secrets["orders"] = &client.Secret{SecretPath: "metadata/encryption/x"}
	f := factoryWith(t, fake)
	if err := exec(newValuesGetCmd(f), "orders"); cli.ExitCode(err) != 3 || len(fake.Calls) != 1 {
		t.Fatalf("resolved path refusal err=%v calls=%v", err, fake.Calls)
	}
}

func TestPurgeExpectedVersionAndNoPrompt(t *testing.T) {
	fake := clienttest.New()
	fake.VaultEntries = map[string]*client.VaultEntry{"team/db": {Path: "team/db", Version: 6}}
	f := factoryWith(t, fake)
	if err := exec(newPurgeCmd(f), "--path", "team/db", "--yes", "--expected-version", "4"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(fake.VaultVersions, []int64{4}) {
		t.Fatal(fake.VaultVersions)
	}
	fake = clienttest.New()
	fake.VaultEntries = map[string]*client.VaultEntry{"team/db": {Version: 6}}
	f = factoryWith(t, fake)
	if err := exec(newPurgeCmd(f), "--path", "team/db"); cli.ExitCode(err) != 2 || len(fake.VaultVersions) > 0 {
		t.Fatalf("%v", err)
	}
}

func TestValuesGoldens(t *testing.T) {
	for _, name := range []string{"values-get", "values-set", "values-conflict", "values-unconfirmed", "purge"} {
		t.Run(name, func(t *testing.T) {
			fake := clienttest.New()
			fake.VaultEntries = map[string]*client.VaultEntry{"team/db": {Path: "team/db", Version: 3, CreatedAt: "2026-10-29T12:00:00Z", UpdatedAt: "2026-10-29T12:00:00Z", References: []string{}}}
			fake.VaultWriteResult = &client.VaultWriteResult{Path: "team/db", Version: 4, RefreshRequested: []string{"orders"}}
			f := factoryWith(t, fake)
			f.IOStreams.In = strings.NewReader(`{"FIELD_A":"` + valueSentinel + `"}`)
			var err error
			switch name {
			case "values-get":
				err = exec(newValuesGetCmd(f), "--path", "team/db", "-o", "json")
			case "purge":
				err = exec(newPurgeCmd(f), "--path", "team/db", "--yes")
			default:
				if name == "values-conflict" {
					fake.PutVaultValuesErr = &client.APIError{StatusCode: 412, Code: "secret_version_conflict"}
				}
				if name == "values-unconfirmed" {
					fake.PutVaultValuesErr = &client.APIError{StatusCode: 503, Code: "secret_write_unconfirmed"}
				}
				err = exec(newValuesSetCmd(f), "--path", "team/db", "--expected-version", "3", "--stdin", "-o", "json")
			}
			stdout := f.IOStreams.Out.(interface{ String() string }).String()
			stderr := f.IOStreams.ErrOut.(interface{ String() string }).String()
			if strings.Contains(stdout+stderr, valueSentinel) {
				t.Fatal("value in output")
			}
			result := "stdout:\n" + stdout + "stderr:\n" + stderr
			if err != nil {
				result += "error: " + err.Error() + "\n"
				var ce *cli.Error
				if errors.As(err, &ce) && ce.Hint != "" {
					result += "hint: " + ce.Hint + "\n"
				}
			}
			result += fmt.Sprintf("exit: %d\n", cli.ExitCode(err))
			path := filepath.Join("testdata", name+".golden")
			expected, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if string(expected) != result {
				t.Fatalf("golden mismatch\nwant:\n%s\ngot:\n%s", expected, result)
			}
		})
	}
}
