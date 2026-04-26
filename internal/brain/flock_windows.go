//go:build windows

package brain

import "os"

// withFileLock is a no-op on Windows: cross-process flock is not implemented
// (gg-cli is primarily macOS/Linux). fn is called directly without locking.
func withFileLock(_ *os.File, fn func() error) error {
	return fn()
}
