//go:build windows

package auth

import (
	"os"

	"golang.org/x/sys/windows"
)

// lockFile takes an exclusive lock on the whole file via LockFileEx, blocking
// until acquired. Windows releases the lock when the handle is closed (which
// happens on process exit), matching the Unix flock semantics.
func lockFile(f *os.File) error {
	ol := new(windows.Overlapped)
	// Lock a large fixed range covering the (empty) lock file.
	return windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK, 0, ^uint32(0), ^uint32(0), ol)
}

// unlockFile releases the lock taken by lockFile.
func unlockFile(f *os.File) error {
	ol := new(windows.Overlapped)
	return windows.UnlockFileEx(windows.Handle(f.Fd()), 0, ^uint32(0), ^uint32(0), ol)
}
