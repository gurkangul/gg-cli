package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/gurkangul/gg-cli/internal/store"
)

func TestRenderTaskPacketIncludesReviewerClosureContext(t *testing.T) {
	packet := taskPacket{
		Task: &store.Task{
			ID:               "TASK-123",
			Title:            "Ship thing",
			Status:           "ready_for_live",
			Priority:         "high",
			Owner:            "omo-slim",
			ReadyForLiveBy:   "implementer",
			ReadyForLiveAt:   "2026-05-24T12:00:00Z",
			ReadyForLivePlan: "run live smoke and gg doctor",
		},
		Decisions: []store.Decision{{ID: "decision-123456", Text: "Use reviewer packet", CreatedAt: "2026-05-24T12:01:00Z"}},
		Messages:  []store.Message{{FromRole: "implementer", ToRole: "reviewer", Content: "TASK-123 ready for review", CreatedAt: "2026-05-24T12:02:00Z"}},
		Events:    []taskPacketEvent{{Action: "ready_for_live_updated", FromStatus: "ready_for_live", ToStatus: "ready_for_live", Actor: "implementer", Detail: "plan corrected", CreatedAt: "2026-05-24T12:03:00Z"}},
	}

	var buf bytes.Buffer
	renderTaskPacket(&buf, packet)
	out := buf.String()
	for _, want := range []string{
		"Task packet: TASK-123",
		"Ready for live:",
		"Plan: run live smoke and gg doctor",
		"Use reviewer packet",
		"TASK-123 ready for review",
		"ready_for_live_updated",
		"gg task review TASK-123 --approve",
		"gg task done TASK-123",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("packet output missing %q:\n%s", want, out)
		}
	}
}

func TestFilterTaskMessagesUsesTaskIDAndContent(t *testing.T) {
	messages := []store.Message{
		{TaskID: "TASK-123", Content: "direct"},
		{Content: "mentions task-123 in body"},
		{TaskID: "TASK-999", Content: "other"},
	}
	got := filterTaskMessages("TASK-123", messages)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2 (%#v)", len(got), got)
	}
}
