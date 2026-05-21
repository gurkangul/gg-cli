package cmd

import (
	"testing"
	"time"

	"github.com/gurkangul/gg-cli/internal/brain"
	"github.com/gurkangul/gg-cli/internal/store"
)

func TestFoldTaskEventsTracksOwnershipLifecycle(t *testing.T) {
	creates := map[string]brain.Entry{
		"TASK-001": {Payload: map[string]any{
			"task_id": "TASK-001",
			"status":  "pending",
		}},
	}
	events := []brain.Entry{
		taskEventEntry("TASK-001", "started", "pending", "in_progress", "codex", "2026-05-20T10:30:00Z", "2026-05-20T10:00:00Z"),
		taskEventEntry("TASK-001", "renewed", "in_progress", "in_progress", "codex", "2026-05-20T11:00:00Z", "2026-05-20T10:20:00Z"),
		taskEventEntry("TASK-001", "released", "in_progress", "pending", "codex", "", "2026-05-20T10:25:00Z"),
	}

	got := foldTaskEvents(creates, events)["TASK-001"]
	if got.Status != "pending" || got.Owner != "" || got.LeaseUntil != "" || got.ClaimedAt != "" {
		t.Fatalf("projection = %+v, want released pending task", got)
	}
}

func TestReconcileTaskDriftDetectsMissingStaleAndOrphaned(t *testing.T) {
	now := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	expected := map[string]taskProjection{
		"TASK-001": {ID: "TASK-001", Status: "in_progress", Owner: "codex", ClaimedAt: "2026-05-20T10:00:00Z", LeaseUntil: "2026-05-20T13:00:00Z"},
		"TASK-002": {ID: "TASK-002", Status: "pending"},
		"TASK-003": {ID: "TASK-003", Status: "in_progress", Owner: "codex", LeaseUntil: "2026-05-20T11:00:00Z"},
		"TASK-004": {ID: "TASK-004", Status: "pending"},
	}
	actual := map[string]store.Task{
		"TASK-001": {ID: "TASK-001", Status: "pending", Version: 2},
		"TASK-003": {ID: "TASK-003", Status: "in_progress", Owner: "codex", LeaseUntil: "2026-05-20T11:00:00Z", Version: 4},
		"TASK-004": {ID: "TASK-004", Status: "pending", LeaseUntil: "2026-05-20T13:00:00Z", Version: 1},
	}

	got := reconcileTaskDrift(expected, actual, now)
	kinds := map[string]bool{}
	for _, drift := range got {
		kinds[drift.Kind] = true
	}
	for _, want := range []string{"projection_drift", "missing_projection", "stale_lease", "orphaned_lease"} {
		if !kinds[want] {
			t.Fatalf("missing drift kind %q in %#v", want, got)
		}
	}
}

func TestStaleLeaseOnlyAppliesToInProgress(t *testing.T) {
	now := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	task := store.Task{ID: "TASK-001", Status: "ready_for_live", Owner: "codex", LeaseUntil: "2026-05-20T11:00:00Z"}
	if staleLease(task, now) {
		t.Fatal("ready_for_live task should not be treated as active stale lease")
	}
	task.Status = "in_progress"
	if !staleLease(task, now) {
		t.Fatal("in_progress task with expired lease should be stale")
	}
}

func taskEventEntry(taskID, action, fromStatus, toStatus, owner, leaseUntil, createdAt string) brain.Entry {
	return brain.Entry{Payload: map[string]any{
		"task_id":     taskID,
		"action":      action,
		"from_status": fromStatus,
		"to_status":   toStatus,
		"owner":       owner,
		"lease_until": leaseUntil,
		"created_at":  createdAt,
	}}
}
