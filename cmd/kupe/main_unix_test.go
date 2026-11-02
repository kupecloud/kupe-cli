//go:build darwin || linux

package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// The child calls the real run entry point, including signal.NotifyContext,
// with an inherited process stdin descriptor. No factory or reader is replaced.
func TestValuesInputSignalHelper(t *testing.T) {
	if os.Getenv("KUPE_VALUES_SIGNAL_HELPER") != "1" {
		return
	}
	os.Args = []string{"kupe", "secret", "values", "set", "--path", "team/db", "--expected-version", "0", "--config", os.Getenv("KUPE_VALUES_TEST_CONFIG")}
	if path := os.Getenv("KUPE_VALUES_TEST_FILE"); path != "" {
		os.Args = append(os.Args, "--file", path)
	} else {
		os.Args = append(os.Args, "--stdin")
	}
	ready := os.NewFile(3, "ready")
	_, _ = ready.Write([]byte{1})
	_ = ready.Close()
	os.Exit(run())
}

func TestValuesInputProcessSignals(t *testing.T) {
	for _, input := range []string{"stdin", "fifo"} {
		for _, signal := range []os.Signal{os.Interrupt, syscall.SIGTERM} {
			t.Run(input+"/"+signal.String(), func(t *testing.T) {
				path := ""
				if input == "fifo" {
					path = filepath.Join(t.TempDir(), "values.fifo")
					if err := unix.Mkfifo(path, 0600); err != nil {
						t.Fatal(err)
					}
				}
				executable, err := os.Executable()
				if err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				child := exec.CommandContext(ctx, executable, "-test.run=^TestValuesInputSignalHelper$") // #nosec G204 -- starts only this test executable
				// Keep race detection enabled, but exclude its deliberate exit
				// sleep from the measured signal-to-exit cancellation bound.
				child.Env = []string{"GOMAXPROCS=2", "GORACE=atexit_sleep_ms=0", "KUPE_VALUES_SIGNAL_HELPER=1", "KUPE_NO_UPDATE_CHECK=1", "KUPE_VALUES_TEST_FILE=" + path, "KUPE_VALUES_TEST_CONFIG=" + filepath.Join(t.TempDir(), "unused.yaml")}
				ready, readyWriter, err := os.Pipe()
				if err != nil {
					t.Fatal(err)
				}
				defer ready.Close()
				defer readyWriter.Close()
				child.ExtraFiles = []*os.File{readyWriter}
				stdin, err := child.StdinPipe()
				if err != nil {
					t.Fatal(err)
				}
				defer stdin.Close()
				var stdout, stderr bytes.Buffer
				child.Stdout, child.Stderr = &stdout, &stderr
				if err = child.Start(); err != nil {
					t.Fatal(err)
				}
				_ = readyWriter.Close()
				done := make(chan error, 1)
				go func() { done <- child.Wait() }()
				joined := false
				t.Cleanup(func() {
					cancel()
					_ = stdin.Close()
					if !joined {
						<-done
					}
				})
				var started [1]byte
				if _, err := ready.Read(started[:]); err != nil {
					t.Fatal(err)
				}
				if input == "stdin" {
					// The writer remains open through cancellation: EOF cannot
					// release the command or explain the expected exit.
					if _, err := stdin.Write([]byte(`{"A":"synthetic-private-sentinel`)); err != nil {
						t.Fatal(err)
					}
				}
				select {
				case err := <-done:
					joined = true
					t.Fatalf("input did not remain pending before signal: %v", err)
				case <-time.After(100 * time.Millisecond):
				}
				if err := child.Process.Signal(signal); err != nil {
					t.Fatal(err)
				}
				select {
				case err := <-done:
					joined = true
					var exit *exec.ExitError
					if !errors.As(err, &exit) || exit.ExitCode() != 130 {
						t.Fatalf("exit=%v stdout=%q stderr=%q", err, stdout.String(), stderr.String())
					}
				case <-time.After(time.Second):
					t.Fatal("signal did not interrupt pending input")
				}
				if stdout.Len() != 0 || strings.Contains(stderr.String(), "synthetic-private-sentinel") || !strings.Contains(stderr.String(), "context canceled") {
					t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
				}
			})
		}
	}
}
