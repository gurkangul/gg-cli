package session

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

type cursorFile struct {
	LastSeenAt time.Time `json:"last_seen_at"`
}

// Path returns the filesystem path for agent's cursor within runtimeDir.
func Path(runtimeDir, agent string) string {
	return filepath.Join(runtimeDir, "sessions", agent, "cursor.json")
}

// Read reads the stored cursor for agent inside runtimeDir.
// Returns (zero, false, nil) when no cursor file exists yet.
// Returns (zero, false, err) on any other I/O or parse error.
func Read(runtimeDir, agent string) (time.Time, bool, error) {
	p := Path(runtimeDir, agent)
	data, err := os.ReadFile(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return time.Time{}, false, nil
		}
		return time.Time{}, false, err
	}
	var cf cursorFile
	if err := json.Unmarshal(data, &cf); err != nil {
		return time.Time{}, false, err
	}
	return cf.LastSeenAt, true, nil
}

// Write persists ts as the cursor for agent inside runtimeDir.
// Parent directories are created if they do not exist.
func Write(runtimeDir, agent string, ts time.Time) error {
	p := Path(runtimeDir, agent)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(cursorFile{LastSeenAt: ts})
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o644)
}
