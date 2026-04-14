//go:build unix

package store

import (
	"os"
	"syscall"
)

// tryLockFile attempts a non-blocking exclusive lock. Returns nil on success,
// an error (typically syscall.EWOULDBLOCK) if another process holds it.
func tryLockFile(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
}

func unlockFile(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}
