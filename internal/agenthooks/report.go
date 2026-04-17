package agenthooks

import (
	"fmt"
	"io"
)

// RenderReport writes a human-readable summary of install results to w.
// Output is stable and testable — one line per agent with an icon, name,
// action, path, and tier tag. Notes (e.g. the zai shell alias advisory)
// follow indented under their owner.
func RenderReport(w io.Writer, results []Result) {
	for _, r := range results {
		ic := icon(r.Action)
		label := r.DisplayName
		if label == "" {
			label = r.AgentName
		}
		path := r.Path
		if path == "" {
			path = "—"
		}
		fmt.Fprintf(w, "  %s %-10s %-12s %-40s [%s]\n",
			ic, label, string(r.Action), path, r.Tier)
		if r.Err != nil {
			fmt.Fprintf(w, "      error: %v\n", r.Err)
		}
		for _, note := range r.Notes {
			fmt.Fprintf(w, "      %s\n", note)
		}
	}
}

// HasProblems reports whether any result represents a real failure
// (ActionFailed). Callers use this to set non-zero exit codes when
// --install-agent-hooks is run in strict mode.
func HasProblems(results []Result) bool {
	for _, r := range results {
		if r.Action == ActionFailed {
			return true
		}
	}
	return false
}

// Summary returns a compact one-line count for the end of a report.
func Summary(results []Result) string {
	counts := map[Action]int{}
	for _, r := range results {
		counts[r.Action]++
	}
	base := fmt.Sprintf("created=%d updated=%d up-to-date=%d not-detected=%d failed=%d",
		counts[ActionCreated],
		counts[ActionUpdated],
		counts[ActionUpToDate],
		counts[ActionNotDetected],
		counts[ActionFailed],
	)
	if counts[ActionDryRun] > 0 {
		base += fmt.Sprintf(" dry-run=%d", counts[ActionDryRun])
	}
	return base
}

func icon(a Action) string {
	switch a {
	case ActionCreated, ActionUpdated:
		return "✓"
	case ActionUpToDate:
		return "·"
	case ActionNotDetected:
		return "-"
	case ActionDryRun:
		return "?"
	case ActionFailed:
		return "✗"
	case ActionSuggested:
		return "⚠"
	}
	return " "
}
