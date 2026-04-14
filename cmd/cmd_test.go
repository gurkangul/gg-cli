// Package cmd — integration tests for cobra commands.
//
// These tests exercise:
//   - cobra argument-count validation (no backend required)
//   - flag validation that runs before loadDeps (no backend required)
//   - config-not-found error paths (no backend required; uses t.Chdir)
//
// Tests that require a live Qdrant + Ollama backend are in cmd_live_test.go
// and are guarded by t.Skip when the services are not reachable.
package cmd

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// execCmd runs the root cobra command with the given args and returns
// (stdout, stderr, error). It uses a fresh bytes.Buffer for output capture.
// Note: rootCmd is a package-level var; cobra resets flag values on each
// Execute() call for flags that are re-parsed. Tests must not run in parallel.
func execCmd(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	var outBuf, errBuf bytes.Buffer
	rootCmd.SetOut(&outBuf)
	rootCmd.SetErr(&errBuf)
	rootCmd.SetArgs(args)
	err = rootCmd.ExecuteContext(context.Background())
	// Reset to default so subsequent tests get clean state.
	rootCmd.SetOut(nil)
	rootCmd.SetErr(nil)
	return outBuf.String(), errBuf.String(), err
}

// ── Argument count validation (no backend) ───────────────────────────────────

func TestDecide_NoArgs_Error(t *testing.T) {
	t.Chdir(t.TempDir()) // ensure no .gg found — fail before backend
	_, _, err := execCmd(t, "decide")
	if err == nil {
		t.Fatal("expected error for missing argument")
	}
}

func TestDecide_TooManyArgs_Error(t *testing.T) {
	t.Chdir(t.TempDir())
	_, _, err := execCmd(t, "decide", "first", "second")
	if err == nil {
		t.Fatal("expected error for too many arguments")
	}
}

func TestReject_NoArgs_Error(t *testing.T) {
	t.Chdir(t.TempDir())
	_, _, err := execCmd(t, "reject")
	if err == nil {
		t.Fatal("expected error for missing argument")
	}
}

func TestNote_NoArgs_Error(t *testing.T) {
	t.Chdir(t.TempDir())
	_, _, err := execCmd(t, "note")
	if err == nil {
		t.Fatal("expected error for missing argument to 'gg note'")
	}
}

func TestTell_TooFewArgs_Error(t *testing.T) {
	t.Chdir(t.TempDir())
	_, _, err := execCmd(t, "tell")
	if err == nil {
		t.Fatal("expected error for missing arguments")
	}
}

func TestBugReport_NoArgs_Error(t *testing.T) {
	t.Chdir(t.TempDir())
	_, _, err := execCmd(t, "bug", "report")
	if err == nil {
		t.Fatal("expected error for missing title argument")
	}
}

// ── Flag validation (no backend) ─────────────────────────────────────────────

func TestDecide_InvalidStance_Error(t *testing.T) {
	t.Chdir(t.TempDir())
	_, _, err := execCmd(t, "decide", "--stance=maybe", "some decision")
	if err == nil {
		t.Fatal("expected error for invalid --stance value")
	}
	if !strings.Contains(err.Error(), "--stance") {
		t.Errorf("error should mention --stance, got: %v", err)
	}
}

// ── Config-not-found error paths ─────────────────────────────────────────────
// These run in a temp dir (no .gg directory) so loadDeps fails with a
// config error rather than reaching the backend.

func TestDecide_NoConfig_ConfigError(t *testing.T) {
	t.Chdir(t.TempDir())
	_, _, err := execCmd(t, "decide", "JWT for auth")
	if err == nil {
		t.Fatal("expected error when .gg config is absent")
	}
	// Cobra exits with the RunE error; we just check it's non-nil.
	// The exact message from config.FindRoot is ".gg not found — run 'gg init' first".
}

func TestSearch_NoConfig_ConfigError(t *testing.T) {
	t.Chdir(t.TempDir())
	_, _, err := execCmd(t, "search", "authentication")
	if err == nil {
		t.Fatal("expected error when .gg config is absent")
	}
}

func TestTaskCreate_NoConfig_ConfigError(t *testing.T) {
	t.Chdir(t.TempDir())
	_, _, err := execCmd(t, "task", "create", "do something")
	if err == nil {
		t.Fatal("expected error when .gg config is absent")
	}
}

func TestBugReport_NoConfig_ConfigError(t *testing.T) {
	t.Chdir(t.TempDir())
	_, _, err := execCmd(t, "bug", "report", "crash on startup")
	if err == nil {
		t.Fatal("expected error when .gg config is absent")
	}
}

func TestDecideReject_NoConfig_ConfigError(t *testing.T) {
	t.Chdir(t.TempDir())
	_, _, err := execCmd(t, "decide", "--stance=reject", "REST API", "--reason", "out of scope")
	if err == nil {
		t.Fatal("expected error when .gg config is absent")
	}
}

// ── gg impact ─────────────────────────────────────────────────────────────────

func TestImpact_NoArgs_Error(t *testing.T) {
	t.Chdir(t.TempDir())
	_, _, err := execCmd(t, "impact")
	if err == nil {
		t.Fatal("expected error for missing file argument")
	}
}

func TestImpact_TooManyArgs_Error(t *testing.T) {
	t.Chdir(t.TempDir())
	_, _, err := execCmd(t, "impact", "file1.go", "file2.go")
	if err == nil {
		t.Fatal("expected error for too many arguments")
	}
}

func TestImpact_NoConfig_ConfigError(t *testing.T) {
	t.Chdir(t.TempDir())
	_, _, err := execCmd(t, "impact", "internal/store/client.go")
	if err == nil {
		t.Fatal("expected error when .gg config is absent")
	}
}

// ── gg check ──────────────────────────────────────────────────────────────────

func TestCheck_NoConfig_ConfigError(t *testing.T) {
	t.Chdir(t.TempDir())
	_, _, err := execCmd(t, "check")
	if err == nil {
		t.Fatal("expected error when .gg config is absent")
	}
}

// ── gg record ─────────────────────────────────────────────────────────────────

func TestRecord_NoArgs_Error(t *testing.T) {
	t.Chdir(t.TempDir())
	_, _, err := execCmd(t, "record")
	if err == nil {
		t.Fatal("expected error for missing argument")
	}
}

func TestRecord_InvalidStance_Error(t *testing.T) {
	t.Chdir(t.TempDir())
	_, _, err := execCmd(t, "record", "--stance=maybe", "some text")
	if err == nil {
		t.Fatal("expected error for invalid --stance")
	}
	if !strings.Contains(err.Error(), "--stance") {
		t.Errorf("error should mention --stance, got: %v", err)
	}
}

func TestRecord_NoConfig_ConfigError(t *testing.T) {
	t.Chdir(t.TempDir())
	_, _, err := execCmd(t, "record", "use JWT")
	if err == nil {
		t.Fatal("expected error when .gg config is absent")
	}
}

func TestRecordHelp_ShowsStanceFlag(t *testing.T) {
	out, _, err := execCmd(t, "record", "--help")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "--stance") {
		t.Errorf("record --help should mention --stance, got:\n%s", out)
	}
}

// ── Help output (always succeeds) ─────────────────────────────────────────────

func TestHelp_Succeeds(t *testing.T) {
	_, _, err := execCmd(t, "--help")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDecideHelp_ShowsStanceFlag(t *testing.T) {
	out, _, err := execCmd(t, "decide", "--help")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "--stance") {
		t.Errorf("decide --help should mention --stance flag, got:\n%s", out)
	}
}

func TestRejectHelp_ShowsDeprecationNotice(t *testing.T) {
	out, _, err := execCmd(t, "reject", "--help")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(strings.ToLower(out), "deprecated") {
		t.Errorf("reject --help should mention deprecation, got:\n%s", out)
	}
}
