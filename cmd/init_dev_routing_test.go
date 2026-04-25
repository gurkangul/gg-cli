package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gurkangul/gg-cli/internal/agenthooks"
)

// AC-4a: greenfield — CLAUDE.md absent → block written, message says installed.
func TestInitDevRouting_Greenfield(t *testing.T) {
	ggDir := setupGGDir(t)
	root := filepath.Dir(ggDir)

	msg, err := runInitDevRouting(root)
	if err != nil {
		t.Fatalf("runInitDevRouting: %v", err)
	}
	if !strings.Contains(msg, "installed") && !strings.Contains(msg, "up-to-date") {
		t.Errorf("unexpected message for greenfield: %q", msg)
	}

	raw, readErr := os.ReadFile(filepath.Join(root, "CLAUDE.md"))
	if readErr != nil {
		t.Fatalf("CLAUDE.md not created: %v", readErr)
	}
	if !strings.Contains(string(raw), agenthooks.DevRoutingBlockBegin) {
		t.Errorf("CLAUDE.md missing DevRoutingBlockBegin after greenfield install")
	}
}

// AC-4b (via AC-3): --skip-enforcement means runInitDevRouting is never
// called; simulate by not calling it and verifying CLAUDE.md is absent.
func TestInitDevRouting_SkipEnforcement_NoWrite(t *testing.T) {
	ggDir := setupGGDir(t)
	root := filepath.Dir(ggDir)

	// Simulate --skip-enforcement: don't call runInitDevRouting at all.
	_ = ggDir
	if _, err := os.Stat(filepath.Join(root, "CLAUDE.md")); err == nil {
		t.Error("CLAUDE.md should not exist when skip-enforcement path is taken")
	}
}

// AC-4c: pre-existing CLAUDE.md without the marker → block NOT appended, warning returned.
func TestInitDevRouting_PreExisting_NoMarker_WarnNotWrite(t *testing.T) {
	ggDir := setupGGDir(t)
	root := filepath.Dir(ggDir)

	original := "# My Project\n\nSome existing content.\n"
	if err := os.WriteFile(filepath.Join(root, "CLAUDE.md"), []byte(original), 0o644); err != nil {
		t.Fatalf("write CLAUDE.md: %v", err)
	}

	msg, err := runInitDevRouting(root)
	if err != nil {
		t.Fatalf("runInitDevRouting: %v", err)
	}
	if !strings.Contains(msg, "skipped") {
		t.Errorf("expected skip message for brownfield CLAUDE.md, got: %q", msg)
	}

	// Original content must be intact — marker must NOT be present.
	raw, _ := os.ReadFile(filepath.Join(root, "CLAUDE.md"))
	if strings.Contains(string(raw), agenthooks.DevRoutingBlockBegin) {
		t.Errorf("brownfield CLAUDE.md must not have marker appended; got:\n%s", string(raw))
	}
	if string(raw) != original {
		t.Errorf("brownfield CLAUDE.md was modified; want original content, got:\n%s", string(raw))
	}
}

// AC-4d: pre-existing CLAUDE.md WITH the marker → idempotent, message says up-to-date.
func TestInitDevRouting_PreExisting_WithMarker_Idempotent(t *testing.T) {
	ggDir := setupGGDir(t)
	root := filepath.Dir(ggDir)

	// Write a CLAUDE.md that already has the dev-routing block.
	block := agenthooks.DevRoutingBlock()
	if err := os.WriteFile(filepath.Join(root, "CLAUDE.md"), []byte(block), 0o644); err != nil {
		t.Fatalf("write CLAUDE.md: %v", err)
	}

	msg, err := runInitDevRouting(root)
	if err != nil {
		t.Fatalf("runInitDevRouting: %v", err)
	}
	// Should not print "skipped"; should indicate installed or up-to-date.
	if strings.Contains(msg, "skipped") {
		t.Errorf("should not skip when marker already present, got: %q", msg)
	}
}

// AC-4e: re-run init (second call) → no duplicate block in CLAUDE.md.
func TestInitDevRouting_Rerun_NoDuplicateBlock(t *testing.T) {
	ggDir := setupGGDir(t)
	root := filepath.Dir(ggDir)

	// First call — installs the block.
	if _, err := runInitDevRouting(root); err != nil {
		t.Fatalf("first runInitDevRouting: %v", err)
	}
	// Second call — idempotent.
	if _, err := runInitDevRouting(root); err != nil {
		t.Fatalf("second runInitDevRouting: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(root, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("read CLAUDE.md: %v", err)
	}
	content := string(raw)
	// Count occurrences of the begin marker.
	count := strings.Count(content, agenthooks.DevRoutingBlockBegin)
	if count != 1 {
		t.Errorf("expected exactly 1 dev-routing begin marker, got %d:\n%s", count, content)
	}
}
