package cmd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gurkangul/gg-cli/internal/index/changed"
	"github.com/gurkangul/gg-cli/internal/index/runner"
	"github.com/gurkangul/gg-cli/internal/index/state"
)

func TestIndexWatchLockPreventsSecondWatcher(t *testing.T) {
	ggDir := t.TempDir()
	release, err := acquireIndexWatchLock(ggDir, indexWatchLock{
		PID:       os.Getpid(),
		StartedAt: time.Now().UTC().Format(time.RFC3339),
		Lang:      "go",
		Root:      t.TempDir(),
	})
	if err != nil {
		t.Fatalf("acquire first lock: %v", err)
	}
	defer release()

	_, err = acquireIndexWatchLock(ggDir, indexWatchLock{PID: os.Getpid(), Lang: "go"})
	if err == nil || !strings.Contains(err.Error(), "already running") {
		t.Fatalf("expected already running error, got %v", err)
	}
}

func TestIndexWatchLockReleaseRemovesFile(t *testing.T) {
	ggDir := t.TempDir()
	release, err := acquireIndexWatchLock(ggDir, indexWatchLock{PID: os.Getpid(), Lang: "go"})
	if err != nil {
		t.Fatalf("acquire lock: %v", err)
	}
	release()
	if _, err := os.Stat(filepath.Join(ggDir, indexWatchLockFile)); !os.IsNotExist(err) {
		t.Fatalf("lock should be removed, got %v", err)
	}
}

func TestCodeGraphStatusShowsRunningWatcher(t *testing.T) {
	ggDir := t.TempDir()
	release, err := acquireIndexWatchLock(ggDir, indexWatchLock{
		PID:       os.Getpid(),
		StartedAt: "2026-05-03T00:00:00Z",
		Lang:      "go",
	})
	if err != nil {
		t.Fatalf("acquire lock: %v", err)
	}
	defer release()

	status := codeGraphStatus{NoWatcherStarted: true}
	status.fillWatcher(ggDir)
	if status.NoWatcherStarted {
		t.Fatal("watcher should be marked as running")
	}
	if !strings.Contains(status.Watcher, "lang=go") {
		t.Fatalf("unexpected watcher detail: %q", status.Watcher)
	}
}

func TestIndexWatchNeedsRun_UsesLanguageStateNotTopLevelSHA(t *testing.T) {
	root, ggDir := setupIndexStatusRepo(t)
	writeIndexWatchFile(t, filepath.Join(root, "go.mod"), "module example.com/app\ngo 1.22\n")
	writeIndexWatchFile(t, filepath.Join(root, "main.go"), "package main\nfunc main() {}\n")
	writeIndexWatchFile(t, filepath.Join(root, "package.json"), "{\"name\":\"example\"}\n")
	writeIndexWatchFile(t, filepath.Join(root, "src", "index.ts"), "export const value = 1\n")
	git(t, root, "add", ".")
	git(t, root, "commit", "-m", "initial mixed project")
	goBase := strings.TrimSpace(git(t, root, "rev-parse", "HEAD"))

	writeIndexWatchFile(t, filepath.Join(root, "main.go"), "package main\nfunc main() { println(1) }\n")
	git(t, root, "add", "main.go")
	git(t, root, "commit", "-m", "change go source")
	topLevelTS := strings.TrimSpace(git(t, root, "rev-parse", "HEAD"))

	if err := state.WriteLanguage(ggDir, "go", goBase, "", []string{".go"}); err != nil {
		t.Fatalf("state.WriteLanguage go: %v", err)
	}
	if err := state.WriteLanguage(ggDir, "typescript", topLevelTS, "", []string{".ts", ".tsx"}); err != nil {
		t.Fatalf("state.WriteLanguage typescript: %v", err)
	}

	needs, full, err := indexWatchNeedsRun(context.Background(), root, ggDir, runner.LangGo)
	if err != nil {
		t.Fatalf("indexWatchNeedsRun: %v", err)
	}
	if !needs || full {
		t.Fatalf("needs=%v full=%v, want needs=true full=false", needs, full)
	}
}

func TestIndexWatchNeedsRun_MissingLanguageStateRunsFullIndex(t *testing.T) {
	root, ggDir := setupIndexStatusRepo(t)
	sha := gitCommit(t, root, "main.go", "package main")
	if err := state.WriteLanguage(ggDir, "typescript", sha, "", []string{".ts", ".tsx"}); err != nil {
		t.Fatalf("state.WriteLanguage: %v", err)
	}

	needs, full, err := indexWatchNeedsRun(context.Background(), root, ggDir, runner.LangGo)
	if err != nil {
		t.Fatalf("indexWatchNeedsRun: %v", err)
	}
	if !needs || !full {
		t.Fatalf("needs=%v full=%v, want needs=true full=true", needs, full)
	}
}

func TestIndexWatchNeedsRun_NonAncestorLanguageStateRunsFullIndex(t *testing.T) {
	root, ggDir := setupIndexStatusRepo(t)
	writeIndexWatchFile(t, filepath.Join(root, "go.mod"), "module example.com/app\ngo 1.22\n")
	writeIndexWatchFile(t, filepath.Join(root, "main.go"), "package main\nfunc main() {}\n")
	git(t, root, "add", ".")
	git(t, root, "commit", "-m", "initial go project")
	base := strings.TrimSpace(git(t, root, "rev-parse", "HEAD"))
	if err := state.WriteLanguage(ggDir, "go", base, "", []string{".go"}); err != nil {
		t.Fatalf("state.WriteLanguage: %v", err)
	}

	git(t, root, "checkout", "--orphan", "other")
	git(t, root, "rm", "-f", "go.mod", "main.go")
	writeIndexWatchFile(t, filepath.Join(root, "go.mod"), "module example.com/other\ngo 1.22\n")
	writeIndexWatchFile(t, filepath.Join(root, "main.go"), "package main\nfunc main() { println(\"other\") }\n")
	git(t, root, "add", "go.mod", "main.go")
	git(t, root, "commit", "-m", "other branch root")

	needs, full, err := indexWatchNeedsRun(context.Background(), root, ggDir, runner.LangGo)
	if err != nil {
		t.Fatalf("indexWatchNeedsRun: %v", err)
	}
	if !needs || !full {
		t.Fatalf("needs=%v full=%v, want needs=true full=true", needs, full)
	}
}

func TestIndexWatchNeedsRun_MatchingDirtyFingerprintDoesNotLoop(t *testing.T) {
	root, ggDir := setupIndexStatusRepo(t)
	writeIndexWatchFile(t, filepath.Join(root, "go.mod"), "module example.com/app\ngo 1.22\n")
	writeIndexWatchFile(t, filepath.Join(root, "main.go"), "package main\nfunc main() {}\n")
	git(t, root, "add", ".")
	git(t, root, "commit", "-m", "initial go project")
	base := strings.TrimSpace(git(t, root, "rev-parse", "HEAD"))
	writeIndexWatchFile(t, filepath.Join(root, "new.go"), "package main\nvar fresh = true\n")
	fingerprint, err := changed.WorkingTreeFingerprintWithNames(context.Background(), root, base, []string{".go"}, []string{"go.mod"})
	if err != nil {
		t.Fatalf("WorkingTreeFingerprintWithNames: %v", err)
	}
	if fingerprint == "" {
		t.Fatal("expected non-empty dirty fingerprint")
	}
	if err := state.WriteLanguage(ggDir, "go", base, fingerprint, []string{".go"}); err != nil {
		t.Fatalf("state.WriteLanguage: %v", err)
	}

	needs, full, err := indexWatchNeedsRun(context.Background(), root, ggDir, runner.LangGo)
	if err != nil {
		t.Fatalf("indexWatchNeedsRun: %v", err)
	}
	if needs || full {
		t.Fatalf("needs=%v full=%v, want needs=false full=false", needs, full)
	}
}

func TestIndexWatchNeedsRun_DirtyFingerprintMismatchRunsFullIndex(t *testing.T) {
	root, ggDir := setupIndexStatusRepo(t)
	writeIndexWatchFile(t, filepath.Join(root, "go.mod"), "module example.com/app\ngo 1.22\n")
	writeIndexWatchFile(t, filepath.Join(root, "main.go"), "package main\nfunc main() {}\n")
	git(t, root, "add", ".")
	git(t, root, "commit", "-m", "initial go project")
	base := strings.TrimSpace(git(t, root, "rev-parse", "HEAD"))
	newFile := filepath.Join(root, "new.go")
	writeIndexWatchFile(t, newFile, "package main\nvar fresh = true\n")
	fingerprint, err := changed.WorkingTreeFingerprintWithNames(context.Background(), root, base, []string{".go"}, []string{"go.mod"})
	if err != nil {
		t.Fatalf("WorkingTreeFingerprintWithNames: %v", err)
	}
	if err := state.WriteLanguage(ggDir, "go", base, fingerprint, []string{".go"}); err != nil {
		t.Fatalf("state.WriteLanguage: %v", err)
	}
	if err := os.Remove(newFile); err != nil {
		t.Fatalf("remove dirty file: %v", err)
	}

	needs, full, err := indexWatchNeedsRun(context.Background(), root, ggDir, runner.LangGo)
	if err != nil {
		t.Fatalf("indexWatchNeedsRun: %v", err)
	}
	if !needs || !full {
		t.Fatalf("needs=%v full=%v, want needs=true full=true", needs, full)
	}
}

func TestIndexWatchNeedsRun_DirtyFingerprintMismatchThenMatchingDoesNotLoop(t *testing.T) {
	root, ggDir := setupIndexStatusRepo(t)
	writeIndexWatchFile(t, filepath.Join(root, "go.mod"), "module example.com/app\ngo 1.22\n")
	writeIndexWatchFile(t, filepath.Join(root, "main.go"), "package main\nfunc main() {}\n")
	git(t, root, "add", ".")
	git(t, root, "commit", "-m", "initial go project")
	base := strings.TrimSpace(git(t, root, "rev-parse", "HEAD"))

	dirtyFile := filepath.Join(root, "new.go")
	writeIndexWatchFile(t, dirtyFile, "package main\nvar fresh = 1\n")
	oldFingerprint, err := changed.WorkingTreeFingerprintWithNames(context.Background(), root, base, []string{".go"}, []string{"go.mod"})
	if err != nil {
		t.Fatalf("old WorkingTreeFingerprintWithNames: %v", err)
	}
	if err := state.WriteLanguage(ggDir, "go", base, oldFingerprint, []string{".go"}); err != nil {
		t.Fatalf("state.WriteLanguage old fingerprint: %v", err)
	}

	writeIndexWatchFile(t, dirtyFile, "package main\nvar fresh = 2\n")
	needs, full, err := indexWatchNeedsRun(context.Background(), root, ggDir, runner.LangGo)
	if err != nil {
		t.Fatalf("indexWatchNeedsRun first: %v", err)
	}
	if !needs || !full {
		t.Fatalf("first needs=%v full=%v, want needs=true full=true", needs, full)
	}

	currentFingerprint, err := changed.WorkingTreeFingerprintWithNames(context.Background(), root, base, []string{".go"}, []string{"go.mod"})
	if err != nil {
		t.Fatalf("current WorkingTreeFingerprintWithNames: %v", err)
	}
	if currentFingerprint == oldFingerprint {
		t.Fatal("expected changed dirty fingerprint")
	}
	if err := state.WriteLanguage(ggDir, "go", base, currentFingerprint, []string{".go"}); err != nil {
		t.Fatalf("state.WriteLanguage current fingerprint: %v", err)
	}

	needs, full, err = indexWatchNeedsRun(context.Background(), root, ggDir, runner.LangGo)
	if err != nil {
		t.Fatalf("indexWatchNeedsRun second: %v", err)
	}
	if needs || full {
		t.Fatalf("second needs=%v full=%v, want needs=false full=false", needs, full)
	}
}

func TestIndexWatchNeedsRun_ModuleManifestChangeRunsFullIndex(t *testing.T) {
	root, ggDir := setupIndexStatusRepo(t)
	writeIndexWatchFile(t, filepath.Join(root, "go.mod"), "module example.com/app\ngo 1.22\n")
	writeIndexWatchFile(t, filepath.Join(root, "main.go"), "package main\nfunc main() {}\n")
	git(t, root, "add", ".")
	git(t, root, "commit", "-m", "initial go project")
	base := strings.TrimSpace(git(t, root, "rev-parse", "HEAD"))
	if err := state.WriteLanguage(ggDir, "go", base, "", []string{".go"}); err != nil {
		t.Fatalf("state.WriteLanguage: %v", err)
	}

	writeIndexWatchFile(t, filepath.Join(root, "go.mod"), "module example.com/app\ngo 1.23\n")
	needs, full, err := indexWatchNeedsRun(context.Background(), root, ggDir, runner.LangGo)
	if err != nil {
		t.Fatalf("indexWatchNeedsRun: %v", err)
	}
	if !needs || !full {
		t.Fatalf("needs=%v full=%v, want needs=true full=true", needs, full)
	}
}

func TestIndexWatchNeedsRun_ModuleManifestChangeIsLanguageScoped(t *testing.T) {
	root, ggDir := setupIndexStatusRepo(t)
	writeIndexWatchFile(t, filepath.Join(root, "go.mod"), "module example.com/app\ngo 1.22\n")
	writeIndexWatchFile(t, filepath.Join(root, "main.go"), "package main\nfunc main() {}\n")
	writeIndexWatchFile(t, filepath.Join(root, "package.json"), "{\"name\":\"example\"}\n")
	writeIndexWatchFile(t, filepath.Join(root, "src", "index.ts"), "export const value = 1\n")
	git(t, root, "add", ".")
	git(t, root, "commit", "-m", "initial mixed project")
	base := strings.TrimSpace(git(t, root, "rev-parse", "HEAD"))
	if err := state.WriteLanguage(ggDir, "go", base, "", []string{".go"}); err != nil {
		t.Fatalf("state.WriteLanguage go: %v", err)
	}
	if err := state.WriteLanguage(ggDir, "typescript", base, "", []string{".ts", ".tsx"}); err != nil {
		t.Fatalf("state.WriteLanguage typescript: %v", err)
	}

	writeIndexWatchFile(t, filepath.Join(root, "package.json"), "{\"name\":\"example\",\"version\":\"2.0.0\"}\n")

	goNeeds, goFull, err := indexWatchNeedsRun(context.Background(), root, ggDir, runner.LangGo)
	if err != nil {
		t.Fatalf("indexWatchNeedsRun go: %v", err)
	}
	if goNeeds || goFull {
		t.Fatalf("go needs=%v full=%v, want needs=false full=false for unrelated package.json", goNeeds, goFull)
	}
	tsNeeds, tsFull, err := indexWatchNeedsRun(context.Background(), root, ggDir, runner.LangTypeScript)
	if err != nil {
		t.Fatalf("indexWatchNeedsRun typescript: %v", err)
	}
	if !tsNeeds || !tsFull {
		t.Fatalf("typescript needs=%v full=%v, want needs=true full=true for package.json", tsNeeds, tsFull)
	}
}

func writeIndexWatchFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
