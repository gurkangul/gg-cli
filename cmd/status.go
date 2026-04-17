package cmd

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/config"
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
	messages, messagesErr := d.store.GetInbox(ctx, "")
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
		"open_tasks":  openTasks,
		"messages":    messages,
		"discussions": openDiscs,
		"decisions":   decisions,
		"rejections":  rejections,
	}

	return printJSON(payload, func() {
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
