package changed_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gurkangul/gg-cli/internal/index/changed"
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
	writeFile(t, f, "hello\n")
	run("add", "a.txt")
	run("commit", "-m", "init")

	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("rev-parse: %v", err)
	}
	return dir, strings.TrimSpace(string(out))
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
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

func TestHeadSHA_UnbornReturnsEmptyTreeSHA(t *testing.T) {
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v — %s", args, err, out)
		}
	}
	run("init")
	sha, err := changed.HeadSHA(context.Background(), dir)
	if err != nil {
		t.Fatalf("HeadSHA: %v", err)
	}
	if sha != changed.EmptyTreeSHA {
		t.Fatalf("HeadSHA=%q want empty tree %q", sha, changed.EmptyTreeSHA)
	}
}

func TestIsAncestor_UnbornAcceptsEmptyTreeSHA(t *testing.T) {
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v — %s", args, err, out)
		}
	}
	run("init")
	ok, err := changed.IsAncestor(context.Background(), dir, changed.EmptyTreeSHA)
	if err != nil {
		t.Fatalf("IsAncestor: %v", err)
	}
	if !ok {
		t.Fatal("expected empty tree SHA to be accepted as ancestor")
	}
}

func TestFiles_UnbornEmptyTreeIncludesUntrackedSources(t *testing.T) {
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v — %s", args, err, out)
		}
	}
	run("init")
	writeFile(t, filepath.Join(dir, "main.go"), "package main\n")
	files, err := changed.Files(context.Background(), dir, changed.EmptyTreeSHA, []string{".go"})
	if err != nil {
		t.Fatalf("Files: %v", err)
	}
	if len(files) != 1 || !strings.HasSuffix(files[0], "main.go") {
		t.Fatalf("expected main.go from unborn repo, got %v", files)
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

func TestFiles_IncludesTrackedWorkingTreeAndUntracked(t *testing.T) {
	root, headSHA := makeGitRepo(t)
	writeFile(t, filepath.Join(root, "a.txt"), "changed\n")
	writeFile(t, filepath.Join(root, "new.go"), "package main\n")

	files, err := changed.Files(context.Background(), root, headSHA, nil)
	if err != nil {
		t.Fatalf("Files: %v", err)
	}
	got := strings.Join(files, "\n")
	for _, want := range []string{"a.txt", "new.go"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %s in %v", want, files)
		}
	}
}

func TestDetailedFiles_ClassifiesModifiedAddedDeletedAndRename(t *testing.T) {
	root, _ := makeGitRepo(t)
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v — %s", args, err, out)
		}
	}
	writeFile(t, filepath.Join(root, "delete.go"), "package main\n")
	run("add", "delete.go")
	run("commit", "-m", "add delete target")
	out, err := exec.Command("git", "-C", root, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("rev-parse: %v", err)
	}
	baseSHA := strings.TrimSpace(string(out))

	writeFile(t, filepath.Join(root, "new.go"), "package main\n")
	run("mv", "a.txt", "renamed.txt")
	if err := os.Remove(filepath.Join(root, "delete.go")); err != nil {
		t.Fatalf("remove delete.go: %v", err)
	}

	changes, err := changed.DetailedFiles(context.Background(), root, baseSHA, nil)
	if err != nil {
		t.Fatalf("DetailedFiles: %v", err)
	}
	got := map[string]changed.FileChangeKind{}
	for _, change := range changes {
		got[filepath.Base(change.Path)] = change.Kind
	}
	for name, kind := range map[string]changed.FileChangeKind{
		"a.txt":       changed.FileDeleted,
		"renamed.txt": changed.FileAdded,
		"new.go":      changed.FileAdded,
		"delete.go":   changed.FileDeleted,
	} {
		if got[name] != kind {
			t.Fatalf("%s kind=%q want %q (all=%v)", name, got[name], kind, changes)
		}
	}
}

func TestFiles_ExtensionFilter_AppliesToUntracked(t *testing.T) {
	root, headSHA := makeGitRepo(t)
	writeFile(t, filepath.Join(root, "new.go"), "package main\n")
	writeFile(t, filepath.Join(root, "new.txt"), "text\n")

	files, err := changed.Files(context.Background(), root, headSHA, []string{".go"})
	if err != nil {
		t.Fatalf("Files: %v", err)
	}
	if len(files) != 1 || !strings.HasSuffix(files[0], "new.go") {
		t.Fatalf("expected only new.go, got %v", files)
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
