//go:build !windows

package terminal

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// surfaceLockPath returns the path of the per-surface advisory flock file.
// Surface IDs like "surface:46" are sanitised so that ":" becomes "_".
func surfaceLockPath(spawnDir string, id SurfaceID) string {
	safe := strings.NewReplacer(":", "_", "/", "_", "\\", "_").Replace(string(id))
	return filepath.Join(spawnDir, safe+".lock")
}

// acquireFlockCtx opens (creating if absent) the lock file at path, acquires an
// exclusive flock, and returns the open file. The caller must call the returned
// release function to unlock and close the file.
//
// The flock is acquired by polling with LOCK_NB so that context cancellation is
// honoured. If the lock is held by another process we retry every 50 ms until
// the context is done.
func acquireFlockCtx(ctx context.Context, path string) (*os.File, func(), error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, nil, fmt.Errorf("create flock dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, nil, fmt.Errorf("open flock file: %w", err)
	}

	const retryInterval = 50 * time.Millisecond
	for {
		lockErr := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if lockErr == nil {
			release := func() {
				_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
				_ = f.Close()
			}
			return f, release, nil
		}
		// EWOULDBLOCK / EAGAIN → another process holds the lock.
		if lockErr != syscall.EWOULDBLOCK && lockErr != syscall.EAGAIN {
			_ = f.Close()
			return nil, nil, fmt.Errorf("flock acquire: %w", lockErr)
		}
		select {
		case <-ctx.Done():
			_ = f.Close()
			return nil, nil, ctx.Err()
		case <-time.After(retryInterval):
		}
	}
}
