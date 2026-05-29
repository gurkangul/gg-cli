package agenthooks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCodex_Detect(t *testing.T) {
	root := t.TempDir()
	if (&codexInstaller{}).Detect(root) {
		t.Error("expected false without AGENTS.md")
	}
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("# x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !(&codexInstaller{}).Detect(root) {
		t.Error("expected true with AGENTS.md present")
	}
}

func TestCodex_Install_MissingAgentsMDFails(t *testing.T) {
	root := t.TempDir()
	_, err := (&codexInstaller{}).Install(root, Options{})
	if err == nil {
		t.Fatal("expected error when AGENTS.md missing")
	}
	if !strings.Contains(err.Error(), "gg init") {
		t.Errorf("error should hint at gg init: %v", err)
	}
}

func TestCodex_Install_AppendsBlockWhenAbsent(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "AGENTS.md")
	orig := "# My Project\n\nExisting rules here.\n"
	if err := os.WriteFile(path, []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := (&codexInstaller{}).Install(root, Options{})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if res.Action != ActionUpdated {
		t.Errorf("Action = %q, want %q", res.Action, ActionUpdated)
	}
	raw, _ := os.ReadFile(path)
	s := string(raw)
	if !strings.HasPrefix(s, orig) {
		t.Errorf("original content should be preserved at start:\n%s", s)
	}
	if !strings.Contains(s, codexBlockStart) || !strings.Contains(s, codexBlockEnd) {
		t.Errorf("managed block markers missing: %s", s)
	}
	if !strings.Contains(s, "Durable outputs include decisions") {
		t.Errorf("managed block must explain durable-memory capture: %s", s)
	}
}

func TestCodex_Install_IdempotentOnRerun(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "AGENTS.md")
	if err := os.WriteFile(path, []byte("# X\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	inst := &codexInstaller{}
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

func TestCodex_Install_ReplacesDriftedBlock(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "AGENTS.md")
	// Simulate a stale managed block from an older gg version.
	content := "# X\n\n" + codexBlockStart + "\nstale content\n" + codexBlockEnd + "\n\n# Tail\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := (&codexInstaller{}).Install(root, Options{}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	s := string(raw)
	if strings.Contains(s, "stale content") {
		t.Errorf("stale content should have been replaced: %s", s)
	}
	if !strings.Contains(s, "# Tail") {
		t.Errorf("content after the block should be preserved: %s", s)
	}
	// Only one start / end marker should remain — no duplication.
	if c := strings.Count(s, codexBlockStart); c != 1 {
		t.Errorf("expected 1 start marker, got %d", c)
	}
	if c := strings.Count(s, codexBlockEnd); c != 1 {
		t.Errorf("expected 1 end marker, got %d", c)
	}
}

func TestCodex_ManagedBody_AllowsNativeGSDScratchpad(t *testing.T) {
	body := codexManagedBody()
	for _, want := range []string{
		"GSD itself is allowed",
		"local scratchpad/helper",
		"copy durable outcomes into gg",
		"do not rely on `.gsd/gsd.db` for shared memory",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("codex managed body missing %q", want)
		}
	}
	removed := []string{
		"developer execution " + "worker",
		"gg " + "spawn",
		"gg gsd " + "open",
		"developer" + ".command",
		"Every GSD " + "task (T-level, not milestone or slice) MUST have a gg task",
		"Mirror at " + "pickup",
		"tabs, panes, queues, nudges, heartbeats, and worker routing",
	}
	for _, removed := range removed {
		if strings.Contains(body, removed) {
			t.Fatalf("codex managed body still contains retired managed-block text %q", removed)
		}
	}
}
