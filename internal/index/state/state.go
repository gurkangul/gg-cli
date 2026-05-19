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
	"sort"
	"time"
)

const fileName = "index-state.json"

// ErrNoState is returned by Read when no index-state.json exists yet.
// Callers should fall back to a full re-index.
var ErrNoState = errors.New("no index state found — run a full index first")

// IndexState records the outcome of successful index runs.
type IndexState struct {
	// Legacy top-level fields describe the most recent successful index run.
	// Older gg versions only wrote these fields, so readers must continue to
	// treat them as a conservative all-source freshness record when Languages is
	// empty.
	LastIndexedSHA string `json:"last_indexed_sha"`
	IndexedAt      string `json:"indexed_at"`
	// WorkingTreeFingerprint captures dirty/untracked source content indexed on
	// top of LastIndexedSHA. Empty means the tree was clean, or this state was
	// written by an older gg version before dirty-tree fingerprints existed.
	WorkingTreeFingerprint string `json:"working_tree_fingerprint,omitempty"`

	// Languages records freshness per indexed language. Multi-language projects
	// are healthy only when every detected language has a fresh entry; this avoids
	// the old false choice between marking mixed repos stale forever or hiding
	// dirty source files from unindexed languages.
	Languages map[string]LanguageState `json:"languages,omitempty"`
}

// LanguageState records one successful index run for a single language.
type LanguageState struct {
	LastIndexedSHA         string   `json:"last_indexed_sha"`
	IndexedAt              string   `json:"indexed_at"`
	WorkingTreeFingerprint string   `json:"working_tree_fingerprint,omitempty"`
	Extensions             []string `json:"extensions,omitempty"`
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
	if s.LastIndexedSHA == "" && len(s.Languages) == 0 {
		return nil, ErrNoState
	}
	return &s, nil
}

// ForLanguage returns the freshness record that should anchor incremental or
// status checks for lang. Language-aware state is preferred; legacy top-level
// state remains a conservative fallback for projects indexed before the
// language map existed.
func (s *IndexState) ForLanguage(lang string) (LanguageState, bool) {
	if s == nil {
		return LanguageState{}, false
	}
	if s.Languages != nil {
		entry, ok := s.Languages[lang]
		if ok && entry.LastIndexedSHA != "" {
			return entry, true
		}
		return LanguageState{}, false
	}
	if s.LastIndexedSHA == "" {
		return LanguageState{}, false
	}
	return LanguageState{
		LastIndexedSHA:         s.LastIndexedSHA,
		IndexedAt:              s.IndexedAt,
		WorkingTreeFingerprint: s.WorkingTreeFingerprint,
	}, true
}

// IndexedLanguages returns language keys in deterministic order.
func (s *IndexState) IndexedLanguages() []string {
	if s == nil || len(s.Languages) == 0 {
		return nil
	}
	langs := make([]string, 0, len(s.Languages))
	for lang, entry := range s.Languages {
		if entry.LastIndexedSHA == "" {
			continue
		}
		langs = append(langs, lang)
	}
	sort.Strings(langs)
	return langs
}

// Write atomically overwrites <ggDir>/index-state.json with the given state.
// The indexed_at timestamp is always set to now.
// Only call Write after a successful index run — never on failure.
func Write(ggDir, sha string) error {
	return WriteWithFingerprint(ggDir, sha, "")
}

// WriteWithFingerprint records a legacy successful index run and, when
// non-empty, the dirty working-tree source fingerprint that was included in
// that run. Prefer WriteLanguage for new code so multi-language repos can track
// per-language freshness.
func WriteWithFingerprint(ggDir, sha, workingTreeFingerprint string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	s := IndexState{
		LastIndexedSHA:         sha,
		IndexedAt:              now,
		WorkingTreeFingerprint: workingTreeFingerprint,
	}
	return writeState(ggDir, s)
}

// WriteLanguage records a successful index run for one language while
// preserving freshness entries for other languages in the same project.
func WriteLanguage(ggDir, lang, sha, workingTreeFingerprint string, extensions []string) error {
	if lang == "" {
		return WriteWithFingerprint(ggDir, sha, workingTreeFingerprint)
	}
	s, err := Read(ggDir)
	if errors.Is(err, ErrNoState) {
		s = &IndexState{}
	} else if err != nil {
		return err
	}
	if s.Languages == nil {
		s.Languages = make(map[string]LanguageState)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	extCopy := append([]string(nil), extensions...)
	s.Languages[lang] = LanguageState{
		LastIndexedSHA:         sha,
		IndexedAt:              now,
		WorkingTreeFingerprint: workingTreeFingerprint,
		Extensions:             extCopy,
	}
	// Keep legacy fields useful for older readers and compact human output: they
	// describe the most recent language index, not whole-project freshness.
	s.LastIndexedSHA = sha
	s.IndexedAt = now
	s.WorkingTreeFingerprint = workingTreeFingerprint
	return writeState(ggDir, *s)
}

func writeState(ggDir string, s IndexState) error {
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
