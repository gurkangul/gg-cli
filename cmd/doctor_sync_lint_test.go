// Package cmd — tests for `gg doctor --capture-lint-baseline` and the
// 60-lint-gate.sh baseline JSON format.
package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCaptureLintBaseline_NoGolangciLint verifies that the command exits cleanly
// with an informative error when golangci-lint is not on PATH, rather than
// panicking or producing a malformed baseline file.
func TestCaptureLintBaseline_NoGolangciLint(t *testing.T) {
	setupGGDir(t)
	// Shadow PATH so golangci-lint cannot be found.
	t.Setenv("PATH", t.TempDir())

	err := runDoctorCaptureLintBaseline()
	if err == nil {
		t.Fatal("expected error when golangci-lint not found, got nil")
	}
}

// TestCaptureLintBaseline_WritesJSONWithExpectedShape verifies that when
// golangci-lint IS available and produces zero issues (a mock scenario achieved
// via a fake binary), the baseline file is written with the correct JSON shape:
// version=1, captured_at (non-empty), issue_count (int >= 0).
func TestCaptureLintBaseline_WritesJSONWithExpectedShape(t *testing.T) {
	ggDir := setupGGDir(t)

	// Create a fake golangci-lint binary that exits 0 with no output.
	fakeDir := t.TempDir()
	fakeBin := filepath.Join(fakeDir, "golangci-lint")
	if err := os.WriteFile(fakeBin, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake binary: %v", err)
	}
	// Prepend fake dir so it shadows any real golangci-lint.
	origPath := os.Getenv("PATH")
	t.Setenv("PATH", fakeDir+":"+origPath)

	if err := runDoctorCaptureLintBaseline(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	baselinePath := filepath.Join(ggDir, "lint-baseline.json")
	data, err := os.ReadFile(baselinePath)
	if err != nil {
		t.Fatalf("baseline not written: %v", err)
	}

	var b lintBaseline
	if err := json.Unmarshal(data, &b); err != nil {
		t.Fatalf("baseline JSON invalid: %v\n%s", err, data)
	}
	if b.Version != 1 {
		t.Errorf("expected version=1, got %d", b.Version)
	}
	if b.CapturedAt == "" {
		t.Error("captured_at must not be empty")
	}
	if b.IssueCount != 0 {
		t.Errorf("expected issue_count=0 (fake lint produced no output), got %d", b.IssueCount)
	}
}

// TestCaptureLintBaseline_ParsesSummaryLine verifies that the issue count is
// extracted from golangci-lint's "N issues:" summary line (stable across v1/v2).
func TestCaptureLintBaseline_ParsesSummaryLine(t *testing.T) {
	ggDir := setupGGDir(t)

	// Fake golangci-lint that emits 3 issues via the canonical summary line
	// and exits 1 (golangci-lint exits non-zero when issues are found).
	fakeDir := t.TempDir()
	fakeBin := filepath.Join(fakeDir, "golangci-lint")
	script := "#!/bin/sh\nprintf 'file.go:1:1: msg (linter)\\nfile.go:2:1: msg (linter)\\nfile.go:3:1: msg (linter)\\n3 issues:\\n* linter: 3\\n'\nexit 1\n"
	if err := os.WriteFile(fakeBin, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake binary: %v", err)
	}
	origPath := os.Getenv("PATH")
	t.Setenv("PATH", fakeDir+":"+origPath)

	if err := runDoctorCaptureLintBaseline(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	baselinePath := filepath.Join(ggDir, "lint-baseline.json")
	data, err := os.ReadFile(baselinePath)
	if err != nil {
		t.Fatalf("baseline not written: %v", err)
	}

	var b lintBaseline
	if err := json.Unmarshal(data, &b); err != nil {
		t.Fatalf("baseline JSON invalid: %v", err)
	}
	if b.IssueCount != 3 {
		t.Errorf("expected issue_count=3 (from summary line), got %d", b.IssueCount)
	}
}

// TestLintGateHook_AutoShrink verifies that the lint gate template contains the
// auto-shrink path: when current issue count is LESS than the baseline, the
// template must update the baseline file and exit 0 (not block the task close).
func TestLintGateHook_AutoShrink(t *testing.T) {
	body := string([]byte(getLintGateTemplateBody(t)))
	// Must contain the shrink branch — less-than comparison and a write back.
	for _, want := range []string{
		`"$CURRENT_COUNT" -lt "$BASELINE_COUNT"`,
		`> "$BASELINE_FILE"`,
		"baseline updated",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("lint gate hook missing auto-shrink signal %q", want)
		}
	}
}

// getLintGateTemplateBody returns the content of the installed 60-lint-gate.sh
// by going through the templates package (same source of truth as install).
func getLintGateTemplateBody(t *testing.T) string {
	t.Helper()
	f := newHookInstallFixture(t, false, false)
	if err := runDoctorInstallTaskHooks(); err != nil {
		t.Fatalf("installer: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(f.preDir, "60-lint-gate.sh"))
	if err != nil {
		t.Fatalf("read lint gate: %v", err)
	}
	return string(data)
}

// TestCaptureLintBaseline_Idempotent verifies that re-running capture
// overwrites the previous baseline (the caller wants a fresh snapshot).
func TestCaptureLintBaseline_Idempotent(t *testing.T) {
	ggDir := setupGGDir(t)

	fakeDir := t.TempDir()
	fakeBin := filepath.Join(fakeDir, "golangci-lint")
	// First run: 2 issues (summary line included).
	if err := os.WriteFile(fakeBin, []byte("#!/bin/sh\nprintf 'f.go:1:1: a\\nf.go:2:1: b\\n2 issues:\\n* l: 2\\n'\nexit 1\n"), 0o755); err != nil {
		t.Fatalf("write fake binary: %v", err)
	}
	origPath := os.Getenv("PATH")
	t.Setenv("PATH", fakeDir+":"+origPath)

	if err := runDoctorCaptureLintBaseline(); err != nil {
		t.Fatalf("first run error: %v", err)
	}

	// Update fake to emit 0 issues.
	if err := os.WriteFile(fakeBin, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("update fake binary: %v", err)
	}
	if err := runDoctorCaptureLintBaseline(); err != nil {
		t.Fatalf("second run error: %v", err)
	}

	baselinePath := filepath.Join(ggDir, "lint-baseline.json")
	data, err := os.ReadFile(baselinePath)
	if err != nil {
		t.Fatalf("baseline not written: %v", err)
	}
	var b lintBaseline
	if err := json.Unmarshal(data, &b); err != nil {
		t.Fatalf("baseline JSON invalid: %v", err)
	}
	// Second run should overwrite: 0 issues.
	if b.IssueCount != 0 {
		t.Errorf("expected issue_count=0 after second run, got %d", b.IssueCount)
	}
}
