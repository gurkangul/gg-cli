package embedding

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gurkangul/gg-cli/internal/config"
)

// newVoyageBackendForTest builds a voyageBackend pointed at a mock server by
// swapping its HTTP transport to rewrite the fixed cloud endpoint to srv.URL.
// This keeps the production endpoint constant (no test seam in prod code) while
// letting tests intercept the request.
func newVoyageBackendForTest(t *testing.T, srv *httptest.Server, cfg *config.EmbeddingConfig) *voyageBackend {
	t.Helper()
	b := newVoyageBackend(cfg)
	b.client = srv.Client()
	b.client.Transport = rewriteTransport{base: srv.Client().Transport, target: srv.URL}
	return b
}

// rewriteTransport redirects every request to target's host while preserving
// the path/body, so the voyageEndpoint constant is exercised verbatim.
type rewriteTransport struct {
	base   http.RoundTripper
	target string
}

func (rt rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	u, _ := req.URL.Parse(rt.target)
	req.URL.Scheme = u.Scheme
	req.URL.Host = u.Host
	base := rt.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(req)
}

// TestVoyageBackend_RequestShapeAndParse verifies the Voyage adapter sends the
// model + input + output_dimension, sets the Bearer auth header, and parses the
// returned embedding honoring the configured dimension.
func TestVoyageBackend_RequestShapeAndParse(t *testing.T) {
	const wantDim = 1024
	t.Setenv("VOYAGE_API_KEY", "sk-test-secret")

	var gotAuth, gotPath, gotCT string
	var gotBody voyageRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotCT = r.Header.Get("Content-Type")
		gotPath = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)

		vec := make([]float32, wantDim)
		for i := range vec {
			vec[i] = float32(i) * 0.0001
		}
		resp := voyageResponse{Data: []struct {
			Embedding []float32 `json:"embedding"`
		}{{Embedding: vec}}}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	cfg := &config.EmbeddingConfig{
		Backend: config.BackendVoyage,
		Voyage:  config.VoyageConfig{Model: "voyage-3.5-lite", OutputDim: wantDim, APIKeyEnv: "VOYAGE_API_KEY"},
	}
	b := newVoyageBackendForTest(t, srv, cfg)

	vec, err := b.generate(context.Background(), "hello world")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	if gotAuth != "Bearer sk-test-secret" {
		t.Errorf("Authorization header = %q, want Bearer sk-test-secret", gotAuth)
	}
	if gotCT != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", gotCT)
	}
	if gotPath != "/v1/embeddings" {
		t.Errorf("request path = %q, want /v1/embeddings", gotPath)
	}
	if gotBody.Model != "voyage-3.5-lite" {
		t.Errorf("request model = %q, want voyage-3.5-lite", gotBody.Model)
	}
	if len(gotBody.Input) != 1 || gotBody.Input[0] != "hello world" {
		t.Errorf("request input = %v, want [hello world]", gotBody.Input)
	}
	if gotBody.OutputDimension != wantDim {
		t.Errorf("request output_dimension = %d, want %d", gotBody.OutputDimension, wantDim)
	}
	if len(vec) != wantDim {
		t.Fatalf("parsed embedding dim = %d, want %d", len(vec), wantDim)
	}
}

// TestVoyageBackend_HonorsConfiguredDim proves a non-default output_dim flows
// through to the request and the parsed vector length.
func TestVoyageBackend_HonorsConfiguredDim(t *testing.T) {
	const wantDim = 256
	t.Setenv("VOYAGE_API_KEY", "sk-test")

	var gotDim int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body voyageRequest
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		gotDim = body.OutputDimension
		vec := make([]float32, wantDim)
		resp := voyageResponse{Data: []struct {
			Embedding []float32 `json:"embedding"`
		}{{Embedding: vec}}}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	cfg := &config.EmbeddingConfig{
		Backend: config.BackendVoyage,
		Voyage:  config.VoyageConfig{Model: "voyage-3.5-lite", OutputDim: wantDim, APIKeyEnv: "VOYAGE_API_KEY"},
	}
	b := newVoyageBackendForTest(t, srv, cfg)
	vec, err := b.generate(context.Background(), "x")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if gotDim != wantDim {
		t.Errorf("server saw output_dimension %d, want %d", gotDim, wantDim)
	}
	if len(vec) != wantDim {
		t.Errorf("vector len %d, want %d", len(vec), wantDim)
	}
}

// TestVoyageBackend_MissingAPIKey fails clearly when the env var is unset and
// never echoes any secret. No network call is attempted.
func TestVoyageBackend_MissingAPIKey(t *testing.T) {
	t.Setenv("VOYAGE_API_KEY", "") // explicitly empty
	cfg := &config.EmbeddingConfig{
		Backend: config.BackendVoyage,
		Voyage:  config.VoyageConfig{Model: "voyage-3.5-lite", OutputDim: 1024, APIKeyEnv: "VOYAGE_API_KEY"},
	}
	b := newVoyageBackend(cfg)
	_, err := b.generate(context.Background(), "hi")
	if err == nil {
		t.Fatal("expected error when API key is unset")
	}
	if !strings.Contains(err.Error(), "VOYAGE_API_KEY") {
		t.Errorf("error should name the env var to set, got: %v", err)
	}
}

// TestVoyageBackend_APIError surfaces a non-200 body without leaking the auth
// header / key into the error string.
func TestVoyageBackend_APIError(t *testing.T) {
	t.Setenv("VOYAGE_API_KEY", "sk-must-not-leak")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid api key"}}`))
	}))
	defer srv.Close()

	cfg := &config.EmbeddingConfig{
		Backend: config.BackendVoyage,
		Voyage:  config.VoyageConfig{Model: "voyage-3.5-lite", OutputDim: 1024, APIKeyEnv: "VOYAGE_API_KEY"},
	}
	b := newVoyageBackendForTest(t, srv, cfg)
	_, err := b.generate(context.Background(), "x")
	if err == nil {
		t.Fatal("expected error for 401 response")
	}
	if !strings.Contains(err.Error(), "invalid api key") {
		t.Errorf("error should surface the API message, got: %v", err)
	}
	if strings.Contains(err.Error(), "sk-must-not-leak") {
		t.Fatalf("SECRET LEAK: API key appeared in error: %v", err)
	}
}

// TestVoyageBackend_ModelIdentity asserts the backend-qualified identity used
// by the meta-mismatch guard.
func TestVoyageBackend_ModelIdentity(t *testing.T) {
	b := newVoyageBackend(&config.EmbeddingConfig{
		Backend: config.BackendVoyage,
		Voyage:  config.VoyageConfig{Model: "voyage-3.5-lite"},
	})
	if got := b.modelIdentity(); got != "voyage:voyage-3.5-lite" {
		t.Fatalf("modelIdentity = %q, want voyage:voyage-3.5-lite", got)
	}
}
