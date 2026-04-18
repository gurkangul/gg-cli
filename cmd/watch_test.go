package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEmitWatchItem_Pretty(t *testing.T) {
	item := watchItem{
		Type:   "event",
		Verb:   "tell",
		Origin: "agent",
		TS:     "2026-04-18T19:00:00Z",
	}
	var buf bytes.Buffer
	emitWatchItem(&buf, item, "pretty")
	got := buf.String()
	if !strings.Contains(got, "[event]") || !strings.Contains(got, "tell") || !strings.Contains(got, "agent") {
		t.Errorf("unexpected pretty event output: %q", got)
	}
}

func TestEmitWatchItem_PrettyMessage(t *testing.T) {
	item := watchItem{
		Type:    "message",
		From:    "developer",
		To:      "qa",
		Content: "TASK-192 ready",
		TaskID:  "TASK-192",
		TS:      "2026-04-18T19:00:00Z",
	}
	var buf bytes.Buffer
	emitWatchItem(&buf, item, "pretty")
	got := buf.String()
	if !strings.Contains(got, "[msg]") || !strings.Contains(got, "developer") || !strings.Contains(got, "TASK-192 ready") {
		t.Errorf("unexpected pretty message output: %q", got)
	}
	if !strings.Contains(got, "[TASK-192]") {
		t.Errorf("expected task ID in output: %q", got)
	}
}

func TestEmitWatchItem_NDJSON(t *testing.T) {
	item := watchItem{
		Type:    "message",
		From:    "dev",
		To:      "all",
		Content: "hello",
		TS:      "2026-04-18T19:00:00Z",
	}
	var buf bytes.Buffer
	emitWatchItem(&buf, item, "ndjson")
	got := strings.TrimSpace(buf.String())
	if !strings.HasPrefix(got, "{") || !strings.HasSuffix(got, "}") {
		t.Errorf("expected JSON object, got: %q", got)
	}
	if !strings.Contains(got, `"type":"message"`) {
		t.Errorf("expected type field: %q", got)
	}
}

func TestPollTelemetryFile_NewLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "telemetry.jsonl")

	// Write two entries; seek past first one.
	line1 := `{"verb":"status","origin":"human","ts":"2026-04-18T10:00:00Z"}` + "\n"
	line2 := `{"verb":"tell","origin":"agent","ts":"2026-04-18T10:01:00Z"}` + "\n"
	if err := os.WriteFile(path, []byte(line1+line2), 0o600); err != nil {
		t.Fatal(err)
	}

	// Start after line1.
	offset := int64(len(line1))
	var buf bytes.Buffer
	newOffset := pollTelemetryFile(&buf, path, offset, "")
	got := buf.String()
	if !strings.Contains(got, "tell") {
		t.Errorf("expected 'tell' event in output: %q", got)
	}
	if strings.Contains(got, "status") {
		t.Errorf("'status' event should be skipped (before offset): %q", got)
	}
	if newOffset <= offset {
		t.Errorf("expected offset to advance, got %d <= %d", newOffset, offset)
	}
}

func TestPollTelemetryFile_EventFilter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "telemetry.jsonl")

	lines := `{"verb":"status","origin":"human","ts":"2026-04-18T10:00:00Z"}` + "\n" +
		`{"verb":"tell","origin":"agent","ts":"2026-04-18T10:01:00Z"}` + "\n"
	if err := os.WriteFile(path, []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	pollTelemetryFile(&buf, path, 0, "tell")
	got := buf.String()
	if !strings.Contains(got, "tell") {
		t.Errorf("expected 'tell' in output: %q", got)
	}
	if strings.Contains(got, "status") {
		t.Errorf("'status' should be filtered out: %q", got)
	}
}

func TestSeekTelemetryOffset_ZeroTime(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "telemetry.jsonl")
	content := `{"verb":"status","origin":"human","ts":"2026-04-18T10:00:00Z"}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	// Zero sinceTime should return EOF.
	offset, err := seekTelemetryOffset(path, time.Time{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if offset != int64(len(content)) {
		t.Errorf("expected offset=%d (EOF), got %d", len(content), offset)
	}
}

func TestSeekTelemetryOffset_WithSince(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "telemetry.jsonl")

	line1 := `{"verb":"status","origin":"human","ts":"2026-04-18T09:00:00Z"}` + "\n"
	line2 := `{"verb":"tell","origin":"agent","ts":"2026-04-18T10:00:00Z"}` + "\n"
	if err := os.WriteFile(path, []byte(line1+line2), 0o600); err != nil {
		t.Fatal(err)
	}

	sinceTime, _ := time.Parse(time.RFC3339, "2026-04-18T09:30:00Z")
	offset, err := seekTelemetryOffset(path, sinceTime)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Offset should point to the start of line2.
	if offset != int64(len(line1)) {
		t.Errorf("expected offset=%d (start of line2), got %d", len(line1), offset)
	}
}
