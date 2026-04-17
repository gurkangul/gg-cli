package runner

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// ErrIndexerMissing is returned by Resolve when the named binary cannot be
// found via any of the resolution strategies. Callers can use errors.As to
// extract the binary name and installation hint.
type ErrIndexerMissing struct {
	Binary string // e.g. "scip-go"
	Hint   string // actionable install command, e.g. "go install ..."
}

func (e *ErrIndexerMissing) Error() string {
	return fmt.Sprintf(
		"indexer %q not found in PATH or ~/.gg/bin/ — run 'gg doctor --install-indexers' or manually: %s",
		e.Binary, e.Hint,
	)
}

// IsIndexerMissing reports whether err (or any error in its chain) is an
// ErrIndexerMissing. This is the canonical check callers should use.
func IsIndexerMissing(err error) bool {
	var e *ErrIndexerMissing
	return errors.As(err, &e)
}

// indexerHints maps binary names to their manual installation commands.
// Used as the Hint field of ErrIndexerMissing.
var indexerHints = map[string]string{
	"scip-go":         "go install github.com/sourcegraph/scip-go/cmd/scip-go@latest",
	"scip-typescript": "npm install -g @sourcegraph/scip-typescript",
	"scip-python":     "npm install -g @sourcegraph/scip-python",
}

// resolveChain is the ordered list of strategies tried when locating a binary.
// The first hit wins.
//
//  1. PATH  — user already has it installed globally
//  2. ~/.gg/bin/<name>  — gg-managed installs (gg doctor --install-indexers writes here)
//  3. ErrIndexerMissing  — actionable error with install hint
type resolveChain struct{}

// Resolve returns the absolute path to the named binary, or an ErrIndexerMissing
// describing what to run to fix it.
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

	// 3. Not found — return typed error with actionable hint.
	hint := indexerHints[name]
	if hint == "" {
		hint = fmt.Sprintf("check the %s project for install instructions", name)
	}
	return "", &ErrIndexerMissing{Binary: name, Hint: hint}
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

// ResolveIndexer is the package-level entry point used by cmd/doctor and
// runners. It returns the absolute path to the named binary, or an
// *ErrIndexerMissing if not found.
func ResolveIndexer(name string) (string, error) {
	return resolver.Resolve(name)
}
