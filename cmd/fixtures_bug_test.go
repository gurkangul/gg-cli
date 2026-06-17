// Package cmd — fresh-project (un-reembedded) tests for bug commands + bug
// helper functions. With the embedded SQLite store there is no "store down"
// state: write commands persist JSONL-first, reads of a missing bug return
// not-found, and reads of an un-materialized collection surface a not-found
// error until `gg reembed` materializes the collections.
package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

// tempReproFile creates a minimal *_test.go file in the CWD (set by setupGGDir)
// and returns its relative path for use with --repro in tests.
func tempReproFile(t *testing.T) string {
	t.Helper()
	name := "repro_stub_test.go"
	if err := os.WriteFile(name, []byte("package cmd\n"), 0o644); err != nil {
		t.Fatalf("write repro stub: %v", err)
	}
	return name
}

// TestBugReport_StoreDown verifies offline-resilience (BUG-030 / TASK-352):
// 'gg bug report' must succeed (exit 0) when Qdrant is down, writing to JSONL.
func TestBugReport_StoreDown(t *testing.T) {
	ggDir := setupGGDir(t)
	_, _, err := execCmd(t, "bug", "report", "nil pointer in search")
	// AC-2: caller gets exit 0.
	if err != nil {
		t.Fatalf("expected exit 0 on offline bug report, got: %v", err)
	}
	// AC-1: JSONL must be written.
	jsonlPath := filepath.Join(ggDir, "brain", "bugs.jsonl")
	data, readErr := os.ReadFile(jsonlPath)
	if readErr != nil {
		t.Fatalf("brain/bugs.jsonl not written: %v", readErr)
	}
	if len(data) == 0 {
		t.Error("brain/bugs.jsonl is empty")
	}
}

func TestBugList_FreshProject_NotMaterialized(t *testing.T) {
	setupGGDir(t)
	// bug list reads the bugs collection directly; on a fresh project it is not
	// materialized, so the read surfaces a not-found error (no Qdrant-down
	// sentinel exists anymore).
	if _, _, err := execCmd(t, "bug", "list"); err == nil {
		t.Fatal("expected a not-found error listing an un-materialized bugs collection")
	}
}

func TestBugGet_FreshProject_NotFound(t *testing.T) {
	setupGGDir(t)
	_, _, err := execCmd(t, "bug", "get", "BUG-001")
	if err == nil {
		t.Fatal("expected an error fetching a non-existent bug")
	}
	ee, ok := err.(*ExitError)
	if !ok {
		t.Fatalf("expected *ExitError, got %T: %v", err, err)
	}
	if ee.Code != ExitNotFound {
		t.Errorf("expected ExitNotFound(%d) for a missing bug, got %d", ExitNotFound, ee.Code)
	}
}

func TestBugFix_FreshProject_Fails(t *testing.T) {
	setupGGDir(t)
	repro := tempReproFile(t)
	// Fixing a bug that does not exist on a fresh project must fail (gate or
	// not-found) rather than silently succeed.
	if _, _, err := execCmd(t, "bug", "fix", "BUG-001", "nil deref in search handler", "--root-cause", "missing nil check", "--repro", repro); err == nil {
		t.Fatal("expected an error fixing a non-existent bug on a fresh project")
	}
}

func TestBugStart_FreshProject_Fails(t *testing.T) {
	setupGGDir(t)
	if _, _, err := execCmd(t, "bug", "start", "BUG-001"); err == nil {
		t.Fatal("expected an error starting a non-existent bug on a fresh project")
	}
}

func TestBugWontFix_FreshProject_Fails(t *testing.T) {
	setupGGDir(t)
	repro := tempReproFile(t)
	if _, _, err := execCmd(t, "bug", "wontfix", "BUG-001", "not reproducible", "--repro", repro); err == nil {
		t.Fatal("expected an error wontfix-ing a non-existent bug on a fresh project")
	}
}

func TestBugTriage_FreshProject_Fails(t *testing.T) {
	setupGGDir(t)
	if _, _, err := execCmd(t, "bug", "triage", "BUG-001"); err == nil {
		t.Fatal("expected an error triaging a non-existent bug on a fresh project")
	}
}

func TestBugStatusIcon(t *testing.T) {
	cases := []struct {
		status string
		want   string
	}{
		{"fixed", "✓"},
		{"wontfix", "–"},
		{"fixing", "→"},
		{"open", "!"},
		{"unknown", "!"},
		{"", "!"},
	}
	for _, tc := range cases {
		got := bugStatusIcon(tc.status)
		if got != tc.want {
			t.Errorf("bugStatusIcon(%q) = %q, want %q", tc.status, got, tc.want)
		}
	}
}

func TestBugReport_InvalidSeverity(t *testing.T) {
	setupGGDir(t)
	_, _, err := execCmd(t, "bug", "report", "nil pointer in search", "--severity", "mega-critical")
	// severity validation happens before loadDeps
	if err == nil {
		t.Fatal("expected error for invalid severity")
	}
}
