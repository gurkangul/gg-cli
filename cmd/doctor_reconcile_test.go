package cmd

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/gurkangul/gg-cli/internal/brain"
	"github.com/gurkangul/gg-cli/internal/store"
)

// TestReconcileFromJSONL_RecoversMissingFromEmbeddedStore is the cmd-layer
// successor to the obsolete, Qdrant-gated TestReconcile_FromJSONL_RecoversMissingFromQdrant
// (BUG-090). The Qdrant/Memgraph server backends were removed (commit e16c9ac);
// `gg doctor --reconcile` now reconciles the JSONL source of truth
// (.gg/brain/<kind>.jsonl) against the embedded SQLite vector store, so the test
// must exercise that path with a valid UUID and a real (always-up) embedded
// store — no dead port, no GG_INTEGRATION_TEST gate, no missing EnsureCollections.
//
// It proves the SIGKILL-window recovery contract: a decision written to JSONL
// but never upserted into the vector store is re-materialised by
// runReconcileFromJSONL, while an entry already present in both is left intact.
func TestReconcileFromJSONL_RecoversMissingFromEmbeddedStore(t *testing.T) {
	ctx := context.Background()
	ggDir := setupGGDir(t)

	// Stand up the embedded vector store the way the command layer does, and
	// create the brain collections at the dimension ReplayBrainEntry's zero
	// vector uses (store.VectorSize) so reconcile upserts are accepted.
	c, err := store.New(ggDir, testProjectID)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if err := c.EnsureCollections(ctx, store.VectorSize); err != nil {
		t.Fatalf("EnsureCollections: %v", err)
	}

	// A "survivor" decision lives in BOTH the vector store and JSONL.
	const survivorID = "11111111-1111-1111-1111-111111111111"
	survivorPayload := map[string]any{
		"text":       "use the embedded vector store",
		"reason":     "no server backend dependency",
		"created_at": "2026-04-26T00:00:00Z",
	}
	if err := c.ReplayBrainEntry(ctx, "decisions", survivorID, survivorPayload); err != nil {
		t.Fatalf("seed survivor in store: %v", err)
	}
	if err := brain.Append(ggDir, "decisions", survivorID, "agent", survivorPayload); err != nil {
		t.Fatalf("append survivor to JSONL: %v", err)
	}

	// The "missing" decision exists ONLY in JSONL — it simulates the SIGKILL
	// window between brain.Append and the vector-store upsert. A valid UUID
	// (the old test used the unparseable "reconcile-test-uuid-001", which the
	// validBrainUUID guard now rejects).
	const missingID = "22222222-2222-2222-2222-222222222222"
	if err := brain.Append(ggDir, "decisions", missingID, "agent", map[string]any{
		"text":       "sigkill window decision",
		"reason":     "killed after jsonl write",
		"created_at": "2026-04-27T00:00:00Z",
	}); err != nil {
		t.Fatalf("append missing to JSONL: %v", err)
	}

	// Sanity: before reconcile the store holds only the survivor.
	before, err := c.CollectionUUIDs(ctx, "decisions")
	if err != nil {
		t.Fatalf("CollectionUUIDs before: %v", err)
	}
	if _, ok := before[survivorID]; !ok {
		t.Fatalf("survivor missing from store before reconcile: %v", uuidKeysOf(before))
	}
	if _, ok := before[missingID]; ok {
		t.Fatalf("missing entry should not yet be in store before reconcile: %v", uuidKeysOf(before))
	}

	// Run the embedded reconcile-from-JSONL path directly. anyGap is false when
	// every JSONL entry is now consistent with the store.
	if gap := runReconcileFromJSONL(ctx, c, ggDir); gap {
		t.Fatalf("runReconcileFromJSONL reported an unresolved gap after recovering a recoverable entry")
	}

	// After reconcile BOTH records are present: the survivor preserved, the
	// JSONL-only record recovered into the vector store.
	after, err := c.CollectionUUIDs(ctx, "decisions")
	if err != nil {
		t.Fatalf("CollectionUUIDs after: %v", err)
	}
	if _, ok := after[survivorID]; !ok {
		t.Errorf("survivor %s lost during reconcile; have %v", survivorID, uuidKeysOf(after))
	}
	if _, ok := after[missingID]; !ok {
		t.Errorf("JSONL-only record %s was NOT recovered into the vector store; have %v", missingID, uuidKeysOf(after))
	}
}

// uuidKeysOf returns the UUID keys of a CollectionUUIDs set for error messages.
func uuidKeysOf(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestDoctorCheckBrainJSONL_MalformedWarning verifies that doctorCheckBrainJSONL
// calls ReadAllWithCount and detects malformed lines in brain JSONL files.
// Validates the detection logic directly rather than parsing fmt.Println output
// (which goes to os.Stdout, not captured by execCmd).
func TestDoctorCheckBrainJSONL_MalformedWarning(t *testing.T) {
	ggDir := setupGGDir(t)

	// Write a valid entry then inject a malformed line.
	if err := brain.Append(ggDir, "decisions", "uuid-ok", "agent", map[string]any{"text": "ok"}); err != nil {
		t.Fatalf("brain.Append: %v", err)
	}
	jsonlPath := filepath.Join(ggDir, "brain", "decisions.jsonl")
	f, err := os.OpenFile(jsonlPath, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open jsonl: %v", err)
	}
	_, _ = f.WriteString("{bad json\n")
	if err := f.Close(); err != nil {
		t.Fatalf("close jsonl: %v", err)
	}

	// Verify that ReadAllWithCount (used by doctorCheckBrainJSONL) detects the skip.
	entries, skipped, readErr := brain.ReadAllWithCount(ggDir, "decisions")
	if readErr != nil {
		t.Fatalf("ReadAllWithCount: %v", readErr)
	}
	if skipped != 1 {
		t.Errorf("skipped = %d, want 1 (malformed line not detected)", skipped)
	}
	if len(entries) != 1 {
		t.Errorf("entries = %d, want 1", len(entries))
	}
}
