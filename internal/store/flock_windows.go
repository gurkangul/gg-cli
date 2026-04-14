//go:build windows

package store

import (
	"os"

	"golang.org/x/sys/windows"
)

// tryLockFile attempts a non-blocking exclusive lock. Returns an error
// (ERROR_LOCK_VIOLATION / ERROR_IO_PENDING) if another process holds it.
func tryLockFile(f *os.File) error {
	ol := &windows.Overlapped{}
	return windows.LockFileEx(
		windows.Handle(f.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0, 1, 0, ol,
	)
}

// isLockContention reports whether the error means "would block".
func isLockContention(err error) bool {
	return err == windows.ERROR_LOCK_VIOLATION || err == windows.ERROR_IO_PENDING
}

func unlockFile(f *os.File) error {
	ol := &windows.Overlapped{}
	return windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, ol)
}
