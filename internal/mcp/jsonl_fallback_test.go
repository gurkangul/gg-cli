package mcp

import (
	"context"
	"errors"
	"testing"

	brainpkg "github.com/gurkangul/gg-cli/internal/brain"
	"github.com/gurkangul/gg-cli/internal/store"
)

// TestFallbackSearch_CollectionNotFound exercises FIX 1: when the store search
// returns store.IsCollectionNotFoundError (a fresh / un-reembedded project on
// the embedded SQLite backend), fallbackSearch must NOT report empty — it must
// fall back to the JSONL lexical scan and return the seeded record. This is the
// durable-memory false-negative the MCP tools used to silently produce.
func TestFallbackSearch_CollectionNotFound(t *testing.T) {
	ggDir := t.TempDir()
	// Seed a JSONL decision but DO NOT materialize the vector collection, so the
	// real embedded store returns the collection-not-found sentinel on search.
	if err := brainpkg.Append(ggDir, "decisions", "dec-1", "tester", map[string]any{
		"text":   "use embedded sqlite vector store",
		"status": "active",
	}); err != nil {
		t.Fatalf("seed jsonl: %v", err)
	}

	client, err := store.New(ggDir, "proj-test")
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	defer func() { _ = client.Close() }()

	ctx := context.Background()
	vector := make([]float32, store.VectorSize)

	// Sanity: confirm the store itself surfaces the not-found sentinel for an
	// un-created collection (the precondition the fallback hinges on).
	if _, serr := client.SearchDecisions(ctx, vector, 5, false); !store.IsCollectionNotFoundError(serr) {
		t.Fatalf("expected collection-not-found from store, got: %v", serr)
	}

	out, err := fallbackSearch(ggDir, "decisions", "sqlite",
		func() ([]store.Decision, error) { return client.SearchDecisions(ctx, vector, 5, false) },
		decisionFromEntry)
	if err != nil {
		t.Fatalf("fallbackSearch returned error: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 JSONL-fallback decision, got %d", len(out))
	}
	if out[0].Text != "use embedded sqlite vector store" {
		t.Fatalf("unexpected fallback decision text: %q", out[0].Text)
	}
}

// TestFallbackSearch_OtherErrorSurfaces asserts a non-not-found store error is
// returned verbatim (caller emits isError=true) rather than being swallowed.
func TestFallbackSearch_OtherErrorSurfaces(t *testing.T) {
	boom := errors.New("transient store failure")
	out, err := fallbackSearch(t.TempDir(), "decisions", "q",
		func() ([]store.Decision, error) { return nil, boom },
		decisionFromEntry)
	if !errors.Is(err, boom) {
		t.Fatalf("expected the store error to surface, got: %v", err)
	}
	if out != nil {
		t.Fatalf("expected nil results on error, got %v", out)
	}
}

// TestFallbackSearch_HappyPath asserts the store result is returned untouched
// when the search succeeds (no JSONL scan).
func TestFallbackSearch_HappyPath(t *testing.T) {
	want := []store.Decision{{ID: "d1", Text: "from store"}}
	out, err := fallbackSearch(t.TempDir(), "decisions", "q",
		func() ([]store.Decision, error) { return want, nil },
		decisionFromEntry)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out) != 1 || out[0].Text != "from store" {
		t.Fatalf("expected store result passed through, got %v", out)
	}
}
