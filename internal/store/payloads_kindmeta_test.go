// Package store — unit tests for kindMeta (dedup helpers).
package store

import (
	"testing"
)

// ── kindMeta (dedup helpers) ──────────────────────────────────────────────────

func TestKindMeta_Tasks(t *testing.T) {
	c := &Client{projectID: "test-proj"}
	coll, idField, labelField, err := kindMeta(c, "tasks")
	if err != nil {
		t.Fatalf("kindMeta(tasks): %v", err)
	}
	if coll == "" {
		t.Error("coll must not be empty")
	}
	if idField != "task_id" {
		t.Errorf("idField: got %q, want %q", idField, "task_id")
	}
	if labelField != "title" {
		t.Errorf("labelField: got %q, want %q", labelField, "title")
	}
}

func TestKindMeta_Decisions(t *testing.T) {
	c := &Client{projectID: "test-proj"}
	coll, idField, labelField, err := kindMeta(c, "decisions")
	if err != nil {
		t.Fatalf("kindMeta(decisions): %v", err)
	}
	if coll == "" {
		t.Error("coll must not be empty")
	}
	if idField != "" {
		t.Errorf("decisions have no short ID field, got %q", idField)
	}
	if labelField != "text" {
		t.Errorf("labelField: got %q, want %q", labelField, "text")
	}
}

func TestKindMeta_Rejections(t *testing.T) {
	c := &Client{projectID: "test-proj"}
	_, _, labelField, err := kindMeta(c, "rejections")
	if err != nil {
		t.Fatalf("kindMeta(rejections): %v", err)
	}
	if labelField != "approach" {
		t.Errorf("labelField: got %q, want %q", labelField, "approach")
	}
}

func TestKindMeta_Discussions(t *testing.T) {
	c := &Client{projectID: "test-proj"}
	_, idField, labelField, err := kindMeta(c, "discussions")
	if err != nil {
		t.Fatalf("kindMeta(discussions): %v", err)
	}
	if idField != "disc_id" {
		t.Errorf("idField: got %q, want %q", idField, "disc_id")
	}
	if labelField != "topic" {
		t.Errorf("labelField: got %q, want %q", labelField, "topic")
	}
}

func TestKindMeta_Notes(t *testing.T) {
	c := &Client{projectID: "test-proj"}
	_, _, labelField, err := kindMeta(c, "notes")
	if err != nil {
		t.Fatalf("kindMeta(notes): %v", err)
	}
	if labelField != "text" {
		t.Errorf("labelField: got %q, want %q", labelField, "text")
	}
}

func TestKindMeta_Bugs(t *testing.T) {
	c := &Client{projectID: "test-proj"}
	_, idField, labelField, err := kindMeta(c, "bugs")
	if err != nil {
		t.Fatalf("kindMeta(bugs): %v", err)
	}
	if idField != "bug_id" {
		t.Errorf("idField: got %q, want %q", idField, "bug_id")
	}
	if labelField != "title" {
		t.Errorf("labelField: got %q, want %q", labelField, "title")
	}
}

func TestKindMeta_Unknown_Error(t *testing.T) {
	c := &Client{projectID: "test-proj"}
	_, _, _, err := kindMeta(c, "widgets")
	if err == nil {
		t.Fatal("expected error for unknown kind, got nil")
	}
}
