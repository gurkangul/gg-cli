package cmd

import (
	"strings"
	"testing"
)

// BUG-092(b): the telemetry summary used to mix real CLI-command verbs
// (user-invoked subcommands) with internal brain-kind / leaf-verb access labels
// in one ranked list, so a label like "track" or "get" read like a runnable
// `gg <verb>`. topLevelVerbs() backs the split: only registered top-level
// commands (and their aliases) are real `gg <verb>` commands.
func TestTopLevelVerbs_SeparatesCommandsFromBrainKindLabels(t *testing.T) {
	verbs := topLevelVerbs()

	// Real top-level commands the summary must list under "Command usage".
	for _, real := range []string{"verify", "bug", "task", "telemetry", "search", "decisions"} {
		if !verbs[real] {
			t.Errorf("topLevelVerbs() should mark %q as a runnable top-level command", real)
		}
	}

	// Internal brain-kind / leaf-verb access labels that are NOT runnable as
	// `gg <verb>` — these must fall into the separate subcommand-verb section.
	for _, label := range []string{"track", "get", "report", "run-repros"} {
		if verbs[label] {
			t.Errorf("topLevelVerbs() must NOT mark internal access label %q as a top-level command", label)
		}
	}
}

// BUG-092(a): the bar chart was not normalized to the busiest verb, so a
// 903-count verb and a 12-count verb both rendered full-width. telemetryBar must
// scale length proportionally to max so the dominant verb's bar is strictly
// longer than a tiny verb's.
func TestTelemetryBar_NormalizedToMax(t *testing.T) {
	const max = 903
	busy := telemetryBar(max, max)
	tiny := telemetryBar(12, max)

	if len([]rune(busy)) != telemetryBarWidth {
		t.Errorf("busiest verb bar = %d cells, want full width %d", len([]rune(busy)), telemetryBarWidth)
	}
	if len([]rune(tiny)) >= len([]rune(busy)) {
		t.Errorf("a 12/903 bar (%d cells) must be shorter than the 903/903 bar (%d cells) — bars are not normalized",
			len([]rune(tiny)), len([]rune(busy)))
	}
	// A non-zero count always shows at least one cell (never blank).
	if strings.TrimSpace(tiny) == "" {
		t.Error("a non-zero count must render at least one bar cell")
	}
	// Zero / non-positive inputs render nothing.
	if telemetryBar(0, max) != "" || telemetryBar(5, 0) != "" {
		t.Error("zero count or zero max must render an empty bar")
	}
}
