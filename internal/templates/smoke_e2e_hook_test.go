// Package templates — behavioural tests for the 05-smoke-e2e.sh hook.
// These exercise the hook script directly (via /bin/sh + the embedded body)
// in a fresh temp workdir so we can assert its no-op / pass / fail paths.
package templates

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// runSmokeHook drops the embedded script into a temp dir, chmods it, chdirs
// to dir (where Makefile fixtures live), and runs it with the given env.
// Returns exit code; -1 on setup failure (which t.Fatal's).
func runSmokeHook(t *testing.T, dir string, env ...string) int {
	t.Helper()
	scriptPath := filepath.Join(t.TempDir(), "05-smoke-e2e.sh")
	if err := os.WriteFile(scriptPath, []byte(SmokeE2EHook), 0o755); err != nil {
		t.Fatalf("write hook: %v", err)
	}
	cmd := exec.Command("/bin/sh", scriptPath)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	err := cmd.Run()
	if err == nil {
		return 0
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode()
	}
	t.Fatalf("hook run: %v", err)
	return -1
}

// no-Makefile — fresh project; hook must no-op (exit 0) so projects that
// have not adopted the tier pattern are not punished.
func TestSmokeHook_NoMakefile_SkipsQuietly(t *testing.T) {
	dir := t.TempDir()
	if got := runSmokeHook(t, dir); got != 0 {
		t.Errorf("expected exit 0 when no Makefile, got %d", got)
	}
}

// Makefile-without-target — target absence must also produce a quiet skip.
// This is the "added gg but still wiring up Makefile" transitional state.
func TestSmokeHook_MakefileWithoutTarget_SkipsQuietly(t *testing.T) {
	dir := t.TempDir()
	mkBody := "test-unit:\n\techo ran\n"
	if err := os.WriteFile(filepath.Join(dir, "Makefile"), []byte(mkBody), 0o644); err != nil {
		t.Fatalf("write Makefile: %v", err)
	}
	if got := runSmokeHook(t, dir); got != 0 {
		t.Errorf("expected exit 0 when test-smoke target is absent, got %d", got)
	}
}

// happy path — target exists and passes; hook exits 0.
func TestSmokeHook_TargetPasses_ExitsZero(t *testing.T) {
	dir := t.TempDir()
	mkBody := ".PHONY: test-smoke\ntest-smoke:\n\t@echo smoke ok\n"
	if err := os.WriteFile(filepath.Join(dir, "Makefile"), []byte(mkBody), 0o644); err != nil {
		t.Fatalf("write Makefile: %v", err)
	}
	if got := runSmokeHook(t, dir); got != 0 {
		t.Errorf("expected exit 0 on passing smoke, got %d", got)
	}
}

// sad path — target exists and fails; hook MUST exit non-zero so the
// pre-task-done runner wraps it as ExitVerifyFailed and blocks the task.
func TestSmokeHook_TargetFails_ExitsNonZero(t *testing.T) {
	dir := t.TempDir()
	mkBody := ".PHONY: test-smoke\ntest-smoke:\n\t@echo failing; exit 1\n"
	if err := os.WriteFile(filepath.Join(dir, "Makefile"), []byte(mkBody), 0o644); err != nil {
		t.Fatalf("write Makefile: %v", err)
	}
	got := runSmokeHook(t, dir)
	if got == 0 {
		t.Fatal("expected non-zero exit when test-smoke fails — this is the load-bearing gate")
	}
}

// opt-out env var — even when the target would fail, GG_NO_SMOKE=1 must
// exit 0 so emergency bypass works without touching code.
func TestSmokeHook_OptOut_Bypasses(t *testing.T) {
	dir := t.TempDir()
	mkBody := ".PHONY: test-smoke\ntest-smoke:\n\t@exit 1\n"
	if err := os.WriteFile(filepath.Join(dir, "Makefile"), []byte(mkBody), 0o644); err != nil {
		t.Fatalf("write Makefile: %v", err)
	}
	if got := runSmokeHook(t, dir, "GG_NO_SMOKE=1"); got != 0 {
		t.Errorf("GG_NO_SMOKE=1 must bypass, got exit %d", got)
	}
}
