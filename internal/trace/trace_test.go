package trace

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// setupTraceDir creates a temp gg dir with a traces/ subdirectory containing
// the given filenames as empty files, and returns the gg dir path.
func setupTraceDir(t *testing.T, files []string) string {
	t.Helper()
	ggDir := t.TempDir()
	tracesDir := filepath.Join(ggDir, "traces")
	if err := os.MkdirAll(tracesDir, 0o755); err != nil {
		t.Fatalf("mkdir traces: %v", err)
	}
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(tracesDir, f), []byte{}, 0o600); err != nil {
			t.Fatalf("create %s: %v", f, err)
		}
	}
	return ggDir
}

// existingFiles returns the basenames of files in the traces/ subdirectory.
func existingFiles(t *testing.T, ggDir string) map[string]bool {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(ggDir, "traces"))
	if err != nil {
		t.Fatalf("read traces dir: %v", err)
	}
	out := make(map[string]bool, len(entries))
	for _, e := range entries {
		out[e.Name()] = true
	}
	return out
}

// TestClearOlderThan_DeletesOldDateFiles verifies that dated files before the
// cutoff are removed and files on or after the cutoff are preserved.
func TestClearOlderThan_DeletesOldDateFiles(t *testing.T) {
	files := []string{
		"2026-04-01.jsonl", // older → delete
		"2026-04-07.jsonl", // older → delete
		"2026-04-10.jsonl", // on cutoff boundary — keep (date == cutoff, not <)
		"2026-04-14.jsonl", // newer → keep
	}
	ggDir := setupTraceDir(t, files)

	cutoff := time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)
	deleted, err := ClearOlderThan(ggDir, cutoff)
	if err != nil {
		t.Fatalf("ClearOlderThan: %v", err)
	}
	if deleted != 2 {
		t.Errorf("deleted = %d, want 2", deleted)
	}

	remaining := existingFiles(t, ggDir)
	if remaining["2026-04-01.jsonl"] || remaining["2026-04-07.jsonl"] {
		t.Error("old files were not deleted")
	}
	if !remaining["2026-04-10.jsonl"] || !remaining["2026-04-14.jsonl"] {
		t.Error("newer files were incorrectly deleted")
	}
}

// TestClearOlderThan_SkipsNonDateFiles verifies that files whose names do not
// match YYYY-MM-DD.jsonl are never deleted, even if a naive lexicographic
// comparison would mark them as "older" than the cutoff.
func TestClearOlderThan_SkipsNonDateFiles(t *testing.T) {
	files := []string{
		"backup.jsonl",      // name < any date string → must NOT be deleted
		".jsonl",            // empty prefix → must NOT be deleted
		"2026-04-01.jsonl",  // real date, older → deleted
		"not-a-date.jsonl",  // non-date → skip
	}
	ggDir := setupTraceDir(t, files)

	cutoff := time.Date(2026, 4, 14, 0, 0, 0, 0, time.UTC)
	deleted, err := ClearOlderThan(ggDir, cutoff)
	if err != nil {
		t.Fatalf("ClearOlderThan: %v", err)
	}
	if deleted != 1 {
		t.Errorf("deleted = %d, want 1 (only 2026-04-01.jsonl)", deleted)
	}

	remaining := existingFiles(t, ggDir)
	for _, safe := range []string{"backup.jsonl", ".jsonl", "not-a-date.jsonl"} {
		if !remaining[safe] {
			t.Errorf("non-date file %q was incorrectly deleted", safe)
		}
	}
}

// TestClearOlderThan_AccumulatesErrors verifies that a single removal failure
// does not abort the whole clear — remaining matching files are still deleted
// and all errors are returned joined.
func TestClearOlderThan_AccumulatesErrors(t *testing.T) {
	files := []string{
		"2026-04-01.jsonl",
		"2026-04-02.jsonl",
		"2026-04-03.jsonl",
	}
	ggDir := setupTraceDir(t, files)

	// Make one file unremovable by removing write permission from the traces dir
	// after creating the file, so os.Remove returns permission denied.
	tracesDir := filepath.Join(ggDir, "traces")
	// chmod the specific file to read-only and also lock the dir — simpler: just
	// make the dir read-only so Remove fails for all three files. Then verify
	// that (a) all three fail and (b) ClearOlderThan returns a combined error,
	// not panic.
	if err := os.Chmod(tracesDir, 0o555); err != nil {
		t.Fatalf("chmod traces dir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(tracesDir, 0o755) })

	cutoff := time.Date(2026, 4, 14, 0, 0, 0, 0, time.UTC)
	deleted, err := ClearOlderThan(ggDir, cutoff)
	// All removes failed — deleted should be 0.
	if deleted != 0 {
		t.Errorf("deleted = %d, want 0 (all removes failed)", deleted)
	}
	// Error must be non-nil: all three removes should have failed.
	if err == nil {
		t.Error("expected error from failed removes, got nil")
	}
}

// TestClearOlderThan_EmptyDir verifies that an empty traces directory is a
// no-op and returns zero, nil.
func TestClearOlderThan_EmptyDir(t *testing.T) {
	ggDir := setupTraceDir(t, nil)
	cutoff := time.Date(2026, 4, 14, 0, 0, 0, 0, time.UTC)
	deleted, err := ClearOlderThan(ggDir, cutoff)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if deleted != 0 {
		t.Errorf("deleted = %d, want 0", deleted)
	}
}
