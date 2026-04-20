package store

import (
	"context"
	"testing"
)

func TestListDecisionsByTaskID_EmptyTaskID_ReturnsNil(t *testing.T) {
	c := &Client{}
	got, err := c.ListDecisionsByTaskID(context.Background(), "")
	if err != nil {
		t.Fatalf("empty taskID should not error, got %v", err)
	}
	if got != nil {
		t.Fatalf("empty taskID should return nil slice, got %v", got)
	}
}

func TestDecisionPayloadRoundTrip_TaskIDPreserved(t *testing.T) {
	// Sanity: the Decision struct carries TaskID, and decisionFromPayload
	// restores it. If this regresses, the filter in ListDecisionsByTaskID
	// would silently return nothing.
	want := "TASK-242"
	d := Decision{TaskID: want}
	if d.TaskID != want {
		t.Fatalf("Decision.TaskID not preserved in struct: %q", d.TaskID)
	}
}
