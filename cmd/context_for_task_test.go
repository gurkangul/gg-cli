package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/brain"
	"github.com/gurkangul/gg-cli/internal/store"
)

func makeTaskContextBundle(pinDecision bool) taskContextBundle {
	dec := store.Decision{
		ID:        "dec-pinned",
		Text:      "Use Memgraph for graph queries",
		TaskID:    "TASK-100",
		Status:    "active",
		CreatedAt: "2026-04-10T00:00:00Z",
		Tags:      []string{"graph", "memgraph"},
	}
	decUnrelated := store.Decision{
		ID:        "dec-other",
		Text:      "Use JWT for auth",
		Status:    "active",
		CreatedAt: "2026-04-01T00:00:00Z",
	}
	rej := store.Rejection{
		ID:        "rej-1",
		Approach:  "Use REST polling instead",
		CreatedAt: "2026-04-05T00:00:00Z",
	}
	anchor := store.Task{
		ID:        "TASK-100",
		Title:     "Write DEPENDS_ON edges",
		Status:    "in_progress",
		Priority:  "high",
		Detail:    "Create Task->Task edges in Memgraph",
		Tags:      []string{"graph", "memgraph"},
		DependsOn: []string{"TASK-90"},
	}
	dep := store.Task{
		ID:       "TASK-90",
		Title:    "Setup Memgraph dual-write",
		Status:   "done",
		Priority: "high",
	}
	decisions := []store.Decision{dec, decUnrelated}
	if !pinDecision {
		decisions = []store.Decision{decUnrelated, dec}
	}
	return taskContextBundle{
		anchor: anchor,
		deps:   []store.Task{dep},
		bundle: contextBundle{
			decisions:  decisions,
			rejections: []store.Rejection{rej},
		},
	}
}

func TestRenderForTask_Header(t *testing.T) {
	tb := makeTaskContextBundle(true)
	var buf strings.Builder
	renderForTask(&buf, tb, nil)
	out := buf.String()

	if !strings.Contains(out, "TASK-100") {
		t.Error("anchor task ID missing from header")
	}
	if !strings.Contains(out, "Write DEPENDS_ON edges") {
		t.Error("anchor task title missing from header")
	}
	if !strings.Contains(out, "in_progress") {
		t.Error("anchor status missing")
	}
	if !strings.Contains(out, "high") {
		t.Error("anchor priority missing")
	}
}

func TestRenderForTask_ShowsDeps(t *testing.T) {
	tb := makeTaskContextBundle(true)
	var buf strings.Builder
	renderForTask(&buf, tb, nil)
	out := buf.String()

	if !strings.Contains(out, "TASK-90") {
		t.Error("dependency TASK-90 missing from output")
	}
	if !strings.Contains(out, "Setup Memgraph dual-write") {
		t.Error("dependency title missing from output")
	}
}

func TestRenderForTask_ShowsDecisions(t *testing.T) {
	tb := makeTaskContextBundle(true)
	var buf strings.Builder
	renderForTask(&buf, tb, nil)
	out := buf.String()

	if !strings.Contains(out, "Use Memgraph for graph queries") {
		t.Error("related decision missing from output")
	}
}

func TestRenderForTask_ShowsRejections(t *testing.T) {
	tb := makeTaskContextBundle(true)
	var buf strings.Builder
	renderForTask(&buf, tb, nil)
	out := buf.String()

	if !strings.Contains(out, "Use REST polling instead") {
		t.Error("related rejection missing from output")
	}
}

func TestBoostRelatedDecisions_TaskIDPinned(t *testing.T) {
	decisions := []store.Decision{
		{ID: "d1", Text: "unrelated", TaskID: ""},
		{ID: "d2", Text: "pinned", TaskID: "TASK-100"},
	}
	result := boostRelatedDecisions(decisions, "TASK-100", nil)
	if result[0].ID != "d2" {
		t.Errorf("TaskID-matched decision should be first, got %s", result[0].ID)
	}
}

func TestBoostRelatedDecisions_TagOverlapPinned(t *testing.T) {
	decisions := []store.Decision{
		{ID: "d1", Text: "unrelated"},
		{ID: "d2", Text: "tag match", Tags: []string{"memgraph"}},
	}
	anchorTags := map[string]bool{"memgraph": true}
	result := boostRelatedDecisions(decisions, "TASK-999", anchorTags)
	if result[0].ID != "d2" {
		t.Errorf("tag-overlap decision should be first, got %s", result[0].ID)
	}
}

func TestBoostRelatedDecisions_NoDuplicates(t *testing.T) {
	decisions := []store.Decision{
		{ID: "d1", Text: "both", TaskID: "TASK-100", Tags: []string{"memgraph"}},
		{ID: "d2", Text: "other"},
	}
	anchorTags := map[string]bool{"memgraph": true}
	result := boostRelatedDecisions(decisions, "TASK-100", anchorTags)
	seen := make(map[string]int)
	for _, d := range result {
		seen[d.ID]++
	}
	if seen["d1"] != 1 {
		t.Errorf("d1 appears %d times, want 1", seen["d1"])
	}
}

func TestRenderForTask_NoDepsSection(t *testing.T) {
	tb := makeTaskContextBundle(true)
	tb.deps = nil
	var buf strings.Builder
	renderForTask(&buf, tb, nil)
	out := buf.String()

	if strings.Contains(out, "DEPENDENCIES") {
		t.Error("DEPENDENCIES section should be absent when no deps")
	}
}

func TestRenderForTask_ShowsWarnings(t *testing.T) {
	tb := makeTaskContextBundle(true)
	var buf strings.Builder
	renderForTask(&buf, tb, []string{"decisions: some error"})
	out := buf.String()

	if !strings.Contains(out, "some error") {
		t.Error("warning missing from output")
	}
}

func TestRunContextForTaskJSONLFallback_RendersSeededData(t *testing.T) {
	ggDir := setupGGDir(t)

	seed := func(kind, id string, payload map[string]any) {
		t.Helper()
		if err := brain.Append(ggDir, kind, id, "test", payload); err != nil {
			t.Fatalf("Append %s %s: %v", kind, id, err)
		}
	}
	seed("tasks", "TASK-444", map[string]any{
		"task_id":    "TASK-444",
		"title":      "Semantic memory reliability",
		"detail":     "Test JSONL fallback rendering",
		"status":     "in_progress",
		"priority":   "high",
		"depends_on": []any{"TASK-443"},
		"tags":       []any{"semantic", "reliability"},
		"created_at": "2026-05-10T00:00:00Z",
	})
	seed("tasks", "TASK-443", map[string]any{
		"task_id":    "TASK-443",
		"title":      "Seed dependency",
		"status":     "done",
		"priority":   "medium",
		"created_at": "2026-05-09T00:00:00Z",
	})
	seed("decisions", "dec-444", map[string]any{
		"text":       "Use JSONL fallback for degraded semantic memory paths",
		"reason":     "qdrant unavailable",
		"task_id":    "TASK-444",
		"created_at": "2026-05-10T01:00:00Z",
	})
	seed("rejections", "rej-444", map[string]any{
		"approach":   "Ignore degraded paths",
		"reason":     "rejected because reliability matters",
		"task_id":    "TASK-444",
		"created_at": "2026-05-10T02:00:00Z",
	})
	seed("notes", "note-444", map[string]any{
		"text":       "Remember JSONL fallback warnings",
		"task_id":    "TASK-444",
		"created_at": "2026-05-10T03:00:00Z",
	})
	seed("messages", "msg-444", map[string]any{
		"from_role":  "planner",
		"to_role":    "developer",
		"content":    "Fallback is expected in tests",
		"task_id":    "TASK-444",
		"created_at": "2026-05-10T04:00:00Z",
	})

	cmd := &cobra.Command{}
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	if err := runContextForTaskJSONLFallback(cmd, "TASK-444", "qdrant unreachable"); err != nil {
		t.Fatalf("runContextForTaskJSONLFallback: %v", err)
	}
	out := stdout.String() + stderr.String()
	if !strings.Contains(out, "served from JSONL fallback") {
		t.Fatalf("missing fallback warning in output: %s", out)
	}
	if !strings.Contains(out, "Run: gg doctor") {
		t.Fatalf("missing doctor hint in output: %s", out)
	}
	if !strings.Contains(out, "TASK-444") || !strings.Contains(out, "Semantic memory reliability") {
		t.Fatalf("missing seeded task content: %s", out)
	}
	if !strings.Contains(out, "dec-444") || !strings.Contains(out, "rej-444") || !strings.Contains(out, "note-444") {
		t.Fatalf("missing linked JSONL records: %s", out)
	}
	if !strings.Contains(out, "Fallback is expected in tests") {
		t.Fatalf("missing linked JSONL task message: %s", out)
	}
}
