//go:build windows

package config

import "os"

func withFileLock(_ *os.File, fn func() error) error {
	return fn()
}
