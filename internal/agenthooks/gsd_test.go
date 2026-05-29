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

// TestGSD_BridgeBlock_ContainsDurableMemoryRule verifies the GSD bridge block
// keeps GSD as a native scratchpad/helper while requiring durable outcomes in gg.
func TestGSD_BridgeBlock_ContainsDurableMemoryRule(t *testing.T) {
	body := gsdBridgeBlock()
	checks := []struct {
		substr string
		label  string
	}{
		{"Native GSD Scratchpad", "native scratchpad heading"},
		{"does not own GSD's workflow", "gg scope clause"},
		{"durable outcomes", "durable outcome clause"},
		{"canonical cross-agent memory", "canonical memory clause"},
		{"`.gsd/gsd.db` as shared memory", "gsd db non-canonical clause"},
		{"`gg gsd audit` is advisory", "advisory audit guidance"},
	}
	for _, c := range checks {
		if !strings.Contains(body, c.substr) {
			t.Errorf("gsdBridgeBlock missing %s: want substring %q", c.label, c.substr)
		}
	}
	assertNoLegacyDigestMatches(t, "gsdBridgeBlock", body, []legacyDigest{
		{"legacy mirror heading", 4, "393fe9daeddcbab33f036593813662a6255753116b58ed2060a73b376a6c3f55"},
		{"legacy mirror rule", 12, "8c7704bc0033b4cdba5d4c4e62823600c7694e8c56f42b303ad1b4ef51f60ead"},
		{"legacy execution mode", 3, "1d0f3c1352c9126238f349458b3b3dc57196ed7d8a67e19024ca17fa9c7d50ff"},
		{"legacy task hook", 3, "e0b1b87b32b1cb8bd37f7a2f9ab5a44f17157cbab34f477726bfcfae26252abb"},
		{"legacy ordered sequence", 7, "2746f91e0296b35f8e1768fec049314a52d84b0fa0582446c2476323369a8145"},
		{"legacy ordered sequence with connector", 8, "1e43b32b22aa71ec98d066aea274a281dc1231bc6e17f3833314b6728c572813"},
	})
}
