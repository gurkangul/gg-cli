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

func TestRegistry_DuplicateRootFor(t *testing.T) {
	withTempHome(t)
	reg, _ := LoadRegistry()
	reg.Add("proj-1", "/tmp/a")

	// Same id, different root → flagged.
	if prev, dup := reg.DuplicateRootFor("proj-1", "/tmp/b"); !dup || prev != "/tmp/a" {
		t.Errorf("expected dup at /tmp/a, got dup=%v prev=%q", dup, prev)
	}
	// Same id, same root (re-register) → not a dup.
	if _, dup := reg.DuplicateRootFor("proj-1", "/tmp/a"); dup {
		t.Error("same-root re-register should not be flagged as duplicate")
	}
	// New id → not a dup.
	if _, dup := reg.DuplicateRootFor("proj-2", "/tmp/c"); dup {
		t.Error("brand-new id should not be flagged as duplicate")
	}
}

// writeProjectConfig creates root/.gg/config.yaml with the given body so an
// entry can be classified as ok/invalid. An empty body yields a present file.
func writeProjectConfig(t *testing.T, root, body string) {
	t.Helper()
	ggDir := filepath.Join(root, DirName)
	if err := os.MkdirAll(ggDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", ggDir, err)
	}
	if err := os.WriteFile(filepath.Join(ggDir, ConfigFile), []byte(body), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

// validatingLoad is the per-entry health check used by the registry status
// tests. It mirrors the production wiring (config.ParseFromGGDir): "ok" means
// the config parses as YAML, not that it is a fully-runnable config — a normal
// registered project that omits runtime-only fields must still read "ok".
func validatingLoad(ggDir string) error {
	return ParseFromGGDir(ggDir)
}

func TestEntryStatus_OkMissingInvalid(t *testing.T) {
	base := t.TempDir()

	okRoot := filepath.Join(base, "ok-proj")
	writeProjectConfig(t, okRoot, "project_id: ok-1\n")

	invalidRoot := filepath.Join(base, "invalid-proj")
	writeProjectConfig(t, invalidRoot, ": : not yaml : :\n\t- broken")

	missingRoot := filepath.Join(base, "gone-proj") // never created

	cases := []struct {
		name string
		root string
		want EntryStatus
	}{
		{"ok", okRoot, EntryOK},
		{"missing", missingRoot, EntryMissing},
		{"invalid", invalidRoot, EntryInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := ProjectEntry{ID: "x", Root: tc.root}
			if got := e.Status(validatingLoad); got != tc.want {
				t.Errorf("Status(%s) = %q, want %q", tc.root, got, tc.want)
			}
		})
	}
}

func TestRegistry_StatsCountsStaleAndInvalid(t *testing.T) {
	withTempHome(t)
	base := t.TempDir()

	okRoot := filepath.Join(base, "ok-proj")
	writeProjectConfig(t, okRoot, "project_id: ok-1\n")
	invalidRoot := filepath.Join(base, "invalid-proj")
	writeProjectConfig(t, invalidRoot, ": : not yaml : :")

	reg, _ := LoadRegistry()
	reg.Add("ok-1", okRoot)
	reg.Add("invalid-1", invalidRoot)
	reg.Add("gone-1", filepath.Join(base, "gone-proj")) // missing root

	s := reg.Stats(validatingLoad)
	if s.Total != 3 || s.OK != 1 || s.Missing != 1 || s.Invalid != 1 {
		t.Errorf("Stats = %+v, want Total=3 OK=1 Missing=1 Invalid=1", s)
	}
}

// TestEntryStatus_ParsesButNotFullyValidIsOK locks the registry-hygiene
// contract: "ok" means the config parses, NOT that it passes the full runtime
// Validate(). A config holding only project_id (no qdrant/embedding/backup
// runtime fields) fails LoadFromGGDir's Validate() yet must still read "ok" —
// otherwise every normally-registered project would be mislabeled "invalid"
// and a reader might wrongly try to prune/repair it.
func TestEntryStatus_ParsesButNotFullyValidIsOK(t *testing.T) {
	root := filepath.Join(t.TempDir(), "minimal-proj")
	writeProjectConfig(t, root, "project_id: minimal-1\n")
	ggDir := filepath.Join(root, DirName)

	// Sanity: full validation rejects this minimal config…
	if _, err := LoadFromGGDir(ggDir); err == nil {
		t.Fatal("expected LoadFromGGDir to reject a config missing runtime fields")
	}
	// …but the registry-hygiene status must still be ok (parse-only contract).
	e := ProjectEntry{ID: "minimal-1", Root: root}
	if got := e.Status(ParseFromGGDir); got != EntryOK {
		t.Errorf("Status = %q, want %q (parseable config must be ok)", got, EntryOK)
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
