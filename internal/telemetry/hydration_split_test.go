package telemetry

import (
	"os"
	"testing"
	"time"
)

// Honest hydration split aggregates (TASK-491). These tests are split out of
// telemetry_test.go to keep that file under the 800-line test cap.

// TestRecordHydration_HonestSplit is the load-bearing TASK-491 behavior matrix:
// a fixture mixing (a) a human full-read, (b) an agent gate-MANDATED --full read,
// and (c) an agent DISCRETIONARY re-fetch must surface as:
//
//	HydrationCalls               = 3 (all three, unchanged total)
//	AgentHydrationCalls          = 2 (human excluded — mirrors TASK-490)
//	AgentMandatedHydrationCalls  = 1 (the --full gate read)
//	AgentDiscretionaryHydration  = 1 (the only drop-list-risk signal)
//	MandatedHydrationBytesTotal  = bytes of the mandated read only
func TestRecordHydration_HonestSplit(t *testing.T) {
	dir := t.TempDir()
	// (a) human full read — origin human requires NO agent env. Clear GG_ROLE/
	// GG_AGENT explicitly so the test is robust when run under an agent-tagged
	// shell (e.g. the pre-task-done verify gate exports GG_ROLE); otherwise the
	// ambient env would misclassify this entry as agent.
	t.Setenv("GG_ROLE", "")
	t.Setenv("GG_AGENT", "")
	RecordHydration(dir, "get", "", 400, false)
	// (b) + (c) are agent-origin (GG_ROLE set).
	t.Setenv("GG_ROLE", "developer")
	RecordHydration(dir, "get", "", 500, true)  // mandated gate --full read
	RecordHydration(dir, "get", "", 300, false) // discretionary re-fetch

	sum, err := Summarize(dir)
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if sum.HydrationCalls != 3 {
		t.Errorf("HydrationCalls = %d, want 3", sum.HydrationCalls)
	}
	if sum.AgentHydrationCalls != 2 {
		t.Errorf("AgentHydrationCalls = %d, want 2 (human excluded)", sum.AgentHydrationCalls)
	}
	if sum.AgentMandatedHydrationCalls != 1 {
		t.Errorf("AgentMandatedHydrationCalls = %d, want 1", sum.AgentMandatedHydrationCalls)
	}
	if sum.AgentDiscretionaryHydration != 1 {
		t.Errorf("AgentDiscretionaryHydration = %d, want 1", sum.AgentDiscretionaryHydration)
	}
	if sum.MandatedHydrationBytesTotal != 500 {
		t.Errorf("MandatedHydrationBytesTotal = %d, want 500", sum.MandatedHydrationBytesTotal)
	}
}

// TestRecordHydration_AllMandatedNoDiscretionary is the NEGATIVE-path guard: a
// high TOTAL re-fetch rate that is entirely gate-mandated (no discretionary
// reads) must leave AgentDiscretionaryHydration at 0, so the drop-list warning
// (which the caller drives off discretionary only) never fires falsely.
func TestRecordHydration_AllMandatedNoDiscretionary(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GG_ROLE", "developer")
	for i := 0; i < 5; i++ {
		RecordHydration(dir, "get", "", 500, true) // all gate-mandated
	}
	sum, err := Summarize(dir)
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if sum.AgentMandatedHydrationCalls != 5 {
		t.Errorf("AgentMandatedHydrationCalls = %d, want 5", sum.AgentMandatedHydrationCalls)
	}
	if sum.AgentDiscretionaryHydration != 0 {
		t.Errorf("AgentDiscretionaryHydration = %d, want 0 (no false drop-list signal)", sum.AgentDiscretionaryHydration)
	}
}

// TestRecordHydration_LegacyEntryDefaultsDiscretionary proves the additive-JSON
// contract (AC-2): an old telemetry line WITHOUT a "mandated" field decodes as
// mandated=false, i.e. counted as discretionary (conservative pre-TASK-491).
func TestRecordHydration_LegacyEntryDefaultsDiscretionary(t *testing.T) {
	dir := t.TempDir()
	ts := time.Now().UTC().Format(time.RFC3339)
	// Hand-write a legacy agent-origin hydration entry with no mandated field.
	legacy := `{"verb":"get","origin":"agent","ts":"` + ts + `","hydration":true,"bytes_hydrated":250}`
	if err := os.WriteFile(filePath(dir), []byte(legacy+"\n"), 0o600); err != nil {
		t.Fatalf("write legacy: %v", err)
	}
	sum, err := Summarize(dir)
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if sum.AgentMandatedHydrationCalls != 0 {
		t.Errorf("legacy entry counted as mandated: AgentMandatedHydrationCalls = %d, want 0", sum.AgentMandatedHydrationCalls)
	}
	if sum.AgentDiscretionaryHydration != 1 {
		t.Errorf("legacy entry should be discretionary: AgentDiscretionaryHydration = %d, want 1", sum.AgentDiscretionaryHydration)
	}
}
