package embedding

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gurkangul/gg/internal/config"
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
	host   string
	model  string
	client *http.Client
}

func New(cfg *config.EmbeddingConfig) *Generator {
	return &Generator{
		host:   cfg.Host,
		model:  cfg.Model,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

// Generate creates an embedding vector for the given text via Ollama.
func (g *Generator) Generate(text string) ([]float32, error) {
	body, err := json.Marshal(embedRequest{
		Model: g.model,
		Input: text,
	})
	if err != nil {
		return nil, err
	}

	resp, err := g.client.Post(g.host+"/api/embed", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("ollama API call failed: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama returned status %d: %s", resp.StatusCode, string(data))
	}
	if err != nil {
		return nil, err
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
