// Package store — unit tests for taskFromPayload deserializer.
package store

import (
	"testing"

	"github.com/qdrant/go-client/qdrant"
)

// ── taskFromPayload ───────────────────────────────────────────────────────────

func TestTaskFromPayload_Full(t *testing.T) {
	pay, err := qdrant.TryValueMap(map[string]any{
		"task_id":      "TASK-007",
		"title":        "implement auth",
		"detail":       "JWT-based auth flow",
		"status":       "in_progress",
		"priority":     "high",
		"depends_on":   []any{"TASK-001"},
		"tags":         []any{"auth", "security"},
		"block_reason": "",
		"done_summary": "",
		"author":       "developer",
		"created_at":   "2026-01-01T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("TryValueMap: %v", err)
	}

	task := taskFromPayload(pay)

	if task.ID != "TASK-007" {
		t.Errorf("ID: got %q, want %q", task.ID, "TASK-007")
	}
	if task.Title != "implement auth" {
		t.Errorf("Title: got %q", task.Title)
	}
	if task.Detail != "JWT-based auth flow" {
		t.Errorf("Detail: got %q", task.Detail)
	}
	if task.Status != "in_progress" {
		t.Errorf("Status: got %q", task.Status)
	}
	if task.Priority != "high" {
		t.Errorf("Priority: got %q", task.Priority)
	}
	if len(task.DependsOn) != 1 || task.DependsOn[0] != "TASK-001" {
		t.Errorf("DependsOn: got %v", task.DependsOn)
	}
	if len(task.Tags) != 2 || task.Tags[0] != "auth" || task.Tags[1] != "security" {
		t.Errorf("Tags: got %v", task.Tags)
	}
	if task.Author != "developer" {
		t.Errorf("Author: got %q", task.Author)
	}
	if task.CreatedAt != "2026-01-01T00:00:00Z" {
		t.Errorf("CreatedAt: got %q", task.CreatedAt)
	}
}

func TestTaskFromPayload_Empty(t *testing.T) {
	// Nil/missing fields must not panic; they should produce zero values.
	pay := map[string]*qdrant.Value{}
	task := taskFromPayload(pay)
	if task.ID != "" {
		t.Errorf("expected empty ID, got %q", task.ID)
	}
	if task.Status != "" {
		t.Errorf("expected empty status, got %q", task.Status)
	}
	if task.DependsOn != nil {
		t.Errorf("expected nil DependsOn, got %v", task.DependsOn)
	}
}
