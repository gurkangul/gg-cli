package agenthooks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBMAD_Detect_WithDir(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "_bmad"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !(&bmadInstaller{}).Detect(root) {
		t.Error("expected Detect to return true when _bmad/ exists")
	}
}

func TestBMAD_Detect_WithoutDir(t *testing.T) {
	root := t.TempDir()
	if (&bmadInstaller{}).Detect(root) {
		t.Error("expected Detect to return false when _bmad/ absent")
	}
}

func TestBMAD_Install_MissingAgentsMDFails(t *testing.T) {
	root := t.TempDir()
	_, err := (&bmadInstaller{}).Install(root, Options{})
	if err == nil {
		t.Error("expected error when AGENTS.md is missing")
	}
}

func TestBMAD_Install_AppendsBlock(t *testing.T) {
	root := t.TempDir()
	agentsPath := filepath.Join(root, "AGENTS.md")
	if err := os.WriteFile(agentsPath, []byte("# Agents\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := (&bmadInstaller{}).Install(root, Options{})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if res.Action != ActionUpdated {
		t.Errorf("Action = %q, want %q", res.Action, ActionUpdated)
	}

	raw, _ := os.ReadFile(agentsPath)
	content := string(raw)
	if !strings.Contains(content, bmadBlockStart) {
		t.Error("AGENTS.md missing bmad block start marker")
	}
	if !strings.Contains(content, "BMAD") {
		t.Error("AGENTS.md missing BMAD content")
	}
	if !strings.Contains(content, "# Agents") {
		t.Error("AGENTS.md lost pre-existing content")
	}
}

func TestBMAD_Install_Idempotent(t *testing.T) {
	root := t.TempDir()
	agentsPath := filepath.Join(root, "AGENTS.md")
	if err := os.WriteFile(agentsPath, []byte("# Agents\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := (&bmadInstaller{}).Install(root, Options{}); err != nil {
		t.Fatalf("first install: %v", err)
	}
	res, err := (&bmadInstaller{}).Install(root, Options{})
	if err != nil {
		t.Fatalf("second install: %v", err)
	}
	if res.Action != ActionUpToDate {
		t.Errorf("second Action = %q, want %q", res.Action, ActionUpToDate)
	}
}

// TestBMAD_ManagedBody_ContainsAuthorityPreamble verifies AC-3: the BMAD
// managed block body declares the gg-cli authority order so that the
// orchestrating agent sees gg wins over BMAD party rules. Must include the
// full spec-required clauses: AUTHORITY opener, gg-cli wins, subagents role,
// and persisted via gg requirement.
func TestBMAD_ManagedBody_ContainsAuthorityPreamble(t *testing.T) {
	body := bmadManagedBody()
	checks := []struct {
		substr string
		label  string
	}{
		{"AUTHORITY:", "AUTHORITY: preamble opener"},
		{"gg-cli wins", "gg-cli wins clause"},
		{"subagents", "subagents role clause"},
		{"persisted via gg", "persisted via gg requirement"},
	}
	for _, c := range checks {
		if !strings.Contains(body, c.substr) {
			t.Errorf("bmadManagedBody missing %s (AC-3): want substring %q", c.label, c.substr)
		}
	}
}

func TestBMAD_Install_DryRun(t *testing.T) {
	root := t.TempDir()
	agentsPath := filepath.Join(root, "AGENTS.md")
	if err := os.WriteFile(agentsPath, []byte("# Agents\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := (&bmadInstaller{}).Install(root, Options{DryRun: true})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if res.Action != ActionDryRun {
		t.Errorf("Action = %q, want %q", res.Action, ActionDryRun)
	}

	raw, _ := os.ReadFile(agentsPath)
	if strings.Contains(string(raw), bmadBlockStart) {
		t.Error("dry-run should not have modified AGENTS.md")
	}
}
