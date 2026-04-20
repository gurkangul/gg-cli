package session

import (
	"bytes"
	"strings"
	"testing"
)

func TestBriefing_Render_ContainsMarker(t *testing.T) {
	var buf bytes.Buffer
	b := Briefing{Agent: "claude-code"}
	if err := b.Render(&buf); err != nil {
		t.Fatalf("Render: %v", err)
	}
	out := buf.String()
	wantFirstLine := MarkerPrefix + ProtocolVersion
	if !strings.HasPrefix(out, wantFirstLine+"\n") {
		t.Errorf("first line = %q, want prefix %q", firstLine(out), wantFirstLine)
	}
	if !strings.Contains(out, "agent: claude-code") {
		t.Errorf("output missing agent line: %q", out)
	}
}

func TestBriefing_Render_OmitsEmptyFields(t *testing.T) {
	var buf bytes.Buffer
	b := Briefing{Agent: "gsd"} // no ProjectID, no ProjectRoot
	if err := b.Render(&buf); err != nil {
		t.Fatalf("Render: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, "project_id:") {
		t.Errorf("empty ProjectID should be omitted, got %q", out)
	}
	if strings.Contains(out, "project_root:") {
		t.Errorf("empty ProjectRoot should be omitted, got %q", out)
	}
}

func TestBriefing_Render_IncludesProtocolSteps(t *testing.T) {
	var buf bytes.Buffer
	b := Briefing{Agent: "cursor", ProjectID: "abc", ProjectRoot: "/tmp/x"}
	if err := b.Render(&buf); err != nil {
		t.Fatalf("Render: %v", err)
	}
	out := buf.String()
	// All 5 rules must appear in order. If any fall out the briefing has
	// silently stopped reminding agents of the full protocol.
	musts := []string{"AGENTS.md", "gg search", "decision", "gg tell", "gg inbox"}
	for _, m := range musts {
		if !strings.Contains(out, m) {
			t.Errorf("briefing missing reference to %q: %q", m, out)
		}
	}

	// Rule 5 must mention silent skip.
	if !strings.Contains(out, "silent skip") && !strings.Contains(out, "silent-skip") &&
		!strings.Contains(out, "violation") {
		t.Errorf("briefing rule 5 must mention violation consequence: %q", out)
	}
}

func TestPasteBlock_DefaultAgent(t *testing.T) {
	got := PasteBlock("")
	if !strings.Contains(got, "GG_AGENT=claude-code") {
		t.Errorf("empty hint should default to claude-code, got %q", got)
	}
}

func TestPasteBlock_CustomAgent(t *testing.T) {
	got := PasteBlock("cursor")
	if !strings.Contains(got, "GG_AGENT=cursor") {
		t.Errorf("hint=cursor should appear in paste block, got %q", got)
	}
}

func TestPasteBlock_TrimsWhitespaceHint(t *testing.T) {
	got := PasteBlock("   ")
	if !strings.Contains(got, "GG_AGENT=claude-code") {
		t.Errorf("whitespace hint should fall back to claude-code, got %q", got)
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
