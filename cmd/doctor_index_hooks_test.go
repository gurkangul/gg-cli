package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallGitIndexHooks(t *testing.T) {
	root := t.TempDir()
	if out, err := exec.Command("git", "-C", root, "init").CombinedOutput(); err != nil {
		t.Skipf("git init: %v\n%s", err, out)
	}
	if indexHooksInstalled(root) {
		t.Fatalf("indexHooksInstalled true before install")
	}
	if err := installGitIndexHooks(root); err != nil {
		t.Fatalf("installGitIndexHooks: %v", err)
	}
	// TASK-502: post-commit is now part of the installed set.
	for _, name := range []string{"pre-push", "post-merge", "post-commit"} {
		p := filepath.Join(root, ".git", "hooks", name)
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("hook %s not written: %v", name, err)
		}
		if !strings.Contains(string(data), indexHookMarker) {
			t.Errorf("%s missing marker", name)
		}
		if !strings.Contains(string(data), "gg index --changed") {
			t.Errorf("%s does not run gg index --changed", name)
		}
		// Must always exit 0 so a commit/push/merge is never blocked.
		if !strings.Contains(string(data), "exit 0") {
			t.Errorf("%s does not force exit 0 (must never block git)", name)
		}
		fi, _ := os.Stat(p)
		if fi.Mode().Perm()&0o100 == 0 {
			t.Errorf("%s not executable", name)
		}
	}
	if !indexHooksInstalled(root) {
		t.Errorf("indexHooksInstalled false after install")
	}
	// Idempotent re-run must not duplicate.
	if err := installGitIndexHooks(root); err != nil {
		t.Fatalf("re-run: %v", err)
	}
	postCommit := filepath.Join(root, ".git", "hooks", "post-commit")
	data, _ := os.ReadFile(postCommit)
	if n := strings.Count(string(data), indexHookMarker); n != 1 {
		t.Errorf("post-commit marker appears %d times after re-run, want 1", n)
	}
}

// TestIndexHooksInstalledPartial verifies the detector requires ALL hooks: a
// missing post-commit (e.g. a pre-TASK-502 install) reports not-installed so
// status/doctor prompt to (re)install.
func TestIndexHooksInstalledPartial(t *testing.T) {
	root := t.TempDir()
	if out, err := exec.Command("git", "-C", root, "init").CombinedOutput(); err != nil {
		t.Skipf("git init: %v\n%s", err, out)
	}
	hooksDir := filepath.Join(root, ".git", "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Only pre-push + post-merge present (the old TASK-471 set).
	for _, name := range []string{"pre-push", "post-merge"} {
		if err := os.WriteFile(filepath.Join(hooksDir, name), []byte(indexHookBody), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if indexHooksInstalled(root) {
		t.Errorf("indexHooksInstalled true with post-commit missing")
	}
}
