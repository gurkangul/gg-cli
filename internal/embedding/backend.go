package embedding

import (
	"context"
	"fmt"
	"os"
	"strings"
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

// EffectiveModel returns the Ollama model string that will actually be used —
// GG_EMBED_MODEL env override if set, otherwise cfg.Model. A no-op under the
// Voyage backend (returns cfg.Model unchanged; Voyage picks its model from
// cfg.Voyage.Model). This is the single source of truth for which model string
// the backend sends to Ollama, so EffectiveModelIdentity and newOllamaBackend
// both read it instead of cfg.Model directly — the struct is never mutated,
// so Save() cannot persist an env override into config.yaml.
func EffectiveModel(cfg *config.EmbeddingConfig) string {
	if resolveBackendName(cfg) == config.BackendOllama {
		if m := strings.TrimSpace(os.Getenv(config.EmbedModelEnv)); m != "" {
			return m
		}
	}
	return cfg.Model
}

// CorpusAlignedConfig returns a copy of cfg whose Model is the model the corpus
// was actually embedded with, as recorded in embedding-meta.json.
//
// A query MUST be embedded with the same model as the stored vectors — vectors
// from two different models are not comparable. When config.yaml drifts from
// the corpus (e.g. config says nomic-embed-text while the collections were
// built with qwen3-embedding:0.6b), every semantic read either hard-fails with
// ErrModelMismatch or, worse, compares vectors across embedding spaces. Before
// this, the only remedy was a human exporting GG_EMBED_MODEL in every shell,
// which made recall depend on operator memory (TASK-516).
//
// Precedence:
//  1. An explicit GG_EMBED_MODEL is honoured untouched — setting it is a
//     deliberate migration signal, so a real mismatch still trips CheckMeta and
//     points the operator at `gg reembed`.
//  2. Otherwise embedding-meta.json beats config.yaml, because meta records what
//     the vectors on disk were actually built with.
//  3. No meta (fresh project) or a non-Ollama backend leaves cfg unchanged.
//
// The bool reports whether an alignment happened, so callers can surface the
// drift once rather than silently diverging from config.yaml.
func CorpusAlignedConfig(cfg *config.EmbeddingConfig, ggDir string) (config.EmbeddingConfig, bool) {
	if cfg == nil {
		return config.EmbeddingConfig{}, false
	}
	out := *cfg
	if resolveBackendName(cfg) != config.BackendOllama {
		return out, false
	}
	// Explicit override = migration intent; do not second-guess it.
	if strings.TrimSpace(os.Getenv(config.EmbedModelEnv)) != "" {
		return out, false
	}
	meta, err := ReadMeta(ggDir)
	if err != nil || meta == nil {
		return out, false
	}
	corpus := strings.TrimSpace(meta.ModelName)
	if corpus == "" || corpus == strings.TrimSpace(cfg.Model) {
		return out, false
	}
	out.Model = corpus
	return out, true
}

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
	return EffectiveModel(cfg)
}

// EffectiveDim returns the vector dimension cfg's backend produces. It is the
// SINGLE source of truth for both the embedding-meta.json dim guard (CheckMeta)
// and the Generator's expectedDim — if those two disagree, CheckMeta trips a
// false ErrModelMismatch (the bug that bit a switch to a non-768 Ollama model:
// the generator used meta.Dim=1024 while the guard used the 768 fallback).
//
//   - Voyage: the configured output_dim (deterministic; no network, no meta).
//   - Ollama with embedding-meta.json present: the recorded meta.Dim — the
//     authoritative steady-state answer, with NO network call.
//   - Ollama without meta (first run): one live probe of the configured model so
//     a non-768 model (e.g. qwen3-embedding:0.6b → 1024) is recorded at its true
//     dim instead of being forced to the nomic fallback.
//   - Ollama, no meta, probe unavailable (server down / model missing): the
//     fallback (store.VectorSize, the nomic 768 default). A wrong stamp here is
//     self-healing — `gg reembed` probes and rewrites meta with the real dim.
//
// ctx bounds the first-run probe; pass a cancellable/timeout context.
func EffectiveDim(ctx context.Context, cfg *config.EmbeddingConfig, ggDir string, fallback int) int {
	if resolveBackendName(cfg) == config.BackendVoyage {
		if cfg.Voyage.OutputDim > 0 {
			return cfg.Voyage.OutputDim
		}
		return config.DefaultVoyageOutputDim
	}
	// Ollama: once collections exist, the recorded dim is authoritative and read
	// without any network call — this is the everyday/steady-state path.
	if meta, err := ReadMeta(ggDir); err == nil && meta != nil && meta.Dim > 0 {
		return meta.Dim
	}
	// First run (no meta yet): probe the model for its true dimension. expectedDim=0
	// disables the Generator's own dim guard so the probe itself can't error on size.
	if vec, err := New(cfg, 0).Generate(ctx, "dimension probe"); err == nil && len(vec) > 0 {
		return len(vec)
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
			"embedding dimension mismatch: model %q returned %d dimensions, expected %d — check that your embedding model/backend matches the vector store collection size (the default qwen3-embedding:0.6b produces 1024-dim vectors, nomic-embed-text 768; changing model or backend requires `gg reembed`)",
			g.model, len(vec), g.expectedDim,
		)
	}
	return vec, nil
}
