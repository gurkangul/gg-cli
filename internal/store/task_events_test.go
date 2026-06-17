package store

import (
	"testing"

	"github.com/gurkangul/gg-cli/internal/brain"
)

func TestAppendTaskEvent_WritesJSONL(t *testing.T) {
	c, err := New(t.TempDir(), "test-project")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = c.Close() }()

	if err := c.AppendTaskEvent(TaskEvent{
		TaskID:     "TASK-001",
		Action:     "started",
		FromStatus: "pending",
		ToStatus:   "in_progress",
		Owner:      "codex",
		LeaseUntil: "2026-01-01T00:30:00Z",
		Actor:      "codex",
	}); err != nil {
		t.Fatalf("AppendTaskEvent: %v", err)
	}

	entries, err := brain.ReadAll(c.dataDir, taskEventsKind)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries: got %d, want 1", len(entries))
	}
	payload := entries[0].Payload
	if payload["task_id"] != "TASK-001" {
		t.Errorf("task_id: got %v", payload["task_id"])
	}
	if payload["action"] != "started" {
		t.Errorf("action: got %v", payload["action"])
	}
	if payload["owner"] != "codex" {
		t.Errorf("owner: got %v", payload["owner"])
	}
}

func TestAppendTaskEvent_RequiresTaskAndAction(t *testing.T) {
	c, err := New(t.TempDir(), "test-project")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = c.Close() }()

	if err := c.AppendTaskEvent(TaskEvent{Action: "started"}); err == nil {
		t.Fatal("expected missing task_id error")
	}
	if err := c.AppendTaskEvent(TaskEvent{TaskID: "TASK-001"}); err == nil {
		t.Fatal("expected missing action error")
	}
}
