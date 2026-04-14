// Package cmd — unit tests for pure helper functions that need no backend.
package cmd

import (
	"testing"
)

// ── parseTags ────────────────────────────────────────────────────────────────

func TestParseTags_Empty(t *testing.T) {
	if got := parseTags(""); got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

func TestParseTags_Single(t *testing.T) {
	got := parseTags("auth")
	if len(got) != 1 || got[0] != "auth" {
		t.Errorf("got %v", got)
	}
}

func TestParseTags_Multiple(t *testing.T) {
	got := parseTags("auth, api, security")
	want := []string{"auth", "api", "security"}
	if len(got) != len(want) {
		t.Fatalf("len: got %d, want %d — %v", len(got), len(want), got)
	}
	for i, v := range want {
		if got[i] != v {
			t.Errorf("[%d]: got %q, want %q", i, got[i], v)
		}
	}
}

func TestParseTags_SkipsEmpty(t *testing.T) {
	got := parseTags(",auth,,api,")
	if len(got) != 2 {
		t.Errorf("expected 2, got %d: %v", len(got), got)
	}
}

// ── requireNonEmpty ──────────────────────────────────────────────────────────

func TestRequireNonEmpty_Valid(t *testing.T) {
	got, err := requireNonEmpty("field", "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "hello" {
		t.Errorf("got %q, want %q", got, "hello")
	}
}

func TestRequireNonEmpty_Empty(t *testing.T) {
	if _, err := requireNonEmpty("field", ""); err == nil {
		t.Fatal("expected error for empty string")
	}
}

func TestRequireNonEmpty_Whitespace(t *testing.T) {
	if _, err := requireNonEmpty("field", "   "); err == nil {
		t.Fatal("expected error for whitespace-only string")
	}
}

func TestRequireNonEmpty_StripsDashPrefix(t *testing.T) {
	got, err := requireNonEmpty("title", "--fix broken auth")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "fix broken auth" {
		t.Errorf("got %q, want %q", got, "fix broken auth")
	}
}

func TestRequireNonEmpty_DoubleDashOnly(t *testing.T) {
	if _, err := requireNonEmpty("title", "--"); err == nil {
		t.Fatal("expected error for '--' only")
	}
}

// ── normalizeTaskRef ─────────────────────────────────────────────────────────

func TestNormalizeTaskRef_Empty(t *testing.T) {
	got, err := normalizeTaskRef("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestNormalizeTaskRef_Valid(t *testing.T) {
	got, err := normalizeTaskRef("task-007")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "TASK-007" {
		t.Errorf("got %q, want %q", got, "TASK-007")
	}
}

func TestNormalizeTaskRef_Invalid(t *testing.T) {
	if _, err := normalizeTaskRef("BUG-001"); err == nil {
		t.Fatal("expected error for BUG-001 as task ref")
	}
}

// ── requireTaskID ─────────────────────────────────────────────────────────────

func TestRequireTaskID_Valid(t *testing.T) {
	got, err := requireTaskID("TASK-042")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "TASK-042" {
		t.Errorf("got %q, want %q", got, "TASK-042")
	}
}

func TestRequireTaskID_Empty(t *testing.T) {
	if _, err := requireTaskID(""); err == nil {
		t.Fatal("expected error for empty task ID")
	}
}

func TestRequireTaskID_Invalid(t *testing.T) {
	if _, err := requireTaskID("not-a-task"); err == nil {
		t.Fatal("expected error for invalid task ID format")
	}
}
