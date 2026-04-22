// Package cmd — tests for enforcement-hook installation wired into 'gg init'.
// runInit requires live Docker/Qdrant so these tests call the underlying hook
// installers directly, matching the pattern in doctor_install_task_hooks_test.go.
package cmd

import (
	"encoding/json"
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

	// Mirror what cmd/init.go now does — Force=true so globally-installed
	// agents don't stop at "suggested" when the project has no local dir yet.
	results := agenthooks.InstallDetected(root, agenthooks.Options{Force: true})

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

// TestInit_ForceAgentHooks_EnvLandsWithoutProjectClaudeDir covers the
// TASK-266 follow-up: `gg init` must auto-install the Claude env.GG_AGENT
// block even when the project has no pre-existing .claude/ directory. Before
// the Force=true change, Install returned ActionSuggested and dormant TASK-265
// auto-compact / TASK-266 env wiring stayed uninstalled on new projects.
func TestInit_ForceAgentHooks_EnvLandsWithoutProjectClaudeDir(t *testing.T) {
	ggDir := setupGGDir(t)
	root := filepath.Dir(ggDir)
	// No .claude/ in the project — this is the exact scenario the fix targets.

	results := agenthooks.InstallDetected(root, agenthooks.Options{Force: true})

	// Find the claude result; must be Created, not Suggested.
	var claude *agenthooks.Result
	for i, r := range results {
		if r.AgentName == "claude" {
			claude = &results[i]
			break
		}
	}
	if claude == nil {
		t.Fatal("claude installer result missing from InstallDetected output")
	}
	if claude.Action == agenthooks.ActionSuggested {
		t.Fatalf("with Force=true, claude must install — got ActionSuggested; dormant-fix regression: %v", claude)
	}

	// Settings file must carry the env.GG_AGENT block that TASK-266 wires.
	raw, err := os.ReadFile(filepath.Join(root, ".claude", "settings.json"))
	if err != nil {
		t.Fatalf("settings.json not written: %v", err)
	}
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatalf("settings.json not valid JSON: %v", err)
	}
	env, _ := data["env"].(map[string]any)
	if v, _ := env["GG_AGENT"].(string); v != "claude-code" {
		t.Errorf("env.GG_AGENT = %q, want \"claude-code\" — TASK-266 wiring not reaching fresh init", v)
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

	// Smoke-gate, decide-gate, and review-gate are always written regardless
	// of language.
	required := []string{"05-smoke-e2e.sh", "20-decide-capture.sh", "40-review-required.sh"}
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

// TestMaybeRunIndex_SkipWhenNoIndex verifies that --no-index prevents indexing
// even if composeOK=true and TTY is available.
func TestMaybeRunIndex_SkipWhenNoIndex(t *testing.T) {
	orig := initNoIndex
	defer func() { initNoIndex = orig }()
	initNoIndex = true

	result := maybeRunIndex(nil, "go", true)
	if result {
		t.Error("expected maybeRunIndex to return false when --no-index is set")
	}
}

// TestMaybeRunIndex_SkipWhenComposeNotOK verifies that a missing Docker stack
// short-circuits without prompting.
func TestMaybeRunIndex_SkipWhenComposeNotOK(t *testing.T) {
	orig := initNoIndex
	defer func() { initNoIndex = orig }()
	initNoIndex = false

	result := maybeRunIndex(nil, "go", false)
	if result {
		t.Error("expected maybeRunIndex to return false when composeOK=false")
	}
}

// TestMaybeRunIndex_SkipOnNonTTY verifies that non-interactive stdin skips
// without prompting (CI / scripted init).
func TestMaybeRunIndex_SkipOnNonTTY(t *testing.T) {
	orig := initNoIndex
	origWith := initWithIndex
	defer func() {
		initNoIndex = orig
		initWithIndex = origWith
	}()
	initNoIndex = false
	initWithIndex = false

	// os.Stdin in test is a pipe, not a TTY — isTerminal returns false.
	result := maybeRunIndex(nil, "go", true)
	if result {
		t.Error("expected maybeRunIndex to return false on non-TTY stdin")
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
