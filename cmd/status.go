package cmd

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/enforcement"
	"github.com/gurkangul/gg-cli/internal/filesize"
	"github.com/gurkangul/gg-cli/internal/outbox"
	"github.com/gurkangul/gg-cli/internal/store"
	"github.com/gurkangul/gg-cli/internal/telemetry"
	"github.com/gurkangul/gg-cli/internal/trace"
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
	// so the user sees whatever is available when Qdrant is partially degraded.
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
	// race with a concurrent `gg inbox` between Count and Scroll.
	messages, messagesErr := d.store.GetInbox(ctx, "", true)
	if messagesErr != nil {
		fmt.Fprintln(os.Stderr, "warning: get inbox:", messagesErr)
	}

	// Open discussions — unresolved topics the next agent must close.
	openDiscs, discsErr := d.store.ListDiscussions(ctx, "open")
	if discsErr != nil {
		fmt.Fprintln(os.Stderr, "warning: list discussions:", discsErr)
	}

	// Recent decisions
	decisions, decisionsErr := d.store.ListDecisions(ctx, 5)
	if decisionsErr != nil {
		fmt.Fprintln(os.Stderr, "warning: list decisions:", decisionsErr)
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

	// Outbox backlog — pending Memgraph writes from crashed gg index runs.
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
		"open_tasks":  openTasks,
		"messages":    messages,
		"discussions": openDiscs,
		"decisions":   decisions,
		"rejections":  rejections,
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
				if tsum, tErr := telemetry.Summarize(rtDir); tErr == nil {
					agentPct := pct(tsum.AgentCalls, tsum.Total)
					if tsum.Total > 0 {
						fmt.Printf("North Star  Last 7d: %d calls, %d%% agent-initiated\n", tsum.Total, agentPct)
					} else {
						fmt.Println("North Star  Last 7d: no calls recorded yet")
					}
					if tsum.CompactCalls > 0 && tsum.CompactBytesDefault > 0 {
						saved := tsum.CompactBytesDefault - tsum.CompactBytesOut
						pctSaved := float64(saved) / float64(tsum.CompactBytesDefault) * 100
						fmt.Printf("Compact     %d calls, %s saved (avg %.0f%% reduction)\n",
							tsum.CompactCalls, humanFileSize(int64(saved)), pctSaved)
					}
					fmt.Println()
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
		}

		// Outbox — pending Memgraph writes. Any count > 0 is a signal the
		// reconcile loop has not run since the last crash; the oldest-age
		// line tells the operator how stale the backlog is.
		if outboxCount > 0 {
			ageDetail := ""
			if outboxOldestAge != "" {
				ageDetail = "  (oldest: " + outboxOldestAge + " ago — run `gg doctor --reconcile`)"
			}
			fmt.Printf("\nOUTBOX:\n  ⏳ Pending: %d%s\n", outboxCount, ageDetail)
		}

		if messagesErr == nil {
			fmt.Printf("\nMESSAGES:\n  Unread: %d\n", len(messages))
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
			fmt.Println("\nRECENT DECISIONS:")
			for _, dec := range decisions {
				fmt.Printf("  • %s\n", dec.Text)
				if len(dec.Tags) > 0 {
					fmt.Printf("    Tags: %s\n", strings.Join(dec.Tags, ", "))
				}
			}
		}

		if rejectionsErr == nil && len(rejections) > 0 {
			fmt.Println("\nRECENT REJECTIONS:")
			for _, r := range rejections {
				fmt.Printf("  ✗ %s\n", r.Approach)
			}
		}

		// Bug reopen stats — best-effort weekly summary for the reopen-rate metric.
		if bugTotal, bugByDir, bugErr := d.store.BugReopenStats(ctx); bugErr == nil && bugTotal > 0 {
			fmt.Printf("\nBUGS REOPENED (last 7d): %d\n", bugTotal)
			type kv struct {
				dir   string
				count int
			}
			var dirs []kv
			for dir, c := range bugByDir {
				dirs = append(dirs, kv{dir, c})
			}
			sort.Slice(dirs, func(i, j int) bool {
				if dirs[i].count != dirs[j].count {
					return dirs[i].count > dirs[j].count
				}
				return dirs[i].dir < dirs[j].dir
			})
			for _, kv := range dirs {
				fmt.Printf("  %-20s %d\n", kv.dir, kv.count)
			}
		}

		// Trace percentiles — shown only when trace data exists (GG_TRACE=1 must
		// have been set during prior runs). Best-effort; missing files are silent.
		if ggDir, dirErr := config.GGDir(); dirErr == nil {
			if pcts, pErr := trace.ReadPercentiles(ggDir, 100); pErr == nil && pcts.N > 0 {
				fmt.Printf("\nOP LATENCY (last %d spans — p50 %s  p95 %s  p99 %s)\n",
					pcts.N,
					trace.FmtMs(pcts.P50),
					trace.FmtMs(pcts.P95),
					trace.FmtMs(pcts.P99),
				)
			}
		}

		// Telemetry — best-effort weekly summary.
		if cfg2, cfgErr2 := config.Load(); cfgErr2 == nil {
			if rtDir2, rtErr2 := cfg2.RuntimeDir(); rtErr2 == nil {
				if tsum, tErr := telemetry.Summarize(rtDir2); tErr == nil && tsum.Total > 0 {
					fmt.Printf("\nTELEMETRY (last 7 days — %d calls, %d%% agent-initiated):\n",
						tsum.Total, pct(tsum.AgentCalls, tsum.Total))
					// Print verb breakdown sorted by count desc.
					type kv struct {
						verb  string
						count int
					}
					var verbs []kv
					for v, c := range tsum.VerbCounts {
						verbs = append(verbs, kv{v, c})
					}
					sort.Slice(verbs, func(i, j int) bool {
						if verbs[i].count != verbs[j].count {
							return verbs[i].count > verbs[j].count
						}
						return verbs[i].verb < verbs[j].verb
					})
					for _, v := range verbs {
						fmt.Printf("  %-16s %d\n", v.verb, v.count)
					}
				}
			}
		}

		// Inbox-obedience warnings — best-effort, never fatal.
		since7d := time.Now().UTC().Add(-7 * 24 * time.Hour)
		if rows, obErr := computeObedienceRows(ctx, d.store, since7d, ""); obErr == nil {
			for _, r := range rows {
				if r.LowCompliance {
					fmt.Printf("\n⚠ Inbox obedience: agent %q %.0f%% (%d/%d) last 7d — run `gg audit inbox-obedience` for detail\n",
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

func pct(part, total int) int {
	if total == 0 {
		return 0
	}
	return (part * 100) / total
}

func fmtCount(n uint64, err error) string {
	if err != nil {
		return "?"
	}
	return fmt.Sprintf("%d", n)
}
