package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/client/clienttest"
	"github.com/kupecloud/kupe-cli/internal/cmd/cmdutil"
)

func TestAppWaitFailuresOnlySuggestAvailableCommands(t *testing.T) {
	for _, operation := range []struct {
		name, status string
		args         []string
		environment  bool
	}{
		{"app delete", "kupe app get shop", []string{"delete", "shop", "--yes"}, false},
		{"environment create", "kupe app env get shop production", []string{"env", "create", "shop", "production", "--cluster", "europe", "--image", digest, "--port", "8080"}, true},
		{"environment update", "kupe app env get shop production", []string{"env", "update", "shop", "production", "--port", "9090"}, true},
		{"environment delete", "kupe app env get shop production", []string{"env", "delete", "shop", "production", "--yes"}, true},
	} {
		for _, interrupted := range []bool{false, true} {
			name := "timeout"
			if interrupted {
				name = "cancelled"
			}
			t.Run(operation.name+"/"+name, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				var written atomic.Bool
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("ETag", `"uid.1"`)
					if r.Method == http.MethodGet && written.Load() && interrupted {
						cancel()
					}
					if r.Method != http.MethodGet {
						written.Store(true)
					}
					if operation.environment {
						env := seededEnv()
						if written.Load() {
							env.Generation = 2
							env.Status.Phase = client.PhaseProvisioning
						}
						_ = json.NewEncoder(w).Encode(env)
					} else {
						_ = json.NewEncoder(w).Encode(&client.App{Name: "shop", Status: &client.AppStatus{Phase: client.PhaseTerminating}})
					}
				}))
				defer srv.Close()
				f := factoryWith(clienttest.New())
				f.Client = func() (client.Interface, error) { return client.New(srv.URL, "acme", "", "test"), nil }
				timeout := "1ms"
				wantCode := cli.ExitTimeout
				if interrupted {
					timeout = "1s"
					wantCode = cli.ExitInterrupted
				}
				cmd := NewCmd(f)
				cmd.SetArgs(append(append([]string{}, operation.args...), "--wait-timeout", timeout))
				cmd.SetOut(f.IOStreams.Out)
				cmd.SetErr(f.IOStreams.ErrOut)
				cmd.SilenceUsage, cmd.SilenceErrors = true, true
				err := cmd.ExecuteContext(ctx)
				var ce *cli.Error
				if cli.ExitCode(err) != wantCode || !errors.As(err, &ce) {
					t.Fatalf("exit=%d err=%v", cli.ExitCode(err), err)
				}
				wantHint := "check status:  " + operation.status
				if interrupted && operation.name == "environment create" {
					wantHint += "\nabandon:       kupe app env delete shop production"
				}
				if ce.Hint != wantHint {
					t.Fatalf("hint=%q want=%q", ce.Hint, wantHint)
				}
				if !written.Load() {
					t.Fatal("wait failed before the mutation was accepted")
				}
			})
		}
	}
}

func TestAppLostContactWaitHintsKeepErrorAndAvailableGuidance(t *testing.T) {
	cause := &client.APIError{StatusCode: http.StatusServiceUnavailable, Message: "temporarily unavailable"}
	for _, verb := range []string{"create", "update", "delete"} {
		err := environmentHint(cmdutil.MapWaitErr(cause, "environment", "production", verb), "shop")
		var ce *cli.Error
		if cli.ExitCode(err) != cli.ExitUnavailable || !errors.As(err, &ce) || !errors.Is(err, cause) || ce.Hint != "check status:  kupe app env get shop production" {
			t.Fatalf("verb=%s error=%v hint=%v", verb, err, ce)
		}
	}
	err := availableWaitHint(cmdutil.MapWaitErr(cause, "app", "shop", "delete"))
	var ce *cli.Error
	if cli.ExitCode(err) != cli.ExitUnavailable || !errors.As(err, &ce) || !errors.Is(err, cause) || ce.Hint != "check status:  kupe app get shop" {
		t.Fatalf("error=%v hint=%v", err, ce)
	}
}

func TestAppWaitHintAdapterPreservesOtherHints(t *testing.T) {
	err := cli.MisuseError("image must be a digest reference").WithHint(imageHint)
	var ce *cli.Error
	if !errors.As(availableWaitHint(err), &ce) || ce.Hint != imageHint {
		t.Fatalf("image hint changed: %v", ce)
	}
	if !strings.Contains(ce.Hint, `"kupe app deploy"`) || !strings.Contains(ce.Hint, `"kupe app image resolve"`) {
		t.Fatalf("approved future image guidance changed: %s", ce.Hint)
	}
}
