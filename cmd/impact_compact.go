package cmd

import (
	"fmt"
	"io"
	"strings"
)

func renderImpactCompact(w io.Writer, r impactResult) {
	if r.HopDepth <= 1 {
		fmt.Fprintf(w, "impact: %s — %d deps %d sym %dD %dT %dR %dB\n\n",
			r.File, len(r.Dependents), len(r.Symbols),
			len(r.Decisions), len(r.Tasks), len(r.Rejections), len(r.HistoricalBugs))
		for _, dep := range r.Dependents {
			fmt.Fprintf(w, "→ %s\n", dep)
		}
	} else {
		fmt.Fprintf(w, "impact: %s — %d deps h%d %d sym %dD %dT %dR %dB\n\n",
			r.File, len(r.Dependents), r.HopDepth, len(r.Symbols),
			len(r.Decisions), len(r.Tasks), len(r.Rejections), len(r.HistoricalBugs))
		for _, dep := range r.DependentHops {
			fmt.Fprintf(w, "H%d %s\n", dep.Hop, dep.Path)
		}
		if len(r.Traversal.Cycles) > 0 {
			fmt.Fprintf(w, "C %s\n", strings.Join(r.Traversal.Cycles, ", "))
		}
		if r.Traversal.Truncated {
			fmt.Fprintf(w, "T truncated at h%d\n", r.Traversal.MaxDepth)
		}
	}
	for _, s := range r.Symbols {
		name, _ := s["name"].(string)
		if name != "" {
			fmt.Fprintf(w, "S %s\n", name)
		}
	}
	writeCompactDecisions(w, r.Decisions)
	writeCompactTasks(w, r.Tasks)
	writeCompactRejections(w, r.Rejections)
	for _, b := range r.HistoricalBugs {
		fmt.Fprintf(w, "B  %s  %s\n", b.BugID, compactTrim(b.Title, compactLineWidth))
	}
	if len(r.Warnings) > 0 {
		fmt.Fprintf(w, "\n! %s\n", strings.Join(r.Warnings, "; "))
	}
}
