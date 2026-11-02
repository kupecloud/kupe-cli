package secret

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/kupecloud/kupe-cli/internal/auth"
	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/client/clienttest"
)

type vaultSetupTransport func(*http.Request) (*http.Response, error)

func (f vaultSetupTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestVaultWriteTokenFailureBeforeSubmission(t *testing.T) {
	typed := cli.AuthError("session expired; run kupe auth login").WithHint("sign in before retrying")
	for _, test := range []struct {
		name, message, hint string
		cause               error
		exit                int
	}{
		{"typed-auth", typed.Error(), typed.Hint, typed, cli.ExitAuth},
		{"missing-token", "not logged in (or your session expired); run \"kupe auth login\"", "", auth.ErrNotFound, cli.ExitAuth},
		{"keyring", "could not read credentials from the OS keyring", "set KUPE_STORAGE=plaintext", auth.ErrKeyringUnavailable, cli.ExitAuth},
		{"refresh-timeout", "resolving credentials", "", cli.ErrAuthRefreshTimeout, cli.ExitAuth},
		{"cancelled", "context canceled", "", context.Canceled, cli.ExitInterrupted},
		{"deadline", "context deadline exceeded", "", context.DeadlineExceeded, cli.ExitTimeout},
	} {
		for _, operation := range []string{"set", "purge"} {
			t.Run(test.name+"/"+operation, func(t *testing.T) {
				tokens, reads, writes := 0, 0, 0
				api := client.New("https://synthetic.invalid", "acme", "", "test",
					client.WithTokenSource(func(context.Context) (string, error) {
						tokens++
						if tokens == 1 {
							return "synthetic", nil
						}
						return "", test.cause
					}),
					client.WithHTTPClient(&http.Client{Transport: vaultSetupTransport(func(r *http.Request) (*http.Response, error) {
						if r.Method != http.MethodGet {
							writes++
						} else {
							reads++
						}
						body := `{"path":"team/db","version":3,"references":[],"secretPath":"team/db"}`
						return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
					})}),
				)
				f := factoryWith(t, clienttest.New())
				f.Client = func() (client.Interface, error) { return api, nil }
				f.IOStreams.In = strings.NewReader(`{"A":"` + valueSentinel + `"}`)
				cmd := newValuesSetCmd(f)
				args := []string{"orders", "--expected-version", "3", "--stdin"}
				if operation == "purge" {
					cmd = newPurgeCmd(f)
					args = []string{"--path", "team/db", "--yes"}
				}
				err := exec(cmd, args...)
				if writes != 0 || reads != 1 || tokens != 2 || !errors.Is(err, test.cause) {
					t.Fatalf("reads=%d writes=%d tokens=%d err=%v", reads, writes, tokens, err)
				}
				if cli.ExitCode(err) != test.exit || !strings.Contains(err.Error(), test.message) {
					t.Fatalf("exit=%d err=%v", cli.ExitCode(err), err)
				}
				if test.hint != "" {
					var ce *cli.Error
					if !errors.As(err, &ce) || !strings.Contains(ce.Hint, test.hint) {
						t.Fatalf("missing credential hint: %v", err)
					}
				}
				if strings.Contains(err.Error(), valueSentinel) || strings.Contains(err.Error(), "may or may not") || strings.Contains(err.Error(), "repeat the command") {
					t.Fatalf("unsafe setup error: %v", err)
				}
			})
		}
	}
}

type failedVaultResponse struct{ cause error }

func (r failedVaultResponse) Read([]byte) (int, error) { return 0, r.cause }

func (failedVaultResponse) Close() error { return nil }

func TestVaultWriteResponseReadRemainsUnknown(t *testing.T) {
	for _, operation := range []string{"set", "purge"} {
		t.Run(operation, func(t *testing.T) {
			cause := errors.New(valueSentinel)
			writes := 0
			api := client.New("https://synthetic.invalid", "acme", "", "test", client.WithHTTPClient(&http.Client{Transport: vaultSetupTransport(func(r *http.Request) (*http.Response, error) {
				body := io.NopCloser(strings.NewReader(`{"path":"team/db","version":3,"references":[]}`))
				if r.Method != http.MethodGet {
					writes++
					body = failedVaultResponse{cause: cause}
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: body}, nil
			})}))
			f := factoryWith(t, clienttest.New())
			f.Client = func() (client.Interface, error) { return api, nil }
			f.IOStreams.In = strings.NewReader(`{"A":"synthetic"}`)
			cmd := newValuesSetCmd(f)
			args := []string{"--path", "team/db", "--expected-version", "3", "--stdin"}
			want := "the write may or may not have been saved"
			if operation == "purge" {
				cmd = newPurgeCmd(f)
				args = []string{"--path", "team/db", "--yes"}
				want = "repeat the command to confirm"
			}
			err := exec(cmd, args...)
			if cli.ExitCode(err) != cli.ExitUnavailable || err.Error() != want || writes != 1 || !errors.Is(err, cause) {
				t.Fatalf("writes=%d exit=%d err=%v", writes, cli.ExitCode(err), err)
			}
		})
	}
}
