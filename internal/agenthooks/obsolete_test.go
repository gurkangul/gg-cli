package agenthooks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRemoveObsoleteBlocks_StripsRemovedOrchestrationBlocks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")
	content := `# Project

keep before
<!-- gg:master-role:begin v3 -->
old master orchestration
<!-- gg:master-role:end -->
middle
<!-- gg:dev-routing:begin v1 -->
old dev routing
<!-- gg:dev-routing:end -->
keep after
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write CLAUDE.md: %v", err)
	}

	lines, errs := RemoveObsoleteBlocks(dir)
	if len(errs) > 0 {
		t.Fatalf("RemoveObsoleteBlocks errors: %v", errs)
	}
	if len(lines) != 2 {
		t.Fatalf("removed lines = %d, want 2: %v", len(lines), lines)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read CLAUDE.md: %v", err)
	}
	updated := string(raw)
	for _, needle := range []string{
		"gg:master-role",
		"gg:dev-routing",
		"old master orchestration",
		"old dev routing",
	} {
		if strings.Contains(updated, needle) {
			t.Fatalf("obsolete content %q still present:\n%s", needle, updated)
		}
	}
	for _, needle := range []string{"keep before", "middle", "keep after"} {
		if !strings.Contains(updated, needle) {
			t.Fatalf("wanted preserved content %q in:\n%s", needle, updated)
		}
	}
}
