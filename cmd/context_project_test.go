package cmd

import (
	"testing"

	"github.com/gurkangul/gg-cli/internal/store"
)

func TestProjectContextTasksFiltersResolvedByDefault(t *testing.T) {
	tasks := []store.Task{
		{ID: "TASK-004", Title: "done", Status: "done", Priority: "high"},
		{ID: "TASK-002", Title: "ready", Status: "pending", Priority: "medium"},
		{ID: "TASK-001", Title: "active", Status: "in_progress", Priority: "low"},
		{ID: "TASK-003", Title: "risk", Status: "blocked", Priority: "high"},
	}

	got := projectContextTasks(tasks, 0, false)
	ids := make([]string, 0, len(got))
	for _, task := range got {
		ids = append(ids, task.ID)
		if task.Status == "done" {
			t.Fatalf("done task leaked into default project context: %#v", got)
		}
	}
	want := []string{"TASK-001", "TASK-003", "TASK-002"}
	if len(ids) != len(want) {
		t.Fatalf("ids = %#v, want %#v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("ids = %#v, want %#v", ids, want)
		}
	}
}

func TestProjectContextTasksIncludeResolvedAndLimit(t *testing.T) {
	tasks := []store.Task{
		{ID: "TASK-003", Status: "done", Priority: "high"},
		{ID: "TASK-001", Status: "in_progress", Priority: "high"},
		{ID: "TASK-002", Status: "pending", Priority: "high"},
	}

	got := projectContextTasks(tasks, 2, true)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].ID != "TASK-001" || got[1].ID != "TASK-002" {
		t.Fatalf("unexpected ordering/limit: %#v", got)
	}
}
