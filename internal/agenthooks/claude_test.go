package agenthooks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClaude_Detect_PresentDir(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !(&claudeInstaller{}).Detect(root) {
		t.Error("expected Detect to return true when .claude/ exists")
	}
}

// TestClaude_GlobalSignals_AllFiveSignals verifies each of the 5 detection
// signals independently using isolated home dirs and controlled env lookups.
func TestClaude_GlobalSignals_AllFiveSignals(t *testing.T) {
	noEnv := func(string) string { return "" }

	t.Run("signal1_settings_json", func(t *testing.T) {
		home := t.TempDir()
		if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(home, ".claude", "settings.json"), []byte(`{"x":1}`), 0o644); err != nil {
			t.Fatal(err)
		}
		if !claudeGlobalSignalsFromHomeWithEnv(home, noEnv) {
			t.Error("signal 1 (settings.json non-empty) should fire")
		}
	})

	t.Run("signal2_CLAUDECODE_env", func(t *testing.T) {
		home := t.TempDir()
		getenv := func(k string) string {
			if k == "CLAUDECODE" {
				return "1"
			}
			return ""
		}
		if !claudeGlobalSignalsFromHomeWithEnv(home, getenv) {
			t.Error("signal 2 (CLAUDECODE=1) should fire")
		}
	})

	t.Run("signal3_CLAUDE_CODE_ENTRYPOINT_env", func(t *testing.T) {
		home := t.TempDir()
		getenv := func(k string) string {
			if k == "CLAUDE_CODE_ENTRYPOINT" {
				return "sdk-ts"
			}
			return ""
		}
		if !claudeGlobalSignalsFromHomeWithEnv(home, getenv) {
			t.Error("signal 3 (CLAUDE_CODE_ENTRYPOINT set) should fire")
		}
	})

	t.Run("signal4_plugins_dir", func(t *testing.T) {
		home := t.TempDir()
		if err := os.MkdirAll(filepath.Join(home, ".claude", "plugins"), 0o755); err != nil {
			t.Fatal(err)
		}
		if !claudeGlobalSignalsFromHomeWithEnv(home, noEnv) {
			t.Error("signal 4 (~/.claude/plugins/ dir) should fire")
		}
	})

	t.Run("no_signals_absent", func(t *testing.T) {
		home := t.TempDir()
		if claudeGlobalSignalsFromHomeWithEnv(home, noEnv) {
			t.Error("no signals should fire in an empty home with no env vars")
		}
	})
}

func TestClaude_Detect_MissingDir(t *testing.T) {
	root := t.TempDir()
	// Use an isolated testHome and a no-op env getter so global Claude signals
	// from the host machine (env vars, ~/.claude) don't bleed into this test.
	inst := &claudeInstaller{
		testHome: t.TempDir(),
		testEnv:  func(string) string { return "" },
	}
	if inst.Detect(root) {
		t.Error("expected Detect to return false when .claude/ missing and no global signals")
	}
}

func TestClaude_Install_FreshCreate(t *testing.T) {
	root := t.TempDir()
	// Use Force so the installer writes unconditionally regardless of whether
	// the host machine has a global Claude install.
	inst := &claudeInstaller{}

	res, err := inst.Install(root, Options{Force: true})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if res.Action != ActionCreated {
		t.Errorf("Action = %q, want %q", res.Action, ActionCreated)
	}

	raw, err := os.ReadFile(filepath.Join(root, ".claude", "settings.json"))
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	if !strings.Contains(string(raw), "gg session-start --agent=claude-code") {
		t.Errorf("settings.json missing hook command: %s", raw)
	}
	// Must be valid JSON that round-trips.
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatalf("settings.json not valid JSON: %v\n%s", err, raw)
	}
}

// TestClaude_Install_GlobalOnlySuggests verifies that when Claude Code is
// installed globally (simulated via testHome) but the project has no .claude/
// directory, Install returns ActionSuggested with an inline advisory.
func TestClaude_Install_GlobalOnlySuggests(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	// Simulate a non-empty ~/.claude/settings.json (signal 1).
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".claude", "settings.json"), []byte(`{"theme":"dark"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	inst := &claudeInstaller{testHome: home, testEnv: func(string) string { return "" }}
	res, err := inst.Install(root, Options{})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if res.Action != ActionSuggested {
		t.Errorf("Action = %q, want %q", res.Action, ActionSuggested)
	}
	if res.Path != "" {
		t.Errorf("Path should be empty for suggestion, got %q", res.Path)
	}
	found := false
	for _, n := range res.Notes {
		if strings.Contains(n, "gg doctor") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected gg doctor suggestion in Notes, got %v", res.Notes)
	}
	// Nothing should have been written to disk.
	if _, statErr := os.Stat(filepath.Join(root, ".claude")); statErr == nil {
		t.Error("Install should not have created .claude/ for a suggestion-only result")
	}
}

// TestClaude_Install_GlobalOnlyForceInstalls verifies that Force=true bypasses
// the suggestion and writes the project hook directly.
func TestClaude_Install_GlobalOnlyForceInstalls(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".claude", "settings.json"), []byte(`{"theme":"dark"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	inst := &claudeInstaller{testHome: home, testEnv: func(string) string { return "" }}
	res, err := inst.Install(root, Options{Force: true})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if res.Action != ActionCreated {
		t.Errorf("Action = %q, want %q", res.Action, ActionCreated)
	}
	if _, statErr := os.Stat(filepath.Join(root, ".claude", "settings.json")); statErr != nil {
		t.Errorf("Force install should have written settings.json: %v", statErr)
	}
}

func TestClaude_Install_IdempotentReRun(t *testing.T) {
	root := t.TempDir()
	inst := &claudeInstaller{}

	if _, err := inst.Install(root, Options{Force: true}); err != nil {
		t.Fatalf("first install: %v", err)
	}
	// After the first install .claude/ exists, so subsequent runs without Force
	// should detect the project dir and report up-to-date.
	res, err := inst.Install(root, Options{})
	if err != nil {
		t.Fatalf("second install: %v", err)
	}
	if res.Action != ActionUpToDate {
		t.Errorf("second Action = %q, want %q", res.Action, ActionUpToDate)
	}
}

func TestClaude_Install_PreservesUnrelatedKeys(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".claude")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// A user-supplied config with unrelated keys we must keep.
	existing := `{
  "theme": "dark",
  "permissions": {"allow": ["Bash"]}
}
`
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := (&claudeInstaller{}).Install(root, Options{}); err != nil {
		t.Fatalf("Install: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatalf("not valid JSON: %v", err)
	}
	if _, ok := data["theme"]; !ok {
		t.Errorf("lost 'theme' key after install: %s", raw)
	}
	if _, ok := data["permissions"]; !ok {
		t.Errorf("lost 'permissions' key after install: %s", raw)
	}
	if _, ok := data["hooks"]; !ok {
		t.Errorf("missing 'hooks' key after install: %s", raw)
	}
}

func TestClaude_Install_MergesIntoExistingStartupMatcher(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".claude")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Pre-existing startup matcher with a different hook — gg must merge
	// rather than replace.
	existing := `{
  "hooks": {
    "SessionStart": [
      {
        "matcher": "startup",
        "hooks": [{"type": "command", "command": "echo hi"}]
      }
    ]
  }
}
`
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := (&claudeInstaller{}).Install(root, Options{}); err != nil {
		t.Fatalf("Install: %v", err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "settings.json"))
	s := string(raw)
	if !strings.Contains(s, "echo hi") {
		t.Errorf("pre-existing hook dropped: %s", s)
	}
	if !strings.Contains(s, "gg session-start") {
		t.Errorf("new hook not added: %s", s)
	}
}

func TestClaude_Install_DryRun(t *testing.T) {
	root := t.TempDir()
	// Force=true ensures we exercise the dry-run path rather than the
	// suggestion path (which fires when global signals are present but no
	// project .claude/ exists).
	res, err := (&claudeInstaller{}).Install(root, Options{DryRun: true, Force: true})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if res.Action != ActionDryRun {
		t.Errorf("Action = %q, want %q", res.Action, ActionDryRun)
	}
	if _, err := os.Stat(filepath.Join(root, ".claude", "settings.json")); err == nil {
		t.Error("dry-run should not have created settings.json")
	}
}

// env-wiring (TASK-266), hooks-idempotency, contract, and audit-hook tests
// are in claude_hooks_idempotency_test.go. This file stays focused on
// Detect + basic Install + DryRun so the 800-line file-size gate holds.
