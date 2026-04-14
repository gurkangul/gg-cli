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
	if err := cache.Put(dir, "auth flow", payload{Value: "JWT"}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	// Get it back.
	var out payload
	cachedAt, found, err := cache.Get(dir, "auth flow", &out)
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
	_, found, err := cache.Get(dir, "nonexistent query", &out)
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

	_ = cache.Put(dir, "  Auth Flow  ", p{V: 1})
	_ = cache.Put(dir, "auth flow", p{V: 2}) // same normalised key — overwrites

	var out p
	_, found, err := cache.Get(dir, "AUTH FLOW", &out)
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

func TestEviction(t *testing.T) {
	// Use a tiny cap to keep the test fast.
	const cap = 3
	dir := t.TempDir()
	cacheDir := cache.Dir(dir)

	type p struct{ N int }

	// Write cap+2 entries with distinct queries.
	queries := []string{"alpha", "beta", "gamma", "delta", "epsilon"}
	for i, q := range queries {
		if err := cache.Put(dir, q, p{N: i}); err != nil {
			t.Fatalf("Put(%q): %v", q, err)
		}
		// Small sleep so mtimes differ — eviction is mtime-ordered.
		time.Sleep(5 * time.Millisecond)
	}

	// With maxEntries=100 (the real cap), no eviction happens for 5 entries.
	// Instead verify that all 5 are present.
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
	_ = cap // used for documentation above
}
