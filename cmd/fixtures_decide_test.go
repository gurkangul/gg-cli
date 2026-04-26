// Package cmd — store-down tests for decide, reject, record commands + requireNonEmpty.
package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

// ── write commands: fail at loadDeps (ExitStoreDown / exit 6) ────────────────

// TestDecide_StoreDown verifies that 'gg decide' returns a store-down error
// when Qdrant is unreachable. This exercises the full decide handler up to
// the store call and back.
func TestDecide_StoreDown(t *testing.T) {
	setupGGDir(t)
	_, _, err := execCmd(t, "decide", "use JWT for auth")
	if err == nil {
		t.Fatal("expected error when Qdrant is down")
	}
	ee, ok := err.(*ExitError)
	if !ok {
		t.Fatalf("expected *ExitError, got %T: %v", err, err)
	}
	if ee.Code != ExitStoreDown {
		t.Errorf("expected ExitStoreDown(%d), got %d", ExitStoreDown, ee.Code)
	}
}

func TestReject_StoreDown(t *testing.T) {
	setupGGDir(t)
	_, _, err := execCmd(t, "reject", "microservices")
	if err == nil {
		t.Fatal("expected error when Qdrant is down")
	}
	ee, ok := err.(*ExitError)
	if !ok {
		t.Fatalf("expected *ExitError, got %T: %v", err, err)
	}
	if ee.Code != ExitStoreDown {
		t.Errorf("expected ExitStoreDown(%d), got %d", ExitStoreDown, ee.Code)
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
