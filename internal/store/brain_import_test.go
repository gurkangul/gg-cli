package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestBrainImport_LargePayload verifies that a 2 MB JSONL line is parsed
// without error under the default 64 MB buffer.
func TestBrainImport_LargePayload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "large.jsonl")

	payload := map[string]any{
		"id": "test-large-id",
		"payload": map[string]any{
			"text": strings.Repeat("x", 2<<20), // 2 MB text value
		},
	}
	line, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	line = append(line, '\n')
	if err := os.WriteFile(path, line, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	records, err := ReadBrainJSONL(path)
	if err != nil {
		t.Fatalf("unexpected error for 2 MB line: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("want 1 record, got %d", len(records))
	}
	if records[0].ID != "test-large-id" {
		t.Errorf("want id=test-large-id, got %s", records[0].ID)
	}
}

// TestBrainImport_OversizePayload_ClearError verifies that a JSONL line
// exceeding the buffer limit produces a clear, actionable error rather than
// the opaque bufio "token too long" message.
// The buffer limit is temporarily reduced so the test doesn't allocate 65 MB.
func TestBrainImport_OversizePayload_ClearError(t *testing.T) {
	oldMax := brainScanMaxBytes
	brainScanMaxBytes = 1 << 10 // 1 KB for this test
	defer func() { brainScanMaxBytes = oldMax }()

	dir := t.TempDir()
	path := filepath.Join(dir, "oversize.jsonl")

	// 2 KB text value — exceeds the temporary 1 KB limit.
	line := fmt.Sprintf(`{"id":"x","payload":{"text":"%s"}}`, strings.Repeat("y", 2<<10))
	if err := os.WriteFile(path, []byte(line+"\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, err := ReadBrainJSONL(path)
	if err == nil {
		t.Fatal("expected error for oversize line, got nil")
	}
	if !strings.Contains(err.Error(), "exceeds 64 MB") {
		t.Errorf("expected 'exceeds 64 MB' in error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "payload too large") {
		t.Errorf("expected 'payload too large' in error, got: %v", err)
	}
}
