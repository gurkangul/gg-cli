//go:build windows

package brain

import "errors"

// ErrReconcileLocked mirrors the unix sentinel; on Windows cross-process flock
// is not implemented so the lock is a no-op (gg-cli is primarily macOS/Linux).
var ErrReconcileLocked = errors.New("brain: reconcile already in progress")

// AcquireReconcileLock is a no-op on Windows: it always succeeds with a no-op
// release. See flock_windows.go for the same rationale.
func AcquireReconcileLock(_ string) (func(), error) {
	return func() {}, nil
}
