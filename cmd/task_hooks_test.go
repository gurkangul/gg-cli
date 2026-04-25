package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/gurkangul/gg-cli/internal/hooks"
)

// TestGGInsideHookPropagation verifies the TASK-308 regression fix: the
// taskHookEnv function must include GG_INSIDE_HOOK=1, and that value must
// reach the hook subprocess via RunHooks so scripts can detect they are
// running nested under gg and skip re-entrant calls.
//
// This is the mechanism documented in docs/hook-env-vars.md §Variable
// propagation across process boundaries.
func TestGGInsideHookPropagation(t *testing.T) {
	// ── AC-3a: taskHookEnv sets GG_INSIDE_HOOK=1 ───────────────────────────
	env := taskHookEnv("TASK-999", "test summary", "proj-abc")
	if got, ok := env["GG_INSIDE_HOOK"]; !ok || got != "1" {
		t.Errorf("taskHookEnv: GG_INSIDE_HOOK = %q (want %q)", got, "1")
	}
	// Also assert the other four injected vars are present and non-empty.
	for _, key := range []string{"GG_TASK_ID", "GG_TASK_SUMMARY", "GG_PROJECT_ID"} {
		if env[key] == "" {
			t.Errorf("taskHookEnv: %s is empty", key)
		}
	}

	// ── AC-3b: RunHooks propagates GG_INSIDE_HOOK into the child process ───
	// Write a hook that exits 1 when GG_INSIDE_HOOK != "1".
	root := t.TempDir()
	hookDir := filepath.Join(root, ".gg", "hooks", "pre-task-done.d")
	if err := os.MkdirAll(hookDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	script := `#!/bin/sh
if [ "${GG_INSIDE_HOOK:-0}" != "1" ]; then
  echo "GG_INSIDE_HOOK not set or not 1: ${GG_INSIDE_HOOK}" >&2
  exit 1
fi
`
	scriptPath := filepath.Join(hookDir, "10-check-inside-hook.sh")
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	ggDir := filepath.Join(root, ".gg")
	var out bytes.Buffer
	results, err := hooks.RunHooks(ggDir, "pre-task-done", env, true, &out)
	if err != nil {
		t.Errorf("RunHooks: GG_INSIDE_HOOK not propagated to child — hook failed: %v\n%s", err, out.String())
	}
	if len(results) == 1 && results[0].ExitCode != 0 {
		t.Errorf("hook script saw GG_INSIDE_HOOK != 1: %s", results[0].Output)
	}
}
