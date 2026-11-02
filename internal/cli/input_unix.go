//go:build darwin || linux

package cli

import (
	"context"
	"errors"
	"io"
	"os"
	"runtime"

	"golang.org/x/sys/unix"
)

func openInputFile(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0) // #nosec G304 -- caller explicitly selects a local input file
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil // #nosec G115 -- successful Open returns a non-negative native descriptor
}

func readFileInput(ctx context.Context, file *os.File, limit int64) ([]byte, error) {
	conn, err := file.SyscallConn()
	if err != nil {
		return nil, err
	}
	var raw []byte
	var readErr error
	// Control keeps the borrowed descriptor alive without Fd's side effect of
	// disabling Go's nonblocking mode. No read worker or cancellation pipe is
	// needed. Restore the exact flags before returning ownership to the caller.
	err = conn.Control(func(fd uintptr) {
		const setBits = len(unix.FdSet{}.Bits) * unix.NFDBITS
		if runtime.GOOS == "darwin" && fd >= uintptr(setBits) {
			// Darwin's libc select limits nfds to FD_SETSIZE. Own a low
			// duplicate for this wait; exhaustion fails like any read error.
			duplicate, dupErr := unix.FcntlInt(fd, unix.F_DUPFD_CLOEXEC, 0)
			if dupErr != nil {
				readErr = dupErr
				return
			}
			defer func() { _ = unix.Close(duplicate) }()
			if duplicate >= setBits {
				readErr = unix.EMFILE
				return
			}
			fd = uintptr(duplicate) // #nosec G115 -- successful FcntlInt returns a non-negative native descriptor
		}
		flags, flagErr := unix.FcntlInt(fd, unix.F_GETFL, 0)
		if flagErr != nil {
			readErr = flagErr
			return
		}
		if _, flagErr = unix.FcntlInt(fd, unix.F_SETFL, flags|unix.O_NONBLOCK); flagErr != nil {
			readErr = flagErr
			return
		}
		raw, readErr = readInput(ctx, &fileInput{ctx: ctx, fd: int(fd)}, limit) // #nosec G115 -- RawConn supplies a native descriptor, bounded to an int
		if _, restoreErr := unix.FcntlInt(fd, unix.F_SETFL, flags); readErr == nil {
			readErr = restoreErr
		}
	})
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, err
	}
	return raw, readErr
}

type fileInput struct {
	ctx context.Context
	fd  int
}

func (r *fileInput) Read(p []byte) (int, error) {
	for {
		if err := r.ctx.Err(); err != nil {
			return 0, err
		}
		// macOS poll/kqueue can miss FIFO EOF. Select preserves both the
		// initial writer rendezvous and EOF, with a bounded cancellation wait.
		// Linux copies enough consecutive bits for fd+1 descriptors. Darwin
		// uses a low owned duplicate when the inherited descriptor is higher.
		const setBits = len(unix.FdSet{}.Bits) * unix.NFDBITS
		sets := make([]unix.FdSet, r.fd/setBits+1)
		sets[r.fd/setBits].Set(r.fd % setBits)
		ready, err := unix.Select(r.fd+1, &sets[0], nil, nil, &unix.Timeval{Usec: 50_000})
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return 0, err
		}
		if ready == 0 {
			continue
		}
		if err := r.ctx.Err(); err != nil {
			return 0, err
		}
		n, err := unix.Read(r.fd, p)
		if errors.Is(err, unix.EINTR) || errors.Is(err, unix.EAGAIN) {
			continue
		}
		if err != nil {
			return 0, err
		}
		if n == 0 {
			return 0, io.EOF
		}
		return n, err
	}
}
