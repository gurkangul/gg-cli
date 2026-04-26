// Package brain provides the JSONL write path for gg brain entries.
//
// Architecture (TASK-129 / BUG-030): .gg/brain/<kind>.jsonl is the primary
// source of truth. Qdrant is a derived index rebuilt via `gg doctor --reconcile`.
// All brain-write verbs (record, task, bug, reject) write JSONL first; the
// caller then attempts Qdrant upsert with a short deadline, queuing an outbox
// entry on failure so the upsert can be replayed when Qdrant recovers.
//
// JSONL entries are append-only and idempotent by uuid: if a uuid already
// appears in the file the appended line is a no-op when scanned (uuid is the
// dedup key during replay).
package brain

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
)

const brainDir = "brain"

// Entry is a single JSONL record.  Kind matches the Qdrant collection suffix
// (decisions, rejections, tasks, bugs).
type Entry struct {
	UUID      string         `json:"uuid"`
	Kind      string         `json:"kind"`
	CreatedAt string         `json:"created_at"`
	Author    string         `json:"author,omitempty"`
	Payload   map[string]any `json:"payload"`
}

// dir returns the path to .gg/brain/.
func dir(ggDir string) string {
	return filepath.Join(ggDir, brainDir)
}

// kindPath returns the JSONL file path for a given kind.
func kindPath(ggDir, kind string) string {
	return filepath.Join(dir(ggDir), kind+".jsonl")
}

// Append writes an entry to .gg/brain/<kind>.jsonl, creating the file and
// directory on first use.  Idempotent by UUID: callers set UUID before calling
// so retries are safe.
//
// An exclusive flock is held for the duration of the write so that concurrent
// PIDs do not produce torn lines for payloads larger than PIPE_BUF (~4096 B).
func Append(ggDir, kind, id, author string, payload map[string]any) error {
	if id == "" {
		id = uuid.New().String()
	}
	e := Entry{
		UUID:      id,
		Kind:      kind,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		Author:    author,
		Payload:   payload,
	}
	data, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("brain: marshal entry: %w", err)
	}
	if err := os.MkdirAll(dir(ggDir), 0o755); err != nil {
		return fmt.Errorf("brain: mkdir: %w", err)
	}
	f, err := os.OpenFile(kindPath(ggDir, kind), os.O_APPEND|os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return fmt.Errorf("brain: open %s: %w", kind, err)
	}
	defer f.Close()
	return withFileLock(f, func() error {
		if _, wErr := fmt.Fprintf(f, "%s\n", data); wErr != nil {
			return fmt.Errorf("brain: write %s: %w", kind, wErr)
		}
		return nil
	})
}

// ReadAll reads every entry from .gg/brain/<kind>.jsonl.
// Returns nil, nil when the file does not exist (empty brain for this kind).
// Malformed lines are skipped silently; use ReadAllWithCount to observe them.
func ReadAll(ggDir, kind string) ([]Entry, error) {
	entries, _, err := ReadAllWithCount(ggDir, kind)
	return entries, err
}

// ReadAllWithCount is like ReadAll but also returns the number of malformed
// lines skipped. A non-zero count means the JSONL file is partially corrupted
// (torn write or manual edit). Callers that want to surface this to the user
// should emit a warning when skipped > 0.
func ReadAllWithCount(ggDir, kind string) (entries []Entry, skipped int, err error) {
	path := kindPath(ggDir, kind)
	f, openErr := os.Open(path)
	if openErr != nil {
		if os.IsNotExist(openErr) {
			return nil, 0, nil
		}
		return nil, 0, fmt.Errorf("brain: open %s: %w", kind, openErr)
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20) // 1 MiB max line — large detail fields
	for sc.Scan() {
		var e Entry
		if jsonErr := json.Unmarshal(sc.Bytes(), &e); jsonErr != nil {
			skipped++
			continue
		}
		entries = append(entries, e)
	}
	if scanErr := sc.Err(); scanErr != nil {
		return nil, skipped, fmt.Errorf("brain: scan %s: %w", kind, scanErr)
	}
	return entries, skipped, nil
}

// SearchByText returns entries whose payload "text" or "approach" field
// contains any of the query words (case-insensitive substring).  This is a
// minimal offline fallback for gg search — semantic ranking is not available.
func SearchByText(ggDir, kind, query string) ([]Entry, error) {
	all, err := ReadAll(ggDir, kind)
	if err != nil {
		return nil, err
	}
	if query == "" {
		return all, nil
	}
	// Simple substring scan over the serialised payload.
	qLower := lowercaseASCII(query)
	var out []Entry
	for _, e := range all {
		if entryMatchesQuery(e, qLower) {
			out = append(out, e)
		}
	}
	return out, nil
}

func entryMatchesQuery(e Entry, qLower string) bool {
	for _, field := range []string{"text", "approach", "reason", "title", "detail"} {
		if v, ok := e.Payload[field]; ok {
			if s, ok := v.(string); ok {
				if contains(lowercaseASCII(s), qLower) {
					return true
				}
			}
		}
	}
	return false
}

// lowercaseASCII lowercases ASCII letters only — avoids unicode dependencies.
func lowercaseASCII(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexBytes(s, sub) >= 0)
}

func indexBytes(s, sub string) int {
	if len(sub) == 0 {
		return 0
	}
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
