package cmd

import (
	"strings"
	"testing"

	"github.com/gurkangul/gg-cli/internal/store"
)

// TestValidateTaskAckAllowed verifies that terminal-state tasks are rejected
// before any store write occurs — the guard must fire before AddDecision.
func TestValidateTaskAckAllowed(t *testing.T) {
	for _, status := range []string{"done", "ready_for_live", "blocked"} {
		err := validateTaskAckAllowed("TASK-001", status)
		if err == nil {
			t.Fatalf("validateTaskAckAllowed(%q) = nil, want error", status)
		}
		if !strings.Contains(err.Error(), "cannot ACK") {
			t.Fatalf("validateTaskAckAllowed(%q) error = %q, want 'cannot ACK'", status, err)
		}
	}
	for _, status := range []string{"pending", "in_progress"} {
		if err := validateTaskAckAllowed("TASK-001", status); err != nil {
			t.Fatalf("validateTaskAckAllowed(%q) = %v, want nil", status, err)
		}
	}
}

func TestFormatTaskAckDecision(t *testing.T) {
	got := formatTaskAckDecision("TASK-042", " AC-1: parse; AC-2: test ")
	want := "TASK-042 ACK: AC-1: parse; AC-2: test"
	if got != want {
		t.Fatalf("formatTaskAckDecision() = %q, want %q", got, want)
	}
}

func TestSplitTaskAckInboxBlockers_ConsumesMatchingTaskOnly(t *testing.T) {
	msgs := []store.Message{
		{ID: "msg-task-field", TaskID: "TASK-387", ToRole: "developer", Content: "assignment"},
		{ID: "msg-task-content", ToRole: "developer", Content: "please ack TASK-387"},
		{ID: "msg-other", TaskID: "TASK-999", ToRole: "developer", Content: "other assignment"},
	}

	blocking, handled := splitTaskAckInboxBlockers(msgs, "TASK-387")
	if len(handled) != 2 {
		t.Fatalf("handled = %+v, want two matching task messages", handled)
	}
	if len(blocking) != 1 || blocking[0].ID != "msg-other" {
		t.Fatalf("blocking = %+v, want only unrelated assignment", blocking)
	}
}

func TestTaskAckMessageTargets_ExcludesAuthor(t *testing.T) {
	t.Setenv("GG_ROLE", "developer")
	t.Setenv("GG_AGENT", "codex")

	got := taskAckMessageTargets("developer")
	for _, target := range got {
		if target == "developer" {
			t.Fatalf("taskAckMessageTargets included author: %+v", got)
		}
	}
	want := []string{"codex", "master"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("taskAckMessageTargets() = %+v, want %+v", got, want)
	}
}

func TestFilterPendingAckTasks(t *testing.T) {
	tasks := []store.Task{
		{ID: "TASK-001", Status: "in_progress"},
		{ID: "TASK-002", Status: "in_progress"},
		{ID: "TASK-003", Status: "in_progress"},
	}
	msgs := []store.Message{
		{TaskID: "TASK-001", Content: "TASK-001 ACK: AC-1 = parse", CreatedAt: "2026-01-01T00:01:00Z"},
		{TaskID: "TASK-002", Content: "TASK-002 ACK: AC-1 = parse", CreatedAt: "2026-01-01T00:01:00Z"},
		{TaskID: "TASK-002", Content: "TASK-002 ACK-OK", CreatedAt: "2026-01-01T00:02:00Z"},
	}
	got := filterPendingAckTasks(tasks, msgs)
	if len(got) != 1 || got[0].ID != "TASK-001" {
		t.Fatalf("filterPendingAckTasks() = %+v, want only TASK-001", got)
	}
}

// TestFilterPendingAckTasks_ReAckAfterAckFix verifies that after master sends
// ACK-FIX and the worker re-ACKs, the task re-appears as pending-ack.
// The boolean-presence approach (B1) would permanently mark it resolved.
func TestFilterPendingAckTasks_ReAckAfterAckFix(t *testing.T) {
	tasks := []store.Task{
		{ID: "TASK-001", Status: "in_progress"},
	}
	msgs := []store.Message{
		// Round 1: worker ACKs, master sends ACK-FIX
		{TaskID: "TASK-001", Content: "TASK-001 ACK: AC-1 = first attempt", CreatedAt: "2026-01-01T00:01:00Z"},
		{TaskID: "TASK-001", Content: "TASK-001 ACK-FIX correct AC-1", CreatedAt: "2026-01-01T00:02:00Z"},
		// Round 2: worker re-ACKs after fix — must surface again as pending-ack
		{TaskID: "TASK-001", Content: "TASK-001 ACK: AC-1 = corrected paraphrase", CreatedAt: "2026-01-01T00:03:00Z"},
	}
	got := filterPendingAckTasks(tasks, msgs)
	if len(got) != 1 || got[0].ID != "TASK-001" {
		t.Fatalf("filterPendingAckTasks() after re-ACK = %+v, want TASK-001 to be pending-ack again", got)
	}
}

// TestFilterPendingAckTasks_ResolvedAfterReAck verifies that once master sends
// ACK-OK after a re-ACK, the task is no longer pending.
func TestFilterPendingAckTasks_ResolvedAfterReAck(t *testing.T) {
	tasks := []store.Task{
		{ID: "TASK-001", Status: "in_progress"},
	}
	msgs := []store.Message{
		{TaskID: "TASK-001", Content: "TASK-001 ACK: AC-1 = first attempt", CreatedAt: "2026-01-01T00:01:00Z"},
		{TaskID: "TASK-001", Content: "TASK-001 ACK-FIX correct AC-1", CreatedAt: "2026-01-01T00:02:00Z"},
		{TaskID: "TASK-001", Content: "TASK-001 ACK: AC-1 = corrected paraphrase", CreatedAt: "2026-01-01T00:03:00Z"},
		{TaskID: "TASK-001", Content: "TASK-001 ACK-OK", CreatedAt: "2026-01-01T00:04:00Z"},
	}
	got := filterPendingAckTasks(tasks, msgs)
	if len(got) != 0 {
		t.Fatalf("filterPendingAckTasks() after final ACK-OK = %+v, want empty (resolved)", got)
	}
}
