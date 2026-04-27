//go:build windows

package spawn

// withPanesFlock is a no-op on Windows: calls fn directly without locking.
// Cross-process serialisation via syscall.Flock is not available on Windows.
func withPanesFlock(_ string, fn func() error) error {
	return fn()
}
