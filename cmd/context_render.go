package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// compactLineWidth caps per-item text in compact output so a bundle of
// long-titled items still fits in a standard terminal.
const compactLineWidth = 80

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
	return string(runes[:n-1]) + "…"
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
	jsonPayload := map[string]any{
		"query":       query,
		"decisions":   bundle.decisions,
		"rejections":  bundle.rejections,
		"tasks":       bundle.tasks,
		"discussions": bundle.discussions,
		"notes":       bundle.notes,
		"warnings":    errs,
	}
	if !cachedAt.IsZero() {
		jsonPayload["cached_at"] = cachedAt.UTC().Format(time.RFC3339)
		jsonPayload["stale_seconds"] = int(time.Since(cachedAt).Seconds())
	}

	return printJSON(jsonPayload, func() {
		if contextBudget > 0 {
			emitCompact(cmd, "context",
				func(w io.Writer) { renderContextDefault(w, query, bundle, errs) },
				func(w io.Writer) { renderContextBudget(w, query, bundle, errs, contextBudget) },
			)
			return
		}
		if isCompactActive(cmd) {
			emitCompact(cmd, "context",
				func(w io.Writer) { renderContextDefault(w, query, bundle, errs) },
				func(w io.Writer) { renderContextCompact(w, query, bundle, errs) },
			)
			return
		}
		renderContextDefault(os.Stdout, query, bundle, errs)
	})
}

func renderContextDefault(w io.Writer, query string, bundle contextBundle, errs []string) {
	fmt.Fprintf(w, "CONTEXT BUNDLE: %q\n", query)
	fmt.Fprintln(w, strings.Repeat("─", 60))

	if len(bundle.decisions) > 0 {
		fmt.Fprintln(w, "\nDECISIONS:")
		for _, dec := range bundle.decisions {
			fmt.Fprintf(w, "  • [%s] %s\n", shortDate(dec.CreatedAt), dec.Text)
			if dec.Reason != "" {
				fmt.Fprintf(w, "    Reason: %s\n", dec.Reason)
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
			fmt.Fprintf(w, "  ✗ [%s] %s\n", shortDate(r.CreatedAt), r.Approach)
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
			fmt.Fprintf(w, "  %s [%s] %s — %s\n", taskStatusIcon(t.Status), t.ID, t.Title, t.Priority)
			if t.Detail != "" {
				fmt.Fprintf(w, "    %s\n", compactTrim(t.Detail, 120))
			}
		}
	}

	if len(bundle.discussions) > 0 {
		fmt.Fprintln(w, "\nDISCUSSIONS:")
		for _, disc := range bundle.discussions {
			fmt.Fprintf(w, "  %s [%s] %s\n", discStatusMark(disc.Status), disc.ID, disc.Topic)
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
			fmt.Fprintf(w, "  [%s]", shortDate(n.CreatedAt))
			if n.TaskID != "" {
				fmt.Fprintf(w, " (%s)", n.TaskID)
			}
			fmt.Fprintf(w, " %s\n", n.Text)
		}
	}

	fmt.Fprintln(w)
	fmt.Fprintf(w, "  %d decisions  %d rejections  %d tasks  %d discussions  %d notes\n",
		len(bundle.decisions), len(bundle.rejections), len(bundle.tasks), len(bundle.discussions), len(bundle.notes))

	if len(errs) > 0 {
		fmt.Fprintf(w, "\nWarnings:\n  %s\n", strings.Join(errs, "\n  "))
	}
}

// renderContextCompact drops Reason/Detail/Tags/transcript bodies so an
// agent can scan a bundle and decide what to fetch in full. Typical 60-85%
// size reduction vs default rendering.
func renderContextCompact(w io.Writer, query string, bundle contextBundle, errs []string) {
	fmt.Fprintf(w, "context: %q — %dD %dR %dT %d? %dN\n\n",
		query,
		len(bundle.decisions), len(bundle.rejections),
		len(bundle.tasks), len(bundle.discussions), len(bundle.notes))

	writeCompactDecisions(w, bundle.decisions)
	writeCompactRejections(w, bundle.rejections)
	writeCompactTasks(w, bundle.tasks)
	writeCompactDiscussions(w, bundle.discussions)
	writeCompactNotes(w, bundle.notes)

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
			fmt.Fprintf(w, "  %s\n", compactDecisionLine(dec))
		}
	}

	if len(tb.bundle.rejections) > 0 {
		fmt.Fprintf(w, "\nRELATED REJECTIONS (%d):\n", len(tb.bundle.rejections))
		for _, r := range tb.bundle.rejections {
			fmt.Fprintf(w, "  %s\n", compactRejectionLine(r))
		}
	}

	fmt.Fprintln(w)
	fmt.Fprintf(w, "  %d decisions  %d rejections  %d deps\n",
		len(tb.bundle.decisions), len(tb.bundle.rejections), len(tb.deps))

	if len(errs) > 0 {
		fmt.Fprintf(w, "\nWarnings:\n  %s\n", strings.Join(errs, "\n  "))
	}
}
