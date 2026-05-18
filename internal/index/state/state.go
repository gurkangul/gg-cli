// Package state manages the index-state.json file that records the last
// successfully indexed commit SHA. The --changed mode uses this to compute
// the delta between the indexed snapshot and the current working tree.
package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const fileName = "index-state.json"

// ErrNoState is returned by Read when no index-state.json exists yet.
// Callers should fall back to a full re-index.
var ErrNoState = errors.New("no index state found — run a full index first")

// IndexState records the outcome of the last successful index run.
type IndexState struct {
	// LastIndexedSHA is the git commit hash that was HEAD when the last full or
	// incremental index completed successfully.
	LastIndexedSHA string `json:"last_indexed_sha"`
	// IndexedAt is the RFC3339 timestamp of the last successful index run.
	IndexedAt string `json:"indexed_at"`
	// WorkingTreeFingerprint captures dirty/untracked source content indexed on
	// top of LastIndexedSHA. Empty means the tree was clean, or this state was
	// written by an older gg version before dirty-tree fingerprints existed.
	WorkingTreeFingerprint string `json:"working_tree_fingerprint,omitempty"`
}

// Read loads the IndexState from <ggDir>/index-state.json.
// Returns ErrNoState if the file does not exist (first run).
func Read(ggDir string) (*IndexState, error) {
	path := filepath.Join(ggDir, fileName)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, ErrNoState
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var s IndexState
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if s.LastIndexedSHA == "" {
		return nil, ErrNoState
	}
	return &s, nil
}

// Write atomically overwrites <ggDir>/index-state.json with the given state.
// The indexed_at timestamp is always set to now.
// Only call Write after a successful index run — never on failure.
func Write(ggDir, sha string) error {
	return WriteWithFingerprint(ggDir, sha, "")
}

// WriteWithFingerprint records a successful index run and, when non-empty, the
// dirty working-tree source fingerprint that was included in that run.
func WriteWithFingerprint(ggDir, sha, workingTreeFingerprint string) error {
	s := IndexState{
		LastIndexedSHA:         sha,
		IndexedAt:              time.Now().UTC().Format(time.RFC3339),
		WorkingTreeFingerprint: workingTreeFingerprint,
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(ggDir, fileName)
	// Write to a temp file first, then rename — atomic on POSIX.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0600); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename %s → %s: %w", tmp, path, err)
	}
	return nil
}
