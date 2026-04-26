//go:build !windows

package brain

import (
	"fmt"
	"os"
	"syscall"
)

// withFileLock acquires an exclusive flock on f, calls fn, then releases.
// POSIX O_APPEND is atomic only up to PIPE_BUF (~4096 B); longer payloads can
// interleave between concurrent PIDs, producing torn JSONL lines. The flock
// serialises writers so each line lands atomically regardless of size.
func withFileLock(f *os.File, fn func() error) error {
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("brain: flock acquire: %w", err)
	}
	fnErr := fn()
	if unlockErr := syscall.Flock(int(f.Fd()), syscall.LOCK_UN); unlockErr != nil {
		// flock release failure is extremely rare — log but don't mask fn error.
		if fnErr == nil {
			return fmt.Errorf("brain: flock release: %w", unlockErr)
		}
	}
	return fnErr
}
