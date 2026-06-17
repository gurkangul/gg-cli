// Package cmd — fresh-project (un-reembedded) tests for task commands + task ref
// helpers. With the embedded SQLite store there is no "store down" state: task
// create persists JSONL-first, reads of a missing task return not-found, reads of
// an un-materialized collection surface a not-found error, and lifecycle
// transitions on a non-existent task fail at the gate/store rather than the
// (removed) Qdrant-down sentinel.
package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

// TestTaskCreate_StoreDown verifies offline-resilience (BUG-030 / TASK-352):
// 'gg task create' must succeed (exit 0) when Qdrant is down, writing to JSONL.
func TestTaskCreate_StoreDown(t *testing.T) {
	ggDir := setupGGDir(t)
	_, _, err := execCmd(t, "task", "create", "--requester=user", "implement rate limiting")
	// AC-2: caller gets exit 0.
	if err != nil {
		t.Fatalf("expected exit 0 on offline task create, got: %v", err)
	}
	// AC-1: JSONL must be written.
	jsonlPath := filepath.Join(ggDir, "brain", "tasks.jsonl")
	data, readErr := os.ReadFile(jsonlPath)
	if readErr != nil {
		t.Fatalf("brain/tasks.jsonl not written: %v", readErr)
	}
	if len(data) == 0 {
		t.Error("brain/tasks.jsonl is empty")
	}
}

func TestTaskDone_FreshProject_Fails(t *testing.T) {
	setupGGDir(t)
	// Completing a task that does not exist on a fresh project must fail (gate or
	// not-found) rather than silently succeed.
	if _, _, err := execCmd(t, "task", "done", "TASK-001", "shipped the feature"); err == nil {
		t.Fatal("expected an error completing a non-existent task on a fresh project")
	}
}

func TestTaskBlock_FreshProject_Fails(t *testing.T) {
	setupGGDir(t)
	if _, _, err := execCmd(t, "task", "block", "TASK-001", "waiting on infra team"); err == nil {
		t.Fatal("expected an error blocking a non-existent task on a fresh project")
	}
}

func TestTaskList_FreshProject_NotMaterialized(t *testing.T) {
	setupGGDir(t)
	// task list reads the tasks collection directly; on a fresh project it is not
	// materialized, so the read surfaces a not-found error.
	if _, _, err := execCmd(t, "task", "list"); err == nil {
		t.Fatal("expected a not-found error listing an un-materialized tasks collection")
	}
}

func TestTaskGet_FreshProject_NotFound(t *testing.T) {
	setupGGDir(t)
	_, _, err := execCmd(t, "task", "get", "TASK-001")
	if err == nil {
		t.Fatal("expected an error fetching a non-existent task")
	}
	ee, ok := err.(*ExitError)
	if !ok {
		t.Fatalf("expected *ExitError, got %T: %v", err, err)
	}
	if ee.Code != ExitNotFound {
		t.Errorf("expected ExitNotFound(%d) for a missing task, got %d", ExitNotFound, ee.Code)
	}
}

func TestTaskDeps_FreshProject_NotFound(t *testing.T) {
	setupGGDir(t)
	_, _, err := execCmd(t, "task", "deps", "TASK-001")
	if err == nil {
		t.Fatal("expected an error resolving deps of a non-existent task")
	}
	ee, ok := err.(*ExitError)
	if !ok {
		t.Fatalf("expected *ExitError, got %T: %v", err, err)
	}
	if ee.Code != ExitNotFound {
		t.Errorf("expected ExitNotFound(%d) for a missing task, got %d", ExitNotFound, ee.Code)
	}
}

func TestNormalizeTaskRef(t *testing.T) {
	// empty is valid (optional field)
	ref, err := normalizeTaskRef("")
	if err != nil || ref != "" {
		t.Errorf("empty normalizeTaskRef: got %q, %v", ref, err)
	}
	// valid task ID
	ref2, err := normalizeTaskRef("task-007")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ref2 != "TASK-007" {
		t.Errorf("expected TASK-007, got %q", ref2)
	}
	// invalid
	if _, err := normalizeTaskRef("not-a-task"); err == nil {
		t.Error("expected error for invalid task ref")
	}
}

func TestParseTaskIDList(t *testing.T) {
	// empty → nil
	ids, err := parseTaskIDList("")
	if err != nil || ids != nil {
		t.Errorf("empty: got %v, %v", ids, err)
	}
	// single valid
	ids2, err := parseTaskIDList("TASK-001")
	if err != nil || len(ids2) != 1 || ids2[0] != "TASK-001" {
		t.Errorf("single: got %v, %v", ids2, err)
	}
	// multiple valid
	ids3, err := parseTaskIDList("TASK-001, TASK-002 , TASK-003")
	if err != nil || len(ids3) != 3 {
		t.Errorf("multiple: got %v, %v", ids3, err)
	}
	// invalid entry
	if _, err := parseTaskIDList("TASK-001,bad-id"); err == nil {
		t.Error("expected error for invalid ID in list")
	}
}
