package config

import (
	"os"
	"path/filepath"
	"strings"
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

func TestValidate_BackupIntervalInvalid(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ProjectID = "proj"
	cfg.Backup.Interval = "invalid"
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "backup.interval") {
		t.Fatalf("expected backup.interval validation error, got: %v", err)
	}
}

func TestValidate_BackupTimeoutInvalid(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ProjectID = "proj"
	cfg.Backup.Timeout = "invalid"
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "backup.timeout") {
		t.Fatalf("expected backup.timeout validation error, got: %v", err)
	}
}

func TestLoadFromGGDir_AppliesBackupDefaults(t *testing.T) {
	dir := t.TempDir()
	ggDir := filepath.Join(dir, ".gg")
	if err := os.MkdirAll(ggDir, 0o755); err != nil {
		t.Fatalf("mkdir .gg: %v", err)
	}
	cfg := `project_id: proj
qdrant:
  host: localhost
  port: 6334
embedding:
  host: http://localhost:11434
  model: nomic-embed-text
memgraph:
  uri: bolt://localhost:7687
`
	if err := os.WriteFile(filepath.Join(ggDir, "config.yaml"), []byte(cfg), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	loaded, err := LoadFromGGDir(ggDir)
	if err != nil {
		t.Fatalf("LoadFromGGDir: %v", err)
	}
	if !loaded.Backup.AutoEnabled() {
		t.Fatal("missing backup.enabled should default to enabled")
	}
	if loaded.Backup.Interval != "24h" {
		t.Fatalf("Backup.Interval = %q, want 24h", loaded.Backup.Interval)
	}
	if loaded.Backup.Timeout != "30s" {
		t.Fatalf("Backup.Timeout = %q, want 30s", loaded.Backup.Timeout)
	}
}
