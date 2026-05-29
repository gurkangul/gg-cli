package agenthooks

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode"
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
	assertNoLegacyDigestMatches(t, "codex managed body", body, []legacyDigest{
		{"legacy execution role", 3, "9dee7f158e99b1c042cd2d01f84586560e9bdf9e78682401c443f0658e499592"},
		{"legacy command one", 2, "f7ffc78c762a1e5203a39a4b3cdbfafbb69039ea0959b46f36dd1691ba3f27fd"},
		{"legacy command two", 3, "7bac59500df4734f6ca58fcc50632f8dca25ba6d008e26fc2d546a7e5d949f2a"},
		{"legacy API knob", 2, "e96f3c45d60ba01ce93392fa9ad32880863163fa8ffa456a0d60e09df307a160"},
		{"legacy mirror rule", 14, "e923edc7c589de6b848c191a2d592d7c3ac4c3d69c78847568b1ba95b9ec32a7"},
		{"legacy handoff rule", 3, "fb8dc76e6f12fd5047bedb23d996b6da46f95c70f4ff4809e92aaa2cdb27a16f"},
		{"legacy ordered sequence", 7, "2746f91e0296b35f8e1768fec049314a52d84b0fa0582446c2476323369a8145"},
		{"legacy ordered sequence with connector", 8, "1e43b32b22aa71ec98d066aea274a281dc1231bc6e17f3833314b6728c572813"},
	})
}

type legacyDigest struct {
	label string
	words int
	sha   string
}

// The hashes are SHA-256 of normalized legacy snippets. Keep them as hashes so
// active tests can preserve absence coverage without storing the old wording.
func assertNoLegacyDigestMatches(t *testing.T, label, text string, digests []legacyDigest) {
	t.Helper()
	tokens := normalizedTokens(text)
	for _, digest := range digests {
		if digest.words <= 0 || digest.words > len(tokens) {
			continue
		}
		for i := 0; i <= len(tokens)-digest.words; i++ {
			if sha256Hex(strings.Join(tokens[i:i+digest.words], " ")) == digest.sha {
				t.Fatalf("%s still contains retired text matching %s", label, digest.label)
			}
		}
	}
}

func normalizedTokens(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

func sha256Hex(text string) string {
	sum := sha256.Sum256([]byte(text))
	return fmt.Sprintf("%x", sum)
}
