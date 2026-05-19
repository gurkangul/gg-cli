package config

import (
	"fmt"
	"os"
	"path/filepath"
)

const configWriteLockFile = "config.yaml.lock"

// WithWriteLock serializes project-local config read/modify/write operations.
func WithWriteLock(fn func() error) error {
	ggDir, err := GGDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(ggDir, 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	lockPath := filepath.Join(ggDir, configWriteLockFile)
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("open config lock: %w", err)
	}
	defer func() { _ = f.Close() }()
	if err := withFileLock(f, fn); err != nil {
		return fmt.Errorf("config write lock: %w", err)
	}
	return nil
}
