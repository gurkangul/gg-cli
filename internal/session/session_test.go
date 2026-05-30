package session

import (
	"bytes"
	"strings"
	"testing"
)

func TestBriefing_Render_ContainsMarker(t *testing.T) {
	var buf bytes.Buffer
	b := Briefing{Agent: "omo-slim", Role: "implementer"}
	if err := b.Render(&buf); err != nil {
		t.Fatalf("Render: %v", err)
	}
	out := buf.String()
	wantFirstLine := MarkerPrefix + ProtocolVersion
	if !strings.HasPrefix(out, wantFirstLine+"\n") {
		t.Errorf("first line = %q, want prefix %q", firstLine(out), wantFirstLine)
	}
	if !strings.Contains(out, "agent: omo-slim") {
		t.Errorf("output missing agent line: %q", out)
	}
	if !strings.Contains(out, "role: implementer") {
		t.Errorf("output missing role line: %q", out)
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
	b := Briefing{Agent: "cursor", Role: "reviewer", ProjectID: "abc", ProjectRoot: "/tmp/x"}
	if err := b.Render(&buf); err != nil {
		t.Fatalf("Render: %v", err)
	}
	out := buf.String()
	musts := []string{"Next:", "gg next --agent cursor --role reviewer", "Fallback:", "gg inbox --role reviewer --peek", "gg task list --ready --compact", "gg context --compact", "docs/agent-protocol-v1.md"}
	for _, m := range musts {
		if !strings.Contains(out, m) {
			t.Errorf("briefing missing reference to %q: %q", m, out)
		}
	}
}

func TestPasteBlock_DefaultAgent(t *testing.T) {
	got := PasteBlock("")
	if !strings.Contains(got, "Suggested for this hook context: agent") {
		t.Errorf("empty hint should default to generic agent, got %q", got)
	}
}

func TestPasteBlock_CustomAgent(t *testing.T) {
	got := PasteBlock("claude-code")
	if !strings.Contains(got, "Suggested for this hook context: claude-code") {
		t.Errorf("hint=claude-code should appear in paste block, got %q", got)
	}
	if !strings.Contains(got, "--role \"$GG_ROLE\"") {
		t.Errorf("paste block should use session-start --role, got %q", got)
	}
	if strings.Contains(got, "export GG_AGENT=") {
		t.Errorf("paste block should not contain a copyable GG_AGENT assignment, got %q", got)
	}
	if !strings.Contains(got, "do not continue with a placeholder") {
		t.Errorf("paste block should forbid placeholder identities, got %q", got)
	}
	if !strings.Contains(got, "If this shell is GSD, use a unique gsd-* id") {
		t.Errorf("paste block should explain GSD shell identity, got %q", got)
	}
}

func TestPasteBlock_TrimsWhitespaceHint(t *testing.T) {
	got := PasteBlock("   ")
	if !strings.Contains(got, "Suggested for this hook context: agent") {
		t.Errorf("whitespace hint should fall back to generic agent, got %q", got)
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
