package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ollamaTagsResponse mirrors the subset of Ollama's GET /api/tags payload we
// need: the list of locally-available model names.
type ollamaTagsResponse struct {
	Models []struct {
		Name string `json:"name"`
	} `json:"models"`
}

// modelPresent reports whether want is among the model names Ollama has locally.
// Ollama tags are versioned ("nomic-embed-text:latest"); a config model without
// a tag ("nomic-embed-text") matches the base name before the ':' so users who
// omit the tag are not told the model is missing.
func modelPresent(models []string, want string) bool {
	want = strings.TrimSpace(want)
	if want == "" {
		return false
	}
	wantBase := want
	if i := strings.IndexByte(want, ':'); i >= 0 {
		wantBase = want[:i]
	}
	for _, m := range models {
		m = strings.TrimSpace(m)
		if m == want {
			return true
		}
		base := m
		if i := strings.IndexByte(m, ':'); i >= 0 {
			base = m[:i]
		}
		if base == wantBase {
			return true
		}
	}
	return false
}

// embeddingModelLine builds the doctor line describing embedding-model
// readiness (AC-2). It is a pure formatter so the "ready" / "missing — run:
// ollama pull X" wording is unit-testable without a live Ollama.
//
//   - reachable=false → unknown (Ollama itself is down; the connectivity check
//     already reported that, so we stay advisory rather than double-failing).
//   - present=true    → "<model> (ready)"
//   - present=false   → "<model> (missing — run: ollama pull <model>)"
func embeddingModelLine(model string, reachable, present bool) (detail string, ok bool) {
	model = strings.TrimSpace(model)
	if model == "" {
		model = "(unset)"
	}
	if !reachable {
		return fmt.Sprintf("%s (unknown — Ollama unreachable)", model), false
	}
	if present {
		return fmt.Sprintf("%s (ready)", model), true
	}
	return fmt.Sprintf("%s (missing — run: ollama pull %s)", model, model), false
}

// fetchOllamaModels queries Ollama's /api/tags for the locally-available model
// names. It degrades gracefully: any error (Ollama down, bad JSON, timeout)
// returns reachable=false with the error rather than crashing the caller.
func fetchOllamaModels(ctx context.Context, host string) (models []string, reachable bool, err error) {
	url := strings.TrimRight(host, "/") + "/api/tags"
	reqCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return nil, false, err
	}
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, false, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, false, fmt.Errorf("ollama /api/tags returned status %d", resp.StatusCode)
	}
	var parsed ollamaTagsResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, false, fmt.Errorf("parse ollama /api/tags: %w", err)
	}
	names := make([]string, 0, len(parsed.Models))
	for _, m := range parsed.Models {
		names = append(names, m.Name)
	}
	return names, true, nil
}

// doctorCheckEmbeddingModel reports whether the configured embedding model is
// actually present in the local Ollama (AC-2). Non-fatal: a missing model is a
// warning with the exact `ollama pull` fix, and an unreachable Ollama is a
// warning (connectivity is already reported separately by doctorCheckOllama).
func doctorCheckEmbeddingModel(ctx context.Context, host, model string, report *doctorReport) {
	models, reachable, ferr := fetchOllamaModels(ctx, host)
	present := reachable && modelPresent(models, model)
	detail, ok := embeddingModelLine(model, reachable, present)
	switch {
	case ok:
		report.ok("embedding model", detail)
	case !reachable:
		// Surface the probe error but stay advisory — don't double-count the
		// Ollama outage already flagged by the connectivity check.
		if ferr != nil {
			detail += fmt.Sprintf(" [%v]", ferr)
		}
		report.warn("embedding model", detail)
	default:
		report.warn("embedding model", detail)
	}
}
