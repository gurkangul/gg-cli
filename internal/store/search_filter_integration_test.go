package store

import (
	"testing"

	"github.com/google/uuid"
)

// BUG-085: the non-degraded filter used Qdrant is_null, which matches only keys
// that exist AND are explicitly null. Normal records never set
// gg_vector_degraded, so is_null excluded EVERY record and all Search* queries
// returned zero results. This asserts a normal record is found and an
// explicitly-degraded record is excluded.
func TestSearchExcludesOnlyDegraded_Integration(t *testing.T) {
	c, ctx := newIntegrationClient(t, "search-filter")

	vec := make([]float32, VectorSize)
	for i := range vec {
		vec[i] = 0.1
	}

	// Normal record (real, non-degraded vector).
	id := uuid.New().String()
	if err := c.AddDecision(ctx, Decision{ID: id, Text: "alpha pooling decision", Reason: "r", Status: "active", Author: "t"}, vec); err != nil {
		t.Fatalf("AddDecision: %v", err)
	}

	// Explicitly degraded record (zero vector + gg_vector_degraded marker), like
	// a reconcile placeholder.
	degID := uuid.New().String()
	if err := c.ReplayBrainEntry(ctx, "decisions", degID, map[string]any{
		"text": "beta degraded decision", "status": "active", "created_at": "2026-01-01T00:00:00Z",
	}); err != nil {
		t.Fatalf("ReplayBrainEntry(degraded): %v", err)
	}

	got, err := c.SearchDecisions(ctx, vec, 50, true)
	if err != nil {
		t.Fatalf("SearchDecisions: %v", err)
	}
	var foundNormal, foundDegraded bool
	for _, d := range got {
		switch d.ID {
		case id:
			foundNormal = true
		case degID:
			foundDegraded = true
		}
	}
	if !foundNormal {
		t.Fatal("normal record missing from search results (BUG-085: is_null excluded everything)")
	}
	if foundDegraded {
		t.Fatal("degraded zero-vector record must be excluded from semantic search")
	}
}
