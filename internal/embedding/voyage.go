package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gurkangul/gg-cli/internal/config"
)

// voyageEndpoint is the Voyage AI embeddings API URL. Unlike Ollama, Voyage has
// a single fixed cloud endpoint, so it is not configurable.
const voyageEndpoint = "https://api.voyageai.com/v1/embeddings"

// voyageRequest is the JSON body for POST /v1/embeddings. OutputDimension lets
// callers pick 256/512/1024/2048 for the voyage-3.5 family.
type voyageRequest struct {
	Model           string   `json:"model"`
	Input           []string `json:"input"`
	OutputDimension int      `json:"output_dimension,omitempty"`
}

// voyageResponse is the relevant subset of the Voyage embeddings response.
type voyageResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
	// Voyage returns an error object on failures; capture its message so it is
	// surfaced rather than swallowed.
	Detail string `json:"detail,omitempty"`
	Error  struct {
		Message string `json:"message,omitempty"`
	} `json:"error,omitempty"`
}

// voyageBackend calls the Voyage AI cloud embeddings API. It is opt-in (off by
// default) because it sends text off-machine. The API key is read from an
// environment variable (never config, never logged).
type voyageBackend struct {
	model     string
	outputDim int
	apiKeyEnv string
	client    *http.Client
}

func newVoyageBackend(cfg *config.EmbeddingConfig) *voyageBackend {
	model := strings.TrimSpace(cfg.Voyage.Model)
	if model == "" {
		model = config.DefaultVoyageModel
	}
	dim := cfg.Voyage.OutputDim
	if dim == 0 {
		dim = config.DefaultVoyageOutputDim
	}
	keyEnv := strings.TrimSpace(cfg.Voyage.APIKeyEnv)
	if keyEnv == "" {
		keyEnv = config.DefaultVoyageAPIKeyEnv
	}
	return &voyageBackend{
		model:     model,
		outputDim: dim,
		apiKeyEnv: keyEnv,
		client:    &http.Client{Timeout: 30 * time.Second},
	}
}

// modelIdentity returns a backend-qualified identity ("voyage:<model>") so a
// switch away from Ollama is always detected by CheckMeta as a mismatch — the
// Ollama backend uses a bare model name, so the two namespaces never collide.
func (b *voyageBackend) modelIdentity() string { return "voyage:" + b.model }

func (b *voyageBackend) generate(ctx context.Context, text string) ([]float32, error) {
	apiKey := strings.TrimSpace(os.Getenv(b.apiKeyEnv))
	if apiKey == "" {
		// Do not echo any value — only name the env var the user must set.
		return nil, fmt.Errorf("voyage backend requires an API key: set the %s environment variable", b.apiKeyEnv)
	}

	body, err := json.Marshal(voyageRequest{
		Model:           b.model,
		Input:           []string{text},
		OutputDimension: b.outputDim,
	})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, voyageEndpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := b.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("voyage API call failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("voyage returned status %d: %s", resp.StatusCode, voyageErrMsg(data, resp.StatusCode))
	}

	var result voyageResponse
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("failed to parse voyage response: %w", err)
	}
	if result.Error.Message != "" {
		return nil, fmt.Errorf("voyage error: %s", result.Error.Message)
	}
	if len(result.Data) == 0 || len(result.Data[0].Embedding) == 0 {
		return nil, fmt.Errorf("no embedding returned from voyage")
	}
	return result.Data[0].Embedding, nil
}

// voyageErrMsg extracts a human-readable error from a non-200 body, falling
// back to a trimmed snippet. It never includes request headers, so the API key
// can never leak into an error string.
func voyageErrMsg(body []byte, status int) string {
	var r voyageResponse
	if err := json.Unmarshal(body, &r); err == nil {
		if r.Error.Message != "" {
			return r.Error.Message
		}
		if r.Detail != "" {
			return r.Detail
		}
	}
	msg := strings.TrimSpace(string(body))
	if msg == "" {
		return fmt.Sprintf("HTTP %d (empty body)", status)
	}
	if len(msg) > 200 {
		msg = msg[:200] + "…"
	}
	return msg
}
