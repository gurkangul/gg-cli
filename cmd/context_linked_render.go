package cmd

import (
	"fmt"
	"io"

	"github.com/gurkangul/gg-cli/internal/store"
)

func writeCompactDecisionsWithSource(w io.Writer, ds []store.Decision, sources sourceLabels) {
	for _, d := range ds {
		fmt.Fprintln(w, compactDecisionLine(d)+sourceSuffix(sources.get("decision", d.ID)))
	}
}

func writeCompactRejectionsWithSource(w io.Writer, rs []store.Rejection, sources sourceLabels) {
	for _, r := range rs {
		fmt.Fprintln(w, compactRejectionLine(r)+sourceSuffix(sources.get("rejection", r.ID)))
	}
}

func writeCompactTasksWithSource(w io.Writer, ts []store.Task, sources sourceLabels) {
	for _, t := range ts {
		fmt.Fprintln(w, compactTaskLine(t)+sourceSuffix(sources.get("task", t.ID)))
	}
}

func writeCompactNotesWithSource(w io.Writer, ns []store.Note, sources sourceLabels) {
	if len(sources) == 0 {
		writeCompactNotes(w, ns)
		return
	}
	for _, n := range ns {
		fmt.Fprintln(w, compactNoteLine(n)+sourceSuffix(sources.get("note", n.ID)))
	}
}

func writeCompactDiscussionsWithSource(w io.Writer, ds []store.Discussion, sources sourceLabels) {
	if len(sources) == 0 {
		writeCompactDiscussions(w, ds)
		return
	}
	for _, d := range ds {
		fmt.Fprintln(w, compactDiscussionLine(d)+sourceSuffix(sources.get("discussion", d.ID)))
	}
}
