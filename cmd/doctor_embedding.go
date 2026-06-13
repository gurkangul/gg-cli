package cmd

import (
	"context"
	"fmt"
	"math"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/embedding"
	"github.com/gurkangul/gg-cli/internal/store"
)

// doctorCheckEmbedding is the backend-aware embedding check. Under the Ollama
// backend (default) it probes the Ollama endpoint and runs the dimension +
// semantic canary. Under Voyage it skips the Ollama probe entirely and does a
// lightweight check (API-key env present), so a correctly-configured Voyage user
// is NOT falsely told `ollama=down` (the TASK-495 gap this closes).
func doctorCheckEmbedding(cmd *cobra.Command, cfg *config.Config, report *doctorReport) {
	if cfg.Embedding.ResolvedBackendName() == config.BackendVoyage {
		doctorCheckVoyage(cfg, report)
		return
	}
	doctorCheckOllama(cmd, cfg, report)
}

// doctorCheckVoyage performs the lightweight Voyage embedding check: it confirms
// the API-key env var is present (the secret itself is never read into config).
// It deliberately makes NO network call — a live probe would cost an API request
// and require connectivity, which is the opposite of a fast health check.
func doctorCheckVoyage(cfg *config.Config, report *doctorReport) {
	keyEnv := cfg.Embedding.Voyage.APIKeyEnv
	if strings.TrimSpace(keyEnv) == "" {
		keyEnv = config.DefaultVoyageAPIKeyEnv
	}
	model := cfg.Embedding.Voyage.Model
	if model == "" {
		model = config.DefaultVoyageModel
	}
	if strings.TrimSpace(os.Getenv(keyEnv)) == "" {
		report.fail("embedding (voyage)", fmt.Sprintf("backend=voyage but %s is not set — export it (the key is never stored in config.yaml)", keyEnv))
		return
	}
	report.ok("embedding (voyage)", fmt.Sprintf("backend=voyage, model=%s, %s present (no live probe)", model, keyEnv))
}

// doctorCheckOllama checks Ollama connectivity, vector compatibility, and a
// small semantic canary before claiming embedding guidance is healthy.
func doctorCheckOllama(cmd *cobra.Command, cfg *config.Config, report *doctorReport) {
	ollamaURL := cfg.Embedding.Host + "/api/tags"
	curlCtx, curlCancel := context.WithTimeout(cmd.Context(), 3*time.Second)
	defer curlCancel()
	c := exec.CommandContext(curlCtx, "curl", "-sf", "--max-time", "3", ollamaURL)
	if err := c.Run(); err != nil {
		if hint := sandboxPermissionHint(err); hint != "" {
			fmt.Fprintf(os.Stderr, "  ✗ ollama unreachable at %s — operation not permitted (sandbox?)\n", cfg.Embedding.Host)
			fmt.Fprintf(os.Stderr, "    %s\n", hint)
			report.problems++
			return
		}
		report.fail("ollama", fmt.Sprintf("unreachable at %s — install native Ollama (`brew install ollama` + `ollama serve` + `ollama pull %s`) or switch to the Voyage backend (embedding.backend: voyage + export VOYAGE_API_KEY)", cfg.Embedding.Host, cfg.Embedding.Model))
		return
	}
	report.ok("ollama", fmt.Sprintf("reachable at %s (model: %s)", cfg.Embedding.Host, cfg.Embedding.Model))

	// AC-2: confirm the configured embedding model is actually present locally
	// (queryable via /api/tags) before the embedding canaries run, so a missing
	// model gives the exact `ollama pull` fix instead of a cryptic embed error.
	doctorCheckEmbeddingModel(cmd.Context(), cfg.Embedding.Host, cfg.Embedding.Model, report)

	gen := embedding.New(&cfg.Embedding, store.VectorSize)
	dimCtx, dimCancel := withTimeout(cmd.Context())
	defer dimCancel()
	vec, err := gen.Generate(dimCtx, "dimension check")
	if err != nil {
		if strings.Contains(err.Error(), "dimension mismatch") {
			report.fail("ollama embedding dim", err.Error())
		} else {
			report.warn("ollama embedding dim", fmt.Sprintf("could not verify: %v", err))
		}
		return
	}
	if len(vec) != store.VectorSize {
		report.fail("ollama embedding dim", fmt.Sprintf("model %q returns %d-dim vectors, Qdrant expects %d", cfg.Embedding.Model, len(vec), store.VectorSize))
	} else {
		report.ok("ollama embedding dim", fmt.Sprintf("%d-dim ✓ (model: %s)", len(vec), cfg.Embedding.Model))
	}
	if norm := vectorNorm(vec); norm == 0 || math.IsNaN(norm) || math.IsInf(norm, 0) {
		report.fail("ollama embedding quality", fmt.Sprintf("model %q returned invalid/zero vector for canary", cfg.Embedding.Model))
		return
	}
	semanticCanaryCheck(cmd, gen, report)
}

func semanticCanaryCheck(cmd *cobra.Command, gen *embedding.Generator, report *doctorReport) {
	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()
	base, err := gen.Generate(ctx, "authentication token refresh policy")
	if err != nil {
		report.warn("ollama semantic canary", fmt.Sprintf("could not embed base canary: %v", err))
		return
	}
	related, err := gen.Generate(ctx, "JWT refresh token rotation")
	if err != nil {
		report.warn("ollama semantic canary", fmt.Sprintf("could not embed related canary: %v", err))
		return
	}
	unrelated, err := gen.Generate(ctx, "banana bread recipe oven temperature")
	if err != nil {
		report.warn("ollama semantic canary", fmt.Sprintf("could not embed unrelated canary: %v", err))
		return
	}
	relatedScore := cosineSimilarity(base, related)
	unrelatedScore := cosineSimilarity(base, unrelated)
	if math.IsNaN(relatedScore) || math.IsNaN(unrelatedScore) || relatedScore <= unrelatedScore {
		report.warn("ollama semantic canary", fmt.Sprintf("weak ordering: related=%.3f unrelated=%.3f — embedding model may degrade gg search", relatedScore, unrelatedScore))
		return
	}
	report.ok("ollama semantic canary", fmt.Sprintf("related %.3f > unrelated %.3f", relatedScore, unrelatedScore))
}

func vectorNorm(v []float32) float64 {
	var sum float64
	for _, x := range v {
		f := float64(x)
		sum += f * f
	}
	return math.Sqrt(sum)
}

func cosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return math.NaN()
	}
	var dot float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
	}
	den := vectorNorm(a) * vectorNorm(b)
	if den == 0 {
		return math.NaN()
	}
	return dot / den
}
