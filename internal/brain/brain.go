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
	"sort"
	"strings"
	"time"
	"unicode"

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
	defer func() {
		_ = f.Close()
	}()
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
	defer func() {
		_ = f.Close()
	}()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20) // 1 MiB max line — large detail fields
	for sc.Scan() {
		e, parseErr := parseEntry(sc.Bytes())
		if parseErr != nil {
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

// parseEntry unmarshals a single JSONL brain entry, handling both the current
// format (uuid/kind/created_at/payload) and the legacy format (id/payload)
// written before TASK-362. The legacy "id" field is mapped to Entry.UUID so
// that validBrainUUID and downstream Qdrant replay both work without data loss.
func parseEntry(line []byte) (Entry, error) {
	// Try the canonical format first.
	var e Entry
	if err := json.Unmarshal(line, &e); err != nil {
		return Entry{}, err
	}
	// If UUID is empty, the line used the legacy "id" key.
	if e.UUID == "" {
		var legacy struct {
			ID      string         `json:"id"`
			Payload map[string]any `json:"payload"`
		}
		if err := json.Unmarshal(line, &legacy); err != nil || legacy.ID == "" {
			return Entry{}, fmt.Errorf("brain: entry missing both uuid and id fields")
		}
		e.UUID = legacy.ID
		if e.Payload == nil {
			e.Payload = legacy.Payload
		}
	}
	return e, nil
}

// ScoredEntry is a JSONL fallback match plus its lightweight token/field score.
type ScoredEntry struct {
	Entry Entry
	Score int
}

// SearchByText returns entries whose payload text fields match the query using
// a lightweight case-insensitive token scorer. It is deliberately small: JSONL
// remains the durable fallback, while Qdrant remains the semantic index.
func SearchByText(ggDir, kind, query string) ([]Entry, error) {
	matches, err := SearchByTextScored(ggDir, kind, query)
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(matches))
	for _, match := range matches {
		out = append(out, match.Entry)
	}
	return out, nil
}

// SearchByTextScored is SearchByText plus the score used for ordering so CLI
// JSON output can expose the same fallback ranking signal it actually used.
func SearchByTextScored(ggDir, kind, query string) ([]ScoredEntry, error) {
	all, err := ReadAll(ggDir, kind)
	if err != nil {
		return nil, err
	}
	query = strings.TrimSpace(query)
	if query == "" {
		out := make([]ScoredEntry, 0, len(all))
		for _, e := range all {
			out = append(out, ScoredEntry{Entry: e})
		}
		return out, nil
	}
	terms := queryTerms(query)
	phrase := strings.ToLower(query)
	type scored struct {
		entry Entry
		score int
		idx   int
	}
	var scoredEntries []scored
	for i, e := range all {
		score := entryQueryScore(e, phrase, terms)
		if score > 0 {
			scoredEntries = append(scoredEntries, scored{entry: e, score: score, idx: i})
		}
	}
	sort.SliceStable(scoredEntries, func(i, j int) bool {
		if scoredEntries[i].score != scoredEntries[j].score {
			return scoredEntries[i].score > scoredEntries[j].score
		}
		if scoredEntries[i].entry.CreatedAt != scoredEntries[j].entry.CreatedAt {
			return scoredEntries[i].entry.CreatedAt > scoredEntries[j].entry.CreatedAt
		}
		return scoredEntries[i].idx < scoredEntries[j].idx
	})
	out := make([]ScoredEntry, 0, len(scoredEntries))
	for _, s := range scoredEntries {
		out = append(out, ScoredEntry{Entry: s.entry, Score: s.score})
	}
	return out, nil
}

type weightedText struct {
	text   string
	weight int
}

func entryQueryScore(e Entry, phrase string, terms []string) int {
	fields := entryWeightedFields(e)
	if len(fields) == 0 || len(terms) == 0 {
		return 0
	}
	matchedTerms := map[string]bool{}
	score := 0
	for _, f := range fields {
		text := strings.ToLower(f.text)
		if text == "" {
			continue
		}
		if phrase != "" && strings.Contains(text, phrase) {
			score += 120 + f.weight
		}
		for _, term := range terms {
			if strings.Contains(text, term) {
				matchedTerms[term] = true
				score += f.weight
			}
		}
	}
	if len(matchedTerms) == 0 {
		return 0
	}
	if len(matchedTerms) == len(terms) {
		score += 80 // AND-style bonus: all query tokens are represented.
	} else {
		score += 10 // OR-style fallback: at least one token matched.
	}
	return score
}

func entryWeightedFields(e Entry) []weightedText {
	fields := []weightedText{
		{text: e.UUID, weight: 90},
		{text: e.Kind, weight: 5},
		{text: e.Author, weight: 5},
	}
	for _, spec := range []struct {
		key    string
		weight int
	}{
		{"task_id", 100}, {"bug_id", 100}, {"id", 90},
		{"title", 80}, {"text", 80}, {"approach", 80},
		{"tags", 60},
		{"reason", 45}, {"detail", 35}, {"root_cause", 35}, {"fix_summary", 35},
		{"affected_files", 30}, {"affected_symbols", 30},
		{"status", 15}, {"priority", 15}, {"severity", 15},
		{"content", 35}, {"from_role", 15}, {"to_role", 15},
	} {
		for _, s := range payloadStrings(e.Payload[spec.key]) {
			fields = append(fields, weightedText{text: s, weight: spec.weight})
		}
	}
	return fields
}

func payloadStrings(v any) []string {
	switch value := v.(type) {
	case string:
		if value == "" {
			return nil
		}
		return []string{value}
	case []string:
		return append([]string(nil), value...)
	case []any:
		out := make([]string, 0, len(value))
		for _, item := range value {
			if s, ok := item.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func queryTerms(query string) []string {
	seen := map[string]bool{}
	var terms []string
	for _, part := range strings.FieldsFunc(strings.ToLower(query), func(r rune) bool {
		return !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_')
	}) {
		part = strings.TrimSpace(part)
		if part == "" || seen[part] {
			continue
		}
		seen[part] = true
		terms = append(terms, part)
	}
	return terms
}
