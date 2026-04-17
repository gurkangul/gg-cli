// Package cmd — tests for the pre-done verify gate wired into `gg task done`.
package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

// writePreTaskDoneHook drops a shell script into .gg/hooks/pre-task-done.d/
// under the test's current ggDir. body is appended after a `#!/bin/sh` shebang.
func writePreTaskDoneHook(t *testing.T, ggDir, name, body string) {
	t.Helper()
	hookDir := filepath.Join(ggDir, "hooks", "pre-task-done.d")
	if err := os.MkdirAll(hookDir, 0o755); err != nil {
		t.Fatalf("mkdir pre-task-done.d: %v", err)
	}
	path := filepath.Join(hookDir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatalf("write hook %s: %v", name, err)
	}
}

// (a) No pre-task-done.d directory → pre-hook stage is a no-op and the command
// proceeds to the store. Since the test fixture points Qdrant at a dead port,
// we expect ExitStoreDown — which proves the pre-hook did not block.
func TestTaskDone_NoPreHookDir_ReachesStore(t *testing.T) {
	setupGGDir(t)
	_, _, err := execCmd(t, "task", "done", "TASK-001", "no pre-hook installed")
	if err == nil {
		t.Fatal("expected error when Qdrant is down")
	}
	ee, ok := err.(*ExitError)
	if !ok {
		t.Fatalf("expected *ExitError, got %T: %v", err, err)
	}
	if ee.Code != ExitStoreDown {
		t.Errorf("expected ExitStoreDown(%d) when no pre-hook dir, got %d", ExitStoreDown, ee.Code)
	}
}

// (b) A pre-hook that exits non-zero MUST block the transition: return
// ExitVerifyFailed, never reach the store. Distinguishable from ExitStoreDown
// so agents know the failure was a gate rejection, not infra.
func TestTaskDone_PreHookFails_BlocksTransition(t *testing.T) {
	ggDir := setupGGDir(t)
	writePreTaskDoneHook(t, ggDir, "01-fail.sh", "echo 'build broken' >&2\nexit 1")
	_, _, err := execCmd(t, "task", "done", "TASK-001", "attempt while build broken")
	if err == nil {
		t.Fatal("expected error: pre-hook should have blocked the transition")
	}
	ee, ok := err.(*ExitError)
	if !ok {
		t.Fatalf("expected *ExitError, got %T: %v", err, err)
	}
	if ee.Code != ExitVerifyFailed {
		t.Errorf("expected ExitVerifyFailed(%d), got %d (msg=%q)", ExitVerifyFailed, ee.Code, ee.Message)
	}
}

// (c) A pre-hook that passes MUST let the command continue. With Qdrant dead
// the expected terminal error is ExitStoreDown — confirming the gate opened.
func TestTaskDone_PreHookPasses_FallsThroughToStore(t *testing.T) {
	ggDir := setupGGDir(t)
	writePreTaskDoneHook(t, ggDir, "01-ok.sh", "echo 'verify ok'\nexit 0")
	_, _, err := execCmd(t, "task", "done", "TASK-001", "build passed")
	if err == nil {
		t.Fatal("expected error when Qdrant is down (pre-hook passed, store still unreachable)")
	}
	ee, ok := err.(*ExitError)
	if !ok {
		t.Fatalf("expected *ExitError, got %T: %v", err, err)
	}
	if ee.Code != ExitStoreDown {
		t.Errorf("expected ExitStoreDown(%d) after pre-hook pass, got %d", ExitStoreDown, ee.Code)
	}
}
