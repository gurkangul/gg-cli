package telemetry

import (
	"os"
	"testing"
	"time"
)

// ── Session-level aggregation (TASK-286) ──────────────────────────────────────

func TestSummarizeSessions_MissingFile_ReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	ss, err := SummarizeSessions(dir, time.Now().UTC().AddDate(0, 0, -7))
	if err != nil {
		t.Fatalf("SummarizeSessions: %v", err)
	}
	if ss.ActiveSessions != 0 {
		t.Errorf("ActiveSessions = %d, want 0 for empty dir", ss.ActiveSessions)
	}
}

func TestSummarizeSessions_NoSessionID_IgnoresEntries(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_SESSION_ID", "")
	t.Setenv("GG_SESSION_ID", "")
	// Compact entries without a session ID must not be counted.
	RecordCompact(dir, "context", "", 200, 1000, 1, "")
	RecordCompact(dir, "search", "", 100, 500, 1, "")

	ss, err := SummarizeSessions(dir, time.Now().UTC().AddDate(0, 0, -7))
	if err != nil {
		t.Fatalf("SummarizeSessions: %v", err)
	}
	if ss.ActiveSessions != 0 {
		t.Errorf("ActiveSessions = %d, want 0 (no session IDs recorded)", ss.ActiveSessions)
	}
}

func TestSummarizeSessions_SingleSession_Aggregates(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_SESSION_ID", "sess-abc")

	RecordCompact(dir, "context", "", 200, 1000, 1, "")
	RecordCompact(dir, "search", "", 100, 500, 1, "")
	Record(dir, "status", "") // non-compact — must not appear in session totals

	ss, err := SummarizeSessions(dir, time.Now().UTC().AddDate(0, 0, -7))
	if err != nil {
		t.Fatalf("SummarizeSessions: %v", err)
	}
	if ss.ActiveSessions != 1 {
		t.Errorf("ActiveSessions = %d, want 1", ss.ActiveSessions)
	}
	if ss.AvgCompactCallsPerSession != 2.0 {
		t.Errorf("AvgCompactCallsPerSession = %.1f, want 2.0", ss.AvgCompactCallsPerSession)
	}
	// Single session: bytes_out=300 total; p50=p95=same value.
	wantKB := float64(300) / 1024.0
	if ss.P50CumulativeKB != wantKB {
		t.Errorf("P50CumulativeKB = %.4f, want %.4f", ss.P50CumulativeKB, wantKB)
	}
	if ss.P95CumulativeKB != wantKB {
		t.Errorf("P95CumulativeKB = %.4f, want %.4f", ss.P95CumulativeKB, wantKB)
	}
}

func TestSummarizeSessions_TwoSessions_PercentilesDistinct(t *testing.T) {
	dir := t.TempDir()

	// Four sessions with bytes [100, 200, 500, 2000] (sorted).
	// nearest-rank: p50 → ceil(0.50*4)-1 = ceil(2.0)-1 = 1 → 200 bytes
	//               p95 → ceil(0.95*4)-1 = ceil(3.8)-1 = 3 → 2000 bytes
	// wantP50 != wantP95, which catches p50/p95 index-swap bugs that a 2-element
	// dataset cannot expose (both values would still differ but in an arbitrary order).
	sessions := []struct {
		id    string
		bytes int
	}{
		{"sess-a", 100},
		{"sess-b", 200},
		{"sess-c", 500},
		{"sess-d", 2000},
	}
	for _, s := range sessions {
		t.Setenv("CLAUDE_SESSION_ID", s.id)
		RecordCompact(dir, "context", "", s.bytes, s.bytes*5, 1, "")
	}

	ss, err := SummarizeSessions(dir, time.Now().UTC().AddDate(0, 0, -7))
	if err != nil {
		t.Fatalf("SummarizeSessions: %v", err)
	}
	if ss.ActiveSessions != 4 {
		t.Errorf("ActiveSessions = %d, want 4", ss.ActiveSessions)
	}
	wantP50 := float64(200) / 1024.0
	wantP95 := float64(2000) / 1024.0
	if ss.P50CumulativeKB != wantP50 {
		t.Errorf("P50CumulativeKB = %.4f, want %.4f", ss.P50CumulativeKB, wantP50)
	}
	if ss.P95CumulativeKB != wantP95 {
		t.Errorf("P95CumulativeKB = %.4f, want %.4f", ss.P95CumulativeKB, wantP95)
	}
	if wantP50 == wantP95 {
		t.Fatal("test invariant violated: wantP50 must != wantP95 to catch swap bugs")
	}
	// avg calls: (1+1+1+1)/4 = 1.0
	if ss.AvgCompactCallsPerSession != 1.0 {
		t.Errorf("AvgCompactCallsPerSession = %.1f, want 1.0", ss.AvgCompactCallsPerSession)
	}
}

func TestSummarizeSessions_OverThreshold(t *testing.T) {
	dir := t.TempDir()

	// Session A: below 100 KB (1000 bytes)
	t.Setenv("CLAUDE_SESSION_ID", "sess-small")
	RecordCompact(dir, "context", "", 1000, 5000, 1, "")

	// Session B: above 100 KB
	t.Setenv("CLAUDE_SESSION_ID", "sess-big")
	RecordCompact(dir, "search", "", sessionThresholdBytes+1, 200000, 1, "")

	ss, err := SummarizeSessions(dir, time.Now().UTC().AddDate(0, 0, -7))
	if err != nil {
		t.Fatalf("SummarizeSessions: %v", err)
	}
	if ss.ActiveSessions != 2 {
		t.Errorf("ActiveSessions = %d, want 2", ss.ActiveSessions)
	}
	if ss.OverThresholdCount != 1 {
		t.Errorf("OverThresholdCount = %d, want 1 (only sess-big crosses 100 KB)", ss.OverThresholdCount)
	}
}

func TestSummarizeSessions_GGSessionIDFallback(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_SESSION_ID", "")
	t.Setenv("GG_SESSION_ID", "gg-sess-xyz")

	RecordCompact(dir, "context", "", 300, 900, 1, "")

	ss, err := SummarizeSessions(dir, time.Now().UTC().AddDate(0, 0, -7))
	if err != nil {
		t.Fatalf("SummarizeSessions: %v", err)
	}
	if ss.ActiveSessions != 1 {
		t.Errorf("ActiveSessions = %d, want 1 (GG_SESSION_ID fallback should be picked up)", ss.ActiveSessions)
	}
}

func TestSummarizeSessions_OldEntriesExcluded(t *testing.T) {
	dir := t.TempDir()

	// Write a stale compact entry (>7 days ago) directly with session_id.
	oldEntry := `{"verb":"context","origin":"agent","ts":"` +
		time.Now().AddDate(0, 0, -8).UTC().Format(time.RFC3339) +
		`","compact":true,"bytes_out":5000,"bytes_default":10000,"renderer_v":1,"session_id":"sess-old"}` + "\n"
	if err := os.WriteFile(filePath(dir), []byte(oldEntry), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Write a recent entry in a different session.
	t.Setenv("CLAUDE_SESSION_ID", "sess-new")
	RecordCompact(dir, "context", "", 200, 500, 1, "")

	ss, err := SummarizeSessions(dir, time.Now().UTC().AddDate(0, 0, -7))
	if err != nil {
		t.Fatalf("SummarizeSessions: %v", err)
	}
	if ss.ActiveSessions != 1 {
		t.Errorf("ActiveSessions = %d, want 1 (old session entry should be excluded)", ss.ActiveSessions)
	}
}

func TestRecord_SessionID_OmittedWhenEmpty(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_SESSION_ID", "")
	t.Setenv("GG_SESSION_ID", "")
	Record(dir, "status", "")

	data, err := os.ReadFile(filePath(dir))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if containsField(string(data), `"session_id"`) {
		t.Errorf("session_id should be omitted when no session env var is set: %s", data)
	}
}

func TestRecord_SessionID_RecordedWhenSet(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_SESSION_ID", "test-session-123")

	Record(dir, "status", "")

	data, err := os.ReadFile(filePath(dir))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !containsField(string(data), `"session_id"`) {
		t.Errorf("session_id should be present when CLAUDE_SESSION_ID is set: %s", data)
	}
	if !containsField(string(data), `"test-session-123"`) {
		t.Errorf("session_id value should be test-session-123: %s", data)
	}
}

func TestSortInts_Ascending(t *testing.T) {
	a := []int{5, 2, 8, 1, 9, 3}
	sortInts(a)
	for i := 1; i < len(a); i++ {
		if a[i] < a[i-1] {
			t.Errorf("sortInts result not sorted at index %d: %v", i, a)
		}
	}
}

func TestP50P95Idx_SingleElement(t *testing.T) {
	if p50idx(1) != 0 {
		t.Errorf("p50idx(1) = %d, want 0", p50idx(1))
	}
	if p95idx(1) != 0 {
		t.Errorf("p95idx(1) = %d, want 0", p95idx(1))
	}
}

func TestP50P95Idx_TwoElements(t *testing.T) {
	// nearest-rank: p50 → ceil(0.50*2)-1 = ceil(1.0)-1 = 0; p95 → ceil(0.95*2)-1 = ceil(1.9)-1 = 1.
	if p50idx(2) != 0 {
		t.Errorf("p50idx(2) = %d, want 0", p50idx(2))
	}
	if p95idx(2) != 1 {
		t.Errorf("p95idx(2) = %d, want 1", p95idx(2))
	}
}

func TestP50P95Idx_TwentyElements(t *testing.T) {
	// nearest-rank: p50 → ceil(0.50*20)-1 = ceil(10)-1 = 9; p95 → ceil(0.95*20)-1 = ceil(19)-1 = 18.
	if p50idx(20) != 9 {
		t.Errorf("p50idx(20) = %d, want 9", p50idx(20))
	}
	if p95idx(20) != 18 {
		t.Errorf("p95idx(20) = %d, want 18", p95idx(20))
	}
}
