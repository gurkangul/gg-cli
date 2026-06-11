package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

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
