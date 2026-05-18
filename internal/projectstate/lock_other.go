//go:build !unix && !windows

package projectstate

import (
	"fmt"
	"os"
	"path/filepath"
)

func lock(runtimeDir string) (func(), error) {
	path := filepath.Join(runtimeDir, fileName+".lock")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open lock %s: %w", path, err)
	}
	return func() {
		_ = f.Close()
	}, nil
}
