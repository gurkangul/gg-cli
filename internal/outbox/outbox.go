// Package outbox implements a file-based pending-write queue for derived-store
// crash-safety. It protects `gg index` writes to Memgraph and brain JSONL writes
// whose Qdrant semantic-index upsert must be replayed later.
//
// Scope: outbox entries are written for index operations and for JSONL-first
// brain writes when Qdrant/Ollama is unavailable. The durable local JSONL write
// remains source-of-truth; the outbox records derived-index repair work.
//
// Each pending entry is a small JSON file in <ggDir>/outbox/<id>.json.
// On successful completion the caller deletes the file. If the process dies
// between "start" and "done", the entry survives and reconcile picks it up.
package outbox

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
)

const dirName = "outbox"

// Entry is a pending dual-store write.
type Entry struct {
	ID        string          `json:"id"`
	Kind      string          `json:"kind"`    // e.g. "full-index", "changed-index"
	Payload   json.RawMessage `json:"payload"` // kind-specific, arbitrary JSON
	CreatedAt string          `json:"created_at"`
	Retries   int             `json:"retries"`
}

func outboxDir(ggDir string) string {
	return filepath.Join(ggDir, dirName)
}

func entryPath(ggDir, id string) string {
	return filepath.Join(outboxDir(ggDir), id+".json")
}

// Write records a new pending operation and returns its ID.
// The caller must call Delete with the returned ID when the operation succeeds.
func Write(ggDir, kind string, payload any) (string, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("outbox: marshal payload: %w", err)
	}
	e := Entry{
		ID:        uuid.New().String(),
		Kind:      kind,
		Payload:   raw,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	data, err := json.Marshal(e)
	if err != nil {
		return "", fmt.Errorf("outbox: marshal entry: %w", err)
	}
	if err := os.MkdirAll(outboxDir(ggDir), 0o755); err != nil {
		return "", fmt.Errorf("outbox: mkdir: %w", err)
	}
	if err := os.WriteFile(entryPath(ggDir, e.ID), data, 0o644); err != nil {
		return "", fmt.Errorf("outbox: write %s: %w", e.ID, err)
	}
	return e.ID, nil
}

// Delete removes an outbox entry. Idempotent — no error if the file is already gone.
func Delete(ggDir, id string) error {
	err := os.Remove(entryPath(ggDir, id))
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("outbox: remove %s: %w", id, err)
	}
	return nil
}

// List returns all pending entries from the outbox directory.
// Returns nil, nil when the directory does not exist.
func List(ggDir string) ([]Entry, error) {
	dir := outboxDir(ggDir)
	fis, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("outbox: list: %w", err)
	}
	var out []Entry
	for _, fi := range fis {
		if fi.IsDir() || filepath.Ext(fi.Name()) != ".json" {
			continue
		}
		data, readErr := os.ReadFile(filepath.Join(dir, fi.Name()))
		if readErr != nil {
			continue // skip unreadable files — they'll stay for the next reconcile pass
		}
		var e Entry
		if jsonErr := json.Unmarshal(data, &e); jsonErr != nil {
			continue // skip malformed files
		}
		out = append(out, e)
	}
	return out, nil
}

// IncrementRetries bumps the retry counter on an outbox entry.
func IncrementRetries(ggDir, id string) error {
	path := entryPath(ggDir, id)
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("outbox: read %s: %w", id, err)
	}
	var e Entry
	if err := json.Unmarshal(data, &e); err != nil {
		return fmt.Errorf("outbox: unmarshal %s: %w", id, err)
	}
	e.Retries++
	updated, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("outbox: marshal updated %s: %w", id, err)
	}
	return os.WriteFile(path, updated, 0o644)
}
