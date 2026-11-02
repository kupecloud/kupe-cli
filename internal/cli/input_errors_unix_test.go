//go:build darwin || linux

package cli

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestNativeInputTerminalReadErrorCount(t *testing.T) {
	for _, test := range []struct {
		name string
		flag int
		want error
	}{
		{"directory", os.O_RDONLY, unix.EISDIR},
		{"write-only", os.O_WRONLY | os.O_CREATE, unix.EBADF},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := t.TempDir()
			if test.name != "directory" {
				path = filepath.Join(path, "write-only")
			}
			file, err := os.OpenFile(path, test.flag, 0600)
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			withNativeInputFD(t, file, func(fd int) {
				n, err := (&fileInput{ctx: ctx, fd: fd}).Read(make([]byte, 32))
				if n != 0 || !errors.Is(err, test.want) {
					t.Errorf("native Reader returned count=%d err=%v; want count=0 err=%v", n, err, test.want)
				}
			})
		})
	}
}

func TestNativeInputReadErrorKeepsAccumulatedBytes(t *testing.T) {
	const prefix = "synthetic-prefix-before-native-error"
	path := filepath.Join(t.TempDir(), "prefix")
	if err := os.WriteFile(path, []byte(prefix), 0600); err != nil {
		t.Fatal(err)
	}
	good, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer good.Close()
	bad, err := os.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer bad.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	withNativeInputFD(t, good, func(goodFD int) {
		withNativeInputFD(t, bad, func(badFD int) {
			// Both sources use actual native reads. The terminal directory
			// failure must not shorten bytes already accumulated by ReadAll.
			input := io.MultiReader(&fileInput{ctx: ctx, fd: goodFD}, &fileInput{ctx: ctx, fd: badFD})
			raw, err := readInput(ctx, input, 1024)
			if string(raw) != prefix || !errors.Is(err, unix.EISDIR) {
				t.Errorf("read=%q err=%v; want complete prefix and EISDIR", raw, err)
			}
		})
	})
}

func TestReadInputNativeErrorRestoresBorrowedFlags(t *testing.T) {
	file, err := os.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	flags := inputFileFlags(t, file)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	defer func() {
		if recovered := recover(); recovered != nil {
			t.Errorf("ReadInput panicked on a native read error: %v", recovered)
		}
		if got := inputFileFlags(t, file); got != flags {
			t.Errorf("borrowed descriptor flags changed on error: got %d want %d", got, flags)
		}
	}()
	raw, err := ReadInput(ctx, file, 1024)
	if len(raw) != 0 || !errors.Is(err, unix.EISDIR) {
		t.Errorf("read=%q err=%v; want no bytes and EISDIR", raw, err)
	}
}

func withNativeInputFD(t *testing.T, file *os.File, run func(int)) {
	t.Helper()
	conn, err := file.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Control(func(fd uintptr) { run(int(fd)) }); err != nil {
		t.Fatal(err)
	}
}
