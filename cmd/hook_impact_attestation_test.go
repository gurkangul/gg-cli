// Package cmd — integration tests for .gg/hooks/pre-task-done.d/60-impact-attestation.sh
//
// Tests run the shell script directly via exec.Command("sh", ...) with a
// controlled git repo and fake `gg impact` stub. They do NOT invoke Qdrant
// or Memgraph — graph-dependent tests skip when MEMGRAPH_URI is empty.
package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// hookImpactAttestationPath returns the absolute path to the hook script.
func hookImpactAttestationPath(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(file))
	hookPath := filepath.Join(repoRoot, ".gg", "hooks", "pre-task-done.d", "60-impact-attestation.sh")
	if _, err := os.Stat(hookPath); err != nil {
		t.Skipf("hook script not found at %s — skipping: %v", hookPath, err)
	}
	return hookPath
}

// impactAttestationResult holds the result of a hook run.
type impactAttestationResult struct {
	Output   string
	ExitCode int
}

// runImpactAttestationHook sets up a temp git repo with the given source files
// committed under commitMsg and runs the hook. ggImpactLines maps file path
// substrings to the line output gg impact should return for that file.
// When ggImpactLines is nil, a stub that returns empty output is used.
func runImpactAttestationHook(
	t *testing.T,
	sourceFiles []string, // file names to commit (extension determines if counted as source)
	commitMsg string,
	ggImpactLines map[string][]string, // file→lines returned by fake gg impact
	extraEnv map[string]string,
) impactAttestationResult {
	return runImpactAttestationHookWithActiveDiff(t, sourceFiles, commitMsg, ggImpactLines, extraEnv, true)
}

func runImpactAttestationHookWithActiveDiff(
	t *testing.T,
	sourceFiles []string,
	commitMsg string,
	ggImpactLines map[string][]string,
	extraEnv map[string]string,
	activeDiff bool,
) impactAttestationResult {
	return runImpactAttestationHookWithDiffOptions(t, sourceFiles, commitMsg, ggImpactLines, extraEnv, activeDiff, nil)
}

func runImpactAttestationHookWithDiffOptions(
	t *testing.T,
	sourceFiles []string,
	commitMsg string,
	ggImpactLines map[string][]string,
	extraEnv map[string]string,
	activeDiff bool,
	untrackedFiles []string,
) impactAttestationResult {
	t.Helper()

	hookPath := hookImpactAttestationPath(t)

	dir := t.TempDir()

	runIn := func(name string, args ...string) {
		t.Helper()
		c := exec.Command(name, args...)
		c.Dir = dir
		out, _ := c.CombinedOutput()
		_ = out
	}
	runIn("git", "init", "-b", "main")
	runIn("git", "config", "user.email", "test@example.com")
	runIn("git", "config", "user.name", "Test")

	// Write and commit all source files so git log --name-only returns them.
	for _, f := range sourceFiles {
		full := filepath.Join(dir, f)
		_ = os.MkdirAll(filepath.Dir(full), 0o755)
		_ = os.WriteFile(full, []byte("package x\n"), 0o644)
	}
	if len(sourceFiles) == 0 {
		// At least one file so we have a commit.
		_ = os.WriteFile(filepath.Join(dir, "README.md"), []byte("init"), 0o644)
	}
	runIn("git", "add", ".")
	runIn("git", "commit", "-m", commitMsg)

	// The hook is task/diff-aware: default fixtures exercise current
	// staged/unstaged source changes; specific tests can leave the worktree clean
	// to exercise HEAD commit fallback when GG_TASK_ID matches the commit body.
	if activeDiff {
		for _, f := range sourceFiles {
			full := filepath.Join(dir, f)
			_ = os.WriteFile(full, []byte("package x\n// active task diff\n"), 0o644)
		}
	}
	for _, f := range untrackedFiles {
		full := filepath.Join(dir, f)
		_ = os.MkdirAll(filepath.Dir(full), 0o755)
		_ = os.WriteFile(full, []byte("package x\n// untracked task diff\n"), 0o644)
	}

	// Build a fake gg stub that responds to `gg impact --compact <file>`.
	// It matches the file argument by substring against ggImpactLines keys.
	binDir := filepath.Join(dir, "fakebin")
	_ = os.MkdirAll(binDir, 0o755)
	fakeGG := filepath.Join(binDir, "gg")
	stub := buildImpactStub(ggImpactLines)
	_ = os.WriteFile(fakeGG, []byte(stub), 0o755)

	env := os.Environ()
	env = append(env,
		"GG_TASK_ID=TASK-TEST",
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

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}
	return impactAttestationResult{Output: string(out), ExitCode: exitCode}
}

// buildImpactStub returns a shell script body for a fake `gg` binary.
// The stub is called as: gg impact --compact <file>
// It matches the last argument against ggImpactLines keys (substring) and
// prints the matching lines. Falls back to empty output for unmatched files.
func buildImpactStub(lines map[string][]string) string {
	var sb strings.Builder
	sb.WriteString("#!/bin/sh\n")
	sb.WriteString("# Fake gg stub for impact-attestation tests\n")
	sb.WriteString("TARGET=\"\"\n")
	sb.WriteString("while [ \"$#\" -gt 0 ]; do\n")
	sb.WriteString("  TARGET=\"$1\"\n")
	sb.WriteString("  shift\n")
	sb.WriteString("done\n")
	for key, ls := range lines {
		fmt.Fprintf(&sb, "case \"$TARGET\" in *%s*)\n", key)
		for _, l := range ls {
			fmt.Fprintf(&sb, "  printf '%%s\\n' %q\n", l)
		}
		sb.WriteString("  ;;\nesac\n")
	}
	sb.WriteString("exit 0\n")
	return sb.String()
}

// TestImpactAttestation_MandatoryAtThreshold: 4 changed source files, no
// Impact-Reviewed: trailer → hook must exit 7 (ExitVerifyFailed).
func TestImpactAttestation_MandatoryAtThreshold(t *testing.T) {
	files := []string{"a.go", "b.go", "c.go", "d.go"}
	r := runImpactAttestationHook(t, files, "feat: change four files without trailer", nil, nil)
	if r.ExitCode != 7 {
		t.Errorf("expected exit 7 (mandatory at >=3 source files), got %d\noutput:\n%s", r.ExitCode, r.Output)
	}
	if !strings.Contains(r.Output, "Impact-Reviewed") {
		t.Errorf("expected mention of Impact-Reviewed: in output\ngot: %s", r.Output)
	}
}

func TestImpactAttestation_NoCurrentSourceDiffSkips(t *testing.T) {
	r := runImpactAttestationHook(t, nil, "chore: evidence-only closure", nil, nil)
	if r.ExitCode != 0 {
		t.Errorf("expected exit 0 for no current source diff, got %d\noutput:\n%s", r.ExitCode, r.Output)
	}
	if !strings.Contains(r.Output, "no task source diff detected") {
		t.Errorf("expected explicit no-diff skip message\ngot: %s", r.Output)
	}
}

func TestImpactAttestation_CommittedTaskHeadWithoutTrailerBlocks(t *testing.T) {
	files := []string{"a.go", "b.go", "c.go", "d.go"}
	r := runImpactAttestationHookWithActiveDiff(t, files, "feat(TASK-TEST): change four files without trailer", nil, nil, false)
	if r.ExitCode != 7 {
		t.Errorf("expected exit 7 for committed task HEAD without trailer, got %d\noutput:\n%s", r.ExitCode, r.Output)
	}
	if !strings.Contains(r.Output, "HEAD commit for TASK-TEST") {
		t.Errorf("expected HEAD task commit source in output\ngot: %s", r.Output)
	}
}

func TestImpactAttestation_ActiveDiffIgnoresUnrelatedHeadTrailer(t *testing.T) {
	files := []string{"a.go", "b.go", "c.go", "d.go"}
	commitMsg := "feat(TASK-999): previous task\n\nImpact-Reviewed: old.go — 0 callers"
	r := runImpactAttestationHookWithActiveDiff(t, files, commitMsg, nil, nil, true)
	if r.ExitCode != 7 {
		t.Errorf("expected exit 7 for active diff without current-task trailer, got %d\noutput:\n%s", r.ExitCode, r.Output)
	}
}

func TestImpactAttestation_UntrackedSourceFilesCount(t *testing.T) {
	files := []string{"a.go", "b.go", "c.go", "d.go"}
	r := runImpactAttestationHookWithDiffOptions(t, nil, "chore: base", nil, nil, false, files)
	if r.ExitCode != 7 {
		t.Errorf("expected exit 7 for untracked source files at threshold, got %d\noutput:\n%s", r.ExitCode, r.Output)
	}
	if !strings.Contains(r.Output, "changed source files: 4") {
		t.Errorf("expected untracked sources to be counted\ngot: %s", r.Output)
	}
}

func TestImpactAttestation_CleanUnrelatedHeadSkips(t *testing.T) {
	files := []string{"a.go", "b.go", "c.go", "d.go"}
	r := runImpactAttestationHookWithActiveDiff(t, files, "feat(TASK-999): unrelated task without trailer", nil, nil, false)
	if r.ExitCode != 0 {
		t.Errorf("expected exit 0 for clean unrelated HEAD, got %d\noutput:\n%s", r.ExitCode, r.Output)
	}
	if !strings.Contains(r.Output, "no task source diff detected") {
		t.Errorf("expected no task-diff skip message\ngot: %s", r.Output)
	}
}

func TestImpactAttestation_TaskIDPrefixDoesNotMatchHeadFallback(t *testing.T) {
	files := []string{"a.go", "b.go", "c.go", "d.go"}
	commitMsg := "feat(TASK-440): unrelated prefix task\n\nImpact-Reviewed: a.go — 0 callers"
	r := runImpactAttestationHookWithActiveDiff(t, files, commitMsg, nil, map[string]string{"GG_TASK_ID": "TASK-44"}, false)
	if r.ExitCode != 0 {
		t.Errorf("expected exit 0 for clean HEAD mentioning only prefix-similar task, got %d\noutput:\n%s", r.ExitCode, r.Output)
	}
	if strings.Contains(r.Output, "HEAD commit for TASK-44") {
		t.Errorf("prefix-similar task id should not trigger HEAD fallback\ngot: %s", r.Output)
	}
}

// TestImpactAttestation_AdvisoryBelowThreshold: 2 changed source files, no
// trailer → hook must exit 0 with an advisory warning on stderr.
func TestImpactAttestation_AdvisoryBelowThreshold(t *testing.T) {
	files := []string{"a.go", "b.go"}
	r := runImpactAttestationHook(t, files, "feat: change two files without trailer", nil, nil)
	if r.ExitCode != 0 {
		t.Errorf("expected exit 0 (advisory below threshold), got %d\noutput:\n%s", r.ExitCode, r.Output)
	}
	if !strings.Contains(r.Output, "advisory") {
		t.Errorf("expected 'advisory' in output for below-threshold run\ngot: %s", r.Output)
	}
}

// TestImpactAttestation_HighDependentEscalation: 1 source file but the fake
// `gg impact` returns 6 dependent lines → hook escalates to mandatory and
// exits 7 (requires Memgraph to count; skipped when MEMGRAPH_URI is empty).
func TestImpactAttestation_HighDependentEscalation(t *testing.T) {
	if os.Getenv("MEMGRAPH_URI") == "" {
		// gg impact --compact is a no-op without Memgraph; stub 6 lines to
		// simulate a high-dependent result independent of Memgraph.
		_ = 0 // proceed — stub covers this
	}
	// Stub 6 dependent lines for a.go so the hook sees MAX_DEPENDENTS=6.
	stubLines := map[string][]string{
		"a.go": {
			"cmd/foo.go — imports a",
			"cmd/bar.go — imports a",
			"cmd/baz.go — imports a",
			"cmd/qux.go — imports a",
			"internal/x/x.go — imports a",
			"internal/y/y.go — imports a",
		},
	}
	files := []string{"a.go"}
	r := runImpactAttestationHook(t, files, "feat: change one file, high deps, no trailer", stubLines, nil)
	if r.ExitCode != 7 {
		t.Errorf("expected exit 7 (escalation: >=5 dependents), got %d\noutput:\n%s", r.ExitCode, r.Output)
	}
}

// TestImpactAttestation_TrailerSatisfies: commit with Impact-Reviewed: trailer
// passes regardless of file count.
func TestImpactAttestation_TrailerSatisfies(t *testing.T) {
	files := []string{"a.go", "b.go", "c.go", "d.go", "e.go"}
	commitMsg := "feat(TASK-TEST): change five files\n\nImpact-Reviewed: a.go — 2 callers, tests green"
	r := runImpactAttestationHookWithActiveDiff(t, files, commitMsg, nil, nil, false)
	if r.ExitCode != 0 {
		t.Errorf("expected exit 0 when Impact-Reviewed: trailer present, got %d\noutput:\n%s", r.ExitCode, r.Output)
	}
	if !strings.Contains(r.Output, "trailer found") {
		t.Errorf("expected 'trailer found' confirmation in output\ngot: %s", r.Output)
	}
}

func TestImpactAttestation_LowercaseImpactFileTrailerSatisfies(t *testing.T) {
	files := []string{"a.go", "b.go", "c.go", "d.go", "e.go"}
	commitMsg := "feat(TASK-TEST): change five files\n\nimpact a.go: 2 callers, tests green"
	r := runImpactAttestationHookWithActiveDiff(t, files, commitMsg, nil, nil, false)
	if r.ExitCode != 0 {
		t.Errorf("expected exit 0 when impact <file>: trailer present, got %d\noutput:\n%s", r.ExitCode, r.Output)
	}
}

func TestImpactAttestation_CompactImpactLineSatisfies(t *testing.T) {
	files := []string{"a.go", "b.go", "c.go", "d.go", "e.go"}
	commitMsg := "feat(TASK-TEST): change five files\n\nimpact: a.go — 2 deps 3 sym 1D 0T 0R 0B"
	r := runImpactAttestationHookWithActiveDiff(t, files, commitMsg, nil, nil, false)
	if r.ExitCode != 0 {
		t.Errorf("expected exit 0 when gg impact --compact header is pasted, got %d\noutput:\n%s", r.ExitCode, r.Output)
	}
}

// TestImpactAttestation_ModeOff: GG_IMPACT_ATTESTATION=off → hook exits 0
// unconditionally regardless of file count.
func TestImpactAttestation_ModeOff(t *testing.T) {
	files := []string{"a.go", "b.go", "c.go", "d.go"}
	r := runImpactAttestationHook(t, files, "feat: four files no trailer",
		nil, map[string]string{"GG_IMPACT_ATTESTATION": "off"})
	if r.ExitCode != 0 {
		t.Errorf("expected exit 0 when GG_IMPACT_ATTESTATION=off, got %d\noutput:\n%s", r.ExitCode, r.Output)
	}
}

// TestImpactAttestation_BypassAccepted: GG_BYPASS_RATIONALE set → gate bypassed
// even at mandatory threshold.
func TestImpactAttestation_BypassAccepted(t *testing.T) {
	files := []string{"a.go", "b.go", "c.go", "d.go"}
	r := runImpactAttestationHook(t, files, "feat: four files no trailer",
		nil, map[string]string{"GG_BYPASS_RATIONALE": "TASK-TEST: emergency hotfix"}) //nolint:gosec // Test fixture env var, not a credential.
	if r.ExitCode != 0 {
		t.Errorf("expected exit 0 when GG_BYPASS_RATIONALE set, got %d\noutput:\n%s", r.ExitCode, r.Output)
	}
	if !strings.Contains(r.Output, "bypass accepted") {
		t.Errorf("expected 'bypass accepted' in output\ngot: %s", r.Output)
	}
}

// TestImpactAttestation_TestFilesExcluded: only _test.go files changed →
// source count stays 0 → advisory (exit 0), not mandatory.
func TestImpactAttestation_TestFilesExcluded(t *testing.T) {
	files := []string{"a_test.go", "b_test.go", "c_test.go", "d_test.go"}
	r := runImpactAttestationHook(t, files, "test: add tests without trailer", nil, nil)
	if r.ExitCode != 0 {
		t.Errorf("expected exit 0 when only test files changed, got %d\noutput:\n%s", r.ExitCode, r.Output)
	}
}
