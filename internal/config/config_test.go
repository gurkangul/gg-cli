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

// TestDefaultConfig_EmbeddingBackendIsOllama proves the offline default stays
// Ollama after the pluggable-backend change.
func TestDefaultConfig_EmbeddingBackendIsOllama(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Embedding.Backend != BackendOllama {
		t.Fatalf("default embedding.backend = %q, want %q", cfg.Embedding.Backend, BackendOllama)
	}
	if cfg.Embedding.Model != "nomic-embed-text" {
		t.Fatalf("default embedding.model = %q, want nomic-embed-text", cfg.Embedding.Model)
	}
}

// TestApplyDefaults_EmptyBackendResolvesToOllama ensures a config written before
// the backend field existed (empty Backend) loads as Ollama, unchanged.
func TestApplyDefaults_EmptyBackendResolvesToOllama(t *testing.T) {
	cfg := &Config{Embedding: EmbeddingConfig{Host: "http://localhost:11434", Model: "nomic-embed-text"}}
	cfg.ApplyDefaults()
	if cfg.Embedding.Backend != BackendOllama {
		t.Fatalf("empty backend should default to ollama, got %q", cfg.Embedding.Backend)
	}
	// Voyage sub-defaults are stamped but inert while backend is ollama.
	if cfg.Embedding.Voyage.Model != DefaultVoyageModel {
		t.Fatalf("voyage.model default = %q, want %q", cfg.Embedding.Voyage.Model, DefaultVoyageModel)
	}
	if cfg.Embedding.Voyage.OutputDim != DefaultVoyageOutputDim {
		t.Fatalf("voyage.output_dim default = %d, want %d", cfg.Embedding.Voyage.OutputDim, DefaultVoyageOutputDim)
	}
	if cfg.Embedding.Voyage.APIKeyEnv != DefaultVoyageAPIKeyEnv {
		t.Fatalf("voyage.api_key_env default = %q, want %q", cfg.Embedding.Voyage.APIKeyEnv, DefaultVoyageAPIKeyEnv)
	}
}

// TestValidate_VoyageBackend covers the voyage-specific validation: the
// host-URL rule does not apply, but model/output_dim/api_key_env do.
func TestValidate_VoyageBackend(t *testing.T) {
	base := func() *Config {
		c := DefaultConfig()
		c.ProjectID = "proj"
		c.Embedding.Backend = BackendVoyage
		c.ApplyDefaults() // stamps voyage sub-defaults + backup defaults
		return c
	}

	t.Run("valid voyage config passes without a host URL", func(t *testing.T) {
		c := base()
		c.Embedding.Host = "" // irrelevant for voyage; must not error
		if err := c.Validate(); err != nil {
			t.Fatalf("valid voyage config should pass, got: %v", err)
		}
	})

	t.Run("unsupported output_dim is rejected", func(t *testing.T) {
		c := base()
		c.Embedding.Voyage.OutputDim = 777
		if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "output_dim") {
			t.Fatalf("expected output_dim validation error, got: %v", err)
		}
	})

	t.Run("missing api_key_env is rejected", func(t *testing.T) {
		c := base()
		c.Embedding.Voyage.APIKeyEnv = ""
		if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "api_key_env") {
			t.Fatalf("expected api_key_env validation error, got: %v", err)
		}
	})
}

// TestValidate_UnknownBackend rejects a typo'd backend name rather than
// silently falling back.
func TestValidate_UnknownBackend(t *testing.T) {
	c := DefaultConfig()
	c.ProjectID = "proj"
	c.Embedding.Backend = "openai"
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "backend") {
		t.Fatalf("expected backend validation error, got: %v", err)
	}
}
