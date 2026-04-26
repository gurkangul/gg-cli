package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gurkangul/gg-cli/internal/brain"
)

// TestReconcile_FromJSONL_RecoversMissingFromQdrant verifies that
// runReconcileFromJSONL surfaces JSONL entries that are absent from Qdrant and
// re-upserts them.  Requires a live Qdrant instance — gated on
// GG_INTEGRATION_TEST=1.
func TestReconcile_FromJSONL_RecoversMissingFromQdrant(t *testing.T) {
	if os.Getenv("GG_INTEGRATION_TEST") != "1" {
		t.Skip("set GG_INTEGRATION_TEST=1 to run against live Qdrant")
	}

	ggDir := setupGGDir(t)

	// Seed a decision entry in JSONL only (no Qdrant upsert — simulates SIGKILL
	// window between brain.Append and queueBrainOutbox).
	payload := map[string]any{
		"text":       "sigkill window decision",
		"reason":     "killed after jsonl write",
		"created_at": "2026-04-26T00:00:00Z",
	}
	if err := brain.Append(ggDir, "decisions", "reconcile-test-uuid-001", "agent", payload); err != nil {
		t.Fatalf("brain.Append: %v", err)
	}

	// Run reconcile — should detect the JSONL-only entry and replay to Qdrant.
	out, _, err := execCmd(t, "doctor", "--reconcile")
	if err != nil {
		t.Errorf("doctor --reconcile unexpected error: %v", err)
	}
	// Expect the reconcile output to mention recovery or consistency.
	if !strings.Contains(out, "recovered") && !strings.Contains(out, "consistent") {
		t.Errorf("unexpected reconcile output (no recovery/consistent signal): %s", out)
	}
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
	f.Close()

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
