package embedding

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gurkangul/gg-cli/internal/config"
)

func testServer(t *testing.T, dims int) (*httptest.Server, *Generator) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		vec := make([]float32, dims)
		for i := range vec {
			vec[i] = float32(i) * 0.001
		}
		resp := embedResponse{Embeddings: [][]float32{vec}}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)

	gen := New(&config.EmbeddingConfig{Host: srv.URL, Model: "test-model"}, 0)
	return srv, gen
}

// TestGenerate_HappyPath verifies that a well-formed Ollama response returns
// the vector unchanged.
func TestGenerate_HappyPath(t *testing.T) {
	_, gen := testServer(t, 768)
	gen.expectedDim = 0 // no validation

	vec, err := gen.Generate(context.Background(), "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vec) != 768 {
		t.Fatalf("expected 768-dim vector, got %d", len(vec))
	}
}

// TestGenerate_DimValidation_Pass verifies that correct-dim embeddings pass.
func TestGenerate_DimValidation_Pass(t *testing.T) {
	_, gen := testServer(t, 768)
	gen.expectedDim = 768

	vec, err := gen.Generate(context.Background(), "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vec) != 768 {
		t.Fatalf("expected 768-dim vector, got %d", len(vec))
	}
}

// TestGenerate_DimValidation_Fail verifies that a wrong-dim embedding returns
// a descriptive error mentioning the model name and both dim values.
func TestGenerate_DimValidation_Fail(t *testing.T) {
	_, gen := testServer(t, 4096) // model returns 4096, we expect 4242
	gen.expectedDim = 4242

	_, err := gen.Generate(context.Background(), "hello")
	if err == nil {
		t.Fatal("expected dimension mismatch error, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "4096") {
		t.Errorf("error should mention actual dim 4096: %s", msg)
	}
	// Sentinel, not 768: the error's static hint contains "nomic-embed-text 768",
	// so asserting 768 here passed even with the "expected %d" verb deleted — it
	// claimed to check the expected dim while checking boilerplate.
	if !strings.Contains(msg, "4242") {
		t.Errorf("error should mention expected dim 4242: %s", msg)
	}
	if !strings.Contains(msg, "test-model") {
		t.Errorf("error should mention model name: %s", msg)
	}
}

// TestGenerate_NoDimValidation_SkipsCheck verifies expectedDim=0 bypasses
// the check (useful when the collection size is unknown at init time).
func TestGenerate_NoDimValidation_SkipsCheck(t *testing.T) {
	_, gen := testServer(t, 512)
	gen.expectedDim = 0

	vec, err := gen.Generate(context.Background(), "hello")
	if err != nil {
		t.Fatalf("expected no error with expectedDim=0, got: %v", err)
	}
	if len(vec) != 512 {
		t.Fatalf("expected 512-dim vector, got %d", len(vec))
	}
}

// TestGenerate_EmptyEmbedding verifies the guard against an empty Embeddings
// slice in the Ollama response (e.g. empty input or model error not surfaced
// in the error field).
func TestGenerate_EmptyEmbedding(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := embedResponse{Embeddings: [][]float32{}}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	gen := New(&config.EmbeddingConfig{Host: srv.URL, Model: "test-model"}, 768)
	_, err := gen.Generate(context.Background(), "hello")
	if err == nil {
		t.Fatal("expected error for empty embeddings, got nil")
	}
	if !strings.Contains(err.Error(), "no embedding") {
		t.Errorf("error should mention 'no embedding': %s", err.Error())
	}
}

// TestGenerate_OllamaError verifies that an error field in the Ollama response
// is surfaced as an error (not silently dropped).
func TestGenerate_OllamaError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := embedResponse{Error: "model not found: nomic-embed-text"}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	gen := New(&config.EmbeddingConfig{Host: srv.URL, Model: "nomic-embed-text"}, 768)
	_, err := gen.Generate(context.Background(), "hello")
	if err == nil {
		t.Fatal("expected error from ollama error field, got nil")
	}
	if !strings.Contains(err.Error(), "model not found") {
		t.Errorf("expected ollama error message to be surfaced: %s", err.Error())
	}
}

// TestGenerate_HTTPError verifies that non-200 responses surface the body.
func TestGenerate_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("service unavailable"))
	}))
	defer srv.Close()

	gen := New(&config.EmbeddingConfig{Host: srv.URL, Model: "test-model"}, 768)
	_, err := gen.Generate(context.Background(), "hello")
	if err == nil {
		t.Fatal("expected error for non-200 response, got nil")
	}
	if !strings.Contains(err.Error(), "503") {
		t.Errorf("error should mention status code 503: %s", err.Error())
	}
}
