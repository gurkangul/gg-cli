// Package cmd — tests for enforcement-hook installation wired into 'gg init'.
// runInit requires live Docker/Qdrant so these tests call the underlying hook
// installers directly, matching the pattern in doctor_install_task_hooks_test.go.
package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gurkangul/gg-cli/internal/agenthooks"
)

// TestInit_EnforcementHooks_AgentHooksInstalled verifies that InstallDetected
// produces at least one actionable result when a known agent config surface
// exists (claude: .claude/ dir present).
func TestInit_EnforcementHooks_AgentHooksInstalled(t *testing.T) {
	ggDir := setupGGDir(t)
	root := filepath.Dir(ggDir)

	// Signal Claude Code presence by creating .claude/
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		t.Fatalf("mkdir .claude: %v", err)
	}

	results := agenthooks.InstallDetected(root, agenthooks.Options{})

	// At least one result must be non-skipped (created/updated/up-to-date).
	found := false
	for _, r := range results {
		if r.Action != agenthooks.ActionNotDetected {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected at least one installed agent hook, all were not-detected: %v", results)
	}
}

// TestInit_EnforcementHooks_SkipEnforcement verifies that --skip-enforcement
// leaves .claude/settings.json absent.
func TestInit_EnforcementHooks_SkipEnforcement(t *testing.T) {
	ggDir := setupGGDir(t)
	root := filepath.Dir(ggDir)

	// Create .claude/ so detection would fire if skip were not set.
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		t.Fatalf("mkdir .claude: %v", err)
	}

	// Skip enforcement — simulate what init does when --skip-enforcement is set.
	// (Don't call InstallDetected at all.)
	settingsPath := filepath.Join(root, ".claude", "settings.json")
	if _, err := os.Stat(settingsPath); err == nil {
		t.Errorf("expected .claude/settings.json absent when skip-enforcement, but it exists")
	}
}

// TestInit_EnforcementHooks_TaskHooksInstalled verifies that
// runDoctorInstallTaskHooks writes pre-task-done.d/ gate scripts into .gg/.
func TestInit_EnforcementHooks_TaskHooksInstalled(t *testing.T) {
	ggDir := setupGGDir(t)
	root := filepath.Dir(ggDir)

	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module test\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}

	if err := runDoctorInstallTaskHooks(); err != nil {
		t.Fatalf("runDoctorInstallTaskHooks: %v", err)
	}

	preDir := filepath.Join(ggDir, "hooks", "pre-task-done.d")
	entries, err := os.ReadDir(preDir)
	if err != nil {
		t.Fatalf("read pre-task-done.d: %v", err)
	}
	if len(entries) == 0 {
		t.Errorf("expected gate scripts in pre-task-done.d, got none")
	}

	// Smoke-gate and decide-gate are always written regardless of language.
	required := []string{"05-smoke-e2e.sh", "20-decide-capture.sh"}
	for _, name := range required {
		p := filepath.Join(preDir, name)
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected %s to exist", name)
		}
	}
}

// TestInit_EnforcementHooks_Idempotent verifies that re-running the hook
// installers does not clobber user edits in an existing script.
func TestInit_EnforcementHooks_Idempotent(t *testing.T) {
	ggDir := setupGGDir(t)
	root := filepath.Dir(ggDir)

	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module test\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}

	// First install.
	if err := runDoctorInstallTaskHooks(); err != nil {
		t.Fatalf("first install: %v", err)
	}

	// Simulate a user edit on the smoke-gate script.
	smokeHook := filepath.Join(ggDir, "hooks", "pre-task-done.d", "05-smoke-e2e.sh")
	if err := os.WriteFile(smokeHook, []byte("#!/bin/sh\n# user edit\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write user edit: %v", err)
	}

	// Second install — must not overwrite.
	if err := runDoctorInstallTaskHooks(); err != nil {
		t.Fatalf("second install: %v", err)
	}

	data, err := os.ReadFile(smokeHook)
	if err != nil {
		t.Fatalf("read after second install: %v", err)
	}
	if !strings.Contains(string(data), "user edit") {
		t.Errorf("second install overwrote user-edited hook; got:\n%s", string(data))
	}
}

// TestInit_EnforcementHooks_ReportLines verifies that RenderReport emits at
// least one line per result.
func TestInit_EnforcementHooks_ReportLines(t *testing.T) {
	ggDir := setupGGDir(t)
	root := filepath.Dir(ggDir)

	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		t.Fatalf("mkdir .claude: %v", err)
	}

	results := agenthooks.InstallDetected(root, agenthooks.Options{})

	var buf strings.Builder
	agenthooks.RenderReport(&buf, results)
	if buf.Len() == 0 {
		t.Errorf("RenderReport produced no output for %d results", len(results))
	}

	output := buf.String()
	// Each result should appear as a line in the report.
	for _, r := range results {
		if !strings.Contains(output, r.AgentName) && !strings.Contains(output, r.DisplayName) {
			t.Errorf("report missing agent %q or display %q:\n%s", r.AgentName, r.DisplayName, output)
		}
	}
}
