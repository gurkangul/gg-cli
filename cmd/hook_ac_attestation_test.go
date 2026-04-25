// Package cmd — integration tests for .gg/hooks/pre-task-done.d/50-ac-attestation.sh
//
// The tests exercise the shell script directly via exec.Command("sh", ...) with a
// fake `gg` stub in PATH that returns controlled JSON. They do NOT invoke the gg
// binary or the Qdrant store — the script's only gg dependency is
// `gg task get $GG_TASK_ID --json`.
package cmd

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// hookACAttestationPath returns the absolute path to the hook script under the
// repo root, regardless of where the test binary runs.
func hookACAttestationPath(t *testing.T) string {
	t.Helper()
	// Walk upward from the test binary's source file to the repo root.
	// runtime.Caller gives us the source file path at compile time.
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// file is cmd/hook_ac_attestation_test.go; walk to repo root
	repoRoot := filepath.Dir(filepath.Dir(file))
	hookPath := filepath.Join(repoRoot, ".gg", "hooks", "pre-task-done.d", "50-ac-attestation.sh")
	if _, err := os.Stat(hookPath); err != nil {
		t.Skipf("hook script not found at %s — skipping: %v", hookPath, err)
	}
	return hookPath
}

// acAttestationTestEnv returns the common env overrides for running the hook in isolation.
// fakeGGScript is the content of the fake `gg` binary (a shell script).
// taskID is the task ID the hook will look up.
// commitMsg is written as the git HEAD commit message in the temp git repo.
func runACAttestationHook(t *testing.T, taskJSON, commitMsg string, extraEnv map[string]string) (output string, exitCode int) {
	t.Helper()

	hookPath := hookACAttestationPath(t)

	// ── 1. Temp dir with a minimal git repo ───────────────────────────────────
	dir := t.TempDir()

	// Initialise a bare git repo with one commit so git log -1 works.
	runIn := func(name string, args ...string) {
		t.Helper()
		c := exec.Command(name, args...)
		c.Dir = dir
		out, err := c.CombinedOutput()
		if err != nil {
			t.Logf("git setup %v: %s", args, out)
		}
	}
	runIn("git", "init", "-b", "main")
	runIn("git", "config", "user.email", "test@example.com")
	runIn("git", "config", "user.name", "Test")
	// Create an initial commit so git log has something to return.
	f := filepath.Join(dir, "README.md")
	_ = os.WriteFile(f, []byte("init"), 0o644)
	runIn("git", "add", ".")
	// Use commitMsg as the commit message.
	runIn("git", "commit", "-m", commitMsg)

	// ── 2. Fake `gg` stub ─────────────────────────────────────────────────────
	// Write the JSON response to a file; the stub cats it to avoid shell quoting issues.
	binDir := filepath.Join(dir, "fakebin")
	_ = os.MkdirAll(binDir, 0o755)
	jsonFile := filepath.Join(dir, "task.json")
	_ = os.WriteFile(jsonFile, []byte(taskJSON), 0o644)
	fakeGG := filepath.Join(binDir, "gg")
	fakeGGBody := "#!/bin/sh\ncat " + jsonFile + "\n"
	_ = os.WriteFile(fakeGG, []byte(fakeGGBody), 0o755)

	// ── 3. Build env ──────────────────────────────────────────────────────────
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

	// ── 4. Run the hook ───────────────────────────────────────────────────────
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

// taskJSONWith returns valid JSON with the given Detail string, properly marshaled.
func taskJSONWith(detail string) string {
	m := map[string]string{
		"ID":     "TASK-TEST",
		"Title":  "Test task",
		"Detail": detail,
		"Status": "in_progress",
	}
	b, err := json.Marshal(m)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// ── Tests ─────────────────────────────────────────────────────────────────────

// TestACAttestation_NoACSection: task with no ACCEPTANCE heading → hook passes
// (nothing to attest).
func TestACAttestation_NoACSection(t *testing.T) {
	json := taskJSONWith("This task has no acceptance criteria section.\nJust some description.")
	out, code := runACAttestationHook(t, json, "fix: some change", nil)
	if code != 0 {
		t.Errorf("expected exit 0 (no ACCEPTANCE section), got %d\noutput:\n%s", code, out)
	}
}

// TestACAttestation_ThreeACs_TwoRefs_Blocks: 3 ACs in spec, only 2 referenced in
// commit → hook must block (exit 7).
// AC-1 and AC-2 are covered; AC-3 is not.
func TestACAttestation_ThreeACs_TwoRefs_Blocks(t *testing.T) {
	detail := `Implementation description.

ACCEPTANCE
- Pre-task-done hook blocks when spec ACs aren't referenced in commit
- Bypass works and is audited like other gates
- Integration test covers both block and pass cases`

	commitMsg := `feat: implement AC attestation hook

AC-1: hook exits 7 when ACs missing from commit message
AC-2: bypass via GG_ALLOW_INCOMPLETE_AC audited via gg record`

	json := taskJSONWith(detail)
	out, code := runACAttestationHook(t, json, commitMsg, nil)

	if code != 7 {
		t.Errorf("expected exit 7 (AC-3 not referenced), got %d\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "AC-3") {
		t.Errorf("output should mention unmatched AC-3, got:\n%s", out)
	}
}

// TestACAttestation_ThreeACs_ThreeRefs_Passes: 3 ACs in spec, all 3 referenced
// → hook must exit 0.
func TestACAttestation_ThreeACs_ThreeRefs_Passes(t *testing.T) {
	detail := `Implementation description.

ACCEPTANCE
- Pre-task-done hook blocks when spec ACs aren't referenced in commit
- Bypass works and is audited like other gates
- Integration test covers both block and pass cases`

	commitMsg := `feat: implement AC attestation hook

AC-1: hook exits 7 when ACs missing from commit message
AC-2: bypass via GG_ALLOW_INCOMPLETE_AC audited via gg record
AC-3: integration test added in cmd/hook_ac_attestation_test.go`

	json := taskJSONWith(detail)
	out, code := runACAttestationHook(t, json, commitMsg, nil)

	if code != 0 {
		t.Errorf("expected exit 0 (all 3 ACs referenced), got %d\noutput:\n%s", code, out)
	}
}

// TestACAttestation_ModeOff_SkipsAlways: GG_AC_ATTESTATION=off → always exit 0.
func TestACAttestation_ModeOff_SkipsAlways(t *testing.T) {
	detail := `ACCEPTANCE
- This AC is definitely not covered`
	json := taskJSONWith(detail)
	out, code := runACAttestationHook(t, json, "fix: unrelated commit", map[string]string{
		"GG_AC_ATTESTATION": "off",
	})
	if code != 0 {
		t.Errorf("expected exit 0 with GG_AC_ATTESTATION=off, got %d\noutput:\n%s", code, out)
	}
}

// TestACAttestation_ModeWarn_NonBlocking: GG_AC_ATTESTATION=warn → exits 0 even
// when ACs are unmatched.
func TestACAttestation_ModeWarn_NonBlocking(t *testing.T) {
	detail := `ACCEPTANCE
- AC that is not in commit
- Another AC that is not in commit`
	json := taskJSONWith(detail)
	out, code := runACAttestationHook(t, json, "fix: nothing relevant", map[string]string{
		"GG_AC_ATTESTATION": "warn",
	})
	if code != 0 {
		t.Errorf("expected exit 0 in warn mode, got %d\noutput:\n%s", code, out)
	}
	// Should still print the warning
	if !strings.Contains(out, "AC-1") {
		t.Errorf("warn mode should still report unmatched ACs, got:\n%s", out)
	}
}

// TestACAttestation_Bypass_GG_ALLOW_INCOMPLETE_AC: bypass env exits 0 and audits.
func TestACAttestation_Bypass_GG_ALLOW_INCOMPLETE_AC(t *testing.T) {
	detail := `ACCEPTANCE
- This AC is not referenced in the commit message`
	json := taskJSONWith(detail)
	out, code := runACAttestationHook(t, json, "fix: nothing", map[string]string{
		"GG_ALLOW_INCOMPLETE_AC": "master approved partial ship",
	})
	if code != 0 {
		t.Errorf("expected exit 0 with bypass, got %d\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "bypass accepted") {
		t.Errorf("bypass should print acceptance notice, got:\n%s", out)
	}
}

// TestACAttestation_NoFalsePositive_ACAsWord: "AC" appearing as an ordinary
// substring in a commit message does NOT count as attestation for AC-1.
// e.g. "refactor: compact the storage" contains "ac" but is not AC-1.
func TestACAttestation_NoFalsePositive_ACAsWord(t *testing.T) {
	detail := `ACCEPTANCE
- hook blocks when ACs not present`
	json := taskJSONWith(detail)
	// The word "abstract", "cache", "exact" contain "ac" but should not count.
	commitMsg := `fix: abstract cache lookup for exact match

This change refactors the abstract layer to cache results. The exact
mechanism is an action-based lookup.`

	out, code := runACAttestationHook(t, json, commitMsg, nil)
	if code != 7 {
		t.Errorf("expected exit 7 (no AC-1: attestation), got %d\noutput:\n%s", code, out)
	}
}

// TestACAttestation_NumberedLine_AlternativeStyle: "1:" at start of commit
// body line counts as AC-1 attestation.
func TestACAttestation_NumberedLine_AlternativeStyle(t *testing.T) {
	detail := `ACCEPTANCE
- hook blocks when spec ACs are missing
- bypass is audited`
	json := taskJSONWith(detail)
	commitMsg := `feat: hook implementation

1: implemented the blocking logic in 50-ac-attestation.sh
2: bypass recorded via gg record when GG_ALLOW_INCOMPLETE_AC set`

	out, code := runACAttestationHook(t, json, commitMsg, nil)
	if code != 0 {
		t.Errorf("expected exit 0 (numbered-line style attestation), got %d\noutput:\n%s", code, out)
	}
}

// TestACAttestation_GapFormat_Blocks: Detail uses "Gap A:" / "Gap B:" style
// (like TASK-297). Both must be attested; only Gap A is covered → block.
func TestACAttestation_GapFormat_Blocks(t *testing.T) {
	detail := `WHY
Gap A: word-boundary regex was too broad
Gap B: no cross-process file flock — only in-process sync.Map

ACCEPTANCE
- gaps resolved`
	j := taskJSONWith(detail)
	commitMsg := `fix: resolve Gap A via word-boundary regex

AC-1: Gap A word-boundary fix applied in cmux.go`

	out, code := runACAttestationHook(t, j, commitMsg, nil)
	if code != 7 {
		t.Errorf("expected exit 7 (Gap B not referenced), got %d\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "Gap B") {
		t.Errorf("output should mention unmatched Gap B, got:\n%s", out)
	}
}

// TestACAttestation_GapFormat_Passes: Gap A and Gap B both attested.
func TestACAttestation_GapFormat_Passes(t *testing.T) {
	detail := `Gap A: word-boundary regex was too broad
Gap B: no cross-process file flock

ACCEPTANCE
- both gaps resolved`
	j := taskJSONWith(detail)
	commitMsg := `fix: resolve both gaps

AC-1: Gap A fixed via word-boundary regex in cmux.go
AC-2: Gap B fixed via syscall.Flock in terminal/flock.go
AC-3: both gaps resolved`

	out, code := runACAttestationHook(t, j, commitMsg, nil)
	if code != 0 {
		t.Errorf("expected exit 0 (all items attested), got %d\noutput:\n%s", code, out)
	}
}

// TestACAttestation_BulletsOutsideAcceptance: "- " bullets in a WHY section
// (not under ACCEPTANCE heading) are still extracted and must be attested.
func TestACAttestation_BulletsOutsideAcceptance(t *testing.T) {
	detail := `WHY
- silent AC narrowing happened 3 times
- each cost 1-2 rework cycles

WHAT
New hook blocks on unattested ACs.`
	j := taskJSONWith(detail)
	// Only attest AC-1; AC-2 unmatched → should block.
	commitMsg := `feat: add attestation hook

AC-1: silent narrowing prevented by gate`

	out, code := runACAttestationHook(t, j, commitMsg, nil)
	if code != 7 {
		t.Errorf("expected exit 7 (AC-2 from WHY bullets not attested), got %d\noutput:\n%s", code, out)
	}
}

func TestACAttestation_ImplementationHintsBullets_NotCounted(t *testing.T) {
	detail := `Acceptance Criteria:
AC-1: default task get renders full Detail
AC-2: --short renders one line
AC-3: --json remains unchanged
AC-4: docs include the flag
AC-5: full race suite passes

Implementation hints:
- reuse the compact renderer
- keep default output verbose
- update generated CLI docs
- avoid changing JSON shape`
	j := taskJSONWith(detail)
	commitMsg := `feat: implement task get short flag

AC-1: default task get renders full Detail
AC-2: --short renders one line
AC-3: --json remains unchanged
AC-4: docs include the flag
AC-5: full race suite passes`

	out, code := runACAttestationHook(t, j, commitMsg, nil)
	if code != 0 {
		t.Errorf("expected exit 0 (Implementation hints bullets ignored), got %d\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "found 5 acceptance criterion/criteria") {
		t.Errorf("expected exactly 5 AC anchors, got:\n%s", out)
	}
	if strings.Contains(out, "AC-6") || strings.Contains(out, "reuse the compact renderer") {
		t.Errorf("implementation hint bullets should not be reported as ACs, got:\n%s", out)
	}
}

func TestACAttestation_FixReworkBullets_StillSkipped(t *testing.T) {
	detail := `Acceptance Criteria:
AC-1: user-visible behavior is fixed

FIX:
- edit parser regex
- add regression tests

REWORK:
- rerun hook`
	j := taskJSONWith(detail)
	commitMsg := `fix: repair parser

AC-1: user-visible behavior is fixed`

	out, code := runACAttestationHook(t, j, commitMsg, nil)
	if code != 0 {
		t.Errorf("expected exit 0 (FIX/REWORK bullets skipped), got %d\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "found 1 acceptance criterion/criteria") {
		t.Errorf("expected exactly 1 AC anchor, got:\n%s", out)
	}
}

// TestACAttestation_NumberedItemsOutsideACSection: numbered items in WHAT
// block (not under ACCEPTANCE) are also extracted.
func TestACAttestation_NumberedItemsOutsideACSection(t *testing.T) {
	detail := `WHAT
1. Parse task Detail for AC anchors
2. Check commit message for each
3. Exit 7 when any unmatched`
	j := taskJSONWith(detail)
	commitMsg := `feat: implement gate

AC-1: parser extracts all AC anchors
AC-2: commit check implemented
AC-3: exit 7 on unmatched`

	out, code := runACAttestationHook(t, j, commitMsg, nil)
	if code != 0 {
		t.Errorf("expected exit 0 (all 3 numbered items attested), got %d\noutput:\n%s", code, out)
	}
}

// TestACAttestation_DiffTestName_Passes: AC-1 has no commit-body reference but
// the diff contains a test function "TestAC1_BlocksOnMissingRef" — rule (d).
func TestACAttestation_DiffTestName_Passes(t *testing.T) {
	hookPath := hookACAttestationPath(t)

	dir := t.TempDir()

	runIn := func(name string, args ...string) {
		t.Helper()
		c := exec.Command(name, args...)
		c.Dir = dir
		out, err := c.CombinedOutput()
		if err != nil {
			t.Logf("git setup %v: %s", args, out)
		}
	}
	runIn("git", "init", "-b", "main")
	runIn("git", "config", "user.email", "test@example.com")
	runIn("git", "config", "user.name", "Test")

	// Write a file that contains a test function name matching AC-1.
	testFile := filepath.Join(dir, "hook_test.go")
	_ = os.WriteFile(testFile, []byte(`package main

func TestAC1_BlocksOnMissingRef(t *testing.T) {
	// verifies AC-1: hook blocks when commit omits AC reference
}
`), 0o644)
	runIn("git", "add", ".")
	// Commit message intentionally does NOT reference AC-1 in body.
	runIn("git", "commit", "-m", "feat: add attestation hook test")

	taskJSON := taskJSONWith(`ACCEPTANCE
- hook blocks when commit omits AC reference`)

	binDir := filepath.Join(dir, "fakebin")
	_ = os.MkdirAll(binDir, 0o755)
	jsonFile := filepath.Join(dir, "task.json")
	_ = os.WriteFile(jsonFile, []byte(taskJSON), 0o644)
	fakeGG := filepath.Join(binDir, "gg")
	_ = os.WriteFile(fakeGG, []byte("#!/bin/sh\ncat "+jsonFile+"\n"), 0o755)

	env := os.Environ()
	env = append(env,
		"GG_TASK_ID=TASK-TEST",
		"GG_TASK_SUMMARY=test summary",
		"GG_PROJECT_ID=test-project",
		"GG_ACTOR=developer",
		"PATH="+binDir+":"+os.Getenv("PATH"),
		"HOME="+dir,
	)
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
	if exitCode != 0 {
		t.Errorf("expected exit 0 (TestAC1_ in diff satisfies rule d), got %d\noutput:\n%s", exitCode, string(out))
	}
	if !strings.Contains(string(out), "test name") {
		t.Errorf("expected 'test name' in output to confirm rule (d) matched, got:\n%s", string(out))
	}
}

// TestACAttestation_DiffComment_Passes: AC-1 has no commit-body reference but
// the diff contains "// AC-1 covered by implementation" — rule (e).
func TestACAttestation_DiffComment_Passes(t *testing.T) {
	hookPath := hookACAttestationPath(t)

	dir := t.TempDir()

	runIn := func(name string, args ...string) {
		t.Helper()
		c := exec.Command(name, args...)
		c.Dir = dir
		out, err := c.CombinedOutput()
		if err != nil {
			t.Logf("git setup %v: %s", args, out)
		}
	}
	runIn("git", "init", "-b", "main")
	runIn("git", "config", "user.email", "test@example.com")
	runIn("git", "config", "user.name", "Test")

	// Write a file containing a // AC-1 comment.
	srcFile := filepath.Join(dir, "attestation.go")
	_ = os.WriteFile(srcFile, []byte(`package main

// AC-1 covered by this implementation
func blockOnMissingRef() error {
	return nil
}
`), 0o644)
	runIn("git", "add", ".")
	// Commit message intentionally does NOT reference AC-1 in body.
	runIn("git", "commit", "-m", "feat: implement blocking logic")

	taskJSON := taskJSONWith(`ACCEPTANCE
- hook blocks when commit omits AC reference`)

	binDir := filepath.Join(dir, "fakebin")
	_ = os.MkdirAll(binDir, 0o755)
	jsonFile := filepath.Join(dir, "task.json")
	_ = os.WriteFile(jsonFile, []byte(taskJSON), 0o644)
	fakeGG := filepath.Join(binDir, "gg")
	_ = os.WriteFile(fakeGG, []byte("#!/bin/sh\ncat "+jsonFile+"\n"), 0o755)

	env := os.Environ()
	env = append(env,
		"GG_TASK_ID=TASK-TEST",
		"GG_TASK_SUMMARY=test summary",
		"GG_PROJECT_ID=test-project",
		"GG_ACTOR=developer",
		"PATH="+binDir+":"+os.Getenv("PATH"),
		"HOME="+dir,
	)
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
	if exitCode != 0 {
		t.Errorf("expected exit 0 (// AC-1 comment in diff satisfies rule e), got %d\noutput:\n%s", exitCode, string(out))
	}
	if !strings.Contains(string(out), "func/comment") {
		t.Errorf("expected 'func/comment' in output to confirm rule (e) matched, got:\n%s", string(out))
	}
}

// runACAttestationHookWithFiles creates a temp git repo, writes files, commits, and
// runs the hook. files is a map of filename → content; commitMsg is used verbatim.
// Returns combined output and exit code.
func runACAttestationHookWithFiles(t *testing.T, taskJSON, commitMsg string, files map[string]string) (string, int) {
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
	for name, content := range files {
		_ = os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644)
	}
	runIn("git", "add", ".")
	runIn("git", "commit", "-m", commitMsg)

	binDir := filepath.Join(dir, "fakebin")
	_ = os.MkdirAll(binDir, 0o755)
	jsonFile := filepath.Join(dir, "task.json")
	_ = os.WriteFile(jsonFile, []byte(taskJSON), 0o644)
	_ = os.WriteFile(filepath.Join(binDir, "gg"), []byte("#!/bin/sh\ncat "+jsonFile+"\n"), 0o755)

	env := append(os.Environ(),
		"GG_TASK_ID=TASK-TEST", "GG_TASK_SUMMARY=test summary",
		"GG_PROJECT_ID=test-project", "GG_ACTOR=developer",
		"PATH="+binDir+":"+os.Getenv("PATH"), "HOME="+dir,
	)
	cmd := exec.Command("/bin/sh", hookPath)
	cmd.Env = env
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else {
			code = -1
		}
	}
	return string(out), code
}

// TestACAttestation_DiffTestGap_Passes: Gap A item; diff has TestGapA_ — rule (d-gap).
// Detail has only the Gap A line (no duplicate bullet) so exactly one AC is extracted.
func TestACAttestation_DiffTestGap_Passes(t *testing.T) {
	detail := "Gap A: no cross-process flock — only in-process sync.Map"
	files := map[string]string{
		"flock_test.go": "package main\n\nfunc TestGapA_FixesCrossProcess(t *testing.T) {}\n",
	}
	out, code := runACAttestationHookWithFiles(t, taskJSONWith(detail), "fix: implement cross-process flock", files)
	if code != 0 {
		t.Errorf("expected exit 0 (TestGapA_ in diff → rule d-gap), got %d\noutput:\n%s", code, out)
	}
}

// TestACAttestation_DiffGapComment_Passes: Gap B item; diff has "// Gap B" comment — rule (e-gap).
// Gap A is attested via commit body so only Gap B depends on the comment.
func TestACAttestation_DiffGapComment_Passes(t *testing.T) {
	detail := "Gap A: word-boundary regex too broad\nGap B: no cross-process flock"
	files := map[string]string{
		"flock.go": "package main\n\n// Gap B fixed by syscall.Flock\nfunc lockSurface() error { return nil }\n",
	}
	commitMsg := "fix: add cross-process flock\n\nAC-1: Gap A word-boundary fix applied"
	out, code := runACAttestationHookWithFiles(t, taskJSONWith(detail), commitMsg, files)
	if code != 0 {
		t.Errorf("expected exit 0 (// Gap B comment → rule e-gap), got %d\noutput:\n%s", code, out)
	}
}

// TestACAttestation_FilePathTestAC_Passes: file path TestAC1_gate_test.go satisfies
// rule (d) file-path branch even when diff lines use a generic function name.
func TestACAttestation_FilePathTestAC_Passes(t *testing.T) {
	detail := "ACCEPTANCE\n- gate blocks when commit omits AC reference"
	files := map[string]string{
		"TestAC1_gate_test.go": "package main\n\nfunc TestGateBehaviour(t *testing.T) {}\n",
	}
	out, code := runACAttestationHookWithFiles(t, taskJSONWith(detail), "test: add gate test file", files)
	if code != 0 {
		t.Errorf("expected exit 0 (TestAC1 in file path → rule d file-path), got %d\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "test file path") {
		t.Errorf("expected 'test file path' in output, got:\n%s", out)
	}
}

// TestACAttestation_TestNameProof: commit adds a TestAC2_* function; no "AC-2:" in
// commit body. Rule (d) should fire on the diff and satisfy AC-2.
func TestACAttestation_TestNameProof(t *testing.T) {
	detail := `ACCEPTANCE
- hook blocks when commit omits AC reference
- test name in diff counts as proof`
	files := map[string]string{
		"gate_test.go": `package main

func TestAC2_DiffTestNameProof(t *testing.T) {
	// verifies AC-2: test name in diff satisfies the attestation gate
}
`,
	}
	// Commit body only references AC-1; AC-2 proof comes from diff test name.
	commitMsg := "feat: add attestation proof via test name\n\nAC-1: hook blocks when commit omits AC reference"
	out, code := runACAttestationHookWithFiles(t, taskJSONWith(detail), commitMsg, files)
	if code != 0 {
		t.Errorf("expected exit 0 (TestAC2_ in diff satisfies rule d for AC-2), got %d\noutput:\n%s", code, out)
	}
}

// TestACAttestation_CommentProof: commit adds a "// AC-1 …" comment near edited code;
// no "AC-1:" in commit body. Rule (e) should fire on the diff and satisfy AC-1.
func TestACAttestation_CommentProof(t *testing.T) {
	detail := `ACCEPTANCE
- hook blocks when commit omits AC reference`
	files := map[string]string{
		"impl.go": `package main

// AC-1 covered by this blocking implementation
func blockOnMissingRef() error {
	return nil
}
`,
	}
	// Commit body has no AC reference; proof is the comment in the diff.
	commitMsg := "feat: implement blocking logic"
	out, code := runACAttestationHookWithFiles(t, taskJSONWith(detail), commitMsg, files)
	if code != 0 {
		t.Errorf("expected exit 0 (// AC-1 comment in diff satisfies rule e), got %d\noutput:\n%s", code, out)
	}
}

// TestACAttestation_Bypass_EmitsGGRecord: when GG_ALLOW_INCOMPLETE_AC is set,
// the hook must call `gg record` with tags "bypass,ac-attestation,<task-id>"
// and the reason string. A recording fake-gg stub captures the invocation.
func TestACAttestation_Bypass_EmitsGGRecord(t *testing.T) {
	hookPath := hookACAttestationPath(t)

	dir := t.TempDir()

	// Minimal git repo with a commit that does NOT reference the AC.
	runIn := func(name string, args ...string) {
		t.Helper()
		c := exec.Command(name, args...)
		c.Dir = dir
		out, err := c.CombinedOutput()
		if err != nil {
			t.Logf("git setup %v: %s", args, out)
		}
	}
	runIn("git", "init", "-b", "main")
	runIn("git", "config", "user.email", "test@example.com")
	runIn("git", "config", "user.name", "Test")
	f := filepath.Join(dir, "README.md")
	_ = os.WriteFile(f, []byte("init"), 0o644)
	runIn("git", "add", ".")
	runIn("git", "commit", "-m", "fix: no AC reference here")

	// Task JSON with one unattested AC.
	taskJSON := taskJSONWith(`ACCEPTANCE
- hook calls gg record when bypass env is set`)

	binDir := filepath.Join(dir, "fakebin")
	_ = os.MkdirAll(binDir, 0o755)

	jsonFile := filepath.Join(dir, "task.json")
	_ = os.WriteFile(jsonFile, []byte(taskJSON), 0o644)

	// recordLog captures every `gg record` invocation as a single line of
	// space-joined arguments.
	recordLog := filepath.Join(dir, "gg_record.log")

	// The stub: if first arg is "record", append all args to recordLog; otherwise
	// (task get) cat the JSON file.
	fakeGGBody := `#!/bin/sh
if [ "$1" = "record" ]; then
  echo "$@" >> ` + recordLog + `
  exit 0
fi
cat ` + jsonFile + `
`
	fakeGG := filepath.Join(binDir, "gg")
	_ = os.WriteFile(fakeGG, []byte(fakeGGBody), 0o755)

	bypassReason := "master approved partial ship for TASK-TEST"

	env := os.Environ()
	env = append(env,
		"GG_TASK_ID=TASK-TEST",
		"GG_TASK_SUMMARY=test summary",
		"GG_PROJECT_ID=test-project",
		"GG_ACTOR=developer",
		"GG_ALLOW_INCOMPLETE_AC="+bypassReason,
		"PATH="+binDir+":"+os.Getenv("PATH"),
		"HOME="+dir,
	)

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

	if exitCode != 0 {
		t.Fatalf("expected exit 0 with bypass, got %d\noutput:\n%s", exitCode, out)
	}

	// Verify gg record was called.
	logBytes, readErr := os.ReadFile(recordLog)
	if readErr != nil {
		t.Fatalf("gg record was never called (log file missing): %v\nhook output:\n%s", readErr, out)
	}
	logContent := string(logBytes)

	// Must contain the expected tags.
	wantTags := "bypass,ac-attestation,TASK-TEST"
	if !strings.Contains(logContent, wantTags) {
		t.Errorf("gg record call missing tags %q\nrecord log:\n%s", wantTags, logContent)
	}

	// Must contain the bypass reason in the --reason argument.
	if !strings.Contains(logContent, bypassReason) {
		t.Errorf("gg record call missing reason %q\nrecord log:\n%s", bypassReason, logContent)
	}
}

// TestACAttestation_NarrativeGapNoColon_NotCounted: a WHY section that contains
// 'Gap 2 GSD did X' (no colon) is prose, not an AC definition.
// The hook must NOT extract it as an AC anchor, so the task passes with no ACs.
func TestACAttestation_NarrativeGapNoColon_NotCounted(t *testing.T) {
	detail := `WHY
This rework was triggered because TASK-292 Gap 2 GSD did X incorrectly.
Gap 2 was the cross-process flock issue described in the prior session.
We also saw Gap 3 appear in the context replay.

WHAT
Fix the flock implementation.`

	// No ACs at all — if Gap 2 / Gap 3 were extracted the hook would block.
	json := taskJSONWith(detail)
	out, code := runACAttestationHook(t, json, "fix: implement cross-process flock", nil)
	if code != 0 {
		t.Errorf("expected exit 0 (narrative Gap N without colon = no AC anchors), got %d\noutput:\n%s", code, out)
	}
	if strings.Contains(out, "Gap 2") || strings.Contains(out, "Gap 3") {
		t.Errorf("narrative Gap references should not appear as AC anchors, got:\n%s", out)
	}
}

// TestACAttestation_GapUnderGapsHeader_Counted: 'Gap A' listed under a GAPS
// header (without colon) should be extracted as an AC because it is inside a
// recognised AC section.
func TestACAttestation_GapUnderGapsHeader_Counted(t *testing.T) {
	detail := `WHY
Rework cycle identified two gaps.

GAPS
Gap A no cross-process flock — only in-process sync.Map
Gap B word-boundary regex too broad`

	// Only attest Gap A; Gap B is left unattested → hook must block.
	json := taskJSONWith(detail)
	commitMsg := `fix: address gaps

AC-1: Gap A fixed via syscall.Flock`

	out, code := runACAttestationHook(t, json, commitMsg, nil)
	if code != 7 {
		t.Errorf("expected exit 7 (Gap B under GAPS header is an AC, not attested), got %d\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "Gap B") {
		t.Errorf("output should mention unmatched Gap B, got:\n%s", out)
	}
}

