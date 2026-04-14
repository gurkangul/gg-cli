// Package store — unit tests for UUID derivation helpers.
package store

import (
	"strings"
	"testing"
)

// ── pointUUIDForTaskID ────────────────────────────────────────────────────────

func TestPointUUIDForTaskID_Deterministic(t *testing.T) {
	a := pointUUIDForTaskID("TASK-001")
	b := pointUUIDForTaskID("TASK-001")
	if a != b {
		t.Errorf("expected same UUID on repeated calls, got %q and %q", a, b)
	}
}

func TestPointUUIDForTaskID_DifferentIDs(t *testing.T) {
	a := pointUUIDForTaskID("TASK-001")
	b := pointUUIDForTaskID("TASK-002")
	if a == b {
		t.Errorf("expected distinct UUIDs for TASK-001 and TASK-002, both got %q", a)
	}
}

func TestPointUUIDForTaskID_IsUUIDFormat(t *testing.T) {
	id := pointUUIDForTaskID("TASK-042")
	// UUID format: 8-4-4-4-12 hex chars separated by dashes.
	parts := strings.Split(id, "-")
	if len(parts) != 5 {
		t.Errorf("expected 5 dash-separated parts, got %d in %q", len(parts), id)
	}
}

// ── pointUUIDForDiscID ────────────────────────────────────────────────────────

func TestPointUUIDForDiscID_Deterministic(t *testing.T) {
	a := pointUUIDForDiscID("DISC-001")
	b := pointUUIDForDiscID("DISC-001")
	if a != b {
		t.Errorf("expected same UUID on repeated calls, got %q and %q", a, b)
	}
}

func TestPointUUIDForDiscID_DifferentIDs(t *testing.T) {
	a := pointUUIDForDiscID("DISC-001")
	b := pointUUIDForDiscID("DISC-002")
	if a == b {
		t.Errorf("expected distinct UUIDs for DISC-001 and DISC-002, both got %q", a)
	}
}

func TestPointUUIDForDiscID_DoesNotCollidWithTask(t *testing.T) {
	// Same numeric suffix but different prefix → different namespace → different UUID.
	taskUUID := pointUUIDForTaskID("TASK-001")
	discUUID := pointUUIDForDiscID("DISC-001")
	if taskUUID == discUUID {
		t.Errorf("task and disc UUID namespaces must not collide, both got %q", taskUUID)
	}
}
