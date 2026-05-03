package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gurkangul/gg-cli/internal/contextartifacts"
)

func TestContextArtifactsIndexCommandWritesLock(t *testing.T) {
	setupGGDir(t)
	if err := os.MkdirAll("docs", 0o755); err != nil {
		t.Fatalf("mkdir docs: %v", err)
	}
	if err := os.WriteFile(".gg/context-artifacts.yaml", []byte("paths:\n  - docs\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.WriteFile(filepath.Join("docs", "domain.md"), []byte("Payments schema\n"), 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}

	stdout, _, err := execCmd(t, "context", "artifacts", "index")
	if err != nil {
		t.Fatalf("context artifacts index: %v", err)
	}
	if !strings.Contains(stdout, "Indexed 1 context artifacts") {
		t.Fatalf("unexpected stdout: %q", stdout)
	}
	if _, err := os.Stat(".gg/context-artifacts.lock.json"); err != nil {
		t.Fatalf("lock not written: %v", err)
	}
}

func TestRenderContextIncludesArtifactSnippets(t *testing.T) {
	bundle := contextBundle{
		artifacts: []contextartifacts.Snippet{{
			Path: "docs/domain.md", StartLine: 2, EndLine: 3,
			Stale: true, Text: "Domain glossary\nPayments schema",
		}},
	}
	var buf bytes.Buffer
	renderContextDefault(&buf, "payments", bundle, nil, nil)
	out := buf.String()
	for _, want := range []string{"ARTIFACTS:", "[docs/domain.md:2-3] stale", "Payments schema", "1 artifacts"} {
		if !strings.Contains(out, want) {
			t.Fatalf("render missing %q in:\n%s", want, out)
		}
	}
}
