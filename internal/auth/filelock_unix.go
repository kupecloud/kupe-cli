//go:build !windows

package auth

import (
	"os"

	"golang.org/x/sys/unix"
)

// lockFile takes an exclusive advisory lock (flock LOCK_EX), blocking until
// acquired. The lock is associated with the open file description and is
// released automatically by the kernel when the process exits.
func lockFile(f *os.File) error {
	//#nosec G115 -- os.File.Fd() returns a real OS file descriptor that always fits in an int; this is the standard flock idiom.
	return unix.Flock(int(f.Fd()), unix.LOCK_EX)
}

// unlockFile releases the advisory lock.
func unlockFile(f *os.File) error {
	//#nosec G115 -- see lockFile: a file descriptor always fits in an int.
	return unix.Flock(int(f.Fd()), unix.LOCK_UN)
}
