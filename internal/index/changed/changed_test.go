package changed_test

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gurkangul/gg/internal/index/changed"
)

// makeGitRepo creates a throwaway git repo with at least one commit and returns
// the repo root + the HEAD SHA.
func makeGitRepo(t *testing.T) (root, headSHA string) {
	t.Helper()
	dir := t.TempDir()

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v — %s", args, err, out)
		}
	}

	run("init")
	run("config", "user.email", "test@test.com")
	run("config", "user.name", "Test")

	// First commit
	f := filepath.Join(dir, "a.txt")
	if err := exec.Command("sh", "-c", "echo hello > "+f).Run(); err != nil {
		t.Fatalf("write file: %v", err)
	}
	run("add", "a.txt")
	run("commit", "-m", "init")

	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("rev-parse: %v", err)
	}
	return dir, strings.TrimSpace(string(out))
}

func TestIsAncestor_SelfIsAncestor(t *testing.T) {
	root, headSHA := makeGitRepo(t)
	// The current HEAD is its own ancestor.
	ok, err := changed.IsAncestor(context.Background(), root, headSHA)
	if err != nil {
		t.Fatalf("IsAncestor: %v", err)
	}
	if !ok {
		t.Error("expected HEAD to be its own ancestor")
	}
}

func TestIsAncestor_UnknownSHA(t *testing.T) {
	root, _ := makeGitRepo(t)
	// A completely made-up SHA should not be an ancestor (and should not panic).
	ok, err := changed.IsAncestor(context.Background(), root, "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef")
	if err == nil && ok {
		t.Error("expected unknown SHA to not be an ancestor")
	}
	// We accept either err!=nil OR ok==false here — git may return exit code 128
	// for an unknown object (which we map to error) or exit 1 (not-ancestor).
	_ = ok
}

func TestIsAncestor_ParentIsAncestor(t *testing.T) {
	root, firstSHA := makeGitRepo(t)

	// Add a second commit.
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if err := cmd.Run(); err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
	}
	f := filepath.Join(root, "b.txt")
	exec.Command("sh", "-c", "echo world > "+f).Run() //nolint
	run("add", "b.txt")
	run("commit", "-m", "second")

	// The first commit is an ancestor of the second.
	ok, err := changed.IsAncestor(context.Background(), root, firstSHA)
	if err != nil {
		t.Fatalf("IsAncestor: %v", err)
	}
	if !ok {
		t.Error("expected first commit to be ancestor of second")
	}
}

func TestHeadSHA_ReturnsNonEmpty(t *testing.T) {
	root, _ := makeGitRepo(t)
	sha, err := changed.HeadSHA(context.Background(), root)
	if err != nil {
		t.Fatalf("HeadSHA: %v", err)
	}
	if len(sha) != 40 {
		t.Errorf("expected 40-char SHA, got %q", sha)
	}
}

// ── Files ─────────────────────────────────────────────────────────────────────

func TestFiles_EmptyBaseSHA_ReturnsError(t *testing.T) {
	root, _ := makeGitRepo(t)
	_, err := changed.Files(context.Background(), root, "", nil)
	if err == nil {
		t.Fatal("expected error for empty baseSHA")
	}
}

func TestFiles_NoChanges_ReturnsEmpty(t *testing.T) {
	root, headSHA := makeGitRepo(t)
	// diff HEAD..HEAD — no changed files.
	files, err := changed.Files(context.Background(), root, headSHA, nil)
	if err != nil {
		t.Fatalf("Files: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("expected no files, got %v", files)
	}
}

func TestFiles_WithChanges_ReturnsChanged(t *testing.T) {
	root, firstSHA := makeGitRepo(t)

	// Add a second commit with two files.
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v — %s", args, err, out)
		}
	}
	exec.Command("sh", "-c", "echo x > "+filepath.Join(root, "foo.go")).Run() //nolint
	exec.Command("sh", "-c", "echo y > "+filepath.Join(root, "bar.ts")).Run() //nolint
	run("add", "foo.go", "bar.ts")
	run("commit", "-m", "add files")

	files, err := changed.Files(context.Background(), root, firstSHA, nil)
	if err != nil {
		t.Fatalf("Files: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 changed files, got %d: %v", len(files), files)
	}
}

func TestFiles_ExtensionFilter_OnlyReturnsMatching(t *testing.T) {
	root, firstSHA := makeGitRepo(t)

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v — %s", args, err, out)
		}
	}
	exec.Command("sh", "-c", "echo x > "+filepath.Join(root, "foo.go")).Run() //nolint
	exec.Command("sh", "-c", "echo y > "+filepath.Join(root, "bar.ts")).Run() //nolint
	run("add", "foo.go", "bar.ts")
	run("commit", "-m", "add files")

	files, err := changed.Files(context.Background(), root, firstSHA, []string{".go"})
	if err != nil {
		t.Fatalf("Files: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 .go file, got %d: %v", len(files), files)
	}
	if !strings.HasSuffix(files[0], "foo.go") {
		t.Errorf("expected foo.go, got %q", files[0])
	}
}
