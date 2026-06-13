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
)

type embedRequest struct {
	Model string `json:"model"`
	Input string `json:"input"`
}

type embedResponse struct {
	Embeddings [][]float32 `json:"embeddings"`
	Error      string      `json:"error,omitempty"`
}

// ollamaBackend talks to a local Ollama server's /api/embed endpoint. It is the
// default, fully-offline backend. The HTTP request shape is byte-identical to
// the pre-refactor Generator so the default path is unchanged — whether Ollama
// runs in Docker or natively makes no difference here (it is just an HTTP host).
// Dimension validation is performed by the Generator wrapper, not here.
type ollamaBackend struct {
	host   string
	model  string
	client *http.Client
}

func newOllamaBackend(cfg *config.EmbeddingConfig) *ollamaBackend {
	return &ollamaBackend{
		host:   cfg.Host,
		model:  cfg.Model,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

// modelIdentity returns the bare model name for the Ollama backend. It is
// deliberately NOT backend-prefixed: existing installs recorded "nomic-embed-text"
// in embedding-meta.json, so prefixing would spuriously trip the mismatch guard
// on every upgrade. A switch to Voyage is still detected because the Voyage
// identity IS prefixed (and the dim differs), so the two can never collide.
func (b *ollamaBackend) modelIdentity() string { return b.model }

func (b *ollamaBackend) generate(ctx context.Context, text string) ([]float32, error) {
	body, err := json.Marshal(embedRequest{
		Model: b.model,
		Input: text,
	})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.host+"/api/embed", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := b.client.Do(req)
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
	return result.Embeddings[0], nil
}
