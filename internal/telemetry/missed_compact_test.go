package telemetry

import "testing"

func TestMissedCompactByVerb_AggregatesPerVerb(t *testing.T) {
	// Entries must be agent-origin so MissedCompactByVerb counts them as misses.
	t.Setenv("GG_ROLE", "implementer")
	dir := t.TempDir()
	// "context": 1 compact (saved 800), 4 default — 4 missed × 800 = 3200 est
	RecordCompact(dir, "context", "", 200, 1000, 1, "")
	for i := 0; i < 4; i++ {
		Record(dir, "context", "")
	}
	// "search": 2 compact (saved 350+250=600 → avg 300), 1 default — 1 × 300 = 300 est
	RecordCompact(dir, "search", "", 150, 500, 1, "")
	RecordCompact(dir, "search", "", 250, 500, 1, "")
	Record(dir, "search", "")
	// "status": 0 compact — must NOT appear (no per-verb savings data)
	for i := 0; i < 3; i++ {
		Record(dir, "status", "")
	}
	sum, err := Summarize(dir)
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}

	rows := sum.MissedCompactByVerb(0)
	if len(rows) != 2 {
		t.Fatalf("rows len = %d, want 2 (status excluded — no compact baseline); got: %+v", len(rows), rows)
	}
	if rows[0].Verb != "context" {
		t.Errorf("rows[0].Verb = %q, want context (highest est. miss)", rows[0].Verb)
	}
	if rows[0].EstimatedBytesMissed != 3200 {
		t.Errorf("rows[0].EstimatedBytesMissed = %d, want 3200", rows[0].EstimatedBytesMissed)
	}
	if rows[0].MissedCalls != 4 || rows[0].TotalCalls != 5 || rows[0].CompactCalls != 1 {
		t.Errorf("rows[0] miss/total/compact = %d/%d/%d, want 4/5/1",
			rows[0].MissedCalls, rows[0].TotalCalls, rows[0].CompactCalls)
	}
	if rows[1].Verb != "search" {
		t.Errorf("rows[1].Verb = %q, want search", rows[1].Verb)
	}
	if rows[1].EstimatedBytesMissed != 300 {
		t.Errorf("rows[1].EstimatedBytesMissed = %d, want 300", rows[1].EstimatedBytesMissed)
	}
	if rows[1].AvgBytesSavedPerCall != 300 {
		t.Errorf("rows[1].AvgBytesSavedPerCall = %d, want 300", rows[1].AvgBytesSavedPerCall)
	}
}

func TestMissedCompactByVerb_BoundedByLimit(t *testing.T) {
	// Entries must be agent-origin so MissedCompactByVerb counts them as misses.
	t.Setenv("GG_ROLE", "implementer")
	dir := t.TempDir()
	// 5 verbs, each with 1 compact + N defaults so each appears in rows.
	for i, v := range []string{"a", "b", "c", "d", "e"} {
		RecordCompact(dir, v, "", 100, 100+(i+1)*100, 1, "") // saved = (i+1)*100
		for j := 0; j <= i; j++ {
			Record(dir, v, "")
		}
	}
	sum, err := Summarize(dir)
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}

	if got := sum.MissedCompactByVerb(3); len(got) != 3 {
		t.Errorf("limit=3 returned %d rows, want 3", len(got))
	}
	if got := sum.MissedCompactByVerb(0); len(got) != 5 {
		t.Errorf("limit=0 returned %d rows, want 5 (all)", len(got))
	}
	if got := sum.MissedCompactByVerb(100); len(got) != 5 {
		t.Errorf("limit=100 returned %d rows, want 5 (cap > rows)", len(got))
	}
}

func TestMissedCompactByVerb_ZeroData(t *testing.T) {
	dir := t.TempDir()
	if rows := (&WeeklySummary{}).MissedCompactByVerb(3); rows != nil {
		t.Errorf("nil-summary fallback returned %v, want nil", rows)
	}
	sum, err := Summarize(dir)
	if err != nil {
		t.Fatalf("Summarize empty: %v", err)
	}
	if rows := sum.MissedCompactByVerb(3); len(rows) != 0 {
		t.Errorf("empty telemetry returned %d rows, want 0", len(rows))
	}

	// No misses (all compact-eligible calls used compact) — must produce empty.
	RecordCompact(dir, "search", "", 150, 500, 1, "")
	sum2, err := Summarize(dir)
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if rows := sum2.MissedCompactByVerb(3); len(rows) != 0 {
		t.Errorf("100%% compact verb returned %d rows, want 0 (no missed)", len(rows))
	}
}
