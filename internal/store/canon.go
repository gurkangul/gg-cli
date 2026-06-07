package store

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// CanonEntry is one distilled "what every dev must know" bucket of the project's
// institutional memory (TASK-468). Areas are durable buckets like
// "architecture", "auth", "gotchas". Canon is the agent-distilled layer on top
// of the raw decision/bug ledger: gg stores it and injects it at session-start,
// the agent produces it (no-network → no cloud LLM; distillation is agent-driven).
type CanonEntry struct {
	Area      string `json:"area"`
	Text      string `json:"text"`
	Author    string `json:"author"`
	UpdatedAt string `json:"updated_at"`
}

// canonPath stores canon at <ggDir>/canon.jsonl — deliberately OUTSIDE
// <ggDir>/brain/, because `gg brain export` rebuilds the brain dir from Qdrant
// and would clobber a JSONL-only file there. Canon is append-only with
// last-write-wins per area (re-setting an area overwrites it).
func canonPath(ggDir string) string {
	return filepath.Join(ggDir, "canon.jsonl")
}

// SetCanon writes (or overwrites) the canon entry for an area by appending a new
// line; ReadCanon folds to the latest per area. An empty text clears the area.
func (c *Client) SetCanon(area, text, author string) error {
	return WriteCanon(c.dataDir, area, text, author)
}

// WriteCanon appends a canon entry to <ggDir>/canon.jsonl. Standalone (no
// Client) so any caller with a resolved .gg dir can use it.
func WriteCanon(ggDir, area, text, author string) error {
	area = strings.TrimSpace(area)
	if area == "" {
		return fmt.Errorf("canon area is required (e.g. architecture, auth, gotchas)")
	}
	if err := os.MkdirAll(ggDir, 0o755); err != nil {
		return fmt.Errorf("canon mkdir: %w", err)
	}
	e := CanonEntry{Area: area, Text: text, Author: author, UpdatedAt: time.Now().UTC().Format(time.RFC3339)}
	data, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("canon marshal: %w", err)
	}
	f, err := os.OpenFile(canonPath(ggDir), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("canon open: %w", err)
	}
	defer func() { _ = f.Close() }()
	if _, err := fmt.Fprintf(f, "%s\n", data); err != nil {
		return fmt.Errorf("canon write: %w", err)
	}
	return nil
}

// ListCanon returns the current canon (one entry per area, folded
// last-write-wins, sorted by area).
func (c *Client) ListCanon() ([]CanonEntry, error) {
	return ReadCanon(c.dataDir)
}

// ReadCanon reads canon directly from a known .gg dir (cwd-independent, no
// Qdrant) and folds it to one current entry per area. Empty-text areas (cleared)
// are omitted. Missing file => empty, no error.
func ReadCanon(ggDir string) ([]CanonEntry, error) {
	f, err := os.Open(canonPath(ggDir))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("canon read: %w", err)
	}
	defer func() { _ = f.Close() }()

	latest := map[string]CanonEntry{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		var e CanonEntry
		if json.Unmarshal(sc.Bytes(), &e) != nil || strings.TrimSpace(e.Area) == "" {
			continue
		}
		latest[strings.ToLower(e.Area)] = e // last line wins
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("canon scan: %w", err)
	}
	out := make([]CanonEntry, 0, len(latest))
	for _, e := range latest {
		if strings.TrimSpace(e.Text) == "" {
			continue
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Area < out[j].Area })
	return out, nil
}
