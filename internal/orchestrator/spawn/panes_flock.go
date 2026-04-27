//go:build !windows

package spawn

import (
	"fmt"
	"os"
	"syscall"
)

// withPanesFlock acquires an exclusive advisory flock on the given lock file
// path, calls fn, then releases the lock. The lock file is created if absent.
// This serialises cross-process RMW cycles on panes.json.
func withPanesFlock(lockPath string, fn func() error) error {
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("open panes flock file: %w", err)
	}
	defer f.Close() //nolint:errcheck

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("panes flock acquire: %w", err)
	}
	defer func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }()

	return fn()
}
