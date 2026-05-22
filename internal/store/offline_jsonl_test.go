// Package store — offline-resilience tests (BUG-030 / TASK-352).
//
// These tests verify that brain-write operations succeed (exit 0, JSONL written)
// when Qdrant is unreachable, producing OutboxQueued instead of a hard failure.
package store

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestRecordOffline_WritesJSONL_NoQdrant (AC-1, AC-5): AddDecision must write
// to .gg/brain/decisions.jsonl and return OutboxQueued (not a hard error) when
// Qdrant is unreachable.
func TestRecordOffline_WritesJSONL_NoQdrant(t *testing.T) {
	c := newDownClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	dec := Decision{
		Text:   "use JWT for auth",
		Reason: "stateless, portable",
		Tags:   []string{"auth", "security"},
		Author: "test-agent",
	}

	err := c.AddDecision(ctx, dec, make([]float32, VectorSize))

	// Must return OutboxQueued (JSONL written) not a hard failure.
	if err == nil {
		t.Fatal("expected OutboxQueued error, got nil")
	}
	oq, ok := err.(*OutboxQueued)
	if !ok {
		t.Fatalf("expected *OutboxQueued, got %T: %v", err, err)
	}
	if oq.Kind != OutboxKindDecision {
		t.Errorf("OutboxQueued.Kind = %q, want %q", oq.Kind, OutboxKindDecision)
	}

	// AC-1: JSONL file must exist and contain the decision.
	jsonlPath := filepath.Join(c.dataDir, "brain", "decisions.jsonl")
	data, readErr := os.ReadFile(jsonlPath)
	if readErr != nil {
		t.Fatalf("brain/decisions.jsonl not written: %v", readErr)
	}

	var entry map[string]any
	if jsonErr := json.Unmarshal(data[:len(data)-1], &entry); jsonErr != nil { // strip trailing \n
		t.Fatalf("invalid JSONL line: %v", jsonErr)
	}
	payload, _ := entry["payload"].(map[string]any)
	if payload["text"] != "use JWT for auth" {
		t.Errorf("JSONL payload.text = %v, want %q", payload["text"], "use JWT for auth")
	}
	if entry["uuid"] == "" || entry["uuid"] == nil {
		t.Error("JSONL entry.uuid is empty")
	}
}

// TestRejectionOffline_WritesJSONL_NoQdrant (AC-1, AC-5): AddRejection must
// write to .gg/brain/rejections.jsonl and return OutboxQueued when Qdrant is down.
func TestRejectionOffline_WritesJSONL_NoQdrant(t *testing.T) {
	c := newDownClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	r := Rejection{
		Approach: "microservices",
		Reason:   "too complex for current team",
		Tags:     []string{"arch"},
		Author:   "test-agent",
	}

	err := c.AddRejection(ctx, r, make([]float32, VectorSize))
	if err == nil {
		t.Fatal("expected OutboxQueued error, got nil")
	}
	oq, ok := err.(*OutboxQueued)
	if !ok {
		t.Fatalf("expected *OutboxQueued, got %T: %v", err, err)
	}
	if oq.Kind != OutboxKindRejection {
		t.Errorf("OutboxQueued.Kind = %q, want %q", oq.Kind, OutboxKindRejection)
	}

	jsonlPath := filepath.Join(c.dataDir, "brain", "rejections.jsonl")
	data, readErr := os.ReadFile(jsonlPath)
	if readErr != nil {
		t.Fatalf("brain/rejections.jsonl not written: %v", readErr)
	}

	var entry map[string]any
	if jsonErr := json.Unmarshal(data[:len(data)-1], &entry); jsonErr != nil {
		t.Fatalf("invalid JSONL line: %v", jsonErr)
	}
	payload, _ := entry["payload"].(map[string]any)
	if payload["approach"] != "microservices" {
		t.Errorf("JSONL payload.approach = %v, want %q", payload["approach"], "microservices")
	}
}

// TestTask_CreateAndComplete_OfflineCycle (AC-1, AC-5): CreateTask must write
// to .gg/brain/tasks.jsonl and return (taskID, OutboxQueued) when Qdrant is down.
func TestTask_CreateAndComplete_OfflineCycle(t *testing.T) {
	c := newDownClient(t)
	// Seed seq file so allocTaskID skips the Qdrant bootstrap.
	seedSeqFile(t, c, taskSeqFile, 10)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	id, err := c.CreateTask(ctx, Task{
		Title:    "implement rate limiting",
		Priority: "high",
		Author:   "test-agent",
		Status:   "pending",
	}, make([]float32, VectorSize))

	if id == "" {
		t.Error("expected non-empty task ID on offline create")
	}
	if err == nil {
		t.Fatal("expected OutboxQueued error, got nil")
	}
	oq, ok := err.(*OutboxQueued)
	if !ok {
		t.Fatalf("expected *OutboxQueued, got %T: %v", err, err)
	}
	if oq.Kind != OutboxKindTask {
		t.Errorf("OutboxQueued.Kind = %q, want %q", oq.Kind, OutboxKindTask)
	}

	// AC-1: JSONL file must contain the task.
	jsonlPath := filepath.Join(c.dataDir, "brain", "tasks.jsonl")
	data, readErr := os.ReadFile(jsonlPath)
	if readErr != nil {
		t.Fatalf("brain/tasks.jsonl not written: %v", readErr)
	}

	var entry map[string]any
	if jsonErr := json.Unmarshal(data[:len(data)-1], &entry); jsonErr != nil {
		t.Fatalf("invalid JSONL line: %v", jsonErr)
	}
	payload, _ := entry["payload"].(map[string]any)
	if payload["title"] != "implement rate limiting" {
		t.Errorf("JSONL payload.title = %v, want %q", payload["title"], "implement rate limiting")
	}
	// task_id in payload must match the returned id.
	if payload["task_id"] != id {
		t.Errorf("JSONL payload.task_id = %v, want %q", payload["task_id"], id)
	}
}

// TestBugOffline_WritesJSONL_NoQdrant (AC-1, AC-5): ReportBug must write to
// .gg/brain/bugs.jsonl and return (bugID, OutboxQueued) when Qdrant is down.
func TestBugOffline_WritesJSONL_NoQdrant(t *testing.T) {
	c := newDownClient(t)
	seedSeqFile(t, c, bugSeqFile, 10)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	id, err := c.ReportBug(ctx, Bug{
		Title:    "nil pointer in search handler",
		Severity: "high",
		By:       "test-agent",
	}, make([]float32, VectorSize))

	if id == "" {
		t.Error("expected non-empty bug ID on offline report")
	}
	if err == nil {
		t.Fatal("expected OutboxQueued error, got nil")
	}
	oq, ok := err.(*OutboxQueued)
	if !ok {
		t.Fatalf("expected *OutboxQueued, got %T: %v", err, err)
	}
	if oq.Kind != OutboxKindBug {
		t.Errorf("OutboxQueued.Kind = %q, want %q", oq.Kind, OutboxKindBug)
	}

	jsonlPath := filepath.Join(c.dataDir, "brain", "bugs.jsonl")
	data, readErr := os.ReadFile(jsonlPath)
	if readErr != nil {
		t.Fatalf("brain/bugs.jsonl not written: %v", readErr)
	}

	var entry map[string]any
	if jsonErr := json.Unmarshal(data[:len(data)-1], &entry); jsonErr != nil {
		t.Fatalf("invalid JSONL line: %v", jsonErr)
	}
	payload, _ := entry["payload"].(map[string]any)
	if payload["title"] != "nil pointer in search handler" {
		t.Errorf("JSONL payload.title = %v, want %q", payload["title"], "nil pointer in search handler")
	}
}

func TestNoteOffline_WritesJSONL_NoQdrant(t *testing.T) {
	c := newDownClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	id, err := c.AddNote(ctx, Note{
		Text:   "semantic fallback should keep notes durable",
		Tags:   []string{"semantic", "fallback"},
		TaskID: "TASK-444",
	}, make([]float32, VectorSize))

	if id == "" {
		t.Error("expected non-empty note ID on offline add")
	}
	if err == nil {
		t.Fatal("expected OutboxQueued error, got nil")
	}
	oq, ok := err.(*OutboxQueued)
	if !ok {
		t.Fatalf("expected *OutboxQueued, got %T: %v", err, err)
	}
	if oq.Kind != OutboxKindNote {
		t.Errorf("OutboxQueued.Kind = %q, want %q", oq.Kind, OutboxKindNote)
	}

	jsonlPath := filepath.Join(c.dataDir, "brain", "notes.jsonl")
	data, readErr := os.ReadFile(jsonlPath)
	if readErr != nil {
		t.Fatalf("brain/notes.jsonl not written: %v", readErr)
	}
	var entry map[string]any
	if jsonErr := json.Unmarshal(data[:len(data)-1], &entry); jsonErr != nil {
		t.Fatalf("invalid JSONL line: %v", jsonErr)
	}
	payload, _ := entry["payload"].(map[string]any)
	if payload["text"] != "semantic fallback should keep notes durable" {
		t.Errorf("JSONL payload.text = %v", payload["text"])
	}
	if payload["task_id"] != "TASK-444" {
		t.Errorf("JSONL payload.task_id = %v, want TASK-444", payload["task_id"])
	}
}

func TestMessageOffline_WritesJSONL_NoQdrant(t *testing.T) {
	c := newDownClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err := c.SendMessage(ctx, Message{
		ID:       "msg-offline-1",
		FromRole: "developer",
		ToRole:   "reviewer",
		Content:  "TASK-444 fallback is ready for review",
		TaskID:   "TASK-444",
	})

	if err == nil {
		t.Fatal("expected OutboxQueued error, got nil")
	}
	oq, ok := err.(*OutboxQueued)
	if !ok {
		t.Fatalf("expected *OutboxQueued, got %T: %v", err, err)
	}
	if oq.Kind != OutboxKindMessage {
		t.Errorf("OutboxQueued.Kind = %q, want %q", oq.Kind, OutboxKindMessage)
	}

	jsonlPath := filepath.Join(c.dataDir, "brain", "messages.jsonl")
	data, readErr := os.ReadFile(jsonlPath)
	if readErr != nil {
		t.Fatalf("brain/messages.jsonl not written: %v", readErr)
	}
	var entry map[string]any
	if jsonErr := json.Unmarshal(data[:len(data)-1], &entry); jsonErr != nil {
		t.Fatalf("invalid JSONL line: %v", jsonErr)
	}
	payload, _ := entry["payload"].(map[string]any)
	if payload["content"] != "TASK-444 fallback is ready for review" {
		t.Errorf("JSONL payload.content = %v", payload["content"])
	}
	if payload["task_id"] != "TASK-444" {
		t.Errorf("JSONL payload.task_id = %v, want TASK-444", payload["task_id"])
	}
}

// TestOutboxQueued_IsIdempotentOnRetry (AC-1): writing the same UUID twice
// to brain JSONL appends two lines, but replay skips the second (UUID dedup).
// This test verifies the file structure (two lines) — replay idempotency is
// tested in the replay path.
func TestOutboxQueued_IsIdempotentOnRetry(t *testing.T) {
	c := newDownClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	dec := Decision{
		ID:     "fixed-uuid-for-idempotency-test",
		Text:   "idempotency test decision",
		Author: "test-agent",
	}

	// First write.
	_ = c.AddDecision(ctx, dec, make([]float32, VectorSize))
	// Second write with same ID — JSONL appends, outbox dedup is caller's responsibility.
	_ = c.AddDecision(ctx, dec, make([]float32, VectorSize))

	jsonlPath := filepath.Join(c.dataDir, "brain", "decisions.jsonl")
	data, err := os.ReadFile(jsonlPath)
	if err != nil {
		t.Fatalf("brain/decisions.jsonl not found: %v", err)
	}
	lines := splitLines(data)
	if len(lines) < 2 {
		t.Errorf("expected at least 2 JSONL lines after 2 writes, got %d", len(lines))
	}
}

// splitLines splits JSONL data into non-empty lines.
func splitLines(data []byte) [][]byte {
	var lines [][]byte
	start := 0
	for i, b := range data {
		if b == '\n' {
			line := data[start:i]
			if len(line) > 0 {
				lines = append(lines, line)
			}
			start = i + 1
		}
	}
	if start < len(data) {
		lines = append(lines, data[start:])
	}
	return lines
}
