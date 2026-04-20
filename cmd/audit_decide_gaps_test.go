package cmd

import (
	"testing"
	"time"
)

func TestMessageHasDecideLanguage(t *testing.T) {
	cases := []struct {
		content string
		want    bool
	}{
		// Positive — clear decision statements.
		{"We decided to use JWT for auth", true},
		{"going with the embedded store approach", true},
		{"We chose cobra over urfave/cli", true},
		{"We rejected the REST approach because it adds a daemon", true},
		{"Rejected because it would require a background process", true},
		{"instead of gRPC we'll use plain HTTP", true},
		{"The decision is to keep a single binary", true},
		{"decision: use cobra v1", true},
		{"karar verdik: Qdrant kalacak", true},
		{"reddettik — daemon yaklaşımı reddettik", true},
		{"We agreed to use the outbox pattern", true},
		{"agreed on a single state file per project", true},
		// Negative — ambiguous or non-decision text.
		{"just pushed the fix", false},
		{"tests are green", false},
		{"looking at the options now", false},
		{"the code is ready for review", false},
		{"TASK-042 done: shipped the feature", false},
		{"blocked on Qdrant being unreachable", false},
	}
	for _, c := range cases {
		got := messageHasDecideLanguage(c.content)
		if got != c.want {
			t.Errorf("messageHasDecideLanguage(%q) = %v, want %v", c.content, got, c.want)
		}
	}
}

func TestDecisionsInWindow(t *testing.T) {
	base := time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC)

	inside := base.Add(30 * time.Minute).Format(time.RFC3339)
	outside := base.Add(3 * time.Hour).Format(time.RFC3339)
	invalid := "not-a-time"

	// Match within window.
	got := decisionsInWindow([]string{inside, outside, invalid}, base)
	if len(got) != 1 || got[0] != inside {
		t.Errorf("decisionsInWindow: got %v, want [%s]", got, inside)
	}

	// No decisions at all.
	got = decisionsInWindow(nil, base)
	if len(got) != 0 {
		t.Errorf("decisionsInWindow(nil): expected empty, got %v", got)
	}

	// Exactly at boundary (2h) — should be included.
	boundary := base.Add(decideGapCoverageWindow).Format(time.RFC3339)
	got = decisionsInWindow([]string{boundary}, base)
	if len(got) != 1 {
		t.Errorf("decisionsInWindow at boundary: expected 1 match, got %v", got)
	}

	// Just over boundary — should be excluded.
	overBoundary := base.Add(decideGapCoverageWindow + time.Second).Format(time.RFC3339)
	got = decisionsInWindow([]string{overBoundary}, base)
	if len(got) != 0 {
		t.Errorf("decisionsInWindow over boundary: expected 0 matches, got %v", got)
	}
}

func TestTruncate(t *testing.T) {
	cases := []struct {
		s    string
		max  int
		want string
	}{
		{"short", 10, "short"},
		{"exactly10x", 10, "exactly10x"},
		{"this is longer than ten", 10, "this is l…"},
		{"has\nnewlines\nhere", 50, "has newlines here"},
	}
	for _, c := range cases {
		got := truncate(c.s, c.max)
		if got != c.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", c.s, c.max, got, c.want)
		}
	}
}
