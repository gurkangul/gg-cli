package store

import (
	"testing"

	"github.com/gurkangul/gg-cli/internal/brain"
)

// BUG-080 L1: human-prefixed IDs sort by numeric suffix, UUIDs lexically.
func TestBrainIDLess(t *testing.T) {
	if !brainIDLess("TASK-999", "TASK-1000") {
		t.Error("TASK-999 should sort before TASK-1000 (numeric, not lexical)")
	}
	if brainIDLess("TASK-1000", "TASK-999") {
		t.Error("TASK-1000 must not sort before TASK-999")
	}
	// Distinct prefixes / UUIDs fall back to lexical and must be deterministic.
	a, b := "aaaa-1111", "bbbb-2222"
	if brainIDLess(a, b) == brainIDLess(b, a) {
		t.Error("UUID compare must be a strict order")
	}
}

// BUG-080 L4: discussion seq bootstraps from JSONL when the vector store is unavailable.
func TestMaxDiscIDFromBrainJSONL(t *testing.T) {
	dir := t.TempDir()
	for _, id := range []string{"DISC-003", "DISC-011", "DISC-007"} {
		if err := brain.Append(dir, "discussions", "u-"+id, "tester", map[string]any{"disc_id": id}); err != nil {
			t.Fatalf("Append %s: %v", id, err)
		}
	}
	got, err := maxDiscIDFromBrainJSONL(dir)
	if err != nil {
		t.Fatalf("maxDiscIDFromBrainJSONL: %v", err)
	}
	if got != 11 {
		t.Fatalf("max disc id = %d, want 11", got)
	}
}
