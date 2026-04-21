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

// TestClaude_Install_WritesEnvGGAgent verifies the TASK-266 fix: fresh
// install must write env.GG_AGENT=claude-code so tool-call subprocesses
// inherit the identity and trigger auto-compact / auto-broadcast /
// origin=agent classification without manual exports.
func TestClaude_Install_WritesEnvGGAgent(t *testing.T) {
	root := t.TempDir()
	if _, err := (&claudeInstaller{}).Install(root, Options{Force: true}); err != nil {
		t.Fatalf("Install: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(root, ".claude", "settings.json"))
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatalf("not valid JSON: %v", err)
	}
	env, ok := data["env"].(map[string]any)
	if !ok {
		t.Fatalf("env block missing: %s", raw)
	}
	if v, _ := env["GG_AGENT"].(string); v != "claude-code" {
		t.Errorf("env.GG_AGENT = %q, want \"claude-code\" — dormant-fix regression", v)
	}
}

// TestClaude_Install_PreservesExistingEnvEntries verifies that user-supplied
// env entries are kept when the installer adds GG_AGENT.
func TestClaude_Install_PreservesExistingEnvEntries(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".claude")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	existing := `{"env": {"CLAUDE_DEBUG": "1", "USER_VAR": "keep-me"}}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := (&claudeInstaller{}).Install(root, Options{}); err != nil {
		t.Fatalf("Install: %v", err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "settings.json"))
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatalf("not valid JSON: %v", err)
	}
	env, _ := data["env"].(map[string]any)
	for k, want := range map[string]string{
		"CLAUDE_DEBUG": "1",
		"USER_VAR":     "keep-me",
		"GG_AGENT":     "claude-code",
	} {
		if got, _ := env[k].(string); got != want {
			t.Errorf("env[%q] = %q, want %q — unrelated env must survive, our key must land", k, got, want)
		}
	}
}

// TestClaude_Install_IdempotentEnvDoesNotDuplicate verifies second run is
// up-to-date (not rewriting) when env.GG_AGENT is already correct.
func TestClaude_Install_IdempotentEnvDoesNotDuplicate(t *testing.T) {
	root := t.TempDir()
	if _, err := (&claudeInstaller{}).Install(root, Options{Force: true}); err != nil {
		t.Fatalf("first install: %v", err)
	}
	res, err := (&claudeInstaller{}).Install(root, Options{})
	if err != nil {
		t.Fatalf("second install: %v", err)
	}
	if res.Action != ActionUpToDate {
		t.Errorf("second Action = %q, want %q (env already present)", res.Action, ActionUpToDate)
	}
	// Surface the env-present note so a human running `gg doctor` sees the
	// status explicitly rather than inferring from silence.
	foundEnvNote := false
	for _, n := range res.Notes {
		if strings.Contains(n, "env.GG_AGENT already present") {
			foundEnvNote = true
		}
	}
	if !foundEnvNote {
		t.Errorf("up-to-date result should note env presence, got notes: %v", res.Notes)
	}
}

// TestClaude_Install_DryRunSurfacesEnvNote verifies dry-run reports the env
// change it would make, consistent with how other hook changes are reported.
func TestClaude_Install_DryRunSurfacesEnvNote(t *testing.T) {
	root := t.TempDir()
	res, err := (&claudeInstaller{}).Install(root, Options{DryRun: true, Force: true})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	found := false
	for _, n := range res.Notes {
		if strings.Contains(n, "would set env.GG_AGENT") {
			found = true
		}
	}
	if !found {
		t.Errorf("dry-run should note env change, got notes: %v", res.Notes)
	}
}

// TestClaude_Install_UserPromptSubmitHook verifies that a fresh install writes
// both the SessionStart and UserPromptSubmit hooks.
func TestClaude_Install_UserPromptSubmitHook(t *testing.T) {
	root := t.TempDir()
	res, err := (&claudeInstaller{}).Install(root, Options{Force: true})
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
	s := string(raw)
	if !strings.Contains(s, "gg inbox") {
		t.Errorf("settings.json missing UserPromptSubmit hook: %s", s)
	}
	if !strings.Contains(s, "UserPromptSubmit") {
		t.Errorf("settings.json missing UserPromptSubmit event: %s", s)
	}
	// Both hooks must be valid JSON.
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatalf("settings.json not valid JSON: %v\n%s", err, raw)
	}
}

// TestClaude_Install_IdempotentBothHooks verifies that running Install twice
// reports UpToDate (no changes) when both hooks are already present.
func TestClaude_Install_IdempotentBothHooks(t *testing.T) {
	root := t.TempDir()
	if _, err := (&claudeInstaller{}).Install(root, Options{Force: true}); err != nil {
		t.Fatalf("first install: %v", err)
	}
	res, err := (&claudeInstaller{}).Install(root, Options{})
	if err != nil {
		t.Fatalf("second install: %v", err)
	}
	if res.Action != ActionUpToDate {
		t.Errorf("second install Action = %q, want %q", res.Action, ActionUpToDate)
	}
	found := false
	for _, n := range res.Notes {
		if strings.Contains(n, "UserPromptSubmit") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected UserPromptSubmit note in up-to-date result, got %v", res.Notes)
	}
}

// TestClaude_Install_AddsUserPromptToExistingSessionStart verifies that when
// only the SessionStart hook exists, a re-run adds UserPromptSubmit and
// reports ActionUpdated.
func TestClaude_Install_AddsUserPromptToExistingSessionStart(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".claude")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Pre-existing settings with only SessionStart.
	existing := `{
  "hooks": {
    "SessionStart": [
      {
        "matcher": "startup",
        "hooks": [{"type": "command", "command": "gg session-start --agent=claude-code"}]
      }
    ]
  }
}
`
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := (&claudeInstaller{}).Install(root, Options{})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if res.Action != ActionUpdated {
		t.Errorf("Action = %q, want %q", res.Action, ActionUpdated)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "settings.json"))
	s := string(raw)
	if !strings.Contains(s, "gg inbox") {
		t.Errorf("UserPromptSubmit hook not added: %s", s)
	}
	if !strings.Contains(s, "gg session-start") {
		t.Errorf("SessionStart hook dropped: %s", s)
	}
}

func TestClaude_Install_AddsPreToolUseGSDGuard(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".claude")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	res, err := (&claudeInstaller{}).Install(root, Options{})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if res.Action == ActionUpToDate {
		t.Fatal("expected install action, got up-to-date")
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "settings.json"))
	s := string(raw)
	if !strings.Contains(s, "gg gsd-guard") {
		t.Errorf("PreToolUse gsd-guard hook not added: %s", s)
	}
	if !strings.Contains(s, claudeEventPreToolUse) {
		t.Errorf("PreToolUse key missing in settings.json: %s", s)
	}
}

func TestClaude_Install_AddsPostToolUseVerifyHook(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".claude")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	res, err := (&claudeInstaller{}).Install(root, Options{})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if res.Action == ActionUpToDate {
		t.Fatal("expected install action, got up-to-date")
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "settings.json"))
	s := string(raw)
	if !strings.Contains(s, "gg verify --file") {
		t.Errorf("PostToolUse verify hook not added: %s", s)
	}
	if !strings.Contains(s, claudeEventPostToolUse) {
		t.Errorf("PostToolUse key missing in settings.json: %s", s)
	}
	if !strings.Contains(s, "Edit|Write|MultiEdit") {
		t.Errorf("PostToolUse matcher missing in settings.json: %s", s)
	}
}

func TestClaude_Install_IdempotentOnVerifyPresent(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".claude")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	inst := &claudeInstaller{}
	if _, err := inst.Install(root, Options{}); err != nil {
		t.Fatal(err)
	}
	res, err := inst.Install(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Action != ActionUpToDate {
		t.Errorf("second Install Action = %q, want %q", res.Action, ActionUpToDate)
	}

	// Verify the hook appears exactly once.
	raw, _ := os.ReadFile(filepath.Join(dir, "settings.json"))
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatalf("parse settings.json: %v", err)
	}
	hooks, _ := data["hooks"].(map[string]any)
	entries, _ := hooks[claudeEventPostToolUse].([]any)
	verifyCount := 0
	for _, e := range entries {
		m, _ := e.(map[string]any)
		inner, _ := m["hooks"].([]any)
		for _, h := range inner {
			hm, _ := h.(map[string]any)
			if cmd, _ := hm["command"].(string); strings.Contains(cmd, "gg verify --file") {
				verifyCount++
			}
		}
	}
	if verifyCount != 1 {
		t.Errorf("expected exactly 1 verify hook, got %d", verifyCount)
	}
}

func TestClaude_Install_AddsVerifyToExistingHooks(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".claude")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Pre-existing settings with all hooks except PostToolUse.
	existing := `{
  "hooks": {
    "SessionStart": [
      {"matcher": "startup", "hooks": [{"type": "command", "command": "gg session-start --agent=claude-code"}]}
    ],
    "UserPromptSubmit": [
      {"matcher": "", "hooks": [{"type": "command", "command": "gg inbox --peek"}]}
    ],
    "PreToolUse": [
      {"matcher": "gsd_plan_milestone", "hooks": [{"type": "command", "command": "gg gsd-guard"}]}
    ]
  }
}
`
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := (&claudeInstaller{}).Install(root, Options{})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if res.Action != ActionUpdated {
		t.Errorf("Action = %q, want %q", res.Action, ActionUpdated)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "settings.json"))
	s := string(raw)
	if !strings.Contains(s, "gg verify --file") {
		t.Errorf("PostToolUse verify hook not added: %s", s)
	}
	// Existing hooks must not be dropped.
	if !strings.Contains(s, "gg session-start") {
		t.Errorf("SessionStart hook dropped: %s", s)
	}
	if !strings.Contains(s, "gg inbox") {
		t.Errorf("UserPromptSubmit hook dropped: %s", s)
	}
	if !strings.Contains(s, "gg gsd-guard") {
		t.Errorf("PreToolUse gsd-guard hook dropped: %s", s)
	}
}

func TestClaude_Install_IdempotentOnGSDGuardPresent(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".claude")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	inst := &claudeInstaller{}
	if _, err := inst.Install(root, Options{}); err != nil {
		t.Fatal(err)
	}
	res, err := inst.Install(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Action != ActionUpToDate {
		t.Errorf("second Install Action = %q, want %q", res.Action, ActionUpToDate)
	}

	// Verify the guard hook only appears once.
	raw, _ := os.ReadFile(filepath.Join(dir, "settings.json"))
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatalf("parse settings.json: %v", err)
	}
	hooks, _ := data["hooks"].(map[string]any)
	entries, _ := hooks[claudeEventPreToolUse].([]any)
	guardCount := 0
	for _, e := range entries {
		m, _ := e.(map[string]any)
		inner, _ := m["hooks"].([]any)
		for _, h := range inner {
			hm, _ := h.(map[string]any)
			if cmd, _ := hm["command"].(string); strings.Contains(cmd, "gsd-guard") {
				guardCount++
			}
		}
	}
	if guardCount != 1 {
		t.Errorf("expected exactly 1 gsd-guard hook, got %d", guardCount)
	}
}

func TestClaude_Install_AuditHooksAdded(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".claude")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	res, err := (&claudeInstaller{}).Install(root, Options{})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if res.Action == ActionUpToDate {
		t.Error("fresh install should not be up-to-date")
	}

	raw, _ := os.ReadFile(filepath.Join(dir, "settings.json"))
	s := string(raw)
	if !strings.Contains(s, claudeAuditTrackCommandMarker) {
		t.Errorf("PostToolUse audit-track hook not added: %s", s)
	}
	if !strings.Contains(s, claudeAuditReportCommandMarker) {
		t.Errorf("Stop audit-report hook not added: %s", s)
	}
}

func TestClaude_Install_AuditHooksIdempotent(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".claude")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	inst := &claudeInstaller{}
	if _, err := inst.Install(root, Options{}); err != nil {
		t.Fatal(err)
	}
	res, err := inst.Install(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Action != ActionUpToDate {
		t.Errorf("second Install Action = %q, want %q", res.Action, ActionUpToDate)
	}

	// Verify each audit hook appears exactly once.
	raw, _ := os.ReadFile(filepath.Join(dir, "settings.json"))
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatalf("parse settings.json: %v", err)
	}
	hooks, _ := data["hooks"].(map[string]any)

	countHookInEvent := func(event, marker string) int {
		entries, _ := hooks[event].([]any)
		n := 0
		for _, e := range entries {
			m, _ := e.(map[string]any)
			inner, _ := m["hooks"].([]any)
			for _, h := range inner {
				hm, _ := h.(map[string]any)
				if cmd, _ := hm["command"].(string); strings.Contains(cmd, marker) {
					n++
				}
			}
		}
		return n
	}

	if n := countHookInEvent(claudeEventPostToolUse, claudeAuditTrackCommandMarker); n != 1 {
		t.Errorf("expected exactly 1 audit-track PostToolUse hook, got %d", n)
	}
	if n := countHookInEvent(claudeEventStop, claudeAuditReportCommandMarker); n != 1 {
		t.Errorf("expected exactly 1 audit-report Stop hook, got %d", n)
	}
}

func TestClaude_Install_AuditHooksPreserveExistingOnUpdate(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".claude")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Build settings with all pre-existing hooks except audit ones via
	// json.Marshal so shell variables in commands don't break JSON encoding.
	existingData := map[string]any{
		"hooks": map[string]any{
			claudeEventSessionStart: []any{
				map[string]any{
					"matcher": claudeMatcherStartup,
					"hooks":   []any{map[string]any{"type": "command", "command": claudeCommand}},
				},
			},
			claudeEventUserPromptSubmit: []any{
				map[string]any{
					"matcher": "",
					"hooks":   []any{map[string]any{"type": "command", "command": claudeInboxCommand}},
				},
			},
			claudeEventPreToolUse: []any{
				map[string]any{
					"matcher": claudeMatcherGSDPlan,
					"hooks":   []any{map[string]any{"type": "command", "command": claudeGSDGuardCommand}},
				},
			},
			claudeEventPostToolUse: []any{
				map[string]any{
					"matcher": claudeMatcherWriteTools,
					"hooks":   []any{map[string]any{"type": "command", "command": claudeVerifyCommand}},
				},
			},
		},
	}
	existingJSON, err := json.Marshal(existingData)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), existingJSON, 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := (&claudeInstaller{}).Install(root, Options{})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if res.Action != ActionUpdated {
		t.Errorf("Action = %q, want %q", res.Action, ActionUpdated)
	}

	raw, _ := os.ReadFile(filepath.Join(dir, "settings.json"))
	s := string(raw)
	if !strings.Contains(s, claudeAuditTrackCommandMarker) {
		t.Errorf("audit-track hook not added: %s", s)
	}
	if !strings.Contains(s, claudeAuditReportCommandMarker) {
		t.Errorf("audit-report hook not added: %s", s)
	}
	// Prior hooks must survive.
	if !strings.Contains(s, "gg session-start") {
		t.Errorf("SessionStart hook dropped")
	}
	if !strings.Contains(s, "gg verify --file") {
		t.Errorf("verify hook dropped")
	}
}

// TestClaude_Install_InboxCommandContainsNewFlags verifies that a fresh install
// writes the updated UserPromptSubmit hook with cursor and role flags.
func TestClaude_Install_InboxCommandContainsNewFlags(t *testing.T) {
	root := t.TempDir()
	if _, err := (&claudeInstaller{}).Install(root, Options{Force: true}); err != nil {
		t.Fatalf("Install: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(root, ".claude", "settings.json"))
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	s := string(raw)
	for _, flag := range []string{"--since-cursor", "--advance-cursor", "--include-agents", "--role"} {
		if !strings.Contains(s, flag) {
			t.Errorf("settings.json missing flag %q in UserPromptSubmit hook: %s", flag, s)
		}
	}
}

// TestClaude_Install_RewritesStaleInboxHook verifies that when the old
// "gg inbox --peek" hook is present, doctor rewrites it to the current format.
func TestClaude_Install_RewritesStaleInboxHook(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".claude"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	// Write a settings.json with the old-style hook.
	stale := `{"hooks":{"UserPromptSubmit":[{"matcher":"","hooks":[{"type":"command","command":"gg inbox --peek"}]}]}}`
	if err := os.WriteFile(filepath.Join(dir, ".claude", "settings.json"), []byte(stale), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	res, err := (&claudeInstaller{}).Install(dir, Options{Force: true})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if res.Action != ActionUpdated {
		t.Errorf("Action = %q, want %q", res.Action, ActionUpdated)
	}

	raw, _ := os.ReadFile(filepath.Join(dir, ".claude", "settings.json"))
	s := string(raw)
	if !strings.Contains(s, "--since-cursor") {
		t.Errorf("stale hook not rewritten — missing --since-cursor: %s", s)
	}
	// Must not have duplicate inbox entries.
	count := strings.Count(s, "gg inbox")
	if count != 1 {
		t.Errorf("expected 1 inbox hook entry, got %d: %s", count, s)
	}

	// Second install must be idempotent.
	res2, err := (&claudeInstaller{}).Install(dir, Options{})
	if err != nil {
		t.Fatalf("second install: %v", err)
	}
	if res2.Action != ActionUpToDate {
		t.Errorf("second install Action = %q, want %q", res2.Action, ActionUpToDate)
	}
}

// TestClaude_Install_DryRunIncludesContractNote — regression guard for BUG-020.
// Before the fix, writeContractBlock was called AFTER the dry-run early return,
// so dry-run previews silently skipped the CLAUDE.md contract note. After the
// fix, the contract block write runs first and its notes land in both branches.
func TestClaude_Install_DryRunIncludesContractNote(t *testing.T) {
	root := t.TempDir()
	// Seed a .claude/settings.json with a stale UserPromptSubmit hook so the
	// installer enters the hooks-need-update path (not the all-up-to-date
	// early return). This is the exact path BUG-020 regressed.
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	stale := `{"hooks":{"UserPromptSubmit":[{"matcher":"","hooks":[{"type":"command","command":"gg inbox"}]}]}}`
	if err := os.WriteFile(filepath.Join(root, ".claude", "settings.json"), []byte(stale), 0o644); err != nil {
		t.Fatal(err)
	}

	inst := &claudeInstaller{}
	res, err := inst.Install(root, Options{Force: true, DryRun: true})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if res.Action != ActionDryRun {
		t.Errorf("Action = %q, want %q", res.Action, ActionDryRun)
	}
	hasContract := false
	for _, n := range res.Notes {
		if strings.Contains(strings.ToLower(n), "contract block") {
			hasContract = true
			break
		}
	}
	if !hasContract {
		t.Errorf("dry-run Notes missing contract block entry (BUG-020 regression): %v", res.Notes)
	}
}
