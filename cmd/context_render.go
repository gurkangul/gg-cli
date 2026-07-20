package cmd

import (
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// compactLineWidth caps per-item text in compact output so a bundle of
// long-titled items still fits in a standard terminal.
const compactLineWidth = 80

// ggIDPattern matches structured IDs (TASK-NNN, BUG-NNN, DISC-NNN) embedded
// in prose. Used by compactTrim to avoid silently cutting a reference.
var ggIDPattern = regexp.MustCompile(`\b(TASK|BUG|DISC)-\d+\b`)

func compactTrim(s string, n int) string {
	if n <= 1 {
		if n == 1 && s != "" {
			return "…"
		}
		return ""
	}
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	// String will be truncated. Collect all GG IDs whose start byte falls at or
	// beyond the cut point — rescue every one, not just the first. IDs are ASCII
	// so byte positions from the regexp map 1:1 to rune positions in the prefix.
	cutByteOffset := len(string(runes[:n-1])) // byte position of the cut point
	var rescued []string
	for _, loc := range ggIDPattern.FindAllStringIndex(s, -1) {
		if loc[0] >= cutByteOffset {
			rescued = append(rescued, s[loc[0]:loc[1]])
		}
	}
	if len(rescued) > 0 {
		suffix := " …(" + strings.Join(rescued, ", ") + ")"
		overhead := []rune(suffix)
		prefixLen := n - len(overhead)
		if prefixLen < 1 {
			prefixLen = 1
		}
		return string(runes[:prefixLen]) + suffix
	}
	return strings.TrimRight(string(runes[:n-1]), " ") + "…"
}

func shortDate(ts string) string {
	if len(ts) >= 10 {
		return ts[:10]
	}
	return "—"
}

func taskStatusIcon(status string) string {
	switch status {
	case "done":
		return "✓"
	case "in_progress":
		return "→"
	case "blocked":
		return "!"
	default:
		return "○"
	}
}

func discStatusMark(status string) string {
	switch status {
	case "resolved":
		return "✓"
	case "dismissed":
		return "–"
	default:
		return "?"
	}
}

// printContextBundle renders a context bundle as human-readable text or JSON.
// errs is a list of non-fatal collection errors to show as warnings.
// cachedAt, when non-zero, indicates the results are from the LKG cache and
// adds "cached_at" and "stale_seconds" to the JSON output so agents know the
// data may be stale.
func printContextBundle(cmd *cobra.Command, query string, bundle contextBundle, errs []string, cachedAt time.Time) error {
	conflicts := detectConflicts(bundle)

	jsonPayload := map[string]any{
		"query":           query,
		"decisions":       bundle.decisions,
		"rejections":      bundle.rejections,
		"tasks":           bundle.tasks,
		"discussions":     bundle.discussions,
		"notes":           bundle.notes,
		"artifacts":       bundle.artifacts,
		"source_projects": bundle.sources,
		"conflicts":       conflicts,
		"warnings":        errs,
	}
	// BUG-076: surface per-collection query failures so a partial-failure empty
	// list (e.g. rejections errored) is not misread as authoritative-empty.
	if collErrs := bundleCollectionErrors(bundle); len(collErrs) > 0 {
		jsonPayload["collection_errors"] = collErrs
	}
	if !cachedAt.IsZero() {
		jsonPayload["cached_at"] = cachedAt.UTC().Format(time.RFC3339)
		jsonPayload["stale_seconds"] = int(time.Since(cachedAt).Seconds())
	}

	return printJSON(jsonPayload, func() {
		if contextBudget > 0 {
			emitCompact(cmd, "context",
				func(w io.Writer) { renderContextDefault(w, query, bundle, errs, conflicts) },
				func(w io.Writer) { renderContextBudget(w, query, bundle, errs, contextBudget) },
				compactRendererV_context,
			)
			return
		}
		if isCompactActive(cmd) {
			emitCompact(cmd, "context",
				func(w io.Writer) { renderContextDefault(w, query, bundle, errs, conflicts) },
				func(w io.Writer) { renderContextCompact(w, query, bundle, errs, conflicts) },
				compactRendererV_context,
			)
			return
		}
		renderContextDefault(os.Stdout, query, bundle, errs, conflicts)
	})
}

func renderContextDefault(w io.Writer, query string, bundle contextBundle, errs []string, conflicts []contextConflict) {
	fmt.Fprintf(w, "CONTEXT BUNDLE: %q\n", query)
	fmt.Fprintln(w, strings.Repeat("─", 60))

	if len(conflicts) > 0 {
		fmt.Fprintln(w, "\nCONFLICTS:")
		for _, c := range conflicts {
			fmt.Fprintf(w, "  ⚡ [%s] %s references resolved (%q) but live status is %s — live status is authoritative\n",
				c.ID, c.SourceType, c.ClosureToken, c.LiveStatus)
		}
	}

	if len(bundle.decisions) > 0 {
		fmt.Fprintln(w, "\nDECISIONS:")
		for _, dec := range bundle.decisions {
			pin := ""
			if dec.Pinned {
				pin = "📌 "
			}
			fmt.Fprintf(w, "  • %s%s[%s] %s\n", pin, sourcePrefix(bundle.sources.get("decision", dec.ID)), shortDate(dec.CreatedAt), dec.Text)
			if dec.Reason != "" {
				fmt.Fprintf(w, "    Reason: %s\n", dec.Reason)
			}
			// BUG-086: surface evidence so a verified fact is distinguishable from
			// an unverified claim. TASK-519: and so a check made eight months ago
			// is distinguishable from one made this morning — the tier is derived
			// from verification age and never changes the decision's validity.
			tier := trustTier(dec, time.Now())
			if dec.Evidence != "" {
				fmt.Fprintf(w, "    Evidence: %s %s\n", dec.Evidence, trustLabel(tier))
			} else {
				fmt.Fprintf(w, "    %s\n", trustLabel(tier))
			}
			if len(dec.Tags) > 0 {
				fmt.Fprintf(w, "    Tags: %s\n", strings.Join(dec.Tags, ", "))
			}
			if dec.TaskID != "" {
				fmt.Fprintf(w, "    Task: %s\n", dec.TaskID)
			}
		}
	}

	if len(bundle.rejections) > 0 {
		fmt.Fprintln(w, "\nREJECTIONS:")
		for _, r := range bundle.rejections {
			fmt.Fprintf(w, "  ✗ %s[%s] %s\n", sourcePrefix(bundle.sources.get("rejection", r.ID)), shortDate(r.CreatedAt), r.Approach)
			if r.Reason != "" {
				fmt.Fprintf(w, "    Reason: %s\n", r.Reason)
			}
			if len(r.Tags) > 0 {
				fmt.Fprintf(w, "    Tags: %s\n", strings.Join(r.Tags, ", "))
			}
			if r.TaskID != "" {
				fmt.Fprintf(w, "    Task: %s\n", r.TaskID)
			}
		}
	}

	if len(bundle.tasks) > 0 {
		fmt.Fprintln(w, "\nTASKS:")
		for _, t := range bundle.tasks {
			fmt.Fprintf(w, "  %s %s[%s] %s — %s\n", taskStatusIcon(t.Status), sourcePrefix(bundle.sources.get("task", t.ID)), t.ID, t.Title, t.Priority)
			if t.Detail != "" {
				fmt.Fprintf(w, "    %s\n", compactTrim(t.Detail, 120))
			}
		}
	}

	if len(bundle.discussions) > 0 {
		fmt.Fprintln(w, "\nDISCUSSIONS:")
		for _, disc := range bundle.discussions {
			fmt.Fprintf(w, "  %s %s[%s] %s\n", discStatusMark(disc.Status), sourcePrefix(bundle.sources.get("discussion", disc.ID)), disc.ID, disc.Topic)
			if disc.Detail != "" {
				fmt.Fprintf(w, "    %s\n", compactTrim(disc.Detail, 120))
			}
			if disc.Status == "resolved" && disc.ResolvedNote != "" {
				fmt.Fprintf(w, "    Resolved: %s\n", disc.ResolvedNote)
			}
			if contextFullTranscript && len(disc.Turns) > 0 {
				fmt.Fprintf(w, "    Transcript (%d turns):\n", len(disc.Turns))
				for i, t := range disc.Turns {
					fmt.Fprintf(w, "      [%d] %s (%s): %s\n", i+1, t.By, t.Role, t.Text)
				}
			} else if len(disc.Turns) > 0 {
				last := disc.Turns[len(disc.Turns)-1]
				fmt.Fprintf(w, "    Latest: %s (%s) — %s\n", last.By, last.Role, last.Text)
				if len(disc.Turns) > 1 {
					fmt.Fprintf(w, "    (%d more turns — use --full to show all)\n", len(disc.Turns)-1)
				}
			}
		}
	}

	if len(bundle.notes) > 0 {
		fmt.Fprintln(w, "\nNOTES:")
		for _, n := range bundle.notes {
			fmt.Fprintf(w, "  %s[%s]", sourcePrefix(bundle.sources.get("note", n.ID)), shortDate(n.CreatedAt))
			if n.TaskID != "" {
				fmt.Fprintf(w, " (%s)", n.TaskID)
			}
			fmt.Fprintf(w, " %s\n", n.Text)
		}
	}

	if len(bundle.artifacts) > 0 {
		fmt.Fprintln(w, "\nARTIFACTS:")
		for _, a := range bundle.artifacts {
			stale := ""
			if a.Stale {
				stale = " stale"
			}
			fmt.Fprintf(w, "  [%s:%d-%d]%s\n", a.Path, a.StartLine, a.EndLine, stale)
			for _, line := range strings.Split(compactTrim(a.Text, 220), "\n") {
				fmt.Fprintf(w, "    %s\n", line)
			}
		}
	}

	fmt.Fprintln(w)
	fmt.Fprintf(w, "  %d decisions  %d rejections  %d tasks  %d discussions  %d notes  %d artifacts\n",
		len(bundle.decisions), len(bundle.rejections), len(bundle.tasks), len(bundle.discussions), len(bundle.notes), len(bundle.artifacts))

	if len(errs) > 0 {
		fmt.Fprintf(w, "\nWarnings:\n  %s\n", strings.Join(errs, "\n  "))
	}
}

// renderContextCompact drops Reason/Detail/Tags/transcript bodies so an
// agent can scan a bundle and decide what to fetch in full. Typical 60-85%
// size reduction vs default rendering.
func renderContextCompact(w io.Writer, query string, bundle contextBundle, errs []string, conflicts []contextConflict) {
	conflictSuffix := ""
	if len(conflicts) > 0 {
		conflictSuffix = fmt.Sprintf(" ⚡%dX", len(conflicts))
	}
	fmt.Fprintf(w, "context: %q — %dD %dR %dT %d? %dN %dA%s\n\n",
		query,
		len(bundle.decisions), len(bundle.rejections),
		len(bundle.tasks), len(bundle.discussions), len(bundle.notes),
		len(bundle.artifacts),
		conflictSuffix)

	for _, c := range conflicts {
		fmt.Fprintf(w, "⚡ CONFLICT [%s] %s says resolved (%q) — live: %s\n", c.ID, c.SourceType, c.ClosureToken, c.LiveStatus)
	}
	if len(conflicts) > 0 {
		fmt.Fprintln(w)
	}

	writeCompactDecisionsWithSource(w, bundle.decisions, bundle.sources)
	writeCompactRejectionsWithSource(w, bundle.rejections, bundle.sources)
	writeCompactTasksWithSource(w, bundle.tasks, bundle.sources)
	writeCompactDiscussionsWithSource(w, bundle.discussions, bundle.sources)
	writeCompactNotesWithSource(w, bundle.notes, bundle.sources)
	writeCompactArtifacts(w, bundle.artifacts)

	if len(errs) > 0 {
		fmt.Fprintf(w, "\n! %s\n", strings.Join(errs, "; "))
	}
}

// renderForTask prints the for-task context bundle.
func renderForTask(w io.Writer, tb taskContextBundle, errs []string) {
	a := tb.anchor
	fmt.Fprintf(w, "FOR-TASK CONTEXT: %s — %s\n", a.ID, compactTrim(a.Title, 70))
	fmt.Fprintf(w, "  Status: %s | Priority: %s\n", a.Status, a.Priority)
	if a.Detail != "" {
		fmt.Fprintf(w, "  Detail: %s\n", compactTrim(a.Detail, 120))
	}
	if len(a.Tags) > 0 {
		fmt.Fprintf(w, "  Tags: %s\n", strings.Join(a.Tags, ", "))
	}
	fmt.Fprintln(w, strings.Repeat("─", 60))

	if len(tb.deps) > 0 {
		fmt.Fprintf(w, "\nDEPENDENCIES (%d):\n", len(tb.deps))
		for _, dep := range tb.deps {
			fmt.Fprintf(w, "  %s %s  %s (%s)\n",
				taskStatusIcon(dep.Status), dep.ID, compactTrim(dep.Title, 60), dep.Priority)
		}
	}

	if len(tb.bundle.decisions) > 0 {
		fmt.Fprintf(w, "\nRELATED DECISIONS (%d):\n", len(tb.bundle.decisions))
		for _, dec := range tb.bundle.decisions {
			line := compactDecisionLine(dec)
			if dec.ID != "" {
				line = fmt.Sprintf("[%s] %s", dec.ID, line)
			}
			fmt.Fprintf(w, "  %s\n", line)
		}
	}

	if len(tb.bundle.rejections) > 0 {
		fmt.Fprintf(w, "\nRELATED REJECTIONS (%d):\n", len(tb.bundle.rejections))
		for _, r := range tb.bundle.rejections {
			line := compactRejectionLine(r)
			if r.ID != "" {
				line = fmt.Sprintf("[%s] %s", r.ID, line)
			}
			fmt.Fprintf(w, "  %s\n", line)
		}
	}

	if len(tb.bundle.notes) > 0 {
		fmt.Fprintf(w, "\nRELATED NOTES (%d):\n", len(tb.bundle.notes))
		for _, n := range tb.bundle.notes {
			fmt.Fprintf(w, "  [%s] %s %s\n", n.ID, shortDate(n.CreatedAt), compactTrim(n.Text, 100))
		}
	}

	if len(tb.messages) > 0 {
		fmt.Fprintf(w, "\nRECENT TASK MESSAGES (%d):\n", len(tb.messages))
		for _, m := range tb.messages {
			fmt.Fprintf(w, "  [%s → %s] %s\n", m.FromRole, m.ToRole, compactTrim(m.Content, 100))
		}
	}

	fmt.Fprintln(w)
	fmt.Fprintf(w, "  %d decisions  %d rejections  %d notes  %d messages  %d deps\n",
		len(tb.bundle.decisions), len(tb.bundle.rejections), len(tb.bundle.notes), len(tb.messages), len(tb.deps))

	if len(errs) > 0 {
		fmt.Fprintf(w, "\nWarnings:\n  %s\n", strings.Join(errs, "\n  "))
	}
}
