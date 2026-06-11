// Package store — integration tests for the near-duplicate surface in dedup.go.
// These exercise the real Qdrant gRPC QueryPoints API (score-thresholded vector
// search) and are gated on GG_INTEGRATION_TEST=1 so the normal `go test ./...`
// gate stays green without a live Qdrant. Each test gets a unique throwaway
// project (collections dropped in t.Cleanup via newIntegrationClient).
//
//	GG_INTEGRATION_TEST=1 go test ./internal/store/ -run 'Dedup.*Integration' -v -count=1
package store

import (
	"testing"

	"github.com/google/uuid"
)

// unitVec returns a VectorSize-length vector that is 1 at index i, 0 elsewhere.
// Two such vectors at different indices are orthogonal (cosine similarity 0);
// the same index gives cosine similarity 1. This lets a threshold cleanly
// separate a near-duplicate from an unrelated record.
func unitVec(i int) []float32 {
	v := make([]float32, VectorSize)
	v[i] = 1
	return v
}

// TestDedupThresholdFiltering_Integration writes two decisions with orthogonal
// vectors, then queries near the first. With a high threshold only the matching
// decision (score ~1.0) is returned; the orthogonal one (score ~0) is filtered.
func TestDedupThresholdFiltering_Integration(t *testing.T) {
	c, ctx := newIntegrationClient(t, "dedup-thresh")

	matchVec := unitVec(0)
	orthoVec := unitVec(1)

	matchID := uuid.New().String()
	if err := c.AddDecision(ctx, Decision{
		ID: matchID, Text: "matching decision", Reason: "r", Status: "active", Author: "tester",
	}, matchVec); err != nil {
		t.Fatalf("AddDecision match: %v", err)
	}
	orthoID := uuid.New().String()
	if err := c.AddDecision(ctx, Decision{
		ID: orthoID, Text: "orthogonal decision", Reason: "r", Status: "active", Author: "tester",
	}, orthoVec); err != nil {
		t.Fatalf("AddDecision ortho: %v", err)
	}

	// Threshold 0.9 admits the cosine-1.0 match, rejects the cosine-0 orthogonal.
	cands, err := c.FindNearDups(ctx, "decisions", matchVec, 0.9, 10)
	if err != nil {
		t.Fatalf("FindNearDups: %v", err)
	}
	if len(cands) != 1 {
		t.Fatalf("FindNearDups returned %d candidates, want 1 (threshold should filter orthogonal): %+v", len(cands), cands)
	}
	got := cands[0]
	// decisions are UUID-keyed: ID is the 8-char UUID prefix.
	if got.ID != matchID[:8] {
		t.Fatalf("candidate ID = %q, want UUID prefix %q", got.ID, matchID[:8])
	}
	if got.Label != "matching decision" {
		t.Fatalf("candidate label = %q, want 'matching decision'", got.Label)
	}
	if got.Score < 0.9 {
		t.Fatalf("candidate score = %v, want >= 0.9", got.Score)
	}
}

// TestDedupEmptyResultSet_Integration verifies that when no stored vector clears
// the threshold, FindNearDups returns an empty (non-nil-error) candidate set —
// not an error, not a false positive. The single stored decision is orthogonal
// to the query.
func TestDedupEmptyResultSet_Integration(t *testing.T) {
	c, ctx := newIntegrationClient(t, "dedup-empty")

	if err := c.AddDecision(ctx, Decision{
		ID: uuid.New().String(), Text: "far away", Reason: "r", Status: "active", Author: "tester",
	}, unitVec(5)); err != nil {
		t.Fatalf("AddDecision: %v", err)
	}

	// Query orthogonal to the only stored vector with a high threshold.
	cands, err := c.FindNearDups(ctx, "decisions", unitVec(0), 0.9, 10)
	if err != nil {
		t.Fatalf("FindNearDups: %v", err)
	}
	if len(cands) != 0 {
		t.Fatalf("FindNearDups returned %d candidates, want 0 (none clear threshold): %+v", len(cands), cands)
	}
}

// TestDedupCollectionNotFound_Integration verifies FindNearDups treats a missing
// collection as no-duplicates (nil, nil) rather than erroring — the fresh-install
// path. We drop all collections so the target kind's collection is absent.
func TestDedupCollectionNotFound_Integration(t *testing.T) {
	c, ctx := newIntegrationClient(t, "dedup-missing")

	if err := c.DropAllCollections(ctx); err != nil {
		t.Fatalf("DropAllCollections: %v", err)
	}

	cands, err := c.FindNearDups(ctx, "decisions", unitVec(0), 0.5, 10)
	if err != nil {
		t.Fatalf("FindNearDups over missing collection should be graceful, got err: %v", err)
	}
	if cands != nil {
		t.Fatalf("FindNearDups over missing collection returned %v, want nil", cands)
	}
}

// TestDedupUnknownKind_Integration verifies kindMeta validation surfaces an error
// for an unsupported kind (negative path) without touching Qdrant.
func TestDedupUnknownKind_Integration(t *testing.T) {
	c, ctx := newIntegrationClient(t, "dedup-badkind")

	cands, err := c.FindNearDups(ctx, "not-a-kind", unitVec(0), 0.5, 10)
	if err == nil {
		t.Fatal("FindNearDups with unknown kind returned nil error, want validation error")
	}
	if cands != nil {
		t.Fatalf("FindNearDups on unknown kind returned %v, want nil candidates", cands)
	}
}
