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
	for _, e := range BuildAutoCanon(decs, rejs, bugs) {
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

	entries := BuildAutoCanon(decs, nil, nil)
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
