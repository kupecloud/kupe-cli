//go:build darwin || linux

package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestReadInputNativeCancellationPreservesBorrowedFile(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	flags := inputFileFlags(t, reader)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := ReadInput(ctx, reader, 1024); done <- err }()
	joined := false
	t.Cleanup(func() {
		cancel()
		_ = writer.Close()
		if !joined {
			<-done
		}
		_ = reader.Close()
	})
	select {
	case err := <-done:
		joined = true
		t.Fatalf("open pipe read returned before cancellation: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	cancel()
	select {
	case err := <-done:
		joined = true
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("native pipe read did not finish after cancellation")
	}
	if got := inputFileFlags(t, reader); got != flags {
		t.Fatalf("borrowed descriptor flags changed: got %d want %d", got, flags)
	}
}

func inputFileFlags(t *testing.T, file *os.File) int {
	t.Helper()
	conn, err := file.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	var flags int
	var flagErr error
	if err := conn.Control(func(fd uintptr) { flags, flagErr = unix.FcntlInt(fd, unix.F_GETFL, 0) }); err != nil {
		t.Fatal(err)
	}
	if flagErr != nil {
		t.Fatal(flagErr)
	}
	return flags
}

func TestReadInputFIFOAwaitsWriterAndCancels(t *testing.T) {
	path := filepath.Join(t.TempDir(), "values.fifo")
	if err := unix.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		file, err := OpenInputFile(ctx, path)
		if err == nil {
			defer file.Close()
			_, err = ReadInput(ctx, file, 1024)
		}
		done <- err
	}()
	joined := false
	t.Cleanup(func() {
		cancel()
		// Even a broken blocking-open implementation is released and joined.
		if fd, err := unix.Open(path, unix.O_WRONLY|unix.O_NONBLOCK, 0); err == nil {
			_ = unix.Close(fd)
		}
		if !joined {
			<-done
		}
	})
	select {
	case err := <-done:
		joined = true
		t.Fatalf("FIFO returned before its first writer: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	cancel()
	select {
	case err := <-done:
		joined = true
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("FIFO open/read did not cancel")
	}
}

func TestReadInputFIFOWriterEOF(t *testing.T) {
	for _, input := range []string{"", `{"A":"synthetic"}`} {
		t.Run(input, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "values.fifo")
			if err := unix.Mkfifo(path, 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			file, err := OpenInputFile(ctx, path)
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			type result struct {
				raw []byte
				err error
			}
			done := make(chan result, 1)
			go func() { raw, err := ReadInput(ctx, file, 1024); done <- result{raw, err} }()
			joined := false
			t.Cleanup(func() {
				cancel()
				if !joined {
					<-done
				}
			})
			select {
			case got := <-done:
				joined = true
				t.Fatalf("FIFO returned before its late writer: read=%q err=%v", got.raw, got.err)
			case <-time.After(100 * time.Millisecond):
			}
			// The read is pending when the late writer arrives, even if it
			// immediately closes without writing any bytes.
			writer, err := os.OpenFile(path, os.O_WRONLY, 0)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = writer.WriteString(input); err != nil {
				t.Fatal(err)
			}
			_ = writer.Close()
			got := <-done
			joined = true
			if got.err != nil || string(got.raw) != input {
				t.Fatalf("read=%q err=%v", got.raw, got.err)
			}
		})
	}
}

func TestReadInputHighDescriptor(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	conn, err := reader.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	var fd int
	var dupErr error
	if err := conn.Control(func(original uintptr) { fd, dupErr = unix.FcntlInt(original, unix.F_DUPFD_CLOEXEC, 1100) }); err != nil {
		t.Fatal(err)
	}
	if dupErr != nil {
		t.Fatal(dupErr)
	}
	high := os.NewFile(uintptr(fd), "high-descriptor-input")
	defer high.Close()
	flags := inputFileFlags(t, high)
	if _, err := writer.WriteString("synthetic"); err != nil {
		t.Fatal(err)
	}
	_ = writer.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	raw, err := ReadInput(ctx, high, 1024)
	if err != nil || string(raw) != "synthetic" {
		t.Fatalf("read=%q err=%v", raw, err)
	}
	if got := inputFileFlags(t, high); got != flags {
		t.Fatalf("high borrowed descriptor flags changed: got %d want %d", got, flags)
	}
}
