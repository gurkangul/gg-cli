package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/store"
)

// ── isCompactActive ──────────────────────────────────────────────────────────

func newTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().Bool("compact", false, "")
	cmd.Flags().String("from", "", "")
	return cmd
}

func TestIsCompactActive_FlagExplicitTrue(t *testing.T) {
	t.Setenv("GG_ROLE", "")
	t.Setenv("GG_AGENT", "")
	t.Setenv("GG_COMPACT", "")
	cmd := newTestCmd()
	_ = cmd.Flags().Set("compact", "true")
	if !isCompactActive(cmd) {
		t.Error("explicit --compact=true should win")
	}
}

func TestIsCompactActive_FlagExplicitFalseOverridesAgent(t *testing.T) {
	// Agent env would imply compact, but explicit --compact=false must win
	// so an agent can opt out for debugging.
	t.Setenv("GG_AGENT", "claude-code")
	t.Setenv("GG_COMPACT", "")
	cmd := newTestCmd()
	_ = cmd.Flags().Set("compact", "false")
	if isCompactActive(cmd) {
		t.Error("explicit --compact=false should override agent auto-compact")
	}
}

func TestIsCompactActive_EnvTrue(t *testing.T) {
	t.Setenv("GG_ROLE", "")
	t.Setenv("GG_AGENT", "")
	t.Setenv("GG_COMPACT", "1")
	cmd := newTestCmd()
	if !isCompactActive(cmd) {
		t.Error("GG_COMPACT=1 should activate compact")
	}
}

func TestIsCompactActive_EnvFalseOverridesAgent(t *testing.T) {
	t.Setenv("GG_AGENT", "claude-code")
	t.Setenv("GG_COMPACT", "0")
	cmd := newTestCmd()
	if isCompactActive(cmd) {
		t.Error("GG_COMPACT=0 should override agent auto-compact")
	}
}

func TestIsCompactActive_AgentRoleImpliesCompact(t *testing.T) {
	t.Setenv("GG_COMPACT", "")
	t.Setenv("GG_AGENT", "")
	t.Setenv("GG_ROLE", "developer")
	cmd := newTestCmd()
	if !isCompactActive(cmd) {
		t.Error("GG_ROLE set should imply compact")
	}
}

func TestIsCompactActive_AgentEnvImpliesCompact(t *testing.T) {
	t.Setenv("GG_COMPACT", "")
	t.Setenv("GG_ROLE", "")
	t.Setenv("GG_AGENT", "cursor")
	cmd := newTestCmd()
	if !isCompactActive(cmd) {
		t.Error("GG_AGENT set should imply compact")
	}
}

func TestIsCompactActive_FromFlagImpliesCompact(t *testing.T) {
	t.Setenv("GG_COMPACT", "")
	t.Setenv("GG_ROLE", "")
	t.Setenv("GG_AGENT", "")
	cmd := newTestCmd()
	_ = cmd.Flags().Set("from", "architect")
	if !isCompactActive(cmd) {
		t.Error("--from set should imply compact (agent signal)")
	}
}

func TestIsCompactActive_HumanDefaultOff(t *testing.T) {
	t.Setenv("GG_COMPACT", "")
	t.Setenv("GG_ROLE", "")
	t.Setenv("GG_AGENT", "")
	cmd := newTestCmd()
	if isCompactActive(cmd) {
		t.Error("no signals set — humans should get default off")
	}
}

func TestIsCompactActive_NilCmdWithExplicitEnv(t *testing.T) {
	// Safety check: passing nil must not panic. Explicit GG_COMPACT=1 still
	// honoured — the env is a user statement of intent that doesn't depend
	// on command introspection.
	t.Setenv("GG_COMPACT", "1")
	if !isCompactActive(nil) {
		t.Error("nil cmd + GG_COMPACT=1 should return true")
	}
}

func TestIsCompactActive_NilCmdAgentWithoutEnv(t *testing.T) {
	// Without an explicit env, agent auto-compact needs a cmd to verify the
	// command registered a --compact flag. nil cmd = can't tell = off.
	// Prevents callers from silently compacting output on commands with no
	// compact render path.
	t.Setenv("GG_COMPACT", "")
	t.Setenv("GG_AGENT", "claude-code")
	if isCompactActive(nil) {
		t.Error("nil cmd + agent env without explicit GG_COMPACT should be off (no safe auto-compact decision possible)")
	}
}

func TestIsCompactActive_AgentWithoutCompactFlagRegistered(t *testing.T) {
	// Command without a --compact flag registered: agent auto-compact must
	// not kick in — the command has no compact render path to route to.
	t.Setenv("GG_COMPACT", "")
	t.Setenv("GG_AGENT", "claude-code")
	cmd := &cobra.Command{Use: "no-compact-flag"} // no Flags registered
	if isCompactActive(cmd) {
		t.Error("command without --compact flag should never auto-compact for agents")
	}
}

// ── Shared line builders ─────────────────────────────────────────────────────

func TestCompactDecisionLine(t *testing.T) {
	d := store.Decision{
		Text:      "use JWT for stateless auth",
		CreatedAt: "2026-04-10T12:00:00Z",
		TaskID:    "TASK-001",
	}
	got := compactDecisionLine(d)
	want := "D  2026-04-10  use JWT for stateless auth →TASK-001"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestCompactDecisionLine_NoTask(t *testing.T) {
	d := store.Decision{Text: "x", CreatedAt: "2026-04-10T12:00:00Z"}
	got := compactDecisionLine(d)
	if strings.Contains(got, "→") {
		t.Errorf("no TaskID should omit arrow, got %q", got)
	}
}

func TestCompactTaskLine(t *testing.T) {
	tk := store.Task{ID: "TASK-042", Title: "rotate secrets", Status: "in_progress", Priority: "high"}
	got := compactTaskLine(tk)
	want := "T → TASK-042  rotate secrets (high)"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestCompactBugLine(t *testing.T) {
	b := store.Bug{ID: "BUG-007", Severity: "high", Status: "open", Title: "flaky test"}
	got := compactBugLine(b)
	want := "B  BUG-007 [high/open]  flaky test"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestCompactMessageLine_FullTimestamp(t *testing.T) {
	m := store.Message{
		FromRole: "claude-code", ToRole: "all",
		Content:   "TASK-258 shipped",
		CreatedAt: "2026-04-20T23:12:00Z",
	}
	got := compactMessageLine(m)
	// Expect MM-DD HH:mm format compressed from RFC3339.
	if !strings.Contains(got, "04-20 23:12") {
		t.Errorf("expected short timestamp, got %q", got)
	}
	if !strings.Contains(got, "claude-code→all") {
		t.Errorf("expected from→to, got %q", got)
	}
}

func TestCompactMessageLine_MultilineFolded(t *testing.T) {
	m := store.Message{
		FromRole: "a", ToRole: "b",
		Content:   "line1\nline2",
		CreatedAt: "2026-04-20T23:12:00Z",
	}
	got := compactMessageLine(m)
	if strings.Contains(got, "\n") {
		t.Errorf("newlines must be folded in compact message line, got %q", got)
	}
}

// ── humanTokenCount ─────────────────────────────────────────────────────────

func TestHumanTokenCount(t *testing.T) {
	cases := []struct {
		n    int
		want string
	}{
		{0, "0"},
		{42, "42"},
		{999, "999"},
		{1000, "1.0K"},
		{1500, "1.5K"},
		{9999, "10.0K"}, // rounding is fine — metric is approximate
		{10_000, "10K"},
		{12_345, "12K"},
		{166_900, "166K"},
	}
	for _, tc := range cases {
		got := humanTokenCount(tc.n)
		if got != tc.want {
			t.Errorf("humanTokenCount(%d) = %q, want %q", tc.n, got, tc.want)
		}
	}
}

// ── renderTaskImpactCompact ────────────────────────────────────────────────

func TestRenderTaskImpactCompact(t *testing.T) {
	r := taskImpactResult{
		TaskID:     "TASK-100",
		Dependents: []string{"TASK-101", "TASK-102"},
		Siblings:   []string{"TASK-099"},
		Decisions: []store.Decision{
			{Text: "use X", CreatedAt: "2026-04-10T12:00:00Z", TaskID: "TASK-100", Reason: "MUST NOT APPEAR"},
		},
		Tasks: []store.Task{
			{ID: "TASK-050", Title: "prerequisite", Status: "done", Priority: "medium", Detail: "MUST NOT APPEAR"},
		},
		Bugs: []store.Bug{{ID: "BUG-001", Severity: "low", Status: "fixed", Title: "minor glitch"}},
	}
	var buf bytes.Buffer
	renderTaskImpactCompact(&buf, r)
	out := buf.String()

	for _, want := range []string{
		"impact: TASK-100",
		"→ TASK-101",
		"~ TASK-099",
		"D  2026-04-10  use X →TASK-100",
		"T ✓ TASK-050  prerequisite (medium)",
		"B  BUG-001 [low/fixed]  minor glitch",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "MUST NOT APPEAR") {
		t.Errorf("leaked suppressed body:\n%s", out)
	}
}

// ── renderTaskListCompact ──────────────────────────────────────────────────

func TestRenderTaskListCompact(t *testing.T) {
	tasks := []store.Task{
		{ID: "TASK-001", Title: "first task", Status: "pending", Priority: "high", Author: "MUST NOT APPEAR"},
		{ID: "TASK-002", Title: "second task", Status: "in_progress", Priority: "medium"},
	}
	var buf bytes.Buffer
	renderTaskListCompact(&buf, tasks)
	out := buf.String()

	if !strings.Contains(out, "T ○ TASK-001  first task (high)") {
		t.Errorf("missing task 1:\n%s", out)
	}
	if !strings.Contains(out, "T → TASK-002  second task (medium)") {
		t.Errorf("missing task 2:\n%s", out)
	}
	if strings.Contains(out, "MUST NOT APPEAR") {
		t.Errorf("author leaked into compact list")
	}
}

// ── writeInboxCompact ──────────────────────────────────────────────────────

func TestWriteInboxCompact(t *testing.T) {
	msgs := []store.Message{
		{FromRole: "pm", ToRole: "developer", Content: "ship TASK-001", CreatedAt: "2026-04-20T10:00:00Z", TaskID: "TASK-001"},
		{FromRole: "qa", ToRole: "developer", Content: "tests red", CreatedAt: "2026-04-20T09:00:00Z"},
	}
	var buf bytes.Buffer
	writeInboxCompact(&buf, msgs)
	out := buf.String()

	if !strings.Contains(out, "inbox — 2 unread") {
		t.Errorf("missing header:\n%s", out)
	}
	if !strings.Contains(out, "pm→developer") {
		t.Errorf("missing sender→recipient:\n%s", out)
	}
	if !strings.Contains(out, "→TASK-001") {
		t.Errorf("missing task link:\n%s", out)
	}
}
