package cli

import (
	"context"
	"os"
	"runtime"
	"time"

	"golang.org/x/sys/windows"
)

var cancelSynchronousIO = windows.NewLazySystemDLL("kernel32.dll").NewProc("CancelSynchronousIo")

func openInputFile(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|windows.O_FILE_FLAG_OVERLAPPED, 0) // #nosec G304 -- caller explicitly selects a local input file
}

func readFileInput(ctx context.Context, file *os.File, limit int64) ([]byte, error) {
	// Inherited stdin may use synchronous I/O; files opened above use
	// overlapped I/O. Cancel both kinds without closing borrowed stdin.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	thread, err := windows.OpenThread(windows.THREAD_TERMINATE, false, windows.GetCurrentThreadId())
	if err != nil {
		return nil, err
	}
	defer func() { _ = windows.CloseHandle(thread) }()
	conn, err := file.SyscallConn()
	if err != nil {
		return nil, err
	}
	finished := make(chan struct{})
	joined := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		defer close(joined)
		for {
			_, _, _ = cancelSynchronousIO.Call(uintptr(thread))
			_ = conn.Control(func(fd uintptr) { _ = windows.CancelIoEx(windows.Handle(fd), nil) })
			// Retry across the small gap between checking context and starting
			// ReadFile. Always join this callback before releasing either handle.
			select {
			case <-finished:
				return
			case <-time.After(time.Millisecond):
			}
		}
	})
	raw, readErr := readInput(ctx, file, limit)
	close(finished)
	if !stop() {
		<-joined
	}
	return raw, readErr
}
