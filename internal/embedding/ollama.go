package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/trace"
)

type embedRequest struct {
	Model string `json:"model"`
	Input string `json:"input"`
}

type embedResponse struct {
	Embeddings [][]float32 `json:"embeddings"`
	Error      string      `json:"error,omitempty"`
}

type Generator struct {
	host        string
	model       string
	client      *http.Client
	expectedDim int // 0 = no validation
}

// New creates an embedding generator. expectedDim is the vector size the
// caller expects (e.g. store.VectorSize = 768). If the model returns a vector
// with a different dimension, Generate returns a descriptive error instead of
// letting Qdrant surface a cryptic size-mismatch at upsert time.
// Pass 0 to skip dimension validation.
func New(cfg *config.EmbeddingConfig, expectedDim int) *Generator {
	return &Generator{
		host:        cfg.Host,
		model:       cfg.Model,
		client:      &http.Client{Timeout: 30 * time.Second},
		expectedDim: expectedDim,
	}
}

// Generate creates an embedding vector for the given text via Ollama.
// The context is honored for Ctrl+C and upstream timeouts.
func (g *Generator) Generate(ctx context.Context, text string) (vec []float32, retErr error) {
	start := time.Now()
	defer func() { trace.Record("embed.generate", start, retErr) }()
	body, err := json.Marshal(embedRequest{
		Model: g.model,
		Input: text,
	})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.host+"/api/embed", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := g.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama API call failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama returned status %d: %s", resp.StatusCode, string(data))
	}

	var result embedResponse
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("failed to parse ollama response: %w", err)
	}
	if result.Error != "" {
		return nil, fmt.Errorf("ollama error: %s", result.Error)
	}
	if len(result.Embeddings) == 0 || len(result.Embeddings[0]) == 0 {
		return nil, fmt.Errorf("no embedding returned from ollama")
	}
	vec = result.Embeddings[0]
	if g.expectedDim > 0 && len(vec) != g.expectedDim {
		return nil, fmt.Errorf(
			"embedding dimension mismatch: model %q returned %d dimensions, expected %d — check that your embedding model matches the Qdrant collection size (hint: nomic-embed-text produces %d-dim vectors)",
			g.model, len(vec), g.expectedDim, g.expectedDim,
		)
	}
	return vec, nil
}
