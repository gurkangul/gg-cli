package config

import (
	"fmt"
	"net/url"
	"strings"
)

// Embedding backend identifiers. The Ollama backend is the offline default;
// Voyage is an opt-in cloud backend that sends text off-machine (privacy
// trade-off) in exchange for higher-quality embeddings.
const (
	BackendOllama = "ollama"
	BackendVoyage = "voyage"
)

type EmbeddingConfig struct {
	// Backend selects the embedding provider. Empty/"ollama" (default) uses the
	// local Ollama HTTP API at Host; "voyage" uses the Voyage AI cloud API.
	Backend string `yaml:"backend,omitempty"`
	Host    string `yaml:"host"`
	Model   string `yaml:"model"`
	// Voyage holds the optional Voyage cloud-backend settings. It is only
	// consulted when Backend == "voyage". The API key is NEVER stored here —
	// it is read from the environment variable named by Voyage.APIKeyEnv.
	Voyage VoyageConfig `yaml:"voyage,omitempty"`
}

// VoyageConfig configures the optional Voyage AI cloud embedding backend.
// Off by default; opt in by setting embedding.backend: voyage. The API key is
// never persisted in config.yaml — set it in the shell env (default
// VOYAGE_API_KEY) so secrets stay out of the repo.
type VoyageConfig struct {
	// Model is the Voyage embedding model. Default voyage-3.5-lite.
	Model string `yaml:"model,omitempty"`
	// OutputDim is the requested embedding dimension. Voyage supports
	// 256/512/1024/2048; default 1024. This differs from nomic's 768, so a
	// backend switch requires `gg reembed` (the meta guard enforces this).
	OutputDim int `yaml:"output_dim,omitempty"`
	// APIKeyEnv names the environment variable holding the Voyage API key.
	// Default VOYAGE_API_KEY. Never put the key itself in config.yaml.
	APIKeyEnv string `yaml:"api_key_env,omitempty"`
}

// Voyage backend defaults.
const (
	DefaultVoyageModel     = "voyage-3.5-lite"
	DefaultVoyageOutputDim = 1024
	DefaultVoyageAPIKeyEnv = "VOYAGE_API_KEY"
)

// validVoyageDims is the set of output dimensions the Voyage embeddings API
// accepts for the voyage-3.5 family. Requesting anything else returns an API
// error, so reject it early with a clear message.
var validVoyageDims = map[int]struct{}{256: {}, 512: {}, 1024: {}, 2048: {}}

// applyEmbeddingDefaults resolves an empty Backend to the offline Ollama default
// (so configs written before this field existed are unchanged) and stamps the
// Voyage sub-defaults so a partial voyage block still has a usable model/dim/env.
func (e *EmbeddingConfig) applyEmbeddingDefaults() {
	if strings.TrimSpace(e.Backend) == "" {
		e.Backend = BackendOllama
	}
	if strings.TrimSpace(e.Voyage.Model) == "" {
		e.Voyage.Model = DefaultVoyageModel
	}
	if e.Voyage.OutputDim == 0 {
		e.Voyage.OutputDim = DefaultVoyageOutputDim
	}
	if strings.TrimSpace(e.Voyage.APIKeyEnv) == "" {
		e.Voyage.APIKeyEnv = DefaultVoyageAPIKeyEnv
	}
}

// resolvedBackend returns the effective backend id, treating empty as the
// Ollama default. Callers should ensure ApplyDefaults ran, but this keeps the
// validate path correct even if it didn't.
func (e *EmbeddingConfig) resolvedBackend() string {
	if b := strings.TrimSpace(e.Backend); b != "" {
		return strings.ToLower(b)
	}
	return BackendOllama
}

// ResolvedBackendName is the exported form of resolvedBackend, used by the
// command layer (init/doctor) to make the embedding probe backend-aware: probe
// Ollama only under the Ollama backend, check the API-key env under Voyage.
func (e *EmbeddingConfig) ResolvedBackendName() string {
	return e.resolvedBackend()
}

// validate checks the embedding configuration for the selected backend. The
// host-URL requirement only applies to the local Ollama backend; the Voyage
// backend talks to a fixed cloud endpoint and instead requires a model, a
// supported output dimension, and the name of an env var for the API key.
func (e *EmbeddingConfig) validate() error {
	switch e.resolvedBackend() {
	case BackendOllama:
		if strings.TrimSpace(e.Host) == "" {
			return fmt.Errorf("embedding.host is required")
		}
		u, err := url.Parse(e.Host)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("embedding.host must be a full URL including scheme (http:// or https://), got %q", e.Host)
		}
		if strings.TrimSpace(e.Model) == "" {
			return fmt.Errorf("embedding.model is required")
		}
	case BackendVoyage:
		if strings.TrimSpace(e.Voyage.Model) == "" {
			return fmt.Errorf("embedding.voyage.model is required when embedding.backend is %q", BackendVoyage)
		}
		dim := e.Voyage.OutputDim
		if dim == 0 {
			dim = DefaultVoyageOutputDim
		}
		if _, ok := validVoyageDims[dim]; !ok {
			return fmt.Errorf("embedding.voyage.output_dim %d is not supported (Voyage accepts 256, 512, 1024, or 2048)", dim)
		}
		if strings.TrimSpace(e.Voyage.APIKeyEnv) == "" {
			return fmt.Errorf("embedding.voyage.api_key_env is required when embedding.backend is %q (e.g. %s)", BackendVoyage, DefaultVoyageAPIKeyEnv)
		}
	default:
		return fmt.Errorf("embedding.backend %q is not supported (use %q or %q)", e.Backend, BackendOllama, BackendVoyage)
	}
	return nil
}
