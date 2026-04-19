package artifacts_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gurkangul/gg-cli/internal/artifacts"
)

func TestReadMissing(t *testing.T) {
	dir := t.TempDir()
	m, err := artifacts.Read(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.Artifacts) != 0 {
		t.Errorf("expected empty map, got %v", m.Artifacts)
	}
}

func TestWriteRead(t *testing.T) {
	dir := t.TempDir()
	in := artifacts.Manifest{
		Artifacts: map[string]string{
			"RULES.md": "abc123",
		},
	}
	if err := artifacts.Write(dir, in); err != nil {
		t.Fatalf("Write: %v", err)
	}
	out, err := artifacts.Read(dir)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if out.Artifacts["RULES.md"] != "abc123" {
		t.Errorf("got %q, want abc123", out.Artifacts["RULES.md"])
	}
	if out.InstalledAt == "" {
		t.Error("InstalledAt should be set")
	}
	if _, err := os.Stat(filepath.Join(dir, "installed.json.tmp")); !os.IsNotExist(err) {
		t.Error("tmp file should not linger after write")
	}
}

func TestRecord(t *testing.T) {
	dir := t.TempDir()
	if err := artifacts.Record(dir, "RULES.md", "sha1"); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if err := artifacts.Record(dir, "AGENTS.md", "sha2"); err != nil {
		t.Fatalf("Record second: %v", err)
	}
	m, _ := artifacts.Read(dir)
	if m.Artifacts["RULES.md"] != "sha1" {
		t.Error("RULES.md sha mismatch")
	}
	if m.Artifacts["AGENTS.md"] != "sha2" {
		t.Error("AGENTS.md sha mismatch")
	}
}

func TestDiff(t *testing.T) {
	m := artifacts.Manifest{
		Artifacts: map[string]string{
			"RULES.md":  "old-sha",
			"AGENTS.md": "same-sha",
		},
	}
	current := map[string]string{
		"RULES.md":  "new-sha",
		"AGENTS.md": "same-sha",
		"hooks/foo": "fresh",
	}
	entries := artifacts.Diff(m, current)
	statusMap := map[string]string{}
	for _, e := range entries {
		statusMap[e.Key] = e.Status
	}
	if statusMap["RULES.md"] != "drifted" {
		t.Errorf("RULES.md should be drifted, got %q", statusMap["RULES.md"])
	}
	if statusMap["AGENTS.md"] != "ok" {
		t.Errorf("AGENTS.md should be ok, got %q", statusMap["AGENTS.md"])
	}
	if statusMap["hooks/foo"] != "missing" {
		t.Errorf("hooks/foo should be missing, got %q", statusMap["hooks/foo"])
	}
}

func TestSHA256Hex(t *testing.T) {
	a := artifacts.SHA256Hex("hello")
	b := artifacts.SHA256Hex("hello")
	if a != b {
		t.Error("SHA256 should be deterministic")
	}
	if a == artifacts.SHA256Hex("world") {
		t.Error("different input should yield different SHA")
	}
	if len(a) != 64 {
		t.Errorf("expected 64-char hex, got %d", len(a))
	}
}
