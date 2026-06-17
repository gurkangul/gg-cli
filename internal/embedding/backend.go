package embedding

import (
	"context"
	"fmt"
	"time"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/trace"
)

// Backend is the pluggable embedding provider. Each backend turns text into a
// vector and reports the identity it stamps into embedding-meta.json so a
// backend/model switch is detected as a mismatch (see meta.go / CheckMeta)
// rather than silently mixing incompatible vectors in one collection.
//
// Two implementations exist:
//   - ollamaBackend — local Ollama HTTP API (default, fully offline).
//   - voyageBackend — Voyage AI cloud API (opt-in; sends text off-machine).
//
// generate returns the RAW vector; dimension validation is centralised in the
// Generator wrapper so both backends share one error path and the configured
// expectedDim can be adjusted after construction.
type Backend interface {
	generate(ctx context.Context, text string) ([]float32, error)
	// modelIdentity is the string recorded in embedding-meta.json. The Ollama
	// backend returns the bare model name (back-compat with existing installs);
	// the Voyage backend returns a "voyage:<model>" identity so a backend switch
	// is always seen as a mismatch.
	modelIdentity() string
}

// Generator is the public embedding entry point used by every caller in the
// codebase. It wraps a Backend, centralises dimension validation, and records
// the embed.generate trace span. Keeping it a concrete struct (rather than
// returning the Backend interface) means the ~20 existing call-sites that hold
// a *embedding.Generator compile unchanged.
type Generator struct {
	backend Backend
	model   string // effective model identity, surfaced in error/meta paths
	// expectedDim is the vector size the caller expects (e.g. store.VectorSize).
	// If the backend returns a different dimension, Generate returns a
	// descriptive error instead of letting Qdrant surface a cryptic size
	// mismatch at upsert time. 0 = skip validation.
	expectedDim int
}

// New creates an embedding Generator from config, selecting the backend named
// by cfg.Backend (empty/"ollama" = the offline default, "voyage" = cloud).
// expectedDim is the vector size the caller expects; pass 0 to skip validation.
//
// New never fails for the Ollama path (back-compat with the old signature). The
// Voyage path can fail if its API key env var is unset; New surfaces that lazily
// at Generate time instead, so construction stays infallible for all callers.
func New(cfg *config.EmbeddingConfig, expectedDim int) *Generator {
	return NewBackend(cfg, expectedDim)
}

// NewBackend is the explicit factory name; New is kept as a thin alias so
// pre-existing callers need no churn. Both resolve the backend from config.
func NewBackend(cfg *config.EmbeddingConfig, expectedDim int) *Generator {
	switch resolveBackendName(cfg) {
	case config.BackendVoyage:
		b := newVoyageBackend(cfg)
		return &Generator{backend: b, model: b.modelIdentity(), expectedDim: expectedDim}
	default: // config.BackendOllama and any unrecognised value fall back to Ollama
		b := newOllamaBackend(cfg)
		return &Generator{backend: b, model: b.modelIdentity(), expectedDim: expectedDim}
	}
}

// resolveBackendName returns the configured backend id, treating empty as the
// Ollama default so configs written before this field existed keep working.
func resolveBackendName(cfg *config.EmbeddingConfig) string {
	switch cfg.Backend {
	case config.BackendVoyage:
		return config.BackendVoyage
	default:
		return config.BackendOllama
	}
}

// ModelIdentity returns the identity string this generator stamps into the
// embedding meta — bare model name for Ollama, "voyage:<model>" for Voyage.
// CheckMeta uses this so a backend switch trips ErrModelMismatch.
func (g *Generator) ModelIdentity() string { return g.model }

// EffectiveModelIdentity returns the meta identity for cfg WITHOUT constructing
// a live backend or making any network call. It must agree with the identity a
// Generator built from the same cfg reports, so CheckMeta and the generator
// never disagree. Bare model name for Ollama (back-compat), "voyage:<model>"
// for Voyage.
func EffectiveModelIdentity(cfg *config.EmbeddingConfig) string {
	if resolveBackendName(cfg) == config.BackendVoyage {
		model := cfg.Voyage.Model
		if model == "" {
			model = config.DefaultVoyageModel
		}
		return "voyage:" + model
	}
	return cfg.Model
}

// EffectiveDim returns the vector dimension cfg's backend will produce, used to
// stamp/validate embedding-meta.json. For Ollama the dim is model-defined and
// only known by probing the server, so the caller's fallback (store.VectorSize)
// is returned. For Voyage the dim is the configured output_dim (deterministic).
func EffectiveDim(cfg *config.EmbeddingConfig, fallback int) int {
	if resolveBackendName(cfg) == config.BackendVoyage {
		if cfg.Voyage.OutputDim > 0 {
			return cfg.Voyage.OutputDim
		}
		return config.DefaultVoyageOutputDim
	}
	return fallback
}

// Generate produces an embedding vector for text via the configured backend.
// The context is honored for Ctrl+C and upstream timeouts. The embed.generate
// trace span and the dimension-mismatch guard are preserved across all backends.
func (g *Generator) Generate(ctx context.Context, text string) (vec []float32, retErr error) {
	start := time.Now()
	defer func() { trace.Record("embed.generate", start, retErr) }()

	vec, err := g.backend.generate(ctx, text)
	if err != nil {
		return nil, err
	}
	if g.expectedDim > 0 && len(vec) != g.expectedDim {
		return nil, fmt.Errorf(
			"embedding dimension mismatch: model %q returned %d dimensions, expected %d — check that your embedding model/backend matches the vector store collection size (hint: nomic-embed-text produces 768-dim vectors; switching backends requires `gg reembed`)",
			g.model, len(vec), g.expectedDim,
		)
	}
	return vec, nil
}
