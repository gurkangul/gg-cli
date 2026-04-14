package runner

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestResolveIndexer_PATH verifies that a binary on PATH is found.
func TestResolveIndexer_PATH(t *testing.T) {
	// "go" is always on PATH in a Go development environment.
	path, err := ResolveIndexer("go")
	if err != nil {
		t.Fatalf("expected 'go' to be found on PATH, got: %v", err)
	}
	if path == "" {
		t.Fatal("expected non-empty path for 'go'")
	}
}

// TestResolveIndexer_GGBin verifies that a binary in ~/.gg/bin/ is found even
// when not on PATH.
func TestResolveIndexer_GGBin(t *testing.T) {
	// Write a fake executable to a temp dir, then point ~/.gg/bin at it by
	// overriding the ggBinDir resolution via the file structure.
	// Strategy: write to a known path, then adjust the resolver to look there.
	//
	// Since ggBinDir reads os.UserHomeDir(), we redirect HOME to a temp dir.
	dir := t.TempDir()
	ggBin := filepath.Join(dir, ".gg", "bin")
	if err := os.MkdirAll(ggBin, 0755); err != nil {
		t.Fatal(err)
	}

	fakeBin := filepath.Join(ggBin, "gg-test-indexer")
	if err := os.WriteFile(fakeBin, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		t.Skip("chmod-based executable test not applicable on Windows")
	}

	// Override HOME so ggBinDir() points at our temp dir.
	origHome := os.Getenv("HOME")
	t.Setenv("HOME", dir)
	defer os.Setenv("HOME", origHome)

	path, err := ResolveIndexer("gg-test-indexer")
	if err != nil {
		t.Fatalf("expected gg-test-indexer in ~/.gg/bin, got: %v", err)
	}
	if path != fakeBin {
		t.Errorf("expected %s, got %s", fakeBin, path)
	}
}

// TestResolveIndexer_Missing verifies that a nonexistent binary returns
// *ErrIndexerMissing with the correct fields.
func TestResolveIndexer_Missing(t *testing.T) {
	_, err := ResolveIndexer("gg-nonexistent-binary-xyz")
	if err == nil {
		t.Fatal("expected error for nonexistent binary, got nil")
	}

	var missing *ErrIndexerMissing
	if !errors.As(err, &missing) {
		t.Fatalf("expected *ErrIndexerMissing, got %T: %v", err, err)
	}
	if missing.Binary != "gg-nonexistent-binary-xyz" {
		t.Errorf("unexpected Binary field: %q", missing.Binary)
	}
	if missing.Hint == "" {
		t.Error("Hint must not be empty for an unknown binary")
	}
}

// TestIsIndexerMissing verifies the helper predicate.
func TestIsIndexerMissing(t *testing.T) {
	_, err := ResolveIndexer("another-nonexistent-binary-abc")
	if !IsIndexerMissing(err) {
		t.Errorf("IsIndexerMissing should return true for ErrIndexerMissing, got false for %v", err)
	}
	if IsIndexerMissing(nil) {
		t.Error("IsIndexerMissing(nil) should return false")
	}
}

// TestErrIndexerMissing_KnownHints verifies that the three standard indexers
// get their canonical install hints.
func TestErrIndexerMissing_KnownHints(t *testing.T) {
	cases := map[string]string{
		"scip-go":         "go install",
		"scip-typescript": "npm install",
		"scip-python":     "pip install",
	}

	// Ensure none of these are on PATH for the test to be meaningful.
	// If they happen to be installed, just verify the happy path doesn't error.
	for bin, wantPrefix := range cases {
		path, err := ResolveIndexer(bin)
		if err == nil {
			// Binary is installed — skip the hint check.
			t.Logf("skipping hint check for %s (installed at %s)", bin, path)
			continue
		}
		var missing *ErrIndexerMissing
		if !errors.As(err, &missing) {
			t.Errorf("%s: expected ErrIndexerMissing, got %T", bin, err)
			continue
		}
		if !containsPrefix(missing.Hint, wantPrefix) {
			t.Errorf("%s hint: want prefix %q, got %q", bin, wantPrefix, missing.Hint)
		}
	}
}

func containsPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
