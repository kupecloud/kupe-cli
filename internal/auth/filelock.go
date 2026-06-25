package auth

import (
	"fmt"
	"os"
	"path/filepath"
)

// fileLock is a cross-process advisory lock backed by a lock file on disk.
// It serialises OIDC refreshes for a single context across concurrent kupe
// invocations (the exec-plugin distribution model makes parallel processes
// routine). The OS releases the lock automatically if the process dies while
// holding it, so a crash can't wedge every future invocation.
//
// The platform-specific lock/unlock syscalls live in filelock_unix.go and
// filelock_windows.go.
type fileLock struct {
	path string
	f    *os.File
}

// newFileLock returns a fileLock for the given path. The lock file is created
// lazily on Lock; the directory is created if missing (0700).
func newFileLock(path string) *fileLock {
	return &fileLock{path: path}
}

// Lock blocks until the advisory lock is held. On success the caller must call
// Unlock. Errors creating or locking the file are returned; callers treat a
// lock error as non-fatal (degrade to lock-free behaviour) since the lock is
// an optimisation, not a correctness gate on its own.
func (l *fileLock) Lock() error {
	dir := filepath.Dir(l.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating lock dir %s: %w", dir, err)
	}
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_RDWR, 0o600) //#nosec G304 -- path derived from the user's config dir, by design
	if err != nil {
		return fmt.Errorf("opening lock file %s: %w", l.path, err)
	}
	if err := lockFile(f); err != nil {
		_ = f.Close()
		return fmt.Errorf("locking %s: %w", l.path, err)
	}
	l.f = f
	return nil
}

// Unlock releases the lock and closes the underlying file. Safe to call once
// after a successful Lock; a no-op if Lock never succeeded.
func (l *fileLock) Unlock() {
	if l.f == nil {
		return
	}
	_ = unlockFile(l.f)
	_ = l.f.Close()
	l.f = nil
}

// refreshLockPath returns the lock-file path used to serialise refreshes for a
// given context. It sits next to the credentials file so it shares the same
// 0700 directory and never collides with config/credentials data.
func refreshLockPath(credentialsPath, context string) string {
	dir := filepath.Dir(credentialsPath)
	// Context names are validated elsewhere, but be defensive: replace path
	// separators so the lock file always lands in the config dir.
	safe := filepath.Base(context)
	if safe == "" || safe == "." || safe == string(filepath.Separator) {
		safe = "_"
	}
	return filepath.Join(dir, ".refresh-"+safe+".lock")
}
