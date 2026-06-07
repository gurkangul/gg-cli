package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/store"
)

// BUG-050: --full must force a full (non-compact) render even when the agent
// environment would otherwise auto-compact, so an agent can record the hydration
// proof that ready-for-live/done/block require.
func TestIsCompactActive_FullOverridesAgentAutoCompact(t *testing.T) {
	t.Setenv("GG_AGENT", "agent-x")
	t.Setenv("GG_ROLE", "")
	t.Setenv("GG_COMPACT", "")

	cmd := &cobra.Command{Use: "get"}
	cmd.Flags().Bool("compact", false, "")
	cmd.Flags().Bool("full", false, "")

	// With a compact flag present and GG_AGENT set, auto-compact is active.
	if !isCompactActive(cmd) {
		t.Fatal("expected auto-compact under GG_AGENT for a command with a --compact flag")
	}
	// --full overrides it.
	if err := cmd.Flags().Set("full", "true"); err != nil {
		t.Fatalf("set --full: %v", err)
	}
	if isCompactActive(cmd) {
		t.Fatal("--full must force a full (non-compact) render")
	}
}

// BUG-086: the full context render must surface evidence, and mark a decision
// with no evidence as [unverified], so a verified fact is distinguishable from a
// claim.
func TestContextRender_ShowsEvidenceAndUnverified(t *testing.T) {
	bundle := contextBundle{
		decisions: []store.Decision{
			{ID: "d1", Text: "use Redis", Reason: "atomic INCR", Evidence: "bench 50k/s", CreatedAt: "2026-06-07T00:00:00Z"},
			{ID: "d2", Text: "log to stdout", Reason: "12-factor", CreatedAt: "2026-06-07T00:00:00Z"},
		},
	}
	var buf bytes.Buffer
	renderContextDefault(&buf, "logging", bundle, nil, nil)
	out := buf.String()
	if !strings.Contains(out, "Evidence: bench 50k/s") {
		t.Errorf("evidence not rendered:\n%s", out)
	}
	if !strings.Contains(out, "[unverified]") {
		t.Errorf("no-evidence decision must render [unverified]:\n%s", out)
	}
}

// QA follow-up: project orientation must drop test/scrubber fixture notes.
func TestFilterFixtureNotes(t *testing.T) {
	in := []store.Note{
		{Text: "real architectural observation about caching"},
		{Text: "SMOKE-TEST-DO-NOT-USE: fake key sk-test-AAAA"},
		{Text: "test banner"},
		{Text: "[GSD] Slice M001/S01 pending"},
	}
	out := filterFixtureNotes(in)
	if len(out) != 2 {
		t.Fatalf("expected 2 real notes, got %d: %+v", len(out), out)
	}
	for _, n := range out {
		if strings.Contains(strings.ToLower(n.Text), "smoke-test") || strings.TrimSpace(strings.ToLower(n.Text)) == "test banner" {
			t.Errorf("fixture note leaked: %q", n.Text)
		}
	}
}
