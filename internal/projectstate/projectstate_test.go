package projectstate_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gurkangul/gg-cli/internal/projectstate"
)

func TestReadMissingFile(t *testing.T) {
	dir := t.TempDir()
	s, err := projectstate.Read(dir)
	if err != nil {
		t.Fatalf("expected no error for missing file, got %v", err)
	}
	if s.LastSeenCLIVersion != "" {
		t.Errorf("expected empty version, got %q", s.LastSeenCLIVersion)
	}
}

func TestWriteRead(t *testing.T) {
	dir := t.TempDir()
	in := projectstate.State{LastSeenCLIVersion: "v0.2.0"}
	if err := projectstate.Write(dir, in); err != nil {
		t.Fatalf("Write: %v", err)
	}
	out, err := projectstate.Read(dir)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if out.LastSeenCLIVersion != "v0.2.0" {
		t.Errorf("got %q, want v0.2.0", out.LastSeenCLIVersion)
	}
	if out.UpdatedAt == "" {
		t.Error("UpdatedAt should be set after Write")
	}
}

func TestWriteAtomic(t *testing.T) {
	dir := t.TempDir()
	_ = projectstate.Write(dir, projectstate.State{LastSeenCLIVersion: "v0.1.0"})
	_ = projectstate.Write(dir, projectstate.State{LastSeenCLIVersion: "v0.2.0"})
	s, _ := projectstate.Read(dir)
	if s.LastSeenCLIVersion != "v0.2.0" {
		t.Errorf("expected v0.2.0 after two writes, got %q", s.LastSeenCLIVersion)
	}
	if _, err := os.Stat(filepath.Join(dir, "state.json.tmp")); !os.IsNotExist(err) {
		t.Error("tmp file should not exist after successful write")
	}
}
