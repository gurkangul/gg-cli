//go:build windows

package terminal

import (
	"context"
	"os"
	"path/filepath"
	"strings"
)

// surfaceLockPath returns the path of the per-surface advisory flock file.
func surfaceLockPath(spawnDir string, id SurfaceID) string {
	safe := strings.NewReplacer(":", "_", "/", "_", "\\", "_").Replace(string(id))
	return filepath.Join(spawnDir, safe+".lock")
}

// acquireFlockCtx is a no-op on Windows: returns a nil file and a no-op
// release function. Cross-process serialisation via file locking is not
// implemented on Windows (gg-cli is primarily macOS/Linux).
func acquireFlockCtx(_ context.Context, path string) (*os.File, func(), error) {
	_ = path
	return nil, func() {}, nil
}
