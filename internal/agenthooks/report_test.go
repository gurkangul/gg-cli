package agenthooks

import (
	"bytes"
	"strings"
	"testing"
)

func TestRenderReport_OneLinePerResult(t *testing.T) {
	results := []Result{
		{AgentName: "claude", Tier: TierHard, Action: ActionCreated, Path: "/p/.claude/settings.json"},
		{AgentName: "cursor", Tier: TierHard, Action: ActionNotDetected},
		{AgentName: "codex", Tier: TierSoft, Action: ActionUpdated, Path: "/p/AGENTS.md"},
	}
	var buf bytes.Buffer
	RenderReport(&buf, results)
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d:\n%s", len(lines), buf.String())
	}
	if !strings.Contains(lines[0], "claude") || !strings.Contains(lines[0], "[hard]") {
		t.Errorf("line[0] malformed: %q", lines[0])
	}
	if !strings.Contains(lines[2], "[soft]") {
		t.Errorf("line[2] missing tier tag: %q", lines[2])
	}
}

func TestRenderReport_IncludesNotesAndErrors(t *testing.T) {
	results := []Result{
		{
			AgentName: "zai", Tier: TierFallback, Action: ActionUpToDate,
			Notes: []string{"use alias", "or prefix GG_AGENT"},
		},
		{
			AgentName: "codex", Tier: TierSoft, Action: ActionFailed,
			Err: errorsNew("AGENTS.md missing"),
		},
	}
	var buf bytes.Buffer
	RenderReport(&buf, results)
	out := buf.String()
	if !strings.Contains(out, "use alias") {
		t.Errorf("notes missing from output: %s", out)
	}
	if !strings.Contains(out, "AGENTS.md missing") {
		t.Errorf("error missing from output: %s", out)
	}
}

func TestHasProblems(t *testing.T) {
	if HasProblems([]Result{{Action: ActionCreated}}) {
		t.Error("HasProblems=true on clean results")
	}
	if !HasProblems([]Result{{Action: ActionFailed}}) {
		t.Error("HasProblems=false on failed result")
	}
}

func TestSummary_CountsEveryAction(t *testing.T) {
	results := []Result{
		{Action: ActionCreated},
		{Action: ActionCreated},
		{Action: ActionUpdated},
		{Action: ActionUpToDate},
		{Action: ActionNotDetected},
		{Action: ActionFailed},
		{Action: ActionDryRun},
	}
	s := Summary(results)
	wants := []string{"created=2", "updated=1", "up-to-date=1", "not-detected=1", "failed=1", "dry-run=1"}
	for _, w := range wants {
		if !strings.Contains(s, w) {
			t.Errorf("summary missing %q: %s", w, s)
		}
	}
}

func TestSummary_OmitsDryRunWhenZero(t *testing.T) {
	s := Summary([]Result{{Action: ActionCreated}})
	if strings.Contains(s, "dry-run") {
		t.Errorf("dry-run should be omitted when zero: %s", s)
	}
}

// tiny errors shim so the test file stays self-contained.
type simpleErr string

func (s simpleErr) Error() string { return string(s) }
func errorsNew(s string) error    { return simpleErr(s) }
