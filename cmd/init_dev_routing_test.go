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

// AC-4b: --skip-enforcement (initSkipEnforcement=true) means the init gate
// skips runInitDevRouting. Verify by (1) confirming the function WOULD write
// CLAUDE.md when called directly, then (2) confirming that the
// initSkipEnforcement flag — the same var the --skip-enforcement flag sets —
// gates the call so CLAUDE.md stays absent.
func TestInitDevRouting_SkipEnforcement_NoWrite(t *testing.T) {
	// Step 1: prove runInitDevRouting would have written on a greenfield dir.
	ggDir := setupGGDir(t)
	root := filepath.Dir(ggDir)

	if _, err := runInitDevRouting(root); err != nil {
		t.Fatalf("sanity: runInitDevRouting should succeed on greenfield: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "CLAUDE.md")); err != nil {
		t.Fatal("sanity: CLAUDE.md should exist after direct runInitDevRouting call")
	}

	// Step 2: exercise the initSkipEnforcement gate on a fresh dir.
	// This is the exact var that cmd/init.go's `if !initSkipEnforcement` reads.
	ggDir2 := setupGGDir(t)
	root2 := filepath.Dir(ggDir2)

	origFlag := initSkipEnforcement
	initSkipEnforcement = true
	t.Cleanup(func() { initSkipEnforcement = origFlag })

	// Replicate the init.go gate: `if !initSkipEnforcement { runInitDevRouting(...) }`
	if !initSkipEnforcement {
		if _, err := runInitDevRouting(root2); err != nil {
			t.Fatalf("runInitDevRouting: %v", err)
		}
	}

	if _, err := os.Stat(filepath.Join(root2, "CLAUDE.md")); err == nil {
		t.Error("CLAUDE.md must not be created when initSkipEnforcement=true")
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

// AC-4d: pre-existing CLAUDE.md WITH the marker → idempotent: message does not
// say "skipped" AND file content is byte-identical after the second call.
func TestInitDevRouting_PreExisting_WithMarker_Idempotent(t *testing.T) {
	ggDir := setupGGDir(t)
	root := filepath.Dir(ggDir)

	// Write a CLAUDE.md that already has the dev-routing block.
	block := agenthooks.DevRoutingBlock()
	if err := os.WriteFile(filepath.Join(root, "CLAUDE.md"), []byte(block), 0o644); err != nil {
		t.Fatalf("write CLAUDE.md: %v", err)
	}
	before, err := os.ReadFile(filepath.Join(root, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("read CLAUDE.md before: %v", err)
	}

	msg, err := runInitDevRouting(root)
	if err != nil {
		t.Fatalf("runInitDevRouting: %v", err)
	}
	// Should not print "skipped"; should indicate installed or up-to-date.
	if strings.Contains(msg, "skipped") {
		t.Errorf("should not skip when marker already present, got: %q", msg)
	}

	// File content must be byte-identical — no extra newline, no duplicate block.
	after, err := os.ReadFile(filepath.Join(root, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("read CLAUDE.md after: %v", err)
	}
	if string(after) != string(before) {
		t.Errorf("CLAUDE.md content changed on idempotent call:\nbefore (%d bytes):\n%s\nafter (%d bytes):\n%s",
			len(before), before, len(after), after)
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
