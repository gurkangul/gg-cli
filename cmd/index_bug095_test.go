package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gurkangul/gg-cli/internal/index/state"
)

// TestIndexStateLanguages_BUG095 guards the fix for BUG-095: a no-`--lang`
// `gg index` (as run by the install-index-hooks git hooks via `gg index
// --changed`) must refresh the language(s) the project was actually indexed as,
// taken from index-state — NOT silently assume the "go" flag default, which made
// the auto-refresh hook fail with "no go modules found" on every non-go project.
func TestIndexStateLanguages_BUG095(t *testing.T) {
	root := t.TempDir()
	ggDir := filepath.Join(root, ".gg")
	if err := os.MkdirAll(ggDir, 0o755); err != nil {
		t.Fatalf("mkdir .gg: %v", err)
	}
	// Minimal config.yaml so config.FindRoot recognizes this as a project .gg.
	if err := os.WriteFile(filepath.Join(ggDir, "config.yaml"), []byte("project_id: test-bug095\n"), 0o644); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}
	t.Chdir(root)

	// Never-indexed project: no languages → caller keeps the go default (unchanged).
	if langs := indexStateLanguages(); len(langs) != 0 {
		t.Fatalf("expected no languages before any index, got %v", langs)
	}

	// Simulate a TypeScript project that has been indexed (e.g. a Nuxt frontend).
	if err := state.WriteLanguage(ggDir, "typescript", "abc1234", "fp-ts", []string{".ts", ".tsx"}); err != nil {
		t.Fatalf("WriteLanguage typescript: %v", err)
	}

	langs := indexStateLanguages()
	if len(langs) != 1 || langs[0] != "typescript" {
		t.Fatalf("BUG-095: a TS-indexed project must resolve to [typescript], got %v", langs)
	}

	// A multi-language project must refresh every recorded language, not just go.
	if err := state.WriteLanguage(ggDir, "go", "abc1234", "fp-go", []string{".go"}); err != nil {
		t.Fatalf("WriteLanguage go: %v", err)
	}
	langs = indexStateLanguages()
	seen := map[string]bool{}
	for _, l := range langs {
		seen[l] = true
	}
	if !seen["typescript"] || !seen["go"] || len(langs) != 2 {
		t.Fatalf("expected both go and typescript recorded, got %v", langs)
	}
}
