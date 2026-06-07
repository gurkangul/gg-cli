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
	if err := installGitIndexHooks(root); err != nil {
		t.Fatalf("installGitIndexHooks: %v", err)
	}
	for _, name := range []string{"pre-push", "post-merge"} {
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
		fi, _ := os.Stat(p)
		if fi.Mode().Perm()&0o100 == 0 {
			t.Errorf("%s not executable", name)
		}
	}
	// Idempotent re-run must not duplicate.
	if err := installGitIndexHooks(root); err != nil {
		t.Fatalf("re-run: %v", err)
	}
}
