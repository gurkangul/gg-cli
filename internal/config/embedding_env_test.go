package config

// Tests for the GG_EMBED_MODEL invariant: the env override must never be
// persisted to config.yaml. Invariant: env→Load→Save→(env unset)→reload must
// leave the ORIGINAL model value in config.yaml unchanged.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestGGEmbedModel_RoundTrip is the canonical M1 regression oracle:
// GG_EMBED_MODEL overrides the in-process model but NEVER mutates the struct,
// so Save() cannot persist it and a reload after the env is unset recovers the
// original model. Voyage config must be completely unaffected by the env var.
func TestGGEmbedModel_RoundTrip(t *testing.T) {
	const (
		originalModel = "nomic-embed-text"
		overrideModel = "qwen3-embedding:0.6b"
		testProjectID = "eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee"
	)

	// Build a temp project with a fixed Ollama model.
	ggDir := filepath.Join(t.TempDir(), DirName)
	if err := os.MkdirAll(ggDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	rawCfg := "project_id: " + testProjectID + "\n" +
		"embedding:\n" +
		"  host: http://localhost:11434\n" +
		"  model: " + originalModel + "\n"
	if err := os.WriteFile(filepath.Join(ggDir, ConfigFile), []byte(rawCfg), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	// --- Step 1: load with env override set ---
	t.Setenv(EmbedModelEnv, overrideModel)

	cfg, err := LoadFromGGDir(ggDir)
	if err != nil {
		t.Fatalf("LoadFromGGDir: %v", err)
	}
	// The struct must NOT be mutated: cfg.Embedding.Model must hold the
	// persisted original value, not the env override.
	if cfg.Embedding.Model != originalModel {
		t.Errorf("cfg.Embedding.Model = %q after load with env set; want %q (struct must not be mutated)", cfg.Embedding.Model, originalModel)
	}

	// --- Step 2: marshal (simulate Save) and check the serialized YAML ---
	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("yaml.Marshal: %v", err)
	}
	saved := string(data)
	if strings.Contains(saved, overrideModel) {
		t.Errorf("serialized YAML contains env override %q — GG_EMBED_MODEL must not be persisted:\n%s", overrideModel, saved)
	}

	// Write the marshalled config to a new path (second ggDir) so we can reload it.
	ggDir2 := filepath.Join(t.TempDir(), DirName)
	if err := os.MkdirAll(ggDir2, 0o755); err != nil {
		t.Fatalf("mkdir2: %v", err)
	}
	if err := os.WriteFile(filepath.Join(ggDir2, ConfigFile), data, 0o644); err != nil {
		t.Fatalf("write saved config: %v", err)
	}

	// --- Step 3: reload AFTER env is unset --- (t.Setenv restores on cleanup;
	// explicitly clear now so the reload behaves as a fresh shell with no env).
	t.Setenv(EmbedModelEnv, "")

	cfg2, err := LoadFromGGDir(ggDir2)
	if err != nil {
		t.Fatalf("LoadFromGGDir after save: %v", err)
	}
	if cfg2.Embedding.Model != originalModel {
		t.Errorf("model after save+reload = %q; want %q (env override must not have been persisted)", cfg2.Embedding.Model, originalModel)
	}
}

// TestGGEmbedModel_VoyageUnaffected ensures GG_EMBED_MODEL is a no-op when the
// Voyage backend is configured: Voyage selects its model via embedding.voyage.model,
// not the Ollama model field.
func TestGGEmbedModel_VoyageUnaffected(t *testing.T) {
	t.Setenv(EmbedModelEnv, "some-ollama-override")

	cfg := &EmbeddingConfig{
		Backend: BackendVoyage,
		Model:   "ignored-ollama-field",
		Voyage: VoyageConfig{ //nolint:gosec // G101: APIKeyEnv below holds an env var NAME, not a key
			Model:     "voyage-3.5-lite",
			OutputDim: 1024,
			APIKeyEnv: "VOYAGE_API_KEY", //nolint:gosec // env var NAME
		},
	}
	cfg.applyEmbeddingDefaults()

	if cfg.Model != "ignored-ollama-field" {
		t.Errorf("Voyage cfg.Model changed to %q — env var must not touch Voyage config", cfg.Model)
	}
	if cfg.Voyage.Model != "voyage-3.5-lite" {
		t.Errorf("cfg.Voyage.Model = %q, want voyage-3.5-lite", cfg.Voyage.Model)
	}
}
