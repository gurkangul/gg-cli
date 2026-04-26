package brain_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/gurkangul/gg-cli/internal/brain"
)

// TestAppend_ReadAll_RoundTrip verifies that Append writes valid JSONL and
// ReadAll reads it back faithfully.
func TestAppend_ReadAll_RoundTrip(t *testing.T) {
	dir := t.TempDir()

	payload := map[string]any{
		"text":   "use JWT for auth",
		"reason": "stateless",
		"tags":   []any{"auth", "security"},
	}
	if err := brain.Append(dir, "decisions", "uuid-001", "test-agent", payload); err != nil {
		t.Fatalf("Append: %v", err)
	}

	entries, err := brain.ReadAll(dir, "decisions")
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	e := entries[0]
	if e.UUID != "uuid-001" {
		t.Errorf("UUID = %q, want %q", e.UUID, "uuid-001")
	}
	if e.Kind != "decisions" {
		t.Errorf("Kind = %q, want %q", e.Kind, "decisions")
	}
	if e.Author != "test-agent" {
		t.Errorf("Author = %q, want %q", e.Author, "test-agent")
	}
	if e.Payload["text"] != "use JWT for auth" {
		t.Errorf("Payload.text = %v, want %q", e.Payload["text"], "use JWT for auth")
	}
}

// TestReadAll_MissingFile returns nil, nil without error.
func TestReadAll_MissingFile(t *testing.T) {
	dir := t.TempDir()
	entries, err := brain.ReadAll(dir, "decisions")
	if err != nil {
		t.Fatalf("ReadAll on missing file: %v", err)
	}
	if entries != nil {
		t.Errorf("expected nil entries for missing file, got %v", entries)
	}
}

// TestSearchByText_SubstringMatch verifies case-insensitive text scan.
func TestSearchByText_SubstringMatch(t *testing.T) {
	dir := t.TempDir()

	payloads := []map[string]any{
		{"text": "use JWT for authentication", "reason": "stateless"},
		{"text": "use Redis for session cache", "reason": "fast"},
		{"approach": "microservices", "reason": "too complex"},
	}
	for i, p := range payloads {
		kind := "decisions"
		if i == 2 {
			kind = "rejections"
		}
		if err := brain.Append(dir, kind, "uuid-00"+string(rune('1'+i)), "agent", p); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}

	// Search decisions for "jwt" (case-insensitive).
	results, err := brain.SearchByText(dir, "decisions", "jwt")
	if err != nil {
		t.Fatalf("SearchByText: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result for 'jwt', got %d", len(results))
	}
	if results[0].Payload["text"] != "use JWT for authentication" {
		t.Errorf("unexpected match: %v", results[0].Payload["text"])
	}

	// Search rejections for "microservice".
	rejResults, err := brain.SearchByText(dir, "rejections", "microservice")
	if err != nil {
		t.Fatalf("SearchByText rejections: %v", err)
	}
	if len(rejResults) != 1 {
		t.Fatalf("expected 1 rejection result, got %d", len(rejResults))
	}
}

// TestSearchByText_EmptyQuery returns all entries.
func TestSearchByText_EmptyQuery(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 3; i++ {
		p := map[string]any{"text": "entry " + string(rune('a'+i))}
		if err := brain.Append(dir, "decisions", "uuid"+string(rune('a'+i)), "agent", p); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	results, err := brain.SearchByText(dir, "decisions", "")
	if err != nil {
		t.Fatalf("SearchByText empty: %v", err)
	}
	if len(results) != 3 {
		t.Errorf("expected 3 results for empty query, got %d", len(results))
	}
}

// TestSearch_FallsBackToJSONLWhenQdrantDown verifies that the brain package
// can serve search results from JSONL without Qdrant.  This is the store-layer
// component of AC-4/AC-5; the full cmd-level path is covered by integration tests.
func TestSearch_FallsBackToJSONLWhenQdrantDown(t *testing.T) {
	dir := t.TempDir()

	// Simulate prior writes to JSONL (these would have succeeded via AddDecision).
	payload := map[string]any{
		"text":       "offline resilience decision",
		"reason":     "qdrant was down",
		"created_at": "2026-04-26T10:00:00Z",
	}
	if err := brain.Append(dir, "decisions", "offline-uuid-001", "agent", payload); err != nil {
		t.Fatalf("Append: %v", err)
	}

	// SearchByText simulates the offline search path in cmd/search.go.
	results, err := brain.SearchByText(dir, "decisions", "offline")
	if err != nil {
		t.Fatalf("SearchByText: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one result from JSONL fallback search")
	}
	if results[0].UUID != "offline-uuid-001" {
		t.Errorf("result UUID = %q, want %q", results[0].UUID, "offline-uuid-001")
	}

	// Verify the brain dir is at the expected path.
	expectedPath := filepath.Join(dir, "brain", "decisions.jsonl")
	if _, err := brain.ReadAll(dir, "decisions"); err != nil {
		t.Errorf("ReadAll after write failed: %v (path: %s)", err, expectedPath)
	}
}

// TestReadAll_LogsMalformedSkipCount verifies that ReadAllWithCount returns the
// count of malformed lines that were skipped instead of silently discarding them.
func TestReadAll_LogsMalformedSkipCount(t *testing.T) {
	dir := t.TempDir()

	// Write one valid entry.
	if err := brain.Append(dir, "decisions", "uuid-valid", "agent", map[string]any{"text": "valid"}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	// Inject a malformed line directly into the JSONL file.
	jsonlPath := filepath.Join(dir, "brain", "decisions.jsonl")
	f, err := os.OpenFile(jsonlPath, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open jsonl: %v", err)
	}
	_, _ = f.WriteString("{not valid json\n")
	f.Close()

	// Write a second valid entry after the malformed line.
	if err := brain.Append(dir, "decisions", "uuid-valid-2", "agent", map[string]any{"text": "also valid"}); err != nil {
		t.Fatalf("Append 2: %v", err)
	}

	entries, skipped, err := brain.ReadAllWithCount(dir, "decisions")
	if err != nil {
		t.Fatalf("ReadAllWithCount: %v", err)
	}
	if skipped != 1 {
		t.Errorf("skipped = %d, want 1", skipped)
	}
	if len(entries) != 2 {
		t.Errorf("entries = %d, want 2", len(entries))
	}
}

// TestBrainAppend_ConcurrentPIDs_NoTornLines verifies that concurrent Append
// calls from goroutines (simulating concurrent PIDs) produce only valid JSONL
// lines — no torn/interleaved output.
func TestBrainAppend_ConcurrentPIDs_NoTornLines(t *testing.T) {
	dir := t.TempDir()
	const goroutines = 20
	// Use a large payload to stress the POSIX PIPE_BUF boundary.
	largeDetail := fmt.Sprintf("%04096d", 0) // 4096 zeros

	var wg sync.WaitGroup
	errs := make(chan error, goroutines)
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			payload := map[string]any{
				"text":   fmt.Sprintf("concurrent entry %d", i),
				"detail": largeDetail,
			}
			if err := brain.Append(dir, "decisions", fmt.Sprintf("uuid-%03d", i), "agent", payload); err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent Append error: %v", err)
	}

	entries, skipped, err := brain.ReadAllWithCount(dir, "decisions")
	if err != nil {
		t.Fatalf("ReadAllWithCount: %v", err)
	}
	if skipped != 0 {
		t.Errorf("skipped = %d (torn lines!), want 0", skipped)
	}
	if len(entries) != goroutines {
		t.Errorf("entries = %d, want %d", len(entries), goroutines)
	}
}
