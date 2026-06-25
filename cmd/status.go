package cmd

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/enforcement"
	"github.com/gurkangul/gg-cli/internal/filesize"
	"github.com/gurkangul/gg-cli/internal/outbox"
	"github.com/gurkangul/gg-cli/internal/store"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Orient yourself — show open tasks, pending messages, and recent decisions",
	Long: `Run at the start of every session to orient yourself in the project.

Shows: active tasks (by priority), blocked items, unread messages, recent decisions,
and a 7-day telemetry summary (if enabled).

See also: gg status render (write STATUS.md for CI), gg search (find specific context)`,
	RunE: runStatus,
}

func init() {
	rootCmd.AddCommand(statusCmd)
}

func runStatus(cmd *cobra.Command, args []string) error {
	d, err := loadDeps(false)
	if err != nil {
		return err
	}
	defer d.Close()

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	// Tasks summary — each sub-count is allowed to fail independently with "?"
	// so the user sees whatever is available when the embedded vector store is partially degraded.
	counts := map[string]struct {
		n   uint64
		err error
	}{}
	for _, s := range []string{"pending", "in_progress", "blocked", "done"} {
		n, err := d.store.CountTasks(ctx, s)
		counts[s] = struct {
			n   uint64
			err error
		}{n, err}
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: count tasks (%s): %v\n", s, err)
		}
	}

	var openTasks []store.Task
	hasOpen := counts["pending"].n+counts["in_progress"].n+counts["blocked"].n > 0
	if hasOpen {
		tasks, err := d.store.ListTasks(ctx, "")
		if err != nil {
			fmt.Fprintln(os.Stderr, "warning: list tasks:", err)
		} else {
			for _, t := range tasks {
				if t.Status == "done" {
					continue
				}
				openTasks = append(openTasks, t)
			}
		}
	}

	// Messages — fetch once and derive the count from the result so we can't
	// race with a concurrent `gg inbox` between Count and Scroll. The human-only
	// set drives the preview; the all-audience set lets us label how many of the
	// total are agent broadcasts (which `gg inbox` hides by default) so the
	// "Unread" line stops disagreeing with the inbox view (BUG-091).
	messages, messagesErr := d.store.GetInbox(ctx, "", true, "")
	if messagesErr != nil {
		fmt.Fprintln(os.Stderr, "warning: get inbox:", messagesErr)
	}
	agentBroadcasts := 0
	if messagesErr == nil {
		if all, allErr := d.store.GetInbox(ctx, "", false, ""); allErr == nil {
			if m := len(all) - len(messages); m > 0 {
				agentBroadcasts = m
			}
		} else {
			fmt.Fprintln(os.Stderr, "warning: get inbox (all-audience):", allErr)
		}
	}

	// Open discussions — unresolved topics the next agent must close.
	openDiscs, discsErr := d.store.ListDiscussions(ctx, "open")
	if discsErr != nil {
		fmt.Fprintln(os.Stderr, "warning: list discussions:", discsErr)
	}

	// Recent decisions — fetch all active to learn the true total, then show only
	// the top few. The breadcrumb below makes the cap non-silent so a durable rule
	// that has slipped past the window is still discoverable instead of vanishing
	// (TASK-513).
	const recentDecisionsShown = 5
	allActiveDecisions, decisionsErr := d.store.ListDecisions(ctx, 0, false)
	if decisionsErr != nil {
		fmt.Fprintln(os.Stderr, "warning: list decisions:", decisionsErr)
	}
	totalActiveDecisions := len(allActiveDecisions)
	decisions := allActiveDecisions
	if len(decisions) > recentDecisionsShown {
		decisions = decisions[:recentDecisionsShown]
	}

	// Recent rejections
	rejections, rejectionsErr := d.store.ListRejections(ctx, 5)
	if rejectionsErr != nil {
		fmt.Fprintln(os.Stderr, "warning: list rejections:", rejectionsErr)
	}

	// Bug counts by status — mirrors task counts so operators see defect
	// pressure alongside work pressure. Each sub-count degrades independently.
	bugCounts := map[string]uint64{}
	for _, s := range []string{"open", "fixing", "fixed", "wontfix", "reopened"} {
		n, err := d.store.CountBugs(ctx, s)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: count bugs (%s): %v\n", s, err)
			continue
		}
		bugCounts[s] = n
	}
	// Dupe-acknowledged count: bugs filed despite duplicate warning (advisory signal).
	dupeAcknowledged, _ := d.store.CountBugsByTag(ctx, "dupe-acknowledged")

	// Linked projects — read-only cross-project sources consulted only when the
	// caller passes --include-linked to search/context. Surfaced so agents who
	// configured links actually discover the flag (TASK-503). Best-effort: a
	// config load failure simply yields 0 and the line is suppressed.
	var linkedProjectsCount int
	if cfg, cfgErr := config.Load(); cfgErr == nil {
		linkedProjectsCount = len(cfg.LinkedProjects)
	}

	// Index-hook install state (TASK-502) for the JSON payload, mirroring the
	// human compact line in renderCodeGraphStatusCompact.
	indexHooksOn := false
	if root, rootErr := config.FindRoot(); rootErr == nil {
		indexHooksOn = indexHooksInstalled(root)
	}

	// Outbox backlog — pending code-graph outbox writes from crashed gg index runs.
	// Unbounded growth signals that `gg doctor --reconcile` is never run.
	var outboxCount int
	var outboxOldestAge string
	if ggDir, ggErr := config.GGDir(); ggErr == nil {
		if entries, obErr := outbox.List(ggDir); obErr == nil {
			outboxCount = len(entries)
			var oldest time.Time
			for _, e := range entries {
				if t, perr := time.Parse(time.RFC3339, e.CreatedAt); perr == nil {
					if oldest.IsZero() || t.Before(oldest) {
						oldest = t
					}
				}
			}
			if !oldest.IsZero() {
				outboxOldestAge = time.Since(oldest).Round(time.Minute).String()
			}
		}
	}

	type countVal struct {
		Pending    uint64 `json:"pending"`
		InProgress uint64 `json:"in_progress"`
		Blocked    uint64 `json:"blocked"`
		Done       uint64 `json:"done"`
	}
	payload := map[string]any{
		"task_counts": countVal{
			Pending:    counts["pending"].n,
			InProgress: counts["in_progress"].n,
			Blocked:    counts["blocked"].n,
			Done:       counts["done"].n,
		},
		"bug_counts": bugCounts,
		"outbox": map[string]any{
			"pending":    outboxCount,
			"oldest_age": outboxOldestAge,
		},
		"linked_projects":       linkedProjectsCount,
		"index_hooks_installed": indexHooksOn,
		"open_tasks":            openTasks,
		"messages":              messages,
		"discussions":           openDiscs,
		"decisions":             decisions,
		"rejections":            rejections,
	}

	return printJSON(payload, func() {
		// Stabilization banner — when enforcement is off the install-hook,
		// gsd-guard, and pre-task-done verify gate are all silent no-ops.
		// Without this line an operator has no way to tell the guards are
		// asleep. Stays at the very top so it cannot be missed.
		if !enforcement.Enabled() {
			fmt.Printf("⚠ guards=off  (unset %s or set =on to re-enable: install-agent-hooks, gsd-guard, pre-task-done gate)\n\n", enforcement.EnvVar)
		}

		// North Star metric — one-liner at the very top so agents and humans
		// can instantly gauge dogfood adoption without scrolling.
		if cfg, cfgErr := config.Load(); cfgErr == nil {
			if rtDir, rtErr := cfg.RuntimeDir(); rtErr == nil {
				renderNorthStarBlock(rtDir)
			}
		}

		// Code graph freshness belongs near the top so agents see stale impact
		// data before choosing a task.
		if cfg, cfgErr := config.Load(); cfgErr == nil {
			if ggDir, ggErr := config.GGDir(); ggErr == nil {
				if root, rootErr := config.FindRoot(); rootErr == nil {
					fmt.Println(renderCodeGraphStatusCompact(codeGraphStatusWithTimeout(root, ggDir, cfg)))
				}
			}
		}

		fmt.Println("TASKS:")
		fmt.Printf("  ○ Pending: %s  → In Progress: %s  ⚠ Blocked: %s  ✓ Done: %s\n",
			fmtCount(counts["pending"].n, counts["pending"].err),
			fmtCount(counts["in_progress"].n, counts["in_progress"].err),
			fmtCount(counts["blocked"].n, counts["blocked"].err),
			fmtCount(counts["done"].n, counts["done"].err),
		)
		// Show all actionable tasks (pending + in_progress). Blocked tasks are
		// counted in the summary line above and surface via `gg task list
		// --status blocked` — no need to clutter the inline list with them.
		for _, t := range openTasks {
			if t.Status == "blocked" {
				continue
			}
			fmt.Printf("  %s %s [%s] %s\n", statusIcon(t.Status), t.ID, t.Priority, t.Title)
		}

		// Bugs — pressure gauge. Skip silently when every count errored.
		if bugCounts["open"]+bugCounts["fixing"]+bugCounts["reopened"]+bugCounts["fixed"]+bugCounts["wontfix"] > 0 {
			fmt.Println("\nBUGS:")
			fmt.Printf("  ● Open: %d  ⚙ Fixing: %d  ↻ Reopened: %d  ✓ Fixed: %d  ⊘ Wontfix: %d\n",
				bugCounts["open"], bugCounts["fixing"], bugCounts["reopened"], bugCounts["fixed"], bugCounts["wontfix"])
			if dupeAcknowledged > 0 {
				fmt.Printf("  ⓘ Dupe-acknowledged: %d  (filed despite similarity warning — run `gg search dupe-acknowledged` to review)\n",
					dupeAcknowledged)
			}
		}

		// Outbox — pending code-graph outbox writes. Any count > 0 is a signal the
		// reconcile loop has not run since the last crash; the oldest-age
		// line tells the operator how stale the backlog is.
		if outboxCount > 0 {
			ageDetail := ""
			if outboxOldestAge != "" {
				ageDetail = "  (oldest: " + outboxOldestAge + " ago — run `gg doctor --reconcile`)"
			}
			fmt.Printf("\nOUTBOX:\n  ⏳ Pending: %d%s\n", outboxCount, ageDetail)
		}

		// Linked projects — discoverability nudge (TASK-503). Only shown when the
		// project actually configured links, so it never spams a solo project.
		if linkedProjectsCount > 0 {
			fmt.Printf("\nLinked projects: %d — use --include-linked in search/context for cross-project results.\n",
				linkedProjectsCount)
		}

		if messagesErr == nil {
			unreadLine := fmt.Sprintf("  Unread: %d project-wide", len(messages))
			if agentBroadcasts > 0 {
				unreadLine += fmt.Sprintf("  (+%d agent-broadcasts)", agentBroadcasts)
			}
			fmt.Printf("\nMESSAGES:\n%s\n", unreadLine)
			const maxInline = 5
			preview := messages
			truncated := 0
			if len(messages) > maxInline {
				preview = messages[:maxInline]
				truncated = len(messages) - maxInline
			}
			for _, m := range preview {
				content := m.Content
				if len(content) > 100 {
					content = content[:97] + "…"
				}
				fmt.Printf("  [%s → %s] %s\n", m.FromRole, m.ToRole, content)
			}
			if truncated > 0 {
				fmt.Printf("  … and %d more (run `gg inbox` for full list)\n", truncated)
			}
			if len(messages) > 0 {
				fmt.Println("  (run `gg inbox` to mark as read)")
			}
		}

		if discsErr == nil && len(openDiscs) > 0 {
			fmt.Printf("\nOPEN DISCUSSIONS (%d — resolve or dismiss before closing session):\n", len(openDiscs))
			for _, disc := range openDiscs {
				fmt.Printf("  • %s — %s\n", disc.ID, disc.Topic)
			}
		}

		if decisionsErr == nil && len(decisions) > 0 {
			if totalActiveDecisions > len(decisions) {
				fmt.Printf("\nRECENT DECISIONS (%d of %d shown):\n", len(decisions), totalActiveDecisions)
			} else {
				fmt.Println("\nRECENT DECISIONS:")
			}
			for _, dec := range decisions {
				fmt.Printf("  • %s\n", dec.Text)
				if len(dec.Tags) > 0 {
					fmt.Printf("    Tags: %s\n", strings.Join(dec.Tags, ", "))
				}
			}
			if totalActiveDecisions > len(decisions) {
				fmt.Printf("  …%d older not shown — gg decisions · gg search \"<topic>\" · gg canon show\n", totalActiveDecisions-len(decisions))
			}
		}

		if rejectionsErr == nil && len(rejections) > 0 {
			fmt.Println("\nRECENT REJECTIONS:")
			for _, r := range rejections {
				fmt.Printf("  ✗ %s\n", r.Approach)
			}
		}

		// Bug reopen stats — best-effort weekly summary for the reopen-rate metric.
		if bugTotal, bugByDir, bugErr := d.store.BugReopenStats(ctx); bugErr == nil {
			renderBugReopenBlock(bugTotal, bugByDir)
		}

		// Trace percentiles and telemetry verb breakdown.
		if ggDir, dirErr := config.GGDir(); dirErr == nil {
			renderTracePercentilesBlock(ggDir)
		}
		if cfg2, cfgErr2 := config.Load(); cfgErr2 == nil {
			if rtDir2, rtErr2 := cfg2.RuntimeDir(); rtErr2 == nil {
				renderTelemetryVerbBlock(rtDir2)
			}
		}

		// Inbox handoff acknowledgement warnings — best-effort, never fatal.
		since7d := time.Now().UTC().Add(-7 * 24 * time.Hour)
		if rows, obErr := computeObedienceRows(ctx, d.store, since7d, ""); obErr == nil {
			for _, r := range rows {
				if r.LowCompliance {
					fmt.Printf("\n⚠ Inbox handoff coverage: role %q acknowledged %.0f%% (%d/%d) role-targeted message(s) last 7d — run `gg audit inbox-obedience` for detail\n",
						r.Role, r.ObedienceRatio*100, r.Acknowledged, r.Received)
				}
			}
		}

		// File-size drift summary: show a one-liner when new violations exist.
		if cwd, cwdErr := os.Getwd(); cwdErr == nil {
			if allFiles, scanErr := filesize.ScanDir(cwd); scanErr == nil {
				if b, bErr := filesize.ReadBaseline(cwd); bErr == nil {
					violations := filesize.CheckViolations(allFiles, b, true)
					if len(violations) > 0 {
						fmt.Printf("\nFile-size drift: %d new violation(s) (baseline frozen at %d) — run `gg audit file-size`\n",
							len(violations), len(b.Files))
					}
				}
			}
		}
	})
}
