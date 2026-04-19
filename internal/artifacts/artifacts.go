// Package artifacts manages .gg/installed.json — a manifest that records
// which version (SHA256 of content) of each gg-managed template file was
// installed. gg doctor --sync-artifacts reads it to surface drift between
// the installed files and the current CLI's templates.
package artifacts

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const fileName = "installed.json"

// Manifest is the content of .gg/installed.json.
type Manifest struct {
	// Artifacts maps the artifact path (relative to .gg/) to its installed SHA256.
	Artifacts map[string]string `json:"artifacts"`
	// InstalledAt is the RFC3339 timestamp of the last write.
	InstalledAt string `json:"installed_at,omitempty"`
}

// Read loads the Manifest from <ggDir>/installed.json.
// Returns an empty Manifest (not an error) when the file does not exist.
func Read(ggDir string) (Manifest, error) {
	path := filepath.Join(ggDir, fileName)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Manifest{Artifacts: map[string]string{}}, nil
	}
	if err != nil {
		return Manifest{}, fmt.Errorf("read %s: %w", path, err)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return Manifest{}, fmt.Errorf("parse %s: %w", path, err)
	}
	if m.Artifacts == nil {
		m.Artifacts = map[string]string{}
	}
	return m, nil
}

// Write atomically persists m to <ggDir>/installed.json.
// InstalledAt is always set to now.
func Write(ggDir string, m Manifest) error {
	m.InstalledAt = time.Now().UTC().Format(time.RFC3339)
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(ggDir, fileName)
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

// Record marks a single artifact as installed at the given content hash.
// It loads, updates, and atomically saves the manifest.
func Record(ggDir, artifactKey, contentHash string) error {
	m, err := Read(ggDir)
	if err != nil {
		return err
	}
	m.Artifacts[artifactKey] = contentHash
	return Write(ggDir, m)
}

// SHA256Hex returns the hex-encoded SHA256 of content.
func SHA256Hex(content string) string {
	h := sha256.Sum256([]byte(content))
	return fmt.Sprintf("%x", h)
}

// DriftEntry describes a single artifact's drift status.
type DriftEntry struct {
	Key       string // artifact path relative to .gg/
	Installed string // SHA of what's installed (empty = not installed)
	Current   string // SHA of the current CLI template
	Status    string // "ok", "drifted", "missing"
}

// Diff compares the manifest against a map of current template SHAs.
// Returns one DriftEntry per artifact in currentSHAs, ordered as provided.
func Diff(m Manifest, currentSHAs map[string]string) []DriftEntry {
	var out []DriftEntry
	for key, sha := range currentSHAs {
		installed := m.Artifacts[key]
		var status string
		switch {
		case installed == "":
			status = "missing"
		case installed == sha:
			status = "ok"
		default:
			status = "drifted"
		}
		out = append(out, DriftEntry{
			Key:       key,
			Installed: installed,
			Current:   sha,
			Status:    status,
		})
	}
	return out
}
