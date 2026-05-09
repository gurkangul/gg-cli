package agenthooks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGSD_Detect_WithDB(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".gsd"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".gsd", "gsd.db"), []byte("dummy"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !(&gsdInstaller{}).Detect(root) {
		t.Error("expected Detect to return true when .gsd/gsd.db exists")
	}
}

func TestGSD_Detect_WithoutDB(t *testing.T) {
	root := t.TempDir()
	if (&gsdInstaller{}).Detect(root) {
		t.Error("expected Detect to return false when .gsd/gsd.db absent")
	}
}

func TestGSD_Install_CreatesKnowledge(t *testing.T) {
	root := t.TempDir()

	res, err := (&gsdInstaller{}).Install(root, Options{})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if res.Action != ActionCreated {
		t.Errorf("Action = %q, want %q", res.Action, ActionCreated)
	}

	raw, err := os.ReadFile(filepath.Join(root, ".gsd", "KNOWLEDGE.md"))
	if err != nil {
		t.Fatalf("read KNOWLEDGE.md: %v", err)
	}
	content := string(raw)
	if !strings.Contains(content, gsdMarker) {
		t.Error("KNOWLEDGE.md missing gg-bridge start marker")
	}
	if !strings.Contains(content, "gg record") {
		t.Error("KNOWLEDGE.md missing gg record guidance")
	}
}

func TestGSD_Install_AppendsToExisting(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".gsd"), 0o755); err != nil {
		t.Fatal(err)
	}
	existing := "# Knowledge\n\nSome existing content.\n"
	knPath := filepath.Join(root, ".gsd", "KNOWLEDGE.md")
	if err := os.WriteFile(knPath, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := (&gsdInstaller{}).Install(root, Options{})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if res.Action != ActionUpdated {
		t.Errorf("Action = %q, want %q", res.Action, ActionUpdated)
	}

	raw, _ := os.ReadFile(knPath)
	content := string(raw)
	if !strings.Contains(content, "Some existing content.") {
		t.Error("Install clobbered pre-existing content")
	}
	if !strings.Contains(content, gsdMarker) {
		t.Error("gg-bridge block not present after install")
	}
}

func TestGSD_Install_Idempotent(t *testing.T) {
	root := t.TempDir()

	if _, err := (&gsdInstaller{}).Install(root, Options{}); err != nil {
		t.Fatalf("first install: %v", err)
	}
	res, err := (&gsdInstaller{}).Install(root, Options{})
	if err != nil {
		t.Fatalf("second install: %v", err)
	}
	if res.Action != ActionUpToDate {
		t.Errorf("second Action = %q, want %q", res.Action, ActionUpToDate)
	}
}

func TestGSD_Install_DryRun(t *testing.T) {
	root := t.TempDir()
	res, err := (&gsdInstaller{}).Install(root, Options{DryRun: true})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if res.Action != ActionDryRun {
		t.Errorf("Action = %q, want %q", res.Action, ActionDryRun)
	}
	if _, statErr := os.Stat(filepath.Join(root, ".gsd", "KNOWLEDGE.md")); statErr == nil {
		t.Error("dry-run should not have created KNOWLEDGE.md")
	}
}

// TestGSD_BridgeBlock_ContainsAuthorityPreamble verifies the GSD bridge block
// declares gg-cli as canonical while keeping GSD as a manual scratchpad/helper.
func TestGSD_BridgeBlock_ContainsAuthorityPreamble(t *testing.T) {
	body := gsdBridgeBlock()
	checks := []struct {
		substr string
		label  string
	}{
		{"AUTHORITY:", "AUTHORITY: preamble opener"},
		{"gg-cli is canonical", "gg-cli is canonical clause"},
		{"gg mandatory contract", "gg mandatory contract clause"},
		{"MANUAL GSD SCRATCHPAD", "manual scratchpad heading"},
		{"local scratchpad/helper", "scratchpad scope"},
		{"copy durable outcomes back into gg", "durable outcome copy guidance"},
		{"`gg gsd audit` is advisory", "advisory audit guidance"},
	}
	for _, c := range checks {
		if !strings.Contains(body, c.substr) {
			t.Errorf("gsdBridgeBlock missing %s (AC-4): want substring %q", c.label, c.substr)
		}
	}
	removed := []string{
		"MANDATORY PER-TASK " + "MIRROR",
		"Every GSD " + "task (T-level) MUST have exactly one gg task " + "mirror",
		"developer execution " + "environment",
		"gsd_complete" + "_task",
	}
	for _, removed := range removed {
		if strings.Contains(body, removed) {
			t.Fatalf("gsdBridgeBlock still contains strict mirror text %q", removed)
		}
	}
}
