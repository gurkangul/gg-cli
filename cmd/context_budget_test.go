package cmd

import (
	"strings"
	"testing"

	"github.com/gurkangul/gg-cli/internal/store"
)

// makeBundle builds a contextBundle with one item of each category.
func makeBundle() contextBundle {
	return contextBundle{
		decisions: []store.Decision{
			{ID: "dec-1", Text: "Use JWT for auth", Status: "active", CreatedAt: "2026-04-01T00:00:00Z"},
			{ID: "dec-2", Text: "Old approach", Status: "superseded", CreatedAt: "2026-01-01T00:00:00Z"},
		},
		tasks: []store.Task{
			{ID: "TASK-001", Title: "Fix login bug", Status: "in_progress", Priority: "high"},
			{ID: "TASK-002", Title: "Add tests", Status: "pending", Priority: "medium"},
			{ID: "TASK-003", Title: "Deploy v2", Status: "done", Priority: "low"},
		},
		rejections: []store.Rejection{
			{ID: "rej-1", Approach: "Use sessions instead", CreatedAt: "2026-04-10T00:00:00Z"},
		},
		discussions: []store.Discussion{
			{ID: "DISC-001", Topic: "DB choice", Status: "open"},
			{ID: "DISC-002", Topic: "Old debate", Status: "resolved"},
			{ID: "DISC-003", Topic: "Dropped idea", Status: "dismissed"},
		},
		notes: []store.Note{
			{ID: "note-1", Text: "Remember to update docs", CreatedAt: "2026-04-15T00:00:00Z"},
		},
	}
}

func TestBuildTieredItems_TierAssignment(t *testing.T) {
	bundle := makeBundle()
	items := buildTieredItems("", bundle)

	// Count tiers
	tierCount := make(map[int]int)
	for _, it := range items {
		tierCount[it.tier]++
	}

	// P1: active decision + in_progress task = 2
	if got := tierCount[1]; got != 2 {
		t.Errorf("P1 count = %d, want 2", got)
	}
	// P2: rejection + pending task + open discussion = 3
	if got := tierCount[2]; got != 3 {
		t.Errorf("P2 count = %d, want 3", got)
	}
	// P3: superseded decision + done task + resolved discussion = 3
	if got := tierCount[3]; got != 3 {
		t.Errorf("P3 count = %d, want 3", got)
	}
	// P4: note + dismissed discussion = 2
	if got := tierCount[4]; got != 2 {
		t.Errorf("P4 count = %d, want 2", got)
	}
}

func TestBuildTieredItems_SortOrder(t *testing.T) {
	bundle := makeBundle()
	items := buildTieredItems("", bundle)

	// Verify tiers are non-decreasing (sorted P1 first)
	for i := 1; i < len(items); i++ {
		if items[i].tier < items[i-1].tier {
			t.Errorf("tier order broken at index %d: %d < %d", i, items[i].tier, items[i-1].tier)
		}
	}
}

func TestRenderContextBudget_P1AlwaysFirst(t *testing.T) {
	bundle := makeBundle()
	// Compute cost of P1 items only to set a tight budget
	summary := tierBudgetSummary(bundle)
	budget := summary[1] + 1 // just enough for P1, not P2

	out := renderBudgetToString("auth", bundle, budget)

	// P1 items should appear
	if !strings.Contains(out, "Use JWT for auth") {
		t.Error("P1 decision missing from budget output")
	}
	if !strings.Contains(out, "TASK-001") {
		t.Error("P1 in-progress task missing from budget output")
	}
	// P2 items should be dropped or budget exhausted
	if strings.Contains(out, "TASK-002") {
		t.Error("P2 pending task should be dropped under tight budget")
	}
}

func TestRenderContextBudget_ZeroBudgetDropsAll(t *testing.T) {
	bundle := makeBundle()
	// budget = 1 token — nothing fits except maybe the smallest item
	// Just verify the header appears and no panic
	out := renderBudgetToString("auth", bundle, 1)
	if !strings.Contains(out, "context:") {
		t.Error("header missing from budget output with tiny budget")
	}
}

func TestRenderContextBudget_LargeBudgetIncludesAll(t *testing.T) {
	bundle := makeBundle()
	out := renderBudgetToString("auth", bundle, 100000)

	// All items should appear
	for _, want := range []string{"Use JWT for auth", "TASK-001", "TASK-002", "TASK-003",
		"Use sessions instead", "DISC-001", "DISC-002", "DISC-003", "Remember to update docs"} {
		if !strings.Contains(out, want) {
			t.Errorf("large budget: %q missing from output", want)
		}
	}
}

func TestRenderContextBudget_DroppedCountInHeader(t *testing.T) {
	bundle := makeBundle()
	// Tiny budget — most items dropped
	out := renderBudgetToString("auth", bundle, 5)
	if !strings.Contains(out, "dropped") {
		t.Error("header should mention dropped count when items are dropped")
	}
}

func TestRenderContextBudget_NoDrop_NoDroppedInHeader(t *testing.T) {
	bundle := makeBundle()
	out := renderBudgetToString("auth", bundle, 100000)
	if strings.Contains(out, "dropped") {
		t.Error("header should not mention dropped when nothing is dropped")
	}
}

func TestApproxTokens(t *testing.T) {
	cases := []struct {
		s    string
		want int
	}{
		{"abcd", 1},
		{"abcdefgh", 2},
		{"", 1}, // minimum 1
	}
	for _, c := range cases {
		if got := approxTokens(c.s); got != c.want {
			t.Errorf("approxTokens(%q) = %d, want %d", c.s, got, c.want)
		}
	}
}

func TestBuildTieredItems_LineHasTierPrefix(t *testing.T) {
	bundle := makeBundle()
	items := buildTieredItems("", bundle)
	for _, item := range items {
		prefix := "[P" + string(rune('0'+item.tier)) + "]"
		if !strings.HasPrefix(item.line, prefix) {
			t.Errorf("item line %q missing prefix %s", item.line, prefix)
		}
	}
}
