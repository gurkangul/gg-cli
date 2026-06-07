package store

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

// TASK-469: ListPinnedDecisions returns only pinned active decisions, so the
// overview can surface them first regardless of age.
func TestListPinnedDecisions_Integration(t *testing.T) {
	c, ctx := newIntegrationClient(t, "pin")
	vec := make([]float32, VectorSize)
	vec[0] = 1
	pinnedID := uuid.New().String()
	if err := c.AddDecision(ctx, Decision{ID: pinnedID, Text: "pinned one", Reason: "r", Status: "active", Pinned: true}, vec); err != nil {
		t.Fatalf("add pinned: %v", err)
	}
	if err := c.AddDecision(ctx, Decision{ID: uuid.New().String(), Text: "normal one", Reason: "r", Status: "active"}, vec); err != nil {
		t.Fatalf("add normal: %v", err)
	}
	got, err := c.ListPinnedDecisions(context.Background())
	if err != nil {
		t.Fatalf("ListPinnedDecisions: %v", err)
	}
	if len(got) != 1 || got[0].ID != pinnedID || !got[0].Pinned {
		t.Fatalf("want exactly the pinned decision, got %+v", got)
	}
}
