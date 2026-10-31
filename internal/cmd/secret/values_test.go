package secret

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

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
			if cli.ExitCode(err) != test.exit || !errors.As(err, &ce) || !strings.Contains(err.Error(), test.text) || !strings.Contains(ce.Hint, test.hint) {
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
			if test.name == "conflict" && !strings.Contains(err.Error(), "is no longer at the version being purged; run purge again") {
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
	for _, name := range []string{"values-get", "values-set", "values-conflict", "values-unconfirmed", "purge", "values-create-conflict", "purge-conflict", "purge-absent", "purge-unknown"} {
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
			case "purge", "purge-conflict", "purge-absent", "purge-unknown":
				switch name {
				case "purge-conflict":
					fake.PurgeVaultEntryErr = &client.APIError{StatusCode: 412, Code: "secret_version_conflict"}
				case "purge-absent":
					delete(fake.VaultEntries, "team/db")
				case "purge-unknown":
					fake.PurgeVaultEntryErr = fmt.Errorf("executing request: %s", valueSentinel)
				}
				err = exec(newPurgeCmd(f), "--path", "team/db", "--yes")
			default:
				if name == "values-conflict" {
					fake.PutVaultValuesErr = &client.APIError{StatusCode: 412, Code: "secret_version_conflict"}
				}
				if name == "values-unconfirmed" {
					fake.PutVaultValuesErr = &client.APIError{StatusCode: 503, Code: "secret_write_unconfirmed"}
				}
				version := "3"
				if name == "values-create-conflict" {
					version = "0"
					fake.PutVaultValuesErr = &client.APIError{StatusCode: 412, Code: "secret_version_conflict"}
				}
				err = exec(newValuesSetCmd(f), "--path", "team/db", "--expected-version", version, "--stdin", "-o", "json")
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

func TestValuesCreateConflictReviewCopy(t *testing.T) {
	fake := clienttest.New()
	fake.PutVaultValuesErr = &client.APIError{StatusCode: 412, Code: "secret_version_conflict"}
	f := factoryWith(t, fake)
	f.IOStreams.In = strings.NewReader(`{"A":"` + valueSentinel + `"}`)
	err := exec(newValuesSetCmd(f), "--path", "team/db", "--expected-version", "0", "--stdin")
	var ce *cli.Error
	if cli.ExitCode(err) != 5 || err.Error() != "values already exist at team/db" || !errors.As(err, &ce) || ce.Hint != "`kupe secret values get` shows the current version; review it, then pass it as --expected-version" {
		t.Fatalf("err=%v %#v", err, ce)
	}
}

func TestVaultSentWriteUnknownOutcomes(t *testing.T) {
	for _, operation := range []string{"set", "purge"} {
		for _, failure := range []string{"reset", "attempt-timeout", "context-timeout", "cancel", "502", "504", "503", "unknown-code", "unconfirmed", "vault-unavailable"} {
			t.Run(operation+"/"+failure, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				if failure == "context-timeout" {
					var deadlineCancel context.CancelFunc
					ctx, deadlineCancel = context.WithTimeout(ctx, time.Second)
					defer deadlineCancel()
				}
				var writes atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method == http.MethodGet {
						fmt.Fprint(w, `{"path":"team/db","version":3,"references":[]}`)
						return
					}
					writes.Add(1)
					_, _ = io.Copy(io.Discard, r.Body)
					switch failure {
					case "reset":
						conn, _, err := w.(http.Hijacker).Hijack()
						if err != nil {
							t.Error(err)
							return
						}
						_ = conn.Close()
					case "attempt-timeout", "context-timeout":
						<-r.Context().Done()
					case "cancel":
						cancel()
						<-r.Context().Done()
					default:
						status := 503
						if failure == "502" {
							status = 502
						}
						if failure == "504" {
							status = 504
						}
						w.WriteHeader(status)
						switch failure {
						case "vault-unavailable":
							fmt.Fprint(w, `{"code":"vault_unavailable","message":"Vault unavailable"}`)
						case "unconfirmed", "unknown-code":
							code := "secret_write_unconfirmed"
							if failure == "unknown-code" {
								code = "unrecognised_failure"
							}
							fmt.Fprintf(w, `{"code":%q,"message":%q}`, code, valueSentinel)
						default:
							fmt.Fprintf(w, "<html>%s</html>", valueSentinel)
						}
					}
				}))
				defer server.Close()
				httpClient := server.Client()
				httpClient.Timeout = 4 * time.Second
				if failure == "attempt-timeout" {
					httpClient.Timeout = time.Second
				}
				api := client.New(server.URL, "acme", "", "cli-test", client.WithHTTPClient(httpClient))
				f := factoryWith(t, clienttest.New())
				f.Client = func() (client.Interface, error) { return api, nil }
				f.IOStreams.In = strings.NewReader(`{"A":"` + valueSentinel + `"}`)
				cmd := newValuesSetCmd(f)
				args := []string{"--path", "team/db", "--expected-version", "3", "--stdin"}
				want := "the write may or may not have been saved"
				if failure == "vault-unavailable" {
					want = "Vault is unavailable: service unavailable: Vault unavailable"
				}
				if operation == "purge" {
					cmd = newPurgeCmd(f)
					args = []string{"--path", "team/db", "--yes"}
					want = "repeat the command to confirm"
				}
				cmd.SetArgs(args)
				cmd.SetErr(f.IOStreams.ErrOut)
				cmd.SetOut(f.IOStreams.Out)
				err := cmd.ExecuteContext(ctx)
				if cli.ExitCode(err) != 7 || err.Error() != want || writes.Load() != 1 {
					t.Fatalf("writes=%d err=%v exit=%d", writes.Load(), err, cli.ExitCode(err))
				}
				var ce *cli.Error
				if !errors.As(err, &ce) {
					t.Fatal("missing CLI error")
				}
				if operation == "set" && failure != "vault-unavailable" && !strings.Contains(ce.Hint, "compare the version") {
					t.Fatal(ce.Hint)
				}
				output := f.IOStreams.Out.(interface{ String() string }).String() + f.IOStreams.ErrOut.(interface{ String() string }).String() + err.Error() + ce.Hint
				for _, forbidden := range []string{valueSentinel, "<html>", "executing request", "context canceled", "context deadline exceeded"} {
					if strings.Contains(output, forbidden) {
						t.Fatalf("unsafe output contains %q", forbidden)
					}
				}
				if failure == "cancel" && !errors.Is(err, context.Canceled) {
					t.Fatal("cause lost")
				}
				if failure == "context-timeout" && !errors.Is(err, context.DeadlineExceeded) {
					t.Fatal("cause lost")
				}
			})
		}
	}
}

func TestPurgeConflictReadAndPinnedCopy(t *testing.T) {
	for _, pin := range []bool{false, true} {
		t.Run(fmt.Sprint(pin), func(t *testing.T) {
			fake := clienttest.New()
			fake.VaultEntries = map[string]*client.VaultEntry{"team/db": {Version: 6}}
			fake.PurgeVaultEntryErr = &client.APIError{StatusCode: 412, Code: "secret_version_conflict"}
			f := factoryWith(t, fake)
			args := []string{"--path", "team/db", "--yes"}
			if pin {
				args = append(args, "--expected-version", "4")
			}
			err := exec(newPurgeCmd(f), args...)
			if cli.ExitCode(err) != 5 || err.Error() != "team/db is no longer at the version being purged; run purge again" {
				t.Fatal(err)
			}
		})
	}
}

func TestPurgeRepeatConfirmsAbsent(t *testing.T) {
	var writes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			writes.Add(1)
			w.WriteHeader(503)
			fmt.Fprint(w, "<html>unknown</html>")
			return
		}
		if writes.Load() > 0 {
			w.WriteHeader(404)
			fmt.Fprint(w, `{"code":"not_found","message":"not found"}`)
			return
		}
		fmt.Fprint(w, `{"path":"team/db","version":3,"references":[]}`)
	}))
	defer server.Close()
	api := client.New(server.URL, "acme", "", "cli-test")
	for i, want := range []string{"repeat the command to confirm", "no values at team/db; nothing to purge"} {
		f := factoryWith(t, clienttest.New())
		f.Client = func() (client.Interface, error) { return api, nil }
		err := exec(newPurgeCmd(f), "--path", "team/db", "--yes")
		exit := 7
		if i == 1 {
			exit = 4
		}
		if cli.ExitCode(err) != exit || err.Error() != want || writes.Load() != 1 {
			t.Fatalf("err=%v writes=%d", err, writes.Load())
		}
	}
}

func TestVaultFailuresBeforeWriteStayKnown(t *testing.T) {
	fake := clienttest.New()
	fake.GetVaultEntryErr = &client.APIError{StatusCode: 404, Code: "vault_not_configured"}
	f := factoryWith(t, fake)
	if err := exec(newPurgeCmd(f), "--path", "team/db", "--yes"); cli.ExitCode(err) != 7 || !strings.Contains(err.Error(), "Vault is not available on this platform") || len(fake.VaultVersions) != 0 {
		t.Fatalf("%v", err)
	}
	fake = clienttest.New()
	fake.GetSecretErr = context.Canceled
	f = factoryWith(t, fake)
	f.IOStreams.In = strings.NewReader(`{"A":"synthetic"}`)
	if err := exec(newValuesSetCmd(f), "orders", "--expected-version", "3", "--stdin"); cli.ExitCode(err) != 130 || len(fake.VaultVersions) != 0 {
		t.Fatal(err)
	}
	fake = clienttest.New()
	fake.GetVaultEntryErr = context.DeadlineExceeded
	f = factoryWith(t, fake)
	if err := exec(newPurgeCmd(f), "--path", "team/db", "--yes"); cli.ExitCode(err) != 8 || len(fake.VaultVersions) != 0 {
		t.Fatal(err)
	}
}

func TestVaultWriteAuthenticationSetupStaysKnown(t *testing.T) {
	for _, operation := range []string{"set", "purge"} {
		t.Run(operation, func(t *testing.T) {
			fake := clienttest.New()
			f := factoryWith(t, fake)
			authErr := cli.AuthError("sign in before writing values")
			f.Client = func() (client.Interface, error) { return nil, authErr }
			f.IOStreams.In = strings.NewReader(`{"A":"synthetic"}`)
			var err error
			if operation == "set" {
				err = exec(newValuesSetCmd(f), "--path", "team/db", "--expected-version", "3", "--stdin")
			} else {
				err = exec(newPurgeCmd(f), "--path", "team/db", "--yes")
			}
			if cli.ExitCode(err) != 3 || !errors.Is(err, authErr) || len(fake.Calls) != 0 {
				t.Fatalf("err=%v calls=%v", err, fake.Calls)
			}
		})
	}
}

func TestVaultBoundedWriteErrorRetainsRequestIDAndCause(t *testing.T) {
	cause := &client.APIError{StatusCode: 502, Message: "<html>" + valueSentinel + "</html>", RequestID: "vault-request"}
	err := translateVaultWriteErr(cause, "team/db", 3, false)
	var api *client.APIError
	if err.Error() != "the write may or may not have been saved (request-id: vault-request)" || !errors.As(err, &api) || !errors.Is(err, cause) {
		t.Fatalf("err=%v cause=%#v", err, api)
	}
}
