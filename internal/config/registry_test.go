package config

import (
	"os"
	"path/filepath"
	"testing"
)

// withTempHome points HOME at a temp dir for the duration of the test so
// SharedDir() / registryPath() resolve into an isolated filesystem.
func withTempHome(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
}

func TestLoadRegistry_MissingFileReturnsEmpty(t *testing.T) {
	withTempHome(t)
	reg, err := LoadRegistry()
	if err != nil {
		t.Fatalf("LoadRegistry on fresh home: %v", err)
	}
	if reg == nil || reg.Projects == nil {
		t.Fatal("expected non-nil empty registry")
	}
	if len(reg.Projects) != 0 {
		t.Errorf("expected 0 projects, got %d", len(reg.Projects))
	}
}

func TestRegistry_AddSaveLoadRoundtrip(t *testing.T) {
	withTempHome(t)
	reg, _ := LoadRegistry()
	reg.Add("proj-1", "/tmp/proj-1-root")
	reg.Add("proj-2", "/tmp/proj-2-root")
	if err := reg.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := LoadRegistry()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if len(got.Projects) != 2 {
		t.Fatalf("expected 2, got %d", len(got.Projects))
	}
	if got.Projects["proj-1"].Root != "/tmp/proj-1-root" {
		t.Errorf("proj-1 root mismatch: %q", got.Projects["proj-1"].Root)
	}
}

func TestRegistry_AddOverwritesOnSameID(t *testing.T) {
	withTempHome(t)
	reg, _ := LoadRegistry()
	reg.Add("proj-1", "/old/path")
	reg.Add("proj-1", "/new/path")
	if got := reg.Projects["proj-1"].Root; got != "/new/path" {
		t.Errorf("expected /new/path, got %q", got)
	}
	if len(reg.Projects) != 1 {
		t.Errorf("expected 1 entry after overwrite, got %d", len(reg.Projects))
	}
}

func TestRegistry_RemoveReturnsTrueOnHit(t *testing.T) {
	withTempHome(t)
	reg, _ := LoadRegistry()
	reg.Add("proj-1", "/tmp/x")
	if !reg.Remove("proj-1") {
		t.Error("Remove(existing) should return true")
	}
	if reg.Remove("proj-1") {
		t.Error("Remove(missing) should return false")
	}
}

func TestRegistry_SortedIsStableByName(t *testing.T) {
	withTempHome(t)
	reg, _ := LoadRegistry()
	reg.Add("id-b", "/tmp/zebra")
	reg.Add("id-a", "/tmp/alpha")
	reg.Add("id-c", "/tmp/mango")
	got := reg.Sorted()
	want := []string{"alpha", "mango", "zebra"}
	for i, p := range got {
		if p.Name != want[i] {
			t.Errorf("Sorted()[%d].Name = %q, want %q", i, p.Name, want[i])
		}
	}
}

func TestRegistry_SaveIsAtomic(t *testing.T) {
	withTempHome(t)
	reg, _ := LoadRegistry()
	reg.Add("proj-1", "/tmp/proj-1-root")
	if err := reg.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}
	// .tmp file should not exist after a successful save.
	p, _ := registryPath()
	if _, err := os.Stat(p + ".tmp"); !os.IsNotExist(err) {
		t.Error("expected .tmp sidecar to be gone after Save")
	}
	// registry file itself should exist and parse.
	if _, err := os.Stat(p); err != nil {
		t.Errorf("expected registry file, got: %v", err)
	}
}

func TestRegistry_SharedDirIsCreated(t *testing.T) {
	withTempHome(t)
	reg, _ := LoadRegistry()
	reg.Add("proj-1", "/tmp/x")
	if err := reg.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}
	home, _ := os.UserHomeDir()
	if _, err := os.Stat(filepath.Join(home, ".gg")); err != nil {
		t.Errorf("expected ~/.gg/ to be created on first save: %v", err)
	}
}
