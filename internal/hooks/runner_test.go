package hooks_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gurkangul/gg-cli/internal/hooks"
)

func setupHookDir(t *testing.T, scripts map[string]string) string {
	t.Helper()
	root := t.TempDir()
	hookDir := filepath.Join(root, ".gg", "hooks", "task-done.d")
	if err := os.MkdirAll(hookDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for name, body := range scripts {
		path := filepath.Join(hookDir, name)
		if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
			t.Fatalf("write hook %s: %v", name, err)
		}
	}
	return filepath.Join(root, ".gg")
}

func TestRunHooks_NoDir(t *testing.T) {
	ggDir := t.TempDir() // no hooks subdir
	results, err := hooks.RunHooks(ggDir, "task-done", nil, false, &bytes.Buffer{})
	if err != nil {
		t.Errorf("expected nil error for missing hook dir, got %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected no results, got %d", len(results))
	}
}

func TestRunHooks_Success(t *testing.T) {
	ggDir := setupHookDir(t, map[string]string{
		"01-ok.sh": "echo hook ran",
	})
	var out bytes.Buffer
	results, err := hooks.RunHooks(ggDir, "task-done", map[string]string{"GG_TASK_ID": "TASK-001"}, false, &out)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].ExitCode != 0 {
		t.Errorf("expected exit 0, got %d", results[0].ExitCode)
	}
}

func TestRunHooks_WarnOnly(t *testing.T) {
	ggDir := setupHookDir(t, map[string]string{
		"01-fail.sh": "exit 1",
		"02-ok.sh":   "echo second ran",
	})
	var out bytes.Buffer
	results, err := hooks.RunHooks(ggDir, "task-done", nil, false, &out)
	if err != nil {
		t.Errorf("warn-only: expected nil error on hook failure, got %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].ExitCode == 0 {
		t.Error("expected first hook to fail")
	}
	if !strings.Contains(out.String(), "advisory hook 01-fail.sh reported findings") {
		t.Fatalf("warn-only output should describe advisory findings, got:\n%s", out.String())
	}
	if strings.Contains(out.String(), "hook 01-fail.sh failed") {
		t.Fatalf("warn-only advisory output should not use blocking failure wording, got:\n%s", out.String())
	}
	// Second hook should still run in warn-only mode.
	if results[1].ExitCode != 0 {
		t.Errorf("expected second hook to succeed, got exit %d", results[1].ExitCode)
	}
}

func TestRunHooks_Strict(t *testing.T) {
	ggDir := setupHookDir(t, map[string]string{
		"01-fail.sh": "exit 2",
		"02-ok.sh":   "echo should not run",
	})
	var out bytes.Buffer
	results, err := hooks.RunHooks(ggDir, "task-done", nil, true, &out)
	if err == nil {
		t.Error("strict mode: expected error on hook failure, got nil")
	}
	// Strict mode aborts after first failure — second hook not executed.
	if len(results) != 1 {
		t.Errorf("strict mode: expected 1 result (abort after first), got %d", len(results))
	}
}

func TestRunHooks_NonExecutableSkipped(t *testing.T) {
	root := t.TempDir()
	hookDir := filepath.Join(root, ".gg", "hooks", "task-done.d")
	if err := os.MkdirAll(hookDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Write a non-executable script.
	if err := os.WriteFile(filepath.Join(hookDir, "01-nope.sh"), []byte("#!/bin/sh\nexit 1\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	ggDir := filepath.Join(root, ".gg")
	results, err := hooks.RunHooks(ggDir, "task-done", nil, true, &bytes.Buffer{})
	if err != nil {
		t.Errorf("non-executable hook should be skipped, got error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results (non-executable skipped), got %d", len(results))
	}
}
