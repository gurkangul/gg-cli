package cmd

import (
	"strings"
	"testing"

	"github.com/gurkangul/gg-cli/internal/store"
)

func TestFormatOnboarding(t *testing.T) {
	out := formatOnboarding([]store.CanonEntry{{Area: "key-decisions", Text: "• JSONL is the source of truth"}})
	for _, want := range []string{"ONBOARDING", "key-decisions", "JSONL is the source of truth", "how to work here", "gg search", "gg task create"} {
		if !strings.Contains(out, want) {
			t.Errorf("onboarding briefing missing %q\n%s", want, out)
		}
	}
}

func TestFormatOnboarding_EmptyCanon(t *testing.T) {
	out := formatOnboarding(nil)
	if !strings.Contains(out, "no distilled knowledge yet") {
		t.Errorf("empty-canon briefing should explain the canon self-builds:\n%s", out)
	}
	if !strings.Contains(out, "how to work here") {
		t.Error("briefing must always include the workflow section")
	}
}
