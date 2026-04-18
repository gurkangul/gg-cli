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

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// resetCmdTree resets all flag Changed/Value state on cmd and its descendants.
// Cobra reuses package-level command objects between test calls; without a
// reset, flags like --help that were "Changed=true" in a previous execution
// remain set and cause cobra to short-circuit subsequent executions (e.g.
// printing help again instead of running the command).
func resetCmdTree(cmd *cobra.Command) {
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		f.Changed = false
		_ = f.Value.Set(f.DefValue)
	})
	for _, child := range cmd.Commands() {
		resetCmdTree(child)
	}
}

// execCmd runs the root cobra command with the given args and returns
// (stdout, stderr, error). It uses a fresh bytes.Buffer for output capture.
// Tests must not run in parallel because rootCmd is a package-level var.
func execCmd(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	// Reset all command flags before each execution so that a previous call
	// with --help (or any other flag) does not pollute this one.
	resetCmdTree(rootCmd)
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

func TestRecord_InvalidTask_Error(t *testing.T) {
	t.Chdir(t.TempDir())
	_, _, err := execCmd(t, "record", "--task=BUG-001", "use JWT")
	if err == nil {
		t.Fatal("expected error for non-TASK --task value")
	}
}

func TestDecide_InvalidTask_Error(t *testing.T) {
	t.Chdir(t.TempDir())
	_, _, err := execCmd(t, "decide", "--task=BUG-001", "use JWT for auth")
	if err == nil {
		t.Fatal("expected error for non-TASK --task value in decide")
	}
}

func TestReject_InvalidTask_Error(t *testing.T) {
	t.Chdir(t.TempDir())
	_, _, err := execCmd(t, "reject", "--task=BUG-001", "microservices", "--reason", "too complex")
	if err == nil {
		t.Fatal("expected error for non-TASK --task value in reject")
	}
}

func TestBugReport_InvalidTask_Error(t *testing.T) {
	t.Chdir(t.TempDir())
	_, _, err := execCmd(t, "bug", "report", "--task=notvalid", "crash on startup")
	if err == nil {
		t.Fatal("expected error for non-TASK --task value in bug report")
	}
}

func TestTaskCreate_InvalidPriority_Error(t *testing.T) {
	t.Chdir(t.TempDir())
	_, _, err := execCmd(t, "task", "create", "--priority=urgent", "do something")
	if err == nil {
		t.Fatal("expected error for invalid --priority value")
	}
}

func TestTaskCreate_InvalidDeps_Error(t *testing.T) {
	t.Chdir(t.TempDir())
	_, _, err := execCmd(t, "task", "create", "--depends-on=BUG-001", "do something")
	if err == nil {
		t.Fatal("expected error for non-TASK --depends-on value")
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

// ── gg task ───────────────────────────────────────────────────────────────────

func TestTaskList_NoConfig_ConfigError(t *testing.T) {
	t.Chdir(t.TempDir())
	_, _, err := execCmd(t, "task", "list")
	if err == nil {
		t.Fatal("expected error when .gg config is absent")
	}
}

func TestTaskGet_NoArgs_Error(t *testing.T) {
	t.Chdir(t.TempDir())
	_, _, err := execCmd(t, "task", "get")
	if err == nil {
		t.Fatal("expected error for missing task ID")
	}
}

func TestTaskGet_NoConfig_ConfigError(t *testing.T) {
	t.Chdir(t.TempDir())
	_, _, err := execCmd(t, "task", "get", "TASK-001")
	if err == nil {
		t.Fatal("expected error when .gg config is absent")
	}
}

func TestTaskDone_NoArgs_Error(t *testing.T) {
	t.Chdir(t.TempDir())
	_, _, err := execCmd(t, "task", "done")
	if err == nil {
		t.Fatal("expected error for missing task ID")
	}
}

func TestTaskDone_NoConfig_ConfigError(t *testing.T) {
	t.Chdir(t.TempDir())
	_, _, err := execCmd(t, "task", "done", "TASK-001", "shipped it")
	if err == nil {
		t.Fatal("expected error when .gg config is absent")
	}
}

func TestTaskBlock_NoArgs_Error(t *testing.T) {
	t.Chdir(t.TempDir())
	_, _, err := execCmd(t, "task", "block")
	if err == nil {
		t.Fatal("expected error for missing task ID")
	}
}

func TestTaskBlock_NoConfig_ConfigError(t *testing.T) {
	t.Chdir(t.TempDir())
	_, _, err := execCmd(t, "task", "block", "TASK-001", "waiting on infra")
	if err == nil {
		t.Fatal("expected error when .gg config is absent")
	}
}

// ── gg tell ───────────────────────────────────────────────────────────────────

func TestTell_NoConfig_ConfigError(t *testing.T) {
	t.Chdir(t.TempDir())
	_, _, err := execCmd(t, "tell", "developer", "ship it")
	if err == nil {
		t.Fatal("expected error when .gg config is absent")
	}
}

// ── gg export ─────────────────────────────────────────────────────────────────

func TestExport_NoConfig_ConfigError(t *testing.T) {
	t.Chdir(t.TempDir())
	_, _, err := execCmd(t, "export")
	if err == nil {
		t.Fatal("expected error when .gg config is absent")
	}
}

// ── gg status ─────────────────────────────────────────────────────────────────

func TestStatus_NoConfig_ConfigError(t *testing.T) {
	t.Chdir(t.TempDir())
	_, _, err := execCmd(t, "status")
	if err == nil {
		t.Fatal("expected error when .gg config is absent")
	}
}

// ── gg inbox ──────────────────────────────────────────────────────────────────

func TestInbox_NoConfig_ConfigError(t *testing.T) {
	t.Chdir(t.TempDir())
	_, _, err := execCmd(t, "inbox")
	if err == nil {
		t.Fatal("expected error when .gg config is absent")
	}
}

// ── gg reject (deprecated alias) ─────────────────────────────────────────────

func TestReject_NoConfig_ConfigError(t *testing.T) {
	t.Chdir(t.TempDir())
	_, _, err := execCmd(t, "reject", "microservices over monolith")
	if err == nil {
		t.Fatal("expected error when .gg config is absent")
	}
}

// ── gg bug ────────────────────────────────────────────────────────────────────

func TestBugList_NoConfig_ConfigError(t *testing.T) {
	t.Chdir(t.TempDir())
	_, _, err := execCmd(t, "bug", "list")
	if err == nil {
		t.Fatal("expected error when .gg config is absent")
	}
}

func TestBugGet_NoArgs_Error(t *testing.T) {
	t.Chdir(t.TempDir())
	_, _, err := execCmd(t, "bug", "get")
	if err == nil {
		t.Fatal("expected error for missing BUG-ID argument")
	}
}

func TestBugGet_NoConfig_ConfigError(t *testing.T) {
	t.Chdir(t.TempDir())
	_, _, err := execCmd(t, "bug", "get", "BUG-001")
	if err == nil {
		t.Fatal("expected error when .gg config is absent")
	}
}

// ── gg bug lifecycle ──────────────────────────────────────────────────────────

func TestBugTriage_NoArgs_Error(t *testing.T) {
	t.Chdir(t.TempDir())
	_, _, err := execCmd(t, "bug", "triage")
	if err == nil {
		t.Fatal("expected error for missing BUG-ID argument")
	}
}

func TestBugTriage_NoConfig_ConfigError(t *testing.T) {
	t.Chdir(t.TempDir())
	_, _, err := execCmd(t, "bug", "triage", "BUG-001")
	if err == nil {
		t.Fatal("expected error when .gg config is absent")
	}
}

func TestBugFix_NoArgs_Error(t *testing.T) {
	t.Chdir(t.TempDir())
	_, _, err := execCmd(t, "bug", "fix")
	if err == nil {
		t.Fatal("expected error for missing arguments")
	}
}

func TestBugFix_NoConfig_ConfigError(t *testing.T) {
	t.Chdir(t.TempDir())
	_, _, err := execCmd(t, "bug", "fix", "BUG-001", "fixed the nil deref")
	if err == nil {
		t.Fatal("expected error when .gg config is absent")
	}
}

func TestBugStart_NoArgs_Error(t *testing.T) {
	t.Chdir(t.TempDir())
	_, _, err := execCmd(t, "bug", "start")
	if err == nil {
		t.Fatal("expected error for missing BUG-ID argument")
	}
}

func TestBugStart_NoConfig_ConfigError(t *testing.T) {
	t.Chdir(t.TempDir())
	_, _, err := execCmd(t, "bug", "start", "BUG-001")
	if err == nil {
		t.Fatal("expected error when .gg config is absent")
	}
}

func TestBugWontFix_NoArgs_Error(t *testing.T) {
	t.Chdir(t.TempDir())
	_, _, err := execCmd(t, "bug", "wontfix")
	if err == nil {
		t.Fatal("expected error for missing arguments")
	}
}

func TestBugWontFix_NoConfig_ConfigError(t *testing.T) {
	t.Chdir(t.TempDir())
	_, _, err := execCmd(t, "bug", "wontfix", "BUG-001", "not a bug")
	if err == nil {
		t.Fatal("expected error when .gg config is absent")
	}
}

// ── gg task deps ──────────────────────────────────────────────────────────────

func TestTaskDeps_NoArgs_Error(t *testing.T) {
	t.Chdir(t.TempDir())
	_, _, err := execCmd(t, "task", "deps")
	if err == nil {
		t.Fatal("expected error for missing task ID argument")
	}
}

func TestTaskDeps_NoConfig_ConfigError(t *testing.T) {
	t.Chdir(t.TempDir())
	_, _, err := execCmd(t, "task", "deps", "TASK-001")
	if err == nil {
		t.Fatal("expected error when .gg config is absent")
	}
}

// ── gg resolveAuthor ─────────────────────────────────────────────────────────

func TestResolveAuthor_FromEnv(t *testing.T) {
	t.Setenv("GG_ROLE", "architect")
	// Use statusCmd (has no --from flag); just check the env resolution.
	if got := resolveAuthor(statusCmd); got != "architect" {
		t.Errorf("expected 'architect', got %q", got)
	}
}

func TestResolveAuthor_EmptyEnv(t *testing.T) {
	t.Setenv("GG_ROLE", "")
	if got := resolveAuthor(statusCmd); got != "" {
		t.Errorf("expected empty, got %q", got)
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

// ── parseMentions / collectTargets ────────────────────────────────────────────

func TestParseMentions_None(t *testing.T) {
	if got := parseMentions("ship it now"); len(got) != 0 {
		t.Errorf("expected no mentions, got %v", got)
	}
}

func TestParseMentions_Single(t *testing.T) {
	got := parseMentions("hey @qa please review")
	if len(got) != 1 || got[0] != "qa" {
		t.Errorf("expected [qa], got %v", got)
	}
}

func TestParseMentions_Multiple(t *testing.T) {
	got := parseMentions("@architect and @qa both need to sign off")
	if len(got) != 2 || got[0] != "architect" || got[1] != "qa" {
		t.Errorf("expected [architect, qa], got %v", got)
	}
}

func TestParseMentions_Deduped(t *testing.T) {
	got := parseMentions("@qa @qa double mention")
	if len(got) != 1 || got[0] != "qa" {
		t.Errorf("expected [qa] deduped, got %v", got)
	}
}

func TestCollectTargets_CommaSeparated(t *testing.T) {
	got := collectTargets("qa,reviewer", "LGTM")
	if len(got) != 2 || got[0] != "qa" || got[1] != "reviewer" {
		t.Errorf("expected [qa, reviewer], got %v", got)
	}
}

func TestCollectTargets_MentionAddsExtra(t *testing.T) {
	got := collectTargets("all", "@qa please check")
	if len(got) != 2 || got[0] != "all" || got[1] != "qa" {
		t.Errorf("expected [all, qa], got %v", got)
	}
}

func TestCollectTargets_MentionAlreadyInTarget(t *testing.T) {
	got := collectTargets("qa", "@qa LGTM")
	if len(got) != 1 || got[0] != "qa" {
		t.Errorf("expected [qa] deduped, got %v", got)
	}
}

func TestCollectTargets_CommaSeparatedWithMentions(t *testing.T) {
	got := collectTargets("all,developer", "@qa review and @developer check")
	// all, developer (from comma), qa (from mention) — developer already present
	if len(got) != 3 {
		t.Errorf("expected 3 targets, got %d: %v", len(got), got)
	}
}

// ── highlightMentions ─────────────────────────────────────────────────────────

func TestHighlightMentions_NoMentions(t *testing.T) {
	got := highlightMentions("ship it")
	if got != "ship it" {
		t.Errorf("unexpected change: %q", got)
	}
}

func TestHighlightMentions_Single(t *testing.T) {
	got := highlightMentions("hey @qa please review")
	want := "hey <@qa> please review"
	if got != want {
		t.Errorf("want %q, got %q", want, got)
	}
}

func TestHighlightMentions_Multiple(t *testing.T) {
	got := highlightMentions("@architect and @qa sign off")
	want := "<@architect> and <@qa> sign off"
	if got != want {
		t.Errorf("want %q, got %q", want, got)
	}
}
