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
		// BUG-099: neutralise ambient gate-bypass state. os.Environ() inherits the
		// caller's shell, so an exported GG_ALLOW_INCOMPLETE_AC — the very escape
		// hatch this gate documents — silently flipped these tests onto the bypass
		// path and failed the suite, which in turn tripped the repro gate. Using
		// the documented bypass therefore made a task impossible to close.
		// Tests that WANT a bypass pass it through extraEnv, appended below, which
		// wins over these defaults.
		"GG_ALLOW_INCOMPLETE_AC=",
		"GG_AC_ATTESTATION=",
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

// TestACAttestation_ModeOff_RequiresRationale: GG_AC_ATTESTATION=off without a
// rationale is now refused (BUG-079 — no silent gate disable); with a rationale
// it skips (exit 0) and is durably audited.
func TestACAttestation_ModeOff_RequiresRationale(t *testing.T) {
	detail := `ACCEPTANCE
- This AC is definitely not covered`
	json := taskJSONWith(detail)

	// Without a reason -> refused (exit 7).
	out, code := runACAttestationHook(t, json, "fix: unrelated commit", map[string]string{
		"GG_AC_ATTESTATION": "off",
	})
	if code != 7 {
		t.Errorf("expected exit 7 with GG_AC_ATTESTATION=off and no rationale, got %d\noutput:\n%s", code, out)
	}

	// With a reason -> skips (exit 0).
	out, code = runACAttestationHook(t, json, "fix: unrelated commit", map[string]string{
		"GG_AC_ATTESTATION":        "off",
		"GG_AC_ATTESTATION_REASON": "emergency hotfix, follow-up TASK filed",
	})
	if code != 0 {
		t.Errorf("expected exit 0 with GG_AC_ATTESTATION=off + rationale, got %d\noutput:\n%s", code, out)
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

AC-1: Gap A word-boundary fix applied in parser.go`

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

AC-1: Gap A fixed via word-boundary regex in parser.go
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
