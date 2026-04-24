package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/store"
)

// compactRendererV stamps every emitCompact telemetry entry so aggregates
// across format changes can be bucketed. Bump whenever the per-line compact
// output changes meaningfully (drop/add a field, change separators, etc.) —
// a constant-in-code invariant rather than a git-history diff, so
// `gg status` can trust its "avg % reduction" metric.
const compactRendererV = 2

// isCompactActive returns true when the caller wants compact rendering.
//
// Resolution order (first match wins):
//  1. --compact flag is set explicitly → honour it (true/false as given).
//  2. GG_COMPACT env is 1/true/yes/on → compact on (or 0/false/no/off → off).
//  3. Agent origin (GG_ROLE or GG_AGENT env, or --from flag) AND the command
//     actually registered a --compact flag → compact on. The flag-registration
//     check is the opt-in signal: only commands that have implemented a
//     compact render path participate in agent auto-compact, so adding a new
//     command without a --compact flag can never silently swallow output.
//  4. Otherwise → off.
//
// Rationale: agents running without thinking about flags are the highest-cost
// callers — default them to compact; humans stay on rich default unless they
// explicitly ask. The flag always wins so an agent can force-opt-out for
// debugging.
func isCompactActive(cmd *cobra.Command) bool {
	if cmd != nil {
		if f := cmd.Flags().Lookup("compact"); f != nil && f.Changed {
			return f.Value.String() == "true"
		}
	}
	switch strings.ToLower(strings.TrimSpace(os.Getenv("GG_COMPACT"))) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	// Agent auto-compact only fires on commands that opted in by registering
	// a --compact flag. Without this guard, a future caller that invokes
	// isCompactActive on a command with no compact render path would silently
	// return true for agents and the output layer would diverge. nil cmd
	// skips this (callers should supply a cmd) — there's no safe default
	// without knowing whether a compact path exists.
	if cmd == nil || cmd.Flags().Lookup("compact") == nil {
		// Compact was implicitly requested by the agent environment but this
		// command has no compact render path — record the gap so maintainers
		// can prioritise adding emitCompact to the missing command.
		if cmd != nil &&
			(strings.TrimSpace(os.Getenv("GG_ROLE")) != "" ||
				strings.TrimSpace(os.Getenv("GG_AGENT")) != "") {
			emitCompactMissing(cmd, cmd.Name())
		}
		return false
	}
	if strings.TrimSpace(os.Getenv("GG_ROLE")) != "" ||
		strings.TrimSpace(os.Getenv("GG_AGENT")) != "" {
		return true
	}
	if f := cmd.Flags().Lookup("from"); f != nil && strings.TrimSpace(f.Value.String()) != "" {
		return true
	}
	return false
}

// ── Line builders ──────────────────────────────────────────────────────────
//
// These produce the single-line compact form shared across commands.
// Keeping them in one file means a format tweak (e.g. swap date position,
// change suffix marker) ripples to every command that emits compact output
// instead of drifting across 7+ call sites.

// compactDecisionLine renders a decision as:
//
//	D  YYYY-MM-DD  text[…]  [→TASK-NNN]
func compactDecisionLine(d store.Decision) string {
	suffix := ""
	if d.TaskID != "" {
		suffix = " →" + d.TaskID
	}
	return fmt.Sprintf("D  %s  %s%s",
		shortDate(d.CreatedAt), compactTrim(d.Text, compactLineWidth), suffix)
}

// compactRejectionLine renders a rejection as:
//
//	R  YYYY-MM-DD  approach[…]  [→TASK-NNN]
func compactRejectionLine(r store.Rejection) string {
	suffix := ""
	if r.TaskID != "" {
		suffix = " →" + r.TaskID
	}
	return fmt.Sprintf("R  %s  %s%s",
		shortDate(r.CreatedAt), compactTrim(r.Approach, compactLineWidth), suffix)
}

// compactTaskLine renders a task summary as:
//
//	T {icon} TASK-NNN  title[…] (priority)
func compactTaskLine(t store.Task) string {
	return fmt.Sprintf("T %s %s  %s (%s)",
		taskStatusIcon(t.Status), t.ID, compactTrim(t.Title, compactLineWidth), t.Priority)
}

// compactNoteLine renders a note as:
//
//	N  YYYY-MM-DD[  (TASK-NNN)]  text[…]
func compactNoteLine(n store.Note) string {
	taskRef := ""
	if n.TaskID != "" {
		taskRef = "  (" + n.TaskID + ")"
	}
	return fmt.Sprintf("N  %s%s  %s",
		shortDate(n.CreatedAt), taskRef, compactTrim(n.Text, compactLineWidth))
}

// compactDiscussionLine renders a discussion as:
//
//	? {mark} DISC-NNN  topic[…]  [(N turns)]
func compactDiscussionLine(d store.Discussion) string {
	suffix := ""
	if n := len(d.Turns); n > 0 {
		suffix = fmt.Sprintf(" (%d turns)", n)
	}
	return fmt.Sprintf("? %s %s  %s%s",
		discStatusMark(d.Status), d.ID, compactTrim(d.Topic, compactLineWidth), suffix)
}

// compactBugLine renders a bug as:
//
//	B  BUG-NNN [severity/status]  title[…]
func compactBugLine(b store.Bug) string {
	return fmt.Sprintf("B  %s [%s/%s]  %s",
		b.ID, b.Severity, b.Status, compactTrim(b.Title, compactLineWidth))
}

// compactMessageLine renders an inbox message as:
//
//	M  MM-DD HH:mm  from→to  content[…]  [→TASK-NNN]
//
// Date keeps time-of-day (trimmed) because inbox ordering is often about
// minutes, not days — "just now" vs "yesterday" matters for triage.
func compactMessageLine(m store.Message) string {
	ts := m.CreatedAt
	// "2026-04-20T23:12:00Z" → "04-20 23:12"
	switch {
	case len(ts) >= 16:
		ts = ts[5:10] + " " + ts[11:16]
	case len(ts) >= 10:
		ts = ts[5:10]
	default:
		ts = "—"
	}
	suffix := ""
	if m.TaskID != "" {
		suffix = " →" + m.TaskID
	}
	content := compactTrim(strings.ReplaceAll(m.Content, "\n", " "), compactLineWidth)
	return fmt.Sprintf("M  %s  %s→%s  %s%s", ts, m.FromRole, m.ToRole, content, suffix)
}

// ── Writer variants ────────────────────────────────────────────────────────

func writeCompactDecisions(w io.Writer, ds []store.Decision) {
	for _, d := range ds {
		fmt.Fprintln(w, compactDecisionLine(d))
	}
}

func writeCompactRejections(w io.Writer, rs []store.Rejection) {
	for _, r := range rs {
		fmt.Fprintln(w, compactRejectionLine(r))
	}
}

func writeCompactTasks(w io.Writer, ts []store.Task) {
	for _, t := range ts {
		fmt.Fprintln(w, compactTaskLine(t))
	}
}

func writeCompactNotes(w io.Writer, ns []store.Note) {
	for _, n := range ns {
		fmt.Fprintln(w, compactNoteLine(n))
	}
}

func writeCompactDiscussions(w io.Writer, ds []store.Discussion) {
	for _, d := range ds {
		fmt.Fprintln(w, compactDiscussionLine(d))
	}
}

func writeCompactBugs(w io.Writer, bs []store.Bug) {
	for _, b := range bs {
		fmt.Fprintln(w, compactBugLine(b))
	}
}

// humanTokenCount prints a token count with a K suffix for >1000 —
// agents skim for order-of-magnitude ("~12K tok saved") not exact precision.
//
// Visual discontinuity at the 10K boundary is intentional: below 10K we keep
// one decimal of resolution (9.9K), at/above 10K we drop to integer (10K).
// The jump from "9.9K" to "10K" (not "10.0K") is minor and acceptable — the
// metric is approximate (bytes/4) so extra precision would be misleading.
func humanTokenCount(n int) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	if n < 10_000 {
		return fmt.Sprintf("%.1fK", float64(n)/1000)
	}
	return fmt.Sprintf("%dK", n/1000)
}
