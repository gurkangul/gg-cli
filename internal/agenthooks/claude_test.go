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

func TestClaude_Detect_MissingDir(t *testing.T) {
	root := t.TempDir()
	if (&claudeInstaller{}).Detect(root) {
		t.Error("expected Detect to return false when .claude/ missing")
	}
}

func TestClaude_Install_FreshCreate(t *testing.T) {
	root := t.TempDir()
	inst := &claudeInstaller{}

	res, err := inst.Install(root, Options{})
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

func TestClaude_Install_IdempotentReRun(t *testing.T) {
	root := t.TempDir()
	inst := &claudeInstaller{}

	if _, err := inst.Install(root, Options{}); err != nil {
		t.Fatalf("first install: %v", err)
	}
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
	res, err := (&claudeInstaller{}).Install(root, Options{DryRun: true})
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
