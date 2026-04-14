// Package cache provides a file-based last-known-good (LKG) cache for
// search results. Entries are stored as JSON under <ggDir>/cache/search-lkg/,
// keyed by a truncated SHA-256 of the normalised query. The cache is capped
// at maxEntries; oldest entries (by mtime) are evicted when the cap is
// exceeded.
package cache

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	maxEntries = 100
	subDir     = "cache/search-lkg"
)

// Entry is the on-disk envelope for a cached payload.
type Entry struct {
	Query    string          `json:"query"`
	CachedAt time.Time       `json:"cached_at"`
	Data     json.RawMessage `json:"data"`
}

// Dir returns the cache directory under ggDir.
func Dir(ggDir string) string {
	return filepath.Join(ggDir, subDir)
}

// Put serialises data as JSON and writes it to the cache under ggDir.
// It evicts the oldest entries when the cap is exceeded.
func Put(ggDir, query string, data any) error {
	dir := Dir(ggDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("cache mkdir: %w", err)
	}

	raw, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("cache marshal: %w", err)
	}

	entry := Entry{
		Query:    query,
		CachedAt: time.Now().UTC(),
		Data:     raw,
	}
	encoded, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return fmt.Errorf("cache marshal entry: %w", err)
	}

	path := filepath.Join(dir, keyFile(query))
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		return fmt.Errorf("cache write: %w", err)
	}

	// Best-effort eviction — don't fail the caller if this errors.
	_ = evict(dir, maxEntries)
	return nil
}

// Get reads a cached entry for query and unmarshals its data into out.
// Returns the cache timestamp and true when found; returns false when the
// entry does not exist. Any other error is returned verbatim.
func Get(ggDir, query string, out any) (cachedAt time.Time, found bool, err error) {
	path := filepath.Join(Dir(ggDir), keyFile(query))
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, fmt.Errorf("cache read: %w", err)
	}

	var entry Entry
	if err := json.Unmarshal(raw, &entry); err != nil {
		return time.Time{}, false, fmt.Errorf("cache decode: %w", err)
	}
	if err := json.Unmarshal(entry.Data, out); err != nil {
		return time.Time{}, false, fmt.Errorf("cache decode data: %w", err)
	}
	return entry.CachedAt, true, nil
}

// keyFile returns the filename for a query: first 16 hex chars of SHA-256
// of the lowercased, trimmed query.
func keyFile(query string) string {
	h := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(query))))
	return fmt.Sprintf("%x.json", h[:8])
}

// evict removes the oldest cache entries (by mtime) until ≤ max remain.
func evict(dir string, max int) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	// Filter to .json files only.
	var files []os.DirEntry
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".json" {
			files = append(files, e)
		}
	}
	if len(files) <= max {
		return nil
	}

	type fileAge struct {
		path string
		mtime time.Time
	}
	ages := make([]fileAge, 0, len(files))
	for _, f := range files {
		info, err := f.Info()
		if err != nil {
			continue
		}
		ages = append(ages, fileAge{filepath.Join(dir, f.Name()), info.ModTime()})
	}
	// Sort oldest first.
	sort.Slice(ages, func(i, j int) bool { return ages[i].mtime.Before(ages[j].mtime) })

	toDelete := len(ages) - max
	for i := range toDelete {
		_ = os.Remove(ages[i].path)
	}
	return nil
}
