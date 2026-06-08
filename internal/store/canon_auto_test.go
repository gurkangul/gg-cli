package store

import (
	"fmt"
	"strings"
	"testing"
)

func TestBuildAutoCanon_FiltersNoiseDedupsKeepsImportant(t *testing.T) {
	decs := []Decision{
		{ID: "1", Text: "bypass rationale: TASK-468 shipped", Status: "active", CreatedAt: "2026-06-07"},
		{ID: "2", Text: "bypass rationale: TASK-468 shipped", Status: "active", CreatedAt: "2026-06-07"},
		{ID: "3", Text: "JSONL is source of truth; Qdrant is derived", Tags: []string{"architecture"}, Status: "active", CreatedAt: "2024-01-01"},
		{ID: "4", Text: "Use cobra for the CLI", Status: "active", CreatedAt: "2026-06-01"},
		{ID: "5", Text: "Use cobra for the CLI", Status: "active", CreatedAt: "2026-06-02"},
		{ID: "6", Text: "an old superseded thing", Status: "superseded", CreatedAt: "2026-06-03"},
		{ID: "7", Text: "Pinned invariant that is ancient", Pinned: true, Status: "active", CreatedAt: "2023-01-01"},
	}
	rejs := []Rejection{
		{ID: "r1", Approach: "add a background daemon", Reason: "violates no-daemon", CreatedAt: "2026-01-01"},
		{ID: "r2", Approach: "add a background daemon", CreatedAt: "2026-01-02"},
	}
	bugs := []Bug{
		{ID: "BUG-1", Title: "search broke", RootCause: "is_null filter matched nothing", Status: "fixed", CreatedAt: "2026-05-01"},
		{ID: "BUG-2", Title: "bug with no root cause", Status: "fixed", CreatedAt: "2026-05-02"},
	}

	var all strings.Builder
	for _, e := range BuildAutoCanon(decs, rejs, bugs, nil) {
		all.WriteString(e.Area + "\n" + e.Text + "\n")
	}
	got := all.String()

	checks := []struct {
		cond bool
		msg  string
	}{
		{strings.Contains(got, "bypass rationale"), "bypass noise must be filtered out"},
		{strings.Count(got, "Use cobra for the CLI") != 1, "duplicate decision must collapse to one"},
		{!strings.Contains(got, "JSONL is source of truth"), "architecture-tagged old decision must be kept"},
		{!strings.Contains(got, "Pinned invariant that is ancient"), "pinned old decision must be kept"},
		{strings.Contains(got, "superseded thing"), "non-active decision must be excluded"},
		{strings.Count(got, "add a background daemon") != 1, "duplicate rejection must collapse to one"},
		{!strings.Contains(got, "is_null filter matched nothing"), "fixed-bug root cause must appear"},
		{strings.Contains(got, "no root cause"), "bug without root cause must be skipped"},
	}
	for _, c := range checks {
		if c.cond {
			t.Errorf("%s\n--- canon ---\n%s", c.msg, got)
		}
	}
}

func TestBuildAutoCanon_CapsRoutineKeepsImportant(t *testing.T) {
	var decs []Decision
	for i := 0; i < 20; i++ {
		decs = append(decs, Decision{
			ID:        fmt.Sprintf("d%02d", i),
			Text:      fmt.Sprintf("routine decision number %02d", i),
			Status:    "active",
			CreatedAt: fmt.Sprintf("2026-06-%02d", i+1),
		})
	}
	decs = append(decs, Decision{ID: "imp", Text: "ancient architecture rule", Tags: []string{"architecture"}, Status: "active", CreatedAt: "2000-01-01"})

	entries := BuildAutoCanon(decs, nil, nil, nil)
	if len(entries) == 0 {
		t.Fatal("expected a key-decisions entry")
	}
	text := entries[0].Text
	bullets := strings.Count(text, "\n") + 1
	if bullets > autoCanonDecisionCap+1 { // +1 for the always-kept important one
		t.Errorf("routine decisions not capped: got %d bullets (cap %d)", bullets, autoCanonDecisionCap)
	}
	if !strings.Contains(text, "ancient architecture rule") {
		t.Errorf("important old decision was capped away:\n%s", text)
	}
}

func TestBuildAutoCanonCompact_HardCapsDecisions(t *testing.T) {
	var decs []Decision
	// 6 important (architecture-tagged) + 6 routine = 12 candidates; compact caps at 8.
	for i := 0; i < 6; i++ {
		decs = append(decs, Decision{ID: fmt.Sprintf("imp%d", i), Text: fmt.Sprintf("architecture rule %d", i), Tags: []string{"architecture"}, Status: "active", CreatedAt: fmt.Sprintf("2026-01-%02d", i+1)})
	}
	for i := 0; i < 6; i++ {
		decs = append(decs, Decision{ID: fmt.Sprintf("rt%d", i), Text: fmt.Sprintf("routine rule %d", i), Status: "active", CreatedAt: fmt.Sprintf("2026-02-%02d", i+1)})
	}
	entries := BuildAutoCanonCompact(decs, nil, nil, nil)
	if len(entries) == 0 {
		t.Fatal("expected key-decisions")
	}
	bullets := strings.Count(entries[0].Text, "\n") + 1
	if bullets > autoCanonCompactDecisions {
		t.Errorf("compact must hard-cap decisions at %d, got %d", autoCanonCompactDecisions, bullets)
	}
	if !strings.Contains(entries[0].Text, "architecture rule") {
		t.Error("important decisions must be prioritized under the hard cap")
	}
}

func TestIsLowSignal_DropsReleaseNotes(t *testing.T) {
	in := []Decision{
		{ID: "1", Text: "Release v0.3.25 shipped and synced across registered projects"},
		{ID: "2", Text: "JSONL is the source of truth"},
	}
	out := FilterDecisionNoise(in)
	if len(out) != 1 || out[0].Text != "JSONL is the source of truth" {
		t.Fatalf("release-note must be filtered, got %+v", out)
	}
}

func TestBuildAutoCanon_ReferenceDegreeSurfacesImportant(t *testing.T) {
	var decs []Decision
	for i := 0; i < 20; i++ { // recent routine noise to exhaust the cap
		decs = append(decs, Decision{ID: fmt.Sprintf("r%02d", i), Text: fmt.Sprintf("recent routine %02d", i), Status: "active", CreatedAt: fmt.Sprintf("2026-06-%02d", i+1)})
	}
	// Old, unpinned, untagged decision on a heavily-referenced ("hot") task.
	decs = append(decs, Decision{ID: "imp", Text: "old central decision on a hot task", Status: "active", TaskID: "TASK-1", CreatedAt: "2000-01-01"})
	decs = append(decs, Decision{ID: "x1", Text: "another decision on task one", Status: "active", TaskID: "TASK-1", CreatedAt: "2026-01-01"})
	bugs := []Bug{{ID: "BUG-1", Title: "b", TaskID: "TASK-1", Status: "fixed"}}
	tasks := []Task{{ID: "TASK-2", DependsOn: []string{"TASK-1"}}}
	// TASK-1 reference degree = 2 decisions + 1 bug + 1 dependent = 4 (>= threshold 3).

	var all strings.Builder
	for _, e := range BuildAutoCanon(decs, nil, bugs, tasks) {
		all.WriteString(e.Text + "\n")
	}
	if !strings.Contains(all.String(), "old central decision on a hot task") {
		t.Errorf("reference-degree decision should auto-surface as important:\n%s", all.String())
	}
}

func TestFilterDecisionNoise(t *testing.T) {
	in := []Decision{
		{ID: "1", Text: "bypass rationale: foo"},
		{ID: "2", Text: "a real decision"},
		{ID: "3", Text: "a real decision"},
		{ID: "4", Text: "tagged bypass", Tags: []string{"bypass"}},
	}
	out := FilterDecisionNoise(in)
	if len(out) != 1 || out[0].Text != "a real decision" {
		t.Fatalf("expected only [a real decision], got %+v", out)
	}
}
