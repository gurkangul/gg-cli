package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestIndexHooksDetached verifies a fresh install writes the DETACHED hook form
// (nohup … & to a log) so git never blocks on indexing, while keeping the
// invariants the older foreground template guaranteed (marker, exit 0).
func TestIndexHooksDetached(t *testing.T) {
	root := t.TempDir()
	if out, err := exec.Command("git", "-C", root, "init").CombinedOutput(); err != nil {
		t.Skipf("git init: %v\n%s", err, out)
	}
	if err := installGitIndexHooks(root); err != nil {
		t.Fatalf("installGitIndexHooks: %v", err)
	}
	for _, name := range indexHookNames {
		data, err := os.ReadFile(filepath.Join(root, ".git", "hooks", name))
		if err != nil {
			t.Fatalf("hook %s: %v", name, err)
		}
		body := string(data)
		for _, want := range []string{"nohup gg index --changed", ".gg/index-hook.log", "</dev/null &", "exit 0", indexHookMarker} {
			if !strings.Contains(body, want) {
				t.Errorf("%s missing %q\n%s", name, want, body)
			}
		}
	}
}

// TestIndexHooksUpgradeOldForeground verifies that re-installing over an OUTDATED
// gg-owned hook rewrites it to the current (detached) template — the whole point
// of the upgrade path, since the old code skipped on any marker match and so the
// new template never reached existing installs.
func TestIndexHooksUpgradeOldForeground(t *testing.T) {
	root := t.TempDir()
	if out, err := exec.Command("git", "-C", root, "init").CombinedOutput(); err != nil {
		t.Skipf("git init: %v\n%s", err, out)
	}
	hooksDir := filepath.Join(root, ".git", "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Simulate a pre-detached gg-owned hook (foreground template shape).
	oldBody := "#!/bin/sh\n# " + indexHookMarker + " — installed by gg\n" +
		"# Foreground, non-blocking.\n" +
		"command -v gg >/dev/null 2>&1 || exit 0\n" +
		"gg index --changed >/dev/null 2>&1 || echo stale >&2\nexit 0\n"
	prePush := filepath.Join(hooksDir, "pre-push")
	if err := os.WriteFile(prePush, []byte(oldBody), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := installGitIndexHooks(root); err != nil {
		t.Fatalf("installGitIndexHooks: %v", err)
	}
	data, _ := os.ReadFile(prePush)
	got := string(data)
	if !strings.Contains(got, "nohup gg index --changed") {
		t.Errorf("outdated gg hook not upgraded to detached form:\n%s", got)
	}
	if n := strings.Count(got, indexHookMarker); n != 1 {
		t.Errorf("marker appears %d times after upgrade, want 1", n)
	}
}

// TestIndexHooksForeignNotOverwritten verifies a foreign hook is appended to (not
// clobbered) and gets the detached stanza; upgrade must never rewrite user hooks.
func TestIndexHooksForeignNotOverwritten(t *testing.T) {
	root := t.TempDir()
	if out, err := exec.Command("git", "-C", root, "init").CombinedOutput(); err != nil {
		t.Skipf("git init: %v\n%s", err, out)
	}
	hooksDir := filepath.Join(root, ".git", "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	foreign := "#!/bin/sh\necho 'my custom pre-push'\n"
	prePush := filepath.Join(hooksDir, "pre-push")
	if err := os.WriteFile(prePush, []byte(foreign), 0o755); err != nil {
		t.Fatal(err)
	}

	// Install twice: the second run must be idempotent (no duplicate stanza).
	for i := range 2 {
		if err := installGitIndexHooks(root); err != nil {
			t.Fatalf("installGitIndexHooks run %d: %v", i, err)
		}
	}
	data, _ := os.ReadFile(prePush)
	got := string(data)
	if !strings.Contains(got, "echo 'my custom pre-push'") {
		t.Errorf("foreign hook content lost:\n%s", got)
	}
	if !strings.Contains(got, "nohup gg index --changed") {
		t.Errorf("detached gg stanza not appended:\n%s", got)
	}
	// The appended stanza must background the whole group with redirected fds
	// ({ … } >log </dev/null &), else the backgrounded `A && B` subshell keeps
	// git's stdout/stderr pipe open and git BLOCKS until the index finishes.
	if !strings.Contains(got, "{ command -v gg") || !strings.Contains(got, "} >>.gg/index-hook.log 2>&1 </dev/null &") {
		t.Errorf("appended stanza not group-redirected (git would block):\n%s", got)
	}
	if n := strings.Count(got, indexHookMarker); n != 1 {
		t.Errorf("marker appears %d times on foreign hook, want 1 (idempotent)", n)
	}
}

// TestAcquireIndexLockSerializes verifies the concurrency guard: while one run
// holds the lock a second acquire is refused (skip, not race), and the lock is
// reusable after release.
func TestAcquireIndexLockSerializes(t *testing.T) {
	ggDir := t.TempDir()
	root := t.TempDir()

	release, acquired, err := acquireIndexLock(ggDir, root, "go")
	if err != nil || !acquired {
		t.Fatalf("first acquire: acquired=%v err=%v", acquired, err)
	}

	// Held by this live process → second acquire must be refused.
	if r2, ok2, err2 := acquireIndexLock(ggDir, root, "go"); err2 != nil || ok2 {
		t.Errorf("second acquire should be refused while held: acquired=%v err=%v", ok2, err2)
		if r2 != nil {
			r2()
		}
	}

	release()

	// After release the lock is free again.
	release3, acquired3, err3 := acquireIndexLock(ggDir, root, "go")
	if err3 != nil || !acquired3 {
		t.Fatalf("acquire after release: acquired=%v err=%v", acquired3, err3)
	}
	release3()
}

// TestAcquireIndexLockReclaimsStale verifies a lock left behind by an unclean
// kill (live-looking PID but an ancient start time) is reclaimed rather than
// wedging indexing forever via PID reuse. Guards the fix for review finding #3.
func TestAcquireIndexLockReclaimsStale(t *testing.T) {
	ggDir := t.TempDir()
	root := t.TempDir()
	// os.Getpid() is alive, but started_at is far past indexLockStaleAfter.
	stale := fmt.Sprintf(`{"pid":%d,"started_at":"2000-01-01T00:00:00Z","lang":"go","root":%q}`, os.Getpid(), root)
	if err := os.WriteFile(filepath.Join(ggDir, indexLockFile), []byte(stale), 0o600); err != nil {
		t.Fatal(err)
	}
	if indexLockActive(ggDir) {
		t.Errorf("a lock older than indexLockStaleAfter must report inactive")
	}
	release, acquired, err := acquireIndexLock(ggDir, root, "go")
	if err != nil || !acquired {
		t.Fatalf("stale lock should be reclaimed: acquired=%v err=%v", acquired, err)
	}
	release()
}

// TestIndexHooksPreserveUserEditedGGHook verifies the upgrade path does NOT
// overwrite a gg-owned hook the user has since added their own command to —
// silently discarding that edit. Guards the fix for review finding #4.
func TestIndexHooksPreserveUserEditedGGHook(t *testing.T) {
	root := t.TempDir()
	if out, err := exec.Command("git", "-C", root, "init").CombinedOutput(); err != nil {
		t.Skipf("git init: %v\n%s", err, out)
	}
	hooksDir := filepath.Join(root, ".git", "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// gg-owned (marker, no append marker) but with a user-added command line.
	edited := "#!/bin/sh\n# " + indexHookMarker + " — installed by gg\n" +
		"command -v gg >/dev/null 2>&1 || exit 0\n" +
		"gg index --changed >/dev/null 2>&1 || true\n" +
		"make lint   # user's own addition\n" +
		"exit 0\n"
	prePush := filepath.Join(hooksDir, "pre-push")
	if err := os.WriteFile(prePush, []byte(edited), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := installGitIndexHooks(root); err != nil {
		t.Fatalf("install: %v", err)
	}
	got, _ := os.ReadFile(prePush)
	if !strings.Contains(string(got), "make lint") {
		t.Errorf("user edit was silently discarded on upgrade:\n%s", got)
	}
	if strings.Contains(string(got), "nohup gg index --changed") {
		t.Errorf("user-edited gg hook must not be overwritten to the detached template:\n%s", got)
	}
}
