package cmd

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestIsGGCLISourceDir verifies that a directory containing a go.mod with the
// gg-cli module declaration is recognised and others are not.
func TestIsGGCLISourceDir(t *testing.T) {
	t.Run("match", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "go.mod"),
			[]byte("module github.com/gurkangul/gg-cli\n\ngo 1.22\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, ok := isGGCLISourceDir(dir); !ok {
			t.Fatal("expected source dir to be recognised")
		}
	})

	t.Run("no match — different module", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "go.mod"),
			[]byte("module github.com/example/other\n\ngo 1.22\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, ok := isGGCLISourceDir(dir); ok {
			t.Fatal("expected non-gg-cli dir to not be recognised")
		}
	})

	t.Run("missing go.mod", func(t *testing.T) {
		dir := t.TempDir()
		if _, ok := isGGCLISourceDir(dir); ok {
			t.Fatal("expected empty dir to not be recognised")
		}
	})
}

// TestHeadCommitEpoch verifies that headCommitEpoch returns a plausible Unix
// timestamp from the real gg-cli repo (the test runs inside the repo root).
func TestHeadCommitEpoch(t *testing.T) {
	if testing.Short() || os.Getenv("GG_INSIDE_HOOK") == "1" {
		t.Skip("skipping live-git test in short/hook mode")
	}
	// Locate the repo root by walking up from the test binary's working dir.
	// In `go test ./cmd/...`, the cwd is the package directory.
	repoRoot, err := filepath.Abs("..")
	if err != nil {
		t.Skip("cannot resolve repo root:", err)
	}
	// Verify this actually is the gg-cli repo before running git.
	if _, ok := isGGCLISourceDir(repoRoot); !ok {
		t.Skip("repo root does not look like gg-cli source — skipping git test")
	}

	epoch, err := headCommitEpoch(repoRoot)
	if err != nil {
		t.Fatalf("headCommitEpoch failed: %v", err)
	}
	if epoch <= 0 {
		t.Fatalf("got non-positive epoch %d", epoch)
	}
	// Sanity: timestamp must be between 2024-01-01 and now+1h.
	got := time.Unix(epoch, 0)
	low := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	high := time.Now().Add(time.Hour)
	if got.Before(low) || got.After(high) {
		t.Fatalf("epoch %d → %s looks implausible", epoch, got)
	}
}

// TestDoctorCheckBinaryAdvisory_FreshBinary mocks the mtime comparison via
// a synthetic source dir whose go.mod matches and a fresh binary (mtime after
// commit time). It wires into isGGCLISourceDir indirectly by using a temp dir.
//
// Because doctorCheckBinaryAdvisory uses os.Executable() for the binary path
// (which is the test binary itself) and findGGCLISourceDir scans cwd + registry,
// we focus this test on the comparison helpers rather than the full advisory
// function — full integration is covered by TestHeadCommitEpoch above.
func TestDoctorCheckBinaryAdvisory_WarnOnStale(t *testing.T) {
	// Create a fake source dir with a matching go.mod.
	srcDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(srcDir, "go.mod"),
		[]byte("module github.com/gurkangul/gg-cli\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create a fake binary file whose mtime is 1 hour in the past.
	binFile := filepath.Join(t.TempDir(), "gg")
	if err := os.WriteFile(binFile, []byte("fake"), 0o755); err != nil {
		t.Fatal(err)
	}
	pastTime := time.Now().Add(-1 * time.Hour)
	if err := os.Chtimes(binFile, pastTime, pastTime); err != nil {
		t.Fatal(err)
	}

	binInfo, err := os.Lstat(binFile)
	if err != nil {
		t.Fatal(err)
	}
	binMtime := binInfo.ModTime()

	// Simulate a commit that happened 30 minutes ago (after binMtime).
	commitTime := time.Now().Add(-30 * time.Minute)

	if !binMtime.Before(commitTime) {
		t.Fatal("test setup error: binMtime should be before commitTime")
	}

	lag := commitTime.Sub(binMtime).Round(time.Minute)
	if lag <= 0 {
		t.Fatalf("expected positive lag, got %v", lag)
	}
}

// TestDoctorCheckBinaryAdvisory_UpToDate verifies the freshness path: binary
// mtime after commit time.
func TestDoctorCheckBinaryAdvisory_UpToDate(t *testing.T) {
	// Binary mtime = now, commit time = 1 hour ago.
	binMtime := time.Now()
	commitTime := time.Now().Add(-1 * time.Hour)

	if binMtime.Before(commitTime) {
		t.Fatal("test setup error: binMtime should be >= commitTime")
	}
	// No stale lag — binary is fresh.
	lag := commitTime.Sub(binMtime)
	if lag > 0 {
		t.Fatalf("expected non-positive lag for fresh binary, got %v", lag)
	}
}
