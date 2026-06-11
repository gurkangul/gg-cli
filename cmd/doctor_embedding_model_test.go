package cmd

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestModelPresent(t *testing.T) {
	models := []string{"nomic-embed-text:latest", "llama3:8b"}
	cases := []struct {
		want string
		ok   bool
	}{
		{"nomic-embed-text", true},        // base name matches versioned tag
		{"nomic-embed-text:latest", true}, // exact match
		{"llama3:8b", true},
		{"llama3", true},     // base match
		{"mistral", false},   // absent
		{"", false},          // empty never matches
		{"nomic", false},     // partial prefix must NOT match
	}
	for _, tc := range cases {
		if got := modelPresent(models, tc.want); got != tc.ok {
			t.Errorf("modelPresent(%q) = %v, want %v", tc.want, got, tc.ok)
		}
	}
}

func TestEmbeddingModelLine(t *testing.T) {
	cases := []struct {
		name      string
		model     string
		reachable bool
		present   bool
		wantOK    bool
		wantSub   string
	}{
		{"ready", "nomic-embed-text", true, true, true, "nomic-embed-text (ready)"},
		{"missing", "nomic-embed-text", true, false, false, "missing — run: ollama pull nomic-embed-text"},
		{"unreachable", "nomic-embed-text", false, false, false, "Ollama unreachable"},
		{"empty-model", "", true, false, false, "(unset)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			detail, ok := embeddingModelLine(tc.model, tc.reachable, tc.present)
			if ok != tc.wantOK {
				t.Errorf("ok = %v, want %v (detail=%q)", ok, tc.wantOK, detail)
			}
			if !strings.Contains(detail, tc.wantSub) {
				t.Errorf("detail %q missing %q", detail, tc.wantSub)
			}
		})
	}
}

// TestFetchOllamaModels_Present drives fetchOllamaModels against a fake Ollama
// /api/tags endpoint so the readiness path is covered without a live Ollama.
func TestFetchOllamaModels_Present(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"models":[{"name":"nomic-embed-text:latest"},{"name":"llama3:8b"}]}`))
	}))
	defer srv.Close()

	models, reachable, err := fetchOllamaModels(context.Background(), srv.URL)
	if err != nil || !reachable {
		t.Fatalf("expected reachable with no error, got reachable=%v err=%v", reachable, err)
	}
	if !modelPresent(models, "nomic-embed-text") {
		t.Fatalf("expected nomic-embed-text present in %v", models)
	}
}

// TestFetchOllamaModels_Unreachable verifies graceful degradation when Ollama is
// down (connection refused on a closed server) — reachable=false, error set, no
// panic.
func TestFetchOllamaModels_Unreachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close() // close immediately so the port refuses connections

	_, reachable, err := fetchOllamaModels(context.Background(), url)
	if reachable {
		t.Fatalf("expected reachable=false against closed server")
	}
	if err == nil {
		t.Fatalf("expected an error against closed server")
	}
}

// TestDoctorCheckEmbeddingModel_ReadyAndMissing exercises the doctor wiring end
// to end against a fake Ollama, asserting report state for present vs absent.
func TestDoctorCheckEmbeddingModel_ReadyAndMissing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"models":[{"name":"nomic-embed-text:latest"}]}`))
	}))
	defer srv.Close()

	// Present → no problem recorded.
	r1 := &doctorReport{}
	doctorCheckEmbeddingModel(context.Background(), srv.URL, "nomic-embed-text", r1)
	if r1.problems != 0 {
		t.Errorf("ready model should not count as problem, got %d", r1.problems)
	}

	// Missing → warning (advisory, not a hard problem) but still surfaced.
	r2 := &doctorReport{}
	doctorCheckEmbeddingModel(context.Background(), srv.URL, "some-other-model", r2)
	if r2.problems != 0 {
		t.Errorf("missing model is advisory warn, must not increment problems, got %d", r2.problems)
	}
}
