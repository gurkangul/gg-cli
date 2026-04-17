package graph

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestReadChunksJSONL_LargePayload verifies that a 2 MB JSONL line is parsed
// without error under the default 64 MiB buffer.
func TestReadChunksJSONL_LargePayload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "large.jsonl")

	record := map[string]any{
		"id":    "file:large-test",
		"label": "File",
		"properties": map[string]any{
			"path": strings.Repeat("a", 2<<20), // 2 MB path value
		},
	}
	line, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	line = append(line, '\n')
	if err := os.WriteFile(path, line, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	records, err := readChunksJSONL(path)
	if err != nil {
		t.Fatalf("unexpected error for 2 MB line: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("want 1 record, got %d", len(records))
	}
	if records[0].ID != "file:large-test" {
		t.Errorf("want id=file:large-test, got %s", records[0].ID)
	}
}

// TestReadChunksJSONL_OversizePayload_ClearError verifies that a JSONL line
// exceeding the buffer limit produces a clear, actionable error.
// The buffer limit is temporarily reduced so the test doesn't allocate 65 MiB.
func TestReadChunksJSONL_OversizePayload_ClearError(t *testing.T) {
	oldMax := jsonlMaxLineBytes
	jsonlMaxLineBytes = 1 << 10 // 1 KB for this test
	defer func() { jsonlMaxLineBytes = oldMax }()

	dir := t.TempDir()
	path := filepath.Join(dir, "oversize.jsonl")

	// 2 KB path — exceeds the temporary 1 KB limit.
	line := fmt.Sprintf(`{"id":"file:x","label":"File","properties":{"path":"%s"}}`, strings.Repeat("b", 2<<10))
	if err := os.WriteFile(path, []byte(line+"\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, err := readChunksJSONL(path)
	if err == nil {
		t.Fatal("expected error for oversize line, got nil")
	}
	if !strings.Contains(err.Error(), "exceeds") {
		t.Errorf("expected 'exceeds' in error, got: %v", err)
	}
}
