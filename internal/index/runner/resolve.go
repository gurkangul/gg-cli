package runner

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// resolveChain is the ordered list of strategies tried when locating a binary.
// The first hit wins.
//
//  1. PATH  — user already has it installed
//  2. ~/.gg/bin/<name>  — gg-managed installs (gg doctor --install-indexers puts them here)
//  3. docker           — fallback via `docker run` wrapper (not implemented yet; reserved)
type resolveChain struct{}

// Resolve returns the absolute path to the named binary, or an error
// describing where it was looked for.
func (resolveChain) Resolve(name string) (string, error) {
	// 1. PATH
	if p, err := exec.LookPath(name); err == nil {
		return p, nil
	}

	// 2. ~/.gg/bin/<name>
	ggBin, err := ggBinDir()
	if err == nil {
		candidate := filepath.Join(ggBin, name)
		if isExecutable(candidate) {
			return candidate, nil
		}
	}

	// 3. Docker fallback — TASK-012 implements gg doctor --install-indexers;
	// docker shim lives there. Returning a descriptive error for now.
	return "", fmt.Errorf(
		"binary %q not found: checked PATH and %s — run 'gg doctor --install-indexers' to download",
		name, filepath.Join("~/.gg/bin", name),
	)
}

// ggBinDir returns the absolute path to ~/.gg/bin/.
func ggBinDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".gg", "bin"), nil
}

// isExecutable reports whether path exists and is a regular executable file.
func isExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.Mode().IsRegular() && (info.Mode()&0o111 != 0)
}

var resolver = resolveChain{}
