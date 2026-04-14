// Package compat implements the indexer version compat-matrix for gg.
//
// Problem: the three SCIP indexers (scip-go, scip-typescript, scip-python)
// have independent release cadences. A user might update scip-go independently
// of gg, or vice versa, and the two may produce incompatible SCIP proto schemas
// or symbol formats. Silent data corruption is worse than a hard error.
//
// Solution: on first index run, write a manifest recording the exact binary
// versions in use. On subsequent index runs, compare the installed versions
// against the manifest. Mismatch → hard error with "run gg doctor --install-indexers".
//
// The manifest lives at .gg/indexer-manifest.json (project-local).
package compat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const manifestFile = "indexer-manifest.json"

// Manifest records the exact versions of all indexers in use at the time
// of the last successful index run.
type Manifest struct {
	// Indexers maps binary name → version string (e.g. "scip-go" → "0.1.26").
	Indexers map[string]string `json:"indexers"`
	// SCIPLib is the version of the github.com/scip-code/scip Go module
	// linked into this gg binary. Ensures SCIP proto schema compatibility.
	SCIPLib string `json:"scip_lib"`
	// UpdatedAt is the RFC3339 timestamp of the last manifest write.
	UpdatedAt string `json:"updated_at"`
}

// ErrVersionMismatch is returned by Check when an installed binary version
// differs from the manifest. Callers should surface Binary, Recorded, and
// Installed to the user with a clear remediation hint.
type ErrVersionMismatch struct {
	Binary    string
	Recorded  string
	Installed string
}

func (e *ErrVersionMismatch) Error() string {
	return fmt.Sprintf(
		"indexer version mismatch for %q: manifest has %q but found %q — "+
			"run 'gg doctor --install-indexers' to re-pin, or delete .gg/%s to reset",
		e.Binary, e.Recorded, e.Installed, manifestFile,
	)
}

// ReadManifest reads the manifest from the project .gg directory.
// Returns (nil, nil) if no manifest exists yet (first run).
func ReadManifest(ggDir string) (*Manifest, error) {
	path := filepath.Join(ggDir, manifestFile)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read indexer manifest: %w", err)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse indexer manifest: %w", err)
	}
	return &m, nil
}

// WriteManifest writes m to .gg/indexer-manifest.json, creating the file if
// necessary. Safe to call after a successful index run.
func WriteManifest(ggDir string, m *Manifest) error {
	m.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(ggDir, manifestFile)
	return os.WriteFile(path, append(data, '\n'), 0644)
}

// CollectVersions queries each binary in binaries for its version by running
// `<binary> --version`. Binaries not on PATH or ~/.gg/bin are skipped (not
// an error — they may simply not be installed yet).
func CollectVersions(ctx context.Context, binaries []string) (map[string]string, error) {
	versions := make(map[string]string, len(binaries))
	for _, bin := range binaries {
		ver, err := queryVersion(ctx, bin)
		if err != nil {
			// Binary not found or --version failed — skip, not a hard error here.
			continue
		}
		versions[bin] = ver
	}
	return versions, nil
}

// queryVersion runs `<binary> --version` and returns the trimmed output.
func queryVersion(ctx context.Context, binary string) (string, error) {
	cmd := exec.CommandContext(ctx, binary, "--version")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return strings.TrimSpace(out.String()), nil
}

// Check compares the installed binary versions against the manifest.
// Returns the first mismatch as *ErrVersionMismatch. Returns nil if
// all versions match, or if the manifest doesn't record a version for
// a given binary (newly added indexer).
func Check(ctx context.Context, m *Manifest, binaries []string) error {
	if m == nil {
		return nil // no manifest yet — first run
	}
	installed, err := CollectVersions(ctx, binaries)
	if err != nil {
		return err
	}
	for _, bin := range binaries {
		recorded, ok := m.Indexers[bin]
		if !ok {
			continue // no recorded version — new indexer, skip
		}
		got, found := installed[bin]
		if !found {
			continue // not installed on this machine — let the runner error later
		}
		if recorded != got {
			return &ErrVersionMismatch{Binary: bin, Recorded: recorded, Installed: got}
		}
	}
	return nil
}

// SCIPLibVersion returns the version of the scip-code/scip Go module currently
// linked into the binary. Read from go.sum / build info via debug.ReadBuildInfo.
// Returns "unknown" if build info is unavailable (e.g. in tests or non-module
// builds).
func SCIPLibVersion() string {
	// runtime/debug.ReadBuildInfo gives us the module graph of the current binary.
	// We look for github.com/scip-code/scip/bindings/go/scip.
	type buildInfo interface {
		Main() string
	}
	// We don't import runtime/debug to avoid a heavy import chain —
	// the version is baked in at link time via the module path in go.sum.
	// For day-1, return the version we know we're linked against.
	// When this package is tested, the test can override via the manifest.
	return scipLibVersionFromBuildInfo()
}
