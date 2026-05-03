package contextartifacts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestSearchReturnsPathLineRangeAndStaleness(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ConfigFile), "paths:\n  - docs\n")
	writeFile(t, filepath.Join(root, "docs", "architecture.md"), "intro\nopenapi schema lives here\noutro\n")

	snips, err := Search(root, "schema", 5)
	if err != nil {
		t.Fatalf("Search before index: %v", err)
	}
	if len(snips) != 1 {
		t.Fatalf("got %d snippets, want 1", len(snips))
	}
	if snips[0].Path != "docs/architecture.md" || snips[0].StartLine != 1 || snips[0].EndLine != 3 {
		t.Fatalf("unexpected snippet location: %+v", snips[0])
	}
	if !snips[0].Stale {
		t.Fatal("snippet should be stale before index lock exists")
	}

	result, err := Index(root)
	if err != nil {
		t.Fatalf("Index: %v", err)
	}
	if !result.Configured || result.Indexed != 1 {
		t.Fatalf("unexpected index result: %+v", result)
	}
	snips, err = Search(root, "schema", 5)
	if err != nil {
		t.Fatalf("Search after index: %v", err)
	}
	if snips[0].Stale {
		t.Fatal("snippet should not be stale immediately after index")
	}

	writeFile(t, filepath.Join(root, "docs", "architecture.md"), "intro\nschema changed here\noutro\n")
	snips, err = Search(root, "schema", 5)
	if err != nil {
		t.Fatalf("Search after edit: %v", err)
	}
	if !snips[0].Stale {
		t.Fatal("snippet should be stale after content hash changes")
	}
}

func TestConfiguredPathCannotEscapeRoot(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ConfigFile), "paths:\n  - ../outside.md\n")
	_, err := Search(root, "outside", 5)
	if err == nil || !strings.Contains(err.Error(), "escapes project root") {
		t.Fatalf("expected escape error, got %v", err)
	}
}

func TestIndexNoConfigIsNoop(t *testing.T) {
	result, err := Index(t.TempDir())
	if err != nil {
		t.Fatalf("Index without config: %v", err)
	}
	if result.Configured || result.Indexed != 0 {
		t.Fatalf("unexpected result: %+v", result)
	}
}
