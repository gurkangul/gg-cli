// Package store — unit tests for discussionFromPayload, rejectionFromPayload,
// and noteFromPayload deserializers.
package store

import (
	"testing"
)

// ── discussionFromPayload ─────────────────────────────────────────────────────

func TestDiscussionFromPayload_Full(t *testing.T) {
	pay, err := TryValueMap(map[string]any{
		"disc_id":       "DISC-003",
		"topic":         "DB choice",
		"detail":        "postgres vs sqlite",
		"status":        "resolved",
		"resolved_via":  "decision",
		"resolved_note": "decided postgres, see D004",
		"dismiss_note":  "",
		"tags":          []any{"db", "infra"},
		"turns":         []any{},
		"created_at":    "2026-02-01T00:00:00Z",
		"updated_at":    "2026-03-01T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("TryValueMap: %v", err)
	}

	d := discussionFromPayload(pay)

	if d.ID != "DISC-003" {
		t.Errorf("ID: got %q", d.ID)
	}
	if d.Topic != "DB choice" {
		t.Errorf("Topic: got %q", d.Topic)
	}
	if d.Status != "resolved" {
		t.Errorf("Status: got %q", d.Status)
	}
	if d.ResolvedVia != "decision" {
		t.Errorf("ResolvedVia: got %q", d.ResolvedVia)
	}
	if d.ResolvedNote != "decided postgres, see D004" {
		t.Errorf("ResolvedNote: got %q", d.ResolvedNote)
	}
	if len(d.Tags) != 2 || d.Tags[0] != "db" || d.Tags[1] != "infra" {
		t.Errorf("Tags: got %v", d.Tags)
	}
}

func TestDiscussionFromPayload_Empty(t *testing.T) {
	pay := map[string]*Value{}
	d := discussionFromPayload(pay)
	if d.ID != "" {
		t.Errorf("expected empty ID, got %q", d.ID)
	}
	if d.Status != "" {
		t.Errorf("expected empty status, got %q", d.Status)
	}
}

// ── rejectionFromPayload ──────────────────────────────────────────────────────

func TestRejectionFromPayload_Full(t *testing.T) {
	pay, err := TryValueMap(map[string]any{
		"approach":   "microservices",
		"reason":     "too complex for current team size",
		"tags":       []any{"arch", "scope"},
		"task_id":    "TASK-012",
		"author":     "architect",
		"created_at": "2026-01-15T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("TryValueMap: %v", err)
	}

	r := rejectionFromPayload("some-uuid", pay)

	if r.ID != "some-uuid" {
		t.Errorf("ID: got %q", r.ID)
	}
	if r.Approach != "microservices" {
		t.Errorf("Approach: got %q", r.Approach)
	}
	if r.Reason != "too complex for current team size" {
		t.Errorf("Reason: got %q", r.Reason)
	}
	if len(r.Tags) != 2 || r.Tags[0] != "arch" {
		t.Errorf("Tags: got %v", r.Tags)
	}
	if r.TaskID != "TASK-012" {
		t.Errorf("TaskID: got %q", r.TaskID)
	}
	if r.Author != "architect" {
		t.Errorf("Author: got %q", r.Author)
	}
}

func TestRejectionFromPayload_Empty(t *testing.T) {
	pay := map[string]*Value{}
	r := rejectionFromPayload("id1", pay)
	if r.ID != "id1" {
		t.Errorf("expected ID 'id1', got %q", r.ID)
	}
	if r.Approach != "" {
		t.Errorf("expected empty approach, got %q", r.Approach)
	}
}

// ── noteFromPayload ───────────────────────────────────────────────────────────

func TestNoteFromPayload_Full(t *testing.T) {
	pay, err := TryValueMap(map[string]any{
		"text":       "high latency on search endpoint",
		"tags":       []any{"perf", "search"},
		"task_id":    "TASK-020",
		"created_at": "2026-04-01T09:00:00Z",
	})
	if err != nil {
		t.Fatalf("TryValueMap: %v", err)
	}

	n := noteFromPayload("note-uuid", pay)

	if n.ID != "note-uuid" {
		t.Errorf("ID: got %q", n.ID)
	}
	if n.Text != "high latency on search endpoint" {
		t.Errorf("Text: got %q", n.Text)
	}
	if n.TaskID != "TASK-020" {
		t.Errorf("TaskID: got %q", n.TaskID)
	}
	if len(n.Tags) != 2 || n.Tags[0] != "perf" {
		t.Errorf("Tags: got %v", n.Tags)
	}
}

func TestNoteFromPayload_Empty(t *testing.T) {
	pay := map[string]*Value{}
	n := noteFromPayload("nid", pay)
	if n.ID != "nid" {
		t.Errorf("expected ID 'nid', got %q", n.ID)
	}
}
