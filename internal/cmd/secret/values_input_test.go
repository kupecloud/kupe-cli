package secret

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/client/clienttest"
)

type stalledValuesInput struct {
	*io.PipeReader
	started, closing, release, closed chan struct{}
	readOnce, closeOnce               sync.Once
}

func (r *stalledValuesInput) Read(p []byte) (int, error) {
	r.readOnce.Do(func() { close(r.started) })
	return r.PipeReader.Read(p)
}

func (r *stalledValuesInput) Close() error {
	r.closeOnce.Do(func() {
		_ = r.PipeReader.Close()
		close(r.closing)
		<-r.release
		close(r.closed)
	})
	return nil
}

func TestValuesStalledInputCancellationJoinsCleanup(t *testing.T) {
	reader, writer := io.Pipe()
	input := &stalledValuesInput{PipeReader: reader, started: make(chan struct{}), closing: make(chan struct{}), release: make(chan struct{}), closed: make(chan struct{})}
	f := factoryWith(t, clienttest.New())
	var calls atomic.Int32
	f.Client = func() (client.Interface, error) { calls.Add(1); return clienttest.New(), nil }
	f.IOStreams.In = input
	cmd := newValuesSetCmd(f)
	cmd.SetArgs([]string{"--path", "team/db", "--expected-version", "0", "--stdin"})
	cmd.SetOut(f.IOStreams.Out)
	cmd.SetErr(f.IOStreams.ErrOut)
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
		t.Fatal("command did not enter the real pipe read")
	}
	cancel()
	select {
	case <-input.closing:
	case <-time.After(time.Second):
		t.Fatal("cancellation did not close stalled input")
	}
	select {
	case <-done:
		joined = true
		t.Fatal("command returned before cancellation cleanup completed")
	default:
	}
	release.Do(func() { close(input.release) })
	select {
	case err := <-done:
		joined = true
		if cli.ExitCode(err) != cli.ExitInterrupted || !errors.Is(err, context.Canceled) || calls.Load() != 0 {
			t.Fatalf("exit=%d err=%v client calls=%d", cli.ExitCode(err), err, calls.Load())
		}
	case <-time.After(time.Second):
		t.Fatal("command did not finish after cancellation cleanup")
	}
	select {
	case <-input.closed:
	default:
		t.Fatal("input cleanup was not joined")
	}
}
