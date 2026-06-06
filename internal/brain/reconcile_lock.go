//go:build !windows

package brain

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// ErrReconcileLocked means another process already holds the reconcile lock.
// Reconcile must not run concurrently — two reconcilers can clobber a healthy
// vector store (BUG-073).
var ErrReconcileLocked = errors.New("brain: reconcile already in progress")

// AcquireReconcileLock takes a non-blocking exclusive lock on
// .gg/brain/.reconcile.lock. It returns a release func on success, or
// ErrReconcileLocked when another reconcile holds it. Non-blocking (LOCK_NB) so
// a second reconcile fails fast instead of serialising behind the first.
func AcquireReconcileLock(ggDir string) (func(), error) {
	if err := os.MkdirAll(dir(ggDir), 0o755); err != nil {
		return nil, fmt.Errorf("brain: mkdir: %w", err)
	}
	path := filepath.Join(dir(ggDir), ".reconcile.lock")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("brain: open reconcile lock: %w", err)
	}
	if lockErr := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); lockErr != nil {
		_ = f.Close()
		if errors.Is(lockErr, syscall.EWOULDBLOCK) {
			return nil, ErrReconcileLocked
		}
		return nil, fmt.Errorf("brain: acquire reconcile lock: %w", lockErr)
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}
