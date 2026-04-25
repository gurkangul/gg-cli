// Package cmd — commit-scope tests for 50-ac-attestation.sh.
//
// Exercises the BUG-025 scenario: the hook must examine the task's attributed
// commit, not HEAD, when follow-up commits land after the task commit.
package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// runACAttestationHookTwoCommits creates a git repo with two commits:
//   - taskCommitMsg: the task's implementation commit (should contain AC refs)
//   - headCommitMsg: a follow-up cosmetic commit that becomes HEAD
//
// The task ID injected via GG_TASK_ID is "TASK-SCOPE-TEST".
// taskJSON is the `gg task get` stub response.
func runACAttestationHookTwoCommits(t *testing.T, taskJSON, taskCommitMsg, headCommitMsg string, extraEnv map[string]string) (output string, exitCode int) {
	t.Helper()

	hookPath := hookACAttestationPath(t)

	dir := t.TempDir()

	runIn := func(name string, args ...string) {
		t.Helper()
		c := exec.Command(name, args...)
		c.Dir = dir
		out, err := c.CombinedOutput()
		if err != nil {
			t.Logf("git %v: %s", args, out)
		}
	}
	runIn("git", "init", "-b", "main")
	runIn("git", "config", "user.email", "test@example.com")
	runIn("git", "config", "user.name", "Test")

	// First commit — the task's implementation (carries AC references).
	f := filepath.Join(dir, "impl.go")
	_ = os.WriteFile(f, []byte("package main\n"), 0o644)
	runIn("git", "add", ".")
	runIn("git", "commit", "-m", taskCommitMsg)

	// Second commit — a cosmetic follow-up that lands after the task commit.
	f2 := filepath.Join(dir, "README.md")
	_ = os.WriteFile(f2, []byte("follow-up"), 0o644)
	runIn("git", "add", ".")
	runIn("git", "commit", "-m", headCommitMsg)

	// Fake gg stub.
	binDir := filepath.Join(dir, "fakebin")
	_ = os.MkdirAll(binDir, 0o755)
	jsonFile := filepath.Join(dir, "task.json")
	_ = os.WriteFile(jsonFile, []byte(taskJSON), 0o644)
	fakeGG := filepath.Join(binDir, "gg")
	_ = os.WriteFile(fakeGG, []byte("#!/bin/sh\ncat "+jsonFile+"\n"), 0o755)

	env := os.Environ()
	env = append(env,
		"GG_TASK_ID=TASK-SCOPE-TEST",
		"GG_TASK_SUMMARY=test summary",
		"GG_PROJECT_ID=test-project",
		"GG_ACTOR=developer",
		"PATH="+binDir+":"+os.Getenv("PATH"),
		"HOME="+dir,
	)
	for k, v := range extraEnv {
		env = append(env, k+"="+v)
	}

	cmd := exec.Command("/bin/sh", hookPath)
	cmd.Env = env
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	output = string(out)
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}
	return output, exitCode
}

// taskJSONWithScopeTaskID returns task JSON with ID=TASK-SCOPE-TEST.
func taskJSONWithScopeTaskID(detail string) string {
	return taskJSONWith(detail) // reuse helper; GG_TASK_ID is injected via env not JSON ID
}

// TestACAttestation_TaskCommitNotHEAD_Passes: BUG-025 regression.
// The task commit (first commit) has full AC coverage; HEAD is a follow-up
// cosmetic commit without AC references. Hook must examine the task commit
// and pass.
func TestACAttestation_TaskCommitNotHEAD_Passes(t *testing.T) {
	detail := `ACCEPTANCE
- hook exits 7 when ACs not in commit
- bypass is audited`

	taskCommitMsg := `feat(TASK-SCOPE-TEST): implement hook

AC-1: hook exits 7 when ACs missing
AC-2: bypass audited via gg record`

	headCommitMsg := `docs: fix typo in README`

	json := taskJSONWithScopeTaskID(detail)
	out, code := runACAttestationHookTwoCommits(t, json, taskCommitMsg, headCommitMsg, nil)

	if code != 0 {
		t.Errorf("BUG-025: expected exit 0 — hook should examine task commit, not HEAD; got %d\noutput:\n%s", code, out)
	}
}

// TestACAttestation_TaskCommitNotHEAD_Blocks: when the task commit itself
// lacks AC coverage, the hook must still block (exit 7) even when HEAD is
// a different commit.
func TestACAttestation_TaskCommitNotHEAD_Blocks(t *testing.T) {
	detail := `ACCEPTANCE
- hook exits 7 when ACs not in commit
- bypass is audited`

	taskCommitMsg := `feat(TASK-SCOPE-TEST): implement hook — only covers AC-1

AC-1: hook exits 7 when ACs missing`
	// AC-2 deliberately omitted from task commit.

	headCommitMsg := `docs: update changelog`

	json := taskJSONWithScopeTaskID(detail)
	out, code := runACAttestationHookTwoCommits(t, json, taskCommitMsg, headCommitMsg, nil)

	if code != 7 {
		t.Errorf("expected exit 7 (AC-2 missing from task commit), got %d\noutput:\n%s", code, out)
	}
}
