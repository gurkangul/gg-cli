package agenthooks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCursor_Detect(t *testing.T) {
	root := t.TempDir()
	inst := &cursorInstaller{}
	if inst.Detect(root) {
		t.Error("expected Detect false without .cursor/")
	}
	if err := os.MkdirAll(filepath.Join(root, ".cursor"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !inst.Detect(root) {
		t.Error("expected Detect true with .cursor/")
	}
}

func TestCursor_Install_CreatesRule(t *testing.T) {
	root := t.TempDir()
	res, err := (&cursorInstaller{}).Install(root, Options{})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if res.Action != ActionCreated {
		t.Errorf("Action = %q, want %q", res.Action, ActionCreated)
	}
	raw, err := os.ReadFile(filepath.Join(root, ".cursor", "rules", cursorRuleFile))
	if err != nil {
		t.Fatalf("read rule: %v", err)
	}
	body := string(raw)
	if !strings.Contains(body, "alwaysApply: true") {
		t.Errorf("rule missing alwaysApply: true — %s", body)
	}
	if !strings.Contains(body, "AGENTS.md") {
		t.Errorf("rule missing AGENTS.md pointer — %s", body)
	}
}

func TestCursor_Install_IdempotentWhenContentMatches(t *testing.T) {
	root := t.TempDir()
	inst := &cursorInstaller{}
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
}

func TestCursor_Install_UpdatesDriftedContent(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".cursor", "rules")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, cursorRuleFile), []byte("stale content\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := (&cursorInstaller{}).Install(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Action != ActionUpdated {
		t.Errorf("Action = %q, want %q", res.Action, ActionUpdated)
	}
}
