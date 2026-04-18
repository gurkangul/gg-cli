// Package store — unit tests for pure helper functions that do not require
// a running Qdrant instance. These cover ID parsers, payload deserializers,
// and sorting helpers.
package store

import (
	"testing"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/qdrant/go-client/qdrant"
)

// ── ParseTaskID ────────────────────────────────────────────────────────────

func TestParseTaskID_Valid(t *testing.T) {
	cases := []struct {
		id   string
		want int
	}{
		{"TASK-001", 1},
		{"TASK-042", 42},
		{"TASK-999", 999},
		{"TASK-1000", 1000},
	}
	for _, tc := range cases {
		got, err := ParseTaskID(tc.id)
		if err != nil {
			t.Errorf("ParseTaskID(%q): unexpected error: %v", tc.id, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ParseTaskID(%q): got %d, want %d", tc.id, got, tc.want)
		}
	}
}

func TestParseTaskID_Invalid(t *testing.T) {
	bad := []string{"", "task-001", "TASK-", "TASK-abc", "001", "TASK001", "BUG-001"}
	for _, id := range bad {
		if _, err := ParseTaskID(id); err == nil {
			t.Errorf("ParseTaskID(%q): expected error, got nil", id)
		}
	}
}

// ── ParseBugID ────────────────────────────────────────────────────────────

func TestParseBugID_Valid(t *testing.T) {
	got, err := ParseBugID("BUG-007")
	if err != nil {
		t.Fatalf("ParseBugID: %v", err)
	}
	if got != 7 {
		t.Errorf("want 7, got %d", got)
	}
}

func TestParseBugID_Invalid(t *testing.T) {
	bad := []string{"", "bug-001", "BUG-", "BUG-abc", "TASK-001"}
	for _, id := range bad {
		if _, err := ParseBugID(id); err == nil {
			t.Errorf("ParseBugID(%q): expected error, got nil", id)
		}
	}
}

// ── ParseDiscID ───────────────────────────────────────────────────────────

func TestParseDiscID_Valid(t *testing.T) {
	got, err := ParseDiscID("DISC-003")
	if err != nil {
		t.Fatalf("ParseDiscID: %v", err)
	}
	if got != 3 {
		t.Errorf("want 3, got %d", got)
	}
}

func TestParseDiscID_Invalid(t *testing.T) {
	bad := []string{"", "disc-001", "DISC-", "TASK-001", "BUG-001"}
	for _, id := range bad {
		if _, err := ParseDiscID(id); err == nil {
			t.Errorf("ParseDiscID(%q): expected error, got nil", id)
		}
	}
}

// ── toAnySlice ────────────────────────────────────────────────────────────

func TestToAnySlice_Nil(t *testing.T) {
	if got := toAnySlice(nil); got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

func TestToAnySlice_NonNil(t *testing.T) {
	in := []string{"a", "b", "c"}
	got := toAnySlice(in)
	if len(got) != len(in) {
		t.Fatalf("len: got %d, want %d", len(got), len(in))
	}
	for i, v := range got {
		s, ok := v.(string)
		if !ok {
			t.Errorf("[%d]: not a string: %T", i, v)
			continue
		}
		if s != in[i] {
			t.Errorf("[%d]: got %q, want %q", i, s, in[i])
		}
	}
}

// ── extractStringList ─────────────────────────────────────────────────────

func TestExtractStringList_Nil(t *testing.T) {
	if got := extractStringList(nil); got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

func TestExtractStringList_Values(t *testing.T) {
	v := &qdrant.Value{
		Kind: &qdrant.Value_ListValue{
			ListValue: &qdrant.ListValue{
				Values: []*qdrant.Value{
					{Kind: &qdrant.Value_StringValue{StringValue: "alpha"}},
					{Kind: &qdrant.Value_StringValue{StringValue: "beta"}},
				},
			},
		},
	}
	got := extractStringList(v)
	if len(got) != 2 || got[0] != "alpha" || got[1] != "beta" {
		t.Errorf("extractStringList: got %v", got)
	}
}

// ── sortDecisionsDesc ─────────────────────────────────────────────────────

func TestSortDecisionsDesc(t *testing.T) {
	ds := []Decision{
		{ID: "a", CreatedAt: "2024-01-01T00:00:00Z"},
		{ID: "b", CreatedAt: "2024-03-01T00:00:00Z"},
		{ID: "c", CreatedAt: "2024-02-01T00:00:00Z"},
	}
	sortDecisionsDesc(ds)
	want := []string{"b", "c", "a"}
	for i, d := range ds {
		if d.ID != want[i] {
			t.Errorf("[%d]: got %q, want %q", i, d.ID, want[i])
		}
	}
}

// ── decisionFromPayload ───────────────────────────────────────────────────

func TestDecisionFromPayload(t *testing.T) {
	pay, err := qdrant.TryValueMap(map[string]any{
		"text":       "use JWT",
		"reason":     "stateless auth",
		"tags":       []any{"auth", "security"},
		"task_id":    "TASK-001",
		"author":     "architect",
		"created_at": "2024-06-01T12:00:00Z",
	})
	if err != nil {
		t.Fatalf("TryValueMap: %v", err)
	}
	d := decisionFromPayload("uuid-123", pay)
	if d.ID != "uuid-123" {
		t.Errorf("ID: got %q, want %q", d.ID, "uuid-123")
	}
	if d.Text != "use JWT" {
		t.Errorf("Text: got %q", d.Text)
	}
	if d.Reason != "stateless auth" {
		t.Errorf("Reason: got %q", d.Reason)
	}
	if len(d.Tags) != 2 || d.Tags[0] != "auth" || d.Tags[1] != "security" {
		t.Errorf("Tags: got %v", d.Tags)
	}
	if d.TaskID != "TASK-001" {
		t.Errorf("TaskID: got %q", d.TaskID)
	}
	if d.Author != "architect" {
		t.Errorf("Author: got %q", d.Author)
	}
}

// ── New — validation ──────────────────────────────────────────────────────

func TestNewClient_RequiresProjectID(t *testing.T) {
	cfg := &config.QdrantConfig{Host: "127.0.0.1", Port: 19997}
	if _, err := New(cfg, t.TempDir(), ""); err == nil {
		t.Fatal("expected error for empty projectID")
	}
}

func TestNewClient_ConnectsWithAnyHost(t *testing.T) {
	// New() should succeed even when Qdrant is not listening — it only creates
	// the client struct, not an actual connection. HealthCheck would fail.
	cfg := &config.QdrantConfig{Host: "127.0.0.1", Port: 19997}
	c, err := New(cfg, t.TempDir(), "test-proj")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = c.Close() }()
}

// ── SearchDiscussions filter ──────────────────────────────────────────────────

// TestSearchDiscussions_ExcludesResolvedByDefault verifies that the filter
// helper used when includeResolved=false targets only the "open" status.
func TestSearchDiscussions_ExcludesResolvedByDefault(t *testing.T) {
	f := OpenDiscussionsFilter()
	if f == nil {
		t.Fatal("OpenDiscussionsFilter: expected non-nil filter")
	}
	if len(f.Must) != 1 {
		t.Fatalf("Must conditions: got %d, want 1", len(f.Must))
	}
	kw := f.Must[0].GetField().GetMatch().GetKeyword()
	if kw != "open" {
		t.Errorf("keyword: got %q, want %q", kw, "open")
	}
	field := f.Must[0].GetField().GetKey()
	if field != "status" {
		t.Errorf("field: got %q, want %q", field, "status")
	}
}

// TestSearchTasks_ExcludesDoneByDefault verifies that the filter helper used
// when includeAll=false targets only pending and in_progress statuses.
func TestSearchTasks_ExcludesDoneByDefault(t *testing.T) {
	f := ActiveTasksFilter()
	if f == nil {
		t.Fatal("ActiveTasksFilter: expected non-nil filter")
	}
	if len(f.Must) != 1 {
		t.Fatalf("Must conditions: got %d, want 1", len(f.Must))
	}
	field := f.Must[0].GetField().GetKey()
	if field != "status" {
		t.Errorf("field: got %q, want %q", field, "status")
	}
	values := f.Must[0].GetField().GetMatch().GetKeywords().GetStrings()
	want := map[string]bool{"pending": true, "in_progress": true}
	if len(values) != len(want) {
		t.Fatalf("keywords: got %v, want %v", values, want)
	}
	for _, v := range values {
		if !want[v] {
			t.Errorf("unexpected keyword %q in active tasks filter", v)
		}
	}
}

// ── Turn (de)serialization ────────────────────────────────────────────────────

func TestTurnsRoundTrip(t *testing.T) {
	turns := []Turn{
		{By: "architect", Text: "we should use JWT", Role: "architect", At: "2026-01-01T00:00:00Z"},
		{By: "dev", Text: "agreed, simpler than sessions", Role: "dev", At: "2026-01-01T01:00:00Z"},
	}
	encoded := turnsToAny(turns)
	if len(encoded) != 2 {
		t.Fatalf("turnsToAny: got %d items, want 2", len(encoded))
	}

	// Verify the maps round-trip correctly via TryValueMap then extractTurns.
	pm, err := qdrant.TryValueMap(map[string]any{"turns": encoded})
	if err != nil {
		t.Fatalf("TryValueMap: %v", err)
	}
	decoded := extractTurns(pm["turns"])
	if len(decoded) != len(turns) {
		t.Fatalf("extractTurns: got %d turns, want %d", len(decoded), len(turns))
	}
	for i, want := range turns {
		got := decoded[i]
		if got.By != want.By || got.Text != want.Text || got.Role != want.Role || got.At != want.At {
			t.Errorf("[%d]: got %+v, want %+v", i, got, want)
		}
	}
}

func TestTurnsToAny_Empty(t *testing.T) {
	if got := turnsToAny(nil); got != nil {
		t.Errorf("expected nil for nil input, got %v", got)
	}
	if got := turnsToAny([]Turn{}); got != nil {
		t.Errorf("expected nil for empty input, got %v", got)
	}
}

func TestEstimateTurnsBytes(t *testing.T) {
	turns := []Turn{
		{By: "dev", Text: "short", Role: "dev", At: "2026-01-01T00:00:00Z"},
	}
	size := estimateTurnsBytes(turns)
	if size <= 0 {
		t.Errorf("estimateTurnsBytes: got %d, want > 0", size)
	}
	// Adding a turn must increase the estimate.
	turns2 := append(append([]Turn(nil), turns...), Turn{By: "pm", Text: "another turn", Role: "pm", At: "2026-01-01T01:00:00Z"})
	if estimateTurnsBytes(turns2) <= size {
		t.Error("estimateTurnsBytes: expected larger estimate after adding a turn")
	}
}
