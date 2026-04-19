package cmd

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// setupTestRepo creates a bare git repo with one commit (the "broken" ref) and
// returns the repo dir and the broken-ref SHA. The caller's working directory
// is changed to the repo root and restored via t.Cleanup.
func setupTestRepo(t *testing.T) (repoDir, brokenSHA string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found in PATH — skipping worktree tests")
	}

	dir := t.TempDir()
	run := func(args ...string) string {
		t.Helper()
		c := exec.Command("git", args...)
		c.Dir = dir
		c.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test",
		)
		out, err := c.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return strings.TrimSpace(string(out))
	}

	run("init", "-b", "main")
	run("config", "user.email", "test@test")
	run("config", "user.name", "test")

	// Initial commit — this becomes the "broken ref".
	placeholder := filepath.Join(dir, "placeholder.txt")
	if err := os.WriteFile(placeholder, []byte("before fix\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "placeholder.txt")
	run("commit", "-m", "broken state")
	sha := run("rev-parse", "HEAD")

	// Switch CWD to the repo so relative paths and git worktree work.
	orig, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(orig) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	return dir, sha
}

func TestVerifyReproWithWorktree_ReproPassesAtBrokenRef_Rejected(t *testing.T) {
	_, brokenSHA := setupTestRepo(t)

	// A repro that always exits 0 (never proves the bug existed).
	reproFile := filepath.Join(t.TempDir(), "repro.sh")
	if err := os.WriteFile(reproFile, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	err := verifyReproWithWorktree(context.Background(), reproFile, brokenSHA)
	if err == nil {
		t.Fatal("expected error when repro passes at broken ref, got nil")
	}

	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected *ExitError, got %T: %v", err, err)
	}
	if exitErr.Code != ExitVerifyFailed {
		t.Fatalf("expected ExitVerifyFailed (%d), got %d", ExitVerifyFailed, exitErr.Code)
	}
	if !strings.Contains(exitErr.Message, "exited 0 at") {
		t.Fatalf("expected 'exited 0 at' in message, got: %s", exitErr.Message)
	}
}

func TestVerifyReproWithWorktree_InvalidSHA_Error(t *testing.T) {
	setupTestRepo(t)

	reproFile := filepath.Join(t.TempDir(), "repro.sh")
	if err := os.WriteFile(reproFile, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	err := verifyReproWithWorktree(context.Background(), reproFile, "deadbeefdeadbeef")
	if err == nil {
		t.Fatal("expected error for invalid SHA, got nil")
	}
	if strings.Contains(err.Error(), "exited 0") {
		t.Fatalf("wrong error type — expected worktree add failure, got: %v", err)
	}
}

func TestVerifyReproWithWorktree_HappyPath(t *testing.T) {
	dir, brokenSHA := setupTestRepo(t)

	// Write fix.txt at HEAD — repro checks for its presence.
	fixFile := filepath.Join(dir, "fix.txt")
	if err := os.WriteFile(fixFile, []byte("fixed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		t.Helper()
		c := exec.Command("git", args...)
		c.Dir = dir
		c.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test",
		)
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	run("add", "fix.txt")
	run("commit", "-m", "fix")

	// Repro: fails if fix.txt is absent (broken ref), passes if present (HEAD).
	reproFile := filepath.Join(dir, "repro.sh")
	script := "#!/bin/sh\ntest -f fix.txt\n"
	if err := os.WriteFile(reproFile, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := verifyReproWithWorktree(context.Background(), reproFile, brokenSHA); err != nil {
		t.Fatalf("expected no error on happy path, got: %v", err)
	}
}

func TestExtractGoTestName_Found(t *testing.T) {
	f := filepath.Join(t.TempDir(), "foo_test.go")
	content := "package foo\nimport \"testing\"\nfunc TestMyFeature(t *testing.T) {}\n"
	if err := os.WriteFile(f, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := extractGoTestName(f); got != "TestMyFeature" {
		t.Fatalf("expected TestMyFeature, got %q", got)
	}
}

func TestExtractGoTestName_Fallback(t *testing.T) {
	f := filepath.Join(t.TempDir(), "foo_test.go")
	if err := os.WriteFile(f, []byte("package foo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := extractGoTestName(f); got != "." {
		t.Fatalf("expected '.', got %q", got)
	}
}
