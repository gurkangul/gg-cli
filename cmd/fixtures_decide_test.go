// Package cmd — store-down tests for decide, reject, record commands + requireNonEmpty.
package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

// ── write commands: JSONL-first durability on a fresh project ────────────────

// TestDecide_FreshProject_WritesJSONL verifies that 'gg decide' (the deprecated
// alias for 'gg record') follows the JSONL-first path on a fresh project: the
// vector collection is not materialized yet, but the decision is still durably
// written to brain/decisions.jsonl and the command exits 0.
func TestDecide_FreshProject_WritesJSONL(t *testing.T) {
	requireOllamaOrSkip(t)
	ggDir := setupGGDir(t)
	_, _, err := execCmd(t, "decide", "use JWT for auth")
	if err != nil {
		t.Fatalf("expected exit 0 on offline decide, got: %v", err)
	}
	jsonlPath := filepath.Join(ggDir, "brain", "decisions.jsonl")
	data, readErr := os.ReadFile(jsonlPath)
	if readErr != nil {
		t.Fatalf("brain/decisions.jsonl not written: %v", readErr)
	}
	if len(data) == 0 {
		t.Error("brain/decisions.jsonl is empty")
	}
}

func TestReject_StoreDown(t *testing.T) {
	ggDir := setupGGDir(t)
	_, _, err := execCmd(t, "reject", "microservices")
	if err != nil {
		t.Fatalf("expected deprecated reject to follow JSONL-first offline path, got: %v", err)
	}
	jsonlPath := filepath.Join(ggDir, "brain", "rejections.jsonl")
	data, readErr := os.ReadFile(jsonlPath)
	if readErr != nil {
		t.Fatalf("brain/rejections.jsonl not written: %v", readErr)
	}
	if len(data) == 0 {
		t.Error("brain/rejections.jsonl is empty")
	}
}

// TestRecord_StoreDown verifies the offline-resilience contract (BUG-030 / TASK-352):
// 'gg record' must succeed (exit 0) when Qdrant is down, writing to JSONL instead.
func TestRecord_StoreDown(t *testing.T) {
	ggDir := setupGGDir(t)
	_, _, err := execCmd(t, "record", "use JWT for auth", "--reason", "stateless")
	// AC-2: caller gets exit 0; JSONL is the durable write.
	if err != nil {
		t.Fatalf("expected exit 0 on offline record, got: %v", err)
	}
	// AC-1: JSONL must be written.
	jsonlPath := filepath.Join(ggDir, "brain", "decisions.jsonl")
	data, readErr := os.ReadFile(jsonlPath)
	if readErr != nil {
		t.Fatalf("brain/decisions.jsonl not written: %v", readErr)
	}
	if len(data) == 0 {
		t.Error("brain/decisions.jsonl is empty")
	}
}

func TestRecord_DecisionStatusRejectedWritesRejection(t *testing.T) {
	ggDir := setupGGDir(t)
	_, _, err := execCmd(t, "record", "do not use Redis sessions", "--decision-status", "rejected", "--reason", "ops burden")
	if err != nil {
		t.Fatalf("expected exit 0 on offline rejected record, got: %v", err)
	}

	jsonlPath := filepath.Join(ggDir, "brain", "rejections.jsonl")
	data, readErr := os.ReadFile(jsonlPath)
	if readErr != nil {
		t.Fatalf("brain/rejections.jsonl not written: %v", readErr)
	}
	if len(data) == 0 {
		t.Error("brain/rejections.jsonl is empty")
	}
}

func TestRequireNonEmpty(t *testing.T) {
	if _, err := requireNonEmpty("title", ""); err == nil {
		t.Error("expected error for empty string")
	}
	if _, err := requireNonEmpty("title", "   "); err == nil {
		t.Error("expected error for whitespace-only string")
	}
	// double-dash stripping
	got, err := requireNonEmpty("title", "--fix broken auth")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "fix broken auth" {
		t.Errorf("expected double-dash stripped, got %q", got)
	}
	// normal non-empty
	got2, err := requireNonEmpty("title", "  valid  ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got2 != "valid" {
		t.Errorf("expected trimmed value, got %q", got2)
	}
	// double-dash only → empty after strip
	if _, err := requireNonEmpty("title", "--"); err == nil {
		t.Error("expected error when only -- is provided")
	}
}
