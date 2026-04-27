package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gurkangul/gg-cli/internal/orchestrator/spawn"
)

// writeTestPanesJSON writes a panes.json with a single entry to spawnDir.
func writeTestPanesJSON(t *testing.T, spawnDir, taskID, surfaceID string) {
	t.Helper()
	if err := os.MkdirAll(spawnDir, 0o700); err != nil {
		t.Fatalf("mkdir spawn dir: %v", err)
	}
	panes := []spawn.WorkerPane{{
		SurfaceID: surfaceID,
		TaskID:    taskID,
		Agent:     "gsd",
		SpawnedAt: time.Now().UTC(),
	}}
	b, _ := json.MarshalIndent(panes, "", "  ")
	p := filepath.Join(spawnDir, spawn.PanesFile)
	if err := os.WriteFile(p, append(b, '\n'), 0o600); err != nil {
		t.Fatalf("write panes.json: %v", err)
	}
}

// readTestPanesJSON reads panes from the spawn dir (returns nil if absent).
func readTestPanesJSON(t *testing.T, spawnDir string) []spawn.WorkerPane {
	t.Helper()
	p := filepath.Join(spawnDir, spawn.PanesFile)
	b, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatalf("read panes.json: %v", err)
	}
	var panes []spawn.WorkerPane
	if err := json.Unmarshal(b, &panes); err != nil {
		t.Fatalf("parse panes.json: %v", err)
	}
	return panes
}

// TestCloseWorkerPaneForTask_ClosesAndRemovesEntry verifies that when a panes.json
// entry exists for a task, closeWorkerPaneForTask closes the terminal surface
// and removes the entry.
func TestCloseWorkerPaneForTask_ClosesAndRemovesEntry(t *testing.T) {
	rt := t.TempDir()
	spawnDir := filepath.Join(rt, "spawn")
	writeTestPanesJSON(t, spawnDir, "TASK-999", "fake-1")

	// Use the fake terminal backend; fake-1 is not pre-registered in FakeTerminal
	// so Close returns ErrSurfaceNotFound — we just want to verify the panes.json
	// entry is removed regardless (terminal close is best-effort).
	t.Setenv("GG_TERMINAL", "fake")

	var stdout, stderr bytes.Buffer
	closeWorkerPaneForTask(context.Background(), rt, "TASK-999", &stdout, &stderr)

	// panes.json entry must be gone.
	panes := readTestPanesJSON(t, spawnDir)
	for _, p := range panes {
		if p.TaskID == "TASK-999" {
			t.Error("panes.json entry for TASK-999 should have been removed")
		}
	}

	// stderr may contain [pane-lifecycle] from the Close error (fake surface not alive),
	// but that's expected. stdout should contain the success line.
	if !strings.Contains(stdout.String(), "closed pane fake-1 for TASK-999") {
		t.Errorf("expected stdout success line, got: %q", stdout.String())
	}
}

// TestCloseWorkerPaneForTask_NoEntry_Idempotent verifies that when no panes.json
// entry exists for the task, closeWorkerPaneForTask is a no-op with no error output.
func TestCloseWorkerPaneForTask_NoEntry_Idempotent(t *testing.T) {
	rt := t.TempDir()
	// No panes.json written — spawn dir may not even exist.

	t.Setenv("GG_TERMINAL", "fake")

	var stdout, stderr bytes.Buffer
	closeWorkerPaneForTask(context.Background(), rt, "TASK-888", &stdout, &stderr)

	// No output expected.
	if stdout.Len() != 0 {
		t.Errorf("expected no stdout, got: %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Errorf("expected no stderr, got: %q", stderr.String())
	}
}

func TestCloseWorkerPaneForTask_RefusesCurrentMasterSurface(t *testing.T) {
	rt := t.TempDir()
	spawnDir := filepath.Join(rt, "spawn")
	writeTestPanesJSON(t, spawnDir, "TASK-666", "master-surface")

	t.Setenv("GG_TERMINAL", "fake")
	t.Setenv("GG_SURFACE_ID", "master-surface")

	var stdout, stderr bytes.Buffer
	closeWorkerPaneForTask(context.Background(), rt, "TASK-666", &stdout, &stderr)

	if !strings.Contains(stderr.String(), "refusing to close protected master surface master-surface") {
		t.Fatalf("expected protected master warning, got stderr=%q", stderr.String())
	}
	if !strings.Contains(stdout.String(), "removed pane registry entry master-surface for TASK-666") {
		t.Fatalf("expected registry-only cleanup, got stdout=%q", stdout.String())
	}
	for _, p := range readTestPanesJSON(t, spawnDir) {
		if p.TaskID == "TASK-666" {
			t.Fatal("protected stale pane registry entry should still be removed")
		}
	}
}

// TestCloseWorkerPaneForTask_AlreadyRemovedEntry_Idempotent verifies that calling
// closeWorkerPaneForTask twice (second call finds no entry) does not error.
func TestCloseWorkerPaneForTask_AlreadyRemovedEntry_Idempotent(t *testing.T) {
	rt := t.TempDir()
	spawnDir := filepath.Join(rt, "spawn")
	writeTestPanesJSON(t, spawnDir, "TASK-777", "fake-7")

	t.Setenv("GG_TERMINAL", "fake")

	var stdout1, stderr1 bytes.Buffer
	closeWorkerPaneForTask(context.Background(), rt, "TASK-777", &stdout1, &stderr1)

	// Second call — entry is gone, should be a no-op.
	var stdout2, stderr2 bytes.Buffer
	closeWorkerPaneForTask(context.Background(), rt, "TASK-777", &stdout2, &stderr2)

	if stdout2.Len() != 0 {
		t.Errorf("second call: expected no stdout, got: %q", stdout2.String())
	}
	if stderr2.Len() != 0 {
		t.Errorf("second call: expected no stderr, got: %q", stderr2.String())
	}
}

// TestCloseWorkerPaneForTask_WrongTask_NotRemoved verifies that a panes.json entry
// for a different task is not affected.
func TestCloseWorkerPaneForTask_WrongTask_NotRemoved(t *testing.T) {
	rt := t.TempDir()
	spawnDir := filepath.Join(rt, "spawn")
	writeTestPanesJSON(t, spawnDir, "TASK-111", "fake-11")

	t.Setenv("GG_TERMINAL", "fake")

	var stdout, stderr bytes.Buffer
	closeWorkerPaneForTask(context.Background(), rt, "TASK-999", &stdout, &stderr)

	// TASK-111's entry must still be there.
	panes := readTestPanesJSON(t, spawnDir)
	found := false
	for _, p := range panes {
		if p.TaskID == "TASK-111" {
			found = true
		}
	}
	if !found {
		t.Error("TASK-111 panes.json entry should be untouched when closing TASK-999")
	}
	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Errorf("expected no output for task with no entry, got stdout=%q stderr=%q",
			stdout.String(), stderr.String())
	}
}
