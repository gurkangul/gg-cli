package agenthooks

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstallDetected_ReportsEveryAgent(t *testing.T) {
	root := t.TempDir()
	results := InstallDetected(root, Options{})
	if len(results) != len(AllNames()) {
		t.Fatalf("results len = %d, want %d (one per registered agent)",
			len(results), len(AllNames()))
	}
	// In an empty project dir every agent should be either not-detected or,
	// for claude, suggested (when the host machine has Claude Code installed
	// globally). The always-on soft installers (gemini, openhands) Detect()
	// unconditionally and create their native file even in a bare dir, so they
	// report ActionCreated — that is intentional coverage, not a failure.
	alwaysOn := map[string]bool{"gemini": true, "openhands": true}
	for _, r := range results {
		if alwaysOn[r.AgentName] {
			if r.Action != ActionCreated {
				t.Errorf("agent %s action = %q in empty dir, want %q (always-on soft installer)",
					r.AgentName, r.Action, ActionCreated)
			}
			continue
		}
		switch r.Action {
		case ActionNotDetected, ActionSuggested:
			// OK — agent not active in this project
		default:
			t.Errorf("agent %s action = %q in empty dir, want %q or %q",
				r.AgentName, r.Action, ActionNotDetected, ActionSuggested)
		}
	}
}

func TestInstallDetected_ClaudeOnlyWhenPresent(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	results := InstallDetected(root, Options{})
	var claudeRes *Result
	for i := range results {
		if results[i].AgentName == "claude" {
			claudeRes = &results[i]
		}
	}
	if claudeRes == nil {
		t.Fatal("claude result missing from report")
		return
	}
	if claudeRes.Action != ActionCreated {
		t.Errorf("claude action = %q, want %q", claudeRes.Action, ActionCreated)
	}
}

func TestInstallNamed_UnknownAgentErrors(t *testing.T) {
	root := t.TempDir()
	results := InstallNamed(root, []string{"nope"}, Options{})
	if len(results) != 1 {
		t.Fatalf("results len = %d, want 1", len(results))
	}
	if results[0].Action != ActionFailed {
		t.Errorf("action = %q, want %q", results[0].Action, ActionFailed)
	}
	if results[0].Err == nil {
		t.Error("err should be non-nil for unknown agent")
	}
}

func TestInstallNamed_ForceBypassesDetection(t *testing.T) {
	root := t.TempDir()
	// No .claude/ dir exists — Detect() returns false — but InstallNamed
	// is used when the user explicitly asks, so it should install anyway.
	results := InstallNamed(root, []string{"claude"}, Options{})
	if len(results) != 1 {
		t.Fatalf("results len = %d, want 1", len(results))
	}
	if results[0].Action != ActionCreated {
		t.Errorf("action = %q, want %q", results[0].Action, ActionCreated)
	}
	if _, err := os.Stat(filepath.Join(root, ".claude", "settings.json")); err != nil {
		t.Errorf("settings.json not written: %v", err)
	}
}

func TestTierString(t *testing.T) {
	cases := []struct {
		tier Tier
		want string
	}{
		{TierHard, "hard"},
		{TierSoft, "soft"},
		{TierFallback, "fallback"},
		{TierUnknown, "unknown"},
	}
	for _, c := range cases {
		if got := c.tier.String(); got != c.want {
			t.Errorf("%d.String() = %q, want %q", c.tier, got, c.want)
		}
	}
}

func TestAllNames_CoversAllInstallers(t *testing.T) {
	names := AllNames()
	want := []string{"claude", "cursor", "codex", "gemini", "openhands", "bmad", "gsd"}
	if len(names) != len(want) {
		t.Fatalf("got %v, want %v", names, want)
	}
	for i, w := range want {
		if names[i] != w {
			t.Errorf("names[%d] = %q, want %q", i, names[i], w)
		}
	}
}
