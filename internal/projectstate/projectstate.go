// Package projectstate manages ~/.gg/projects/<id>/state.json — per-project
// runtime state that persists across sessions but is never committed to the
// repo. Currently tracks last_seen_cli_version so session-start can surface
// a version-delta notice when the CLI has been upgraded between sessions.
package projectstate

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const fileName = "state.json"

// State is the persisted per-project runtime state.
type State struct {
	// LastSeenCLIVersion is the CLI version string seen at the last session-start.
	// Empty string means "never recorded" (first session).
	LastSeenCLIVersion string `json:"last_seen_cli_version,omitempty"`
	// UpdatedAt is the RFC3339 timestamp of the last write.
	UpdatedAt string `json:"updated_at,omitempty"`
}

// Read loads the State from <runtimeDir>/state.json.
// Returns a zero State (not an error) when the file does not exist — first
// session, no prior version recorded.
func Read(runtimeDir string) (State, error) {
	path := filepath.Join(runtimeDir, fileName)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return State{}, nil
	}
	if err != nil {
		return State{}, fmt.Errorf("read %s: %w", path, err)
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return State{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return s, nil
}

// Write atomically persists the State to <runtimeDir>/state.json.
// UpdatedAt is always set to now.
func Write(runtimeDir string, s State) error {
	s.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(runtimeDir, fileName)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename %s → %s: %w", tmp, path, err)
	}
	return nil
}
