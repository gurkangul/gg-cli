package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRuntimeDir_CreatesDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfg := &Config{ProjectID: "test-proj-id-001"}
	got, err := cfg.RuntimeDir()
	if err != nil {
		t.Fatalf("RuntimeDir() error: %v", err)
	}

	want := filepath.Join(home, SharedDirName, "projects", "test-proj-id-001")
	if got != want {
		t.Errorf("RuntimeDir() = %q, want %q", got, want)
	}

	info, err := os.Stat(got)
	if err != nil {
		t.Fatalf("directory not created: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("path is not a directory")
	}
	if info.Mode().Perm() != 0o700 {
		t.Errorf("directory permissions = %o, want 700", info.Mode().Perm())
	}
}

func TestRuntimeDir_Idempotent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfg := &Config{ProjectID: "idempotent-id"}
	got1, err := cfg.RuntimeDir()
	if err != nil {
		t.Fatalf("first call error: %v", err)
	}
	got2, err := cfg.RuntimeDir()
	if err != nil {
		t.Fatalf("second call error: %v", err)
	}
	if got1 != got2 {
		t.Errorf("got different paths: %q vs %q", got1, got2)
	}
}

func TestRuntimeDir_IsolatesByProjectID(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfg1 := &Config{ProjectID: "proj-aaa"}
	cfg2 := &Config{ProjectID: "proj-bbb"}

	dir1, _ := cfg1.RuntimeDir()
	dir2, _ := cfg2.RuntimeDir()

	if dir1 == dir2 {
		t.Error("different project IDs produced the same runtime dir")
	}
}
