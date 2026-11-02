//go:build darwin || linux

package app

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/client/clienttest"
)

func TestEnvironmentNativeStdinCancellationKeepsBorrowedFileOpen(t *testing.T) {
	for _, verb := range []string{"create", "update"} {
		t.Run(verb, func(t *testing.T) {
			reader, writer, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			f := factoryWith(clienttest.New())
			var clients atomic.Int32
			f.Client = func() (client.Interface, error) { clients.Add(1); return clienttest.New(), nil }
			f.IOStreams.In = reader
			cmd := NewCmd(f)
			cmd.SetArgs([]string{"env", verb, "shop", "production", "-f", "-"})
			cmd.SetOut(f.IOStreams.Out)
			cmd.SetErr(f.IOStreams.ErrOut)
			cmd.SilenceUsage, cmd.SilenceErrors = true, true
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan error, 1)
			go func() { done <- cmd.ExecuteContext(ctx) }()
			joined := false
			t.Cleanup(func() {
				cancel()
				_ = writer.Close()
				_ = reader.Close()
				if !joined {
					<-done
				}
			})
			select {
			case err := <-done:
				joined = true
				t.Fatalf("open native pipe returned before cancellation: %v", err)
			case <-time.After(20 * time.Millisecond):
			}
			cancel()
			select {
			case err := <-done:
				joined = true
				if cli.ExitCode(err) != cli.ExitInterrupted || !errors.Is(err, context.Canceled) || clients.Load() != 0 {
					t.Fatalf("exit=%d error=%v clients=%d", cli.ExitCode(err), err, clients.Load())
				}
			case <-time.After(time.Second):
				t.Fatal("native stdin read did not honour cancellation")
			}
			if _, err := writer.Write([]byte{'x'}); err != nil {
				t.Fatalf("borrowed pipe was closed: %v", err)
			}
			var data [1]byte
			if n, err := reader.Read(data[:]); n != 1 || err != nil || data[0] != 'x' {
				t.Fatalf("borrowed stdin cannot be reused: n=%d error=%v", n, err)
			}
		})
	}
}

func TestEnvironmentDirectoryFileHasContextualMisuseBeforeClient(t *testing.T) {
	for _, verb := range []string{"create", "update"} {
		t.Run(verb, func(t *testing.T) {
			f := factoryWith(clienttest.New())
			clients := 0
			f.Client = func() (client.Interface, error) { clients++; return clienttest.New(), nil }
			_, _, err := run(f, "env", verb, "shop", "production", "-f", t.TempDir())
			if cli.ExitCode(err) != cli.ExitMisuse || !strings.Contains(err.Error(), "read environment file") || clients != 0 {
				t.Fatalf("exit=%d error=%v clients=%d", cli.ExitCode(err), err, clients)
			}
		})
	}
}
