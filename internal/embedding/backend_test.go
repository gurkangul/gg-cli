package embedding

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/gurkangul/gg-cli/internal/config"
)

// TestNewBackend_DefaultSelectsOllama proves the factory yields the Ollama
// backend for the default config (empty Backend) and for an explicit "ollama".
func TestNewBackend_DefaultSelectsOllama(t *testing.T) {
	for _, name := range []string{"", config.BackendOllama, "unrecognised-fallback"} {
		cfg := &config.EmbeddingConfig{Backend: name, Host: "http://localhost:11434", Model: "nomic-embed-text"}
		g := NewBackend(cfg, 768)
		if _, ok := g.backend.(*ollamaBackend); !ok {
			t.Fatalf("backend %q: expected *ollamaBackend, got %T", name, g.backend)
		}
		if g.ModelIdentity() != "nomic-embed-text" {
			t.Errorf("backend %q: ollama identity = %q, want bare nomic-embed-text", name, g.ModelIdentity())
		}
	}
}

// TestNewBackend_VoyageSelectsVoyage proves the factory yields the Voyage
// backend when configured, with a backend-qualified identity.
func TestNewBackend_VoyageSelectsVoyage(t *testing.T) {
	cfg := &config.EmbeddingConfig{
		Backend: config.BackendVoyage,
		Voyage:  config.VoyageConfig{Model: "voyage-3.5-lite", OutputDim: 1024, APIKeyEnv: "VOYAGE_API_KEY"}, //nolint:gosec // env var NAME, not a real credential
	}
	g := NewBackend(cfg, 1024)
	if _, ok := g.backend.(*voyageBackend); !ok {
		t.Fatalf("expected *voyageBackend, got %T", g.backend)
	}
	if g.ModelIdentity() != "voyage:voyage-3.5-lite" {
		t.Errorf("voyage identity = %q, want voyage:voyage-3.5-lite", g.ModelIdentity())
	}
}

// TestEffectiveModelIdentity confirms the no-network helper agrees with the
// identity a constructed Generator reports — this is what CheckMeta uses.
func TestEffectiveModelIdentity(t *testing.T) {
	ollama := &config.EmbeddingConfig{Backend: config.BackendOllama, Model: "nomic-embed-text"}
	if got := EffectiveModelIdentity(ollama); got != "nomic-embed-text" {
		t.Errorf("ollama identity = %q, want nomic-embed-text", got)
	}
	voyage := &config.EmbeddingConfig{Backend: config.BackendVoyage, Voyage: config.VoyageConfig{Model: "voyage-3.5-lite"}}
	if got := EffectiveModelIdentity(voyage); got != "voyage:voyage-3.5-lite" {
		t.Errorf("voyage identity = %q, want voyage:voyage-3.5-lite", got)
	}
	// Helper must match the live generator's identity.
	if EffectiveModelIdentity(voyage) != NewBackend(voyage, 0).ModelIdentity() {
		t.Error("EffectiveModelIdentity disagrees with Generator.ModelIdentity for voyage")
	}

	// GG_EMBED_MODEL overrides the Ollama identity WITHOUT mutating cfg.Model.
	t.Setenv(config.EmbedModelEnv, "qwen3-embedding:0.6b")
	ollamaOverride := &config.EmbeddingConfig{Backend: config.BackendOllama, Model: "nomic-embed-text"}
	if got := EffectiveModelIdentity(ollamaOverride); got != "qwen3-embedding:0.6b" {
		t.Errorf("ollama identity with env override = %q, want qwen3-embedding:0.6b", got)
	}
	// cfg.Model must remain unmutated (the env is read at call time, not baked in).
	if ollamaOverride.Model != "nomic-embed-text" {
		t.Errorf("cfg.Model was mutated to %q — must stay nomic-embed-text", ollamaOverride.Model)
	}
	// Voyage must be completely unaffected by the Ollama env override.
	if got := EffectiveModelIdentity(voyage); got != "voyage:voyage-3.5-lite" {
		t.Errorf("voyage identity changed to %q when GG_EMBED_MODEL is set — must be unaffected", got)
	}
}

// TestEffectiveDim confirms the dim resolution contract: Voyage reports its
// configured output dim (no network/meta), Ollama reads the authoritative dim
// from embedding-meta.json once it exists, and falls back only when there is
// neither meta nor a reachable model to probe.
func TestEffectiveDim(t *testing.T) {
	ctx := context.Background()

	// Voyage: configured output dim, no meta needed.
	voyage := &config.EmbeddingConfig{Backend: config.BackendVoyage, Voyage: config.VoyageConfig{OutputDim: 256}}
	if got := EffectiveDim(ctx, voyage, t.TempDir(), 768); got != 256 {
		t.Errorf("voyage EffectiveDim = %d, want configured 256", got)
	}
	voyageDefault := &config.EmbeddingConfig{Backend: config.BackendVoyage}
	if got := EffectiveDim(ctx, voyageDefault, t.TempDir(), 768); got != config.DefaultVoyageOutputDim {
		t.Errorf("voyage default EffectiveDim = %d, want %d", got, config.DefaultVoyageOutputDim)
	}

	// Ollama with meta present: meta.Dim is authoritative with NO network call. A
	// 1024-dim model (e.g. qwen3-embedding:0.6b) must report 1024, not 768.
	dir := t.TempDir()
	if err := WriteMeta(dir, &Meta{ModelName: "qwen3-embedding:0.6b", Dim: 1024}); err != nil {
		t.Fatalf("seed meta: %v", err)
	}
	ollama := &config.EmbeddingConfig{Backend: config.BackendOllama, Model: "qwen3-embedding:0.6b"}
	if got := EffectiveDim(ctx, ollama, dir, 768); got != 1024 {
		t.Errorf("ollama EffectiveDim with meta = %d, want authoritative 1024", got)
	}

	// Ollama, no meta, unreachable host: probe fails → nomic fallback.
	ollamaNoMeta := &config.EmbeddingConfig{Backend: config.BackendOllama, Host: "http://127.0.0.1:1", Model: "nomic-embed-text"}
	if got := EffectiveDim(ctx, ollamaNoMeta, t.TempDir(), 768); got != 768 {
		t.Errorf("ollama EffectiveDim no-meta+probe-fail = %d, want fallback 768", got)
	}
}

// TestCheckMeta_BackendSwitchMismatch proves switching ollama -> voyage trips
// ErrModelMismatch because the backend-qualified identity (and dim) differ.
func TestCheckMeta_BackendSwitchMismatch(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	ollama := &config.EmbeddingConfig{Backend: config.BackendOllama, Model: "nomic-embed-text", Host: "http://127.0.0.1:1"}
	if err := CheckMeta(dir, EffectiveModelIdentity(ollama), EffectiveDim(ctx, ollama, dir, 768)); err != nil {
		t.Fatalf("first run (ollama) should write meta cleanly: %v", err)
	}
	voyage := &config.EmbeddingConfig{Backend: config.BackendVoyage, Voyage: config.VoyageConfig{Model: "voyage-3.5-lite", OutputDim: 1024}}
	err := CheckMeta(dir, EffectiveModelIdentity(voyage), EffectiveDim(ctx, voyage, dir, 768))
	if !errors.Is(err, ErrModelMismatch) {
		t.Fatalf("backend switch should trip ErrModelMismatch, got: %v", err)
	}
	// The guidance must point the user at reembed.
	if got := err.Error(); !strings.Contains(got, "gg reembed") {
		t.Errorf("mismatch error should mention `gg reembed`, got: %v", got)
	}
}

// TestCheckMeta_IdentityMismatchAtMatchingDim is the teeth of the safety net:
// it proves a backend switch is caught on the IDENTITY string ALONE, even when
// the vector dim is identical. Without this, a regression that broke the
// identity comparison but kept the dim check would silently mix Voyage and
// nomic vectors in one collection (both happen to be 768-dim here).
//
// This calls CheckMeta directly with crafted identity/dim values rather than
// routing through config validation, because validation rejects Voyage
// output_dim:768 (valid set {256,512,1024,2048}). The scenario is legitimate at
// the CheckMeta layer: stored nomic@768 vs effective voyage@768.
func TestCheckMeta_IdentityMismatchAtMatchingDim(t *testing.T) {
	dir := t.TempDir()
	const dim = 768

	// Seed stored meta as if collections were built with Ollama nomic at 768.
	if err := WriteMeta(dir, &Meta{ModelName: "nomic-embed-text", Dim: dim}); err != nil {
		t.Fatalf("seed stored meta: %v", err)
	}

	// Now the project's effective identity is Voyage — at the SAME dim. Only the
	// identity differs, so the mismatch must be caught purely on the identity.
	err := CheckMeta(dir, "voyage:voyage-3.5-lite", dim)
	if !errors.Is(err, ErrModelMismatch) {
		t.Fatalf("identity-only switch (same dim %d) must trip ErrModelMismatch, got: %v", dim, err)
	}
	// Error must name both the stored and the new identity so the user can tell
	// which backend the collection was built with.
	for _, want := range []string{"nomic-embed-text", "voyage:voyage-3.5-lite", "gg reembed"} {
		if got := err.Error(); !strings.Contains(got, want) {
			t.Errorf("mismatch error %q missing %q", got, want)
		}
	}

	// Sanity: a matching identity at the same dim must NOT trip a mismatch — this
	// confirms the assertion above is firing on the identity, not on noise.
	if err := CheckMeta(dir, "nomic-embed-text", dim); err != nil {
		t.Fatalf("matching identity at same dim must pass cleanly, got: %v", err)
	}
}

// --- Default-unchanged parity proof -------------------------------------------

// TestDefaultPath_OllamaRequestParity is the regression oracle for the refactor:
// with the DEFAULT config the factory selects Ollama, and Generate issues the
// exact same POST /api/embed request ({model,input}) and returns the exact same
// vector the pre-refactor code path produced. Any drift in request shape or
// returned vector fails here.
func TestDefaultPath_OllamaRequestParity(t *testing.T) {
	want := []float32{0.1, 0.2, 0.3, -0.4}

	var gotPath, gotMethod string
	var gotBody embedRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		_ = json.NewEncoder(w).Encode(embedResponse{Embeddings: [][]float32{want}})
	}))
	defer srv.Close()

	// Default config shape (DefaultConfig().Embedding) but pointed at the mock.
	cfg := config.DefaultConfig().Embedding
	cfg.Host = srv.URL
	if cfg.Backend != config.BackendOllama {
		t.Fatalf("DefaultConfig backend = %q, want ollama (default path must be unchanged)", cfg.Backend)
	}

	g := New(&cfg, len(want))
	if _, ok := g.backend.(*ollamaBackend); !ok {
		t.Fatalf("default config must yield *ollamaBackend, got %T", g.backend)
	}

	vec, err := g.Generate(context.Background(), "parity probe")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	// Request parity: same path, method, and body as the legacy Generator.
	if gotMethod != http.MethodPost || gotPath != "/api/embed" {
		t.Errorf("request = %s %s, want POST /api/embed", gotMethod, gotPath)
	}
	if gotBody.Model != "qwen3-embedding:0.6b" {
		t.Errorf("request model = %q, want qwen3-embedding:0.6b", gotBody.Model)
	}
	if gotBody.Input != "parity probe" {
		t.Errorf("request input = %q, want parity probe", gotBody.Input)
	}
	// Vector parity: identical bytes round-tripped.
	if !reflect.DeepEqual(vec, want) {
		t.Errorf("returned vector = %v, want %v (byte-identical to legacy path)", vec, want)
	}
}

// TestDefaultPath_DimMismatchGuardPreserved confirms the dimension-mismatch
// guard still fires on the default path with the same "dimension mismatch"
// signal cmd/doctor and service-hint tests rely on.
func TestDefaultPath_DimMismatchGuardPreserved(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(embedResponse{Embeddings: [][]float32{make([]float32, 384)}})
	}))
	defer srv.Close()

	cfg := config.DefaultConfig().Embedding
	cfg.Host = srv.URL
	// A sentinel model name that appears NOWHERE in the error's static hint.
	// Asserting the real default would be vacuous: the hint text names both
	// qwen3-embedding:0.6b and nomic-embed-text, so the expectation would be
	// satisfied by boilerplate even if the "model %q" substitution were deleted.
	// Verified by deleting that verb and watching this test fail.
	// Same reasoning for the DIM: the hint text contains "nomic-embed-text 768",
	// so asserting "768" would also be satisfied by boilerplate. 999 appears
	// nowhere in the static text, so the assertion requires the "expected %d"
	// substitution. Verified by replacing that verb with a literal and watching
	// this test fail.
	cfg.Model = "sentinel-model-not-in-hint"
	g := New(&cfg, 999) // expect 999, server returns 384

	_, err := g.Generate(context.Background(), "x")
	if err == nil {
		t.Fatal("expected dimension mismatch error")
	}
	for _, want := range []string{"dimension mismatch", "384", "999", cfg.Model} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing %q", err.Error(), want)
		}
	}
}
