package cmd

import (
	"testing"

	"github.com/gurkangul/gg-cli/internal/store"
)

func TestFormatTaskAckDecision(t *testing.T) {
	got := formatTaskAckDecision("TASK-042", " AC-1: parse; AC-2: test ")
	want := "TASK-042 ACK: AC-1: parse; AC-2: test"
	if got != want {
		t.Fatalf("formatTaskAckDecision() = %q, want %q", got, want)
	}
}

func TestFilterPendingAckTasks(t *testing.T) {
	tasks := []store.Task{
		{ID: "TASK-001", Status: "in_progress"},
		{ID: "TASK-002", Status: "in_progress"},
		{ID: "TASK-003", Status: "in_progress"},
	}
	msgs := []store.Message{
		{TaskID: "TASK-001", Content: "TASK-001 ACK: AC-1 = parse"},
		{TaskID: "TASK-002", Content: "TASK-002 ACK: AC-1 = parse"},
		{TaskID: "TASK-002", Content: "TASK-002 ACK-OK"},
	}
	got := filterPendingAckTasks(tasks, msgs)
	if len(got) != 1 || got[0].ID != "TASK-001" {
		t.Fatalf("filterPendingAckTasks() = %+v, want only TASK-001", got)
	}
}
