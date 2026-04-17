// Package cache provides a file-based last-known-good (LKG) cache for
// search results. Entries are stored as JSON under
// ~/.gg/projects/<project_id>/cache/search-lkg/ (the per-project runtime dir),
// keyed by a truncated SHA-256 of the normalised (kind, query) pair. The cache
// is capped at maxEntries; oldest entries (by mtime) are evicted when the cap
// is exceeded.
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

	// DefaultTTL is the maximum age of a cache entry before it is considered a
	// miss. Entries older than this are silently deleted on the next Get.
	DefaultTTL = 7 * 24 * time.Hour
)

// Entry is the on-disk envelope for a cached payload.
type Entry struct {
	Query    string          `json:"query"`
	CachedAt time.Time       `json:"cached_at"`
	Data     json.RawMessage `json:"data"`
}

// Dir returns the cache directory under runtimeDir.
func Dir(runtimeDir string) string {
	return filepath.Join(runtimeDir, subDir)
}

// Put serialises data as JSON and writes it atomically to the cache under
// runtimeDir, keyed by (kind, query). Using distinct kind values ("search",
// "context", etc.) prevents collisions between callers that use the same
// query string.
// It evicts the oldest entries when the cap is exceeded.
func Put(runtimeDir, kind, query string, data any) error {
	dir := Dir(runtimeDir)
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

	// Atomic write: write to a temp file in the same directory, then rename.
	// This prevents a partial write from permanently poisoning the cache.
	tmp, err := os.CreateTemp(dir, ".lkg-tmp-*")
	if err != nil {
		return fmt.Errorf("cache tempfile: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }() // no-op if rename succeeded

	if _, err := tmp.Write(encoded); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("cache write tmp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("cache close tmp: %w", err)
	}
	if err := os.Rename(tmpPath, filepath.Join(dir, keyFile(kind, query))); err != nil {
		return fmt.Errorf("cache rename: %w", err)
	}

	// Best-effort eviction — don't fail the caller if this errors.
	_ = evict(dir, maxEntries)
	return nil
}

// Get reads a cached entry for (kind, query) and unmarshals its data into out.
// Returns (cachedAt, true, nil) on a valid hit.
// Returns (zero, false, nil) on miss, TTL expiry, or a corrupt file (which is
// deleted so it does not poison future reads).
// Any other I/O error is returned verbatim.
func Get(runtimeDir, kind, query string, out any) (cachedAt time.Time, found bool, err error) {
	return GetWithTTL(runtimeDir, kind, query, out, DefaultTTL)
}

// GetWithTTL is like Get but uses a caller-specified TTL instead of DefaultTTL.
// A zero TTL disables expiry.
func GetWithTTL(runtimeDir, kind, query string, out any, ttl time.Duration) (cachedAt time.Time, found bool, err error) {
	path := filepath.Join(Dir(runtimeDir), keyFile(kind, query))
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, fmt.Errorf("cache read: %w", err)
	}

	var entry Entry
	if err := json.Unmarshal(raw, &entry); err != nil {
		// Corrupt file — delete it so it does not block future writes.
		_ = os.Remove(path)
		return time.Time{}, false, nil
	}
	if err := json.Unmarshal(entry.Data, out); err != nil {
		_ = os.Remove(path)
		return time.Time{}, false, nil
	}

	// TTL check: treat expired entries as misses and clean them up.
	if ttl > 0 && time.Since(entry.CachedAt) > ttl {
		_ = os.Remove(path)
		return time.Time{}, false, nil
	}

	return entry.CachedAt, true, nil
}

// keyFile returns the filename for a (kind, query) pair: first 16 hex chars
// of SHA-256 of "<kind>:<lowercased, trimmed query>".
//
// Including kind in the hash prevents callers with different namespaces from
// colliding. For example, search("ctx:auth") and context("auth") both normalise
// to "ctx:auth" without namespacing — with kind they differ.
func keyFile(kind, query string) string {
	key := kind + ":" + strings.ToLower(strings.TrimSpace(query))
	h := sha256.Sum256([]byte(key))
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
		path  string
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
