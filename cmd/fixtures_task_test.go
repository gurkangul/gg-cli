// Package cmd — store-down tests for task commands + task ref helpers.
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

func TestTaskDone_StoreDown(t *testing.T) {
	setupGGDir(t)
	_, _, err := execCmd(t, "task", "done", "TASK-001", "shipped the feature")
	if err == nil {
		t.Fatal("expected error when Qdrant is down")
	}
	ee, ok := err.(*ExitError)
	if !ok {
		t.Fatalf("expected *ExitError, got %T: %v", err, err)
	}
	if ee.Code != ExitStoreDown {
		t.Errorf("expected ExitStoreDown(%d), got %d", ExitStoreDown, ee.Code)
	}
}

func TestTaskBlock_StoreDown(t *testing.T) {
	setupGGDir(t)
	_, _, err := execCmd(t, "task", "block", "TASK-001", "waiting on infra team")
	if err == nil {
		t.Fatal("expected error when Qdrant is down")
	}
	ee, ok := err.(*ExitError)
	if !ok {
		t.Fatalf("expected *ExitError, got %T: %v", err, err)
	}
	if ee.Code != ExitStoreDown {
		t.Errorf("expected ExitStoreDown(%d), got %d", ExitStoreDown, ee.Code)
	}
}

func TestTaskList_StoreDown(t *testing.T) {
	setupGGDir(t)
	_, _, err := execCmd(t, "task", "list")
	if err == nil {
		t.Fatal("expected error when Qdrant is down")
	}
	ee, ok := err.(*ExitError)
	if !ok {
		t.Fatalf("expected *ExitError, got %T: %v", err, err)
	}
	if ee.Code != ExitStoreDown {
		t.Errorf("expected ExitStoreDown(%d), got %d", ExitStoreDown, ee.Code)
	}
}

func TestTaskGet_StoreDown(t *testing.T) {
	setupGGDir(t)
	_, _, err := execCmd(t, "task", "get", "TASK-001")
	if err == nil {
		t.Fatal("expected error when Qdrant is down")
	}
	ee, ok := err.(*ExitError)
	if !ok {
		t.Fatalf("expected *ExitError, got %T: %v", err, err)
	}
	if ee.Code != ExitStoreDown {
		t.Errorf("expected ExitStoreDown(%d), got %d", ExitStoreDown, ee.Code)
	}
}

func TestTaskDeps_StoreDown(t *testing.T) {
	setupGGDir(t)
	_, _, err := execCmd(t, "task", "deps", "TASK-001")
	if err == nil {
		t.Fatal("expected error when Qdrant is down")
	}
	ee, ok := err.(*ExitError)
	if !ok {
		t.Fatalf("expected *ExitError, got %T: %v", err, err)
	}
	if ee.Code != ExitStoreDown {
		t.Errorf("expected ExitStoreDown(%d), got %d", ExitStoreDown, ee.Code)
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
