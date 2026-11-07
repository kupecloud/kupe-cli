package app

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/client/clienttest"
)

type stalledEnvironmentInput struct {
	io.ReadCloser
	started, closing, release, closed chan struct{}
	startedOnce                       sync.Once
}

func (r *stalledEnvironmentInput) Read(p []byte) (int, error) {
	r.startedOnce.Do(func() { close(r.started) })
	return r.ReadCloser.Read(p)
}

func (r *stalledEnvironmentInput) Close() error {
	err := r.ReadCloser.Close()
	close(r.closing)
	<-r.release
	close(r.closed)
	return err
}

func TestEnvironmentStalledStdinCancellationJoinsInputCleanup(t *testing.T) {
	for _, verb := range []string{"create", "update"} {
		t.Run(verb, func(t *testing.T) {
			reader, writer := io.Pipe()
			input := &stalledEnvironmentInput{ReadCloser: reader, started: make(chan struct{}), closing: make(chan struct{}), release: make(chan struct{}), closed: make(chan struct{})}
			f := factoryWith(clienttest.New())
			var clients atomic.Int32
			f.Client = func() (client.Interface, error) { clients.Add(1); return clienttest.New(), nil }
			f.IOStreams.In = input
			cmd := NewCmd(f)
			cmd.SetArgs([]string{"env", verb, "shop", "production", "-f", "-"})
			cmd.SetOut(f.IOStreams.Out)
			cmd.SetErr(f.IOStreams.ErrOut)
			cmd.SilenceUsage, cmd.SilenceErrors = true, true
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan error, 1)
			go func() { done <- cmd.ExecuteContext(ctx) }()
			var release sync.Once
			joined := false
			t.Cleanup(func() {
				cancel()
				release.Do(func() { close(input.release) })
				_ = writer.Close()
				_ = reader.Close()
				if !joined {
					<-done
				}
			})
			select {
			case <-input.started:
			case <-time.After(time.Second):
				t.Fatal("command did not start reading stdin")
			}
			const sentinel = "environment-input-must-not-appear"
			if _, err := io.WriteString(writer, `{"web":{"env":[{"name":"PRIVATE","value":"`+sentinel); err != nil {
				t.Fatal(err)
			}
			cancel()
			select {
			case <-input.closing:
			case <-time.After(time.Second):
				t.Fatal("cancellation did not unblock stdin")
			}
			select {
			case <-done:
				joined = true
				t.Fatal("command returned before input cleanup completed")
			default:
			}
			release.Do(func() { close(input.release) })
			select {
			case err := <-done:
				joined = true
				if cli.ExitCode(err) != cli.ExitInterrupted || !errors.Is(err, context.Canceled) || clients.Load() != 0 {
					t.Fatalf("exit=%d err=%v clients=%d", cli.ExitCode(err), err, clients.Load())
				}
				if strings.Contains(err.Error(), sentinel) || strings.Contains(f.IOStreams.Out.(interface{ String() string }).String(), sentinel) || strings.Contains(f.IOStreams.ErrOut.(interface{ String() string }).String(), sentinel) {
					t.Fatal("input value appeared in command output")
				}
			case <-time.After(time.Second):
				t.Fatal("command did not finish after input cleanup")
			}
			select {
			case <-input.closed:
			default:
				t.Fatal("input cancellation cleanup was not joined")
			}
		})
	}
}

func TestEnvironmentCancelledInputWinsOverFileAndParseErrors(t *testing.T) {
	for _, path := range []string{"-", "missing-environment-file"} {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		fields, err := readEnvironmentFile(ctx, path, strings.NewReader("invalid: [environment-input-must-not-appear"), true)
		if fields != nil || !errors.Is(err, context.Canceled) || cli.ExitCode(err) != cli.ExitInterrupted {
			t.Fatalf("path=%q fields=%v error=%v", path, fields, err)
		}
	}
}

func TestEnvironmentInputByteLimitAndStrictParsingRemain(t *testing.T) {
	prefix := `{"web":{"env":[{"name":"PRIVATE","value":"environment-input-must-not-appear"}]}}`
	for _, tc := range []struct {
		name, input string
		code        int
		message     string
	}{
		{"exact byte limit", prefix + strings.Repeat(" ", maxEnvironmentFile-len(prefix)), cli.ExitOK, ""},
		{"one byte over limit", prefix + strings.Repeat(" ", maxEnvironmentFile+1-len(prefix)), cli.ExitMisuse, "1 MiB"},
		{"duplicate", `{"web":{"port":8080,"port":9090}}`, cli.ExitMisuse, "duplicate"},
		{"multiple documents", "web: {}\n---\nweb: {}\n", cli.ExitMisuse, "one document"},
		{"unknown field", `{"web":{"unexpected":"environment-input-must-not-appear"}}`, cli.ExitMisuse, `unknown field "unexpected"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := readEnvironmentFile(context.Background(), "-", strings.NewReader(tc.input), true)
			if cli.ExitCode(err) != tc.code {
				t.Fatalf("exit=%d error=%v", cli.ExitCode(err), err)
			}
			if err != nil && (!strings.Contains(err.Error(), tc.message) || strings.Contains(err.Error(), "environment-input-must-not-appear")) {
				t.Fatalf("unexpected input error: %v", err)
			}
		})
	}
}

// The Vault path grammar matches the API's: it starts and
// ends with a letter or digit, and allows - _ . / between.
func TestSecretReferencePathGrammarMatchesAPI(t *testing.T) {
	for path, ok := range map[string]bool{
		"apps/shop/api": true, "a": true, "a-b_c.d/e": true, "A1/b2": true,
		"/apps/api": false, "apps/api/": false, "-apps": false, "apps-": false, ".hidden": false, "apps/../x": false, "a b": false, "": false,
	} {
		replicas := int32(1)
		req := client.CreateAppEnvironmentRequest{Cluster: "europe", Web: &client.AppWeb{Image: digest, Port: 8080, Replicas: &replicas, Resources: &client.AppResources{
			Requests: client.AppQuantities{CPU: "250m", Memory: "256Mi"}, Limits: client.AppQuantities{CPU: "1", Memory: "512Mi"}}}}
		req.Secrets = []client.AppSecret{{Name: "db", Path: path}}
		err := validateEnvironment(req)
		if (err == nil) != ok {
			t.Errorf("path %q: ok=%v err=%v", path, ok, err)
		}
	}
}
