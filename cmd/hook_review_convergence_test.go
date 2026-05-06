package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func hookReviewConvergencePath(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(file))
	hookPath := filepath.Join(repoRoot, ".gg", "hooks", "pre-task-done.d", "70-review-convergence.sh")
	if _, err := os.Stat(hookPath); err != nil {
		t.Skipf("hook script not found at %s: %v", hookPath, err)
	}
	return hookPath
}

type reviewConvergenceResult struct {
	output   string
	exitCode int
}

func runReviewConvergenceHook(t *testing.T, commitMsg string, extraEnv map[string]string) reviewConvergenceResult {
	t.Helper()
	hookPath := hookReviewConvergencePath(t)
	dir := t.TempDir()

	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	runGit("init", "-b", "main")
	runGit("config", "user.email", "test@example.com")
	runGit("config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("change\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	runGit("add", ".")
	runGit("commit", "-m", commitMsg)

	binDir := filepath.Join(dir, "fakebin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir fakebin: %v", err)
	}
	fakeGG := filepath.Join(binDir, "gg")
	if err := os.WriteFile(fakeGG, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake gg: %v", err)
	}

	env := filteredReviewConvergenceEnv(os.Environ())
	env = append(env,
		"GG_TASK_ID=TASK-399",
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
	cmd.Dir = dir
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}
	return reviewConvergenceResult{output: string(out), exitCode: exitCode}
}

func filteredReviewConvergenceEnv(in []string) []string {
	out := make([]string, 0, len(in))
	for _, entry := range in {
		switch {
		case strings.HasPrefix(entry, "GG_ALLOW_INCOMPLETE_REVIEW="):
			continue
		case strings.HasPrefix(entry, "GG_REVIEW_CONVERGENCE="):
			continue
		default:
			out = append(out, entry)
		}
	}
	return out
}

func TestReviewConvergenceHook_BlocksMissingTrailer(t *testing.T) {
	r := runReviewConvergenceHook(t, "feat(TASK-399): change without convergence", nil)
	if r.exitCode != 7 {
		t.Fatalf("exitCode=%d, want 7\noutput:\n%s", r.exitCode, r.output)
	}
	for _, want := range []string{"Review-Convergence", "behavior matrix", "stale-string sweep"} {
		if !strings.Contains(r.output, want) {
			t.Errorf("output missing %q:\n%s", want, r.output)
		}
	}
}

func TestReviewConvergenceHook_AllowsTrailer(t *testing.T) {
	msg := "feat(TASK-399): change with convergence\n\nReview-Convergence: behavior matrix + negative path + legacy compatibility + stale-string sweep + docs/templates + live smoke + tests verified"
	r := runReviewConvergenceHook(t, msg, nil)
	if r.exitCode != 0 {
		t.Fatalf("exitCode=%d, want 0\noutput:\n%s", r.exitCode, r.output)
	}
}

func TestReviewConvergenceHook_WarnModeDoesNotBlock(t *testing.T) {
	r := runReviewConvergenceHook(t, "feat(TASK-399): warn only", map[string]string{"GG_REVIEW_CONVERGENCE": "warn"})
	if r.exitCode != 0 {
		t.Fatalf("exitCode=%d, want 0\noutput:\n%s", r.exitCode, r.output)
	}
	if !strings.Contains(r.output, "Review-Convergence") {
		t.Errorf("warn output should still explain missing trailer:\n%s", r.output)
	}
}
