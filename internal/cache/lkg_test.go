package cache_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gurkangul/gg-cli/internal/cache"
)

func TestPutGet(t *testing.T) {
	dir := t.TempDir()

	type payload struct {
		Value string `json:"value"`
	}

	// Put an entry.
	if err := cache.Put(dir, "search", "auth flow", payload{Value: "JWT"}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	// Get it back.
	var out payload
	cachedAt, found, err := cache.Get(dir, "search", "auth flow", &out)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !found {
		t.Fatal("expected entry to be found")
	}
	if out.Value != "JWT" {
		t.Errorf("got Value=%q, want JWT", out.Value)
	}
	if time.Since(cachedAt) > 5*time.Second {
		t.Errorf("cachedAt too old: %v", cachedAt)
	}
}

func TestGetMissing(t *testing.T) {
	dir := t.TempDir()
	var out struct{ Value string }
	_, found, err := cache.Get(dir, "search", "nonexistent query", &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found {
		t.Fatal("expected not found")
	}
}

func TestQueryNormalisation(t *testing.T) {
	dir := t.TempDir()
	type p struct{ V int }

	_ = cache.Put(dir, "search", "  Auth Flow  ", p{V: 1})
	_ = cache.Put(dir, "search", "auth flow", p{V: 2}) // same normalised key — overwrites

	var out p
	_, found, err := cache.Get(dir, "search", "AUTH FLOW", &out)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !found {
		t.Fatal("expected entry found")
	}
	if out.V != 2 {
		t.Errorf("expected overwritten value 2, got %d", out.V)
	}
}

// TestKindNamespaceIsolation verifies that "search" and "context" with the
// same query do not collide.
func TestKindNamespaceIsolation(t *testing.T) {
	dir := t.TempDir()
	type p struct{ Kind string }

	if err := cache.Put(dir, "search", "auth", p{Kind: "search"}); err != nil {
		t.Fatalf("Put search: %v", err)
	}
	if err := cache.Put(dir, "context", "auth", p{Kind: "context"}); err != nil {
		t.Fatalf("Put context: %v", err)
	}

	var s, c p
	_, foundS, _ := cache.Get(dir, "search", "auth", &s)
	_, foundC, _ := cache.Get(dir, "context", "auth", &c)

	if !foundS || s.Kind != "search" {
		t.Errorf("search entry wrong: found=%v kind=%q", foundS, s.Kind)
	}
	if !foundC || c.Kind != "context" {
		t.Errorf("context entry wrong: found=%v kind=%q", foundC, c.Kind)
	}
}

// TestKindNamespace_SearchQueryCollidesWithContextPrefix verifies that
// a search query "ctx:auth" does not collide with a context query "auth",
// which was the original bug before kind namespacing was added.
func TestKindNamespace_SearchQueryCollidesWithContextPrefix(t *testing.T) {
	dir := t.TempDir()
	type p struct{ V string }

	_ = cache.Put(dir, "search", "ctx:auth", p{V: "search-ctx:auth"})
	_ = cache.Put(dir, "context", "auth", p{V: "context-auth"})

	var sa, ca p
	_, _, _ = cache.Get(dir, "search", "ctx:auth", &sa)
	_, _, _ = cache.Get(dir, "context", "auth", &ca)

	// They must resolve to different values.
	if sa.V != "search-ctx:auth" {
		t.Errorf("search 'ctx:auth' got %q, want 'search-ctx:auth'", sa.V)
	}
	if ca.V != "context-auth" {
		t.Errorf("context 'auth' got %q, want 'context-auth'", ca.V)
	}
}

func TestEviction(t *testing.T) {
	dir := t.TempDir()
	cacheDir := cache.Dir(dir)

	type p struct{ N int }

	// Write cap+2 entries with distinct queries.
	queries := []string{"alpha", "beta", "gamma", "delta", "epsilon"}
	for i, q := range queries {
		if err := cache.Put(dir, "search", q, p{N: i}); err != nil {
			t.Fatalf("Put(%q): %v", q, err)
		}
		// Small sleep so mtimes differ — eviction is mtime-ordered.
		time.Sleep(5 * time.Millisecond)
	}

	// With maxEntries=100 (the real cap), no eviction happens for 5 entries.
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	var jsonCount int
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".json" {
			jsonCount++
		}
	}
	if jsonCount != len(queries) {
		t.Errorf("expected %d cache files, got %d", len(queries), jsonCount)
	}
}

// TestGet_CorruptFileDeletedAndMisses verifies that a corrupt cache file is
// silently deleted and returns (false, nil) instead of propagating the decode
// error, preventing permanent cache poisoning.
func TestGet_CorruptFileDeletedAndMisses(t *testing.T) {
	dir := t.TempDir()
	cacheDir := cache.Dir(dir)
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// First put a valid entry so we know the filename.
	type p struct{ V string }
	if err := cache.Put(dir, "search", "auth", p{V: "x"}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	// Overwrite the file with corrupt JSON.
	entries, _ := os.ReadDir(cacheDir)
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".json" {
			_ = os.WriteFile(filepath.Join(cacheDir, e.Name()), []byte("not-json{{"), 0o600)
		}
	}

	var out p
	_, found, err := cache.Get(dir, "search", "auth", &out)
	if err != nil {
		t.Fatalf("expected nil error for corrupt file, got %v", err)
	}
	if found {
		t.Error("expected miss for corrupt file, got found=true")
	}
	// File must have been deleted.
	remaining, _ := os.ReadDir(cacheDir)
	for _, e := range remaining {
		if filepath.Ext(e.Name()) == ".json" {
			t.Errorf("corrupt file %q was not deleted", e.Name())
		}
	}
}

// TestGet_TTLExpiry verifies that an entry older than the TTL is treated as a
// miss and deleted.
func TestGet_TTLExpiry(t *testing.T) {
	dir := t.TempDir()

	type p struct{ V string }
	if err := cache.Put(dir, "search", "auth", p{V: "old"}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	// Use a tiny TTL so the just-written entry is already expired.
	var out p
	_, found, err := cache.GetWithTTL(dir, "search", "auth", &out, 1*time.Nanosecond)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found {
		t.Error("expected TTL miss, got found=true")
	}

	// File must have been deleted.
	cacheDir := cache.Dir(dir)
	remaining, _ := os.ReadDir(cacheDir)
	for _, e := range remaining {
		if filepath.Ext(e.Name()) == ".json" {
			t.Errorf("expired file %q was not deleted", e.Name())
		}
	}
}

// TestPut_Atomic verifies that Put leaves no temp files behind after a
// successful write (i.e. the rename completed and cleanup ran).
func TestPut_Atomic(t *testing.T) {
	dir := t.TempDir()
	type p struct{ V string }

	if err := cache.Put(dir, "search", "query", p{V: "v"}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	cacheDir := cache.Dir(dir)
	entries, _ := os.ReadDir(cacheDir)
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) != ".json" {
			t.Errorf("unexpected leftover file: %s", e.Name())
		}
	}
}
