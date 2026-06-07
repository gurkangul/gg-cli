package store

import (
	"testing"
	"time"
)

// TASK-470: stale agent broadcasts archive out of the inbox but stay in JSONL.
func TestArchiveAgentBroadcasts_Integration(t *testing.T) {
	c, ctx := newIntegrationClient(t, "archive")
	if err := c.SendMessage(ctx, Message{FromRole: "qa", ToRole: "all", Content: "TASK-1 started", Audience: "agents"}); err != nil {
		t.Fatalf("send1: %v", err)
	}
	if err := c.SendMessage(ctx, Message{FromRole: "qa", ToRole: "reviewer", Content: "real handoff", Audience: "all"}); err != nil {
		t.Fatalf("send2: %v", err)
	}
	n, err := c.ArchiveAgentBroadcasts(ctx, time.Now().UTC().Add(time.Hour)) // everything before now+1h
	if err != nil {
		t.Fatalf("archive: %v", err)
	}
	if n != 1 {
		t.Fatalf("want 1 broadcast archived, got %d", n)
	}
	// Archived broadcast gone from inbox; the real handoff remains.
	got, err := c.GetInbox(ctx, "", false, "someagent")
	if err != nil {
		t.Fatalf("inbox: %v", err)
	}
	for _, m := range got {
		if m.Content == "TASK-1 started" {
			t.Errorf("archived broadcast still in inbox")
		}
	}
}
